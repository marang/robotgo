"""Offline runner integration: real Git fixtures, fake transport/auth/Podman."""
import contextlib
import importlib.util
import io
import json
import os
import pwd
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location('local_runner', Path(__file__).with_name('runner.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.state = self.root / 'state'
        self.fixture = self.root / 'fixture'
        self.fixture.mkdir()
        self.git = shutil.which('git')
        def git(*args):
            return subprocess.check_output([self.git, *args], cwd=self.fixture, stderr=subprocess.PIPE).decode().strip()
        self.command_git = git
        git('init', '-b', 'main')
        git('config', 'user.name', 'Fixture')
        git('config', 'user.email', 'fixture@example.invalid')
        (self.fixture / 'go.mod').write_text('module fixture\n\ngo 1.26.10\n')
        (self.fixture / 'README.md').write_text('first version\n')
        (self.fixture / 'fixture-script.sh').write_text('#!/bin/sh\nexit 0\n')
        (self.fixture / 'fixture-script.sh').chmod(0o755)
        git('add', '.')
        git('commit', '-m', 'first')
        self.old = git('rev-parse', 'HEAD')
        (self.fixture / 'README.md').write_text('current main\n')
        git('commit', '-am', 'main')
        self.head = git('rev-parse', 'HEAD')
        # These host-only secrets and uncommitted files must never be cloned.
        git('config', 'fixture.private', 'host-config-secret')
        for directory in ('.entire', '.codex'):
            (self.fixture / directory).mkdir()
            (self.fixture / directory / 'private').write_text('private-session')
        (self.fixture / 'uncommitted.txt').write_text('private-uncommitted')
        self.options = self.root / 'options.json'
        self.options.write_text(json.dumps({'auth': True, 'exit': 0}))
        self.events = self.root / 'events.jsonl'
        self.binaries = {}
        preamble = f'#!{os.sys.executable}\nimport json, os, pathlib, subprocess, sys\noptions=json.loads(pathlib.Path({str(self.options)!r}).read_text())\ndef record(kind, **fields):\n with open({str(self.events)!r},"a") as f: f.write(json.dumps(dict(kind=kind,**fields))+"\\n")\n'
        sources = {
            'entire': preamble + 'record("auth",args=sys.argv[1:])\nprint(json.dumps({"logged_in":options["auth"],"jurisdiction":"eu","token_source":"file /home/izoone_workers/.config/entire-photo-roller-ci/tokens.json"}))\n',
            'git': preamble + f'args=sys.argv[1:]\nrecord("git",args=args,has_bearer="ENTIRE_TOKEN" in os.environ,no_login=os.environ.get("ENTIRE_GIT_NO_LOGIN_FALLBACK"))\nargs=[{str(self.fixture)!r} if arg=={runner.REMOTE!r} else arg for arg in args]\nsys.exit(subprocess.run([{self.git!r},*args]).returncode)\n',
            'podman': preamble + 'raw_args=sys.argv[1:]\nassert raw_args[0]=="--remote=false"\nargs=raw_args[1:]\nif args[0]=="rm":\n record("cleanup",args=raw_args)\n sys.exit(options.get("cleanup_exit",0))\ncid=pathlib.Path(args[args.index("--cidfile")+1])\ncid.write_text("a"*64)\nsource=pathlib.Path(args[args.index("--volume")+1].split(":")[0])\nrecord("podman",args=raw_args,engine_env=dict(os.environ),engine_config=pathlib.Path(os.environ["CONTAINERS_CONF"]).read_text(),has_bearer="ENTIRE_TOKEN" in os.environ,files=sorted(str(p.relative_to(source)) for p in source.iterdir()),config=(source/".git/config").read_text(),home=os.environ.get("HOME"))\nprint("raw-secret-token /private/raw-test-data",flush=True)\nprint("raw-stderr-secret",file=sys.stderr,flush=True)\n(source/"test-artifact").write_text("private artifact")\nimport time\ntime.sleep(options.get("sleep",0))\nsys.exit(options["exit"])\n',
        }
        for name, source in sources.items():
            executable = self.root / name
            executable.write_text(source)
            executable.chmod(0o755)
            self.binaries[name] = str(executable)


    def invoke(self, task='ci', success=True, if_changed=False, extra=()):
        output, errors = io.StringIO(), io.StringIO()
        arguments = ['--task', task, *extra] + (['--if-changed'] if if_changed else [])
        with patch.object(runner, 'STATE_DIRECTORY', self.state), \
             patch.object(runner, 'IMAGE', 'sha256:' + '1' * 64), \
             patch.object(runner, 'audit_remote_auth'), patch.object(runner, 'audit_default_mounts'), \
             patch.dict(os.environ, {'ENTIRE_TOKEN': 'broad-owner-secret', 'LD_PRELOAD': '/hostile/loader.so',
                                    'PYTHONPATH': '/hostile/python', 'HTTPS_PROXY': 'private-proxy',
                                    'CONTAINER_HOST': 'ssh://outside', 'AWS_SECRET_ACCESS_KEY': 'secret',
                                    'CONTAINERS_CONF_OVERRIDE': '/hostile/config', 'SSH_AUTH_SOCK': '/hostile/ssh'}), \
             contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
            code = runner.main(arguments, self.binaries)
        self.assertEqual(code, 0 if success else 1)
        self.assertEqual(errors.getvalue(), '')
        result = json.loads(output.getvalue())
        self.assertEqual(set(result), {'status', 'task', 'commit', 'duration_seconds', 'run_id'})
        self.assertNotIn('secret', output.getvalue())
        self.assertNotIn(str(self.root), output.getvalue())
        return result

    def records(self):
        return [json.loads(line) for line in self.events.read_text().splitlines()]

    def test_private_worker_umask_preserves_git_worktree_modes_only_during_checkout(self):
        previous = os.umask(0o077)
        try:
            with patch.object(runner.subprocess, 'Popen', wraps=subprocess.Popen) as popen:
                result = self.invoke()
            self.assertEqual(os.umask(0o077), 0o077)
        finally:
            os.umask(previous)
        run = self.state / result['run_id']
        source = run / 'source'
        self.assertEqual((source / 'fixture-script.sh').stat().st_mode & 0o777, 0o755)
        self.assertEqual((source / 'go.mod').stat().st_mode & 0o777, 0o644)
        for directory in (self.state, run, source, source / '.git', run / 'objects.git'):
            self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
        for path in (run / 'job.log', self.state / 'runner.lock', run / 'source/.git/config'):
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        checkout_count = 0
        for invocation in popen.call_args_list:
            is_checkout = 'checkout' in invocation.args[0]
            checkout_count += is_checkout
            self.assertEqual(invocation.kwargs.get('umask'), 0o022 if is_checkout else 0o077)
        self.assertEqual(checkout_count, 1)

    def test_snapshot_is_current_main_only_and_container_is_fixed_credential_free(self):
        result = self.invoke()
        self.assertEqual(result['commit'], self.head)
        source = self.state / result['run_id'] / 'source'
        self.assertEqual(self.command_git('-C', str(source), 'rev-list', '--count', 'HEAD'), '1')
        self.assertEqual(self.command_git('-C', str(source), 'rev-parse', 'origin/main'), self.head)
        event = next(e for e in self.records() if e['kind'] == 'podman')
        self.assertNotIn('.entire', event['files'])
        self.assertNotIn('uncommitted.txt', event['files'])
        self.assertNotIn('host-config-secret', event['config'])
        for value in ('--cpus=2', '--memory=8g', '--pids-limit=256', '--timeout=1800',
                      '--cap-drop=all', '--security-opt=no-new-privileges', '--http-proxy=false',
                      '--image-volume=ignore', '--env-host=false', '--pull=never', '--log-driver=none'):
            self.assertIn(value, event['args'])
        self.assertEqual(event['args'].count('--volume'), 1)
        self.assertIn('--entrypoint=/bin/bash', event['args'])
        self.assertEqual(event['args'][-2:], ['/opt/robotgo-ci/container-ci.sh', 'ci'])
        for name in ('ENTIRE_TOKEN', 'LD_PRELOAD', 'PYTHONPATH', 'HTTPS_PROXY', 'AWS_SECRET_ACCESS_KEY',
                     'CONTAINER_HOST', 'SSH_AUTH_SOCK', 'CONTAINERS_CONF_OVERRIDE'):
            self.assertNotIn(name, event['engine_env'])
        for setting in ('env_host=false', 'mounts=[]', 'volumes=[]', 'remote=false', 'empty-hooks'):
            self.assertIn(setting, event['engine_config'])
        fetch = next(e for e in self.records() if e['kind'] == 'git' and runner.REMOTE in e['args'])
        self.assertIn('+refs/heads/main:refs/heads/main', fetch['args'])
        self.assertNotIn('refs/entire', ' '.join(fetch['args']))
        self.assertFalse(fetch['has_bearer'])
        self.assertEqual(fetch['no_login'], 'true')

    def test_ci_watch_marker_requires_success_and_cleanup(self):
        for options in ({'auth': True, 'exit': 7}, {'auth': True, 'exit': 0, 'cleanup_exit': 7}):
            self.options.write_text(json.dumps(options))
            self.invoke(success=False, if_changed=True)
            self.assertFalse((self.state / 'last-ci-success.json').exists())
        self.options.write_text(json.dumps({'auth': True, 'exit': 0}))
        self.assertEqual(self.invoke(if_changed=True)['status'], 'passed')
        self.assertEqual(self.invoke(if_changed=True)['status'], 'unchanged')
        self.assertEqual(sum(e['kind'] == 'podman' for e in self.records()), 3)

    def test_other_tasks_are_fixed_and_never_seed_ci_marker(self):
        self.invoke('security')
        self.invoke('dependencies')
        self.assertFalse((self.state / 'last-ci-success.json').exists())
        events = [e for e in self.records() if e['kind'] == 'podman']
        for event, task in zip(events, ('security', 'dependencies')):
            self.assertIn('--entrypoint=/bin/bash', event['args'])
            self.assertEqual(event['args'][-2:], ['/opt/robotgo-ci/container-ci.sh', task])

    def test_readonly_pipeline_scripts_run_through_bash_not_direct_execution(self):
        # The image's reviewed scripts are mode 0444, not executables. Every
        # task must enter the same environment-sanitizing pipeline dispatcher.
        script = self.root / 'readonly-pipeline.sh'
        script.write_text('printf "%s\\n" "$1"\n')
        script.chmod(0o444)
        for task, words in runner.TASKS.items():
            self.assertEqual(words, ('/opt/robotgo-ci/container-ci.sh', task))
            result = subprocess.run(['/bin/bash', str(script), *words[1:]],
                                    capture_output=True, text=True, check=True)
            self.assertEqual(result.stdout, task + '\n')
        with self.assertRaises(PermissionError):
            subprocess.run([str(script), 'ci'], check=True, capture_output=True)

    def test_arbitrary_commit_state_path_commands_and_bad_change_detection_rejected(self):
        for extra in (('--commit', self.old), ('--state-dir', '/tmp/hostile'), ('--task', 'ci;id'),
                      ('--command', 'id'), ('--help',), ('--if-change',)):
            self.invoke(success=False, extra=extra)
        self.invoke('dependencies', success=False, if_changed=True)
        self.assertFalse(self.events.exists())

    def test_source_metadata_refused_and_no_container_launched(self):
        (self.fixture / '.codex/private').write_text('synthetic fixture, not a real session')
        self.command_git('add', '.codex')
        self.command_git('commit', '-m', 'forbidden metadata fixture')
        self.invoke(success=False)
        self.assertFalse(any(e['kind'] == 'podman' for e in self.records()))

    def add_reviewed_activation_metadata(self):
        repository = Path(__file__).resolve().parents[2]
        for name in ('.codex/hooks.json', '.entire/.gitignore'):
            (self.fixture / name).write_bytes((repository / name).read_bytes())
            self.command_git('add', name)
        self.command_git('commit', '-m', 'reviewed Entire activation metadata')

    def test_reviewed_entire_activation_metadata_allows_current_main(self):
        self.add_reviewed_activation_metadata()
        result = self.invoke()
        self.assertEqual(result['commit'], self.command_git('rev-parse', 'HEAD'))
        source = self.state / result['run_id'] / 'source'
        self.assertTrue((source / '.codex/hooks.json').is_file())
        self.assertTrue((source / '.entire/.gitignore').is_file())
        self.assertFalse((source / '.codex/private').exists())
        self.assertFalse((source / '.entire/private').exists())

    def test_activation_metadata_rejects_changed_bytes_modes_and_extra_private_files(self):
        self.add_reviewed_activation_metadata()
        clean = self.command_git('rev-parse', 'HEAD')
        for name in ('.codex/hooks.json', '.entire/.gitignore'):
            path = self.fixture / name
            original = path.read_bytes()
            for changed in ('bytes', 'executable', 'symlink'):
                with self.subTest(name=name, changed=changed):
                    if changed == 'bytes':
                        path.write_bytes(original + b'\n')
                    elif changed == 'executable':
                        path.chmod(0o755)
                    else:
                        path.unlink()
                        path.symlink_to('uncommitted.txt')
                    self.command_git('add', name)
                    self.command_git('commit', '-m', 'unreviewed activation metadata')
                    self.invoke(success=False)
                    self.command_git('checkout', clean, '--', name)
                    self.command_git('commit', '-m', 'restore reviewed metadata')
        for name in ('.entire/settings.local.json', '.entire/metadata/session.json',
                     '.codex/private', '.claude/settings.json', '.gitmodules', '.gitattributes'):
            with self.subTest(name=name):
                path = self.fixture / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('synthetic forbidden fixture\n')
                self.command_git('add', '-f', name)
                self.command_git('commit', '-m', 'forbidden source metadata')
                self.invoke(success=False)
                self.command_git('rm', '-f', name)
                self.command_git('commit', '-m', 'remove forbidden fixture')
        self.assertFalse(any(e['kind'] == 'podman' for e in self.records()))

    def test_activation_metadata_rejects_pinned_blob_under_whitespace_filename(self):
        self.add_reviewed_activation_metadata()
        original = '.entire/.gitignore'
        renamed = original + ' '
        (self.fixture / original).rename(self.fixture / renamed)
        self.command_git('add', '-A', '--', original, renamed)
        self.command_git('commit', '-m', 'unexpected trailing-space metadata filename')
        self.invoke(success=False)
        self.assertFalse(any(e['kind'] == 'podman' for e in self.records()))

    def test_failure_stays_private_bounded_and_cleanup_is_exact(self):
        self.options.write_text(json.dumps({'auth': True, 'exit': 7}))
        with patch.object(runner, 'MAX_LOG_BYTES', 256):
            result = self.invoke(success=False)
        run = self.state / result['run_id']
        self.assertLessEqual((run / 'job.log').stat().st_size, 256)
        self.assertEqual((run / 'job.log').stat().st_mode & 0o777, 0o600)
        self.assertEqual(run.stat().st_mode & 0o777, 0o700)
        cleanup = [e['args'] for e in self.records() if e['kind'] == 'cleanup']
        self.assertEqual(cleanup, [['--remote=false', 'rm', '--force', '--ignore', 'a' * 64]])

    def test_timeout_cleans_only_owned_container(self):
        self.options.write_text(json.dumps({'auth': True, 'exit': 0, 'sleep': 10}))
        with patch.dict(runner.TIMEOUTS, {'podman': 0.1}):
            self.invoke(success=False)
        cleanup = [e['args'] for e in self.records() if e['kind'] == 'cleanup']
        self.assertEqual(cleanup, [['--remote=false', 'rm', '--force', '--ignore', 'a' * 64]])

    def test_signal_cancellation_cleans_only_owned_container_and_prints_summary(self):
        self.options.write_text(json.dumps({'auth': True, 'exit': 0, 'sleep': 10}))
        entry = self.root / 'invoke.py'
        entry.write_text('import importlib.util,sys\n'
                         + 's=importlib.util.spec_from_file_location("runner",' + repr(runner.__file__) + ')\n'
                         + 'r=importlib.util.module_from_spec(s);s.loader.exec_module(r)\n'
                         + 'r.audit_remote_auth=lambda:None\nr.audit_default_mounts=lambda home:None\n'
                         + 'r.STATE_DIRECTORY=' + repr(str(self.state)) + '\n'
                         + 'r.IMAGE="sha256:"+"1"*64\n'
                         + 'sys.exit(r.main(["--task","ci"],' + repr(self.binaries) + '))\n')
        for sig in (signal.SIGTERM, signal.SIGHUP):
            self.events.unlink(missing_ok=True)
            process = subprocess.Popen([os.sys.executable, '-B', str(entry)], stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, text=True)
            try:
                deadline = time.monotonic() + 10
                while time.monotonic() < deadline:
                    if self.events.exists() and any(e['kind'] == 'podman' for e in self.records()):
                        break
                    time.sleep(0.02)
                else:
                    self.fail('fake Podman did not start')
                process.send_signal(sig)
                output, errors = process.communicate(timeout=5)
                self.assertEqual(process.returncode, 1)
                self.assertEqual(errors, '')
                self.assertEqual(json.loads(output)['status'], 'failed')
                self.assertEqual([e['args'] for e in self.records() if e['kind'] == 'cleanup'],
                                 [['--remote=false', 'rm', '--force', '--ignore', 'a' * 64]])
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
                process.stdout.close()
                process.stderr.close()

    def test_lock_and_symlink_marker_refused(self):
        state = runner.private_directory(self.state)
        with runner.execution_lock(state):
            self.invoke(success=False)
        self.assertFalse(self.events.exists())
        target = self.root / 'untouched'
        target.write_text('synthetic fixture')
        (state / 'last-ci-success.json').symlink_to(target)
        self.invoke(success=False, if_changed=True)
        self.assertEqual(target.read_text(), 'synthetic fixture')
        self.assertFalse(any(e['kind'] == 'podman' for e in self.records()))

    def test_fixed_auth_environment_and_fail_closed_audits(self):
        with patch.object(runner, 'audit_remote_auth'):
            env = runner.host_environment()
        self.assertEqual(env['ENTIRE_CONTEXT'], 'eu.auth.entire.io')
        self.assertEqual(env['ENTIRE_TOKEN_STORE_PATH'], str(runner.AUTH_TOKEN_FILE))
        self.assertNotIn('ENTIRE_TOKEN', env)
        with self.assertRaises(runner.Refused):
            runner.audit_remote_auth()
        with self.assertRaises(runner.Refused):
            runner.audit_default_mounts(self.root)
        link = self.root / 'link'
        link.symlink_to(self.root)
        with self.assertRaises(runner.Refused):
            runner.private_directory(link / 'new')


class ProfileTests(unittest.TestCase):
    def test_verified_descriptor_executes_only_clean_environment_and_rejects_changed_driver(self):
        spec = importlib.util.spec_from_file_location('profile', Path(__file__).with_name('build-profile.py'))
        profile = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(profile)
        go = Path('/home/markus/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.linux-amd64/bin/go')
        if not go.is_file():
            self.skipTest('offline pinned Go 1.26.10 is unavailable')
        # Only the test variant substitutes the current Unix identity and a
        # private home. The deployable builder cannot change these constants.
        with tempfile.TemporaryDirectory(prefix='.robotgo-entry-test-', dir=Path.home()) as temporary:
            root = Path(temporary)
            driver = root / '.local/libexec/robotgo-ci.py'
            driver.parent.mkdir(parents=True, mode=0o700)
            content = b'import json,os,sys\nprint(json.dumps({"env":dict(os.environ),"args":sys.argv[1:]}))\n'
            driver.write_bytes(content)
            driver.chmod(0o700)
            code = Path(__file__).with_name('launcher.go').read_text()
            code = code.replace('const ownerUID = 7051508', 'const ownerUID = ' + str(os.getuid()))
            code = code.replace('const ownerGID = 1004', 'const ownerGID = ' + str(os.getgid()))
            code = code.replace('const ownerHome = "/home/izoone_workers"',
                                'const ownerHome = ' + json.dumps(str(root)))
            binary = profile.build_launcher(code.encode(), content, go)
            entry = root / 'entry'
            entry.write_bytes(binary)
            entry.chmod(0o700)
            startup = root / 'startup-marker'
            (root / 'sitecustomize.py').write_text('from pathlib import Path\nPath(' + repr(str(startup)) + ').touch()\n')
            hostile = dict(os.environ, PYTHONPATH=str(root), LD_PRELOAD='/hostile/absent.so',
                           HOME='/hostile/home', SECRET_TOKEN='private', BASH_ENV='/hostile/shell')
            control = subprocess.run(['/usr/bin/python3', '-c', 'pass'], env=hostile, capture_output=True)
            self.assertEqual(control.returncode, 0)
            self.assertTrue(startup.exists())
            startup.unlink()
            result = subprocess.run([str(entry), '--task', 'ci'], env=hostile, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
            self.assertEqual(result.stderr, '')
            record = json.loads(result.stdout)
            self.assertEqual(record['args'], ['--task', 'ci'])
            self.assertEqual(record['env']['HOME'], str(root))
            self.assertFalse(startup.exists())
            for key in ('LD_PRELOAD', 'PYTHONPATH', 'BASH_ENV', 'SECRET_TOKEN'):
                self.assertNotIn(key, record['env'])
            driver.write_text('raise RuntimeError("unreviewed replacement")\n')
            result = subprocess.run([str(entry)], env=hostile, capture_output=True, text=True)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, '')
            self.assertEqual(json.loads(result.stdout)['status'], 'failed')

    def test_builder_rejects_mutable_image_and_existing_output(self):
        spec = importlib.util.spec_from_file_location('profile', Path(__file__).with_name('build-profile.py'))
        profile = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(profile)
        with tempfile.TemporaryDirectory() as root:
            args = ['--go', '/usr/bin/go', '--output', root]
            with self.assertRaises(ValueError):
                profile.main(['--image-id', 'golang:latest', *args])
            with self.assertRaises(ValueError):
                profile.main(['--image-id', 'sha256:' + '1' * 64, *args])

    def test_static_seal_reproducible_and_refuses_wrong_uid_without_loading_injection(self):
        spec = importlib.util.spec_from_file_location('profile', Path(__file__).with_name('build-profile.py'))
        profile = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(profile)
        go = Path('/home/markus/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.10.linux-amd64/bin/go')
        if not go.is_file():
            self.skipTest('offline pinned Go 1.26.10 is unavailable')
        code = Path(__file__).with_name('launcher.go').read_bytes()
        driver = b'raise RuntimeError("must never execute on wrong UID")\n'
        first = profile.build_launcher(code, driver, go)
        self.assertEqual(first, profile.build_launcher(code, driver, go))
        profile.verify_elf(first)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            entry = root / 'launcher'
            entry.write_bytes(first)
            entry.chmod(0o700)
            marker = root / 'python-startup-ran'
            (root / 'sitecustomize.py').write_text('from pathlib import Path\nPath(' + repr(str(marker)) + ').touch()\n')
            hostile = dict(os.environ, PYTHONPATH=str(root), LD_PRELOAD='/hostile/absent.so',
                           BASH_ENV='/hostile/shell', HOME='/hostile/home', SECRET_TOKEN='private')
            result = subprocess.run([str(entry), '--task', 'ci'], env=hostile, capture_output=True, text=True)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, '')
            self.assertEqual(json.loads(result.stdout)['status'], 'failed')
            self.assertFalse(marker.exists())


if __name__ == '__main__':
    unittest.main()
