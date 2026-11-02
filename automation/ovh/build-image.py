#!/usr/bin/python3 -I
"""Build only the reviewed RobotGo CI image on OVH; no source or auth mount.

Run as the existing worker, through trusted SSH. The context must contain only
the three reviewed image files. The shared controller lock is held by the
administrator invocation. The resulting local image is never pushed.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import signal
import socket
import stat
import subprocess
import uuid

FILES = ('Containerfile', 'container-ci.sh', 'container-dependencies.sh')
HOME = Path('/home/izoone_workers')
UID, GID = 7051508, 1004


def image_id(value):
    if re.fullmatch('[0-9a-f]{64}', value):
        return 'sha256:' + value
    if re.fullmatch('sha256:[0-9a-f]{64}', value):
        return value
    raise ValueError('invalid built image ID')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--context', type=Path, required=True)
    parser.add_argument('--expected', required=True, help='reviewed JSON map of the three SHA256 values')
    parser.add_argument('--inside-unit', action='store_true', help=argparse.SUPPRESS)
    args = parser.parse_args()
    os.umask(0o077)
    if (os.getuid(), os.geteuid(), os.getgid(), os.getegid()) != (UID, UID, GID, GID):
        raise ValueError('fixed rootless worker required')
    if socket.gethostname() != 'vps-ac45f3e2.vps.ovh.net' or pwd.getpwuid(UID).pw_dir != str(HOME):
        raise ValueError('fixed OVH host required')
    expected = json.loads(args.expected)
    if set(expected) != set(FILES) or any(not re.fullmatch('[0-9a-f]{64}', v) for v in expected.values()):
        raise ValueError('exact reviewed file digests required')
    context = args.context
    if (not context.is_absolute() or '..' in context.parts or context.resolve() != context
            or not context.is_relative_to(HOME / '.local/state/robotgo-image-build')):
        raise ValueError('fixed private build namespace required')
    for parent in (*reversed(context.parents), context):
        info = parent.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid not in (0, UID)
                or info.st_mode & 0o022):
            raise ValueError('unsafe build ancestor')
    for private in (context.parent, context):
        info = private.lstat()
        if info.st_uid != UID or stat.S_IMODE(info.st_mode) != 0o700:
            raise ValueError('worker-owned private build directories required')
    if {p.name for p in context.iterdir()} != set(FILES):
        raise ValueError('source-free three-file context required')
    for name in FILES:
        path = context / name
        info = path.lstat()
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != UID or info.st_nlink != 1
                or stat.S_IMODE(info.st_mode) != 0o600
                or hashlib.sha256(path.read_bytes()).hexdigest() != expected[name]):
            raise ValueError('build file differs from review')
    if not args.inside_unit:
        # Keep the complete image build in ONE transient cgroup. No new timer,
        # persistent unit or change to the Dagu worker/controller is required.
        # Delegation stays inside this resource-limited unit. OCI build children
        # are explicitly placed beneath it, never in sibling user.slice units.
        env = {'HOME': str(HOME), 'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8',
               'XDG_RUNTIME_DIR': '/run/user/7051508',
               'DBUS_SESSION_BUS_ADDRESS': 'unix:path=/run/user/7051508/bus'}
        unit = 'robotgo-image-build-' + uuid.uuid4().hex + '.service'
        command = ['/usr/bin/systemd-run', '--user', '--unit=' + unit, '--pipe',
                   '--wait', '--collect', '--quiet', '--service-type=exec',
                   '--property=CPUQuota=200%', '--property=MemoryMax=8589934592',
                   '--property=CPUQuotaPeriodSec=100ms',
                   '--property=TasksMax=256', '--property=RuntimeMaxSec=1800',
                   '--property=Delegate=yes', '--property=DelegateSubgroup=supervisor',
                   '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
                   '/usr/bin/env', '-i', 'HOME=' + str(HOME), 'PATH=/usr/bin:/bin',
                   'XDG_RUNTIME_DIR=/run/user/7051508', 'LANG=C.UTF-8',
                   '/usr/bin/python3', '-I', str(Path(__file__).resolve()),
                   '--inside-unit', '--context', str(context), '--expected', args.expected]
        process = subprocess.Popen(command, env=env, cwd='/', stdin=subprocess.DEVNULL)
        def cancel(signum, frame):
            raise KeyboardInterrupt('build interrupted')
        previous = {sig: signal.signal(sig, cancel) for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
        try:
            code = process.wait(timeout=1850)
            if code:
                raise ValueError('scoped image build failed; inspect private build.log')
        finally:
            for sig in previous:
                signal.signal(sig, signal.SIG_IGN)
            # Stop only this invocation's unit. KillMode=control-group and the
            # independent RuntimeMaxSec also protect SSH loss/parent death.
            stopped = subprocess.run(['/usr/bin/systemctl', '--user', 'stop', unit],
                                     env=env, cwd='/', capture_output=True, timeout=20)
            # A successful --collect unit may already be unloaded.
            if stopped.returncode not in (0, 5):
                raise ValueError('owned build unit cleanup failed')
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)
            for sig, handler in previous.items():
                signal.signal(sig, handler)
        return
    cgroup = Path('/proc/self/cgroup').read_text().strip()
    if not re.fullmatch(r'0::/.*/robotgo-image-build-[0-9a-f]{32}\.service/supervisor', cgroup):
        raise ValueError('admitted transient build cgroup required')
    parent_cgroup = cgroup.removeprefix('0::').removesuffix('/supervisor')
    cgroup_root = Path('/sys/fs/cgroup') / parent_cgroup.lstrip('/')
    if ((cgroup_root / 'memory.max').read_text().strip() != '8589934592'
            or (cgroup_root / 'pids.max').read_text().strip() != '256'
            or (cgroup_root / 'cpu.max').read_text().split() != ['200000', '100000']):
        raise ValueError('actual build cgroup limits differ')
    # systemd moves the supervisor to its subgroup before exec, leaving this
    # delegated parent empty. All OCI RUN cgroups remain below the same caps.
    controllers = set((cgroup_root / 'cgroup.controllers').read_text().split())
    if not {'cpu', 'memory', 'pids'}.issubset(controllers):
        raise ValueError('delegated build controllers unavailable')
    (cgroup_root / 'cgroup.subtree_control').write_text('+cpu +memory +pids')
    builds = cgroup_root / 'builds'
    builds.mkdir()
    # Buildah/crun places RUN processes in this exact leaf, not a child of it.
    # Leave its subtree_control empty (the cgroup-v2 no-internal-process rule).
    # Existing reviewed rootless subscription override: no automatic host binds.
    mounts = HOME / '.config/containers/mounts.conf'
    info = mounts.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != UID or info.st_nlink != 1
            or stat.S_IMODE(info.st_mode) != 0o600
            or any(line.strip() and not line.lstrip().startswith('#') for line in mounts.read_text().splitlines())):
        raise ValueError('automatic mount override differs')
    work = context.parent
    hooks = work / 'empty-hooks'
    hooks.mkdir(mode=0o700)
    config = work / 'containers.conf'
    with config.open('x') as output:
        output.write('[containers]\nenv_host=false\nenv=[]\nmounts=[]\nvolumes=[]\n'
                     '[engine]\nremote=false\ncgroup_manager="cgroupfs"\nenv=[]\nhooks_dir=[' + json.dumps(str(hooks)) + ']\n')
    config.chmod(0o600)
    env = {'HOME': str(HOME), 'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8',
           'XDG_RUNTIME_DIR': '/run/user/7051508', 'CONTAINERS_CONF': str(config)}
    log = work / 'build.log'
    tag = 'localhost/robotgo-ovh-ci:' + expected['Containerfile'][:12] + '-' + expected['container-dependencies.sh'][:12]
    with log.open('xb') as output:
        subprocess.run(['/usr/bin/podman', '--remote=false', 'build', '--pull=missing',
                        '--jobs=1', '--isolation=oci', '--force-rm',
                        '--cgroup-parent=' + parent_cgroup + '/builds',
                        '--http-proxy=false', '--layers=false', '--tag', tag,
                        '--file', str(context / 'Containerfile'), str(context)],
                       env=env, cwd='/', stdout=output, stderr=subprocess.STDOUT,
                       timeout=1750, check=True)
    image = image_id(subprocess.check_output(['/usr/bin/podman', '--remote=false', 'image', 'inspect',
                                             '--format', '{{.Id}}', tag], env=env, cwd='/', timeout=30).decode().strip())
    print(json.dumps({'status': 'built', 'image': image, 'tag': tag,
                      'reviewed_files': expected, 'source_or_credentials_in_context': False}, sort_keys=True))


if __name__ == '__main__':
    main()
