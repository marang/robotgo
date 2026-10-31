"""Offline release tests with unrelated concurrent-project definitions/holds."""

from datetime import datetime, timezone
import fcntl
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location('release', Path(__file__).with_name('release-controller.py'))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)


def proof_fixture():
    instant = datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
    proof = {'schema_version': 1, 'repository': release.REPOSITORY, 'commit': '1' * 40,
             'profile_sha256': '2' * 64, 'image': 'sha256:' + '3' * 64, 'driver_sha256': '4' * 64,
             'verified_at': instant, 'runs': {},
             'success_marker': {'source': release.WORKER_SOURCE, 'path': release.MARKER_PATH,
                                'commit': '1' * 40, 'pipeline': '4' * 64}}
    for index, (task, dag) in enumerate(release.TASK_DAGS.items()):
        proof['runs'][task] = {'dag': dag, 'dag_run_id': dag + '-proof-' + str(index),
                              'dag_status': 'succeeded', 'dag_source': release.DAGU_SOURCE,
                              'worker_id': 'izoone-ovh-01', 'worker_run_id': format(index + 1, '032x'),
                              'worker_status': 'unchanged' if task == 'watch' else 'passed',
                              'worker_source': release.WORKER_SOURCE, 'commit': proof['commit'],
                              'profile_sha256': proof['profile_sha256'], 'image': proof['image'],
                              'completed_at': instant}
    return proof


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name) / 'controller'
        self.home.mkdir(mode=0o700)
        self.bundle = self.home / 'review/bundle'
        source = Path(__file__).with_name('admit-controller.py').read_bytes()
        self.write('review/bundle/admit-controller.py', source, 0o700)
        self.a = release.load_admission(self.bundle, release.ADMISSION_SHA256, self.home, os.getuid(), os.getgid())
        self.c = self.a.Controller(self.home, os.getuid(), os.getgid())
        self.engine = release.Release(self.a, self.c)
        self.a.CONFIG_SHA256 = release.sha(b'policy fixture')
        self.a.HARDENER_SHA256 = release.sha(b'canonical empty-history guard fixture')
        self.write(self.a.CONFIG + '/config.yaml', b'policy fixture')
        for index, path in enumerate(self.a.PROTECTED[1:]):
            self.write(path, b'canonical empty-history guard fixture' if index == 0 else b'policy helper')
        self.write(self.a.LOCK, b'')
        files = {'base.yaml': release.sha(b'base')}
        self.write(self.a.CONFIG + '/base.yaml', b'base')
        for index in range(5):
            name, data = 'concurrent-sway-' + str(index) + '.yaml', b'concurrent fixed definition'
            self.write(self.a.CONFIG + '/dags/' + name, data)
            files['dags/' + name] = release.sha(data)
        for name, data in self.a.definitions().items():
            self.write('review/bundle/' + name, data)
            self.write(self.a.CONFIG + '/dags/' + name, data)
            files['dags/' + name] = release.sha(data)
        self.write(self.a.MANIFEST, self.a.encoded({'files': files}))
        for name in self.a.OLD_HOLDS | {'.suspend.lock', 'concurrent-sway.suspend'}:
            self.write(self.a.HOLDS + '/' + name, b'')
        self.flags = [name.removesuffix('.yaml') + '.suspend' for name in self.a.definitions()]
        for flag in self.flags:
            self.write(self.a.HOLDS + '/' + flag, b'')
        self.write(self.a.PENDING + '/complete.json', b'old admission journal retained')
        self.write('private-auth-marker', b'fixture never read by release')
        self.snapshot_path, self.proof_path = self.home / 'review/snapshot.json', self.home / 'review/proof.json'
        self.proof = proof_fixture()
        self.baseline = self.c.snapshot()
        self.write('review/snapshot.json', self.a.encoded(self.baseline))
        self.write('review/proof.json', release.encoded(self.proof))
        self.snapshot_sha = release.sha(self.snapshot_path.read_bytes())
        self.proof_sha = release.sha(self.proof_path.read_bytes())

    def write(self, relative, data, mode=0o600):
        path = self.home / relative
        absent, parent = [], path.parent
        while not parent.exists():
            absent.append(parent)
            parent = parent.parent
        for directory in reversed(absent):
            directory.mkdir(mode=0o700)
        with path.open('wb') as target:
            target.write(data)
        path.chmod(mode)
        return path

    def run_release(self, apply=True):
        return self.engine.release(self.bundle, self.snapshot_path, self.snapshot_sha,
                                   self.proof_path, self.proof_sha, apply)

    def tree(self):
        return {str(path.relative_to(self.home)): (path.read_bytes(), path.stat().st_ino, path.stat().st_mode)
                for path in self.home.rglob('*') if path.is_file()}

    def test_preview_has_no_writes(self):
        before = self.tree()
        report = self.run_release(False)
        self.assertEqual(report['mode'], 'preview')
        self.assertEqual(set(report['release_only']), set(self.flags))
        self.assertEqual(self.tree(), before)

    def test_only_exact_four_holds_removed_all_other_bytes_and_inodes_stable(self):
        before = self.tree()
        report = self.run_release()
        self.assertEqual(report['mode'], 'apply')
        expected = self.a.decode(self.a.encoded(self.baseline))
        for flag in self.flags:
            expected['holds'].pop(flag)
        self.assertEqual(self.c.snapshot(), expected)
        after = self.tree()
        removed = {self.a.HOLDS + '/' + flag for flag in self.flags}
        self.assertEqual(set(before) - set(after), removed)
        for path, record in before.items():
            if path not in removed:
                self.assertEqual(after[path], record)
        new_paths = set(after) - set(before)
        self.assertTrue(new_paths)
        self.assertTrue(all(path.startswith(release.PENDING + '/') for path in new_paths))
        self.assertTrue((self.home / release.PENDING / 'complete.json').is_file())

    def test_snapshot_and_proof_digest_tampering_refused_before_writes(self):
        for attribute in ('snapshot_sha', 'proof_sha'):
            old = getattr(self, attribute)
            setattr(self, attribute, '0' * 64)
            with self.assertRaisesRegex(release.Refused, 'digest/mode differs'):
                self.run_release()
            setattr(self, attribute, old)
        self.assertFalse((self.home / release.PENDING).exists())

    def test_fresh_concurrent_change_requires_new_snapshot(self):
        self.write(self.a.HOLDS + '/new-project.suspend', b'')
        with self.assertRaisesRegex(release.Refused, 'stale release snapshot'):
            self.run_release()
        self.assertTrue((self.home / self.a.HOLDS / 'new-project.suspend').exists())
        self.assertFalse((self.home / release.PENDING).exists())

    def test_lock_contention_no_change(self):
        before = self.tree()
        with (self.home / self.a.LOCK).open('rb') as active:
            fcntl.flock(active, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaises(self.a.Refused):
                self.run_release()
        self.assertEqual(self.tree(), before)

    def test_private_inputs_hold_symlink_and_hardlink_refused(self):
        self.proof_path.chmod(0o644)
        with self.assertRaises(self.a.Refused):
            self.run_release()
        self.proof_path.chmod(0o600)
        flag = self.home / self.a.HOLDS / self.flags[0]
        flag.unlink()
        flag.symlink_to(self.home / 'unrelated')
        with self.assertRaises((self.a.Refused, FileNotFoundError)):
            self.run_release()
        flag.unlink()
        target = self.write('empty-target', b'')
        os.link(target, flag)
        with self.assertRaises(self.a.Refused):
            self.run_release()

    def test_module_digest_verified_before_execution(self):
        self.write('review/bundle/admit-controller.py', b'raise RuntimeError("must not execute")\n', 0o700)
        with self.assertRaisesRegex(release.Refused, 'module digest differs'):
            release.load_admission(self.bundle, release.ADMISSION_SHA256, self.home, os.getuid(), os.getgid())
        with self.assertRaisesRegex(release.Refused, 'fixed admission digest required'):
            release.load_admission(self.bundle, '0' * 64, self.home, os.getuid(), os.getgid())

    def test_partial_unlink_retains_journal_no_rollback_or_blind_retry(self):
        original, deleted = os.unlink, []
        def interrupt(path, *args, **kwargs):
            if Path(path).name in self.flags:
                if deleted:
                    raise OSError('interrupted release')
                deleted.append(Path(path).name)
            return original(path, *args, **kwargs)
        with mock.patch.object(os, 'unlink', side_effect=interrupt):
            with self.assertRaises(OSError):
                self.run_release()
        self.assertEqual(len(deleted), 1)
        self.assertFalse((self.home / self.a.HOLDS / deleted[0]).exists())
        self.assertTrue((self.home / release.PENDING / 'intent.json').exists())
        self.assertFalse((self.home / release.PENDING / 'complete.json').exists())
        with self.assertRaises(release.Refused):
            self.run_release()

    def test_changed_reviewed_inputs_or_controller_during_release_preserved(self):
        original = self.c.create
        def interfere(relative, data):
            original(relative, data)
            if relative == release.PENDING + '/progress-0.json':
                self.write(self.a.PROTECTED[2], b'unexpected concurrent helper edit')
        with mock.patch.object(self.c, 'create', side_effect=interfere):
            with self.assertRaisesRegex(release.Refused, 'controller changed during release'):
                self.run_release()
        self.assertEqual((self.home / self.a.PROTECTED[2]).read_bytes(), b'unexpected concurrent helper edit')
        self.assertTrue(all((self.home / self.a.HOLDS / flag).exists() for flag in self.flags))

    def test_proof_rejects_missing_failed_foreign_inconsistent_or_duplicate_runs(self):
        for change in ('missing', 'failed', 'foreign', 'commit', 'profile', 'image', 'duplicate', 'marker', 'watch'):
            with self.subTest(change=change):
                proof = proof_fixture()
                if change == 'missing':
                    proof['runs'].pop('dependencies')
                elif change == 'failed':
                    proof['runs']['ci']['dag_status'] = 'running'
                elif change == 'foreign':
                    proof['runs']['ci']['dag'] = 'other-project'
                elif change in ('commit', 'image'):
                    proof['runs']['security'][change] = 'unexpected'
                elif change == 'profile':
                    proof['runs']['security']['profile_sha256'] = '0' * 64
                elif change == 'duplicate':
                    proof['runs']['watch']['dag_run_id'] = proof['runs']['ci']['dag_run_id']
                elif change == 'marker':
                    proof['success_marker']['pipeline'] = '0' * 64
                elif change == 'watch':
                    proof['runs']['watch']['worker_status'] = 'passed'
                with self.assertRaises(release.Refused):
                    release.validate_proof(proof)

    def test_fresh_snapshot_cannot_admit_modified_own_definition(self):
        name = 'robotgo-ovh-ci.yaml'
        self.write(self.a.CONFIG + '/dags/' + name, b'modified own definition')
        manifest = self.baseline['manifest']
        manifest['files']['dags/' + name] = release.sha(b'modified own definition')
        self.write(self.a.MANIFEST, self.a.encoded(manifest))
        self.write('review/snapshot.json', self.a.encoded(self.c.snapshot()))
        self.snapshot_sha = release.sha(self.snapshot_path.read_bytes())
        with self.assertRaisesRegex(release.Refused, 'exact private RobotGo definition'):
            self.run_release()
        self.assertFalse((self.home / release.PENDING).exists())

    def test_changed_proof_after_journal_prevents_any_unlink(self):
        original = self.c.create
        def interfere(relative, data):
            original(relative, data)
            if relative == release.PENDING + '/progress-0.json':
                self.write('review/proof.json', b'changed proof bytes')
        with mock.patch.object(self.c, 'create', side_effect=interfere):
            with self.assertRaisesRegex(release.Refused, 'release inputs changed'):
                self.run_release()
        self.assertTrue(all((self.home / self.a.HOLDS / flag).exists() for flag in self.flags))

    def test_existing_or_symlinked_release_journal_never_adopted(self):
        journal = self.home / release.PENDING
        journal.symlink_to(self.home / self.a.PENDING, target_is_directory=True)
        with self.assertRaisesRegex(release.Refused, 'pending release exists'):
            self.run_release()
        self.assertEqual((self.home / self.a.PENDING / 'complete.json').read_bytes(), b'old admission journal retained')

    def test_proof_qualification_and_completion_timestamps_checked(self):
        for field, bad in (('dag_source', 'https://elsewhere.invalid'),
                           ('worker_source', 'unverified'), ('completed_at', '2999-01-01T00:00:00Z')):
            proof = proof_fixture()
            proof['runs']['ci'][field] = bad
            with self.assertRaises(release.Refused):
                release.validate_proof(proof)


if __name__ == '__main__':
    unittest.main()
