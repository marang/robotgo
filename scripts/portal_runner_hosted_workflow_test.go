package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestHostedGNOMEProofUsesEphemeralGitHubKVM(t *testing.T) {
	t.Parallel()
	for path, cell := range map[string]string{
		"../.github/workflows/remote-desktop-e2e.yml": "remote-desktop",
		"../.github/workflows/screencast-e2e.yml":     "screencast",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// Git may check workflow files out with CRLF on Windows. Normalize the
		// text before asserting YAML fragments so this policy test remains
		// independent of the runner's checkout line endings.
		workflow := normalizeWorkflowText(data)
		start := strings.Index(workflow, "  hosted-gnome:")
		if start < 0 {
			t.Fatalf("%s does not isolate the hosted GNOME job", path)
		}
		end := strings.Index(workflow[start:], "\n  portal-")
		if end <= 0 {
			t.Fatalf("%s does not terminate the hosted GNOME job", path)
		}
		hostedJob := workflow[start : start+end]
		for _, required := range []string{
			"github.event_name == 'push'",
			"inputs.desktop == 'gnome'",
			"inputs.desktop == 'all'",
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
			"go run ./internal/cmd/portalrunner hosted-run",
			`-commit "$GITHUB_SHA"`,
			"-cell " + cell,
			"Reject transient runner artifacts",
			`find "$state_root" -maxdepth 1 -type d -name 'run-*'`,
		} {
			if !strings.Contains(hostedJob, required) {
				t.Errorf("%s hosted GNOME job omits %q", path, required)
			}
		}
		if !strings.Contains(workflow, "permissions:\n  contents: read") {
			t.Errorf("%s omits read-only workflow permissions", path)
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
			"go run ./internal/cmd/portalrunner probe",
		} {
			if strings.Contains(hostedJob, forbidden) {
				t.Errorf(
					"%s hosted GNOME job contains unsafe token %q",
					path,
					forbidden,
				)
			}
		}
	}
}

func TestNormalizeWorkflowTextHandlesWindowsCheckout(t *testing.T) {
	t.Parallel()
	got := normalizeWorkflowText([]byte("permissions:\r\n  contents: read\r\n"))
	want := "permissions:\n  contents: read\n"
	if got != want {
		t.Fatalf("normalizeWorkflowText() = %q, want %q", got, want)
	}
}

func normalizeWorkflowText(data []byte) string {
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}
