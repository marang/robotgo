package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseEvidenceRequiresEveryPromotedHostedCheck(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/release-evidence.yml")
	if err != nil {
		t.Fatalf("read release-evidence workflow: %v", err)
	}
	text := string(workflow)
	if count := strings.Count(text, "api-compat"); count != 2 {
		t.Fatalf(
			"release evidence contains api-compat %d times, want collector and package allowlist",
			count,
		)
	}
	for _, check := range []string{
		"native-capture",
		"native-input",
		"native-output",
		"native-output-multi",
		"native-window",
		"portal-availability",
		"x11-default-suite",
		"Portal capture evidence / GitHub-hosted gnome multi-output persistent capture",
		"Portal capture evidence / GitHub-hosted kde multi-output persistent capture",
		"Portal input evidence / GitHub-hosted gnome multi-output portal input",
		"Portal input evidence / GitHub-hosted kde multi-output portal input",
		"Display bounds evidence / GitHub-hosted gnome multi-output Wayland bounds",
		"Display bounds evidence / GitHub-hosted kde multi-output Wayland bounds",
		"Hyprland window evidence / hyprland-window",
	} {
		count := strings.Count(text, check)
		if !strings.Contains(check, " ") {
			count = 0
			for _, field := range strings.Fields(text) {
				if strings.Trim(field, "'\"\\()") == check {
					count++
				}
			}
		}
		if count != 3 {
			t.Fatalf(
				"release evidence contains %q %d times, want collector, package allowlist, and provider binding",
				check,
				count,
			)
		}
	}
	if !strings.Contains(text, "(.checks | length) == 29") {
		t.Fatal("release evidence does not require the expanded exact check count")
	}
	if !strings.Contains(text, "then .provider == \"github-actions\"") {
		t.Fatal("release evidence does not bind hosted checks to GitHub Actions")
	}
	for _, removed := range []string{
		"ci/circleci: build",
		`provider: "circleci"`,
		"statuses: read",
		"status-source.json",
	} {
		if strings.Contains(text, removed) {
			t.Fatalf("release evidence retains CircleCI contract %q", removed)
		}
	}
}

func TestReleaseEvidenceCallsRealDesktopProofBeforeCollection(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/release-evidence.yml")
	if err != nil {
		t.Fatalf("read release-evidence workflow: %v", err)
	}
	text := normalizeWorkflowText(workflow)
	for _, required := range []string{
		"  portal-input-evidence:",
		"name: Portal input evidence",
		"uses: ./.github/workflows/remote-desktop-e2e.yml",
		"  portal-capture-evidence:",
		"name: Portal capture evidence",
		"uses: ./.github/workflows/screencast-e2e.yml",
		"desktop: all",
		"topology: multi-output",
		"      - portal-capture-evidence",
		"      - portal-input-evidence",
		"  display-bounds-evidence:",
		"name: Display bounds evidence",
		"uses: ./.github/workflows/display-bounds-e2e.yml",
		"      - display-bounds-evidence",
		"  hyprland-window-evidence:",
		"name: Hyprland window evidence",
		"uses: ./.github/workflows/hyprland-e2e.yml",
		"      - hyprland-window-evidence",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("release evidence omits portal-gate contract %q", required)
		}
	}

	start := strings.Index(text, "  display-bounds-evidence:")
	if start < 0 {
		t.Fatal("release evidence does not isolate the display-bounds call")
	}
	end := strings.Index(text[start:], "  hyprland-window-evidence:")
	if end < 0 {
		t.Fatal("release evidence does not terminate the display-bounds call")
	}
	boundsCall := text[start : start+end]
	for _, required := range []string{
		"name: Display bounds evidence",
		"uses: ./.github/workflows/display-bounds-e2e.yml",
		"desktop: all",
	} {
		if !strings.Contains(boundsCall, required) {
			t.Errorf("display-bounds release call omits %q", required)
		}
	}
}
