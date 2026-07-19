package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sanitizerExpectedList = "TestScreencopyDmabufFailureDoesNotCloseStdin\n" +
		"TestScreencopyTimeoutIsBounded\n" +
		"TestScreencopyWlShm\n" +
		"ok\tgithub.com/marang/robotgo/screen\t0.001s"
	sanitizerPassingOutput = "=== RUN   TestScreencopyDmabufFailureDoesNotCloseStdin\n" +
		"--- PASS: TestScreencopyDmabufFailureDoesNotCloseStdin (0.00s)\n" +
		"=== RUN   TestScreencopyTimeoutIsBounded\n" +
		"--- PASS: TestScreencopyTimeoutIsBounded (0.01s)\n" +
		"=== RUN   TestScreencopyWlShm\n" +
		"--- PASS: TestScreencopyWlShm (0.00s)\n" +
		"PASS\nok\tgithub.com/marang/robotgo/screen\t0.012s"
)

func TestWaylandSanitizerRunner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		listOutput      string
		listError       string
		listStatus      string
		runOutput       string
		runError        string
		runStatus       string
		wantStatus      int
		wantOutputParts []string
	}{
		{
			name:       "success emits listing and execution output",
			listOutput: sanitizerExpectedList,
			runOutput:  sanitizerPassingOutput,
			wantOutputParts: []string{
				"TestScreencopyDmabufFailureDoesNotCloseStdin",
				"=== RUN   TestScreencopyWlShm",
				"PASS",
			},
		},
		{
			name:       "manifest mismatch is blocking",
			listOutput: "TestScreencopyWlShm\nok\tgithub.com/marang/robotgo/screen\t0.001s",
			wantStatus: 1,
			wantOutputParts: []string{
				"TestScreencopyWlShm",
				"sanitizer test manifest mismatch",
				"TestScreencopyTimeoutIsBounded",
			},
		},
		{
			name:       "listing failure preserves diagnostics and status",
			listOutput: "listing stdout marker",
			listError:  "listing stderr marker",
			listStatus: "17",
			wantStatus: 17,
			wantOutputParts: []string{
				"listing stdout marker",
				"listing stderr marker",
			},
		},
		{
			name:       "execution failure preserves diagnostics and status",
			listOutput: sanitizerExpectedList,
			runOutput:  "execution stdout marker",
			runError:   "execution stderr marker",
			runStatus:  "23",
			wantStatus: 23,
			wantOutputParts: []string{
				"execution stdout marker",
				"execution stderr marker",
			},
		},
		{
			name:       "missing pass marker is blocking",
			listOutput: sanitizerExpectedList,
			runOutput: strings.ReplaceAll(
				sanitizerPassingOutput,
				"--- PASS: TestScreencopyTimeoutIsBounded (0.01s)\n",
				"",
			),
			wantStatus: 1,
			wantOutputParts: []string{
				"PASS",
				"sanitizer test did not pass: TestScreencopyTimeoutIsBounded",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fakeGo := writeFakeGo(t)
			command := exec.Command("bash", "run-wayland-sanitizer-tests.sh")
			command.Env = append(os.Environ(),
				"ROBOTGO_GO_BIN="+fakeGo,
				"FAKE_LIST_OUTPUT="+test.listOutput,
				"FAKE_LIST_ERROR="+test.listError,
				"FAKE_LIST_STATUS="+test.listStatus,
				"FAKE_RUN_OUTPUT="+test.runOutput,
				"FAKE_RUN_ERROR="+test.runError,
				"FAKE_RUN_STATUS="+test.runStatus,
			)
			output, err := command.CombinedOutput()
			if got := commandExitStatus(t, err); got != test.wantStatus {
				t.Fatalf("runner exit status = %d, want %d; error: %v\noutput:\n%s",
					got, test.wantStatus, err, output)
			}
			for _, part := range test.wantOutputParts {
				if !strings.Contains(string(output), part) {
					t.Errorf("runner output does not contain %q:\n%s", part, output)
				}
			}
		})
	}
}

func writeFakeGo(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-go")
	const source = `#!/usr/bin/env bash
set -euo pipefail

mode=run
for argument in "$@"; do
  if [[ "$argument" == "-list" ]]; then
    mode=list
    break
  fi
done

if [[ "$mode" == "list" ]]; then
  printf '%s\n' "${FAKE_LIST_OUTPUT:-}"
  printf '%s\n' "${FAKE_LIST_ERROR:-}" >&2
  exit "${FAKE_LIST_STATUS:-0}"
fi

printf '%s\n' "${FAKE_RUN_OUTPUT:-}"
printf '%s\n' "${FAKE_RUN_ERROR:-}" >&2
exit "${FAKE_RUN_STATUS:-0}"
`
	if err := os.WriteFile(path, []byte(source), 0o700); err != nil {
		t.Fatalf("write fake go executable: %v", err)
	}
	return path
}

func commandExitStatus(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	t.Fatalf("command did not return an exit status: %v", err)
	return -1
}
