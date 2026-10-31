#!/usr/bin/env python3
"""Fixed-command native Entire pilot: host authentication, credential-free builds."""
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
import pwd
from pathlib import Path
import re
import selectors
import signal
import stat
import subprocess
import sys
import tempfile
import time
import uuid


REMOTE = 'entire://aws-eu-central-1.entire.io/et/marang/robotgo'
IMAGE = '@IMAGE_ID@'  # Offline build-profile.py must seal an immutable local image ID.
BINARIES = {'git': '/usr/bin/git', 'entire': '/usr/local/libexec/izoone-entire/0.12.2/entire', 'podman': '/usr/bin/podman'}
TIMEOUTS = {'podman': 1900, 'entire': 30, 'git': 120, 'cleanup': 15}
TASKS = {
    'ci': ('/opt/robotgo-ci/container-ci.sh', 'ci'),
    'security': ('/opt/robotgo-ci/container-ci.sh', 'security'),
    'dependencies': ('/opt/robotgo-ci/container-ci.sh', 'dependencies'),
}
STATE_DIRECTORY = Path('/home/izoone_workers/.local/state/robotgo-ovh')
MAX_LOG_BYTES = 16 * 1024 * 1024
MAX_CAPTURE_BYTES = 1024 * 1024


class BoundedLog:
    def __init__(self, output):
        self.output = output
        self.remaining = MAX_LOG_BYTES

    def write(self, data):
        kept = data[:self.remaining]
        self.output.write(kept)
        self.remaining -= len(kept)


class Refused(Exception):
    pass


class Cancelled(Refused):
    pass


OWNER = 'izoone_workers'
OWNER_UID, OWNER_GID = 7051508, 1004
OWNER_HOME = '/home/izoone_workers'
AUTH_DIRECTORY = Path(OWNER_HOME) / '.config/entire-photo-roller-ci'
AUTH_TOKEN_FILE = AUTH_DIRECTORY / 'tokens.json'
ENTIRE_BIN_DIRECTORY = '/usr/local/libexec/izoone-entire/0.12.2'
ENTIRE_PINS = {
    'entire': 'dbadcd19ab564ac1f03620d820aafb8ff28469a8fd021dab2a262eefc37013e5',
    'git-remote-entire': 'ba9fee0dbf527bfd4675e266d25c52a4e9f6f557e396134a1219f83debaa13cd',
}


def audit_entire_binaries():
    for name, expected in ENTIRE_PINS.items():
        path = Path(ENTIRE_BIN_DIRECTORY) / name
        for parent in reversed(path.parents):
            info = parent.lstat()
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
                raise Refused('untrusted Entire executable parent')
        info = path.lstat()
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0
                or stat.S_IMODE(info.st_mode) != 0o755 or info.st_nlink != 1):
            raise Refused('untrusted Entire executable')
        if hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            raise Refused('Entire executable differs from the pinned release')


def audit_remote_auth():
    account = pwd.getpwuid(os.getuid())
    if (os.getuid() != OWNER_UID or os.getgid() != OWNER_GID
            or account.pw_name != OWNER or account.pw_dir != OWNER_HOME):
        raise Refused('fixed remote service identity required')
    if Path('/sys/fs/selinux/enforce').read_text().strip() != '1':
        raise Refused('enforcing SELinux required')
    # Metadata only. The wrapper never reads or exports stored token values;
    # the trusted CLI uses its own store after this private-path validation.
    root_owner = Path('/').lstat().st_uid
    for directory in [*reversed(AUTH_DIRECTORY.parents), AUTH_DIRECTORY]:
        info = directory.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in (root_owner, OWNER_UID)
                or info.st_mode & 0o022):
            raise Refused('unsafe authentication directory ancestor')
    info = AUTH_DIRECTORY.lstat()
    if info.st_uid != OWNER_UID or stat.S_IMODE(info.st_mode) != 0o700:
        raise Refused('authentication directory must be private')
    info = AUTH_TOKEN_FILE.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != OWNER_UID
            or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1
            or not 0 < info.st_size <= 1024 * 1024):
        raise Refused('private token store unavailable')
    audit_entire_binaries()


