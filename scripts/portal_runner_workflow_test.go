package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestPortalWorkflowsUseHostedPinnedLaneContract(t *testing.T) {
	t.Parallel()
	scriptData, err := os.ReadFile("run_hosted_portal_e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := normalizeWorkflowText(scriptData)
	workflows := []string{
		"../.github/workflows/remote-desktop-e2e.yml",
		"../.github/workflows/screencast-e2e.yml",
	}
	for _, path := range workflows {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		workflow := normalizeWorkflowText(data)
		contract := workflow + "\n" + script
		for _, required := range []string{
			"desktop:",
			"default: gnome",
			"- gnome",
			"- kde",
			"- all",
			"inputs.desktop == 'kde'",
			"inputs.desktop == 'all'",
			"topology:",
			"default: single-output",
			"inputs.topology == 'multi-output'",
			"inputs.topology == 'all'",
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
			"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
			"Set up Go",
			"go-version: 1.26.x",
			"persist-credentials: false",
			"runs-on: ubuntu-24.04",
			`infrastructure/portal-runner/${{ matrix.desktop }}/manifest.json`,
			`robotgo-${{ matrix.desktop }}-${{ matrix.topology }}-portal-runner`,
			`-topology "$topology"`,
		} {
			if !strings.Contains(contract, required) {
				t.Errorf("%s omits protected runner contract %q", path, required)
			}
		}
		for _, forbidden := range []string{
			"pull_request_target",
			"actions/checkout@v",
			"actions/setup-go@v",
			"actions/upload-artifact@v",
			"persist-credentials: true",
			"runs-on: [self-hosted",
			"environment:",
			"ROBOTGO_REMOTE_DESKTOP_E2E",
			"ROBOTGO_SCREENCAST_E2E",
		} {
			if strings.Contains(workflow, forbidden) {
				t.Errorf("%s contains unsafe protected runner token %q", path, forbidden)
			}
		}
	}
}

func TestProtectedPortalRegistrationAddsExactCellLabel(t *testing.T) {
	t.Parallel()
	for _, lane := range []string{"gnome", "kde"} {
		data, err := os.ReadFile(
			"../infrastructure/portal-runner/" + lane + "/guest/register.sh",
		)
		if err != nil {
			t.Fatal(err)
		}
		script := string(data)
		for _, required := range []string{
			"runner_label=robotgo-$cell",
			`--labels "$labels,$runner_label"`,
			"--no-default-labels",
			"--ephemeral",
		} {
			if !strings.Contains(script, required) {
				t.Errorf("%s registration omits exact cell binding %q", lane, required)
			}
		}
	}
}
