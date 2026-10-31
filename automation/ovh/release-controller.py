#!/usr/bin/python3 -I
"""Release exactly four RobotGo holds against fresh reviewed state and proof.

Controller-account trusted-SSH operation; preview is default. --bundle is the
existing private five-file admission bundle. The admission module is pinned
below and to --expect-admission-sha256 BEFORE executing its verified bytes.
--snapshot/--expect-snapshot-sha256 bind a freshly reviewed Controller.snapshot;
--proof/--expect-proof-sha256 bind independently verified completed-run evidence.

Proof is an offline attestation, not an API or worker query: the operator must
independently verify its source-qualified Dagu records, current sealed worker
profile/image and successful-CI marker before reviewing its digest. Exact proof
schema is enforced by validate_proof(). It requires four successful fixed DAGs,
the watch's unchanged result, one commit/profile/image/driver seal, completed
timestamps, unique run IDs and the matching worker success marker.

No definitions, manifest, auth, services, other holds or old admission journals
are changed. Under the existing persistent activation lock, full snapshot
equality is checked before/after each exact unlink. Apply first writes a new
private robotgo-release.pending intent and snapshots; progress is append-only.
Interruption retains the journal and partial release for explicit reconciliation.
Never delete/adopt a pending journal, blindly retry, or recreate old holds.
"""

import argparse
from datetime import datetime, timezone, timedelta
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import sys
import types


HOME = Path('/home/izoone_automation')
UID, GID = 7047744, 1003
ADMISSION_SHA256 = '9776676064c690a27fa28e45fdc502e7a17e6da80e39805e37f497153ba490b2'
REPOSITORY = 'entire://aws-eu-central-1.entire.io/et/marang/robotgo'
DAGU_SOURCE = 'https://automation.izoone.com'
WORKER_SOURCE = 'trusted-ssh://vps-ac45f3e2.vps.ovh.net/uid/7051508'
MARKER_PATH = '.local/state/robotgo-ovh/last-ci-success.json'
TASK_DAGS = {task: 'robotgo-ovh-' + task for task in ('ci', 'security', 'dependencies', 'watch')}
PENDING = '.local/state/dagu-boundary/robotgo-release.pending'
HASH = re.compile(r'[0-9a-f]{64}')
COMMIT = re.compile(r'[0-9a-f]{40}')
RUN_ID = re.compile(r'[A-Za-z0-9][A-Za-z0-9_-]{0,127}')


class Refused(ValueError):
    """Fixed diagnostics, never private bytes or underlying exception text."""


def sha(data):
    return hashlib.sha256(data).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True) + '\n').encode()


def private_module(path, home, uid, gid):
    """Bootstrap without importing any unchecked same-directory Python."""
    path, home = Path(path), Path(home)
    if not path.is_absolute() or '..' in path.parts or not path.is_relative_to(home) or path == home:
        raise Refused('admission module outside private controller home')
    for parent in reversed(home.parents):
        if not stat.S_ISDIR(parent.lstat().st_mode):
            raise Refused('unsafe admission system ancestor')
    chain = [home, *reversed([p for p in path.parents if p.is_relative_to(home) and p != home]), path]
    for item in chain:
        info = item.lstat()
        if ((info.st_uid, info.st_gid) != (uid, gid) or info.st_mode & 0o077
                or not (stat.S_ISREG(info.st_mode) if item == path else stat.S_ISDIR(info.st_mode))):
            raise Refused('unsafe private admission module path')
    if info.st_nlink != 1 or not 0 < info.st_size <= 1024 * 1024:
        raise Refused('unsafe admission module size or links')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        opened = os.fstat(source.fileno())
        if (opened.st_dev, opened.st_ino, opened.st_mode, opened.st_uid, opened.st_gid, opened.st_nlink) != (
                info.st_dev, info.st_ino, info.st_mode, uid, gid, 1):
            raise Refused('admission module changed while opening')
        data = source.read(1024 * 1024 + 1)
    if len(data) != info.st_size:
        raise Refused('admission module changed while reading')
    return data


def load_admission(bundle, expected, home=HOME, uid=UID, gid=GID):
    if expected != ADMISSION_SHA256:
        raise Refused('independently reviewed fixed admission digest required')
    path = Path(bundle) / 'admit-controller.py'
    data = private_module(path, home, uid, gid)
    if sha(data) != expected:
        raise Refused('admission module digest differs')
    module = types.ModuleType('robotgo_sealed_admission')
    module.__file__ = str(path)
    # Verified immutable source bytes, not a second path-based loader read.
    exec(compile(data, str(path), 'exec'), module.__dict__)
    return module


