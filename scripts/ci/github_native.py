#!/usr/bin/python3 -I
"""Fixed RobotGo CI mirror and verification; install reviewed bytes on the host."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import subprocess
import sys
import tempfile
import time

REPOSITORY = 'marang/robotgo'
REPOSITORY_ID = 965149209
WORKFLOW_ID = 155811268
ENTIRE = 'entire://aws-eu-central-1.entire.io/et/marang/robotgo'
GITHUB = 'https://github.com/marang/robotgo.git'
SHA = re.compile(r'^[0-9a-f]{40}$')
LIMIT = 4 * 1024 * 1024
REQUIRED = {
    'test (macOS-latest)': (
        {'Verify native macOS ARM64 runner', 'Build', 'Test',
         'Check native macOS public Go API', 'Test Pure-Go macOS ARM64 build',
         'Test Pure-Go macOS ARM64 input preflight'},
        {'TestPureGoDarwinInputRuntime': 2, 'TestPureGoDarwinDisplayScaleRuntime': 1}),
    'nocgo (macOS-latest)': (
        {'Verify Pure-Go macOS Intel runner', 'Test Pure-Go build',
         'Test Pure-Go macOS input preflight'},
        {'TestPureGoDarwinInputRuntime': 2, 'TestPureGoDarwinDisplayScaleRuntime': 1}),
    'test (windows-latest)': (
        {'Build', 'Test', 'Check native Windows public Go API',
         'Test native Windows pointer and capture on disposable desktop',
         'Test bounded Windows UI Automation fixture'},
        dict.fromkeys(['TestColor', 'TestSize', 'TestMoveMouse', 'TestMoveMouseSmooth',
                       'TestDragMouse', 'TestMoveRelative', 'TestMoveSmoothRelative',
                       'TestImage', 'TestWindowsUIAInspectsAndActsOnlyOnSelfOwnedBoundedFixture'], 1)),
    'nocgo (windows-latest)': (
        {'Test Pure-Go build', 'Test Pure-Go Windows input desktop',
         'Test Pure-Go Windows window and paste desktop'},
        {'TestPureGoWindowsInputRuntime': 1, 'TestPureGoWindowsWindowRuntime': 3}),
}


class NativeError(Exception):
    pass


def require(value, message):
    if not value:
        raise NativeError(message)


def branch(sha):
    require(bool(SHA.fullmatch(sha)), 'expected a full lowercase source SHA')
    return 'ci/entire/' + sha


def environment(token):
    require(bool(re.fullmatch(r'[A-Za-z0-9_]{20,}', token)), 'missing GitHub CI credential')
    # No ambient Git configuration, credential helpers or tracing enter Git.
    return {'PATH': '/usr/bin:/bin', 'HOME': os.environ.get('HOME', '/nonexistent'),
            'LANG': 'C.UTF-8', 'GH_TOKEN': token, 'GH_HOST': 'github.com',
            'GH_PROMPT_DISABLED': '1', 'GH_PAGER': 'cat', 'NO_COLOR': '1',
            'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_CONFIG_SYSTEM': '/dev/null',
            'GIT_TERMINAL_PROMPT': '0'}


def command(argv, env, *, cwd=None, timeout=120, log=False):
    if log:
        # Unlinked private files bound downloaded logs before they enter memory.
        def bound():
            resource.setrlimit(resource.RLIMIT_FSIZE, (LIMIT, LIMIT))
        with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
            result = subprocess.run(argv, env=env, cwd=cwd, stdout=out, stderr=err,
                                    timeout=timeout, preexec_fn=bound)
            out.seek(0); data = out.read(LIMIT + 1)
            require(result.returncode == 0 and len(data) < LIMIT,
                    'GitHub job log unavailable or exceeds the download limit')
            return data
    result = subprocess.run(argv, env=env, cwd=cwd, capture_output=True, timeout=timeout)
    require(result.returncode == 0, 'trusted Git/GitHub command failed (exit %s)' % result.returncode)
    require(len(result.stdout) <= LIMIT, 'Git/GitHub response exceeds limit')
    return result.stdout


def git(args, env, source):
    return command(['/usr/bin/git', '-c', 'core.hooksPath=/dev/null',
                    '-c', 'credential.helper=', '-c',
                    'credential.https://github.com.helper=!/usr/bin/gh auth git-credential',
                    '-c', 'http.followRedirects=false', *args], env, cwd=source, timeout=180)


def mirror(sha, token):
    ref = 'refs/heads/' + branch(sha)
    work = Path(os.environ.get('DAG_RUN_WORK_DIR', ''))
    require(work.is_absolute() and work != Path('/') and not work.is_symlink(),
            'missing managed Dagu workspace')
    source = work / 'source'
    require(source.is_dir() and not source.is_symlink(), 'missing managed source checkout')
    env = environment(token)
    require(git(['rev-parse', '--show-toplevel'], env, source).decode().strip() == str(source),
            'unexpected Git checkout root')
    require(git(['rev-parse', 'HEAD'], env, source).decode().strip() == sha, 'source SHA mismatch')
    require(not git(['status', '--porcelain', '--untracked-files=all'], env, source).strip(),
            'source checkout is dirty')
    tree = git(['rev-parse', 'HEAD^{tree}'], env, source).decode().strip()
    require(bool(SHA.fullmatch(tree)), 'invalid source tree')
    previous = git(['ls-remote', '--heads', GITHUB, ref], env, source).decode().strip()
    if previous:
        require(previous == sha + '\t' + ref, 'immutable CI mirror ref collision')
    else:
        if git(['rev-parse', '--is-shallow-repository'], env, source).strip() == b'true':
            fetch_env = dict(env)
            fetch_env.pop('GH_TOKEN')
            fetch_env.update(PATH='/usr/local/libexec/izoone-entire/0.12.2:/usr/bin:/bin',
                             ENTIRE_CONFIG_DIR='/home/izoone_workers/.config/entire-photo-roller-ci',
                             ENTIRE_TOKEN_STORE='file', ENTIRE_TOKEN_STORE_PATH=
                             '/home/izoone_workers/.config/entire-photo-roller-ci/tokens.json',
                             ENTIRE_CONTEXT='eu.auth.entire.io', ENTIRE_GIT_NO_LOGIN_FALLBACK='true',
                             ENTIRE_TELEMETRY_OPTOUT='1')
            git(['fetch', '--quiet', '--unshallow', '--no-tags', ENTIRE, sha], fetch_env, source)
        # Empty expected value is a create-only lease, never replacement permission.
        git(['push', '--porcelain', '--no-follow-tags', '--force-with-lease=' + ref + ':',
             GITHUB, sha + ':' + ref], env, source)
        require(git(['ls-remote', '--heads', GITHUB, ref], env, source).decode().strip() ==
                sha + '\t' + ref, 'mirror readback mismatch')
    return {'source': sha, 'tree': tree, 'branch': branch(sha), 'already_mirrored': bool(previous)}


def api(path, env, *, pages=False):
    args = ['/usr/bin/gh', 'api', '--hostname', 'github.com',
            '-H', 'Accept: application/vnd.github+json',
            '-H', 'X-GitHub-Api-Version: 2026-03-10', path]
    if pages:
        args.extend(['--paginate', '--slurp'])
    return json.loads(command(args, env))


def verify_run(run, sha):
    require(run.get('head_sha') == sha and run.get('head_branch') == branch(sha)
            and run.get('event') == 'push' and run.get('workflow_id') == WORKFLOW_ID
            and run.get('repository', {}).get('id') == REPOSITORY_ID
            and run.get('head_repository', {}).get('id') == REPOSITORY_ID,
            'GitHub run source/provider mismatch')
    require(type(run.get('id')) is int and run['id'] > 0 and
            type(run.get('run_attempt')) is int and run['run_attempt'] > 0,
            'invalid actual GitHub run/attempt identity')


def verify_jobs(run, jobs):
    native = {}
    for job in jobs:
        name = job.get('name')
        require(job.get('run_id') == run['id'] and job.get('run_attempt') == run['run_attempt'],
                'job belongs to another run/attempt')
        if name not in REQUIRED:
            require(job.get('conclusion') == 'skipped', 'unexpected executed GitHub job')
            continue
        require(name not in native, 'duplicate native GitHub lane')
        require(job.get('status') == 'completed' and job.get('conclusion') == 'success',
                'native GitHub lane failed or incomplete: ' + name)
        steps = job.get('steps', [])
        for expected in REQUIRED[name][0]:
            selected = [step for step in steps if step.get('name') == expected]
            require(len(selected) == 1 and selected[0].get('status') == 'completed'
                    and selected[0].get('conclusion') == 'success',
                    'required native step missing/skipped/failed: ' + expected)
        native[name] = job
    require(set(native) == set(REQUIRED), 'missing required native GitHub lanes')
    return native


def verify_log(name, data):
    text = data.decode('utf-8', errors='replace')
    counts = {}
    for test, minimum in REQUIRED[name][1].items():
        require(not re.search(r'--- (?:SKIP|FAIL): ' + re.escape(test) + r'(?:\s|$)', text),
                'required runtime test skipped/failed: ' + test)
        count = len(re.findall(r'--- PASS: ' + re.escape(test) + r'(?:\s|$)', text))
        require(count >= minimum, 'required runtime PASS missing: ' + test)
        counts[test] = count
    return counts


def job_log(name, job, env, token):
    raw = command(['/usr/bin/gh', 'api', '--hostname', 'github.com',
                   '--allow-escape-sequences',
                   '/repos/' + REPOSITORY + '/actions/jobs/%s/logs' % job['id']], env, log=True)
    safe = raw.decode('utf-8', errors='replace').replace(token, '[REDACTED]')
    # Native test output can contain ANSI; retain readable text, never controls.
    safe = ''.join(c if c in '\n\t' or ord(c) >= 32 and not 127 <= ord(c) <= 159
                   else json.dumps(c)[1:-1] for c in safe)
    print('\n--- GitHub native: ' + name + ' ---\n' + safe, file=sys.stderr, flush=True)
    return raw


def wait_native(sha, tree, token):
    require(bool(SHA.fullmatch(tree)), 'invalid expected source tree')
    env = environment(token); deadline = time.monotonic() + 3600
    run = None
    while time.monotonic() < deadline:
        pages = api('/repos/' + REPOSITORY + '/actions/workflows/go.yml/runs?event=push&branch='
                    + branch(sha) + '&per_page=100', env, pages=True)
        candidates = [r for page in pages for r in page.get('workflow_runs', [])]
        require(len(candidates) <= 1, 'ambiguous GitHub workflow execution')
        if candidates:
            run = candidates[0]; verify_run(run, sha)
            print('GitHub native run=%s attempt=%s status=%s' %
                  (run['id'], run['run_attempt'], run.get('status')), file=sys.stderr, flush=True)
            if run.get('status') == 'completed':
                break
        time.sleep(30)
    else:
        raise NativeError('GitHub native workflow did not complete within one hour')
    pages = api('/repos/' + REPOSITORY + '/actions/runs/%s/attempts/%s/jobs?per_page=100' %
                (run['id'], run['run_attempt']), env, pages=True)
    commit = api('/repos/' + REPOSITORY + '/git/commits/' + sha, env)
    require(commit.get('sha') == sha and commit.get('tree', {}).get('sha') == tree,
            'GitHub source tree mismatch')
    all_jobs = [job for page in pages for job in page.get('jobs', [])]
    if run.get('conclusion') != 'success':
        for job in all_jobs:
            if (job.get('name') in REQUIRED and job.get('run_id') == run['id'] and
                    job.get('run_attempt') == run['run_attempt'] and job.get('status') == 'completed'):
                try:
                    job_log(job['name'], job, env, token)
                except NativeError:
                    print('GitHub failed-job log unavailable', file=sys.stderr)
        raise NativeError('GitHub native workflow failed: ' + run['html_url'])
    jobs = verify_jobs(run, all_jobs)
    records = []
    for name, job in sorted(jobs.items()):
        raw = job_log(name, job, env, token)
        counts = verify_log(name, raw)
        records.append({'name': name, 'job_id': job['id'], 'runner': job.get('runner_name'),
                        'labels': job.get('labels'), 'tests': counts,
                        'log_sha256': hashlib.sha256(raw).hexdigest(), 'url': job['html_url']})
    latest = api('/repos/' + REPOSITORY + '/actions/runs/%s' % run['id'], env)
    verify_run(latest, sha)
    require(latest['id'] == run['id'] and latest['run_attempt'] == run['run_attempt'] and latest['status'] == 'completed'
            and latest['conclusion'] == 'success', 'GitHub attempt changed during collection')
    return {'status': 'passed', 'source': sha, 'tree': tree, 'provider': 'github-actions',
            'run_id': run['id'], 'run_attempt': run['run_attempt'], 'url': run['html_url'],
            'jobs': records}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=['mirror', 'wait'])
    parser.add_argument('sha')
    parser.add_argument('--tree')
    args = parser.parse_args(); branch(args.sha)
    token = os.environ.get('ROBOTGO_GITHUB_TOKEN', '')
    result = mirror(args.sha, token) if args.operation == 'mirror' else wait_native(args.sha, args.tree or '', token)
    print(json.dumps(result), flush=True)


if __name__ == '__main__':
    try:
        main()
    except (NativeError, subprocess.TimeoutExpired) as error:
        print(json.dumps({'status': 'failed', 'error': str(error)}), flush=True)
        print('native-platform CI: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
