package compositorevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validDaguIdentity(cell Cell) *DaguIdentity {
	return &DaguIdentity{
		AttemptID:  "019a4b17-f71b-7000-8000-123456789abc",
		Step:       daguStep(cell),
		SourceTree: strings.Repeat("a", 40),
		Image:      "sha256:" + strings.Repeat("b", 64),
	}
}

func prepareDaguFinalization(t *testing.T, cell Cell) (string, FinalizeConfig) {
	t.Helper()
	directory, config := prepareFinalization(t, LaneWlroots, cell)
	config.Provider = ProviderDagu
	config.Workflow = daguSwayWorkflow
	config.RunID = "019a4b17-0000-7000-8000-123456789abc"
	config.RunAttempt = 0
	config.Dagu = validDaguIdentity(cell)
	report := validReport(LaneWlroots, cell)
	report.Provider, report.Workflow, report.RunID, report.RunAttempt = config.Provider, config.Workflow, config.RunID, 0
	report.Dagu = validDaguIdentity(cell)
	if err := os.Remove(filepath.Join(directory, preflightFileName)); err != nil {
		t.Fatal(err)
	}
	if err := WritePreflight(directory, report); err != nil {
		t.Fatal(err)
	}
	return directory, config
}

func daguExpectations(config FinalizeConfig) VerifyExpectations {
	dagu := *config.Dagu
	return VerifyExpectations{
		Commit: config.ExpectedCommit, Lane: config.ExpectedLane, Cell: config.ExpectedCell,
		Provider: config.Provider, Workflow: config.Workflow, RunID: config.RunID,
		RunAttempt: config.RunAttempt, Dagu: &dagu,
	}
}

func TestDaguSwayCellsFinalizeAndVerify(t *testing.T) {
	t.Parallel()
	for _, cell := range []Cell{CellNativeInput, CellNativeCapture, CellNativeWindow, CellNativeOutput, CellNativeOutputMulti, CellPortalAvailability} {
		t.Run(string(cell), func(t *testing.T) {
			t.Parallel()
			directory, config := prepareDaguFinalization(t, cell)
			manifest, err := Finalize(config)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.CI.Provider != ProviderDagu || manifest.CI.Dagu == nil || *manifest.CI.Dagu != *config.Dagu {
				t.Fatalf("Dagu provenance lost: %+v", manifest.CI)
			}
			if _, err := VerifyDirectory(directory, daguExpectations(config)); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(directory, evidenceFileName))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte(`"run_attempt"`)) || bytes.Contains(data, []byte("github")) {
				t.Fatal("Dagu evidence included fabricated GitHub identity")
			}
			for _, private := range []string{rawTestLogFileName, preflightFileName} {
				if _, err := os.Lstat(filepath.Join(directory, private)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("private intermediate survived: %s: %v", private, err)
				}
			}
		})
	}
}

func TestDaguRejectsInvalidAndCrossProviderIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*PreflightConfig)
	}{
		{"missing provider", func(c *PreflightConfig) { c.Provider = "" }},
		{"unsupported provider", func(c *PreflightConfig) { c.Provider = "other" }},
		{"missing provenance", func(c *PreflightConfig) { c.Dagu = nil }},
		{"missing run", func(c *PreflightConfig) { c.RunID = "" }},
		{"unsafe run", func(c *PreflightConfig) { c.RunID = "run/../../private" }},
		{"numeric attempt", func(c *PreflightConfig) { c.RunAttempt = 1 }},
		{"missing attempt", func(c *PreflightConfig) { c.Dagu.AttemptID = "" }},
		{"unsafe attempt", func(c *PreflightConfig) { c.Dagu.AttemptID = "attempt\nprivate" }},
		{"wrong DAG", func(c *PreflightConfig) { c.Workflow = "robotgo-ovh" }},
		{"wrong step", func(c *PreflightConfig) { c.Dagu.Step = "native_capture" }},
		{"missing tree", func(c *PreflightConfig) { c.Dagu.SourceTree = "" }},
		{"short tree", func(c *PreflightConfig) { c.Dagu.SourceTree = "abcdef0" }},
		{"missing image", func(c *PreflightConfig) { c.Dagu.Image = "" }},
		{"mutable image", func(c *PreflightConfig) { c.Dagu.Image = "robotgo-sway:latest" }},
		{"not headless", func(c *PreflightConfig) { c.RequireHeadlessSway = false }},
		{"unsupported cell", func(c *PreflightConfig) { c.Cell = CellHyprlandWindow }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := validPreflightConfig(LaneWlroots, CellNativeInput)
			config.Provider, config.Workflow, config.RunID, config.RunAttempt = ProviderDagu, daguSwayWorkflow, "run-opaque", 0
			config.Dagu = validDaguIdentity(config.Cell)
			tc.mutate(&config)
			if err := validatePreflightConfig(config); err == nil {
				t.Fatal("invalid provider identity accepted")
			}
		})
	}
	legacy := validPreflightConfig(LaneWlroots, CellNativeInput)
	legacy.Dagu = validDaguIdentity(legacy.Cell)
	if err := validatePreflightConfig(legacy); err == nil {
		t.Fatal("GitHub evidence accepted Dagu provenance")
	}
}

