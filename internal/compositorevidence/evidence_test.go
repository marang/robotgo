package compositorevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validReport(lane Lane, cell Cell) PreflightReport {
	portal := Portal{}
	operatorReady := false
	pipeWire := PipeWire{Required: cell.PipeWireRequired()}
	if cell.PortalObserved() {
		portal = Portal{
			Observed:             true,
			Available:            true,
			FrontendVersion:      "1.20.0",
			Backend:              string(lane),
			BackendVersion:       "1.20.0",
			RemoteDesktopVersion: 2,
			ScreenCastVersion:    5,
			AvailableSources:     1,
		}
	}
	if cell.ConsentRequired() {
		operatorReady = true
	}
	if cell.PipeWireRequired() {
		pipeWire.Version = "1.2.3"
	}
	compositor := string(lane)
	workflow := "RemoteDesktop E2E"
	if lane == LaneWlroots {
		compositor = "sway"
	}
	if cell == CellScreenCast {
		workflow = "ScreenCast E2E"
	}
	if lane == LaneWlroots {
		workflow = "Sway E2E"
	}
	return PreflightReport{
		SchemaVersion: preflightSchemaVersion,
		Commit:        testCommit,
		Ref:           testRef,
		Workflow:      workflow,
		RunID:         "12345",
		RunAttempt:    2,
		Platform: Platform{
			OSID:          "ubuntu",
			OSVersion:     "24.04",
			KernelName:    "Linux",
			KernelRelease: "6.12.4-test",
			Architecture:  "amd64",
			GoVersion:     "go1.25.0",
		},
		Desktop: Desktop{
			Lane:              lane,
			Cell:              cell,
			Compositor:        compositor,
			CompositorVersion: "1.2.3",
			OutputCount:       2,
			OperatorReady:     operatorReady,
			Portal:            portal,
			PipeWire:          pipeWire,
		},
	}
}

func passedRawLog(spec TestSpec) []byte {
	return []byte(
		"=== RUN   " + spec.Name + "\n" +
			"--- PASS: " + spec.Name + " (1.25s)\n" +
			"PASS\n" +
			"ok  \t" + spec.Package + "\t1.250s\n",
	)
}

func TestSwayCellsHaveFixedEvidenceIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cell Cell
		name string
	}{
		{CellNativeInput, swayInputTestName},
		{CellNativeCapture, swayCaptureTestName},
		{CellNativeWindow, swayWindowTestName},
		{CellNativeOutput, swayOutputTestName},
		{CellPortalAvailability, swayPortalTestName},
	} {
		t.Run(string(tc.cell), func(t *testing.T) {
			t.Parallel()
			spec, err := tc.cell.TestSpec()
			if err != nil {
				t.Fatal(err)
			}
			if spec.Package != swayPackage || spec.Name != tc.name ||
				spec.Command != swayCommandPrefix+tc.name+swayCommandSuffix {
				t.Fatalf("Sway test spec = %+v", spec)
			}
			if err := validateWorkflow(tc.cell, "Sway E2E"); err != nil {
				t.Fatalf("Sway workflow rejected: %v", err)
			}
			if err := validateWorkflow(tc.cell, "RemoteDesktop E2E"); err == nil {
				t.Fatal("Sway cell accepted a portal workflow")
			}
		})
	}
}

func TestPortalAvailabilityRequiresWlrootsLane(t *testing.T) {
	t.Parallel()
	if err := validateLaneCell(LaneWlroots, CellPortalAvailability); err != nil {
		t.Fatalf("wlroots portal-availability rejected: %v", err)
	}
	for _, lane := range []Lane{LaneGNOME, LaneKDE} {
		if err := validateLaneCell(lane, CellPortalAvailability); err == nil {
			t.Fatalf("portal-availability accepted %s lane", lane)
		}
	}
}

