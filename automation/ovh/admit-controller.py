#!/usr/bin/python3 -I
"""Reviewed additive RobotGo admission; preview by default, never releases holds.

Run as the existing OVH controller account through trusted SSH, not Dagu/MCP.
--snapshot prints a fresh, nonsecret baseline for independent offline review.
Store it privately, review its bytes, and put its SHA256 alone in a second
private file. Preview/--apply/--check require both files and a private --bundle
directory containing exactly admit-controller.py and the four fixed YAML files
returned by definitions(). The helper in that bundle must match this process.

--apply creates a persistent private pending journal; even on interruption it
must NEVER be deleted to retry. Reconcile the intent/progress and live state.
All four new holds precede any new YAML; active-manifest.json is replaced last.
No service, existing YAML, auth, history, policy helper or old hold is changed.
--check verifies staged additions; --check --released is a read-only check after
separate runtime proof and explicitly authorized release of only the new holds.

The old fixed-manifest verifier remains unchanged (and will reject additions).
This snapshot-bound checker is an additive admission check, NOT a replacement
for the canonical hardener's stopped-service/empty-history activation guards or
the authenticated global API capability tests. This is not an arbitrary DAG API.
"""

import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import sys


HOME = Path('/home/izoone_automation')
UID, GID = 7047744, 1003
WORKER_HOME = '/home/izoone_workers'
CONFIG = '.config/dagu'
STATE = '.local/state/dagu-boundary'
MANIFEST = STATE + '/active-manifest.json'
LOCK = STATE + '/native-activation.lock'
PENDING = STATE + '/robotgo-admission.pending'
HOLDS = '.local/share/dagu/data/suspend'
CONFIG_SHA256 = '0c886533dba857cb8689cde250f0b1cde83102811db06cefef25c57791aaf914'
HARDENER_SHA256 = '96b91e96cee1755641de37fbb61bb44474e4148de30f43428973cd4043537287'
PROTECTED = (
    CONFIG + '/config.yaml',
    '.local/libexec/harden-dagu-controller.py',
    '.local/libexec/check-dagu-controller-policy.py',
    '.local/libexec/izoone-ovh-ci/controller-policy.py',
    '.local/libexec/izoone-ovh-ci/definitions.py',
)
LIMIT = 1024 * 1024
DIGEST = re.compile(r'[0-9a-f]{64}')
DAG_FILE = re.compile(r'dags/[a-z0-9][a-z0-9-]*\.yaml')
HOLD_FILE = re.compile(r'[a-z0-9][a-z0-9-]*\.suspend')
OLD_HOLDS = {
    'photo-roller-dependencies.suspend', 'photo-roller-security.suspend',
    'position-tracking-renovate.suspend', 'photo-roller-local-ci.suspend',
    'photo-roller-local-watch.suspend', 'photo-roller-local-security.suspend',
    'photo-roller-local-dependencies.suspend',
}


class Refused(ValueError):
    """Fixed nonsecret diagnostics only; never include file contents."""


def sha(data):
    return hashlib.sha256(data).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n').encode()


def decode(data):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise Refused('duplicate JSON key')
            result[key] = value
        return result
    try:
        return json.loads(data, object_pairs_hook=unique)
    except (ValueError, UnicodeError) as exc:
        raise Refused('invalid private JSON') from exc


def definitions():
    tasks = {
        'robotgo-ovh-ci': ('ci', None, False),
        'robotgo-ovh-watch': ('ci', '7-59/15 * * * *', True),
        'robotgo-ovh-security': ('security', '17 4 * * 1', False),
        'robotgo-ovh-dependencies': ('dependencies', '27 4 * * 1', False),
    }
    result = {}
    for name, (task, schedule, changed) in tasks.items():
        command = WORKER_HOME + '/.local/libexec/robotgo-ci --task ' + task
        if changed:
            command += ' --if-changed'
        text = ('type: graph\nshell: direct\nqueue: photoroller-local\n'
                'worker_selector:\n  runner: izoone-ovh\n  isolation: podman\n'
                'params:\n  type: object\n  properties: {}\n  additionalProperties: false\n'
                'artifacts:\n  enabled: false\noverlap_policy: skip\ntimeout_sec: 3700\n'
                'max_active_steps: 1\nhist_retention_days: 14\n')
        if schedule:
            text += 'schedule: "CRON_TZ=Europe/Vienna ' + schedule + '"\n'
        text += 'steps:\n  - id: ' + task + '\n    command: ["' + command + '"]\n'
        result[name + '.yaml'] = text.encode()
    return result


