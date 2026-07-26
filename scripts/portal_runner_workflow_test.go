package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestProtectedPortalWorkflowsUsePinnedSingleLaneContract(t *testing.T) {
	t.Parallel()
	workflows := map[string]string{
		"../.github/workflows/remote-desktop-e2e.yml": "robotgo-remote-desktop",
		"../.github/workflows/screencast-e2e.yml":     "robotgo-screencast",
	}
	for path, cellLabel := range workflows {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		workflow := string(data)
		for _, required := range []string{
			"desktop:",
			"default: gnome",
			"- gnome",
			"- kde",
			"- all",
			"inputs.desktop == 'gnome'",
			"inputs.desktop == 'kde'",
			"inputs.desktop == 'all'",
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
			"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
			"Verify pinned guest toolchain",
			`manifest="infrastructure/portal-runner/${{ matrix.desktop }}/manifest.json"`,
			`test "$(go env GOVERSION)" = "$expected_go"`,
			`[ -f "$GITHUB_WORKSPACE/go.mod" ]`,
			`git -C "$GITHUB_WORKSPACE" rev-parse HEAD`,
			`= "$ROBOTGO_APPROVED_COMMIT"`,
			`cd "$GITHUB_WORKSPACE"`,
			`elif [ -e "$output" ] || [ -L "$output" ]; then`,
			"evidence workspace exists without a trusted checkout",
			"persist-credentials: false",
			"environment:",
			"runs-on: [self-hosted, linux, wayland, \"${{ matrix.desktop }}\", " +
				cellLabel + "]",
		} {
			if !strings.Contains(workflow, required) {
				t.Errorf("%s omits protected runner contract %q", path, required)
			}
		}
		for _, forbidden := range []string{
			"pull_request_target",
			"actions/checkout@v",
			"actions/setup-go@",
			"actions/upload-artifact@v",
			"persist-credentials: true",
		} {
			if strings.Contains(workflow, forbidden) {
				t.Errorf("%s contains unsafe protected runner token %q", path, forbidden)
			}
		}
	}
}

func TestProtectedGNOMERegistrationAddsExactCellLabel(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(
		"../infrastructure/portal-runner/gnome/guest/register.sh",
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
			t.Errorf("GNOME registration omits exact cell binding %q", required)
		}
	}
}
