"""Check native CI routing without executing desktop or input tests.

Run with: python -m unittest discover -s scripts/ci -p test_native_workflow.py
Requires PyYAML; the workflow remains the source of job and matrix definitions.
"""
import ast
import json
from pathlib import Path
import re
import unittest

import yaml


WORKFLOW = Path(__file__).resolve().parents[2] / '.github/workflows/go.yml'
LINUX_JOBS = {
    'api-compat', 'vet', 'race', 'sanitizer', 'lint',
    'x11-backend-evidence', 'ocr', 'wayland-integration',
}
NATIVE_CASES = {
    ('test (macOS-latest)', 'macos-15'),
    ('test (windows-latest)', 'windows-latest'),
    ('nocgo (macOS-latest)', 'macos-26-intel'),
    ('nocgo (windows-latest)', 'windows-latest'),
}


def expression(value, ref):
    """Evaluate only the expression subset used for event routing."""
    if not isinstance(value, str) or not value.startswith('${{'):
        return value
    source = value[3:-2].strip().replace('github.ref', 'ref')
    source = source.replace('&&', ' and ').replace('||', ' or ')
    source = re.sub(r'!(?!=)', 'not ', source).strip()
    tree = ast.parse(source, mode='eval')
    allowed = (ast.Expression, ast.BoolOp, ast.UnaryOp, ast.And, ast.Or,
               ast.Not, ast.Call, ast.Name, ast.Load, ast.Constant)
    if any(not isinstance(node, allowed) for node in ast.walk(tree)):
        raise AssertionError(f'unsupported routing expression: {value}')
    return eval(compile(tree, '<workflow routing>', 'eval'), {'__builtins__': {}}, {
        'ref': ref, 'startsWith': str.startswith, 'fromJSON': json.loads,
    })


def scheduled_cases(workflow, ref):
    cases = set()
    for job_id, job in workflow['jobs'].items():
        if not expression(job.get('if', True), ref):
            continue
        matrix = job.get('strategy', {}).get('matrix')
        if matrix is None:
            cases.add((job_id, job['runs-on']))
            continue
        rows = [{'os': os} for os in expression(matrix['os'], ref)]
        for extra in matrix.get('include', []):
            matches = [row for row in rows if row['os'] == extra['os']]
            if not matches:
                raise AssertionError(f'include unexpectedly creates a lane: {extra}')
            for row in matches:
                row.update(extra)
        for row in rows:
            name = job['name'].replace('${{ matrix.os }}', row['os'])
            cases.add((name, row.get('runner', row['os'])))
    return cases


