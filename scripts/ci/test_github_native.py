"""Exercise the native CI bridge without network, credentials, or desktop access."""
import contextlib
import copy
import io
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import github_native as native


SOURCE = 'a' * 40
TREE = 'b' * 40
TOKEN = 'github_pat_disposable_fixture_only'
WINDOWS_PURE = 'nocgo (windows-latest)'
# Keep runtime evidence independent of the verifier's table: deleting a required
# test or reducing its repetition count must break these regression tests.
RUNTIME_PASSES = {
    'test (macOS-latest)': {'TestPureGoDarwinInputRuntime': 2,
                            'TestPureGoDarwinDisplayScaleRuntime': 1},
    'nocgo (macOS-latest)': {'TestPureGoDarwinInputRuntime': 2,
                             'TestPureGoDarwinDisplayScaleRuntime': 1},
    'test (windows-latest)': dict.fromkeys([
        'TestColor', 'TestSize', 'TestMoveMouse', 'TestMoveMouseSmooth',
        'TestDragMouse', 'TestMoveRelative', 'TestMoveSmoothRelative', 'TestImage',
        'TestWindowsUIAInspectsAndActsOnlyOnSelfOwnedBoundedFixture'], 1),
    WINDOWS_PURE: {'TestPureGoWindowsInputRuntime': 1, 'TestPureGoWindowsWindowRuntime': 3},
}


def successful_run():
    return {
        'id': 801, 'run_attempt': 2, 'head_sha': SOURCE,
        'head_branch': 'ci/entire/' + SOURCE, 'event': 'push',
        'workflow_id': native.WORKFLOW_ID,
        'repository': {'id': native.REPOSITORY_ID},
        'head_repository': {'id': native.REPOSITORY_ID},
        'status': 'completed', 'conclusion': 'success',
        'html_url': 'https://github.com/marang/robotgo/actions/runs/801',
    }


def successful_jobs():
    return [
        {'id': 900 + index, 'run_id': 801, 'run_attempt': 2, 'name': name,
         'status': 'completed', 'conclusion': 'success',
         'runner_name': 'disposable-native-runner', 'labels': ['fixture'],
         'html_url': 'https://github.com/marang/robotgo/actions/jobs/' + str(900 + index),
         'steps': [{'name': step, 'status': 'completed', 'conclusion': 'success'}
                   for step in sorted(steps)]}
        for index, (name, (steps, _)) in enumerate(native.REQUIRED.items())
    ]


def successful_log(name):
    return ''.join('2026-11-05T10:00:00Z --- PASS: %s (0.01s)\n' % test
                   for test, minimum in RUNTIME_PASSES[name].items()
                   for _ in range(minimum)).encode()


class GitHubFixture:
    """Only the explicitly expected API reads and job-log downloads are allowed."""
    def __init__(self):
        self.run = successful_run()
        self.latest = copy.deepcopy(self.run)
        self.jobs = successful_jobs()
        self.tree = TREE
        self.paths = []
        self.logs = {job['id']: successful_log(job['name']) for job in self.jobs}

    def api(self, path, env, *, pages=False):
        self.paths.append(path)
        if '/actions/workflows/go.yml/runs?' in path:
            return [{'workflow_runs': [copy.deepcopy(self.run)]}]
        if path.endswith('/actions/runs/801/attempts/2/jobs?per_page=100'):
            return [{'jobs': copy.deepcopy(self.jobs)}]
        if path.endswith('/actions/runs/801'):
            return copy.deepcopy(self.latest)
        if '/git/commits/' in path or '/commits/' in path:
            return {'sha': SOURCE, 'tree': {'sha': self.tree},
                    'commit': {'tree': {'sha': self.tree}}}
        raise AssertionError('unexpected GitHub request: ' + path)

    def command(self, argv, env, **kwargs):
        self.assert_log_command(argv, kwargs)
        return self.logs[int(argv[-1].split('/')[-2])]

    @staticmethod
    def assert_log_command(argv, kwargs):
        if argv[:4] != ['/usr/bin/gh', 'api', '--hostname', 'github.com']:
            raise AssertionError('unexpected command: ' + repr(argv))
        if not argv[-1].endswith('/logs') or kwargs != {'log': True}:
            raise AssertionError('unexpected log download: ' + repr((argv, kwargs)))


