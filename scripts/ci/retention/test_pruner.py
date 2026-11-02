"""Exercise the retained pruner only against disposable private fixture roots."""
import fcntl
import importlib.util
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('pruner', Path(__file__).with_name('prune-robotgo-state.py'))
pruner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pruner)


class PrunerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='robotgo-retention-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        os.chmod(self.root, 0o700)
        self.lock = self.root / 'runner.lock'
        self.lock.touch(mode=0o600)
        self.fd = os.open(self.root, pruner.DIRECTORY_FLAGS)
        self.addCleanup(os.close, self.fd)
        self.identity = mock.patch.multiple(pruner, WORKER_UID=os.getuid(), WORKER_GID=os.getgid())
        self.identity.start()
        self.addCleanup(self.identity.stop)
        self.now = time.time()

    def run_dir(self, char='a', age=None):
        path = self.root / (char * 32)
        path.mkdir(mode=0o700)
        (path / 'report').write_text('fixture')
        stamp = self.now - (pruner.TTL_SECONDS + 1 if age is None else age)
        os.utime(path, (stamp, stamp))
        return path

    def prune(self, apply=False, budget=100):
        return pruner.prune_root(self.fd, apply, self.now, [budget])

    def test_preview_then_apply_only_expired_run(self):
        expired = self.run_dir()
        young = self.run_dir('b', 0)
        held = self.root / 'hold-journal'
        held.mkdir()
        result = self.prune()
        self.assertEqual(result['runs'], [{'id': expired.name, 'action': 'would_delete'}])
        self.assertEqual(result['young'], 1)
        self.assertTrue(expired.exists())
        result = self.prune(True)
        self.assertEqual(result['runs'][0]['action'], 'deleted')
        self.assertFalse(expired.exists())
        self.assertTrue(young.exists())
        self.assertTrue(held.exists())
        self.assertTrue(self.lock.exists())

    def test_busy_lock_prevents_deletion(self):
        expired = self.run_dir()
        with self.lock.open('r+') as held:
            fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.assertEqual(self.prune(True)['status'], 'busy')
        self.assertTrue(expired.exists())

    def test_run_symlink_is_refused_and_target_preserved(self):
        target = self.root / 'journal'
        target.mkdir(mode=0o700)
        (self.root / ('a' * 32)).symlink_to(target)
        result = self.prune(True)
        self.assertEqual(result['status'], 'error')
        self.assertEqual(result['runs'][0]['action'], 'refused')
        self.assertTrue(target.exists())

    def test_nested_symlink_never_traverses_target(self):
        target = self.root / 'journal'
        target.mkdir()
        (target / 'retained').write_text('fixture')
        expired = self.run_dir()
        (expired / 'external').symlink_to(target)
        stamp = self.now - pruner.TTL_SECONDS - 1
        os.utime(expired, (stamp, stamp))
        self.assertEqual(self.prune(True)['status'], 'ok')
        self.assertEqual((target / 'retained').read_text(), 'fixture')
        self.assertFalse(expired.exists())

    def test_special_file_refuses_entire_run(self):
        expired = self.run_dir()
        os.mkfifo(expired / 'fifo')
        stamp = self.now - pruner.TTL_SECONDS - 1
        os.utime(expired, (stamp, stamp))
        self.assertEqual(self.prune(True)['status'], 'error')
        self.assertTrue(expired.exists())

    def test_lock_hardlink_rejected(self):
        expired = self.run_dir()
        os.link(self.lock, self.root / 'other-lock')
        with self.assertRaisesRegex(ValueError, 'link count'):
            self.prune(True)
        self.assertTrue(expired.exists())

    def test_budget_and_tree_bound_preserve_remaining_runs(self):
        first = self.run_dir()
        second = self.run_dir('b')
        result = self.prune(True, budget=1)
        self.assertTrue(result['limit_reached'])
        self.assertFalse(first.exists())
        self.assertTrue(second.exists())
        with mock.patch.object(pruner, 'MAX_TREE_ENTRIES', 0):
            self.assertEqual(self.prune(True)['status'], 'error')
        self.assertTrue(second.exists())

    def test_wrong_mode_or_missing_lock_fails_closed(self):
        expired = self.run_dir()
        os.chmod(expired, 0o755)
        self.assertEqual(self.prune(True)['status'], 'error')
        self.assertTrue(expired.exists())
        self.lock.unlink()
        with self.assertRaises(FileNotFoundError):
            self.prune(True)


if __name__ == '__main__':
    unittest.main()