class NativeWorkflowTests(unittest.TestCase):
    def test_source_containers_require_successful_credentialed_mirror(self):
        pipeline = WORKFLOW.parents[2] / '.dagu/pipeline.yaml'
        steps = {step['id']: step for step in yaml.safe_load(pipeline.read_text())['steps']}

        def ancestors(name):
            direct = set(steps[name].get('depends', []))
            return direct | {parent for item in direct for parent in ancestors(item)}

        containers = [name for name, step in steps.items()
                      if step.get('action') == 'container.run']
        self.assertTrue(containers)
        for name in containers:
            with self.subTest(step=name):
                self.assertIn('github_mirror', ancestors(name))
        self.assertEqual(ancestors('github_mirror'), {'checkout', 'normalize'})

    @classmethod
    def setUpClass(cls):
        # BaseLoader preserves the GitHub Actions "on" key as a string.
        cls.workflow = yaml.load(WORKFLOW.read_text(), Loader=yaml.BaseLoader)

    def test_original_events_and_explicit_mirror_namespace(self):
        self.assertEqual(self.workflow['on'], {
            'push': {'branches': ['main', 'ci/entire/**'], 'tags': ['v*']},
            'pull_request': '',
        })
        self.assertEqual(self.workflow['permissions'], {'contents': 'read'})

    def test_ordinary_events_keep_all_original_cases_and_context_names(self):
        expected = NATIVE_CASES | {(job, 'ubuntu-latest') for job in LINUX_JOBS} | {
            ('test (ubuntu-latest)', 'ubuntu-latest'),
            ('nocgo (ubuntu-latest)', 'ubuntu-latest'),
        }
        for ref in ('refs/heads/main', 'refs/tags/v1.2.3', 'refs/pull/123/merge'):
            with self.subTest(ref=ref):
                self.assertEqual(scheduled_cases(self.workflow, ref), expected)

    def test_exact_commit_mirror_runs_only_four_native_jobs(self):
        ref = 'refs/heads/ci/entire/' + 'a' * 40
        self.assertEqual(scheduled_cases(self.workflow, ref), NATIVE_CASES)
        self.assertEqual(self.workflow['jobs']['test']['runs-on'],
                         '${{ matrix.runner || matrix.os }}')
        self.assertEqual(self.workflow['jobs']['nocgo']['runs-on'],
                         '${{ matrix.runner || matrix.os }}')

    def test_native_build_modes_and_required_runtime_commands_remain(self):
        steps = {
            job: {step.get('name'): step for step in self.workflow['jobs'][job]['steps']}
            for job in ('test', 'nocgo')
        }
        native = steps['test']
        self.assertEqual(native['Build']['run'], 'go build -v .')
        # Runtime evidence must come from this VM, never a Go test-result cache.
        self.assertEqual(native['Test']['run'], 'go test -count=1 -v ./...')
        self.assertEqual(native['Verify native macOS ARM64 runner']['run'],
                         'test "$(uname -m)" = arm64\n'
                         'test "$(go env GOARCH)" = arm64\n'
                         'test "$(go env CGO_ENABLED)" = 1\n')
        arm_pure = native['Test Pure-Go macOS ARM64 build']
        self.assertEqual(arm_pure['if'], "runner.os == 'macOS'")
        self.assertEqual(arm_pure['env'], {'CGO_ENABLED': '0'})
        self.assertEqual(arm_pure['run'], 'go test -count=1 -v ./...')
        pure = steps['nocgo']
        self.assertEqual(pure['Test Pure-Go build']['env'], {'CGO_ENABLED': '0'})
        self.assertEqual(pure['Test Pure-Go build']['run'], 'go test -count=1 -v ./...')
        self.assertEqual(pure['Verify Pure-Go macOS Intel runner']['run'],
                         'test "$(uname -m)" = x86_64\n'
                         'test "$(go env GOARCH)" = amd64\n')
        for step in (native['Test Pure-Go macOS ARM64 input preflight'],
                     pure['Test Pure-Go macOS input preflight']):
            self.assertEqual(step['env'], {'CGO_ENABLED': '0'})
            self.assertEqual(step['run'], 'go test -run "^TestPureGoDarwinInputRuntime$" '
                             '-count=1 -timeout=30s -v .')
        for name, tag, pattern, count, timeout in (
            ('Test Pure-Go Windows input desktop', 'windowsintegration',
             '^TestPureGoWindowsInputRuntime$', 1, 30),
            ('Test Pure-Go Windows window and paste desktop', 'windowsintegration',
             '^TestPureGoWindowsWindowRuntime$', 3, 60),
        ):
            step = pure[name]
            self.assertEqual(step['if'], "runner.os == 'Windows'")
            self.assertEqual(step['env']['CGO_ENABLED'], '0')
            self.assertEqual(step['run'], f'go test -tags {tag} -run "{pattern}" '
                             f'-count={count} -timeout={timeout}s -v .')
        self.assertEqual(native['Test bounded Windows UI Automation fixture']['run'],
                         'go test -tags windowsintegration ./internal/accessibility '
                         '-run "^TestWindowsUIAInspectsAndActsOnlyOnSelfOwnedBoundedFixture$" '
                         '-count=1 -timeout=30s -v')
        self.assertEqual(native['Test native Windows pointer and capture on disposable desktop']['run'],
                         'go test -tags desktopintegration '
                         '-run "^(TestColor|TestSize|TestMoveMouse|TestMoveMouseSmooth|TestDragMouse|'
                         'TestMoveRelative|TestMoveSmoothRelative|TestImage)$" '
                         '-count=1 -timeout=60s -v .')
        for platform in ('macOS', 'Windows'):
            variant = 'darwin-cgo' if platform == 'macOS' else 'windows-cgo'
            self.assertEqual(native[f'Check native {platform} public Go API']['run'],
                             f'go run ./internal/cmd/apicompat -variant {variant}')


if __name__ == '__main__':
    unittest.main()
