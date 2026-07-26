//go:build linux

package portalrunner

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

type scriptedCommandExecutor struct {
	outputs []string
	errors  []error
	calls   [][]string
}

func (executor *scriptedCommandExecutor) Run(
	_ context.Context,
	name string,
	args []string,
	_ io.Reader,
	output io.Writer,
) error {
	executor.calls = append(
		executor.calls,
		append([]string{name}, args...),
	)
	index := len(executor.calls) - 1
	if index < len(executor.outputs) {
		_, _ = io.WriteString(output, executor.outputs[index])
	}
	if index < len(executor.errors) {
		return executor.errors[index]
	}
	return nil
}

func TestGHProtectedGitHubValidatesExactWorkflowRun(t *testing.T) {
	t.Parallel()

	identity := protectedRunFixture()
	executor := &scriptedCommandExecutor{outputs: []string{`{
		"id": 12345,
		"name": "RemoteDesktop E2E",
		"head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"event": "workflow_dispatch",
		"status": "queued",
		"run_attempt": 2,
		"repository": {"full_name": "marang/robotgo"},
		"head_repository": {"full_name": "marang/robotgo"},
		"ignored_by_forward_compatible_decoder": true
	}`}}
	client := newGHProtectedGitHub(executor)
	if err := client.ValidateRun(context.Background(), identity); err != nil {
		t.Fatalf("ValidateRun: %v", err)
	}
	if len(executor.calls) != 1 ||
		!slices.Contains(executor.calls[0], "repos/marang/robotgo/actions/runs/12345") {
		t.Fatalf("GitHub calls = %v", executor.calls)
	}
}

func TestGHProtectedGitHubRejectsForkOrWrongCommit(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		response string
	}{
		{
			name: "fork",
			response: `{
				"id":12345,"name":"RemoteDesktop E2E",
				"head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"event":"pull_request","status":"queued","run_attempt":2,
				"repository":{"full_name":"marang/robotgo"},
				"head_repository":{"full_name":"attacker/robotgo"}
			}`,
		},
		{
			name: "commit",
			response: `{
				"id":12345,"name":"RemoteDesktop E2E",
				"head_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"event":"workflow_dispatch","status":"queued","run_attempt":2,
				"repository":{"full_name":"marang/robotgo"},
				"head_repository":{"full_name":"marang/robotgo"}
			}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newGHProtectedGitHub(
				&scriptedCommandExecutor{outputs: []string{test.response}},
			)
			if err := client.ValidateRun(
				context.Background(),
				protectedRunFixture(),
			); err == nil || !strings.Contains(err.Error(), "does not match") {
				t.Fatalf("ValidateRun() error = %v, want exact-match rejection", err)
			}
		})
	}
}

func TestGHProtectedGitHubConfirmsOnlySuccessfulCompletion(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		status     string
		conclusion string
		wantDone   bool
		wantError  bool
	}{
		{name: "pending", status: "in_progress"},
		{name: "success", status: "completed", conclusion: "success", wantDone: true},
		{name: "failure", status: "completed", conclusion: "failure", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := `{
				"id":12345,"name":"RemoteDesktop E2E",
				"head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"event":"workflow_dispatch","status":"` + test.status + `",
				"conclusion":"` + test.conclusion + `","run_attempt":2,
				"repository":{"full_name":"marang/robotgo"},
				"head_repository":{"full_name":"marang/robotgo"}
			}`
			client := newGHProtectedGitHub(
				&scriptedCommandExecutor{outputs: []string{response}},
			)
			done, err := client.RunSucceeded(
				context.Background(),
				protectedRunFixture(),
			)
			if done != test.wantDone || (err != nil) != test.wantError {
				t.Fatalf(
					"RunSucceeded() = %t, %v; want done=%t error=%t",
					done,
					err,
					test.wantDone,
					test.wantError,
				)
			}
		})
	}
}

func TestGHProtectedGitHubKeepsRegistrationTokenOutOfArguments(t *testing.T) {
	t.Parallel()

	const token = "A2345678901234567890_secret"
	executor := &scriptedCommandExecutor{outputs: []string{
		`{"token":"` + token + `","expires_at":"` +
			time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`,
	}}
	client := newGHProtectedGitHub(executor)
	got, err := client.RegistrationToken(context.Background(), "marang/robotgo")
	if err != nil {
		t.Fatalf("RegistrationToken: %v", err)
	}
	if string(got) != token {
		t.Fatalf("token = %q, want expected test token", got)
	}
	if strings.Contains(strings.Join(executor.calls[0], " "), token) {
		t.Fatal("registration token was exposed in host command arguments")
	}
}

func TestGHProtectedGitHubDeletesOnlyOfflineExactRunner(t *testing.T) {
	t.Parallel()

	executor := &scriptedCommandExecutor{outputs: []string{
		`{"total_count":1,"runners":[{"id":91,"name":"robotgo-gnome-12345-2-remote-desktop","status":"offline","busy":false}]}`,
		"",
	}}
	client := newGHProtectedGitHub(executor)
	if err := client.DeleteRunner(
		context.Background(),
		"marang/robotgo",
		"robotgo-gnome-12345-2-remote-desktop",
	); err != nil {
		t.Fatalf("DeleteRunner: %v", err)
	}
	if len(executor.calls) != 2 ||
		!slices.Contains(
			executor.calls[1],
			"repos/marang/robotgo/actions/runners/91",
		) {
		t.Fatalf("GitHub calls = %v", executor.calls)
	}
}

func TestGHProtectedGitHubRefusesActiveRunnerDeletion(t *testing.T) {
	t.Parallel()

	executor := &scriptedCommandExecutor{
		outputs: []string{
			`{"total_count":1,"runners":[{"id":91,"name":"runner","status":"online","busy":false}]}`,
		},
		errors: []error{nil},
	}
	client := newGHProtectedGitHub(executor)
	err := client.DeleteRunner(context.Background(), "marang/robotgo", "runner")
	if err == nil || !strings.Contains(err.Error(), "still active") {
		t.Fatalf("DeleteRunner() error = %v, want active-runner rejection", err)
	}
}

func protectedRunFixture() ProtectedRunIdentity {
	return ProtectedRunIdentity{
		Repository: "marang/robotgo",
		Commit:     strings.Repeat("a", 40),
		RunID:      "12345",
		RunAttempt: 2,
		Cell:       "remote-desktop",
	}
}
