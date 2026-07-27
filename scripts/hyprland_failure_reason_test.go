//go:build linux

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHyprlandFailureReasonReaderAllowsOnlyOwnedStageEnum(t *testing.T) {
	t.Parallel()

	runnerTemp := t.TempDir()
	reasonFile := filepath.Join(
		runnerTemp,
		"hyprland-hyprland-window-failure-reason",
	)
	if err := os.WriteFile(reasonFile, []byte("compositor-start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := runHyprlandFailureReasonReader(runnerTemp, reasonFile)
	if err != nil {
		t.Fatalf("reader failed: %v: %s", err, output)
	}
	const expected = "isolated Hyprland evidence failed at sanitized stage: compositor-start\n"
	if output != expected {
		t.Fatalf("reader output = %q, want %q", output, expected)
	}
}

func TestHyprlandFailureReasonReaderRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		content string
		mode    os.FileMode
		mutate  func(*testing.T, string, string)
	}{
		{name: "unknown", content: "private detail\n", mode: 0o600},
		{name: "multiple lines", content: "compositor-start\nsummary\n", mode: 0o600},
		{name: "nul byte", content: "compositor-start\x00private detail\n", mode: 0o600},
		{name: "world readable", content: "compositor-start\n", mode: 0o644},
		{
			name: "symlink",
			mutate: func(t *testing.T, runnerTemp, reasonFile string) {
				t.Helper()
				target := filepath.Join(runnerTemp, "target")
				if err := os.WriteFile(target, []byte("compositor-start\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, reasonFile); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runnerTemp := t.TempDir()
			reasonFile := filepath.Join(
				runnerTemp,
				"hyprland-hyprland-window-failure-reason",
			)
			if test.mutate != nil {
				test.mutate(t, runnerTemp, reasonFile)
			} else {
				if err := os.WriteFile(reasonFile, []byte(test.content), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(reasonFile, test.mode); err != nil {
					t.Fatal(err)
				}
			}
			output, err := runHyprlandFailureReasonReader(runnerTemp, reasonFile)
			if err == nil {
				t.Fatalf("unsafe reason succeeded: %q", output)
			}
			if test.content != "" && strings.Contains(output, test.content) {
				t.Fatalf("reader disclosed unsafe content %q", output)
			}
		})
	}
}

func runHyprlandFailureReasonReader(runnerTemp, reasonFile string) (string, error) {
	command := exec.Command(
		"bash",
		"./read-hyprland-failure-reason.sh",
		runnerTemp,
		reasonFile,
	)
	output, err := command.CombinedOutput()
	return string(output), err
}
