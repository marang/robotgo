#!/usr/bin/env python3
"""Preview or prune only expired, locked ephemeral CI run directories."""

import argparse
from contextlib import contextmanager
import fcntl
import json
import os
from pathlib import Path
import pwd
import re
import shutil
import socket
import stat
import time


WORKER_UID = 7051508
WORKER_GID = 1004
WORKER_HOME = Path("/home/izoone_workers")
HOSTNAME = "vps-ac45f3e2.vps.ovh.net"
ROOTS = ('robotgo-ovh',)
TTL_SECONDS = 14 * 24 * 60 * 60
MAX_RUNS = 100
MAX_ROOT_ENTRIES = 10000
MAX_TREE_ENTRIES = 200000
MAX_DEPTH = 64
DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
RUN_NAME = re.compile(r"[0-9a-f]{32}\Z")


def require_identity():
    if (os.getuid(), os.geteuid(), os.getgid(), os.getegid()) != (WORKER_UID, WORKER_UID, WORKER_GID, WORKER_GID):
        raise ValueError("requires the fixed unprivileged worker identity")
    if (socket.gethostname() != HOSTNAME or os.uname().machine != "x86_64"
            or os.environ.get("HOME") != str(WORKER_HOME)
            or pwd.getpwuid(WORKER_UID).pw_dir != str(WORKER_HOME)):
        raise ValueError("requires the verified OVH host and worker home")
    if not shutil.rmtree.avoids_symlink_attacks:
        raise ValueError("requires fd-protected shutil.rmtree")


def private_directory(info):
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != WORKER_UID or info.st_gid != WORKER_GID or stat.S_IMODE(info.st_mode) != 0o700:
        raise ValueError("directory ownership or private mode rejected")


def kernel_mapping(path):
    with path.open("r", encoding="ascii") as stream:
        text = stream.read(4097)
    if len(text) > 4096:
        raise ValueError("kernel identity data exceeds bound")
    return [tuple(int(value) for value in line.split()) for line in text.splitlines() if line.strip()]


def system_ancestor(info, descriptor):
    if not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) & 0o022:
        raise ValueError("untrusted system ancestor")
    if (info.st_uid, info.st_gid) == (0, 0):
        return
    # A rootless systemd mount namespace can render unmapped host root as the
    # kernel overflow identity. Accept only that exact, read-only arrangement.
    overflow_uid = kernel_mapping(Path("/proc/sys/kernel/overflowuid"))
    overflow_gid = kernel_mapping(Path("/proc/sys/kernel/overflowgid"))
    if (len(overflow_uid) != 1 or len(overflow_gid) != 1
            or len(overflow_uid[0]) != 1 or len(overflow_gid[0]) != 1):
        raise ValueError("kernel overflow identity schema rejected")
    owners = overflow_uid[0][0], overflow_gid[0][0]
    if (0 in owners or owners != (info.st_uid, info.st_gid)
            or kernel_mapping(Path("/proc/self/uid_map")) != [(WORKER_UID, WORKER_UID, 1)]
            or kernel_mapping(Path("/proc/self/gid_map")) != [(WORKER_GID, WORKER_GID, 1)]
            or not os.fstatvfs(descriptor).f_flag & os.ST_RDONLY):
        raise ValueError("untrusted mapped system ancestor")


@contextmanager
def state_root(name):
    # Open each component without following links; retain the final descriptor.
    descriptors = []
    try:
        fd = os.open("/", DIRECTORY_FLAGS)
        descriptors.append(fd)
        for component in ("home", WORKER_HOME.name, ".local", "state", name):
            parent = os.fstat(fd)
            if len(descriptors) <= 2:
                system_ancestor(parent, fd)
            else:
                private_directory(parent)
            fd = os.open(component, DIRECTORY_FLAGS, dir_fd=fd)
            descriptors.append(fd)
        private_directory(os.fstat(fd))
        yield fd
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def validate_tree(fd, remaining, depth=0):
    if depth > MAX_DEPTH:
        raise ValueError("run tree depth exceeds bound")
    with os.scandir(fd) as entries:
        for entry in entries:
            remaining[0] -= 1
            if remaining[0] < 0:
                raise ValueError("run tree entry count exceeds bound")
            info = entry.stat(follow_symlinks=False)
            if info.st_uid != WORKER_UID or info.st_gid != WORKER_GID:
                raise ValueError("run content ownership rejected")
            if stat.S_ISDIR(info.st_mode):
                child = os.open(entry.name, DIRECTORY_FLAGS, dir_fd=fd)
                try:
                    opened = os.fstat(child)
                    if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
                        raise ValueError("run content changed during validation")
                    validate_tree(child, remaining, depth + 1)
                finally:
                    os.close(child)
            elif not (stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode)):
                raise ValueError("run special file rejected")