class IdentityAndEvidenceTests(unittest.TestCase):
    def collect(self, fixture):
        with mock.patch.object(native, 'api', side_effect=fixture.api), \
                mock.patch.object(native, 'command', side_effect=fixture.command), \
                contextlib.redirect_stderr(io.StringIO()), \
                contextlib.redirect_stdout(io.StringIO()):
            return native.wait_native(SOURCE, TREE, TOKEN)

    def test_success_receipt_binds_actual_run_attempt_and_all_native_jobs(self):
        fixture = GitHubFixture()
        result = self.collect(fixture)
        self.assertEqual((result['source'], result['tree']), (SOURCE, TREE))
        self.assertEqual((result['provider'], result['run_id'], result['run_attempt']),
                         ('github-actions', 801, 2))
        self.assertEqual({job['name'] for job in result['jobs']}, set(RUNTIME_PASSES))
        for job in result['jobs']:
            self.assertEqual(job['tests'], RUNTIME_PASSES[job['name']])
        self.assertEqual(next(job for job in result['jobs'] if job['name'] == WINDOWS_PURE)
                         ['tests']['TestPureGoWindowsWindowRuntime'], 3)
        self.assertIn('/repos/marang/robotgo/actions/runs/801/attempts/2/jobs?per_page=100',
                      fixture.paths)

    def test_source_provider_and_run_identity_must_match(self):
        mutations = [
            {'head_sha': 'c' * 40}, {'head_branch': 'main'}, {'event': 'pull_request'},
            {'workflow_id': native.WORKFLOW_ID + 1},
            {'repository': {'id': native.REPOSITORY_ID + 1}},
            {'head_repository': {'id': native.REPOSITORY_ID + 1}},
            {'id': 0}, {'id': True}, {'run_attempt': 0}, {'run_attempt': '2'},
        ]
        for change in mutations:
            with self.subTest(change=change), self.assertRaises(native.NativeError):
                native.verify_run(dict(successful_run(), **change), SOURCE)

    def test_missing_duplicate_failed_or_foreign_jobs_cannot_pass(self):
        jobs = successful_jobs()
        variants = [jobs[:-1], jobs + [copy.deepcopy(jobs[0])]]
        for change in ({'conclusion': 'failure'}, {'conclusion': 'skipped'},
                       {'status': 'in_progress'}, {'run_id': 802}, {'run_attempt': 1}):
            altered = copy.deepcopy(jobs)
            altered[0].update(change)
            variants.append(altered)
        variants.append(jobs + [{'name': 'unapproved runner', 'run_id': 801,
                                 'conclusion': 'success'}])
        for index, altered in enumerate(variants):
            with self.subTest(case=index), self.assertRaises(native.NativeError):
                native.verify_jobs(successful_run(), altered)

    def test_required_steps_cannot_be_missing_skipped_failed_or_duplicated(self):
        for mode in ('missing', 'skipped', 'failure', 'in_progress', 'duplicate'):
            jobs = successful_jobs()
            steps = jobs[0]['steps']
            if mode == 'missing':
                steps.pop()
            elif mode == 'duplicate':
                steps.append(copy.deepcopy(steps[0]))
            elif mode == 'in_progress':
                steps[0]['status'] = mode
            else:
                steps[0]['conclusion'] = mode
            with self.subTest(mode=mode), self.assertRaises(native.NativeError):
                native.verify_jobs(successful_run(), jobs)

    def test_unrelated_skipped_jobs_are_accepted_but_provide_no_native_evidence(self):
        jobs = successful_jobs()
        jobs.append({'name': 'linux-only', 'run_id': 801, 'run_attempt': 2,
                     'conclusion': 'skipped'})
        self.assertEqual(set(native.verify_jobs(successful_run(), jobs)), set(native.REQUIRED))

    def test_all_required_runtime_tests_need_individual_passes(self):
        for lane, expected in RUNTIME_PASSES.items():
            good = successful_log(lane)
            for test, minimum in expected.items():
                prefix = ('--- PASS: ' + test + ' ').encode()
                # One fewer pass is insufficient even when other tests pass.
                lines = good.splitlines()
                lines.remove(next(line for line in lines if prefix in line))
                missing = b'\n'.join(lines)
                with self.subTest(lane=lane, test=test), self.assertRaises(native.NativeError):
                    native.verify_log(lane, missing)

    def test_windows_window_runtime_requires_three_complete_passes(self):
        base = b'--- PASS: TestPureGoWindowsInputRuntime (0.01s)\n'
        window = b'--- PASS: TestPureGoWindowsWindowRuntime (0.01s)\n'
        for count in (0, 1, 2):
            with self.subTest(count=count), self.assertRaises(native.NativeError):
                native.verify_log(WINDOWS_PURE, base + window * count)
        self.assertEqual(native.verify_log(WINDOWS_PURE, base + window * 3)
                         ['TestPureGoWindowsWindowRuntime'], 3)
        with self.assertRaises(native.NativeError):
            native.verify_log(WINDOWS_PURE, base + window * 2 +
                              b'--- PASS: TestPureGoWindowsWindowRuntimeSubtest (0s)\n')

    def test_skip_or_fail_invalidates_runtime_even_with_enough_passes(self):
        for marker in ('SKIP', 'FAIL'):
            log = successful_log(WINDOWS_PURE) + (
                '--- %s: TestPureGoWindowsWindowRuntime (0.01s)\n' % marker).encode()
            with self.subTest(marker=marker), self.assertRaises(native.NativeError):
                native.verify_log(WINDOWS_PURE, log)

    def test_rerun_started_during_collection_invalidates_receipt(self):
        for change in ({'run_attempt': 3}, {'status': 'in_progress'},
                       {'conclusion': 'failure'}, {'id': 802}):
            fixture = GitHubFixture()
            fixture.latest.update(change)
            with self.subTest(change=change), self.assertRaises(native.NativeError):
                self.collect(fixture)

    def test_remote_commit_tree_must_match_the_selected_entire_tree(self):
        fixture = GitHubFixture()
        fixture.tree = 'c' * 40
        with self.assertRaises(native.NativeError):
            self.collect(fixture)


