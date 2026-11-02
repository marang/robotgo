#!/usr/bin/env python3
"""Build a create-only sealed RobotGo profile offline; never install or connect."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
GO_VERSION = 'go1.26.10'
HOME = '/home/izoone_workers'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def verify_elf(binary):
    if len(binary) < 64 or binary[:6] != b'\x7fELF\x02\x01':
        raise ValueError('expected little-endian ELF64')
    if struct.unpack_from('<H', binary, 18)[0] != 62:
        raise ValueError('expected AMD64 launcher')
    offset = struct.unpack_from('<Q', binary, 32)[0]
    size, count = struct.unpack_from('<HH', binary, 54)
    if size < 56 or offset + size * count > len(binary):
        raise ValueError('invalid ELF program headers')
    if any(struct.unpack_from('<I', binary, offset + n * size)[0] == 3 for n in range(count)):
        raise ValueError('dynamic launcher interpreter is forbidden')


def build_launcher(code, driver, go):
    if not go.is_absolute() or not go.is_file():
        raise ValueError('explicit installed Go compiler required')
    with tempfile.TemporaryDirectory(prefix='robotgo-sealed-profile-') as temporary:
        root = Path(temporary)
        env = {'HOME': str(root), 'PATH': '/usr/bin:/bin', 'CGO_ENABLED': '0',
               'GOOS': 'linux', 'GOARCH': 'amd64', 'GOAMD64': 'v1',
               'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
               'GOCACHEPROG': '', 'GOTELEMETRY': 'off', 'GOPROXY': 'off',
               'GOSUMDB': 'off', 'GO111MODULE': 'off', 'GOCACHE': str(root / 'cache')}
        version = subprocess.check_output([str(go), 'version'], env=env, timeout=10).decode().strip()
        if version != 'go version ' + GO_VERSION + ' linux/amd64':
            raise ValueError('profile requires exactly Go 1.26.10 linux/amd64')
        (root / 'launcher.go').write_bytes(code)
        subprocess.run([str(go), 'build', '-trimpath', '-buildvcs=false',
                        '-ldflags', '-s -w -buildid= -X main.driverSHA256=' + sha(driver),
                        '-o', str(root / 'entry'), str(root / 'launcher.go')],
                       cwd=root, env=env, capture_output=True, check=True, timeout=120)
        binary = (root / 'entry').read_bytes()
    verify_elf(binary)
    return binary


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image-id', required=True)
    parser.add_argument('--go', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args(argv)
    if not re.fullmatch(r'sha256:[0-9a-f]{64}', args.image_id):
        raise ValueError('immutable local sha256 image ID required')
    if args.output.exists() or args.output.is_symlink():
        raise ValueError('output must be a new directory')
    original = (HERE / 'runner.py').read_bytes()
    if original.count(b"IMAGE = '@IMAGE_ID@'") != 1:
        raise ValueError('runner image seal seam changed')
    driver = original.replace(b"IMAGE = '@IMAGE_ID@'", ('IMAGE = ' + repr(args.image_id)).encode())
    code = (HERE / 'launcher.go').read_bytes()
    binary = build_launcher(code, driver, args.go)
    files = {'robotgo-ci.py': driver, 'robotgo-ci': binary}
    manifest = {'schema_version': 1, 'owner': 'izoone_workers', 'uid': 7051508,
                'gid': 1004, 'home': HOME, 'image': args.image_id,
                'go_version': GO_VERSION, 'installed': False,
                'source_sha256': {'runner.py': sha(original), 'launcher.go': sha(code)},
                'files': {name: sha(content) for name, content in files.items()}}
    args.output.mkdir(mode=0o700, parents=False, exist_ok=False)
    for name, data in files.items():
        path = args.output / name
        with path.open('xb') as output:
            output.write(data)
        path.chmod(0o700)
    path = args.output / 'profile.json'
    with path.open('x') as output:
        json.dump(manifest, output, sort_keys=True, indent=2)
        output.write('\n')
    path.chmod(0o600)
    print(json.dumps(manifest, sort_keys=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
