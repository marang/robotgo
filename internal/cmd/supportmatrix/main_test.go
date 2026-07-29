package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testContract = `{
  "schemaVersion": 1,
  "published": "2026-07-27",
  "releaseRun": "https://github.com/marang/robotgo/actions/runs/1",
  "releaseCommit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "releaseChecks": ["test"],
  "rows": [{
    "id": "linux-native",
    "platform": "Linux",
    "buildMode": "Native",
    "scope": "test scope",
    "status": "supported",
    "blockingChecks": ["test"],
    "limits": "Explicit limit."
  }]
}`

func TestRunChecksAndWritesGeneratedMatrix(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	contractPath := filepath.Join(directory, "runtime.json")
	markdownPath := filepath.Join(directory, "runtime.md")
	writeFile(t, contractPath, testContract)
	writeFile(
		t,
		markdownPath,
		"before\n<!-- BEGIN GENERATED RUNTIME SUPPORT MATRIX -->\nstale\n"+
			"<!-- END GENERATED RUNTIME SUPPORT MATRIX -->\nafter\n",
	)
	args := []string{"-contract", contractPath, "-markdown", markdownPath}

	if err := run(args); err == nil || !strings.Contains(err.Error(), "is stale") {
		t.Fatalf("run(check stale) = %v, want stale error", err)
	}
	if err := run(append(args, "-write")); err != nil {
		t.Fatalf("run(write): %v", err)
	}
	if err := run(args); err != nil {
		t.Fatalf("run(check current): %v", err)
	}

	document, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read generated document: %v", err)
	}
	text := string(document)
	if !strings.HasPrefix(text, "before\n") ||
		!strings.HasSuffix(text, "\nafter\n") ||
		strings.Contains(text, "\nstale\n") ||
		!strings.Contains(text, "| `linux-native` |") {
		t.Fatalf("generated document = %q", text)
	}
}

func TestRunRejectsPositionalArguments(t *testing.T) {
	t.Parallel()

	if err := run([]string{"unexpected"}); err == nil ||
		!strings.Contains(err.Error(), "unexpected positional arguments") {
		t.Fatalf("run() = %v, want unexpected-argument error", err)
	}
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
