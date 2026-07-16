package benchcmp

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteMarkdownUsesMediansAndCandidateBaselineRatios(t *testing.T) {
	t.Parallel()

	baseline := Results{
		"BenchmarkZed": {
			NanosecondsPerOperation: []float64{20, 10, 30, 40},
			BytesPerOperation:       []float64{8, 12},
			AllocationsPerOperation: []float64{2, 2},
		},
		"BenchmarkAlpha|escaped": {
			NanosecondsPerOperation: []float64{5},
		},
	}
	candidate := Results{
		"BenchmarkAlpha|escaped": {
			NanosecondsPerOperation: []float64{10},
		},
		"BenchmarkZed": {
			NanosecondsPerOperation: []float64{10, 20},
			BytesPerOperation:       []float64{5},
			AllocationsPerOperation: []float64{1},
		},
	}

	var output bytes.Buffer
	err := WriteMarkdown(&output, baseline, candidate, "main|base", "feature")
	if err != nil {
		t.Fatalf("WriteMarkdown() error = %v", err)
	}

	want := `| Benchmark | Metric | main\|base median [Q1–Q3] | feature median [Q1–Q3] | feature / main\|base | N (main\|base / feature) |
|---|---:|---:|---:|---:|---:|
| BenchmarkAlpha\|escaped | ns/op | 5 [5–5] | 10 [10–10] | 2.000x | 1 / 1 |
| BenchmarkZed | ns/op | 25 [17.5–32.5] | 15 [12.5–17.5] | 0.600x | 4 / 2 |
| BenchmarkZed | B/op | 10 [9–11] | 5 [5–5] | 0.500x | 2 / 1 |
| BenchmarkZed | allocs/op | 2 [2–2] | 1 [1–1] | 0.500x | 2 / 1 |
`
	if output.String() != want {
		t.Fatalf("WriteMarkdown() =\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestWriteMarkdownRejectsDifferentBenchmarkSets(t *testing.T) {
	t.Parallel()

	baseline := Results{"BenchmarkOne": {NanosecondsPerOperation: []float64{1}}}
	candidate := Results{"BenchmarkTwo": {NanosecondsPerOperation: []float64{1}}}

	err := WriteMarkdown(&bytes.Buffer{}, baseline, candidate, "base", "candidate")
	wantParts := []string{
		"benchmark sets differ",
		"missing from candidate: BenchmarkOne",
		"missing from baseline: BenchmarkTwo",
	}
	for _, want := range wantParts {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("WriteMarkdown() error = %v, want containing %q", err, want)
		}
	}
}

func TestWriteMarkdownRejectsMetricPresentOnOnlyOneSide(t *testing.T) {
	t.Parallel()

	baseline := Results{
		"BenchmarkOne": {
			NanosecondsPerOperation: []float64{1},
			BytesPerOperation:       []float64{2},
		},
	}
	candidate := Results{
		"BenchmarkOne": {NanosecondsPerOperation: []float64{1}},
	}

	err := WriteMarkdown(&bytes.Buffer{}, baseline, candidate, "base", "candidate")
	if err == nil || !strings.Contains(err.Error(), "metric B/op for BenchmarkOne is missing from candidate") {
		t.Fatalf("WriteMarkdown() error = %v, want metric mismatch", err)
	}
}

func TestWriteMarkdownMarksZeroBaselineRatioUnavailable(t *testing.T) {
	t.Parallel()

	results := Results{
		"BenchmarkZero": {NanosecondsPerOperation: []float64{0}},
	}
	var output bytes.Buffer
	if err := WriteMarkdown(&output, results, results, "base", "candidate"); err != nil {
		t.Fatalf("WriteMarkdown() error = %v", err)
	}
	if !strings.Contains(output.String(), "| 0 [0–0] | 0 [0–0] | n/a | 1 / 1 |") {
		t.Fatalf("WriteMarkdown() = %q, want unavailable zero ratio", output.String())
	}
}