class CredentialsTests(unittest.TestCase):
    def test_sha_and_header_injection_are_rejected_before_commands(self):
        for sha in ('main', 'a' * 39, 'A' * 40, SOURCE + '\n', '--all', SOURCE + ':main'):
            with self.subTest(sha=sha), self.assertRaises(native.NativeError):
                native.branch(sha)
        for token in ('', 'short', TOKEN + '\r\nAuthorization: other', TOKEN + ' extra'):
            with self.subTest(token=token), self.assertRaises(native.NativeError):
                native.environment(token)

    def test_ambient_credentials_and_tracing_do_not_enter_children(self):
        poison = {key: 'sensitive-value' for key in (
            'GIT_TRACE', 'GIT_TRACE_CURL', 'GIT_CURL_VERBOSE', 'GH_DEBUG',
            'GIT_CONFIG_COUNT', 'GIT_CONFIG_KEY_0', 'GIT_CONFIG_VALUE_0',
            'AWS_SECRET_ACCESS_KEY', 'ENTIRE_TOKEN', 'GITHUB_TOKEN')}
        with mock.patch.dict(os.environ, poison):
            env = native.environment(TOKEN)
        self.assertFalse(set(poison) & set(env))
        self.assertEqual(env['GIT_CONFIG_GLOBAL'], '/dev/null')
        self.assertEqual(env['GIT_CONFIG_SYSTEM'], '/dev/null')
        self.assertEqual(env['GIT_TERMINAL_PROMPT'], '0')
        with mock.patch.object(native, 'command', return_value=b'') as command:
            native.git(['status'], env, Path('/disposable'))
        args = command.call_args.args[0]
        self.assertIn('credential.helper=', args)
        self.assertIn('core.hooksPath=/dev/null', args)
        self.assertNotIn(TOKEN, ' '.join(args))

    def test_command_failures_do_not_reprint_credential_bearing_output(self):
        result = subprocess.CompletedProcess(['fixture'], 1, TOKEN.encode(), TOKEN.encode())
        out, err = io.StringIO(), io.StringIO()
        with mock.patch.object(native.subprocess, 'run', return_value=result), \
                contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            with self.assertRaises(native.NativeError) as raised:
                native.command(['/usr/bin/gh', 'api', '/fixture'], native.environment(TOKEN))
        self.assertNotIn(TOKEN, str(raised.exception) + out.getvalue() + err.getvalue())

    def test_downloaded_logs_redact_credentials_before_visible_output(self):
        fixture = GitHubFixture()
        fixture.logs = {job: log + b'\ncredential=' + TOKEN.encode()
                        for job, log in fixture.logs.items()}
        out, err = io.StringIO(), io.StringIO()
        with mock.patch.object(native, 'api', side_effect=fixture.api), \
                mock.patch.object(native, 'command', side_effect=fixture.command), \
                contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            receipt = native.wait_native(SOURCE, TREE, TOKEN)
        self.assertNotIn(TOKEN, repr(receipt) + out.getvalue() + err.getvalue())
        self.assertIn('[REDACTED]', err.getvalue())


class MirrorTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='robotgo-native-bridge-')
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        self.source = self.work / 'source'
        self.remote = self.work / 'github.git'
        self.source.mkdir()
        self.remote.mkdir()
        self.local_git(['init', '-q', '--initial-branch=main'], self.source)
        self.local_git(['init', '-q', '--bare'], self.remote)
        self.local_git(['config', 'user.email', 'fixture@example.invalid'], self.source)
        self.local_git(['config', 'user.name', 'Disposable Fixture'], self.source)
        (self.source / 'source.txt').write_text('selected Entire source\n')
        self.local_git(['add', 'source.txt'], self.source)
        self.local_git(['commit', '-qm', 'fixture source'], self.source)
        self.sha = self.local_git(['rev-parse', 'HEAD'], self.source).decode().strip()
        self.tree = self.local_git(['rev-parse', 'HEAD^{tree}'], self.source).decode().strip()
        self.ref = 'refs/heads/ci/entire/' + self.sha
        self.local_git(['tag', '-a', 'must-not-propagate', '-m', 'fixture tag'], self.source)
        self.local_git(['config', 'push.followTags', 'true'], self.source)
        self.calls = []
        self.original_git = native.git
        self.environment = mock.patch.dict(os.environ, {'DAG_RUN_WORK_DIR': str(self.work)})
        self.environment.start()
        self.addCleanup(self.environment.stop)

    @staticmethod
    def local_git(args, cwd):
        result = subprocess.run(['/usr/bin/git', *args], cwd=cwd,
                                env={'PATH': '/usr/bin:/bin', 'HOME': '/nonexistent',
                                     'GIT_CONFIG_GLOBAL': '/dev/null',
                                     'GIT_CONFIG_SYSTEM': '/dev/null'},
                                capture_output=True, check=True)
        return result.stdout

    def bridge_git(self, args, env, source):
        self.calls.append(list(args))
        # No request can leave this machine: only the known GitHub URL is redirected.
        mapped = [str(self.remote) if arg == native.GITHUB else arg for arg in args]
        if any(arg.startswith(('https:', 'entire:')) for arg in mapped):
            raise AssertionError('unexpected network access: ' + repr(args))
        return self.original_git(mapped, env, source)

    def mirror(self):
        with mock.patch.object(native, 'git', side_effect=self.bridge_git):
            return native.mirror(self.sha, TOKEN)

    def test_mirror_creates_only_selected_immutable_ref_without_tags(self):
        result = self.mirror()
        self.assertEqual((result['source'], result['tree']), (self.sha, self.tree))
        self.assertFalse(result['already_mirrored'])
        refs = self.local_git(['show-ref'], self.remote).decode().splitlines()
        self.assertEqual(refs, [self.sha + ' ' + self.ref])
        push = next(args for args in self.calls if args[0] == 'push')
        self.assertIn('--force-with-lease=' + self.ref + ':', push)
        self.assertIn('--no-follow-tags', push)
        self.assertEqual(push[-1], self.sha + ':' + self.ref)
        self.assertFalse({'--mirror', '--all', '--tags', '--force', '-f'} & set(push))
        self.calls.clear()
        self.assertTrue(self.mirror()['already_mirrored'])
        self.assertFalse(any(args[0] == 'push' for args in self.calls))

    def test_existing_ref_collision_is_preserved(self):
        (self.source / 'source.txt').write_text('different source\n')
        self.local_git(['commit', '-qam', 'different fixture commit'], self.source)
        other = self.local_git(['rev-parse', 'HEAD'], self.source).decode().strip()
        self.local_git(['push', '-q', str(self.remote), 'HEAD:' + self.ref], self.source)
        self.local_git(['checkout', '-q', self.sha], self.source)
        with self.assertRaisesRegex(native.NativeError, 'collision'):
            self.mirror()
        self.assertEqual(self.local_git(['rev-parse', self.ref], self.remote).decode().strip(), other)
        self.assertFalse(any(args[0] == 'push' for args in self.calls))

    def test_create_only_lease_refuses_a_concurrent_ref_creation(self):
        original_bridge = self.bridge_git
        (self.source / 'source.txt').write_text('competing commit\n')
        self.local_git(['commit', '-qam', 'competing fixture commit'], self.source)
        other = self.local_git(['rev-parse', 'HEAD'], self.source).decode().strip()
        self.local_git(['checkout', '-q', self.sha], self.source)

        def competing_writer(args, env, source):
            if args[0] == 'push':
                self.local_git(['push', '-q', '--no-follow-tags', str(self.remote),
                                other + ':' + self.ref], self.source)
            return original_bridge(args, env, source)

        with mock.patch.object(native, 'git', side_effect=competing_writer):
            with self.assertRaises(native.NativeError):
                native.mirror(self.sha, TOKEN)
        self.assertEqual(self.local_git(['rev-parse', self.ref], self.remote).decode().strip(), other)

    def test_dirty_tracked_or_untracked_source_never_pushes(self):
        for path in (self.source / 'source.txt', self.source / 'untracked.txt'):
            original = path.read_bytes() if path.exists() else None
            path.write_text('not the selected clean source\n')
            with self.subTest(path=path.name), self.assertRaisesRegex(native.NativeError, 'dirty'):
                self.mirror()
            if original is None:
                path.unlink()
            else:
                path.write_bytes(original)
        self.assertFalse(any(args[0] == 'push' for args in self.calls))

    def test_checkout_head_must_be_the_selected_commit(self):
        (self.source / 'source.txt').write_text('later commit\n')
        self.local_git(['commit', '-qam', 'later fixture commit'], self.source)
        with self.assertRaisesRegex(native.NativeError, 'SHA mismatch'):
            self.mirror()
        self.assertFalse(any(args[0] == 'push' for args in self.calls))

    def test_symlinked_source_checkout_is_refused(self):
        actual = self.work / 'actual-source'
        self.source.rename(actual)
        self.source.symlink_to(actual, target_is_directory=True)
        with self.assertRaises(native.NativeError):
            self.mirror()
        self.assertEqual(self.calls, [])


if __name__ == '__main__':
    unittest.main()
