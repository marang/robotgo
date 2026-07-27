package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestHostedPortalProofUsesEphemeralGitHubKVM(t *testing.T) {
	t.Parallel()
	scriptData, err := os.ReadFile("run_hosted_portal_e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := normalizeWorkflowText(scriptData)
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
		start := strings.Index(workflow, "  hosted-portal:")
		if start < 0 {
			t.Fatalf("%s does not isolate the hosted portal job", path)
		}
		hostedJob := workflow[start:]
		hostedContract := hostedJob + "\n" + script
		for _, required := range []string{
			"github.event_name == 'release'",
			"github.event_name == 'push'",
			`'["gnome","kde"]'`,
			"inputs.desktop == 'kde'",
			"inputs.desktop == 'all'",
			`'["single-output","multi-output"]'`,
			"inputs.topology == 'multi-output'",
			"inputs.topology == 'all'",
			"runs-on: ubuntu-24.04",
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
			"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
			"persist-credentials: false",
			`ref: ${{ github.sha }}`,
			`test "$(git rev-parse HEAD)" = "$GITHUB_SHA"`,
			"test -c /dev/kvm",
			"sudo chmod 0666 /dev/kvm",
			"test -r /dev/kvm",
			"test -w /dev/kvm",
			"test -r /etc/apt/sources.list.d/ubuntu.sources",
			"Dir::Etc::sourcelist=sources.list.d/ubuntu.sources",
			"Dir::Etc::sourceparts=-",
			"qemu-system-x86",
			"packages+=(qemu-system-gui xauth xvfb)",
			`if [[ "${{ matrix.topology }}" == "multi-output" ]]`,
			`go build -o "$portal_runner" ./internal/cmd/portalrunner`,
			"exec timeout --preserve-status",
			"--signal=TERM --kill-after=2m 20m",
			"scripts/run_hosted_portal_e2e.sh",
			"xvfb-run -a",
			"-screen 0 1280x800x24 -nolisten tcp -noreset",
			`env ROBOTGO_HOSTED_XVFB=1 "${hosted_run[@]}"`,
			`manifest="infrastructure/portal-runner/${{ matrix.desktop }}/manifest.json"`,
			`state_root="$RUNNER_TEMP/robotgo-${{ matrix.desktop }}-${{ matrix.topology }}-portal-runner"`,
			`-manifest "$manifest"`,
			`"$portal_runner" build`,
			`"$portal_runner" hosted-run`,
			`-commit "$commit"`,
			`-cell "$cell"`,
			`-topology "$topology"`,
			cell,
			"Reject transient runner artifacts",
			`"$portal_runner" cleanup`,
			`cleanup_error`,
			"portal runner state is already in use",
			"timed out waiting for portal runner cleanup ownership",
			`trap 'rm -f -- "$portal_runner"' EXIT INT TERM`,
			`rm -f -- "$portal_runner"`,
			"trap - EXIT INT TERM",
			`git -C "$GITHUB_WORKSPACE" rev-parse HEAD`,
			`find "$state_root" -maxdepth 1 -type d -name 'run-*'`,
		} {
			if !strings.Contains(hostedContract, required) {
				t.Errorf("%s hosted portal job omits %q", path, required)
			}
		}
		if !strings.Contains(workflow, "permissions:\n  contents: read") {
			t.Errorf("%s omits read-only workflow permissions", path)
		}
		if !strings.Contains(workflow, "  workflow_call:\n") {
			t.Errorf("%s cannot be called by the release-evidence workflow", path)
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
			"udevadm trigger",
			"99-robotgo-kvm.rules",
			"go run ./internal/cmd/portalrunner probe",
		} {
			if strings.Contains(hostedJob, forbidden) {
				t.Errorf(
					"%s hosted portal job contains unsafe token %q",
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
