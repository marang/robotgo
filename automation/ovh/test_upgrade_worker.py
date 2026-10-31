"""Offline sealed worker upgrade tests; no service/image/auth operations."""

import fcntl
import importlib.util
import os
from pathlib import Path
import struct
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location('upgrade', Path(__file__).with_name('upgrade-worker.py'))
upgrade = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(upgrade)


def elf(seal):
    data = bytearray(128)
    data[:6] = b'\x7fELF\x02\x01'
    struct.pack_into('<H', data, 18, 62)
    struct.pack_into('<Q', data, 32, 64)
    struct.pack_into('<HH', data, 54, 56, 1)
    struct.pack_into('<I', data, 64, 1)
    return bytes(data) + seal.encode()


class WorkerUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        owner = os.getuid(), os.getgid()
        self.upgrade = upgrade.Upgrade(self.root, owner, owner, owner)
        self.old_image, self.new_image = 'sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
        self.old_bundle, self.old_sha, self.old_files = self.make_bundle('old', self.old_image)
        self.new_bundle, self.new_sha, self.new_files = self.make_bundle('new', self.new_image)
        self.write(upgrade.GLOBAL_LOCK, b'', 0o600)
        self.write(upgrade.RUNNER_LOCK, b'', 0o600)
        for name, data in self.old_files.items():
            self.write(upgrade.LIBEXEC + '/' + name, data, 0o700)
        self.write(upgrade.HOME + '/unrelated-project-marker', b'never change', 0o600)
        self.before = self.tree()
        self.checker = mock.patch.object(self.upgrade, 'verify_image')
        self.image = self.checker.start()
        self.addCleanup(self.checker.stop)

    def write(self, absolute, data, mode):
        path = self.upgrade.path(absolute)
        missing, parent = [], path.parent
        while not parent.exists():
            missing.append(parent)
            parent = parent.parent
        for directory in reversed(missing):
            directory.mkdir(mode=0o700)
        with path.open('wb') as output:
            output.write(data)
        path.chmod(mode)
        return path

    def make_bundle(self, label, image):
        driver = ("IMAGE = '" + image + "'\n# profile " + label +
                  "\nraise RuntimeError('profile source must never be executed by admission')\n").encode()
        launcher = elf(upgrade.sha(driver))
        files = {'robotgo-ci': launcher, 'robotgo-ci.py': driver}
        profile = {'schema_version': 1, 'owner': 'izoone_workers', 'uid': upgrade.UID,
                   'gid': upgrade.GID, 'home': upgrade.HOME, 'image': image, 'go_version': 'go1.26.10',
                   'installed': False, 'source_sha256': {'runner.py': 'c' * 64, 'launcher.go': 'd' * 64},
                   'files': {name: upgrade.sha(data) for name, data in files.items()}}
        raw = upgrade.encoded(profile)
        for name, data in {**files, 'profile.json': raw}.items():
            self.write(upgrade.ROOT + '/' + label + '/' + name, data, 0o600)
        return self.upgrade.path(upgrade.ROOT + '/' + label), upgrade.sha(raw), files

    def run_upgrade(self, apply=True):
        return self.upgrade.upgrade(self.new_bundle, self.new_sha, self.old_bundle, self.old_sha, apply)

    def tree(self):
        return {str(path.relative_to(self.root)): (path.read_bytes(), path.stat().st_ino, path.stat().st_mode)
                for path in self.root.rglob('*') if path.is_file()}

    def test_preview_writes_nothing(self):
        report = self.run_upgrade(False)
        self.assertEqual(report['mode'], 'preview')
        self.assertEqual(report['profile_sha256'], self.new_sha)
        self.assertEqual(report['files'], {name: upgrade.sha(data) for name, data in self.new_files.items()})
        self.assertEqual(self.tree(), self.before)
        self.image.assert_called_once_with(self.new_image)

    def test_upgrade_driver_first_launcher_last_and_only_own_paths_change(self):
        original = os.replace
        publications = []
        def observe(source, destination):
            if destination.parent == self.upgrade.path(upgrade.LIBEXEC):
                publications.append(destination.name)
                if destination.name == 'robotgo-ci':
                    self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/robotgo-ci.py').read_bytes(), self.new_files['robotgo-ci.py'])
                    self.assertEqual(destination.read_bytes(), self.old_files['robotgo-ci'])
            return original(source, destination)
        with mock.patch.object(os, 'replace', side_effect=observe):
            report = self.run_upgrade()
        self.assertEqual(publications, ['robotgo-ci.py', 'robotgo-ci'])
        self.assertEqual(report['mode'], 'apply')
        after = self.tree()
        allowed = {str(self.upgrade.path(upgrade.LIBEXEC + '/' + name).relative_to(self.root)) for name in upgrade.NAMES}
        for name, prior in self.before.items():
            if name not in allowed:
                self.assertEqual(after[name], prior)
        for name, data in self.new_files.items():
            path = self.upgrade.path(upgrade.LIBEXEC + '/' + name)
            self.assertEqual(path.read_bytes(), data)
            self.assertEqual(path.stat().st_mode & 0o777, 0o700)
        pending = self.upgrade.path(upgrade.PENDING)
        self.assertTrue((pending / 'complete.json').exists())
        self.assertEqual((pending / 'prior-robotgo-ci.py').read_bytes(), self.old_files['robotgo-ci.py'])

    def test_changed_baseline_or_installed_code_refused_before_write(self):
        with self.assertRaisesRegex(upgrade.Refused, 'profile digest differs'):
            self.upgrade.upgrade(self.new_bundle, self.new_sha, self.old_bundle, '0' * 64, True)
        self.write(upgrade.LIBEXEC + '/robotgo-ci.py', b'unexpected installed code', 0o700)
        with self.assertRaisesRegex(upgrade.Refused, 'reviewed baseline'):
            self.run_upgrade()
        self.assertFalse(self.upgrade.path(upgrade.PENDING).exists())

    def test_modified_bundle_code_and_profile_refused(self):
        self.write(upgrade.ROOT + '/new/robotgo-ci.py', b'tampered', 0o600)
        with self.assertRaisesRegex(upgrade.Refused, 'code hash differs'):
            self.run_upgrade()
        self.write(upgrade.ROOT + '/new/robotgo-ci.py', self.new_files['robotgo-ci.py'], 0o600)
        self.write(upgrade.ROOT + '/new/profile.json', b'{}', 0o600)
        with self.assertRaisesRegex(upgrade.Refused, 'profile digest differs'):
            self.run_upgrade()

    def test_global_and_runner_lock_contention(self):
        for absolute in (upgrade.GLOBAL_LOCK, upgrade.RUNNER_LOCK):
            with self.subTest(lock=absolute):
                with self.upgrade.path(absolute).open('rb') as active:
                    fcntl.flock(active, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    with self.assertRaisesRegex(upgrade.Refused, 'lock busy'):
                        self.run_upgrade()
                self.assertEqual(self.tree(), self.before)

    def test_missing_lock_never_recreated(self):
        self.upgrade.path(upgrade.RUNNER_LOCK).unlink()
        with self.assertRaises(FileNotFoundError):
            self.run_upgrade()
        self.assertFalse(self.upgrade.path(upgrade.RUNNER_LOCK).exists())

    def test_private_inputs_and_symlinked_files_and_ancestors_refused(self):
        path = self.new_bundle / 'robotgo-ci.py'
        path.chmod(0o644)
        with self.assertRaises(upgrade.Refused):
            self.run_upgrade()
        path.chmod(0o600)
        self.new_bundle.chmod(0o755)
        with self.assertRaises(upgrade.Refused):
            self.run_upgrade()
        self.new_bundle.chmod(0o700)
        path.unlink()
        path.symlink_to(self.old_bundle / 'robotgo-ci.py')
        with self.assertRaises(upgrade.Refused):
            self.run_upgrade()

    def test_mutable_image_profile_and_dynamic_elf_refused(self):
        raw = (self.new_bundle / 'profile.json').read_bytes()
        profile = upgrade.decode(raw)
        profile['image'] = 'docker.io/example:latest'
        with self.assertRaisesRegex(upgrade.Refused, 'immutable sealed profile'):
            upgrade.verify_profile(upgrade.encoded(profile), self.new_files)
        data = bytearray(self.new_files['robotgo-ci'])
        struct.pack_into('<I', data, 64, 3)
        with self.assertRaisesRegex(upgrade.Refused, 'dynamic launcher'):
            upgrade.static_elf(bytes(data))

    def test_pre_normalization_profile_identity_is_not_silently_admitted(self):
        profile = upgrade.decode((self.new_bundle / 'profile.json').read_bytes())
        for key in ('owner', 'home'):
            with self.subTest(field=key):
                legacy = dict(profile)
                legacy[key] += '_vps16776'
                with self.assertRaisesRegex(upgrade.Refused, 'fixed immutable sealed profile'):
                    upgrade.verify_profile(upgrade.encoded(legacy), self.new_files)

    def test_image_inspection_accepts_raw_or_prefixed_exact_id(self):
        self.checker.stop()
        for image in (self.new_image, self.new_image.removeprefix('sha256:')):
            with self.subTest(stdout=image), mock.patch.object(upgrade.subprocess, 'run',
                    return_value=upgrade.subprocess.CompletedProcess([], 0, (image + '\n').encode(), b'')) as run:
                self.upgrade.verify_image(self.new_image)
                self.assertEqual(run.call_args.kwargs['user'], upgrade.UID)
                self.assertEqual(run.call_args.kwargs['group'], upgrade.GID)
                self.assertEqual(run.call_args.kwargs['extra_groups'], [])
        with mock.patch.object(upgrade.subprocess, 'run',
                               return_value=upgrade.subprocess.CompletedProcess([], 0, b'c' * 64, b'')):
            with self.assertRaisesRegex(upgrade.Refused, 'image unavailable'):
                self.upgrade.verify_image(self.new_image)

    def test_partial_replace_retains_new_driver_old_launcher_and_journal(self):
        original = os.replace
        def fail(source, destination):
            if destination == self.upgrade.path(upgrade.LIBEXEC + '/robotgo-ci'):
                raise OSError('simulated second publication failure')
            return original(source, destination)
        with mock.patch.object(os, 'replace', side_effect=fail):
            with self.assertRaises(OSError):
                self.run_upgrade()
        self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/robotgo-ci.py').read_bytes(), self.new_files['robotgo-ci.py'])
        self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/robotgo-ci').read_bytes(), self.old_files['robotgo-ci'])
        self.assertNotIn(upgrade.sha(self.new_files['robotgo-ci.py']).encode(), self.old_files['robotgo-ci'])
        pending = self.upgrade.path(upgrade.PENDING)
        self.assertTrue((pending / 'intent.json').exists())
        self.assertFalse((pending / 'complete.json').exists())
        with self.assertRaisesRegex(upgrade.Refused, 'pending worker upgrade'):
            self.run_upgrade()

    def test_partial_staging_preserves_old_installed_code(self):
        original = self.upgrade.create
        def fail(path, data, owner, mode):
            if path.name == '.robotgo-next-driver':
                original(path, data[:3], owner, mode)
                raise OSError('simulated interrupted staging')
            return original(path, data, owner, mode)
        with mock.patch.object(self.upgrade, 'create', side_effect=fail):
            with self.assertRaises(OSError):
                self.run_upgrade()
        for name, data in self.old_files.items():
            self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/' + name).read_bytes(), data)
        self.assertTrue((self.upgrade.path(upgrade.PENDING) / 'intent.json').exists())

    def test_concurrent_installed_mutation_preserved_without_replacement(self):
        original = self.upgrade.create
        def interfere(path, data, owner, mode):
            result = original(path, data, owner, mode)
            if path.name == '.robotgo-next-launcher':
                self.write(upgrade.LIBEXEC + '/robotgo-ci.py', b'concurrent change', 0o700)
            return result
        with mock.patch.object(self.upgrade, 'create', side_effect=interfere):
            with self.assertRaisesRegex(upgrade.Refused, 'installed worker changed'):
                self.run_upgrade()
        self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/robotgo-ci.py').read_bytes(), b'concurrent change')
        self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/robotgo-ci').read_bytes(), self.old_files['robotgo-ci'])

    def test_existing_pending_and_prepared_files_are_never_adopted(self):
        staged = self.write(upgrade.LIBEXEC + '/.robotgo-next-driver', b'unrecognized staging', 0o700)
        with self.assertRaisesRegex(upgrade.Refused, 'prepared upgrade file already exists'):
            self.run_upgrade()
        self.assertEqual(staged.read_bytes(), b'unrecognized staging')
        staged.unlink()
        pending = self.upgrade.path(upgrade.PENDING)
        pending.mkdir(mode=0o700)
        with self.assertRaisesRegex(upgrade.Refused, 'pending worker upgrade exists'):
            self.run_upgrade()
        self.assertTrue(pending.is_dir())

    def test_reviewed_bundle_mutation_during_staging_is_preserved(self):
        original = self.upgrade.create
        def interfere(path, data, owner, mode):
            result = original(path, data, owner, mode)
            if path.name == '.robotgo-next-launcher':
                self.write(upgrade.ROOT + '/new/robotgo-ci.py', b'unexpected reviewed source mutation', 0o600)
            return result
        with mock.patch.object(self.upgrade, 'create', side_effect=interfere):
            with self.assertRaisesRegex(upgrade.Refused, 'code hash differs'):
                self.run_upgrade()
        for name, data in self.old_files.items():
            self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/' + name).read_bytes(), data)
        self.assertEqual((self.new_bundle / 'robotgo-ci.py').read_bytes(), b'unexpected reviewed source mutation')

    def test_bundle_hardlinks_and_installed_symlinks_refused(self):
        path = self.new_bundle / 'robotgo-ci.py'
        os.link(path, self.new_bundle.parent / 'additional-link')
        with self.assertRaises(upgrade.Refused):
            self.run_upgrade()
        (self.new_bundle.parent / 'additional-link').unlink()
        target = self.upgrade.path(upgrade.LIBEXEC + '/robotgo-ci.py')
        target.unlink()
        target.symlink_to(self.old_bundle / 'robotgo-ci.py')
        with self.assertRaises(upgrade.Refused):
            self.run_upgrade()

    def test_image_seal_and_launcher_driver_seal_required(self):
        profile = upgrade.decode((self.new_bundle / 'profile.json').read_bytes())
        files = dict(self.new_files)
        files['robotgo-ci.py'] = files['robotgo-ci.py'].replace(self.new_image.encode(), self.old_image.encode())
        profile['files']['robotgo-ci.py'] = upgrade.sha(files['robotgo-ci.py'])
        files['robotgo-ci'] = elf(profile['files']['robotgo-ci.py'])
        profile['files']['robotgo-ci'] = upgrade.sha(files['robotgo-ci'])
        with self.assertRaisesRegex(upgrade.Refused, 'driver image seal differs'):
            upgrade.verify_profile(upgrade.encoded(profile), files)
        profile = upgrade.decode((self.new_bundle / 'profile.json').read_bytes())
        files = dict(self.new_files)
        files['robotgo-ci'] = elf('0' * 64)
        profile['files']['robotgo-ci'] = upgrade.sha(files['robotgo-ci'])
        with self.assertRaisesRegex(upgrade.Refused, 'driver seal'):
            upgrade.verify_profile(upgrade.encoded(profile), files)

    def successor(self, apply=True):
        bundle, digest, _ = self.third
        return self.upgrade.upgrade(bundle, digest, self.new_bundle, self.new_sha, apply,
                                    self.upgrade.path(upgrade.PENDING))

    def prepare_successor(self):
        self.run_upgrade()
        self.third = self.make_bundle('third', 'sha256:' + 'e' * 64)
        previous = self.upgrade.path(upgrade.PENDING)
        return {path.name: (path.read_bytes(), path.stat().st_ino, path.stat().st_mode)
                for path in previous.iterdir()}, previous.stat().st_ino

    def test_explicit_successor_preserves_completed_old_journal_and_derives_own_namespace(self):
        before, directory_inode = self.prepare_successor()
        previous = self.upgrade.path(upgrade.PENDING)
        tree_before = self.tree()
        report = self.successor(False)
        self.assertEqual(self.tree(), tree_before)
        expected = self.upgrade.path(upgrade.ROOT + '/robotgo-worker-upgrade-' + self.third[1] + '.pending')
        self.assertEqual(report['journal'], str(expected))
        self.assertFalse(expected.exists())
        report = self.successor()
        self.assertEqual(report['previous_journal'], str(previous))
        self.assertTrue((expected / 'complete.json').is_file())
        self.assertEqual(previous.stat().st_ino, directory_inode)
        for name, record in before.items():
            path = previous / name
            self.assertEqual((path.read_bytes(), path.stat().st_ino, path.stat().st_mode), record)
        for name, data in self.third[2].items():
            self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/' + name).read_bytes(), data)

    def test_successor_refuses_incomplete_or_mismatched_completed_receipt(self):
        self.prepare_successor()
        previous = self.upgrade.path(upgrade.PENDING)
        progress = (previous / 'progress.json').read_bytes()
        (previous / 'progress.json').write_bytes(upgrade.encoded({'phase': 'driver-published'}))
        with self.assertRaisesRegex(upgrade.Refused, 'previous completed receipt'):
            self.successor()
        (previous / 'progress.json').write_bytes(progress)
        complete = upgrade.decode((previous / 'complete.json').read_bytes())
        complete['profile_sha256'] = '0' * 64
        (previous / 'complete.json').write_bytes(upgrade.encoded(complete))
        with self.assertRaisesRegex(upgrade.Refused, 'previous completed receipt'):
            self.successor()
        self.assertFalse(self.upgrade.path(upgrade.ROOT + '/robotgo-worker-upgrade-' + self.third[1] + '.pending').exists())

    def test_successor_missing_complete_or_foreign_path_does_not_bypass_pending(self):
        self.prepare_successor()
        previous = self.upgrade.path(upgrade.PENDING)
        (previous / 'complete.json').unlink()
        with self.assertRaisesRegex(upgrade.Refused, 'previous journal is incomplete'):
            self.successor()
        with self.assertRaisesRegex(upgrade.Refused, 'exact own baseline-profile namespace'):
            self.upgrade.upgrade(self.third[0], self.third[1], self.new_bundle, self.new_sha, True,
                                  self.upgrade.path(upgrade.ROOT + '/unrelated-project.pending'))
        with self.assertRaisesRegex(upgrade.Refused, 'pending worker upgrade exists'):
            self.upgrade.upgrade(self.third[0], self.third[1], self.new_bundle, self.new_sha, True)

    def test_successor_existing_namespace_refused_without_touching_old_receipt(self):
        before, _ = self.prepare_successor()
        journal = self.upgrade.path(upgrade.ROOT + '/robotgo-worker-upgrade-' + self.third[1] + '.pending')
        journal.mkdir(mode=0o700)
        with self.assertRaisesRegex(upgrade.Refused, 'pending worker upgrade exists'):
            self.successor()
        for name, record in before.items():
            path = self.upgrade.path(upgrade.PENDING) / name
            self.assertEqual((path.read_bytes(), path.stat().st_ino, path.stat().st_mode), record)

    def test_predecessor_mutation_during_successor_is_preserved_and_stops_publication(self):
        self.prepare_successor()
        original = self.upgrade.create
        def interfere(path, data, owner, mode):
            result = original(path, data, owner, mode)
            if path.name == '.robotgo-next-launcher':
                old = self.upgrade.path(upgrade.PENDING) / 'intent.json'
                old.write_bytes(b'unexpected predecessor mutation')
            return result
        with mock.patch.object(self.upgrade, 'create', side_effect=interfere):
            with self.assertRaisesRegex(upgrade.Refused, 'previous journal changed'):
                self.successor()
        self.assertEqual((self.upgrade.path(upgrade.PENDING) / 'intent.json').read_bytes(), b'unexpected predecessor mutation')
        for name, data in self.new_files.items():
            self.assertEqual(self.upgrade.path(upgrade.LIBEXEC + '/' + name).read_bytes(), data)

    def test_successor_of_completed_successor_and_partial_successor_remain_bounded(self):
        self.prepare_successor()
        self.successor()
        previous = self.upgrade.path(upgrade.ROOT + '/robotgo-worker-upgrade-' + self.third[1] + '.pending')
        fourth = self.make_bundle('fourth', 'sha256:' + 'f' * 64)
        original = os.replace
        def fail(source, destination):
            if destination == self.upgrade.path(upgrade.LIBEXEC + '/robotgo-ci'):
                raise OSError('successor interrupted after driver publication')
            return original(source, destination)
        prior = {path.name: (path.read_bytes(), path.stat().st_ino) for path in previous.iterdir()}
        with mock.patch.object(os, 'replace', side_effect=fail):
            with self.assertRaises(OSError):
                self.upgrade.upgrade(fourth[0], fourth[1], self.third[0], self.third[1], True, previous)
        for name, record in prior.items():
            path = previous / name
            self.assertEqual((path.read_bytes(), path.stat().st_ino), record)
        fourth_journal = self.upgrade.path(upgrade.ROOT + '/robotgo-worker-upgrade-' + fourth[1] + '.pending')
        self.assertTrue((fourth_journal / 'intent.json').is_file())
        self.assertFalse((fourth_journal / 'complete.json').exists())
        with self.assertRaisesRegex(upgrade.Refused, 'pending worker upgrade exists'):
            self.upgrade.upgrade(fourth[0], fourth[1], self.third[0], self.third[1], True, previous)


if __name__ == '__main__':
    unittest.main()
