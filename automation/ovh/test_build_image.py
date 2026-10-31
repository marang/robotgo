"""Offline build supervisor checks; no Podman or systemd operation is executed."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location('build_image', Path(__file__).with_name('build-image.py'))
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)


class BuildSupervisorTests(unittest.TestCase):
    def test_both_podman_image_id_encodings_are_exact(self):
        self.assertEqual(BUILD.image_id('a' * 64), 'sha256:' + 'a' * 64)
        self.assertEqual(BUILD.image_id('sha256:' + 'a' * 64), 'sha256:' + 'a' * 64)
        for value in ('sha512:' + 'a' * 64, 'a' * 63, 'not an ID'):
            with self.assertRaises(ValueError):
                BUILD.image_id(value)

    def setUp(self):
        previous_umask = os.umask(0o022)
        self.addCleanup(os.umask, previous_umask)
        # Private home ancestry also exercises production's no-writable-parent
        # policy; /tmp is deliberately not an admitted image-build ancestor.
        self.temp = tempfile.TemporaryDirectory(prefix='.robotgo-image-fixture-', dir=Path.home())
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.context = self.home / '.local/state/robotgo-image-build/run/context'
        self.context.mkdir(parents=True, mode=0o700)
        self.context.parent.chmod(0o700)
        self.expected = {}
        for name in BUILD.FILES:
            content = ('fixture-' + name).encode()
            path = self.context / name
            path.write_bytes(content)
            path.chmod(0o600)
            self.expected[name] = hashlib.sha256(content).hexdigest()

    def invoke(self, context=None, failure=False):
        process = mock.Mock()
        process.wait.side_effect = TimeoutError() if failure else None
        process.wait.return_value = 0
        process.poll.return_value = 0
        with (mock.patch.object(BUILD, 'HOME', self.home),
              mock.patch.object(BUILD, 'UID', os.getuid()),
              mock.patch.object(BUILD, 'GID', os.getgid()),
              mock.patch.object(BUILD.socket, 'gethostname', return_value='vps-ac45f3e2.vps.ovh.net'),
              mock.patch.object(BUILD.pwd, 'getpwuid', return_value=SimpleNamespace(pw_dir=str(self.home))),
              mock.patch('sys.argv', ['build-image.py', '--context', str(context or self.context),
                                      '--expected', json.dumps(self.expected)]),
              mock.patch.object(BUILD.subprocess, 'Popen', return_value=process) as popen,
              mock.patch.object(BUILD.subprocess, 'run', return_value=SimpleNamespace(returncode=0)) as run):
            try:
                BUILD.main()
            finally:
                self.popen, self.run, self.process = popen, run, process

    def test_transient_unit_has_limits_timeout_and_scoped_cleanup(self):
        self.invoke()
        command = self.popen.call_args.args[0]
        for argument in ('--user', '--pipe', '--wait', '--collect', '--property=CPUQuota=200%',
                         '--property=MemoryMax=8589934592', '--property=TasksMax=256',
                         '--property=Delegate=yes', '--property=DelegateSubgroup=supervisor',
                         '--property=RuntimeMaxSec=1800', '--property=KillMode=control-group'):
            self.assertIn(argument, command)
        unit = next(arg.removeprefix('--unit=') for arg in command if arg.startswith('--unit='))
        self.assertRegex(unit, r'^robotgo-image-build-[0-9a-f]{32}\.service$')
        self.assertEqual(self.run.call_args.args[0], ['/usr/bin/systemctl', '--user', 'stop', unit])
        self.assertFalse((self.context.parent / 'containers.conf').exists())

    def test_parent_timeout_stops_only_owned_unit(self):
        with self.assertRaises(TimeoutError):
            self.invoke(failure=True)
        self.assertEqual(self.run.call_args.args[0][:3], ['/usr/bin/systemctl', '--user', 'stop'])
        self.assertRegex(self.run.call_args.args[0][3], r'^robotgo-image-build-[0-9a-f]{32}\.service$')

    def test_parent_traversal_and_symlink_refused_before_execution(self):
        traversal = self.context.parent.parent / '..' / 'other-project/context'
        with self.assertRaises(ValueError):
            self.invoke(traversal)
        self.popen.assert_not_called()
        link = self.context.parent / 'context-link'
        link.symlink_to(self.context, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.invoke(link)
        self.popen.assert_not_called()

    def test_wrong_hash_or_extra_file_refused(self):
        self.expected['Containerfile'] = '0' * 64
        with self.assertRaises(ValueError):
            self.invoke()
        self.popen.assert_not_called()
        (self.context / 'not-admitted-source').write_text('fixture')
        with self.assertRaises(ValueError):
            self.invoke()
        self.popen.assert_not_called()


if __name__ == '__main__':
    unittest.main()
