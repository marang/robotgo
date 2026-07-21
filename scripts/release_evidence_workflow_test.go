package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseEvidenceRequiresEveryHostedSwayCell(t *testing.T) {
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
	} {
		count := 0
		for _, field := range strings.Fields(text) {
			if strings.Trim(field, "'\"\\()") == check {
				count++
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
	if !strings.Contains(text, "(.checks | length) == 21") {
		t.Fatal("release evidence does not require the expanded exact check count")
	}
	if !strings.Contains(text, "then .provider == \"github-actions\"") {
		t.Fatal("release evidence does not bind hosted Sway checks to GitHub Actions")
	}
}
