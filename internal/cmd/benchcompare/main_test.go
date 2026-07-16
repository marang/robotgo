package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunWritesComparisonFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	baselinePath := writeBenchmarkFile(t, directory, "baseline.txt", 10)
	candidatePath := writeBenchmarkFile(t, directory, "candidate.txt", 5)
	outputPath := filepath.Join(directory, "comparison.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"-baseline", baselinePath,
		"-candidate", candidatePath,
		"-baseline-label", "main",
		"-candidate-label", "feature",
		"-expected-benchmarks", "1",
		"-expected-benchmark", "BenchmarkWork",
		"-expected-samples", "1",
		"-out", outputPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v, stderr = %q", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("run() stdout = %q, want empty", stdout.String())
	}

	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", outputPath, err)
	}
	if !strings.Contains(string(output), "| BenchmarkWork | ns/op | 10 [10–10] | 5 [5–5] | 0.500x | 1 / 1 |") {
		t.Fatalf("comparison output = %q", output)
	}
}

func TestRunRejectsWrongExpectedNameWithMatchingCount(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	baselinePath := writeBenchmarkFile(t, directory, "baseline.txt", 10)
	candidatePath := writeBenchmarkFile(t, directory, "candidate.txt", 5)
	outputPath := filepath.Join(directory, "comparison.md")
	const original = "keep me"
	if err := os.WriteFile(outputPath, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile() output error = %v", err)
	}

	err := run([]string{
		"-baseline", baselinePath,
		"-candidate", candidatePath,
		"-expected-benchmarks", "1",
		"-expected-benchmark", "BenchmarkOther",
		"-expected-samples", "1",
		"-out", outputPath,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	for _, want := range []string{"baseline", "missing: BenchmarkOther", "unexpected: BenchmarkWork"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("run() error = %v, want containing %q", err, want)
		}
	}
	output, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(output) != original {
		t.Fatalf("output = %q, want %q", output, original)
	}
}

func TestRunRejectsExpectedCountManifestMismatch(t *testing.T) {
	t.Parallel()

	err := run([]string{
		"-baseline", "baseline.txt",
		"-candidate", "candidate.txt",
		"-expected-benchmarks", "2",
		"-expected-benchmark", "BenchmarkOne",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-expected-benchmarks is 2") ||
		!strings.Contains(err.Error(), "1 -expected-benchmark values") {
		t.Fatalf("run() error = %v, want count/manifest mismatch", err)
	}
}

func TestRunRejectsIncompleteSamplesBeforeWriting(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	baselinePath := writeBenchmarkFile(t, directory, "baseline.txt", 10)
	candidatePath := writeBenchmarkFile(t, directory, "candidate.txt", 5)
	outputPath := filepath.Join(directory, "comparison.md")
	const original = "keep me"
	if err := os.WriteFile(outputPath, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile() output error = %v", err)
	}

	err := run([]string{
		"-baseline", baselinePath,
		"-candidate", candidatePath,
		"-expected-benchmarks", "1",
		"-expected-samples", "2",
		"-out", outputPath,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "has 1 samples, want 2") {
		t.Fatalf("run() error = %v, want incomplete sample error", err)
	}
	output, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(output) != original {
		t.Fatalf("output = %q, want %q", output, original)
	}
}

func TestRunRequiresInputFlags(t *testing.T) {
	t.Parallel()

	err := run(nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-baseline is required") {
		t.Fatalf("run() error = %v, want required baseline", err)
	}
}

func TestRunDoesNotOverwriteOutputWhenInputsDiffer(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	baselinePath := filepath.Join(directory, "baseline.txt")
	candidatePath := filepath.Join(directory, "candidate.txt")
	outputPath := filepath.Join(directory, "comparison.md")
	if err := os.WriteFile(baselinePath, []byte("BenchmarkOne-8 1 10 ns/op\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() baseline error = %v", err)
	}
	if err := os.WriteFile(candidatePath, []byte("BenchmarkTwo-8 1 10 ns/op\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() candidate error = %v", err)
	}
	const original = "keep me"
	if err := os.WriteFile(outputPath, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile() output error = %v", err)
	}

	err := run([]string{
		"-baseline", baselinePath,
		"-candidate", candidatePath,
		"-out", outputPath,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "benchmark sets differ") {
		t.Fatalf("run() error = %v, want benchmark set mismatch", err)
	}
	output, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(output) != original {
		t.Fatalf("output = %q, want %q", output, original)
	}
}

func writeBenchmarkFile(t *testing.T, directory string, name string, nanoseconds int) string {
	t.Helper()

	path := filepath.Join(directory, name)
	contents := []byte("BenchmarkWork-8 100 " + strconv.Itoa(nanoseconds) + " ns/op 8 B/op 1 allocs/op\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
