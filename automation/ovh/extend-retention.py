#!/usr/bin/python3 -I
"""Admit RobotGo-only fourteen-day retention with its own hourly budget.

Trusted root/SSH tool; preview is default. Review --pruner-sha256 and
--unit-sha256 and --timer-sha256 against a fresh live baseline. Preview returns deterministic
candidate digests. derive(pruner_bytes, unit_bytes) prepares those public code
and service/timer candidates offline; --apply requires all three candidate files and their
independently reviewed SHA256 arguments. Candidate files must be root-owned
0600 inside a root-owned 0700 directory, without symlink ancestors.

No deletion is performed here. No worker/controller/timer is restarted. The
only service-manager action is the worker user's daemon-reload after create-only
installation. The new timer is NOT enabled or started; activate separately only
after real CI and manual retention proof. The shared pruner, service and timer
retain exact bytes and inodes. Their MAX_RUNS budget and frequency are unchanged.
The copied pruner changes only ROOTS to ('robotgo-ovh',), retaining all existing
guards, age, identity and MAX_RUNS=100. Its independent hourly budget avoids
starvation by other projects. The fixed RobotGo state root and runner.lock
must already exist privately before apply. The existing controller activation
lock serializes this transition with other reviewed project admissions.

Failure keeps /var/lib/izoone-robotgo-retention.pending and any prepared files.
Never remove a pending journal to retry or automatically roll back over other
changes; inspect and reconcile its intent, snapshots and progress explicitly.
"""

import argparse
import ast
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import sys


WORKER_UID, WORKER_GID = 7051508, 1004
CONTROLLER_UID, CONTROLLER_GID = 7047744, 1003
WORKER_HOME = '/home/izoone_workers'
CONTROLLER_HOME = '/home/izoone_automation'
NEW_ROOT = 'robotgo-ovh'
REQUIRED_ROOTS = {'photo-roller-remote-runner', 'position-tracking-dependencies'}
PRUNER = '/usr/local/libexec/izoone-ci/prune-state.py'
UNIT = WORKER_HOME + '/.config/systemd/user/izoone-ci-state-retention.service'
TIMER = WORKER_HOME + '/.config/systemd/user/izoone-ci-state-retention.timer'
OWN_PRUNER = '/usr/local/libexec/izoone-ci/prune-robotgo-state.py'
OWN_UNIT = WORKER_HOME + '/.config/systemd/user/robotgo-ci-state-retention.service'
OWN_TIMER = WORKER_HOME + '/.config/systemd/user/robotgo-ci-state-retention.timer'
LOCK = CONTROLLER_HOME + '/.local/state/dagu-boundary/native-activation.lock'
RUNNER_STATE = WORKER_HOME + '/.local/state/' + NEW_ROOT
RUNNER_LOCK = RUNNER_STATE + '/runner.lock'
PENDING = '/var/lib/izoone-robotgo-retention.pending'
MAX_BYTES = 1024 * 1024
HASH = re.compile(r'[0-9a-f]{64}')
ROOT_NAME = re.compile(r'[a-z0-9][a-z0-9-]{0,63}')


class Refused(ValueError):
    """Deliberately nonsecret operator diagnostic."""


def sha(data):
    return hashlib.sha256(data).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True) + '\n').encode()


def assigned(tree, name):
    matches = [node for node in tree.body if isinstance(node, ast.Assign)
               and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name)
               and node.targets[0].id == name]
    stores = [node for node in ast.walk(tree) if isinstance(node, ast.Name)
              and isinstance(node.ctx, ast.Store) and node.id == name]
    if len(matches) != 1 or len(stores) != 1:
        raise Refused('retention constant must have one simple assignment')
    return matches[0].value


def product(node):
    if isinstance(node, ast.Constant) and type(node.value) is int:
        return node.value
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Mult):
        return product(node.left) * product(node.right)
    raise Refused('retention age expression is not the reviewed fixed product')


