package portalrunner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStateRunLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("protected portal runners are Linux-only")
	}
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	stateRoot := filepath.Join(parent, "state")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(stateRoot, repository); err != nil {
		t.Fatal(err)
	}
	runDirectory, err := CreateRun(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	privateData := filepath.Join(runDirectory, "ephemeral-overlay")
	if err := os.WriteFile(privateData, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupRun(stateRoot, runDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(runDirectory); !os.IsNotExist(err) {
		t.Fatalf("portal runner runtime survived cleanup: %v", err)
	}
	if _, err := os.Stat(stateRoot); err != nil {
		t.Fatalf("persistent state root removed: %v", err)
	}
}

func TestCleanupAbandonedRunsRemovesOnlySentinelOwnedRuntimes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("protected portal runners are Linux-only")
	}
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	stateRoot := filepath.Join(parent, "state")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(stateRoot, repository); err != nil {
		t.Fatal(err)
	}
	runDirectory, err := CreateRun(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(runDirectory, "private.log"),
		[]byte("private"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	imageDirectory := filepath.Join(stateRoot, "images")
	if err := os.Mkdir(imageDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CleanupAbandonedRuns(context.Background(), stateRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(runDirectory); !os.IsNotExist(err) {
		t.Fatalf("abandoned runtime survived cleanup: %v", err)
	}
	if _, err := os.Stat(imageDirectory); err != nil {
		t.Fatalf("persistent image directory was removed: %v", err)
	}
}

func TestCleanupAbandonedRunsRejectsForgedRuntime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("protected portal runners are Linux-only")
	}
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	stateRoot := filepath.Join(parent, "state")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(stateRoot, repository); err != nil {
		t.Fatal(err)
	}
	forged := filepath.Join(stateRoot, "run-forged")
	if err := os.Mkdir(forged, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CleanupAbandonedRuns(
		context.Background(),
		stateRoot,
	); err == nil {
		t.Fatal("forged runtime accepted for cleanup")
	}
	if _, err := os.Stat(forged); err != nil {
		t.Fatalf("forged runtime was removed: %v", err)
	}
}

func TestStateRejectsRepositoryAndUnownedCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("protected portal runners are Linux-only")
	}
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(filepath.Join(repository, "runner"), repository); err == nil {
		t.Fatal("repository-contained state root accepted")
	}

	stateRoot := filepath.Join(parent, "state")
	if err := PrepareStateRoot(stateRoot, repository); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(stateRoot, "unrelated")
	if err := os.Mkdir(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CleanupRun(stateRoot, unrelated); err == nil {
		t.Fatal("unrelated directory accepted for cleanup")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unsafe cleanup removed unrelated directory: %v", err)
	}
}

func TestStateRejectsSymlinksAndWeakPermissions(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("protected portal runners are Linux-only")
	}
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	realState := filepath.Join(parent, "real-state")
	if err := os.Mkdir(realState, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkState := filepath.Join(parent, "state-link")
	if err := os.Symlink(realState, symlinkState); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(symlinkState, repository); err == nil {
		t.Fatal("symlinked state root accepted")
	}

	weakState := filepath.Join(parent, "weak-state")
	if err := os.Mkdir(weakState, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateRoot(weakState, repository); err == nil {
		t.Fatal("group-readable state root accepted")
	}
}
