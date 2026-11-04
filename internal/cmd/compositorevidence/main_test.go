package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.izoone.com/robotgo/internal/compositorevidence"
)

func emptyEnvironment(string) string { return "" }

func TestRunRejectsMissingAndUnknownSubcommands(t *testing.T) {
	t.Parallel()
	for _, arguments := range [][]string{nil, {"unknown"}} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), arguments, &stdout, &stderr, emptyEnvironment)
		if err == nil {
			t.Fatalf("run(%v) succeeded", arguments)
		}
	}
}

func TestRunPreflightRejectsInvalidIdentityBeforeCreatingOutput(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	outputDirectory := filepath.Join(runnerTemp, "report")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"preflight",
		"-lane", "invalid",
		"-cell", "remote-desktop",
		"-runner-temp", runnerTemp,
		"-output-dir", outputDirectory,
	}, &stdout, &stderr, emptyEnvironment)
	if err == nil || !strings.Contains(err.Error(), "lane") {
		t.Fatalf("run error = %v, want lane validation", err)
	}
	if _, err := os.Lstat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid identity created output: %v", err)
	}
}

func TestRunPreflightCleansPreparedDirectoryOnValidationFailure(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	outputDirectory := filepath.Join(runnerTemp, "report")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"preflight",
		"-lane", "gnome",
		"-cell", "remote-desktop",
		"-runner-temp", runnerTemp,
		"-output-dir", outputDirectory,
		"-commit", "invalid",
		"-expected-commit", "invalid",
		"-ref", "refs/heads/test",
		"-output-count", "1",
	}, &stdout, &stderr, emptyEnvironment)
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("run error = %v, want commit validation", err)
	}
	if _, err := os.Lstat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preflight left output directory: %v", err)
	}
}

func TestRunCleanupRemovesOnlyOwnedOutput(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	outputDirectory := filepath.Join(runnerTemp, "report")
	if err := compositorevidence.PrepareOutputDirectory(runnerTemp, outputDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		compositorevidence.RawTestLogPath(outputDirectory),
		[]byte("private"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"cleanup",
		"-runner-temp", runnerTemp,
		"-output-dir", outputDirectory,
	}, &stdout, &stderr, emptyEnvironment)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if _, err := os.Lstat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup left output directory: %v", err)
	}
}

func TestSensitiveValuesIgnoreEmptyAndShortEnvironmentValues(t *testing.T) {
	t.Parallel()
	values := sensitiveValues(func(name string) string {
		if name == envWaylandDisplay {
			return "w0"
		}
		return ""
	})
	if len(values) == 0 {
		t.Fatal("sensitiveValues omitted host result")
	}
}

func TestDaguCLIRequiresExplicitIdentityAndVerifiesSameAttempt(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	directory := filepath.Join(runnerTemp, "report")
	commit := strings.Repeat("a", 40)
	dagu := &compositorevidence.DaguIdentity{
		AttemptID: "real-scheduler-attempt", Step: "native_input",
		SourceTree: strings.Repeat("b", 40), Image: "sha256:" + strings.Repeat("c", 64),
	}
	report := compositorevidence.PreflightReport{
		SchemaVersion: "1", Commit: commit, Ref: "refs/heads/fixture",
		Provider: compositorevidence.ProviderDagu, Workflow: "robotgo-ovh-ci", RunID: "real-scheduler-run", Dagu: dagu,
		Platform: compositorevidence.Platform{OSID: "ubuntu", OSVersion: "24.04", KernelName: "Linux", KernelRelease: "6.12.0", Architecture: "amd64", GoVersion: "go1.26.10"},
		Desktop:  compositorevidence.Desktop{Lane: compositorevidence.LaneWlroots, Cell: compositorevidence.CellNativeInput, Compositor: "sway", CompositorVersion: "1.11", OutputCount: 1},
	}
	if err := compositorevidence.PrepareOutputDirectory(runnerTemp, directory); err != nil {
		t.Fatal(err)
	}
	if err := compositorevidence.WritePreflight(directory, report); err != nil {
		t.Fatal(err)
	}
	spec, err := compositorevidence.CellNativeInput.TestSpec()
	if err != nil {
		t.Fatal(err)
	}
	log := "=== RUN   " + spec.Name + "\n--- PASS: " + spec.Name + " (1.25s)\nPASS\nok  \t" + spec.Package + "\t1.250s\n"
	if err := os.WriteFile(compositorevidence.RawTestLogPath(directory), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	flags := []string{
		"--lane", "wlroots", "--cell", "native-input", "--runner-temp", runnerTemp, "--output-dir", directory, "--expected-commit", commit,
		"--provider", "dagu", "--workflow", "robotgo-ovh-ci", "--run-id", report.RunID,
		"--dagu-attempt-id", dagu.AttemptID, "--dagu-step", dagu.Step, "--dagu-source-tree", dagu.SourceTree, "--dagu-image", dagu.Image,
	}
	// The provider identity comes only from these flags; no GitHub environment
	// variables are needed to finalize or verify native Dagu evidence.
	for _, arguments := range [][]string{append(append([]string{"finalize"}, flags...), "--test-exit-code", "0"), append([]string{"verify"}, flags...)} {
		var stdout, stderr bytes.Buffer
		if err := run(t.Context(), arguments, &stdout, &stderr, emptyEnvironment); err != nil {
			t.Fatal(err)
		}
	}
	for _, changed := range []struct{ flag, value string }{
		{"--dagu-attempt-id", "stale-attempt"},
		{"--dagu-source-tree", strings.Repeat("d", 40)},
		{"--dagu-image", "sha256:" + strings.Repeat("d", 64)},
		{"--dagu-step", "native_capture"},
		{"--provider", "github-actions"},
	} {
		t.Run(changed.flag, func(t *testing.T) {
			arguments := append([]string{"verify"}, flags...)
			for i := range arguments {
				if arguments[i] == changed.flag {
					arguments[i+1] = changed.value
					break
				}
			}
			var stdout, stderr bytes.Buffer
			if err := run(t.Context(), arguments, &stdout, &stderr, emptyEnvironment); err == nil {
				t.Fatal("mismatched CLI identity accepted")
			}
		})
	}
}

func TestDaguCLIPreflightCleansOutputWhenProvenanceMissing(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	directory := filepath.Join(runnerTemp, "report")
	commit := strings.Repeat("a", 40)
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{
		"preflight", "--lane", "wlroots", "--cell", "native-input", "--runner-temp", runnerTemp, "--output-dir", directory,
		"--commit", commit, "--expected-commit", commit, "--ref", "refs/heads/fixture", "--output-count", "1", "--require-headless-sway",
		"--provider", "dagu", "--workflow", "robotgo-ovh-ci", "--run-id", "real-run",
	}, &stdout, &stderr, emptyEnvironment)
	if err == nil || !strings.Contains(err.Error(), "attempt ID") {
		t.Fatalf("preflight error = %v, want missing real attempt ID", err)
	}
	if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preflight left private output: %v", err)
	}
}
