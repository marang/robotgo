//go:build linux

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwayFailureReasonReaderAllowsOnlyOwnedStageEnum(t *testing.T) {
	t.Parallel()

	runnerTemp := t.TempDir()
	reasonFile := filepath.Join(runnerTemp, "sway-native-input-failure-reason")
	if err := os.WriteFile(reasonFile, []byte("preflight\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := runFailureReasonReader(runnerTemp, reasonFile)
	if err != nil {
		t.Fatalf("reader failed: %v: %s", err, output)
	}
	const expected = "isolated Sway evidence failed at sanitized stage: preflight\n"
	if output != expected {
		t.Fatalf("reader output = %q, want %q", output, expected)
	}
}

func TestSwayFailureReasonReaderRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		content string
		mode    os.FileMode
		mutate  func(*testing.T, string, string)
	}{
		{name: "unknown", content: "private detail\n", mode: 0o600},
		{name: "multiple lines", content: "preflight\nsummary\n", mode: 0o600},
		{name: "world readable", content: "preflight\n", mode: 0o644},
		{
			name: "symlink",
			mutate: func(t *testing.T, runnerTemp, reasonFile string) {
				t.Helper()
				target := filepath.Join(runnerTemp, "target")
				if err := os.WriteFile(target, []byte("preflight\n"), 0o600); err != nil {
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
				"sway-native-input-failure-reason",
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
			output, err := runFailureReasonReader(runnerTemp, reasonFile)
			if err == nil {
				t.Fatalf("unsafe reason succeeded: %q", output)
			}
			if test.content != "" && strings.Contains(output, test.content) {
				t.Fatalf("reader disclosed unsafe content %q", output)
			}
		})
	}
}

func runFailureReasonReader(runnerTemp, reasonFile string) (string, error) {
	command := exec.Command(
		"bash",
		"./read-sway-failure-reason.sh",
		runnerTemp,
		reasonFile,
	)
	output, err := command.CombinedOutput()
	return string(output), err
}