func TestDaguPreflightPreservesSchedulerIdentity(t *testing.T) {
	t.Parallel()
	config := validPreflightConfig(LaneWlroots, CellNativeInput)
	config.Provider, config.Workflow, config.RunID, config.RunAttempt = ProviderDagu, daguSwayWorkflow, "run-opaque", 0
	config.Dagu = validDaguIdentity(config.Cell)
	report, err := preflight(t.Context(), config, validDependencies(&fakeProbe{}))
	if err != nil {
		t.Fatal(err)
	}
	if report.execution() != config.execution() {
		t.Fatalf("preflight replaced scheduler identity: %+v", report)
	}
}

func TestDaguFinalizationRejectsStaleProvenanceAndRemovesRawLog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*FinalizeConfig)
	}{
		{"run", func(c *FinalizeConfig) { c.RunID = "different-run" }},
		{"attempt", func(c *FinalizeConfig) { c.Dagu.AttemptID = "different-attempt" }},
		{"source tree", func(c *FinalizeConfig) { c.Dagu.SourceTree = strings.Repeat("c", 40) }},
		{"image", func(c *FinalizeConfig) { c.Dagu.Image = "sha256:" + strings.Repeat("c", 64) }},
		{"source commit", func(c *FinalizeConfig) { c.ExpectedCommit = strings.Repeat("c", 40) }},
		{"GitHub provider", func(c *FinalizeConfig) {
			c.Provider, c.Dagu, c.Workflow, c.RunID, c.RunAttempt = ProviderGitHubActions, nil, "Sway E2E", "12345", 2
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory, config := prepareDaguFinalization(t, CellNativeInput)
			tc.mutate(&config)
			if _, err := Finalize(config); err == nil || !strings.Contains(err.Error(), "identity") {
				t.Fatalf("Finalize error = %v, want identity mismatch", err)
			}
			if _, err := os.Lstat(RawTestLogPath(directory)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("raw log survived: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(directory, validatedMarkerName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed evidence was marked validated: %v", err)
			}
		})
	}
}

func TestDaguVerificationRejectsWrongJobAndProvenance(t *testing.T) {
	t.Parallel()
	directory, config := prepareDaguFinalization(t, CellNativeInput)
	if _, err := Finalize(config); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*VerifyExpectations)
	}{
		{"provider", func(e *VerifyExpectations) {
			e.Provider, e.Dagu, e.Workflow, e.RunID, e.RunAttempt = ProviderGitHubActions, nil, "Sway E2E", "12345", 2
		}},
		{"run", func(e *VerifyExpectations) { e.RunID = "new-run" }},
		{"attempt", func(e *VerifyExpectations) { e.Dagu.AttemptID = "new-attempt" }},
		{"tree", func(e *VerifyExpectations) { e.Dagu.SourceTree = strings.Repeat("c", 40) }},
		{"image", func(e *VerifyExpectations) { e.Dagu.Image = "sha256:" + strings.Repeat("c", 64) }},
		{"job", func(e *VerifyExpectations) { e.Cell, e.Dagu.Step = CellNativeCapture, "native_capture" }},
		{"source commit", func(e *VerifyExpectations) { e.Commit = strings.Repeat("c", 40) }},
		{"missing provenance", func(e *VerifyExpectations) { e.Dagu = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := daguExpectations(config)
			tc.mutate(&expected)
			if _, err := VerifyDirectory(directory, expected); err == nil {
				t.Fatal("mismatched protected job accepted")
			}
		})
	}
}

