#!/usr/bin/python3 -I
"""Reviewed two-file RobotGo sealed-worker upgrade, preview by default.

Run only through trusted SSH as OVH root. Both --bundle and --baseline-bundle
must be private root-owned three-file profiles under /var/lib/izoone-ci-provision.
Their independent --expect-profile-sha256 / --expect-baseline-sha256 arguments
bind the reviewed bytes; bundle Python is parsed, NEVER imported or executed.

Acquire the existing controller activation lock, then existing runner.lock,
nonblocking without creating/unlinking either. Verify installed code exactly
matches baseline and verify the new immutable image in rootless worker Podman.
Preview writes nothing. Apply preserves prior public code and intent privately
before staging worker-owned0700 files. Publish driver first, launcher last: the
old sealed launcher refuses the new driver during the short publication gap.

No services, credentials, DAGs, holds, history, other projects or image contents
are changed. A pending journal (including a completed one) is never adopted,
deleted or retried. Failure retains partial files for explicit reconciliation;
there is no automatic rollback over potentially concurrent changes.

An explicitly reviewed --previous-journal enables a successor ONLY after that
own journal completed and proves the currently reviewed baseline profile/files.
It is preserved byte-for-byte. The successor journal name is derived solely
from the new full profile SHA256; an existing successor namespace is refused.
Omitting this option retains the original first-upgrade behavior unchanged.

--reconcile-normalized-baseline SHA256 is a separate, explicit one-time bridge
from the exact reviewed central account-normalization baseline. Its digest binds
both fixed completed pre-normalization journals, including file/directory
fingerprints. They remain historical, never adopted or rewritten. The image is
unchanged, and the new journal is derived from the new full profile SHA256.
Preview and apply both refuse incomplete, changed or unreviewed historical state.
Any other own upgrade-journal namespace blocks this one-time bridge, including
an abandoned attempt for a different candidate profile.
"""

import argparse
import ast
from contextlib import contextmanager
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
import sys


HOME = '/home/izoone_workers'
UID, GID = 7051508, 1004
CONTROLLER = '/home/izoone_automation'
CONTROLLER_UID, CONTROLLER_GID = 7047744, 1003
ROOT = '/var/lib/izoone-ci-provision'
PENDING = ROOT + '/robotgo-worker-upgrade.pending'
STATE = HOME + '/.local/state/robotgo-ovh'
LIBEXEC = HOME + '/.local/libexec'
GLOBAL_LOCK = CONTROLLER + '/.local/state/dagu-boundary/native-activation.lock'
RUNNER_LOCK = STATE + '/runner.lock'
NAMES = ('robotgo-ci', 'robotgo-ci.py')
HASH = re.compile(r'[0-9a-f]{64}')
IMAGE = re.compile(r'sha256:[0-9a-f]{64}')
LIMIT = 16 * 1024 * 1024
NORMALIZED_BASELINE_SHA256 = 'a211836dcc11c73b8299844fbd74f6257fba67e821b762e9759c4bb15686687b'
NORMALIZED_IMAGE = 'sha256:4fb1dd6ef7bc347b9713c3fd89ee6801f8d47b2acc07378063f1420491d05f62'
HISTORICAL_PROFILE_SHA256 = '08efbae7cc1475546edf28c4838ac1b48beabcf2058bdf7ac3cf4defa4bad5be'
JOURNAL_FILES = {'intent.json', 'progress.json', 'complete.json', 'new-profile.json',
                 'prior-profile.json', 'prior-robotgo-ci', 'prior-robotgo-ci.py'}


class Refused(ValueError):
    """Only deliberately nonsecret diagnostics may reach stdout/stderr."""


def sha(data):
    return hashlib.sha256(data).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True) + '\n').encode()


def decode(data):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise Refused('duplicate profile key')
            result[key] = value
        return result
    try:
        return json.loads(data, object_pairs_hook=unique)
    except (UnicodeError, ValueError) as error:
        raise Refused('invalid profile JSON') from error