def candidate(root_fd, name, cutoff):
    info = os.stat(name, dir_fd=root_fd, follow_symlinks=False)
    private_directory(info)
    if info.st_mtime > cutoff:
        return None
    fd = os.open(name, DIRECTORY_FLAGS, dir_fd=root_fd)
    try:
        opened = os.fstat(fd)
        if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
            raise ValueError("run changed during validation")
        validate_tree(fd, [MAX_TREE_ENTRIES])
    finally:
        os.close(fd)
    return info


def prune_root(root_fd, apply, now, budget):
    result = {"status": "ok", "ignored": 0, "young": 0, "runs": []}
    lock = os.open("runner.lock", os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK, dir_fd=root_fd)
    try:
        lock_info = os.fstat(lock)
        if (not stat.S_ISREG(lock_info.st_mode) or lock_info.st_uid != WORKER_UID or lock_info.st_gid != WORKER_GID
                or stat.S_IMODE(lock_info.st_mode) != 0o600 or lock_info.st_nlink != 1):
            raise ValueError("persistent runner.lock ownership, mode or link count rejected")
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return result | {"status": "busy"}

        def same_persistent_lock():
            current = os.stat("runner.lock", dir_fd=root_fd, follow_symlinks=False)
            if (current.st_dev, current.st_ino, current.st_nlink, current.st_mode, current.st_uid, current.st_gid) != (
                    lock_info.st_dev, lock_info.st_ino, 1, lock_info.st_mode, WORKER_UID, WORKER_GID):
                raise ValueError("persistent runner.lock changed")

        same_persistent_lock()
        names = []
        with os.scandir(root_fd) as entries:
            for entry in entries:
                if len(names) + result["ignored"] >= MAX_ROOT_ENTRIES:
                    raise ValueError("state root entry count exceeds bound")
                if RUN_NAME.fullmatch(entry.name):
                    names.append(entry.name)
                else:
                    result["ignored"] += 1
        for name in sorted(names):
            if budget[0] <= 0:
                result["limit_reached"] = True
                break
            same_persistent_lock()
            try:
                info = candidate(root_fd, name, now - TTL_SECONDS)
                if info is None:
                    result["young"] += 1
                    continue
                # The persistent job lock stays held through both validation and removal.
                current = os.stat(name, dir_fd=root_fd, follow_symlinks=False)
                if (current.st_dev, current.st_ino, current.st_mtime_ns) != (info.st_dev, info.st_ino, info.st_mtime_ns):
                    raise ValueError("run changed before removal")
                if apply:
                    shutil.rmtree(name, dir_fd=root_fd)
                result["runs"].append({"id": name, "action": "deleted" if apply else "would_delete"})
                budget[0] -= 1
            except (OSError, ValueError) as error:
                result["status"] = "error"
                # Bounded records; malformed old runs cannot generate unlimited reports.
                result["runs"].append({"id": name, "action": "refused", "reason": str(error)[:300]})
                budget[0] -= 1
        return result
    finally:
        os.close(lock)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply", action="store_true", help="remove validated expired runs; default is preview")
    args = parser.parse_args()
    report = {"mode": "apply" if args.apply else "preview", "ttl_days": 14, "max_runs": MAX_RUNS, "roots": {}}
    try:
        require_identity()
        budget = [MAX_RUNS]
        for name in ROOTS:
            try:
                with state_root(name) as fd:
                    report["roots"][name] = prune_root(fd, args.apply, time.time(), budget)
            except FileNotFoundError:
                report["roots"][name] = {"status": "error", "reason": "required root, ancestor or persistent lock missing"}
            except (OSError, ValueError) as error:
                report["roots"][name] = {"status": "error", "reason": str(error)[:300]}
    except (OSError, ValueError, KeyError) as error:
        report["error"] = str(error)[:300]
    failed = "error" in report or any(value["status"] == "error" for value in report["roots"].values())
    report["status"] = "error" if failed else "ok"
    print(json.dumps(report, sort_keys=True))
    return 2 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