def matches(pattern, value):
    return isinstance(value, str) and pattern.fullmatch(value) is not None


def timestamp(value):
    if not isinstance(value, str) or not value.endswith('Z'):
        raise Refused('completed proof requires UTC timestamps')
    try:
        parsed = datetime.fromisoformat(value[:-1] + '+00:00')
    except ValueError as error:
        raise Refused('invalid completed proof timestamp') from error
    if parsed > datetime.now(timezone.utc) + timedelta(seconds=60):
        raise Refused('completed proof timestamp is in the future')
    return parsed


def validate_proof(proof):
    keys = {'schema_version', 'repository', 'commit', 'profile_sha256', 'image',
            'driver_sha256', 'verified_at', 'runs', 'success_marker'}
    if (not isinstance(proof, dict) or set(proof) != keys or proof['schema_version'] != 1
            or proof['repository'] != REPOSITORY or not matches(COMMIT, proof['commit'])
            or not matches(HASH, proof['profile_sha256']) or not matches(HASH, proof['driver_sha256'])
            or not matches(re.compile(r'sha256:[0-9a-f]{64}'), proof['image'])
            or not isinstance(proof['runs'], dict) or set(proof['runs']) != set(TASK_DAGS)):
        raise Refused('source-qualified complete RobotGo proof required')
    verified_at = timestamp(proof['verified_at'])
    ids, worker_ids = set(), set()
    fields = {'dag', 'dag_run_id', 'dag_status', 'dag_source', 'worker_id', 'worker_run_id',
              'worker_status', 'worker_source', 'commit', 'profile_sha256', 'image', 'completed_at'}
    for task, dag in TASK_DAGS.items():
        record = proof['runs'][task]
        if (not isinstance(record, dict) or set(record) != fields or record['dag'] != dag
                or not matches(RUN_ID, record['dag_run_id']) or record['dag_status'] != 'succeeded'
                or record['dag_source'] != DAGU_SOURCE or record['worker_id'] != 'izoone-ovh-01'
                or not matches(re.compile(r'[0-9a-f]{32}'), record['worker_run_id'])
                or record['worker_status'] != ('unchanged' if task == 'watch' else 'passed')
                or record['worker_source'] != WORKER_SOURCE
                or any(record[key] != proof[key] for key in ('commit', 'profile_sha256', 'image'))):
            raise Refused('completed fixed DAG and worker proof differs')
        if timestamp(record['completed_at']) > verified_at:
            raise Refused('proof verification predates completed run')
        ids.add(record['dag_run_id'])
        worker_ids.add(record['worker_run_id'])
    if len(ids) != 4 or len(worker_ids) != 4:
        raise Refused('proof run identifiers must be distinct')
    marker = proof['success_marker']
    if (not isinstance(marker, dict) or set(marker) != {'source', 'path', 'commit', 'pipeline'}
            or marker['source'] != WORKER_SOURCE or marker['path'] != MARKER_PATH
            or marker['commit'] != proof['commit'] or marker['pipeline'] != proof['driver_sha256']):
        raise Refused('trusted worker successful-CI marker differs')
    return proof


