"""Hermetic task-orchestration tests; never run Go, Xvfb or a real desktop.

Run: python3 -m unittest discover -s automation/ovh -p 'test_pipeline.py' -v
The image build and actual admitted OVH task runs are separate validation gates.
"""

import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import unittest


HERE = Path(__file__).resolve().parent
STUB = r'''#!/usr/bin/python3
import json, os, pathlib, shutil, socket, stat, sys, tempfile, time
tool = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
row = {"tool": tool, "args": args, "env": {key: os.environ.get(key) for key in (
    "HOME", "TMPDIR", "GOMAXPROCS", "GOFLAGS", "CGO_ENABLED", "DISPLAY",
    "WAYLAND_DISPLAY", "DBUS_SESSION_BUS_ADDRESS", "GOTOOLCHAIN", "GOTELEMETRY",
    "GOMODCACHE", "XDG_RUNTIME_DIR", "ROBOTGO_CI_PRIVATE_TMPROOT")}}
private = pathlib.Path(os.environ["ROBOTGO_CI_PRIVATE_TMPROOT"])
row["protocol_files"] = [str(path) for path in private.glob("test-protocol.*")]
row["private_modes"] = {key: stat.S_IMODE(pathlib.Path(os.environ[key]).stat().st_mode)
                        for key in ("HOME", "XDG_RUNTIME_DIR", "ROBOTGO_CI_PRIVATE_TMPROOT")}
if os.environ.get("PIPELINE_RUNTIME_PROBES") and tool == "govulncheck":
    with tempfile.TemporaryDirectory(prefix="TestPortalRunnerConsentGateRequestIdentityAndCancellation", dir=os.environ["TMPDIR"]) as temporary:
        path = pathlib.Path(temporary) / "qmp-control.sock"
        row["socket_path_bytes"] = len(os.fsencode(path))
        row["test_temp_mode"] = stat.S_IMODE(pathlib.Path(temporary).stat().st_mode)
        with socket.socket(socket.AF_UNIX) as connection:
            connection.bind(str(path))
        weak = pathlib.Path(temporary) / "intentionally-weak-fixture"
        weak.mkdir(mode=0o755)
        row["weak_fixture_mode"] = stat.S_IMODE(weak.stat().st_mode)
if os.environ.get("PIPELINE_READONLY_CACHE") and tool == "govulncheck":
    cache = pathlib.Path(os.environ["GOMODCACHE"])
    module = cache / "fixture.invalid/module@v1.0.0" / "nested"
    module.mkdir(parents=True)
    (module / "fixture.go").write_text("synthetic module fixture\n")
    module.chmod(0o555)
with open(os.environ["PIPELINE_TEST_LOG"], "a") as output:
    output.write(json.dumps(row) + "\n")
if tool == os.environ.get("PIPELINE_FAIL_TOOL") and os.environ.get("PIPELINE_FAIL_MATCH", "") in " ".join(args):
    sys.exit(int(os.environ.get("PIPELINE_FAIL_CODE", "9")))
if tool == os.environ.get("PIPELINE_WAIT_TOOL"):
    time.sleep(30)
if tool == "go" and args == ["env", "GOVERSION"]:
    print(os.environ.get("PIPELINE_GO_VERSION", "go1.26.10"))
elif tool == "xkbcli" and args == ["--version"]:
    print(os.environ.get("PIPELINE_XKB_VERSION", "1.6.0"))
elif tool == "go" and args == ["clean", "-modcache"]:
    # Emulate Go's scoped removal of its intentionally read-only module dirs.
    cache = pathlib.Path(os.environ["GOMODCACHE"])
    for current, directories, files in os.walk(cache):
        pathlib.Path(current).chmod(0o700)
    shutil.rmtree(cache)
elif tool == "go" and args and args[0] == "list":
    print('{"Path":"fixture.invalid/mod","Update":{"Version":"v1.2.3"}}')
elif tool == "govulncheck":
    print('{"progress":{"message":"synthetic scanner fixture"}}')
    if os.environ.get("PIPELINE_KNOWN_VULNERABILITY"):
        print('{"finding":{"osv":"GO-FIXTURE-0001","trace":[{"module":"fixture.invalid/mod","function":"Reachable"}]}}')
        # Real v1.9.0 JSON output is an event stream with exit 0; text mode
        # produces the vulnerability verdict. Regressing to -json must fail.
        sys.exit(0 if "-json" in args else 3)
elif tool == "xvfb-run":
    # Only emulate argument routing, not an X server or runtime qualification.
    assert args[0] == "-a" and args[1] == "-s"
    os.environ["DISPLAY"] = ":777"
    os.execvpe(args[3], args[3:], os.environ)
elif tool == "go" and args and args[0] == "test":
    manifests = {
        "^TestWayland": [
            "TestWaylandVirtualKeyboardASCIIAndUnmappedUnicode",
            "TestWaylandExactTextPreflightRejectsUnmappedWithoutMutation",
            "TestWaylandKeyboardFlushFailureReconnects",
            "TestWaylandKeyboardSelectsDeterministicCapableSeatAndCleansAll",
            "TestWaylandKeyboardAcceptsSeatWithoutPhysicalKeyboardCapability",
            "TestWaylandKeyboardRejectsMissingSeat",
            "TestWaylandKeyboardProcessesRuntimeSeatChanges"],
        "^TestWaylandPublic": ["TestWaylandPublicTextPreflightFallsBackToRemoteDesktop", "TestWaylandPublicTypeStrEUsesExactRunes"],
        "^Test(GetScreenSize|GetSysScale|GetTitle)$": ["TestGetScreenSize", "TestGetSysScale", "TestGetTitle"],
        "^TestX11MissingXTestReadinessContract$": ["TestX11MissingXTestReadinessContract"],
        "^TestWaylandBuildPreservesX11Capture$": ["TestWaylandBuildPreservesX11Capture"],
    }
    if "-list" in args:
        names = manifests[args[args.index("-list") + 1]]
        if os.environ.get("PIPELINE_OMIT_TEST"):
            names = names[:-1]
        print("\n".join(names))
    elif "-run" in args and args[args.index("-run") + 1] in manifests:
        for name in manifests[args[args.index("-run") + 1]]:
            state = "SKIP" if os.environ.get("PIPELINE_SKIP_TEST") else "PASS"
            print(f"--- {state}: {name} (0.00s)")
    else:
        print("ok synthetic task fixture")
'''


class PipelineTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="robotgo-pipeline-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        (self.repo / "go.mod").write_text("module github.com/marang/robotgo\n\ngo 1.26.10\n")
        scripts = self.repo / "scripts"
        scripts.mkdir()
        # These supervisors are fixtures: no repository code is executed.
        for name in ("benchmark-x11-backends.sh", "run-wayland-sanitizer-tests.sh"):
            (scripts / name).write_text("#!/bin/bash\nset -euo pipefail\ngo fixture-supervisor \"$0\" \"$@\"\n")
        self.tools = self.root / "bin"
        self.tools.mkdir()
        for name in ("go", "govulncheck", "git", "golangci-lint", "actionlint", "xvfb-run", "xdpyinfo", "xkbcli"):
            path = self.tools / name
            path.write_text(STUB)
            path.chmod(0o755)
        self.log = self.root / "calls.jsonl"
        self.temp = self.root / "tmp"
        self.temp.mkdir()
        self.env = {
            "PATH": f"{self.tools}:/usr/bin:/bin",
            "TMPDIR": str(self.temp),
            "PIPELINE_TEST_LOG": str(self.log),
            "DISPLAY": ":real-desktop-must-not-survive",
            "WAYLAND_DISPLAY": "real-wayland-must-not-survive",
            "DBUS_SESSION_BUS_ADDRESS": "real-bus-must-not-survive",
        }

    def run_task(self, task="ci", **overrides):
        env = self.env | overrides
        # Exercise functions after the production entrypoint's clean-env step,
        # so fixture tools can be substituted without weakening production PATH.
        return subprocess.run(
            ["/bin/bash", "-euo", "pipefail", "-c", 'source "$1"; run_task "$2"',
             "pipeline-test", str(HERE / "container-ci.sh"), task],
            cwd=self.repo, env=env, text=True, capture_output=True, timeout=20,
        )

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def assert_cleaned(self):
        self.assertEqual(list(self.temp.iterdir()), [])
        for row in self.calls():
            home = row["env"]["HOME"]
            if home:
                self.assertFalse(Path(home).exists())
            private = row["env"]["ROBOTGO_CI_PRIVATE_TMPROOT"]
            if private:
                self.assertFalse(Path(private).exists())

    def test_fixed_entrypoint_rejects_commands_and_extra_arguments(self):
        marker = self.root / "must-not-exist"
        for args in ([], ["arbitrary"], [f"ci; touch {marker}"], ["ci", "security"]):
            result = subprocess.run(
                ["/bin/bash", str(HERE / "container-ci.sh"), *args],
                cwd=self.repo, env=self.env, capture_output=True, timeout=5,
            )
            self.assertEqual(result.returncode, 2)
        self.assertFalse(marker.exists())
        self.assertEqual(self.calls(), [])

    def test_ci_command_matrix_and_private_environment(self):
        result = self.run_task()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        calls = self.calls()
        commands = [" ".join([row["tool"], *row["args"]]) for row in calls]
        for wanted in (
            "go build .", "go vet ./...", "go test -race -count=1 -timeout=8m ./...",
            "go test -asan -count=1 -timeout=8m ./...", "go test -tags ocr -count=1 -timeout=8m ./...",
            "go test -tags wayland -count=1 -timeout=8m ./...",
            "go test -tags portal -count=1 -timeout=2m ./screen/portal",
            "go test -race -tags pipewire -count=1 -timeout=2m ./screen/portal",
            "go test -tags pipewire integration ./screen/portal -run ^$",
            "go test -tags integration ./input/portal -run ^$",
            "go test -tags wayland integration . ./mouse ./window -run ^$",
        ):
            self.assertIn(wanted, commands)
        api = next(row for row in calls if row["args"][:2] == ["run", "./internal/cmd/apicompat"])
        self.assertEqual(api["args"].count("-variant"), 11)
        defaults = [row for row in calls if row["args"] == ["test", "-count=1", "-timeout=8m", "./..."]]
        self.assertIn("0", [row["env"]["CGO_ENABLED"] for row in defaults])
        self.assertIn("1", [row["env"]["CGO_ENABLED"] for row in defaults])
        for row in calls:
            self.assertEqual(row["env"]["GOMAXPROCS"], "2")
            self.assertEqual(row["env"]["GOFLAGS"], "-p=2 -mod=readonly")
            self.assertEqual(row["env"]["GOTOOLCHAIN"], "local")
            self.assertEqual(row["env"]["GOTELEMETRY"], "off")
            self.assertIsNone(row["env"]["WAYLAND_DISPLAY"])
            self.assertIsNone(row["env"]["DBUS_SESSION_BUS_ADDRESS"])
            self.assertNotEqual(row["env"]["DISPLAY"], self.env["DISPLAY"])
            self.assertEqual(row["env"]["TMPDIR"], "/tmp")
            self.assertTrue(all(mode == 0o700 for mode in row["private_modes"].values()))
        protocols = [(row["env"]["ROBOTGO_CI_PRIVATE_TMPROOT"], path)
                     for row in calls for path in row["protocol_files"]]
        self.assertTrue(protocols, "test protocol logs must use the cleanup-owned private root")
        self.assertTrue(all(Path(path).parent == Path(private) for private, path in protocols))
        self.assertIn("linux-ci-complete", result.stdout)
        self.assert_cleaned()

    def test_security_is_purego_and_does_not_inventory_updates(self):
        result = self.run_task("security")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(row["args"][:1] == ["list"] for row in self.calls()))
        scanner = next(row for row in self.calls() if row["tool"] == "govulncheck")
        self.assertEqual(scanner["args"], ["./..."])
        self.assertEqual(scanner["env"]["CGO_ENABLED"], "0")
        self.assert_cleaned()

    def test_ci_requires_the_reviewed_xkb_oracle_version(self):
        result = self.run_task(PIPELINE_XKB_VERSION="1.5.0")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("the admitted XKB oracle 1.6.0 is required", result.stderr)
        self.assertFalse(any(row["args"][:1] == ["test"] for row in self.calls()))
        self.assert_cleaned()

    def test_xkb_oracle_is_source_pinned_and_not_a_system_library_override(self):
        for recipe in ("Containerfile", "Containerfile.update"):
            text = (HERE / recipe).read_text()
            self.assertIn("https://xkbcommon.org/download/libxkbcommon-1.6.0.tar.xz", text)
            self.assertIn("0edc14eccdd391514458bc5f5a4b99863ed2d651e4dd761a90abf4f46ef99c2b", text)
            self.assertIn("--prefix=/opt/robotgo-xkb-oracle", text)
            self.assertIn("-Ddefault_library=static", text)
            self.assertNotIn("LD_LIBRARY_PATH", text)

    def test_dependencies_updates_are_inventory_not_false_vulnerabilities(self):
        result = self.run_task("dependencies")
        self.assertEqual(result.returncode, 0, result.stderr)
        inventory = next(row for row in self.calls() if row["args"][:1] == ["list"])
        self.assertEqual(inventory["args"], ["list", "-mod=readonly", "-m", "-u", "-json", "all"])
        self.assertIn('"Update"', result.stdout)
        self.assertIn("not a vulnerability verdict", result.stderr)
        self.assertTrue(any(row["tool"] == "govulncheck" for row in self.calls()))
        self.assert_cleaned()

    def test_vulnerability_and_scanner_error_exit_codes_survive(self):
        for code in (3, 1):
            with self.subTest(code=code):
                result = self.run_task("dependencies", PIPELINE_FAIL_TOOL="govulncheck", PIPELINE_FAIL_CODE=str(code))
                self.assertEqual(result.returncode, code)
                self.assert_cleaned()

    def test_reachable_findings_cannot_turn_json_exit_zero_into_clean(self):
        for task in ("security", "dependencies"):
            with self.subTest(task=task):
                result = self.run_task(task, PIPELINE_KNOWN_VULNERABILITY="1")
                self.assertEqual(result.returncode, 3)
                self.assertIn("GO-FIXTURE-0001", result.stdout)
                self.assert_cleaned()

    def test_short_private_socket_paths_and_unmasked_permission_fixtures(self):
        result = self.run_task("security", PIPELINE_RUNTIME_PROBES="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        probe = next(row for row in self.calls() if row["tool"] == "govulncheck")
        self.assertLess(probe["socket_path_bytes"], 108)
        self.assertEqual(probe["test_temp_mode"], 0o700)
        self.assertEqual(probe["weak_fixture_mode"], 0o755)
        self.assertTrue(all(mode == 0o700 for mode in probe["private_modes"].values()))
        self.assert_cleaned()

    def test_readonly_module_cache_is_cleaned_without_masking_task_verdict(self):
        for code in (0, 1, 3):
            with self.subTest(code=code):
                overrides = {"PIPELINE_READONLY_CACHE": "1"}
                if code:
                    overrides |= {"PIPELINE_FAIL_TOOL": "govulncheck", "PIPELINE_FAIL_CODE": str(code)}
                start = len(self.calls())
                result = self.run_task("security", **overrides)
                self.assertEqual(result.returncode, code, result.stderr)
                fresh = self.calls()[start:]
                cleaners = [row for row in fresh if row["tool"] == "go" and row["args"] == ["clean", "-modcache"]]
                self.assertEqual(len(cleaners), 1)
                self.assertTrue(Path(cleaners[0]["env"]["GOMODCACHE"]).is_relative_to(
                    Path(cleaners[0]["env"]["HOME"]).parent))
                self.assert_cleaned()

    def test_cleanup_failure_cannot_report_success(self):
        result = self.run_task("security", PIPELINE_READONLY_CACHE="1",
                               PIPELINE_FAIL_TOOL="go", PIPELINE_FAIL_MATCH="clean -modcache")
        self.assertEqual(result.returncode, 1)
        self.assertIn("private runtime cleanup failed", result.stderr)

    def test_inventory_tool_error_is_not_a_clean_report(self):
        result = self.run_task("dependencies", PIPELINE_FAIL_TOOL="go", PIPELINE_FAIL_MATCH="list -mod=readonly")
        self.assertEqual(result.returncode, 9)
        self.assertFalse(any(row["tool"] == "govulncheck" for row in self.calls()))
        self.assert_cleaned()

    def test_ci_stops_on_test_failure(self):
        result = self.run_task(PIPELINE_FAIL_TOOL="go", PIPELINE_FAIL_MATCH="test -count=1")
        self.assertEqual(result.returncode, 9)
        self.assertNotIn("linux-ci-complete", result.stdout)
        self.assertFalse(any(row["tool"] == "golangci-lint" for row in self.calls()))
        self.assert_cleaned()

    def test_manifest_omission_or_skip_cannot_pass(self):
        for setting in ("PIPELINE_OMIT_TEST", "PIPELINE_SKIP_TEST"):
            with self.subTest(setting=setting):
                result = self.run_task(**{setting: "1"})
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("linux-ci-complete", result.stdout)
                self.assert_cleaned()

    def test_wrong_toolchain_fails_before_real_work(self):
        result = self.run_task(PIPELINE_GO_VERSION="go1.27.2")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(len(self.calls()), 1)
        self.assert_cleaned()

    def test_term_removes_private_runtime(self):
        process = subprocess.Popen(
            ["/bin/bash", "-euo", "pipefail", "-c", 'source "$1"; run_task security',
             "pipeline-test", str(HERE / "container-ci.sh")],
            cwd=self.repo, env=self.env | {"PIPELINE_WAIT_TOOL": "govulncheck"},
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True,
        )
        try:
            deadline = time.monotonic() + 5
            while not any(row["tool"] == "govulncheck" for row in self.calls()):
                if process.poll() is not None or time.monotonic() > deadline:
                    self.fail("fixture scanner did not start")
                time.sleep(0.01)
            os.killpg(process.pid, signal.SIGTERM)
            process.communicate(timeout=5)
            self.assertNotEqual(process.returncode, 0)
            self.assert_cleaned()
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate(timeout=5)

    def test_image_pins_tools_and_does_not_copy_project_source(self):
        image = (HERE / "Containerfile").read_text()
        self.assertRegex(image, r"FROM docker.io/library/golang:1\.26\.10-bookworm@sha256:[a-f0-9]{64}")
        for tool in ("govulncheck@v1.9.0", "golangci-lint@v2.11.4", "actionlint@v1.7.12"):
            self.assertIn(tool, image)
        self.assertNotIn("@latest", image)
        copies = [line for line in image.splitlines() if line.startswith("COPY")]
        self.assertEqual(copies, ["COPY --chmod=0444 container-ci.sh container-dependencies.sh /opt/robotgo-ci/"])
        self.assertIn("USER 65532:65532", image)
        self.assertIn("/usr/bin/env -i", (HERE / "container-ci.sh").read_text())


if __name__ == "__main__":
    unittest.main()