def derive(pruner, unit):
    """Return (old_roots, own_pruner, own_unit, own_timer); never run code."""
    if len(pruner) > MAX_BYTES or len(unit) > MAX_BYTES:
        raise Refused('retention candidate input exceeds bound')
    try:
        tree = ast.parse(pruner.decode('utf-8'))
        value = assigned(tree, 'ROOTS')
        if not isinstance(value, ast.Tuple):
            raise Refused('retention ROOTS is not a literal tuple')
        roots = ast.literal_eval(value)
        if (not roots or len(roots) > 64 or len(set(roots)) != len(roots)
                or any(not isinstance(name, str) or not ROOT_NAME.fullmatch(name) for name in roots)
                or not REQUIRED_ROOTS.issubset(roots)):
            raise Refused('unsafe or incomplete retention roots')
        if NEW_ROOT in roots:
            raise Refused('RobotGo retention already present; do not retry blindly')
        if (ast.literal_eval(assigned(tree, 'WORKER_UID')) != WORKER_UID
                or ast.literal_eval(assigned(tree, 'WORKER_GID')) != WORKER_GID
                or product(assigned(tree, 'TTL_SECONDS')) != 14 * 24 * 60 * 60
                or ast.literal_eval(assigned(tree, 'MAX_RUNS')) != 100):
            raise Refused('retention identity, fourteen-day age or budget differs')
        home = assigned(tree, 'WORKER_HOME')
        if (not isinstance(home, ast.Call) or not isinstance(home.func, ast.Name) or home.func.id != 'Path'
                or home.keywords or len(home.args) != 1 or ast.literal_eval(home.args[0]) != WORKER_HOME):
            raise Refused('retention worker home differs')
        lines = pruner.splitlines(keepends=True)
        start = sum(map(len, lines[:value.lineno - 1])) + value.col_offset
        end = sum(map(len, lines[:value.end_lineno - 1])) + value.end_col_offset
        new_pruner = pruner[:start] + repr((NEW_ROOT,)).encode('ascii') + pruner[end:]
        compile(new_pruner, '<reviewed-retention-candidate>', 'exec')
        text = unit.decode('utf-8')
    except (SyntaxError, UnicodeError, TypeError, ValueError) as error:
        if isinstance(error, Refused):
            raise
        raise Refused('unsupported retention source or unit') from error
    section, paths = None, None
    unit_lines = text.splitlines(keepends=True)
    directives = {}
    line_indexes = {}
    for index, line in enumerate(unit_lines):
        clean = line.strip()
        if not clean or clean.startswith(('#', ';')):
            continue
        if clean.endswith('\\'):
            raise Refused('unit continuations are not supported')
        if clean.startswith('['):
            section = clean
            continue
        key, separator, contents = clean.partition('=')
        if not separator:
            raise Refused('unsupported retention unit line')
        if key == 'ReadWritePaths':
            if paths is not None or section != '[Service]' or not line.startswith('ReadWritePaths='):
                raise Refused('ambiguous retention writable-path directive')
            paths = contents.split()
        if (section, key) in directives:
            raise Refused('duplicate retention unit directive')
        directives[section, key] = contents
        line_indexes[section, key] = index
    old_paths = [WORKER_HOME + '/.local/state/' + name for name in roots]
    if paths != old_paths:
        raise Refused('retention unit writable paths do not match exact existing roots')
    required = {
        ('[Unit]', 'ConditionUser'): str(WORKER_UID),
        ('[Service]', 'ExecStart'): '/usr/bin/python3 ' + PRUNER + ' --apply',
        ('[Service]', 'Environment'): 'HOME=' + WORKER_HOME,
        ('[Service]', 'UMask'): '0077', ('[Service]', 'NoNewPrivileges'): 'true',
        ('[Service]', 'PrivateTmp'): 'true', ('[Service]', 'ProtectSystem'): 'strict',
        ('[Service]', 'ProtectHome'): 'read-only', ('[Service]', 'Type'): 'oneshot',
        ('[Service]', 'CPUQuota'): '10%', ('[Service]', 'MemoryMax'): '128M',
        ('[Service]', 'TasksMax'): '16', ('[Service]', 'TimeoutStartSec'): '15min',
    }
    if any(directives.get(key) != val for key, val in required.items()):
        raise Refused('retention unit protected boundary differs')
    changes = {('[Service]', 'ExecStart'): '/usr/bin/python3 ' + OWN_PRUNER + ' --apply',
               ('[Service]', 'ReadWritePaths'): RUNNER_STATE}
    if ('[Unit]', 'Description') in directives:
        changes['[Unit]', 'Description'] = 'Expire private RobotGo CI runs after 14 days'
    for key, contents in changes.items():
        index = line_indexes[key]
        old_line = unit_lines[index]
        newline = old_line[len(old_line.rstrip('\r\n')):]
        unit_lines[index] = key[1] + '=' + contents + newline
    own_timer = (
        '[Unit]\nDescription=Hourly retention of private RobotGo CI runs\n\n'
        '[Timer]\nOnCalendar=hourly\nPersistent=true\nRandomizedDelaySec=5m\n'
        'AccuracySec=1min\nUnit=robotgo-ci-state-retention.service\n\n'
        '[Install]\nWantedBy=timers.target\n').encode()
    return roots, new_pruner, ''.join(unit_lines).encode('utf-8'), own_timer