def host_environment():
    audit_remote_auth()
    return {'HOME': OWNER_HOME, 'PATH': ENTIRE_BIN_DIRECTORY + ':/usr/bin:/bin',
            'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
            'XDG_CONFIG_HOME': str(Path(OWNER_HOME) / '.config'),
            'XDG_RUNTIME_DIR': '/run/user/' + str(OWNER_UID),
            'ENTIRE_CONFIG_DIR': str(AUTH_DIRECTORY),
            'ENTIRE_TOKEN_STORE': 'file', 'ENTIRE_TOKEN_STORE_PATH': str(AUTH_TOKEN_FILE),
            'ENTIRE_TELEMETRY_OPTOUT': '1',
            'ENTIRE_CONTEXT': 'eu.auth.entire.io', 'ENTIRE_GIT_NO_LOGIN_FALLBACK': 'true',
            'GIT_TERMINAL_PROMPT': '0',
            'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_CONFIG_SYSTEM': '/dev/null'}


def audit_default_mounts(home):
    # Podman5.8.9 subscriptions uses the first existing mounts file, including
    # an empty rootless override. Validate that precise higher-priority file;
    # do not mutate or consult inert vendor files for this service account.
    if home != Path(OWNER_HOME) or os.getuid() != OWNER_UID:
        raise Refused('fixed rootless service home required')
    root_owner = Path('/').lstat().st_uid
    for directory in [*reversed(home.parents), home]:
        info = directory.lstat()
        sticky = info.st_uid == root_owner and info.st_mode & stat.S_ISVTX
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in (root_owner, OWNER_UID)
                or (info.st_mode & 0o022 and not sticky)):
            raise Refused('unsafe rootless home ancestor')
    for directory in (home, home / '.config', home / '.config/containers'):
        info = directory.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != OWNER_UID
                or stat.S_IMODE(info.st_mode) != 0o700):
            raise Refused('private rootless configuration directories required')
    path = home / '.config/containers/mounts.conf'
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as incoming:
        info = os.fstat(incoming.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != OWNER_UID
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or info.st_size > 65536):
            raise Refused('private regular rootless mounts override required')
        data = incoming.read(65537)
        after = os.fstat(incoming.fileno())
        if (len(data) != info.st_size or len(data) > 65536
                or (info.st_mtime_ns, info.st_ctime_ns) != (after.st_mtime_ns, after.st_ctime_ns)):
            raise Refused('rootless mounts override changed')
    if any(line.strip() and not line.lstrip().startswith('#') for line in data.decode('utf-8').splitlines()):
        raise Refused('automatic rootless mounts are configured')
    # FIPS subscription mounts are supplemental, outside mounts.conf precedence.
    try:
        fips = Path('/proc/sys/crypto/fips_enabled').read_text().strip()
    except FileNotFoundError:
        fips = '0'
    if fips != '0':
        raise Refused('supplemental FIPS mounts are outside this profile')


def podman_environment(run_dir):
    home = Path(pwd.getpwuid(os.getuid()).pw_dir)
    audit_default_mounts(home)
    hooks = run_dir / 'empty-hooks'
    hooks.mkdir(mode=0o700)
    config = run_dir / 'containers.conf'
    config.write_text('[containers]\nenv_host=false\nenv=[]\nmounts=[]\nvolumes=[]\n'
                      '[engine]\nremote=false\nenv=[]\nhooks_dir=[' + json.dumps(str(hooks)) + ']\n')
    os.chmod(config, 0o600)
    # No inherited auth, D-Bus, SSH, proxy, config override, module, remote
    # connection or arbitrary token environment reaches the engine process.
    return {'PATH': '/usr/local/bin:/usr/bin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
            'HOME': str(home), 'XDG_RUNTIME_DIR': f'/run/user/{os.getuid()}',
            'CONTAINERS_CONF': str(config)}


