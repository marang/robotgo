package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marang/robotgo/internal/compositorevidence"
)

func emptyEnvironment(string) string { return "" }

func TestRunRejectsMissingAndUnknownSubcommands(t *testing.T) {
	t.Parallel()
	for _, arguments := range [][]string{nil, {"unknown"}} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), arguments, &stdout, &stderr, emptyEnvironment)
		if err == nil {
			t.Fatalf("run(%v) succeeded", arguments)
		}
	}
}

func TestRunPreflightRejectsInvalidIdentityBeforeCreatingOutput(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	outputDirectory := filepath.Join(runnerTemp, "report")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"preflight",
		"-lane", "invalid",
		"-cell", "remote-desktop",
		"-runner-temp", runnerTemp,
		"-output-dir", outputDirectory,
	}, &stdout, &stderr, emptyEnvironment)
	if err == nil || !strings.Contains(err.Error(), "lane") {
		t.Fatalf("run error = %v, want lane validation", err)
	}
	if _, err := os.Lstat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid identity created output: %v", err)
	}
}

func TestRunPreflightCleansPreparedDirectoryOnValidationFailure(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	outputDirectory := filepath.Join(runnerTemp, "report")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"preflight",
		"-lane", "gnome",
		"-cell", "remote-desktop",
		"-runner-temp", runnerTemp,
		"-output-dir", outputDirectory,
		"-commit", "invalid",
		"-expected-commit", "invalid",
		"-ref", "refs/heads/test",
		"-output-count", "1",
	}, &stdout, &stderr, emptyEnvironment)
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("run error = %v, want commit validation", err)
	}
	if _, err := os.Lstat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preflight left output directory: %v", err)
	}
}

func TestRunCleanupRemovesOnlyOwnedOutput(t *testing.T) {
	t.Parallel()
	runnerTemp := t.TempDir()
	outputDirectory := filepath.Join(runnerTemp, "report")
	if err := compositorevidence.PrepareOutputDirectory(runnerTemp, outputDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		compositorevidence.RawTestLogPath(outputDirectory),
		[]byte("private"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"cleanup",
		"-runner-temp", runnerTemp,
		"-output-dir", outputDirectory,
	}, &stdout, &stderr, emptyEnvironment)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if _, err := os.Lstat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup left output directory: %v", err)
	}
}

func TestSensitiveValuesIgnoreEmptyAndShortEnvironmentValues(t *testing.T) {
	t.Parallel()
	values := sensitiveValues(func(name string) string {
		if name == envWaylandDisplay {
			return "w0"
		}
		return ""
	})
	if len(values) == 0 {
		t.Fatal("sensitiveValues omitted host result")
	}
}
