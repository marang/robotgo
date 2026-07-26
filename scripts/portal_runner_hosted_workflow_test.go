package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestHostedGNOMEProofUsesEphemeralGitHubKVM(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../.github/workflows/remote-desktop-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	start := strings.Index(workflow, "  hosted-image-and-session:")
	end := strings.Index(workflow, "\n  portal-input:")
	if start < 0 || end <= start {
		t.Fatal("RemoteDesktop workflow does not isolate the hosted proof job")
	}
	hostedJob := workflow[start:end]
	for _, required := range []string{
		"inputs.desktop == 'gnome'",
		"runs-on: ubuntu-24.04",
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
		"persist-credentials: false",
		`ref: ${{ github.sha }}`,
		`test "$(git rev-parse HEAD)" = "$GITHUB_SHA"`,
		"test -c /dev/kvm",
		"qemu-system-x86",
		`state_root="$RUNNER_TEMP/robotgo-portal-runner"`,
		"go run ./internal/cmd/portalrunner build",
		"go run ./internal/cmd/portalrunner probe",
		"Reject transient runner artifacts",
		`find "$state_root" -maxdepth 1 -type d -name 'run-*'`,
	} {
		if !strings.Contains(hostedJob, required) {
			t.Errorf("hosted GNOME proof omits %q", required)
		}
	}
	for _, required := range []string{
		"permissions:\n  contents: read",
		"(github.event_name == 'workflow_dispatch' && inputs.desktop != 'gnome')",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("RemoteDesktop workflow omits hosted routing contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"self-hosted",
		"environment:",
		"generate-jitconfig",
		"registration-token",
		"ROBOTGO_REMOTE_DESKTOP_E2E",
		"ROBOTGO_SCREENCAST_E2E",
		"actions/checkout@v",
		"actions/setup-go@v",
		"persist-credentials: true",
	} {
		if strings.Contains(hostedJob, forbidden) {
			t.Errorf("hosted GNOME proof contains unsafe or premature token %q", forbidden)
		}
	}
}
