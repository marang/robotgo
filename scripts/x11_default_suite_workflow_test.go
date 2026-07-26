package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestX11DefaultSuiteWorkflowContract(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../.github/workflows/x11-default-suite.yml")
	if err != nil {
		t.Fatalf("read X11 default-suite workflow: %v", err)
	}
	workflow := string(data)
	for _, required := range []string{
		"name: X11 default suite",
		"push:",
		"pull_request:",
		"x11-default-suite:",
		"name: x11-default-suite",
		"runs-on: ubuntu-24.04",
		`CGO_ENABLED: "1"`,
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
		"persist-credentials: false",
		`test "$(git rev-parse HEAD)" = "$GITHUB_SHA"`,
		"xvfb-run -a",
		`-s "-screen 0 1280x720x24 -nolisten tcp -noreset"`,
		`test -z "${WAYLAND_DISPLAY:-}"`,
		`test -z "${XDG_SESSION_TYPE:-}"`,
		"xdpyinfo >/dev/null",
		"go test -v ./...",
		`-run "^Test(GetScreenSize|GetSysScale|GetTitle)$"`,
		"TestGetScreenSize",
		"TestGetSysScale",
		"TestGetTitle",
		`git status --porcelain --untracked-files=all`,
		`rm -rf -- "$suite_root"`,
		`test ! -e "$suite_root"`,
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("X11 default-suite workflow omits %q", required)
		}
	}
	for _, forbidden := range []string{
		"actions/checkout@v",
		"actions/setup-go@v",
		"WAYLAND_DISPLAY=",
		"continue-on-error:",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("X11 default-suite workflow contains %q", forbidden)
		}
	}
}