def private_directory(path):
    path = Path(path).expanduser().absolute()
    if ':' in str(path) or '\n' in str(path):
        raise Refused('invalid state directory')
    # Restricted execution environments may map the filesystem root's UID.
    root_owner = Path('/').lstat().st_uid
    # Traverse before creating: parents=True would follow attacker-controlled
    # ancestor symlinks. Root-owned sticky /tmp is the sole writable exception.
    for parent in [*reversed(path.parents), path]:
        try:
            info = parent.lstat()
        except FileNotFoundError:
            try:
                parent.mkdir(mode=0o700)
            except FileExistsError:
                pass
            info = parent.lstat()
        mode = stat.S_IMODE(info.st_mode)
        sticky_root = info.st_uid == root_owner and bool(info.st_mode & stat.S_ISVTX)
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in (root_owner, os.getuid())
                or (mode & 0o022 and not sticky_root)):
            raise Refused('unsafe state directory ancestor')
    info = path.stat()
    if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise Refused('state directory must be owned by the worker and mode 0700')
    return path


@contextlib.contextmanager
def execution_lock(state):
    descriptor = os.open(state / 'runner.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(descriptor)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
            raise Refused('unsafe runner lock')
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise Refused('another local job is running') from None
        yield
    finally:
        os.close(descriptor)


def success_marker(state):
    try:
        descriptor = os.open(state / 'last-ci-success.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except FileNotFoundError:
        return None
    with os.fdopen(descriptor, 'rb') as incoming:
        info = os.fstat(incoming.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or info.st_size > 4096):
            raise Refused('unsafe CI success record')
        record = json.loads(incoming.read(4097))
    if (not isinstance(record, dict) or set(record) != {'commit', 'pipeline'}
            or not isinstance(record['commit'], str) or not re.fullmatch('[0-9a-f]{40}', record['commit'])
            or not isinstance(record['pipeline'], str) or not re.fullmatch('[0-9a-f]{64}', record['pipeline'])):
        raise Refused('invalid CI success record')
    return record


def record_success(state, record):
    success_marker(state)  # Refuse unsafe existing destinations before replacement.
    descriptor, temporary = tempfile.mkstemp(prefix='.ci-success-', dir=state)
    try:
        with os.fdopen(descriptor, 'w') as output:
            json.dump(record, output, sort_keys=True)
            output.write('\n')
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, state / 'last-ci-success.json')
    finally:
        Path(temporary).unlink(missing_ok=True)


def execute(args, run_id, binaries=BINARIES):
    if args.task not in TASKS or not re.fullmatch(r'sha256:[0-9a-f]{64}', IMAGE):
        raise Refused('invalid task or unsealed image')
    if args.if_changed and args.task != 'ci':
        raise Refused('change detection requires current main CI')
    if os.getuid() == 0:
        raise Refused('rootless user required')
    state = private_directory(STATE_DIRECTORY)
    with execution_lock(state):
        return execute_locked(args, run_id, state, binaries)


def execute_locked(args, run_id, state, binaries):
    run_dir = state / run_id
    run_dir.mkdir(mode=0o700)
    environment = host_environment()
    podman_env = None
    with (run_dir / 'job.log').open('xb', buffering=0) as logfile:
        log = BoundedLog(logfile)
        os.chmod(run_dir / 'job.log', 0o600)

        def command(tool, *words, capture=False, cleanup=False, source_modes=False):
            argv = [binaries[tool]]
            if tool == 'git':
                argv += ['-c', 'core.hooksPath=/dev/null', '-c', 'protocol.file.allow=always']
            if tool == 'podman':
                argv += ['--remote=false']
            process = subprocess.Popen([*argv, *map(str, words)],
                                       env=podman_env if tool == 'podman' else environment,
                                       stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, start_new_session=True,
                                       umask=0o022 if source_modes else 0o077)
            captured = bytearray()
            deadline = time.monotonic() + TIMEOUTS['cleanup' if cleanup else tool]
            try:
                with selectors.DefaultSelector() as selector:
                    selector.register(process.stdout, selectors.EVENT_READ, capture)
                    selector.register(process.stderr, selectors.EVENT_READ, False)
                    while selector.get_map():
                        remaining = deadline - time.monotonic()
                        if remaining <= 0:
                            raise Refused('fixed command timed out')
                        for key, _ in selector.select(min(remaining, 0.25)):
                            chunk = os.read(key.fileobj.fileno(), 65536)
                            if not chunk:
                                selector.unregister(key.fileobj)
                                continue
                            log.write(chunk)
                            if key.data:
                                captured.extend(chunk)
                                if len(captured) > MAX_CAPTURE_BYTES:
                                    raise Refused('fixed command output exceeds limit')
                process.wait(timeout=max(0.01, deadline - time.monotonic()))
            except BaseException:
                for sig in (signal.SIGTERM, signal.SIGKILL):
                    try:
                        os.killpg(process.pid, sig)
                    except ProcessLookupError:
                        pass
                    try:
                        process.wait(timeout=1)
                    except subprocess.TimeoutExpired:
                        pass
                raise
            finally:
                process.stdout.close()
                process.stderr.close()
            if process.returncode:
                raise Refused('fixed command failed')
            return captured.decode().strip() if capture else ''

        cid_file = run_dir / 'container.cid'
        container_name = 'robotgo-remote-' + run_id
        started_container = False
        try:
            pipeline = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
            status = json.loads(command('entire', 'auth', 'status', '--json', capture=True))
            if not (status.get('logged_in') is True and status.get('jurisdiction') == 'eu'
                    and status.get('token_source') == 'file ' + str(AUTH_TOKEN_FILE)):
                raise Refused('EU service file-store login unavailable')
            if args.if_changed:
                advertised = command('git', 'ls-remote', '--exit-code', REMOTE, 'refs/heads/main', capture=True).splitlines()
                if len(advertised) != 1:
                    raise Refused('unexpected main advertisement')
                fields = advertised[0].split()
                if len(fields) != 2 or fields[1] != 'refs/heads/main' or not re.fullmatch('[0-9a-f]{40}', fields[0]):
                    raise Refused('invalid main advertisement')
                args.resolved_commit = fields[0]
                if success_marker(state) == {'commit': fields[0], 'pipeline': pipeline}:
                    return 'unchanged'
            objects = run_dir / 'objects.git'
            source = run_dir / 'source'
            command('git', 'init', '--bare', objects)
            command('git', '-C', objects, 'fetch', '--no-tags', '--refmap=', REMOTE,
                    '+refs/heads/main:refs/heads/main')
            main = command('git', '-C', objects, 'rev-parse', 'refs/heads/main^{commit}', capture=True)
            if not re.fullmatch(r'[0-9a-f]{40}', main):
                raise Refused('invalid fetched commit')
            commit = main
            args.resolved_commit = commit
            successful = {'commit': commit, 'pipeline': pipeline}
            if args.if_changed and success_marker(state) == successful:
                return 'unchanged'
            forbidden = command('git', '-C', objects, 'ls-tree', '-r', '--name-only', commit,
                                '--', '.entire', '.codex', '.claude', '.git', '.gitmodules', '.gitattributes', capture=True)
            if forbidden:
                raise Refused('source contains forbidden metadata or external-source paths')
            module = command('git', '-C', objects, 'show', f'{commit}:go.mod', capture=True)
            if re.findall(r'^go ([^\s]+)\s*$', module, re.MULTILINE) != ['1.26.10']:
                raise Refused('source Go version does not match fixed image')
            # Full ancestry is inspected only on the trusted host. The container
            # gets a fresh shallow repository containing this snapshot, no prior
            # trees, owner config, checkpoint refs, or authentication material.
            command('git', 'init', source)
            command('git', '-C', source, 'fetch', '--depth=1', '--no-tags', '--refmap=', objects, commit)
            # Git executable/non-executable modes must survive the worker's
            # private umask. Only checkout gets 022; its enclosing source and
            # .git directories, host state, logs and all other children stay
            # private. Never change the long-lived worker process's umask.
            command('git', '-C', source, 'checkout', '--detach', commit, source_modes=True)
            command('git', '-C', source, 'update-ref', 'refs/remotes/origin/main', commit)
            podman_env = podman_environment(run_dir)
            started_container = True
            command('podman', 'run', '--rm', '--pull=never', '--userns=keep-id',
                    '--cidfile', cid_file, '--name', container_name,
                    '--user', f'{os.getuid()}:{os.getgid()}', '--cpus=2', '--memory=8g',
                    '--pids-limit=256', '--timeout=1800', '--cap-drop=all', '--security-opt=no-new-privileges',
                    '--log-driver=none', '--http-proxy=false', '--image-volume=ignore', '--env-host=false',
                    '--volume', f'{source}:/workspace:rw,Z', '--workdir=/workspace',
                    '--env=HOME=/tmp', '--env=GOTOOLCHAIN=local', '--env=GOWORK=off',
                    '--env=GOENV=off', '--env=GOCACHEPROG=', '--env=GOTELEMETRY=off',
                    '--env=GOCACHE=/tmp/go-build', '--env=GOMODCACHE=/tmp/go-mod',
                    '--env=GIT_CONFIG_COUNT=1', '--env=GIT_CONFIG_KEY_0=safe.directory',
                    '--env=GIT_CONFIG_VALUE_0=/workspace', '--entrypoint=/bin/bash',
                    IMAGE, *TASKS[args.task])
        except Exception as error:
            # All diagnostics stay local. Do not return subprocess errors, paths,
            # output, test data, or auth metadata through Dagu's control plane.
            log.write((type(error).__name__ + ': ' + str(error) + '\n').encode())
            raise
        finally:
            if started_container:
                # Repeated signals cannot interrupt the bounded cleanup. Never
                # enumerate containers or remove anything beyond this unique run.
                handlers = {sig: signal.signal(sig, signal.SIG_IGN)
                            for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
                try:
                    identifier = container_name
                    if cid_file.exists():
                        candidate = cid_file.read_text().strip()
                        if re.fullmatch(r'[0-9a-f]{64}', candidate):
                            identifier = candidate
                    command('podman', 'rm', '--force', '--ignore', identifier, cleanup=True)
                except Exception as error:
                    log.write(('cleanup failed: ' + type(error).__name__ + '\n').encode())
                    raise
                finally:
                    for sig, handler in handlers.items():
                        signal.signal(sig, handler)
        # Only a completed current-main CI with successful container cleanup can
        # suppress a later poll. Ancestor builds and other tasks never seed it.
        if args.task == 'ci' and commit == main:
            record_success(state, successful)
    return 'passed'


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise Refused('invalid arguments')


def main(argv=None, binaries=BINARIES):
    started = time.monotonic()
    run_id = uuid.uuid4().hex
    parser = Parser(description=__doc__, add_help=False, allow_abbrev=False)
    parser.add_argument('--task', required=True, choices=tuple(TASKS))
    parser.add_argument('--if-changed', action='store_true')
    args = None
    status = 'failed'
    def cancel(signum, frame):
        raise Cancelled('worker interrupted')
    handlers = {sig: signal.signal(sig, cancel)
                for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
    try:
        args = parser.parse_args(argv)
        status = execute(args, run_id, binaries)
    except (Exception, KeyboardInterrupt):
        pass
    finally:
        for sig, handler in handlers.items():
            signal.signal(sig, handler)
    summary = {'status': status, 'task': args.task if args and args.task in TASKS else None,
               'commit': getattr(args, 'resolved_commit', None), 'duration_seconds': round(time.monotonic() - started, 3),
               'run_id': run_id}
    print(json.dumps(summary, sort_keys=True))
    return 0 if status in ('passed', 'unchanged') else 1


if __name__ == '__main__':
    sys.exit(main())
