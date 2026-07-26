//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRepositoryManifest(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	repositoryRoot := filepath.Join("..", "..", "..")
	manifest := filepath.Join(
		repositoryRoot,
		"infrastructure",
		"portal-runner",
		"gnome",
		"manifest.json",
	)
	if err := run(
		[]string{
			"validate",
			"-manifest", manifest,
			"-repository-root", repositoryRoot,
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("validate: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "valid lane=gnome repository=marang/robotgo") {
		t.Fatalf("validate output = %q", stdout.String())
	}
}

func TestValidateRepositoryKDEManifest(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	repositoryRoot := filepath.Join("..", "..", "..")
	manifest := filepath.Join(
		repositoryRoot,
		"infrastructure",
		"portal-runner",
		"kde",
		"manifest.json",
	)
	if err := run(
		[]string{
			"validate",
			"-manifest", manifest,
			"-repository-root", repositoryRoot,
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("validate: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "valid lane=kde repository=marang/robotgo") {
		t.Fatalf("validate output = %q", stdout.String())
	}
}

func TestValidatePreparesExternalPrivateState(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(
		"..",
		"..",
		"..",
		"infrastructure",
		"portal-runner",
		"gnome",
		"manifest.json",
	)
	stateRoot := filepath.Join(parent, "state")
	if err := run(
		[]string{
			"validate",
			"-manifest", manifest,
			"-repository-root", repository,
			"-state-root", stateRoot,
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("validate: %v\nstderr: %s", err, stderr.String())
	}
	info, err := os.Stat(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("state root mode = %o", info.Mode().Perm())
	}
}

func TestRunRejectsUnsafeCommandsAndProxyBinding(t *testing.T) {
	t.Parallel()
	for _, arguments := range [][]string{
		nil,
		{"unknown"},
		{"proxy", "-listen-host", "0.0.0.0"},
	} {
		if err := run(arguments, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("run(%q) succeeded", arguments)
		}
	}
}

func TestCleanupRequiresExplicitExternalStateAndRemovesRuns(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	stateRoot := filepath.Join(parent, "state")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run(
		[]string{
			"validate",
			"-manifest", filepath.Join(
				"..",
				"..",
				"..",
				"infrastructure",
				"portal-runner",
				"gnome",
				"manifest.json",
			),
			"-repository-root", repository,
			"-state-root", stateRoot,
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}
	runDirectory, err := os.MkdirTemp(stateRoot, "run-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel, err := os.OpenFile(
		filepath.Join(runDirectory, ".robotgo-portal-runner-run"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := sentinel.Close(); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := run(
		[]string{
			"cleanup",
			"-repository-root", repository,
			"-state-root", stateRoot,
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("cleanup: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(
		stdout.String(),
		"portal runner transient cleanup complete",
	) {
		t.Fatalf("cleanup output = %q", stdout.String())
	}
	if _, err := os.Lstat(runDirectory); !os.IsNotExist(err) {
		t.Fatalf("transient runtime survived cleanup: %v", err)
	}
	if err := run(
		[]string{"cleanup"},
		&bytes.Buffer{},
		&bytes.Buffer{},
	); err == nil {
		t.Fatal("cleanup without explicit state root succeeded")
	}
}
