"""Only temporary fixtures; never invoke privileged host installation."""
import importlib.util
import json
import os
from pathlib import Path
import struct
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location('install_worker', Path(__file__).with_name('install-worker.py'))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class InstallChecks(unittest.TestCase):
    def test_static_elf_rejects_interpreter_and_truncation(self):
        data = bytearray(120)
        data[:6] = b'\x7fELF\x02\x01'
        struct.pack_into('<H', data, 18, 62)
        struct.pack_into('<Q', data, 32, 64)
        struct.pack_into('<HH', data, 54, 56, 1)
        struct.pack_into('<I', data, 64, 1)
        MODULE.static_elf(data)
        for candidate in (b'not ELF', data[:70]):
            with self.assertRaises(ValueError):
                MODULE.static_elf(candidate)
        struct.pack_into('<I', data, 64, 3)
        with self.assertRaises(ValueError):
            MODULE.static_elf(data)

    def test_create_is_exclusive_and_metadata_is_strict(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / 'code'
            MODULE.create(path, b'reviewed', os.getuid(), os.getgid(), 0o600)
            self.assertEqual(MODULE.read_file(path, os.getuid(), {0o600}), b'reviewed')
            with self.assertRaises(FileExistsError):
                MODULE.create(path, b'replacement', os.getuid(), os.getgid(), 0o600)
            self.assertEqual(path.read_bytes(), b'reviewed')
            path.chmod(0o644)
            with self.assertRaises(ValueError):
                MODULE.read_file(path, os.getuid(), {0o600})

    def test_symlinks_and_hardlinks_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / 'code'
            MODULE.create(path, b'reviewed', os.getuid(), os.getgid(), 0o600)
            (root / 'link').symlink_to(path)
            with self.assertRaises(OSError):
                MODULE.read_file(root / 'link', os.getuid(), {0o600})
            (root / 'directory-link').symlink_to(root, target_is_directory=True)
            with self.assertRaises(ValueError):
                MODULE.directory(root / 'directory-link', os.getuid(), 0o700)
            os.link(path, root / 'hardlink')
            with self.assertRaises(ValueError):
                MODULE.read_file(path, os.getuid(), {0o600})

    def test_staging_cannot_be_general_filesystem_input(self):
        with self.assertRaises(ValueError):
            MODULE.inputs(Path('/tmp/arbitrary'), '0' * 64)
        with self.assertRaises(ValueError):
            MODULE.inputs(MODULE.ROOT / 'bundle', 'not-a-hash')

    def test_profile_identity_requires_current_normalized_account_and_home(self):
        bundle = MODULE.ROOT / 'identity-fixture'
        files = {name: ('fixture-' + name).encode() for name in MODULE.NAMES}
        profile = {'schema_version': 1, 'owner': 'izoone_workers', 'uid': MODULE.UID,
                   'gid': MODULE.GID, 'home': str(MODULE.HOME), 'go_version': 'go1.26.10',
                   'installed': False, 'image': 'sha256:' + 'a' * 64,
                   'files': {name: MODULE.sha(data) for name, data in files.items()}}
        for legacy_field in (None, 'owner', 'home'):
            with self.subTest(legacy=legacy_field):
                candidate = dict(profile)
                if legacy_field is not None:
                    candidate[legacy_field] += '_vps16776'
                raw = (json.dumps(candidate) + '\n').encode()
                def read(path, owner, modes):
                    return raw if path.name == 'profile.json' else files[path.name]
                with (mock.patch.object(MODULE, 'directory'),
                      mock.patch.object(MODULE, 'read_file', side_effect=read),
                      mock.patch.object(MODULE, 'static_elf'),
                      mock.patch.object(Path, 'iterdir', return_value=[bundle / name for name in (*MODULE.NAMES, 'profile.json')])):
                    if legacy_field is None:
                        self.assertEqual(MODULE.inputs(bundle, MODULE.sha(raw))[0], candidate)
                    else:
                        with self.assertRaisesRegex(ValueError, 'invalid profile'):
                            MODULE.inputs(bundle, MODULE.sha(raw))


if __name__ == '__main__':
    unittest.main()