class Controller:
    """Only library tests may supply a different root/identity; CLI is fixed."""

    def __init__(self, home=HOME, uid=UID, gid=GID):
        self.home, self.uid, self.gid = Path(home), uid, gid

    def path(self, relative):
        candidate = self.home / relative
        if (not candidate.is_absolute() or '..' in candidate.parts
                or not candidate.is_relative_to(self.home) or candidate == self.home):
            raise Refused('path outside private controller home')
        return candidate

    def inspect(self, path, directory=False):
        path = Path(path)
        if '..' in path.parts or not path.is_relative_to(self.home):
            raise Refused('path outside private controller home')
        # Reject symlinks even above the private boundary, without requiring
        # system ancestors (/home, /) to belong to the controller account.
        for parent in reversed(self.home.parents):
            if not stat.S_ISDIR(parent.lstat().st_mode):
                raise Refused('unsafe system ancestor')
        chain = [self.home]
        if path != self.home:
            chain += list(reversed([p for p in path.parents if p.is_relative_to(self.home) and p != self.home]))
            chain += [path]
        for item in chain:
            info = item.lstat()
            if (info.st_uid != self.uid or info.st_gid != self.gid or info.st_mode & 0o077
                    or stat.S_ISLNK(info.st_mode)):
                raise Refused('unsafe private controller path')
            if item != path or directory:
                if not stat.S_ISDIR(info.st_mode):
                    raise Refused('expected private directory')
            elif not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size > LIMIT:
                raise Refused('expected bounded unlinked private regular file')
        return info

    def read(self, relative):
        path = self.path(relative)
        prior = self.inspect(path)
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as source:
            info = os.fstat(source.fileno())
            if (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
                    info.st_size, info.st_nlink) != (prior.st_dev, prior.st_ino,
                    prior.st_mode, prior.st_uid, prior.st_gid, prior.st_size, prior.st_nlink):
                raise Refused('private file changed while opening')
            content = source.read(LIMIT + 1)
            if len(content) > LIMIT:
                raise Refused('private file grew beyond bound')
        return content, {'sha256': sha(content), 'mode': stat.S_IMODE(info.st_mode),
                         'uid': info.st_uid, 'gid': info.st_gid, 'inode': info.st_ino,
                         'device': info.st_dev, 'size': len(content)}

    def names(self, relative):
        path = self.path(relative)
        self.inspect(path, directory=True)
        names = set(os.listdir(path))
        if len(names) > 256:
            raise Refused('private directory exceeds bound')
        return names

    @contextlib.contextmanager
    def lock(self):
        # Open the SAME existing inode. Never create, truncate or unlink it.
        self.read(LOCK)
        original = self.inspect(self.path(LOCK))
        fd = os.open(self.path(LOCK), os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            actual = os.fstat(fd)
            if ((actual.st_dev, actual.st_ino, actual.st_mode, actual.st_uid, actual.st_gid, actual.st_nlink)
                    != (original.st_dev, original.st_ino, original.st_mode, original.st_uid,
                        original.st_gid, original.st_nlink)):
                raise Refused('activation lock replaced')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as exc:
                raise Refused('activation lock busy; no changes made') from exc
            yield
            if self.path(LOCK).lstat().st_ino != original.st_ino:
                raise Refused('activation lock replaced during operation')
        finally:
            os.close(fd)

    def snapshot(self, *, _pending_files=None):
        manifest_bytes, manifest_info = self.read(MANIFEST)
        manifest = decode(manifest_bytes)
        if not isinstance(manifest, dict) or set(manifest) != {'files'}:
            raise Refused('invalid active manifest')
        files = manifest['files']
        if (not isinstance(files, dict) or 'base.yaml' not in files or len(files) > 256
                or any(not isinstance(k, str) or (k != 'base.yaml' and not DAG_FILE.fullmatch(k))
                       or not isinstance(v, str) or not DIGEST.fullmatch(v) for k, v in files.items())):
            raise Refused('invalid active manifest entries')
        if _pending_files is not None:
            files = _pending_files
        names = self.names(CONFIG + '/dags')
        wiki = 'wiki' in names
        if wiki:
            if self.names(CONFIG + '/dags/wiki'):
                raise Refused('wiki directory is not empty')
            names.remove('wiki')
        if {'dags/' + name for name in names} != set(files) - {'base.yaml'}:
            raise Refused('actual DAG set differs from active manifest')
        protected = {}
        for relative in (*PROTECTED, LOCK, *(CONFIG + '/' + name for name in sorted(files))):
            content, protected[relative] = self.read(relative)
            if relative.startswith(CONFIG + '/') and relative[len(CONFIG) + 1:] in files:
                if sha(content) != files[relative[len(CONFIG) + 1:]]:
                    raise Refused('active definition digest differs')
        if protected[CONFIG + '/config.yaml']['sha256'] != CONFIG_SHA256:
            raise Refused('controller policy configuration differs from reviewed boundary')
        if protected[PROTECTED[1]]['sha256'] != HARDENER_SHA256:
            raise Refused('canonical hardener differs from reviewed guard')
        holds = {}
        for name in sorted(self.names(HOLDS)):
            if name != '.suspend.lock' and not HOLD_FILE.fullmatch(name):
                raise Refused('unexpected hold-directory entry')
            content, holds[name] = self.read(HOLDS + '/' + name)
            if content:
                raise Refused('hold file is not empty')
        if not OLD_HOLDS.issubset(holds):
            raise Refused('retained hold missing')
        return {'schema': 1, 'home': str(self.home), 'uid': self.uid, 'gid': self.gid,
                'manifest': manifest, 'manifest_info': manifest_info,
                'protected': protected, 'holds': holds, 'wiki': wiki}

    def reviewed(self, baseline, digest_file, bundle):
        data, _ = self.read(baseline)
        digest_bytes, _ = self.read(digest_file)
        if not re.fullmatch(rb'[0-9a-f]{64}\n?', digest_bytes) or sha(data) != digest_bytes.decode().strip():
            raise Refused('reviewed baseline SHA256 differs')
        value = decode(data)
        # Structural validation is by equality with a freshly constructed
        # snapshot, not by trusting paths or operations from supplied JSON.
        if not isinstance(value, dict) or value.get('schema') != 1:
            raise Refused('unsupported reviewed baseline')
        bundle = Path(bundle)
        expected = definitions()
        if self.names(bundle) != set(expected) | {'admit-controller.py'}:
            raise Refused('bundle file set differs from fixed RobotGo payload')
        for name, content in expected.items():
            if self.read(bundle / name)[0] != content:
                raise Refused('bundle DAG differs from fixed definition')
        if self.read(bundle / 'admit-controller.py')[0] != Path(__file__).read_bytes():
            raise Refused('bundle helper differs from running reviewed helper')
        return value, sha(data)

    def check(self, baseline, released=False, manifest_installed=True):
        # Construct expected state from a trusted snapshot shape; all original
        # entries and fingerprints remain exact. No fixed legacy count assumed.
        expected = decode(encoded(baseline))
        expected_files = dict(expected['manifest']['files'])
        for name, content in definitions().items():
            key = 'dags/' + name
            if key in expected_files:
                raise Refused('RobotGo definition already present in baseline')
            expected_files[key] = sha(content)
        current = self.snapshot_for_additions(expected_files, manifest_installed)
        for name, content in definitions().items():
            relative = CONFIG + '/dags/' + name
            new_info = current['protected'].pop(relative)
            if new_info['sha256'] != sha(content) or new_info['mode'] != 0o600:
                raise Refused('added definition digest or mode differs')
            flag = name.removesuffix('.yaml') + '.suspend'
            if released:
                if flag in current['holds']:
                    raise Refused('RobotGo hold still present')
            else:
                if (flag not in current['holds'] or current['holds'][flag]['mode'] != 0o600
                        or self.read(HOLDS + '/' + flag)[0] != b''):
                    raise Refused('RobotGo hold missing')
                current['holds'].pop(flag)
        if manifest_installed:
            if current['manifest_info']['mode'] != 0o600:
                raise Refused('extended manifest is not private mode0600')
            current['manifest'] = baseline['manifest']
            current['manifest_info'] = baseline['manifest_info']
        if current != expected:
            raise Refused('retained snapshot or exact added state differs')

    def snapshot_for_additions(self, expected_files, manifest_installed):
        # snapshot() checks actual DAGs against the active manifest. Before the
        # final atomic replacement only, use the exact intended manifest in
        # memory while still reading/fingerprinting the old manifest on disk.
        if manifest_installed:
            current = self.snapshot()
            if current['manifest'] != {'files': expected_files}:
                raise Refused('extended active manifest differs')
            if self.read(MANIFEST)[0] != encoded({'files': expected_files}):
                raise Refused('extended manifest bytes differ')
            return current
        return self.snapshot(_pending_files=expected_files)

    def create(self, relative, data):
        path = self.path(relative)
        self.inspect(path.parent, directory=True)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as target:
            target.write(data)
            target.flush()
            os.fsync(target.fileno())
        self.sync_dir(path.parent)

    @staticmethod
    def sync_dir(path):
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    def progress(self, phase):
        temp = PENDING + '/progress.next'
        self.create(temp, encoded({'phase': phase}))
        os.replace(self.path(temp), self.path(PENDING + '/progress.json'))
        self.sync_dir(self.path(PENDING))

    def admit(self, baseline, baseline_sha256, apply=False):
        # Call only while holding lock(). Never merge a stale snapshot.
        if self.snapshot() != baseline:
            raise Refused('stale baseline; review a fresh snapshot')
        if self.path(PENDING).exists() or self.path(PENDING).is_symlink():
            raise Refused('pending journal exists; explicit reconciliation required')
        additions = definitions()
        for name in additions:
            if ('dags/' + name in baseline['manifest']['files']
                    or name.removesuffix('.yaml') + '.suspend' in baseline['holds']):
                raise Refused('RobotGo admission overlaps existing state')
        if not apply:
            return {'mode': 'preview', 'baseline_sha256': baseline_sha256,
                    'additions': sorted(additions), 'holds_released': False}
        self.inspect(self.path(STATE), directory=True)
        os.mkdir(self.path(PENDING), 0o700)
        self.sync_dir(self.path(STATE))
        self.create(PENDING + '/intent.json', encoded({
            'baseline_sha256': baseline_sha256, 'baseline': baseline,
            'new_files': {name: sha(content) for name, content in additions.items()},
            'holds_released': False,
        }))
        self.progress('intent-recorded')
        for name in additions:
            self.create(HOLDS + '/' + name.removesuffix('.yaml') + '.suspend', b'')
        self.progress('all-new-holds-durable')
        for name, content in additions.items():
            self.create(CONFIG + '/dags/' + name, content)
        self.progress('definitions-durable')
        self.check(baseline, manifest_installed=False)
        manifest = dict(baseline['manifest']['files'])
        manifest.update({'dags/' + name: sha(data) for name, data in additions.items()})
        # Prepare bytes privately, check retained state once more, then commit
        # only active-manifest.json. Failed writes leave the journal and holds.
        temp = PENDING + '/extended-manifest.json'
        self.create(temp, encoded({'files': manifest}))
        self.check(baseline, manifest_installed=False)
        os.replace(self.path(temp), self.path(MANIFEST))
        self.sync_dir(self.path(STATE))
        self.check(baseline)
        self.progress('complete-held')
        self.create(PENDING + '/complete.json', encoded({'baseline_sha256': baseline_sha256,
                                                       'state': 'complete-held'}))
        return {'mode': 'applied', 'manifest_entries': len(manifest), 'holds_released': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--snapshot', action='store_true')
    mode.add_argument('--apply', action='store_true')
    mode.add_argument('--check', action='store_true')
    parser.add_argument('--released', action='store_true')
    parser.add_argument('--baseline', type=Path)
    parser.add_argument('--baseline-sha256-file', type=Path)
    parser.add_argument('--bundle', type=Path)
    args = parser.parse_args()
    if (os.getuid() != UID or os.geteuid() != UID or os.getgid() != GID
            or os.getegid() != GID or socket.gethostname().split('.')[0] != 'vps-ac45f3e2'):
        raise Refused('fixed OVH controller identity required')
    if args.released and not args.check:
        parser.error('--released requires read-only --check')
    if not args.snapshot and not all((args.baseline, args.baseline_sha256_file, args.bundle)):
        parser.error('reviewed baseline, SHA256 file and fixed bundle required')
    os.umask(0o077)
    controller = Controller()
    with controller.lock():
        if args.snapshot:
            result = controller.snapshot()
        else:
            baseline, digest = controller.reviewed(args.baseline, args.baseline_sha256_file, args.bundle)
            if args.check:
                controller.check(baseline, args.released)
                result = {'mode': 'checked', 'released': args.released, 'baseline_sha256': digest}
            else:
                result = controller.admit(baseline, digest, args.apply)
    sys.stdout.buffer.write(encoded(result))


if __name__ == '__main__':
    try:
        main()
    except (Refused, OSError, KeyError, TypeError) as error:
        # OSError filenames and JSON parser errors may reveal private paths or
        # bytes. Only deliberately authored Refused messages are public.
        print('REFUSED: ' + (str(error) if isinstance(error, Refused) else 'private state/I/O check failed'), file=sys.stderr)
        sys.exit(1)
