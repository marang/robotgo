"""Offline transaction tests; no remote access, real secrets, or services."""

import fcntl
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location('admission', Path(__file__).with_name('admit-controller.py'))
admission = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(admission)


class AdmissionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name) / 'controller'
        self.home.mkdir(mode=0o700)
        self.controller = admission.Controller(self.home, os.getuid(), os.getgid())
        self.config = b'fixture: controller policy with no secrets\n'
        self.hardener = b'fixture: original empty-history activation guard\n'
        for key, content in ((admission.CONFIG + '/config.yaml', self.config),
                             (admission.PROTECTED[1], self.hardener),
                             *((key, b'unchanged policy helper\n') for key in admission.PROTECTED[2:]),
                             (admission.LOCK, b'')):
            self.write(key, content)
        self.addCleanup(mock.patch.stopall)
        mock.patch.object(admission, 'CONFIG_SHA256', admission.sha(self.config)).start()
        mock.patch.object(admission, 'HARDENER_SHA256', admission.sha(self.hardener)).start()
        self.files = {'base.yaml': admission.sha(b'base'),
                      'dags/other-project.yaml': admission.sha(b'other-project')}
        self.write(admission.CONFIG + '/base.yaml', b'base')
        self.write(admission.CONFIG + '/dags/other-project.yaml', b'other-project')
        self.write(admission.MANIFEST, admission.encoded({'files': self.files}))
        for name in admission.OLD_HOLDS | {'.suspend.lock'}:
            self.write(admission.HOLDS + '/' + name, b'')
        self.baseline = self.controller.snapshot()
        self.baseline_sha = admission.sha(admission.encoded(self.baseline))

    def write(self, relative, content, mode=0o600):
        path = self.home / relative
        missing = []
        current = path.parent
        while not current.exists():
            missing.append(current)
            current = current.parent
        for item in reversed(missing):
            item.mkdir(mode=0o700)
        with path.open('wb') as target:
            target.write(content)
        path.chmod(mode)
        return path

    def apply(self):
        with self.controller.lock():
            return self.controller.admit(self.baseline, self.baseline_sha, apply=True)

    def bundle(self):
        self.write('review/baseline.json', admission.encoded(self.baseline))
        self.write('review/baseline.sha256', (self.baseline_sha + '\n').encode())
        for name, content in admission.definitions().items():
            self.write('review/bundle/' + name, content)
        self.write('review/bundle/admit-controller.py', Path(admission.__file__).read_bytes())
        return (Path('review/baseline.json'), Path('review/baseline.sha256'), Path('review/bundle'))

    def test_preview_has_no_writes(self):
        before = self.controller.snapshot()
        with self.controller.lock():
            result = self.controller.admit(self.baseline, self.baseline_sha)
        self.assertEqual(result['mode'], 'preview')
        self.assertEqual(self.controller.snapshot(), before)
        self.assertFalse((self.home / admission.PENDING).exists())

    def test_apply_preserves_other_projects_and_old_holds(self):
        lock_inode = (self.home / admission.LOCK).stat().st_ino
        result = self.apply()
        self.assertEqual(result['manifest_entries'], 6)
        self.controller.check(self.baseline)
        after = self.controller.snapshot()
        for key, fingerprint in self.baseline['protected'].items():
            self.assertEqual(after['protected'][key], fingerprint)
        for name, fingerprint in self.baseline['holds'].items():
            self.assertEqual(after['holds'][name], fingerprint)
        self.assertEqual((self.home / admission.LOCK).stat().st_ino, lock_inode)
        self.assertTrue((self.home / admission.PENDING / 'complete.json').exists())
        self.assertEqual((self.home / admission.MANIFEST).stat().st_mode & 0o777, 0o600)

    def test_all_holds_precede_any_yaml_and_manifest_is_last(self):
        original_create = self.controller.create
        events = []
        def observe(relative, data):
            events.append(relative)
            if relative.startswith(admission.CONFIG + '/dags/'):
                for name in admission.definitions():
                    self.assertTrue((self.home / admission.HOLDS / (name.removesuffix('.yaml') + '.suspend')).is_file())
                self.assertEqual(self.controller.read(admission.MANIFEST)[0],
                                 admission.encoded(self.baseline['manifest']))
            return original_create(relative, data)
        with mock.patch.object(self.controller, 'create', side_effect=observe):
            self.apply()
        last_hold = max(events.index(admission.HOLDS + '/' + name.removesuffix('.yaml') + '.suspend')
                        for name in admission.definitions())
        first_yaml = min(events.index(admission.CONFIG + '/dags/' + name) for name in admission.definitions())
        self.assertLess(last_hold, first_yaml)

    def test_reviewed_baseline_and_exact_bundle(self):
        paths = self.bundle()
        baseline, digest = self.controller.reviewed(*paths)
        self.assertEqual(baseline, self.baseline)
        self.assertEqual(digest, self.baseline_sha)
        self.write('review/bundle/extra.yaml', b'not admitted')
        with self.assertRaisesRegex(admission.Refused, 'bundle file set'):
            self.controller.reviewed(*paths)

    def test_modified_reviewed_baseline_rejected_before_mutation(self):
        paths = self.bundle()
        self.write(paths[0], admission.encoded(dict(self.baseline, schema=2)))
        with self.assertRaisesRegex(admission.Refused, 'SHA256 differs'):
            self.controller.reviewed(*paths)
        self.assertFalse((self.home / admission.PENDING).exists())

    def test_modified_fixed_bundle_rejected(self):
        paths = self.bundle()
        self.write('review/bundle/robotgo-ovh-watch.yaml', b'shell: bash\n')
        with self.assertRaisesRegex(admission.Refused, 'fixed definition'):
            self.controller.reviewed(*paths)

    def test_stale_baseline_rejected_after_concurrent_project_admission(self):
        self.files['dags/concurrent.yaml'] = admission.sha(b'concurrent')
        self.write(admission.CONFIG + '/dags/concurrent.yaml', b'concurrent')
        self.write(admission.MANIFEST, admission.encoded({'files': self.files}))
        with self.assertRaisesRegex(admission.Refused, 'stale baseline'):
            self.apply()
        self.assertFalse((self.home / admission.PENDING).exists())
        self.assertEqual(self.controller.read(admission.CONFIG + '/dags/concurrent.yaml')[0], b'concurrent')

    def test_stale_hold_or_helper_rejected(self):
        for relative in (admission.PROTECTED[2], admission.HOLDS + '/photo-roller-local-ci.suspend'):
            with self.subTest(relative=relative):
                old, _ = self.controller.read(relative)
                self.write(relative, b'changed')
                with self.assertRaises(admission.Refused):
                    self.apply()
                self.write(relative, old)

    def test_missing_retained_hold_refused(self):
        (self.home / admission.HOLDS / 'position-tracking-renovate.suspend').unlink()
        with self.assertRaisesRegex(admission.Refused, 'retained hold missing'):
            self.apply()

    def test_lock_contention_refused_without_changes(self):
        with (self.home / admission.LOCK).open('rb') as other:
            fcntl.flock(other, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(admission.Refused, 'lock busy'):
                self.apply()
        self.assertEqual(self.controller.snapshot(), self.baseline)

    def test_missing_lock_not_recreated(self):
        (self.home / admission.LOCK).unlink()
        with self.assertRaises(FileNotFoundError):
            self.apply()
        self.assertFalse((self.home / admission.LOCK).exists())

    def test_unsafe_private_paths(self):
        for mode in (0o644, 0o666):
            with self.subTest(mode=mode):
                path = self.home / admission.PROTECTED[2]
                path.chmod(mode)
                with self.assertRaisesRegex(admission.Refused, 'unsafe private'):
                    self.apply()
                path.chmod(0o600)
        with self.assertRaisesRegex(admission.Refused, 'outside private'):
            self.controller.read('../outside')
        with self.assertRaisesRegex(admission.Refused, 'outside private'):
            self.controller.read('/etc/passwd')
        (self.home / '.config').chmod(0o755)
        with self.assertRaisesRegex(admission.Refused, 'unsafe private'):
            self.apply()

    def test_symlink_and_hardlink_and_fifo_refused(self):
        path = self.home / admission.PROTECTED[2]
        original = path.read_bytes()
        target = self.write('safe-target', original)
        path.unlink()
        path.symlink_to(target)
        with self.assertRaises(admission.Refused):
            self.apply()
        path.unlink()
        os.link(target, path)
        with self.assertRaises(admission.Refused):
            self.apply()
        path.unlink()
        os.mkfifo(path, 0o600)
        with self.assertRaises(admission.Refused):
            self.apply()

    def test_symlink_directory_and_pending_refused(self):
        outside = self.home / 'other-dags'
        (self.home / admission.CONFIG / 'dags').rename(outside)
        (self.home / admission.CONFIG / 'dags').symlink_to(outside, target_is_directory=True)
        with self.assertRaises(admission.Refused):
            self.apply()
        (self.home / admission.CONFIG / 'dags').unlink()
        outside.rename(self.home / admission.CONFIG / 'dags')
        (self.home / admission.PENDING).symlink_to(self.home / 'nonexistent')
        with self.assertRaisesRegex(admission.Refused, 'pending journal'):
            self.apply()

    def test_unexpected_dag_not_in_manifest_refused(self):
        self.write(admission.CONFIG + '/dags/injected.yaml', b'not reviewed')
        with self.assertRaisesRegex(admission.Refused, 'actual DAG set'):
            self.apply()

    def test_partial_yaml_write_keeps_holds_old_manifest_and_journal(self):
        original = self.controller.create
        def fail(relative, content):
            if relative.startswith(admission.CONFIG + '/dags/'):
                original(relative, content[:12])
                raise OSError('simulated interrupted write')
            return original(relative, content)
        with mock.patch.object(self.controller, 'create', side_effect=fail):
            with self.assertRaises(OSError):
                self.apply()
        self.assertEqual(self.controller.read(admission.MANIFEST)[0], admission.encoded(self.baseline['manifest']))
        self.assertTrue((self.home / admission.PENDING / 'intent.json').exists())
        for name in admission.definitions():
            self.assertTrue((self.home / admission.HOLDS / (name.removesuffix('.yaml') + '.suspend')).exists())
        with self.assertRaises(admission.Refused):
            self.apply()

    def test_partial_hold_write_never_creates_dag(self):
        original = self.controller.create
        def fail(relative, content):
            if relative.startswith(admission.HOLDS + '/'):
                raise OSError('simulated hold failure')
            return original(relative, content)
        with mock.patch.object(self.controller, 'create', side_effect=fail):
            with self.assertRaises(OSError):
                self.apply()
        self.assertEqual(self.controller.snapshot(), self.baseline)
        self.assertTrue((self.home / admission.PENDING / 'intent.json').exists())
        with self.assertRaisesRegex(admission.Refused, 'pending journal'):
            self.apply()

    def test_manifest_replace_failure_retains_pending_and_all_holds(self):
        original = os.replace
        def fail(source, destination):
            if destination == self.home / admission.MANIFEST:
                raise OSError('simulated failed commit')
            return original(source, destination)
        with mock.patch.object(os, 'replace', side_effect=fail):
            with self.assertRaises(OSError):
                self.apply()
        self.assertEqual(self.controller.read(admission.MANIFEST)[0], admission.encoded(self.baseline['manifest']))
        self.assertTrue((self.home / admission.PENDING / 'extended-manifest.json').exists())
        self.controller.check(self.baseline, manifest_installed=False)

    def test_unexpected_retained_mutation_preserved_without_manifest_commit(self):
        original = self.controller.create
        def interfere(relative, content):
            original(relative, content)
            if relative.startswith(admission.CONFIG + '/dags/'):
                self.write(admission.PROTECTED[2], b'concurrent unexpected change')
        with mock.patch.object(self.controller, 'create', side_effect=interfere):
            with self.assertRaisesRegex(admission.Refused, 'retained snapshot'):
                self.apply()
        self.assertEqual(self.controller.read(admission.PROTECTED[2])[0], b'concurrent unexpected change')
        self.assertEqual(self.controller.read(admission.MANIFEST)[0], admission.encoded(self.baseline['manifest']))
        self.assertTrue((self.home / admission.PENDING / 'intent.json').exists())

    def test_empty_wiki_allowed_but_nonempty_wiki_refused(self):
        (self.home / admission.CONFIG / 'dags/wiki').mkdir(mode=0o700)
        baseline = self.controller.snapshot()
        self.assertTrue(baseline['wiki'])
        self.write(admission.CONFIG + '/dags/wiki/unexpected', b'unexpected')
        with self.assertRaisesRegex(admission.Refused, 'wiki directory is not empty'):
            self.controller.snapshot()

    def test_read_only_released_check_does_not_release_holds(self):
        self.apply()
        with self.assertRaisesRegex(admission.Refused, 'still present'):
            self.controller.check(self.baseline, released=True)
        for name in admission.definitions():
            (self.home / admission.HOLDS / (name.removesuffix('.yaml') + '.suspend')).unlink()
        self.controller.check(self.baseline, released=True)
        with self.assertRaisesRegex(admission.Refused, 'hold missing'):
            self.controller.check(self.baseline)

    def test_duplicate_json_keys_refused(self):
        with self.assertRaisesRegex(admission.Refused, 'invalid private JSON'):
            admission.decode(b'{"files":{},"files":{}}')

    def test_fixed_definitions_are_closed_and_offset(self):
        definitions = admission.definitions()
        self.assertEqual(len(definitions), 4)
        for content in definitions.values():
            for marker in (b'shell: direct\n', b'additionalProperties: false\n', b'enabled: false\n',
                           b'runner: izoone-ovh\n', b'isolation: podman\n', b'queue: photoroller-local\n',
                           b'max_active_steps: 1\n', b'timeout_sec: 3700\n', b'hist_retention_days: 14\n'):
                self.assertIn(marker, content)
        self.assertIn(b'7-59/15 * * * *', definitions['robotgo-ovh-watch.yaml'])
        self.assertIn(b'17 4 * * 1', definitions['robotgo-ovh-security.yaml'])
        self.assertIn(b'27 4 * * 1', definitions['robotgo-ovh-dependencies.yaml'])
        self.assertNotIn(b'--if-changed', definitions['robotgo-ovh-ci.yaml'])


if __name__ == '__main__':
    unittest.main()