func prepareFinalization(t *testing.T, lane Lane, cell Cell) (string, FinalizeConfig) {
	t.Helper()
	runnerTemp := t.TempDir()
	outputDirectory := filepath.Join(runnerTemp, "compositor-report")
	if err := PrepareOutputDirectory(runnerTemp, outputDirectory); err != nil {
		t.Fatal(err)
	}
	if err := WritePreflight(outputDirectory, validReport(lane, cell)); err != nil {
		t.Fatal(err)
	}
	spec, err := cell.TestSpec()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RawTestLogPath(outputDirectory), passedRawLog(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	report := validReport(lane, cell)
	return outputDirectory, FinalizeConfig{
		RunnerTemp:      runnerTemp,
		OutputDirectory: outputDirectory,
		ExpectedCommit:  testCommit,
		ExpectedLane:    lane,
		ExpectedCell:    cell,
		Workflow:        report.Workflow,
		RunID:           "12345",
		RunAttempt:      2,
		TestExitCode:    0,
		CompletedAt:     time.Date(2026, time.July, 21, 12, 30, 0, 0, time.UTC),
		SensitiveValues: []string{"wayland-private-9", "/private/home"},
	}
}

func TestFinalizeAndVerifyCreateOnlySanitizedEvidence(t *testing.T) {
	t.Parallel()
	outputDirectory, config := prepareFinalization(t, LaneGNOME, CellRemoteDesktop)
	manifest, err := Finalize(config)
	if err != nil {
		t.Fatalf("Finalize failed: %v", err)
	}
	if manifest.Test.DurationMillis != 1250 {
		t.Fatalf("duration = %dms, want 1250ms", manifest.Test.DurationMillis)
	}
	verified, err := VerifyDirectory(outputDirectory, VerifyExpectations{
		Commit:     testCommit,
		Lane:       LaneGNOME,
		Cell:       CellRemoteDesktop,
		Workflow:   config.Workflow,
		RunID:      config.RunID,
		RunAttempt: config.RunAttempt,
	})
	if err != nil {
		t.Fatalf("VerifyDirectory failed: %v", err)
	}
	if verified.Test.LogSHA256 != manifest.Test.LogSHA256 {
		t.Fatal("verified test-log digest changed")
	}
	for _, privateName := range []string{rawTestLogFileName, preflightFileName} {
		if _, err := os.Lstat(filepath.Join(outputDirectory, privateName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("private intermediate %q survived: %v", privateName, err)
		}
	}
	entries, err := os.ReadDir(outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		outputSentinelName,
		validatedMarkerName,
		evidenceFileName,
		summaryFileName,
		testLogFileName,
	}
	if len(entries) != len(want) {
		t.Fatalf("evidence files = %v, want %v", entries, want)
	}
	for index, entry := range entries {
		if entry.Name() != want[index] {
			t.Fatalf("evidence file %d = %q, want %q", index, entry.Name(), want[index])
		}
	}
	canonical, err := os.ReadFile(filepath.Join(outputDirectory, testLogFileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(canonical, []byte("=== RUN")) ||
		bytes.Contains(canonical, []byte("ok  \t")) {
		t.Fatalf("canonical log retained raw Go output: %q", canonical)
	}
}

func TestFinalizeRejectsSensitiveRawLogAndRemovesIt(t *testing.T) {
	t.Parallel()
	outputDirectory, config := prepareFinalization(t, LaneGNOME, CellRemoteDesktop)
	rawPath := RawTestLogPath(outputDirectory)
	data, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append([]byte("WAYLAND_DISPLAY=wayland-private-9\n"), data...)
	if err := os.WriteFile(rawPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Finalize(config); err == nil || !strings.Contains(err.Error(), "denylist") {
		t.Fatalf("Finalize error = %v, want denylist rejection", err)
	}
	if _, err := os.Lstat(rawPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private raw log survived rejection: %v", err)
	}
	for _, name := range []string{testLogFileName, evidenceFileName, summaryFileName, validatedMarkerName} {
		if _, err := os.Lstat(filepath.Join(outputDirectory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("artifact %q survived failed finalization: %v", name, err)
		}
	}
}

func TestFinalizeFailureAndPartialWriteCleanup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		configure func(string, *FinalizeConfig) error
	}{
		{
			name: "test failure",
			configure: func(_ string, config *FinalizeConfig) error {
				config.TestExitCode = 1
				return nil
			},
		},
		{
			name: "late output collision",
			configure: func(outputDirectory string, _ *FinalizeConfig) error {
				return os.WriteFile(filepath.Join(outputDirectory, summaryFileName), []byte("collision"), 0o600)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outputDirectory, config := prepareFinalization(t, LaneGNOME, CellRemoteDesktop)
			if err := tc.configure(outputDirectory, &config); err != nil {
				t.Fatal(err)
			}
			if _, err := Finalize(config); err == nil {
				t.Fatal("Finalize succeeded")
			}
			if _, err := os.Lstat(RawTestLogPath(outputDirectory)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("raw log survived: %v", err)
			}
			for _, name := range []string{testLogFileName, evidenceFileName, validatedMarkerName} {
				if _, err := os.Lstat(filepath.Join(outputDirectory, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("partial artifact %q survived: %v", name, err)
				}
			}
		})
	}
}

func TestVerifyRejectsUnknownManifestFieldAndTamperedLog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		tamper func(string) error
		want   string
	}{
		{
			name: "unknown field",
			tamper: func(outputDirectory string) error {
				path := filepath.Join(outputDirectory, evidenceFileName)
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var document map[string]any
				if err := json.Unmarshal(data, &document); err != nil {
					return err
				}
				document["unexpected"] = true
				data, err = json.Marshal(document)
				if err != nil {
					return err
				}
				return os.WriteFile(path, data, 0o600)
			},
			want: "unknown field",
		},
		{
			name: "test log",
			tamper: func(outputDirectory string) error {
				return os.WriteFile(
					filepath.Join(outputDirectory, testLogFileName),
					[]byte("schema=tampered\n"),
					0o600,
				)
			},
			want: "digest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			outputDirectory, config := prepareFinalization(t, LaneGNOME, CellRemoteDesktop)
			if _, err := Finalize(config); err != nil {
				t.Fatal(err)
			}
			if err := tc.tamper(outputDirectory); err != nil {
				t.Fatal(err)
			}
			_, err := VerifyDirectory(outputDirectory, VerifyExpectations{
				Commit:     testCommit,
				Lane:       LaneGNOME,
				Cell:       CellRemoteDesktop,
				Workflow:   config.Workflow,
				RunID:      config.RunID,
				RunAttempt: config.RunAttempt,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("VerifyDirectory error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParsePassedGoTestRejectsSkipAndIncompleteOutput(t *testing.T) {
	t.Parallel()
	spec, err := CellRemoteDesktop.TestSpec()
	if err != nil {
		t.Fatal(err)
	}
	for _, log := range []string{
		"=== RUN   " + spec.Name + "\n--- SKIP: " + spec.Name + " (0.00s)\nPASS\n",
		"PASS\nok  \t" + spec.Package + "\t0.1s\n",
		"=== RUN   TestUnexpected\n--- PASS: TestUnexpected (0.01s)\n" + string(passedRawLog(spec)),
		"--- PASS: " + spec.Name + " (0.01s)\n=== RUN   " + spec.Name + "\nPASS\nok  \t" + spec.Package + "\t0.1s\n",
		"=== RUN   " + spec.Name + "\n--- PASS: " + spec.Name + " (0.01s)\nok  \t" + spec.Package + "\t0.1s\nPASS\n",
		string(passedRawLog(spec)) + "unexpected trailing output\n",
		"=== RUN   " + spec.Name + "\n--- PASS: " + spec.Name + " (1.25s)\nPASS\nok  \t" + spec.Package + "\tnot-a-duration\n",
	} {
		if _, err := parsePassedGoTest([]byte(log), spec); err == nil {
			t.Fatalf("parsePassedGoTest(%q) succeeded", log)
		}
	}
}

func TestFinalizeRejectsPreflightRunIdentityMismatch(t *testing.T) {
	t.Parallel()
	outputDirectory, config := prepareFinalization(t, LaneGNOME, CellRemoteDesktop)
	config.RunID = "54321"
	if _, err := Finalize(config); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("Finalize error = %v, want preflight identity rejection", err)
	}
	if _, err := os.Lstat(RawTestLogPath(outputDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private raw log survived identity rejection: %v", err)
	}
}

func TestCleanupRequiresSentinelAndRemovesPrivateIntermediates(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	unrelated := filepath.Join(runnerTemp, "unrelated")
	if err := os.Mkdir(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(runnerTemp, unrelated); err == nil || !strings.Contains(err.Error(), "sentinel") {
		t.Fatalf("Cleanup unrelated error = %v", err)
	}

	owned := filepath.Join(runnerTemp, "owned")
	if err := PrepareOutputDirectory(runnerTemp, owned); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RawTestLogPath(owned), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Cleanup(runnerTemp, owned); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(owned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned evidence survived cleanup: %v", err)
	}
}

func TestPrepareOutputDirectoryRejectsEscapeAndExistingDirectory(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	if err := PrepareOutputDirectory(runnerTemp, filepath.Join(runnerTemp, "..", "escape")); err == nil {
		t.Fatal("PrepareOutputDirectory accepted escape")
	}
	nestedParent := filepath.Join(runnerTemp, "nested")
	if err := os.Mkdir(nestedParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareOutputDirectory(runnerTemp, filepath.Join(nestedParent, "report")); err == nil {
		t.Fatal("PrepareOutputDirectory accepted nested output path")
	}
	existing := filepath.Join(runnerTemp, "existing")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareOutputDirectory(runnerTemp, existing); err == nil {
		t.Fatal("PrepareOutputDirectory accepted existing directory")
	}
}
