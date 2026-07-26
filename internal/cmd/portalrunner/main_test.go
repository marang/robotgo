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
