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
	if !strings.Contains(text, "(.checks | length) == 25") {
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

func TestReleaseEvidenceCallsPortalProofBeforeCollection(t *testing.T) {
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
	} {
		if !strings.Contains(text, required) {
			t.Errorf("release evidence omits portal-gate contract %q", required)
		}
	}
}
