package scripts

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/marang/robotgo/internal/supportmatrix"
)

func TestRuntimeSupportContractMatchesDocsAndReleaseGate(t *testing.T) {
	t.Parallel()

	contract, err := supportmatrix.Load("../docs/compatibility/runtime-v1.json")
	if err != nil {
		t.Fatalf("load runtime support contract: %v", err)
	}
	if got := len(contract.ReleaseChecks); got != 29 {
		t.Fatalf("release check count = %d, want 29", got)
	}

	document, err := os.ReadFile("../docs/compatibility/runtime-v1.md")
	if err != nil {
		t.Fatalf("read runtime compatibility document: %v", err)
	}
	expected, err := supportmatrix.ReplaceMarkdown(
		string(document),
		supportmatrix.RenderMarkdown(contract),
	)
	if err != nil {
		t.Fatalf("locate generated runtime compatibility table: %v", err)
	}
	if expected != string(document) {
		t.Fatal(
			"runtime-v1.md is stale; run go run ./internal/cmd/supportmatrix -write",
		)
	}

	manifest, err := os.ReadFile("../.github/release-required-checks.txt")
	if err != nil {
		t.Fatalf("read release check manifest: %v", err)
	}
	manifestChecks := releaseCheckManifestLines(t, manifest)
	if !slices.Equal(manifestChecks, contract.ReleaseChecks) {
		t.Fatalf(
			"release check manifest does not exactly match runtime contract\nmanifest: %q\ncontract: %q",
			manifestChecks,
			contract.ReleaseChecks,
		)
	}

	workflow, err := os.ReadFile("../.github/workflows/release-evidence.yml")
	if err != nil {
		t.Fatalf("read release-evidence workflow: %v", err)
	}
	workflowText := string(workflow)
	for _, use := range []string{
		"done < .github/release-required-checks.txt",
		"LC_ALL=C sort .github/release-required-checks.txt",
	} {
		if count := strings.Count(workflowText, use); count != 1 {
			t.Fatalf(
				"release workflow contains canonical manifest use %q %d times, want exactly once",
				use,
				count,
			)
		}
	}
}

func releaseCheckManifestLines(t *testing.T, body []byte) []string {
	t.Helper()
	normalized := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.HasSuffix(normalized, "\n") {
		t.Fatal("release check manifest must end with a newline")
	}
	normalized = strings.TrimSuffix(normalized, "\n")
	lines := strings.Split(normalized, "\n")
	for index, line := range lines {
		if line == "" || strings.TrimSpace(line) != line {
			t.Fatalf(
				"release check manifest line %d is empty or not trimmed",
				index+1,
			)
		}
	}
	return lines
}

func TestRuntimeSupportContractKeepsPermissionGrantedMacOSPending(t *testing.T) {
	t.Parallel()

	contract, err := supportmatrix.Load("../docs/compatibility/runtime-v1.json")
	if err != nil {
		t.Fatalf("load runtime support contract: %v", err)
	}
	pending := map[string]bool{
		"macos-native-permission-granted": false,
		"macos-purego-permission-granted": false,
	}
	for _, row := range contract.Rows {
		if _, tracked := pending[row.ID]; !tracked {
			continue
		}
		if row.Status != supportmatrix.StatusEvidencePending {
			t.Errorf("%s status = %q, want evidence pending", row.ID, row.Status)
		}
		if row.FollowUp == "" || row.MissingEvidence == "" {
			t.Errorf("%s lacks explicit missing evidence or follow-up", row.ID)
		}
		pending[row.ID] = true
	}
	for id, found := range pending {
		if !found {
			t.Errorf("runtime support contract omits %s", id)
		}
	}
}
