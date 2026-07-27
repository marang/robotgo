package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marang/robotgo/internal/apicompat"
)

func TestWriteFileAtomicCleansTemporaryFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	target := filepath.Join(directory, "baseline.api")
	if err := writeFileAtomic(target, []byte("baseline\n")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(body) != "baseline\n" {
		t.Fatalf("target body = %q", body)
	}
	assertNoTemporaryBaselines(t, directory)

	directoryTarget := filepath.Join(directory, "cannot-replace")
	if err := os.Mkdir(directoryTarget, 0o755); err != nil {
		t.Fatalf("create directory target: %v", err)
	}
	if err := writeFileAtomic(directoryTarget, []byte("failure\n")); err == nil {
		t.Fatal("directory target unexpectedly replaced")
	}
	assertNoTemporaryBaselines(t, directory)
}

func TestValidateBaselineWriteSelectionRequiresOwner(t *testing.T) {
	t.Parallel()

	cfg := apicompat.Config{Variants: []apicompat.Variant{
		{Name: "native", Baseline: "native.api"},
		{Name: "feature", Baseline: "native.api"},
	}}
	if err := validateBaselineWriteSelection(cfg, cfg.Variants[1:]); err == nil {
		t.Fatal("shared baseline alias accepted without owner")
	}
	if err := validateBaselineWriteSelection(cfg, cfg.Variants); err != nil {
		t.Fatalf("owner and alias rejected: %v", err)
	}
}

func assertNoTemporaryBaselines(t *testing.T, directory string) {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(directory, ".apicompat-*"))
	if err != nil {
		t.Fatalf("glob temporary baselines: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary baselines remain: %v", matches)
	}
}
