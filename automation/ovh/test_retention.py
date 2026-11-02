"""Offline dedicated RobotGo retention admission tests; never prune runs."""

import ast
import fcntl
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location('retention', Path(__file__).with_name('extend-retention.py'))
retention = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(retention)


def pruner(roots=('photo-roller-remote-runner', 'position-tracking-dependencies')):
    return ('''#!/usr/bin/env python3
from pathlib import Path
WORKER_UID = 7051508
WORKER_GID = 1004
WORKER_HOME = Path('/home/izoone_workers')
TTL_SECONDS = 14 * 24 * 60 * 60
ROOTS = ''' + repr(roots) + '''
MAX_RUNS = 100
def unchanged_retention_logic():
    raise RuntimeError('fixture must never run pruning')
''').encode()


def unit(roots=('photo-roller-remote-runner', 'position-tracking-dependencies')):
    return ('''[Unit]
Description=Expire private ephemeral CI runs after 14 days
ConditionUser=7051508

[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /usr/local/libexec/izoone-ci/prune-state.py --apply
Environment=HOME=/home/izoone_workers
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=''' + ' '.join(retention.WORKER_HOME + '/.local/state/' + root for root in roots) + '''
CPUQuota=10%
MemoryMax=128M
TasksMax=16
TimeoutStartSec=15min
StandardOutput=journal
StandardError=journal
''').encode()


class DerivationTests(unittest.TestCase):
    def test_exact_robotgo_only_ast_scope_and_independent_hourly_budget(self):
        old_roots = ('photo-roller-remote-runner', 'position-tracking-dependencies', 'concurrent-project')
        before, old_unit = pruner(old_roots), unit(old_roots)
        roots, after, new_unit, new_timer = retention.derive(before, old_unit)
        self.assertEqual(roots, old_roots)
        old_ast, new_ast = ast.parse(before), ast.parse(after)
        old_value = retention.assigned(old_ast, 'ROOTS')
        new_value = retention.assigned(new_ast, 'ROOTS')
        self.assertEqual(ast.literal_eval(new_value), ('robotgo-ovh',))
        self.assertEqual(ast.literal_eval(retention.assigned(old_ast, 'MAX_RUNS')), 100)
        self.assertEqual(ast.literal_eval(retention.assigned(new_ast, 'MAX_RUNS')), 100)
        new_value.elts = old_value.elts
        self.assertEqual(ast.dump(old_ast), ast.dump(new_ast))
        self.assertEqual(after.replace(repr(('robotgo-ovh',)).encode(), repr(old_roots).encode()), before)
        self.assertEqual(new_unit.count(retention.RUNNER_STATE.encode()), 1)
        self.assertIn(b'ReadWritePaths=' + retention.RUNNER_STATE.encode() + b'\n', new_unit)
        self.assertIn(retention.OWN_PRUNER.encode(), new_unit)
        for other in old_roots:
            self.assertNotIn(other.encode(), new_unit)
        self.assertIn(b'OnCalendar=hourly\n', new_timer)
        self.assertIn(b'RandomizedDelaySec=5m\n', new_timer)
        self.assertIn(b'Persistent=true\n', new_timer)
        self.assertIn(b'Unit=robotgo-ci-state-retention.service\n', new_timer)
        for field in (b'CPUQuota=10%\n', b'MemoryMax=128M\n', b'TasksMax=16\n',
                      b'TimeoutStartSec=15min\n', b'ProtectSystem=strict\n',
                      b'ProtectHome=read-only\n', b'NoNewPrivileges=true\n'):
            self.assertIn(field, new_unit)

    def test_unsafe_names_duplicates_and_already_admitted_refused(self):
        base = ('photo-roller-remote-runner', 'position-tracking-dependencies')
        for bad in ('../secrets', '/etc', 'a/b', 'any root', '', 'robotgo-ovh', base[0]):
            with self.subTest(root=bad):
                with self.assertRaises(retention.Refused):
                    retention.derive(pruner(base + (bad,)), unit(base + (bad,)))

    def test_age_identity_and_source_ambiguity_refused(self):
        for old, new in ((b'14 * 24', b'7 * 24'), (b'7051508', b'0'),
                         (b"Path('/home/izoone_workers')", b"Path('/etc')"),
                         (b'MAX_RUNS = 100', b'MAX_RUNS = 200'),
                         (b'MAX_RUNS = 100', b'ROOTS = ()')):
            with self.subTest(change=old):
                with self.assertRaises(retention.Refused):
                    retention.derive(pruner().replace(old, new), unit())

    def test_writable_paths_and_unit_boundary_refused(self):
        for bad in (unit().replace(b'ProtectSystem=strict', b'ProtectSystem=false'),
                    unit() + b'ReadWritePaths=/etc\n',
                    unit().replace(b'ReadWritePaths=', b'ReadWritePaths=/etc '),
                    unit().replace(b'UMask=0077', b'UMask=0022'),
                    unit().replace(b'CPUQuota=10%', b'CPUQuota=10%\\\n')):
            with self.assertRaises(retention.Refused):
                retention.derive(pruner(), bad)


class TransactionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        uid, gid = os.getuid(), os.getgid()
        self.extension = retention.Extension(self.root, uid, gid, uid, gid, uid, gid)
        self.write(retention.PRUNER, pruner(), 0o444)
        self.write(retention.UNIT, unit())
        self.write(retention.TIMER, b'[Timer]\nOnCalendar=daily\nPersistent=true\n')
        self.write(retention.LOCK, b'')
        self.write(retention.RUNNER_LOCK, b'')
        (self.root / 'var/lib').mkdir(parents=True, mode=0o755)
        self.before = self.extension.observed()
        self.baseline_hashes = tuple(self.before[key][1]['sha256'] for key in ('pruner', 'unit', 'timer'))
        self.candidates = {}
        _, candidate_pruner, candidate_unit, candidate_timer = retention.derive(pruner(), unit())
        for name, data in (('pruner', candidate_pruner), ('unit', candidate_unit), ('timer', candidate_timer)):
            path = '/review/' + name
            self.candidates[name] = self.write(path, data), retention.sha(data)
        self.reloader = mock.patch.object(self.extension, 'reload_manager')
        self.reload = self.reloader.start()
        self.addCleanup(self.reloader.stop)

    def write(self, absolute, content, mode=0o600):
        path = self.extension.path(absolute)
        missing = []
        parent = path.parent
        while not parent.exists():
            missing.append(parent)
            parent = parent.parent
        for entry in reversed(missing):
            entry.mkdir(mode=0o700)
        with path.open('wb') as stream:
            stream.write(content)
        path.chmod(mode)
        return path

    def apply(self):
        with self.extension.lock():
            return self.extension.extend(*self.baseline_hashes, candidates=self.candidates, apply=True)

    def test_preview_has_no_writes_or_reload(self):
        with self.extension.lock():
            report = self.extension.extend(*self.baseline_hashes)
        self.assertEqual(report['mode'], 'preview')
        self.assertEqual(self.extension.observed(), self.before)
        self.assertFalse(self.extension.path(retention.PENDING).exists())
        self.reload.assert_not_called()

    def test_create_only_own_three_targets_preserves_all_shared_bytes_and_inodes(self):
        runner = self.extension.validate_state()
        report = self.apply()
        self.assertFalse(report['pruning_executed'])
        self.assertFalse(report['timer_activated'])
        self.assertEqual(report['ttl_days'], 14)
        after = self.extension.observed()
        self.assertEqual(after, self.before)
        self.assertEqual(self.extension.validate_state(), runner)
        for key, target, owner, mode in self.extension.targets():
            content, fingerprint = self.extension.read(self.extension.path(target), owner, mode)
            self.assertEqual(retention.sha(content), report['candidate_sha256'][key])
            self.assertEqual(fingerprint['mode'], mode)
            self.assertEqual(self.extension.path(target).stat().st_nlink, 1)
        self.reload.assert_called_once_with()
        pending = self.extension.path(retention.PENDING)
        self.assertTrue((pending / 'complete.json').is_file())
        self.assertEqual((pending / 'prior-pruner').read_bytes(), pruner())
        self.assertEqual((pending / 'prior-unit').read_bytes(), unit())
        self.assertEqual(pending.stat().st_mode & 0o777, 0o700)

    def test_modified_reviewed_baseline_and_candidate_refused(self):
        self.write(retention.UNIT, unit() + b'# concurrent unit change\n')
        with self.assertRaisesRegex(retention.Refused, 'baseline digests differ'):
            self.apply()
        self.assertFalse(self.extension.path(retention.PENDING).exists())
        self.write(retention.UNIT, unit())
        self.candidates['unit'][0].write_bytes(b'not the reviewed candidate')
        with self.assertRaisesRegex(retention.Refused, 'exact additive bytes differ'):
            self.apply()

    def test_apply_requires_candidate_pins(self):
        with self.extension.lock():
            with self.assertRaisesRegex(retention.Refused, 'apply requires'):
                self.extension.extend(*self.baseline_hashes, apply=True)

    def test_candidate_inputs_private_without_symlinks_or_hardlinks(self):
        path = self.candidates['unit'][0]
        for mode in (0o644, 0o666):
            path.chmod(mode)
            with self.assertRaises(retention.Refused):
                self.apply()
        path.chmod(0o600)
        path.parent.chmod(0o755)
        with self.assertRaisesRegex(retention.Refused, 'private root directory'):
            self.apply()
        path.parent.chmod(0o700)
        data = path.read_bytes()
        target = self.write('/review/target', data)
        path.unlink()
        path.symlink_to(target)
        with self.assertRaises(retention.Refused):
            self.apply()
        path.unlink()
        os.link(target, path)
        with self.assertRaises(retention.Refused):
            self.apply()

    def test_lock_contention_and_missing_lock_no_recreation(self):
        with self.extension.path(retention.LOCK).open('rb') as other:
            fcntl.flock(other, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(retention.Refused, 'lock busy'):
                self.apply()
        self.assertEqual(self.extension.observed(), self.before)
        self.extension.path(retention.LOCK).unlink()
        with self.assertRaises(FileNotFoundError):
            self.apply()
        self.assertFalse(self.extension.path(retention.LOCK).exists())

    def test_runner_state_must_be_private_with_persistent_lock(self):
        self.extension.path(retention.RUNNER_STATE).chmod(0o755)
        with self.assertRaises(retention.Refused):
            self.apply()
        self.assertFalse(self.extension.path(retention.PENDING).exists())
        self.extension.path(retention.RUNNER_STATE).chmod(0o700)
        self.extension.path(retention.RUNNER_LOCK).unlink()
        with self.assertRaises(FileNotFoundError):
            self.apply()

    def test_concurrent_mutation_preserved_before_any_replacement(self):
        original = self.extension.create
        def interfere(path, data, owner, mode):
            result = original(path, data, owner, mode)
            if path.name == '.robotgo-retention-next-unit':
                self.write(retention.UNIT, unit() + b'# concurrent mutation\n')
            return result
        with mock.patch.object(self.extension, 'create', side_effect=interfere):
            with self.assertRaisesRegex(retention.Refused, 'stale retention state'):
                self.apply()
        self.assertEqual(self.extension.path(retention.PRUNER).read_bytes(), pruner())
        self.assertTrue(self.extension.path(retention.UNIT).read_bytes().endswith(b'# concurrent mutation\n'))
        self.assertTrue((self.extension.path(retention.PENDING) / 'intent.json').exists())
        self.reload.assert_not_called()

    def test_partial_write_retains_journal_without_replacements(self):
        original = self.extension.create
        def fail(path, data, owner, mode):
            if path.name == '.robotgo-retention-next-pruner':
                original(path, data[:10], owner, mode)
                raise OSError('simulated interrupted write')
            return original(path, data, owner, mode)
        with mock.patch.object(self.extension, 'create', side_effect=fail):
            with self.assertRaises(OSError):
                self.apply()
        self.assertEqual(self.extension.observed(), self.before)
        self.assertTrue((self.extension.path(retention.PENDING) / 'intent.json').exists())
        with self.assertRaisesRegex(retention.Refused, 'pending retention journal'):
            self.apply()

    def test_reload_failure_leaves_own_files_and_pending_without_modifying_shared_files(self):
        self.reload.side_effect = retention.subprocess.CalledProcessError(1, 'fixture reload')
        with self.assertRaises(retention.subprocess.CalledProcessError):
            self.apply()
        self.assertIn(b'robotgo-ovh', self.extension.path(retention.OWN_PRUNER).read_bytes())
        self.assertIn(retention.RUNNER_STATE.encode(), self.extension.path(retention.OWN_UNIT).read_bytes())
        self.assertIn(b'OnCalendar=hourly', self.extension.path(retention.OWN_TIMER).read_bytes())
        pending = self.extension.path(retention.PENDING)
        self.assertFalse((pending / 'complete.json').exists())
        self.assertIn(b'needs-worker-daemon-reload', (pending / 'progress.json').read_bytes())
        self.assertEqual(self.extension.observed(), self.before)

    def test_existing_own_target_is_never_overwritten_or_adopted(self):
        for _, target, _, mode in self.extension.targets():
            with self.subTest(target=target):
                path = self.write(target, b'existing unrelated or interrupted file', mode)
                inode = path.stat().st_ino
                with self.assertRaisesRegex(retention.Refused, 'target exists; never overwrite'):
                    self.apply()
                self.assertEqual(path.read_bytes(), b'existing unrelated or interrupted file')
                self.assertEqual(path.stat().st_ino, inode)
                self.assertFalse(self.extension.path(retention.PENDING).exists())
                path.unlink()

    def test_target_appearing_during_publication_is_preserved(self):
        original = self.extension.publish
        def interfere(source, destination):
            if destination == self.extension.path(retention.OWN_UNIT):
                destination.write_bytes(b'unexpected concurrent new unit')
            return original(source, destination)
        with mock.patch.object(self.extension, 'publish', side_effect=interfere):
            with self.assertRaises(FileExistsError):
                self.apply()
        self.assertEqual(self.extension.path(retention.OWN_UNIT).read_bytes(), b'unexpected concurrent new unit')
        self.assertEqual(self.extension.observed(), self.before)
        self.assertTrue((self.extension.path(retention.PENDING) / 'intent.json').exists())
        self.reload.assert_not_called()

    def test_partial_publication_keeps_shared_state_and_private_journal(self):
        original = self.extension.publish
        def fail(source, destination):
            if destination == self.extension.path(retention.OWN_UNIT):
                raise OSError('interrupted between own pruner and own service')
            return original(source, destination)
        with mock.patch.object(self.extension, 'publish', side_effect=fail):
            with self.assertRaises(OSError):
                self.apply()
        self.assertEqual(self.extension.observed(), self.before)
        self.assertTrue(self.extension.path(retention.OWN_PRUNER).is_file())
        self.assertFalse(self.extension.path(retention.OWN_UNIT).exists())
        self.assertFalse(self.extension.path(retention.OWN_TIMER).exists())
        self.assertTrue((self.extension.path(retention.PENDING) / 'intent.json').is_file())
        self.reload.assert_not_called()
        with self.assertRaisesRegex(retention.Refused, 'pending retention journal'):
            self.apply()

    def test_interrupted_link_publication_retains_both_names_for_reconciliation(self):
        def fail_after_link(source, destination):
            os.link(source, destination, follow_symlinks=False)
            raise OSError('interrupted before own staging-name removal')
        with mock.patch.object(self.extension, 'publish', side_effect=fail_after_link):
            with self.assertRaises(OSError):
                self.apply()
        self.assertEqual(self.extension.observed(), self.before)
        self.assertEqual(self.extension.path(retention.OWN_PRUNER).stat().st_nlink, 2)
        self.assertTrue((self.extension.path(retention.PENDING) / 'intent.json').is_file())
        self.assertFalse(self.extension.path(retention.OWN_TIMER).exists())
        self.reload.assert_not_called()

    def test_daemon_reload_command_never_starts_or_enables_any_unit(self):
        self.reloader.stop()
        with mock.patch.object(retention.subprocess, 'run') as run:
            self.extension.reload_manager()
        command = run.call_args.args[0]
        self.assertEqual(command[-3:], ['/usr/bin/systemctl', '--user', 'daemon-reload'])
        self.assertNotIn('start', command)
        self.assertNotIn('enable', command)


if __name__ == '__main__':
    unittest.main()
