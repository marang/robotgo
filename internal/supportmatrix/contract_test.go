package supportmatrix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRejectsSupportedPendingMix(t *testing.T) {
	t.Parallel()

	contract := validContract()
	contract.Rows[0].MissingEvidence = "permission-granted mutation"
	if err := contract.Validate(); err == nil ||
		!strings.Contains(err.Error(), "pending-evidence fields") {
		t.Fatalf("Validate() = %v, want supported/pending rejection", err)
	}
}

func TestValidateRejectsPendingWithoutFollowUp(t *testing.T) {
	t.Parallel()

	contract := validContract()
	contract.Rows[0] = Row{
		ID:              "macos-permission",
		Platform:        "macOS",
		BuildMode:       "Pure Go",
		Scope:           "permission-granted input",
		Status:          StatusEvidencePending,
		CurrentChecks:   []string{"test"},
		Limits:          "Not in the supported release scope.",
		MissingEvidence: "self-owned runtime evidence",
	}
	if err := contract.Validate(); err == nil ||
		!strings.Contains(err.Error(), "Linear follow-up") {
		t.Fatalf("Validate() = %v, want missing follow-up rejection", err)
	}
}

func TestValidateRejectsUnknownEvidenceCheck(t *testing.T) {
	t.Parallel()

	contract := validContract()
	contract.Rows[0].BlockingChecks = []string{"unknown"}
	if err := contract.Validate(); err == nil ||
		!strings.Contains(err.Error(), "unknown release check") {
		t.Fatalf("Validate() = %v, want unknown-check rejection", err)
	}
}

func TestValidateRejectsInvalidReleaseIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Contract)
		want   string
	}{
		{
			name: "run suffix",
			mutate: func(contract *Contract) {
				contract.ReleaseRun += "/attempts/1"
			},
			want: "exact RobotGo Actions run",
		},
		{
			name: "uppercase commit",
			mutate: func(contract *Contract) {
				contract.ReleaseCommit = strings.Repeat("A", 40)
			},
			want: "lowercase hexadecimal",
		},
		{
			name: "non-hex commit",
			mutate: func(contract *Contract) {
				contract.ReleaseCommit = strings.Repeat("z", 40)
			},
			want: "lowercase hexadecimal",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			contract := validContract()
			test.mutate(&contract)
			if err := contract.Validate(); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsUnstableOrUnsafeRowFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Row)
		want   string
	}{
		{
			name: "unstable ID",
			mutate: func(row *Row) {
				row.ID = "Linux Native"
			},
			want: "lowercase kebab-case",
		},
		{
			name: "table delimiter",
			mutate: func(row *Row) {
				row.Scope = "capture | input"
			},
			want: "Markdown table delimiter",
		},
		{
			name: "newline",
			mutate: func(row *Row) {
				row.Limits = "first\nsecond"
			},
			want: "newline",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			contract := validContract()
			test.mutate(&contract.Rows[0])
			if err := contract.Validate(); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestLoadRejectsUnknownAndTrailingJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown field",
			body: `{"schemaVersion":1,"unknown":true}`,
			want: "unknown field",
		},
		{
			name: "second value",
			body: `{"schemaVersion":1} {}`,
			want: "trailing JSON value",
		},
		{
			name: "malformed trailing data",
			body: `{"schemaVersion":1} {`,
			want: "trailing data",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "contract.json")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if _, err := Load(path); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestRenderAndReplaceMarkdown(t *testing.T) {
	t.Parallel()

	contract := validContract()
	rendered := RenderMarkdown(contract)
	if !strings.Contains(rendered, "| `linux` |") ||
		!strings.Contains(rendered, "Blocking checks: `test`") {
		t.Fatalf("rendered table = %q", rendered)
	}
	replaced, err := ReplaceMarkdown(
		"before\n"+MarkdownStart+"\nstale\n"+MarkdownEnd+"\nafter",
		rendered,
	)
	if err != nil {
		t.Fatalf("ReplaceMarkdown: %v", err)
	}
	if strings.Contains(replaced, "stale") ||
		!strings.HasPrefix(replaced, "before\n"+MarkdownStart) ||
		!strings.HasSuffix(replaced, MarkdownEnd+"\nafter") {
		t.Fatalf("replaced document = %q", replaced)
	}
}

func TestReplaceMarkdownPreservesCRLFDocument(t *testing.T) {
	t.Parallel()

	document := "before\r\n" + MarkdownStart + "\r\nstale\r\n" +
		MarkdownEnd + "\r\nafter\r\n"
	replaced, err := ReplaceMarkdown(document, RenderMarkdown(validContract()))
	if err != nil {
		t.Fatalf("ReplaceMarkdown: %v", err)
	}
	withoutCRLF := strings.ReplaceAll(replaced, "\r\n", "")
	if strings.Contains(withoutCRLF, "\n") {
		t.Fatalf("replaced document contains mixed line endings: %q", replaced)
	}
	if !strings.HasSuffix(replaced, "\r\nafter\r\n") {
		t.Fatalf("replaced document lost CRLF suffix: %q", replaced)
	}
}

func validContract() Contract {
	return Contract{
		SchemaVersion: SchemaVersion,
		Published:     "2026-07-27",
		ReleaseRun:    "https://github.com/marang/robotgo/actions/runs/1",
		ReleaseCommit: strings.Repeat("a", 40),
		ReleaseChecks: []string{"test"},
		Rows: []Row{{
			ID:             "linux",
			Platform:       "Linux",
			BuildMode:      "Native",
			Scope:          "test scope",
			Status:         StatusSupported,
			BlockingChecks: []string{"test"},
			Limits:         "Explicit limit.",
		}},
	}
}