func TestDaguVerificationRejectsTamperedOrMissingProvenance(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{true: "missing", false: "tampered"}[missing], func(t *testing.T) {
			directory, config := prepareDaguFinalization(t, CellNativeInput)
			manifest, err := Finalize(config)
			if err != nil {
				t.Fatal(err)
			}
			expected := daguExpectations(config)
			if missing {
				manifest.CI.Dagu = nil
			} else {
				manifest.CI.Dagu.AttemptID = "tampered-attempt"
				// Even a caller trusting altered metadata must require the original
				// validated marker to bind the complete provenance document.
				expected.Dagu.AttemptID = "tampered-attempt"
			}
			data, err := marshalDocument(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, evidenceFileName), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyDirectory(directory, expected); err == nil {
				t.Fatal("altered provenance accepted")
			}
		})
	}
}

func TestGitHubEvidenceRetainsHistoricJSONBytes(t *testing.T) {
	t.Parallel()
	report := validReport(LaneWlroots, CellNativeInput)
	legacyReport := struct {
		SchemaVersion string   `json:"schema_version"`
		Commit        string   `json:"commit"`
		Ref           string   `json:"ref"`
		Workflow      string   `json:"workflow"`
		RunID         string   `json:"run_id"`
		RunAttempt    int      `json:"run_attempt"`
		Platform      Platform `json:"platform"`
		Desktop       Desktop  `json:"desktop"`
	}{report.SchemaVersion, report.Commit, report.Ref, report.Workflow, report.RunID, report.RunAttempt, report.Platform, report.Desktop}
	current, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(legacyReport)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, legacy) {
		t.Fatalf("historic preflight bytes changed: %s", current)
	}
	directory, config := prepareFinalization(t, LaneWlroots, CellNativeInput)
	manifest, err := Finalize(config)
	if err != nil {
		t.Fatal(err)
	}
	ci, err := json.Marshal(manifest.CI)
	if err != nil {
		t.Fatal(err)
	}
	if string(ci) != `{"provider":"github-actions","workflow":"Sway E2E","run_id":"12345","run_attempt":2}` {
		t.Fatalf("historic CI bytes changed: %s", ci)
	}
	if _, err := VerifyDirectory(directory, VerifyExpectations{Commit: config.ExpectedCommit, Lane: config.ExpectedLane, Cell: config.ExpectedCell, Workflow: config.Workflow, RunID: config.RunID, RunAttempt: config.RunAttempt}); err != nil {
		t.Fatal(err)
	}
}

func TestDaguRetainsExactTestAndLogIntegrityChecks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		log  func(TestSpec) []byte
	}{
		{"wrong test", func(TestSpec) []byte { spec, _ := CellNativeCapture.TestSpec(); return passedRawLog(spec) }},
		{"skipped test", func(spec TestSpec) []byte {
			return []byte("=== RUN   " + spec.Name + "\n--- SKIP: " + spec.Name + " (0.00s)\nPASS\n")
		}},
		{"private output", func(spec TestSpec) []byte { return append([]byte("WAYLAND_DISPLAY=private\n"), passedRawLog(spec)...) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory, config := prepareDaguFinalization(t, CellNativeInput)
			spec, err := CellNativeInput.TestSpec()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(RawTestLogPath(directory), tc.log(spec), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Finalize(config); err == nil {
				t.Fatal("invalid integration result accepted")
			}
			if _, err := os.Lstat(RawTestLogPath(directory)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected raw log survived: %v", err)
			}
		})
	}
	for _, name := range []string{testLogFileName, validatedMarkerName} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory, config := prepareDaguFinalization(t, CellNativeInput)
			if _, err := Finalize(config); err != nil {
				t.Fatal(err)
			}
			if name == testLogFileName {
				if err := os.WriteFile(filepath.Join(directory, name), []byte("tampered log\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(filepath.Join(directory, name)); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyDirectory(directory, daguExpectations(config)); err == nil {
				t.Fatal("unvalidated or tampered evidence accepted")
			}
		})
	}
}