def static_elf(data):
    if len(data) < 64 or data[:6] != b'\x7fELF\x02\x01':
        raise Refused('static little-endian ELF64 launcher required')
    machine = struct.unpack_from('<H', data, 18)[0]
    offset = struct.unpack_from('<Q', data, 32)[0]
    size, count = struct.unpack_from('<HH', data, 54)
    if machine != 62 or size < 56 or not 1 <= count <= 100 or offset + size * count > len(data):
        raise Refused('static x86_64 ELF program headers required')
    if any(struct.unpack_from('<I', data, offset + index * size)[0] == 3 for index in range(count)):
        raise Refused('dynamic launcher interpreter refused')


def verify_profile(raw, files):
    profile = decode(raw)
    keys = {'schema_version', 'owner', 'uid', 'gid', 'home', 'image', 'go_version',
            'installed', 'source_sha256', 'files'}
    if (not isinstance(profile, dict) or set(profile) != keys or profile['schema_version'] != 1
            or profile['owner'] != 'izoone_workers' or profile['uid'] != UID
            or profile['gid'] != GID or profile['home'] != HOME
            or profile['go_version'] != 'go1.26.10' or profile['installed'] is not False
            or not isinstance(profile['image'], str) or not IMAGE.fullmatch(profile['image'])
            or not isinstance(profile['files'], dict) or set(profile['files']) != set(NAMES)
            or not isinstance(profile['source_sha256'], dict)
            or set(profile['source_sha256']) != {'runner.py', 'launcher.go'}
            or any(not isinstance(value, str) or not HASH.fullmatch(value)
                   for value in (*profile['files'].values(), *profile['source_sha256'].values()))):
        raise Refused('fixed immutable sealed profile required')
    if any(sha(content) != profile['files'][name] for name, content in files.items()):
        raise Refused('profile code hash differs')
    static_elf(files['robotgo-ci'])
    # The reviewed builder seals this exact driver hash into the static binary.
    if profile['files']['robotgo-ci.py'].encode() not in files['robotgo-ci']:
        raise Refused('launcher does not contain reviewed driver seal')
    try:
        tree = ast.parse(files['robotgo-ci.py'])
        images = [node.value.value for node in tree.body if isinstance(node, ast.Assign)
                  and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name)
                  and node.targets[0].id == 'IMAGE' and isinstance(node.value, ast.Constant)]
        stores = [node for node in ast.walk(tree) if isinstance(node, ast.Name)
                  and node.id == 'IMAGE' and isinstance(node.ctx, ast.Store)]
    except (SyntaxError, ValueError) as error:
        raise Refused('sealed driver is not valid Python source') from error
    if images != [profile['image']] or len(stores) != 1:
        raise Refused('driver image seal differs from immutable profile')
    return profile