class Release:
    def __init__(self, admission, controller):
        self.a, self.c = admission, controller

    def read_inputs(self, bundle, snapshot, snapshot_sha, proof, proof_sha):
        a, c = self.a, self.c
        expected_names = set(a.definitions()) | {'admit-controller.py'}
        if c.names(bundle) != expected_names:
            raise Refused('exact existing five-file admission bundle required')
        records = {str(Path(bundle) / name): c.read(Path(bundle) / name) for name in sorted(expected_names)}
        if sha(records[str(Path(bundle) / 'admit-controller.py')][0]) != ADMISSION_SHA256:
            raise Refused('sealed admission bundle changed')
        for name, data in a.definitions().items():
            if records[str(Path(bundle) / name)][0] != data:
                raise Refused('fixed RobotGo bundle definition differs')
        for path, digest in ((snapshot, snapshot_sha), (proof, proof_sha)):
            record = c.read(path)
            if not matches(HASH, digest) or sha(record[0]) != digest or record[1]['mode'] != 0o600:
                raise Refused('reviewed snapshot or proof digest/mode differs')
            records[str(path)] = record
        baseline = a.decode(records[str(snapshot)][0])
        evidence = validate_proof(a.decode(records[str(proof)][0]))
        return baseline, evidence, records

    def unchanged_inputs(self, records):
        if any(self.c.read(path) != record for path, record in records.items()):
            raise Refused('reviewed release inputs changed; reconcile')

    def release(self, bundle, snapshot, snapshot_sha, proof, proof_sha, apply=False):
        a, c = self.a, self.c
        with c.lock():
            pending = c.path(PENDING)
            if pending.exists() or pending.is_symlink():
                raise Refused('pending release exists; explicit reconciliation required')
            baseline, evidence, inputs = self.read_inputs(bundle, snapshot, snapshot_sha, proof, proof_sha)
            if c.snapshot() != baseline:
                raise Refused('stale release snapshot; review fresh state')
            flags = []
            for name, content in a.definitions().items():
                relative = a.CONFIG + '/dags/' + name
                flag = name.removesuffix('.yaml') + '.suspend'
                if (baseline['manifest']['files'].get('dags/' + name) != sha(content)
                        or baseline['protected'][relative]['mode'] != 0o600
                        or flag not in baseline['holds'] or baseline['holds'][flag]['mode'] != 0o600
                        or c.read(a.HOLDS + '/' + flag)[0] != b''):
                    raise Refused('exact private RobotGo definition and hold required')
                flags.append(flag)
            report = {'mode': 'apply' if apply else 'preview', 'snapshot_sha256': snapshot_sha,
                      'proof_sha256': proof_sha, 'commit': evidence['commit'],
                      'profile_sha256': evidence['profile_sha256'], 'image': evidence['image'],
                      'release_only': flags, 'other_controller_state_untouched': True}
            if not apply:
                return report
            c.inspect(pending.parent, directory=True)
            os.mkdir(pending, 0o700)
            c.sync_dir(pending.parent)
            c.create(PENDING + '/intent.json', encoded(report))
            c.create(PENDING + '/baseline.json', inputs[str(snapshot)][0])
            c.create(PENDING + '/proof.json', inputs[str(proof)][0])
            c.create(PENDING + '/progress-0.json', encoded({'released': []}))
            expected = a.decode(a.encoded(baseline))
            released = []
            for flag in flags:
                self.unchanged_inputs(inputs)
                if c.snapshot() != expected:
                    raise Refused('controller changed during release; reconcile')
                relative = a.HOLDS + '/' + flag
                if c.read(relative) != (b'', expected['holds'][flag]):
                    raise Refused('RobotGo hold changed before release; reconcile')
                os.unlink(c.path(relative))
                c.sync_dir(c.path(a.HOLDS))
                expected['holds'].pop(flag)
                released.append(flag)
                if c.snapshot() != expected:
                    raise Refused('unexpected controller state after release; reconcile')
                c.create(PENDING + '/progress-' + str(len(released)) + '.json', encoded({'released': released}))
            self.unchanged_inputs(inputs)
            if c.snapshot() != expected:
                raise Refused('post-release snapshot differs; reconcile')
            c.create(PENDING + '/complete.json', encoded(report))
            return report


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--bundle', type=Path, required=True)
    parser.add_argument('--expect-admission-sha256', required=True)
    parser.add_argument('--snapshot', type=Path, required=True)
    parser.add_argument('--expect-snapshot-sha256', required=True)
    parser.add_argument('--proof', type=Path, required=True)
    parser.add_argument('--expect-proof-sha256', required=True)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if (os.getuid(), os.geteuid(), os.getgid(), os.getegid()) != (UID, UID, GID, GID) or socket.gethostname().split('.')[0] != 'vps-ac45f3e2':
        raise Refused('fixed OVH controller identity required')
    os.umask(0o077)
    module = load_admission(args.bundle, args.expect_admission_sha256)
    result = Release(module, module.Controller()).release(args.bundle, args.snapshot, args.expect_snapshot_sha256,
                                                         args.proof, args.expect_proof_sha256, args.apply)
    sys.stdout.buffer.write(encoded(result))


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError) as error:
        print('REFUSED: ' + (str(error) if isinstance(error, Refused) else 'private release check failed; reconcile'), file=sys.stderr)
        sys.exit(1)
