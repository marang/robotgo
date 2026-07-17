package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRejectsUnknownSubcommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"unknown"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("run error = %v, want unknown subcommand", err)
	}
}

func TestRunGenerateRequiresExpectedCGO(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"generate",
		"-out", "evidence.json",
		"-test-log", "test.log",
	}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "-expected-cgo") {
		t.Fatalf("run error = %v, want expected-CGO validation", err)
	}
}

func TestRunVerifyRequiresEvidence(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"verify"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "-evidence") {
		t.Fatalf("run error = %v, want evidence-path validation", err)
	}
}

func TestRunVerifyRequiresExpectedMatrix(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"verify", "-evidence", "evidence.json"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "-expected-matrix") {
		t.Fatalf("run error = %v, want expected-matrix validation", err)
	}
}
