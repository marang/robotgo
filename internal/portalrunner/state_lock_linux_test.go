//go:build linux

package portalrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateLockIsExclusiveAndReleasable(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	state := filepath.Join(parent, "state")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(state, repository); err != nil {
		t.Fatal(err)
	}
	first, err := AcquireStateLock(state)
	if err != nil {
		t.Fatalf("AcquireStateLock(first): %v", err)
	}
	if _, err := AcquireStateLock(state); err == nil ||
		!strings.Contains(err.Error(), "already in use") {
		t.Fatalf("AcquireStateLock(second) error = %v, want exclusive rejection", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close(first): %v", err)
	}
	second, err := AcquireStateLock(state)
	if err != nil {
		t.Fatalf("AcquireStateLock(after release): %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close(second): %v", err)
	}
}

func TestStateLockRejectsSymlink(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	state := filepath.Join(parent, "state")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(state, repository); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(state, ".lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireStateLock(state); err == nil {
		t.Fatal("AcquireStateLock accepted a symlink")
	}
}
