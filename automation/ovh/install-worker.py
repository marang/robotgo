#!/usr/bin/python3 -I
"""Create only the reviewed RobotGo worker profile; preview is the default.

Trusted administrator operation, not a Dagu entrypoint or initial installer.
Never reads token values or modifies another project's files or services.
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import socket
import stat
import struct
import subprocess

HOME = Path('/home/izoone_workers')
UID, GID = 7051508, 1004
CONTROLLER = Path('/home/izoone_automation')
ROOT = Path('/var/lib/izoone-ci-provision')
STATE = HOME / '.local/state/robotgo-ovh'
NAMES = ('robotgo-ci', 'robotgo-ci.py')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def directory(path, owner, mode=None):
    info = path.lstat()
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != owner
            or info.st_mode & 0o022
            or (mode is not None and stat.S_IMODE(info.st_mode) != mode)):
        raise ValueError('unsafe directory')


def read_file(path, owner, modes):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as incoming:
        info = os.fstat(incoming.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != owner
                or stat.S_IMODE(info.st_mode) not in modes or info.st_nlink != 1
                or not 0 < info.st_size <= 16 * 1024 * 1024):
            raise ValueError('unsafe input file')
        data = incoming.read(16 * 1024 * 1024 + 1)
        after = os.fstat(incoming.fileno())
        if (len(data) != info.st_size or
                (info.st_ino, info.st_mtime_ns, info.st_ctime_ns) !=
                (after.st_ino, after.st_mtime_ns, after.st_ctime_ns)):
            raise ValueError('input changed')
        return data


def static_elf(data):
    if len(data) < 64 or data[:6] != b'\x7fELF\x02\x01':
        raise ValueError('64-bit little-endian ELF required')
    machine = struct.unpack_from('<H', data, 18)[0]
    offset = struct.unpack_from('<Q', data, 32)[0]
    size, count = struct.unpack_from('<HH', data, 54)
    if machine != 62 or size < 56 or not 1 <= count <= 100:
        raise ValueError('static x86_64 ELF required')
    if offset + size * count > len(data):
        raise ValueError('truncated ELF')
    if any(struct.unpack_from('<I', data, offset + i * size)[0] == 3 for i in range(count)):
        raise ValueError('dynamic interpreter refused')


def inputs(bundle, expected):
    if not re.fullmatch('[0-9a-f]{64}', expected):
        raise ValueError('reviewed manifest SHA256 required')
    if not bundle.is_absolute() or not bundle.is_relative_to(ROOT) or bundle == ROOT:
        raise ValueError('root-private reviewed staging required')
    for parent in reversed(bundle.parents):
        directory(parent, 0)
    directory(bundle, 0, 0o700)
    if {p.name for p in bundle.iterdir()} != {*NAMES, 'profile.json'}:
        raise ValueError('exact three-file profile required')
    raw = read_file(bundle / 'profile.json', 0, {0o600})
    if sha(raw) != expected:
        raise ValueError('profile differs from review')
    profile = json.loads(raw)
    if (profile.get('schema_version') != 1 or
            profile.get('owner') != 'izoone_workers' or profile.get('uid') != UID or
            profile.get('gid') != GID or profile.get('home') != str(HOME) or
            profile.get('go_version') != 'go1.26.10' or profile.get('installed') is not False or
            not re.fullmatch('sha256:[0-9a-f]{64}', profile.get('image', '')) or
            set(profile.get('files', {})) != set(NAMES)):
        raise ValueError('invalid profile')
    files = {name: read_file(bundle / name, 0, {0o600, 0o700}) for name in NAMES}
    if any(sha(data) != profile['files'][name] for name, data in files.items()):
        raise ValueError('profile code differs from review')
    static_elf(files['robotgo-ci'])
    return profile, files


def create(path, data, owner, group, mode):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, 'wb') as output:
        os.fchown(output.fileno(), owner, group)
        os.fchmod(output.fileno(), mode)
        output.write(data)
        output.flush()
        os.fsync(output.fileno())


def install(bundle, expected, apply):
    if os.getuid() != 0 or socket.gethostname() != 'vps-ac45f3e2.vps.ovh.net':
        raise ValueError('trusted OVH root operation required')
    if Path('/sys/fs/selinux/enforce').read_text().strip() != '1':
        raise ValueError('enforcing SELinux required')
    account = pwd.getpwuid(UID)
    if (account.pw_name, account.pw_gid, account.pw_dir) != ('izoone_workers', GID, str(HOME)):
        raise ValueError('fixed worker identity required')
    profile, files = inputs(bundle, expected)
    for path in (HOME, HOME / '.local', HOME / '.local/libexec', HOME / '.local/state'):
        directory(path, UID, 0o700)
    boundary = CONTROLLER / '.local/state/dagu-boundary'
    for path in (CONTROLLER, CONTROLLER / '.local', CONTROLLER / '.local/state', boundary):
        directory(path, 7047744, 0o700)
    lock = os.open(boundary / 'native-activation.lock', os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(lock)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 7047744 or
                stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
            raise ValueError('unsafe persistent activation lock')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        pending = ROOT / 'robotgo-worker.pending'
        targets = [HOME / '.local/libexec' / name for name in NAMES]
        if any(os.path.lexists(path) for path in [pending, STATE, *targets]):
            raise ValueError('existing RobotGo state requires reconciliation; no blind retry')
        env = {'HOME': str(HOME), 'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8',
               'XDG_RUNTIME_DIR': '/run/user/7051508'}
        check = subprocess.run(['/usr/bin/podman', '--remote=false', 'image', 'inspect',
                                '--format', '{{.Id}}', profile['image']],
                               user=UID, group=GID, extra_groups=[], env=env,
                               cwd='/', capture_output=True, timeout=30)
        if (check.returncode or
                check.stdout.decode().strip().removeprefix('sha256:') != profile['image'].removeprefix('sha256:')):
            raise ValueError('reviewed immutable local image unavailable')
        report = {'mode': 'apply' if apply else 'preview', 'image': profile['image'],
                  'profile_sha256': expected, 'files': profile['files'],
                  'existing_auth_services_and_projects_untouched': True}
        if not apply:
            return report
        create(pending, (json.dumps(report, sort_keys=True) + '\n').encode(), 0, 0, 0o600)
        # All writes are create-only. Partial failures retain the pending receipt.
        STATE.mkdir(mode=0o700)
        os.chown(STATE, UID, GID)
        create(STATE / 'runner.lock', b'', UID, GID, 0o600)
        for name in NAMES:
            create(HOME / '.local/libexec' / name, files[name], UID, GID, 0o700)
        for name in NAMES:
            if read_file(HOME / '.local/libexec' / name, UID, {0o700}) != files[name]:
                raise ValueError('installed code differs')
        for parent in (STATE, HOME / '.local/libexec', ROOT):
            fd = os.open(parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        receipt = ROOT / ('robotgo-worker-' + expected + '.json')
        create(receipt, (json.dumps(report, sort_keys=True) + '\n').encode(), 0, 0, 0o600)
        pending.unlink()
        return report
    finally:
        os.close(lock)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bundle', type=Path, required=True)
    parser.add_argument('--expect-profile-sha256', required=True)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    os.umask(0o077)
    try:
        print(json.dumps(install(args.bundle, args.expect_profile_sha256, args.apply), sort_keys=True))
    except Exception as error:
        print(json.dumps({'status': 'refused', 'reason_type': type(error).__name__}))
        raise SystemExit(1)


if __name__ == '__main__':
    main()