class Extension:
    """Library-only fixture parameters; production CLI paths are fixed."""

    def __init__(self, root=Path('/'), root_uid=0, root_gid=0,
                 worker_uid=WORKER_UID, worker_gid=WORKER_GID,
                 controller_uid=CONTROLLER_UID, controller_gid=CONTROLLER_GID):
        self.root = Path(root)
        self.root_owner = root_uid, root_gid
        self.worker_owner = worker_uid, worker_gid
        self.controller_owner = controller_uid, controller_gid

    def path(self, absolute):
        value = Path(absolute)
        if not value.is_absolute() or '..' in value.parts:
            raise Refused('absolute safe retention path required')
        return self.root / str(value).lstrip('/')

    def inspect(self, path, owner, mode, directory=False, private_input=False):
        path = Path(path)
        if '..' in path.parts or not path.is_relative_to(self.root):
            raise Refused('retention path outside trusted boundary')
        chain = list(reversed([p for p in path.parents if p.is_relative_to(self.root)])) + [path]
        for item in chain:
            info = item.lstat()
            if item != path:
                if (not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o022
                        or (info.st_uid, info.st_gid) not in (self.root_owner, owner)):
                    raise Refused('unsafe retention path ancestor')
                if private_input and item == path.parent and stat.S_IMODE(info.st_mode) != 0o700:
                    raise Refused('candidate parent must be private root directory')
            elif ((info.st_uid, info.st_gid) != owner or stat.S_IMODE(info.st_mode) != mode
                    or not (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
                    or (not directory and (info.st_nlink != 1 or info.st_size > MAX_BYTES))):
                raise Refused('retention file type, ownership or mode rejected')
        return info

    @staticmethod
    def fingerprint(info, content):
        return {'sha256': sha(content), 'mode': stat.S_IMODE(info.st_mode),
                'uid': info.st_uid, 'gid': info.st_gid, 'inode': info.st_ino, 'device': info.st_dev}

    def read(self, path, owner, mode, private_input=False):
        prior = self.inspect(path, owner, mode, private_input=private_input)
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as source:
            info = os.fstat(source.fileno())
            if (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, info.st_nlink) != (
                    prior.st_dev, prior.st_ino, prior.st_mode, prior.st_uid, prior.st_gid, 1):
                raise Refused('retention input changed while opening')
            data = source.read(MAX_BYTES + 1)
            if len(data) > MAX_BYTES:
                raise Refused('retention input grew beyond bound')
        return data, self.fingerprint(info, data)

    def observed(self):
        return {key: self.read(self.path(path), owner, mode) for key, path, owner, mode in (
            ('pruner', PRUNER, self.root_owner, 0o444),
            ('unit', UNIT, self.worker_owner, 0o600),
            ('timer', TIMER, self.worker_owner, 0o600),
            ('activation_lock', LOCK, self.controller_owner, 0o600))}

    @contextmanager
    def lock(self):
        content, info = self.read(self.path(LOCK), self.controller_owner, 0o600)
        fd = os.open(self.path(LOCK), os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            if self.fingerprint(os.fstat(fd), content) != info:
                raise Refused('activation lock changed')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise Refused('activation lock busy; no changes made') from error
            yield
            if self.read(self.path(LOCK), self.controller_owner, 0o600)[1] != info:
                raise Refused('persistent activation lock changed')
        finally:
            os.close(fd)

    def validate_state(self):
        self.inspect(self.path(RUNNER_STATE), self.worker_owner, 0o700, directory=True)
        data, info = self.read(self.path(RUNNER_LOCK), self.worker_owner, 0o600)
        if data:
            raise Refused('persistent RobotGo runner lock must be empty')
        return info

    def targets(self):
        return (('pruner', OWN_PRUNER, self.root_owner, 0o444),
                ('unit', OWN_UNIT, self.worker_owner, 0o600),
                ('timer', OWN_TIMER, self.worker_owner, 0o600))

    def check(self, expected, runner_info, installed=None):
        if self.observed() != expected or self.validate_state() != runner_info:
            raise Refused('stale retention state; preserve changes and reconcile')
        for key, target, owner, mode in self.targets():
            path = self.path(target)
            if installed is not None and key in installed:
                if self.read(path, owner, mode) != installed[key]:
                    raise Refused('installed RobotGo retention changed; reconcile')
            elif path.exists() or path.is_symlink():
                raise Refused('RobotGo retention target exists; never overwrite')

    @staticmethod
    def sync_dir(path):
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    def create(self, path, content, owner, mode):
        self.inspect(path.parent, owner, stat.S_IMODE(path.parent.lstat().st_mode), directory=True)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as target:
            target.write(content)
            target.flush()
            os.fchown(target.fileno(), *owner)
            os.fchmod(target.fileno(), mode)
            os.fsync(target.fileno())
        self.sync_dir(path.parent)
        return self.read(path, owner, mode)

    def progress(self, phase):
        pending = self.path(PENDING)
        self.create(pending / 'progress.next', encoded({'phase': phase}), self.root_owner, 0o600)
        os.replace(pending / 'progress.next', pending / 'progress.json')
        self.sync_dir(pending)

    def reload_manager(self):
        runtime = '/run/user/' + str(WORKER_UID)
        subprocess.run([
            '/usr/bin/setpriv', '--reuid=' + str(WORKER_UID), '--regid=' + str(WORKER_GID), '--clear-groups',
            '/usr/bin/env', '-i', 'HOME=' + WORKER_HOME, 'PATH=/usr/bin:/bin',
            'XDG_RUNTIME_DIR=' + runtime, 'DBUS_SESSION_BUS_ADDRESS=unix:path=' + runtime + '/bus',
            '/usr/bin/systemctl', '--user', 'daemon-reload',
        ], check=True, capture_output=True, timeout=30)

    def publish(self, source, destination):
        # Hard-link publication is atomic and CREATE-ONLY, unlike os.replace.
        # Unlink only our validated staging name, never an existing target.
        # A crash between link/unlink leaves two links and a pending journal;
        # fail closed for explicit reconciliation rather than retry/rollback.
        os.link(source, destination, follow_symlinks=False)
        self.sync_dir(destination.parent)
        os.unlink(source)
        self.sync_dir(source.parent)

    def extend(self, pruner_sha, unit_sha, timer_sha, candidates=None, apply=False):
        """Caller must hold lock(); no subprocess or write in preview."""
        if any(not isinstance(value, str) or not HASH.fullmatch(value) for value in (pruner_sha, unit_sha, timer_sha)):
            raise Refused('reviewed baseline SHA256 arguments required')
        baseline = self.observed()
        if any(baseline[key][1]['sha256'] != value
               for key, value in (('pruner', pruner_sha), ('unit', unit_sha), ('timer', timer_sha))):
            raise Refused('reviewed baseline digests differ; review fresh state')
        roots, new_pruner, new_unit, new_timer = derive(baseline['pruner'][0], baseline['unit'][0])
        pending = self.path(PENDING)
        if pending.exists() or pending.is_symlink():
            raise Refused('pending retention journal exists; reconcile explicitly')
        for _, target, _, _ in self.targets():
            path = self.path(target)
            if path.exists() or path.is_symlink():
                raise Refused('RobotGo retention target exists; never overwrite')
        expected_candidates = {'pruner': new_pruner, 'unit': new_unit, 'timer': new_timer}
        if candidates is not None:
            for name, expected in expected_candidates.items():
                path, digest = candidates[name]
                if not isinstance(digest, str) or not HASH.fullmatch(digest):
                    raise Refused('reviewed candidate SHA256 argument required')
                actual, _ = self.read(Path(path), self.root_owner, 0o600, private_input=True)
                if sha(actual) != digest or actual != expected:
                    raise Refused('reviewed candidate digest or exact additive bytes differ')
        report = {'mode': 'apply' if apply else 'preview', 'old_roots': list(roots),
                  'own_roots': [NEW_ROOT], 'ttl_days': 14,
                  'own_max_runs_per_invocation': 100, 'own_timer_frequency': 'hourly',
                  'candidate_sha256': {key: sha(val) for key, val in expected_candidates.items()},
                  'shared_timer_sha256': baseline['timer'][1]['sha256'], 'shared_files_unchanged': True,
                  'timer_activated': False, 'pruning_executed': False}
        if not apply:
            return report
        if candidates is None:
            raise Refused('apply requires private reviewed candidate files and SHA256 arguments')
        runner_info = self.validate_state()
        self.inspect(pending.parent, self.root_owner, 0o755, directory=True)
        os.mkdir(pending, 0o700)
        self.sync_dir(pending.parent)
        self.create(pending / 'intent.json', encoded({
            'baseline': {key: info for key, (_, info) in baseline.items()},
            'new_sha256': report['candidate_sha256'], 'runner_lock': runner_info,
            'own_roots': [NEW_ROOT], 'ttl_days': 14, 'reload_only_worker_manager': True,
            'shared_files_untouched': True, 'timer_activated': False,
        }), self.root_owner, 0o600)
        for key in ('pruner', 'unit', 'timer'):
            self.create(pending / ('prior-' + key), baseline[key][0], self.root_owner, 0o600)
        self.progress('intent-and-prior-snapshot-durable')
        staged = {}
        for key, target, owner, mode in self.targets():
            destination = self.path(target)
            temp = destination.with_name('.robotgo-retention-next-' + key)
            staged[key] = temp, self.create(temp, expected_candidates[key], owner, mode)
        self.progress('candidates-durable')
        installed = {}
        self.check(baseline, runner_info, installed)
        for key, target, owner, mode in self.targets():
            self.check(baseline, runner_info, installed)
            if self.read(staged[key][0], owner, mode) != staged[key][1]:
                raise Refused('prepared retention file changed; reconcile journal')
            self.publish(staged[key][0], self.path(target))
            installed[key] = staged[key][1]
            self.progress(key + '-installed')
        self.check(baseline, runner_info, installed)
        self.progress('files-installed-needs-worker-daemon-reload')
        self.reload_manager()
        self.check(baseline, runner_info, installed)
        self.progress('complete-worker-manager-reloaded')
        self.create(pending / 'complete.json', encoded(report), self.root_owner, 0o600)
        return report


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--pruner-sha256', required=True)
    parser.add_argument('--unit-sha256', required=True)
    parser.add_argument('--timer-sha256', required=True)
    parser.add_argument('--candidate-pruner', type=Path)
    parser.add_argument('--candidate-unit', type=Path)
    parser.add_argument('--candidate-timer', type=Path)
    parser.add_argument('--candidate-pruner-sha256')
    parser.add_argument('--candidate-unit-sha256')
    parser.add_argument('--candidate-timer-sha256')
    args = parser.parse_args()
    if (os.getuid(), os.geteuid(), os.getgid(), os.getegid()) != (0, 0, 0, 0) or socket.gethostname() != 'vps-ac45f3e2.vps.ovh.net':
        raise Refused('trusted root on fixed OVH hostname required')
    supplied = (args.candidate_pruner, args.candidate_unit, args.candidate_timer,
                args.candidate_pruner_sha256, args.candidate_unit_sha256, args.candidate_timer_sha256)
    if any(supplied) and not all(supplied):
        parser.error('all three candidate files and all three candidate SHA256 arguments are required together')
    candidates = None if not any(supplied) else {
        'pruner': (args.candidate_pruner, args.candidate_pruner_sha256),
        'unit': (args.candidate_unit, args.candidate_unit_sha256),
        'timer': (args.candidate_timer, args.candidate_timer_sha256),
    }
    os.umask(0o077)
    extension = Extension()
    with extension.lock():
        result = extension.extend(args.pruner_sha256, args.unit_sha256, args.timer_sha256, candidates, args.apply)
    sys.stdout.buffer.write(encoded(result))


if __name__ == '__main__':
    try:
        main()
    except (Refused, OSError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('REFUSED: ' + (str(error) if isinstance(error, Refused) else 'private retention I/O or reload failed; reconcile journal'), file=sys.stderr)
        sys.exit(1)