class Upgrade:
    """Fixture root/identities are library-only; production CLI is fixed."""

    def __init__(self, root=Path('/'), root_owner=(0, 0), worker_owner=(UID, GID),
                 controller_owner=(CONTROLLER_UID, CONTROLLER_GID)):
        self.root = Path(root)
        self.root_owner, self.worker_owner, self.controller_owner = root_owner, worker_owner, controller_owner

    def path(self, absolute):
        candidate = Path(absolute)
        if not candidate.is_absolute() or '..' in candidate.parts:
            raise Refused('safe absolute upgrade path required')
        return self.root / str(candidate).lstrip('/')

    def inspect(self, path, owner, modes, directory=False):
        path = Path(path)
        if '..' in path.parts or not path.is_relative_to(self.root):
            raise Refused('upgrade path outside trusted boundary')
        chain = list(reversed([p for p in path.parents if p.is_relative_to(self.root)])) + [path]
        for item in chain:
            info = item.lstat()
            if item != path:
                if (not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o022
                        or (info.st_uid, info.st_gid) not in (self.root_owner, owner)):
                    raise Refused('unsafe upgrade directory ancestor')
            elif ((info.st_uid, info.st_gid) != owner or stat.S_IMODE(info.st_mode) not in modes
                    or not (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
                    or (not directory and (info.st_nlink != 1 or info.st_size > LIMIT))):
                raise Refused('unsafe upgrade file ownership, mode or links')
        return info

    @staticmethod
    def metadata(info):
        return {'device': info.st_dev, 'inode': info.st_ino, 'mode': stat.S_IMODE(info.st_mode),
                'uid': info.st_uid, 'gid': info.st_gid, 'size': info.st_size,
                'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}

    def read(self, path, owner, modes):
        prior = self.inspect(path, owner, modes)
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as incoming:
            info = os.fstat(incoming.fileno())
            if self.metadata(info) != self.metadata(prior) or info.st_nlink != 1:
                raise Refused('upgrade file changed while opening')
            data = incoming.read(LIMIT + 1)
            if (len(data) > LIMIT or len(data) != info.st_size
                    or self.metadata(os.fstat(incoming.fileno())) != self.metadata(info)):
                raise Refused('upgrade file changed while reading')
        return data, self.metadata(info)

    def bundle(self, bundle, expected):
        bundle = Path(bundle)
        boundary = self.path(ROOT)
        if (not isinstance(expected, str) or not HASH.fullmatch(expected) or not bundle.is_absolute()
                or '..' in bundle.parts or not bundle.is_relative_to(boundary) or bundle == boundary):
            raise Refused('reviewed SHA256 and root-private profile path required')
        # Every staging component below the provisioning root stays private.
        for path in (boundary, *reversed([p for p in bundle.parents if p.is_relative_to(boundary) and p != boundary]), bundle):
            self.inspect(path, self.root_owner, {0o700}, directory=True)
        if set(os.listdir(bundle)) != {*NAMES, 'profile.json'}:
            raise Refused('exact three-file sealed bundle required')
        raw, profile_info = self.read(bundle / 'profile.json', self.root_owner, {0o600})
        if sha(raw) != expected:
            raise Refused('profile digest differs from review')
        observed = {name: self.read(bundle / name, self.root_owner, {0o600, 0o700}) for name in NAMES}
        files = {name: record[0] for name, record in observed.items()}
        profile = verify_profile(raw, files)
        return profile, files, {'profile.json': (raw, profile_info), **observed}

    @contextmanager
    def lock(self, absolute, owner):
        path = self.path(absolute)
        anchor = self.path(CONTROLLER if absolute == GLOBAL_LOCK else HOME)
        for parent in reversed(path.parents):
            if parent.is_relative_to(anchor):
                self.inspect(parent, owner, {0o700}, directory=True)
        raw, metadata = self.read(path, owner, {0o600})
        if raw:
            raise Refused('persistent upgrade lock must be empty')
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            if self.metadata(os.fstat(fd)) != metadata:
                raise Refused('persistent upgrade lock changed')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise Refused('persistent upgrade lock busy; no changes made') from error
            yield metadata
            if self.read(path, owner, {0o600}) != (raw, metadata):
                raise Refused('persistent upgrade lock replaced')
        finally:
            os.close(fd)

    def worker_code(self):
        for absolute in (HOME, HOME + '/.local', LIBEXEC, HOME + '/.local/state', STATE):
            self.inspect(self.path(absolute), self.worker_owner, {0o700}, directory=True)
        return {name: self.read(self.path(LIBEXEC + '/' + name), self.worker_owner, {0o700}) for name in NAMES}

    def verify_image(self, image):
        result = subprocess.run(['/usr/bin/podman', '--remote=false', 'image', 'inspect', '--format', '{{.Id}}', image],
                                user=UID, group=GID, extra_groups=[], cwd='/',
                                env={'HOME': HOME, 'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8',
                                     'XDG_RUNTIME_DIR': '/run/user/7051508'},
                                capture_output=True, timeout=30)
        actual = result.stdout.decode('ascii').strip().removeprefix('sha256:')
        if result.returncode or not HASH.fullmatch(actual) or actual != image.removeprefix('sha256:'):
            raise Refused('reviewed immutable rootless image unavailable')

    @staticmethod
    def sync_dir(path):
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    def create(self, path, data, owner, mode):
        self.inspect(path.parent, owner, {0o700}, directory=True)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as output:
            os.fchown(output.fileno(), *owner)
            os.fchmod(output.fileno(), mode)
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        self.sync_dir(path.parent)
        return self.read(path, owner, {mode})

    def progress(self, phase, pending=None):
        pending = self.path(PENDING) if pending is None else pending
        self.create(pending / 'progress.next', encoded({'phase': phase}), self.root_owner, 0o600)
        os.replace(pending / 'progress.next', pending / 'progress.json')
        self.sync_dir(pending)

    def predecessor(self, path, old_sha, old_profile, old_files):
        """Validate one completed own predecessor; never create or repair it."""
        path = Path(path)
        original = self.path(PENDING)
        derived = self.path(ROOT + '/robotgo-worker-upgrade-' + old_sha + '.pending')
        if path not in (original, derived) or '..' in path.parts or not path.is_absolute():
            raise Refused('previous journal must be exact own baseline-profile namespace')
        self.inspect(path, self.root_owner, {0o700}, directory=True)
        names = JOURNAL_FILES
        if set(os.listdir(path)) != names:
            raise Refused('previous journal is incomplete or has unexpected state')
        records = {name: self.read(path / name, self.root_owner, {0o600}) for name in sorted(names)}
        complete = decode(records['complete.json'][0])
        intent = decode(records['intent.json'][0])
        progress = decode(records['progress.json'][0])
        expected = {'mode': 'apply', 'profile_sha256': old_sha, 'files': old_profile['files'],
                    'image': old_profile['image'],
                    'auth_services_dags_holds_history_and_other_projects_untouched': True}
        if (not isinstance(complete, dict) or not isinstance(intent, dict)
                or any(complete.get(key) != value or intent.get(key) != value for key, value in expected.items())
                or not isinstance(complete.get('baseline_sha256'), str)
                or not HASH.fullmatch(complete['baseline_sha256'])
                or intent.get('baseline_sha256') != complete['baseline_sha256']
                or progress != {'phase': 'complete'}
                or sha(records['new-profile.json'][0]) != old_sha
                or sha(records['prior-profile.json'][0]) != complete['baseline_sha256']):
            raise Refused('previous completed receipt does not match reviewed baseline')
        if verify_profile(records['new-profile.json'][0], old_files) != old_profile:
            raise Refused('previous profile differs from reviewed baseline')
        prior_files = {name: records['prior-' + name][0] for name in NAMES}
        prior_profile = verify_profile(records['prior-profile.json'][0], prior_files)
        if intent.get('baseline_files') != prior_profile['files']:
            raise Refused('previous intent and prior public code differ')
        return path, records

    def historical_journals(self):
        """Read only the two completed own journals preceding central rename.

        Return a canonical content/metadata snapshot for independent review.
        Historical profiles are inspected as data, never admitted as executable
        bundles or substituted for the exact normalized live baseline.
        """
        paths = (self.path(PENDING), self.path(ROOT + '/robotgo-worker-upgrade-' +
                                              HISTORICAL_PROFILE_SHA256 + '.pending'))
        snapshot = {}
        previous = None
        for index, path in enumerate(paths):
            directory = self.metadata(self.inspect(path, self.root_owner, {0o700}, directory=True))
            if set(os.listdir(path)) != JOURNAL_FILES:
                raise Refused('historical journal is incomplete or has unexpected state')
            records = {name: self.read(path / name, self.root_owner, {0o600}) for name in sorted(JOURNAL_FILES)}
            complete, intent = (decode(records[name][0]) for name in ('complete.json', 'intent.json'))
            profile, prior = (decode(records[name][0]) for name in ('new-profile.json', 'prior-profile.json'))
            for value in (profile, prior):
                if (not isinstance(value, dict) or value.get('owner') != 'izoone_workers_vps16776'
                        or value.get('home') != '/home/izoone_workers_vps16776'
                        or value.get('uid') != UID or value.get('gid') != GID
                        or value.get('schema_version') != 1 or value.get('go_version') != 'go1.26.10'
                        or value.get('installed') is not False
                        or not isinstance(value.get('image'), str) or not IMAGE.fullmatch(value['image'])
                        or not isinstance(value.get('files'), dict) or set(value['files']) != set(NAMES)
                        or any(not isinstance(digest, str) or not HASH.fullmatch(digest)
                               for digest in value['files'].values())):
                    raise Refused('historical journal profile identity or seals differ')
            expected = {'mode': 'apply', 'profile_sha256': sha(records['new-profile.json'][0]),
                        'baseline_sha256': sha(records['prior-profile.json'][0]),
                        'files': profile['files'], 'image': profile['image'],
                        'auth_services_dags_holds_history_and_other_projects_untouched': True}
            if index:
                expected.update(previous_journal=str(paths[0]), journal=str(path),
                                previous_complete_sha256=sha(previous['complete.json'][0]))
                if records['prior-profile.json'][0] != previous['new-profile.json'][0]:
                    raise Refused('historical journal profile chain differs')
            if (not isinstance(complete, dict) or complete != expected
                    or not isinstance(intent, dict) or set(intent) != set(expected) | {'baseline_files', 'installed_metadata', 'locks'}
                    or any(intent[key] != value for key, value in expected.items())
                    or intent['baseline_files'] != prior['files']
                    or any(sha(records['prior-' + name][0]) != prior['files'][name] for name in NAMES)
                    or decode(records['progress.json'][0]) != {'phase': 'complete'}):
                raise Refused('historical journal is incomplete or receipt seals differ')
            if self.metadata(self.inspect(path, self.root_owner, {0o700}, directory=True)) != directory:
                raise Refused('historical journal changed during validation')
            snapshot[path.name] = {'directory': directory,
                                   'files': {name: dict(info, sha256=sha(data))
                                             for name, (data, info) in records.items()}}
            previous = records
        if (sha(previous['new-profile.json'][0]) != HISTORICAL_PROFILE_SHA256
                or profile['image'] != NORMALIZED_IMAGE):
            raise Refused('historical final profile or unchanged image differs')
        return snapshot

    def reconciliation_namespaces(self, own_journal=None):
        """Account only for own upgrade namespaces in the root-private store."""
        allowed = {Path(PENDING).name,
                   'robotgo-worker-upgrade-' + HISTORICAL_PROFILE_SHA256 + '.pending'}
        if own_journal is not None:
            path, expected = own_journal
            allowed.add(path.name)
            current = self.metadata(self.inspect(path, self.root_owner, {0o700}, directory=True))
            # Contents/mtime change as this transaction writes its own journal;
            # directory identity, ownership and private mode must not change.
            if any(current[key] != expected[key] for key in ('device', 'inode', 'mode', 'uid', 'gid')):
                raise Refused('owned reconciliation journal changed; preserve and reconcile')
        for name in os.listdir(self.path(ROOT)):
            if (name.startswith('robotgo-worker-upgrade-') and name.endswith('.pending')
                    and name not in allowed):
                raise Refused('unaccounted own upgrade journal; preserve and reconcile')

    def check_predecessor(self, previous):
        if previous is None:
            return
        path, records = previous
        self.inspect(path, self.root_owner, {0o700}, directory=True)
        if (set(os.listdir(path)) != set(records)
                or any(self.read(path / name, self.root_owner, {0o600}) != record
                       for name, record in records.items())):
            raise Refused('previous journal changed; preserve and reconcile')

    def check(self, expected_code, locks, bundle, new_sha, old_bundle, old_sha, expected_bundles, previous=None,
              historical=None, reconciliation_journal=None):
        if self.worker_code() != expected_code:
            raise Refused('installed worker changed; preserve and reconcile')
        for absolute, owner, metadata in locks:
            if self.read(self.path(absolute), owner, {0o600}) != (b'', metadata):
                raise Refused('persistent lock changed; preserve and reconcile')
        current = self.bundle(bundle, new_sha)[2], self.bundle(old_bundle, old_sha)[2]
        if current != expected_bundles:
            raise Refused('reviewed bundle changed during upgrade')
        self.check_predecessor(previous)
        if historical is not None:
            if self.historical_journals() != historical:
                raise Refused('historical journals changed; preserve and reconcile')
            self.reconciliation_namespaces(reconciliation_journal)

    def upgrade(self, bundle, new_sha, old_bundle, old_sha, apply=False, previous_journal=None,
                reconcile_normalized_baseline=None):
        if reconcile_normalized_baseline is not None and previous_journal is not None:
            raise Refused('normalized reconciliation and previous-journal are mutually exclusive')
        profile, files, new_inputs = self.bundle(bundle, new_sha)
        old_profile, old_files, old_inputs = self.bundle(old_bundle, old_sha)
        if new_sha == old_sha or profile['files'] == old_profile['files']:
            raise Refused('distinct reviewed upgrade required')
        with self.lock(GLOBAL_LOCK, self.controller_owner) as global_info:
            with self.lock(RUNNER_LOCK, self.worker_owner) as runner_info:
                previous = None if previous_journal is None else self.predecessor(previous_journal, old_sha, old_profile, old_files)
                historical = None
                if reconcile_normalized_baseline is not None:
                    if (old_sha != NORMALIZED_BASELINE_SHA256 or old_profile['image'] != NORMALIZED_IMAGE
                            or profile['image'] != NORMALIZED_IMAGE
                            or not isinstance(reconcile_normalized_baseline, str)
                            or not HASH.fullmatch(reconcile_normalized_baseline)):
                        raise Refused('exact normalized baseline and unchanged image required')
                    historical = self.historical_journals()
                    if sha(encoded(historical)) != reconcile_normalized_baseline:
                        raise Refused('historical journals differ from reviewed SHA256')
                pending = self.path(PENDING if previous is None and historical is None else
                                    ROOT + '/robotgo-worker-upgrade-' + new_sha + '.pending')
                if pending.exists() or pending.is_symlink():
                    raise Refused('pending worker upgrade exists; explicit reconciliation required')
                if historical is not None:
                    self.reconciliation_namespaces()
                current = self.worker_code()
                if {name: record[0] for name, record in current.items()} != old_files:
                    raise Refused('installed code does not match reviewed baseline')
                for suffix in ('driver', 'launcher'):
                    if os.path.lexists(self.path(LIBEXEC + '/.robotgo-next-' + suffix)):
                        raise Refused('prepared upgrade file already exists; reconcile')
                locks = ((GLOBAL_LOCK, self.controller_owner, global_info), (RUNNER_LOCK, self.worker_owner, runner_info))
                baseline_bundles = new_inputs, old_inputs
                self.verify_image(profile['image'])
                self.check(current, locks, bundle, new_sha, old_bundle, old_sha, baseline_bundles, previous, historical)
                report = {'mode': 'apply' if apply else 'preview', 'image': profile['image'],
                          'profile_sha256': new_sha, 'baseline_sha256': old_sha, 'files': profile['files'],
                          'auth_services_dags_holds_history_and_other_projects_untouched': True}
                if previous is not None:
                    report['previous_journal'] = str(previous[0])
                    report['previous_complete_sha256'] = sha(previous[1]['complete.json'][0])
                    report['journal'] = str(pending)
                if historical is not None:
                    report.update(historical_journals_sha256=reconcile_normalized_baseline,
                                  historical_journals=sorted(historical), journal=str(pending))
                if not apply:
                    return report
                os.mkdir(pending, 0o700)
                self.sync_dir(pending.parent)
                reconciliation_journal = ((pending, self.metadata(self.inspect(
                    pending, self.root_owner, {0o700}, directory=True))) if historical is not None else None)
                self.create(pending / 'intent.json', encoded({**report,
                            **({'historical_snapshot': historical} if historical is not None else {}),
                            'baseline_files': old_profile['files'],
                            'installed_metadata': {name: record[1] for name, record in current.items()},
                            'locks': {'global': global_info, 'runner': runner_info}}), self.root_owner, 0o600)
                for name in NAMES:
                    self.create(pending / ('prior-' + name), old_files[name], self.root_owner, 0o600)
                self.create(pending / 'prior-profile.json', old_inputs['profile.json'][0], self.root_owner, 0o600)
                self.create(pending / 'new-profile.json', new_inputs['profile.json'][0], self.root_owner, 0o600)
                self.progress('intent-and-prior-public-code-durable', pending)
                staged = {}
                order = (('robotgo-ci.py', 'driver'), ('robotgo-ci', 'launcher'))
                for name, suffix in order:
                    path = self.path(LIBEXEC + '/.robotgo-next-' + suffix)
                    staged[name] = path, self.create(path, files[name], self.worker_owner, 0o700)
                if historical is not None:
                    self.reconciliation_namespaces(reconciliation_journal)
                self.progress('both-candidates-durable', pending)
                for name, suffix in order:
                    self.check(current, locks, bundle, new_sha, old_bundle, old_sha, baseline_bundles, previous,
                               historical, reconciliation_journal)
                    if self.read(staged[name][0], self.worker_owner, {0o700}) != staged[name][1]:
                        raise Refused('staged worker changed; preserve and reconcile')
                    os.replace(staged[name][0], self.path(LIBEXEC + '/' + name))
                    self.sync_dir(self.path(LIBEXEC))
                    published = self.read(self.path(LIBEXEC + '/' + name), self.worker_owner, {0o700})
                    before_meta, after_meta = dict(staged[name][1][1]), dict(published[1])
                    # rename changes ctime; retain every other fingerprint and
                    # exact bytes, then track the new ctime for later checks.
                    before_meta.pop('ctime_ns')
                    after_meta.pop('ctime_ns')
                    if published[0] != files[name] or before_meta != after_meta:
                        raise Refused('published worker differs from prepared file')
                    current[name] = published
                    self.progress(suffix + '-published', pending)
                self.check(current, locks, bundle, new_sha, old_bundle, old_sha, baseline_bundles, previous,
                           historical, reconciliation_journal)
                if {name: record[0] for name, record in current.items()} != files:
                    raise Refused('post-upgrade public code differs')
                self.progress('complete', pending)
                self.create(pending / 'complete.json', encoded(report), self.root_owner, 0o600)
                return report


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--bundle', type=Path, required=True)
    parser.add_argument('--expect-profile-sha256', required=True)
    parser.add_argument('--baseline-bundle', type=Path, required=True)
    parser.add_argument('--expect-baseline-sha256', required=True)
    parser.add_argument('--previous-journal', type=Path,
                        help='explicit completed own predecessor; derives new journal from new full profile SHA256')
    parser.add_argument('--reconcile-normalized-baseline', metavar='HISTORICAL_JOURNALS_SHA256',
                        help='explicit exact normalized-baseline bridge; binds both completed historical journals by reviewed snapshot SHA256')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if (os.getuid(), os.geteuid(), os.getgid(), os.getegid()) != (0, 0, 0, 0) or socket.gethostname() != 'vps-ac45f3e2.vps.ovh.net':
        raise Refused('trusted root on fixed OVH host required')
    account = pwd.getpwuid(UID)
    if (account.pw_name, account.pw_gid, account.pw_dir) != ('izoone_workers', GID, HOME):
        raise Refused('fixed worker account required')
    if Path('/sys/fs/selinux/enforce').read_text().strip() != '1':
        raise Refused('enforcing SELinux required')
    os.umask(0o077)
    report = Upgrade().upgrade(args.bundle, args.expect_profile_sha256,
                               args.baseline_bundle, args.expect_baseline_sha256, args.apply, args.previous_journal,
                               args.reconcile_normalized_baseline)
    sys.stdout.buffer.write(encoded(report))


if __name__ == '__main__':
    try:
        main()
    except (Refused, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('REFUSED: ' + (str(error) if isinstance(error, Refused) else 'private upgrade validation/I/O failed; reconcile journal'), file=sys.stderr)
        sys.exit(1)
