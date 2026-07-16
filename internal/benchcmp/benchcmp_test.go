package benchcmp

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCollectsSamplesAndNormalizesCPUSuffix(t *testing.T) {
	t.Parallel()

	input := `goos: linux
goarch: amd64
pkg: example.test/bench
BenchmarkEncode
BenchmarkEncode/small-8          1000        12.5 ns/op        32 B/op        2 allocs/op
BenchmarkEncode/small-16         1000        10.5 ns/op        28 B/op        1 allocs/op
BenchmarkDecode-8                 500        30 ns/op          3 MB/s
PASS
`

	results, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(Parse()) = %d, want 2", len(results))
	}

	encode := results["BenchmarkEncode/small"]
	assertFloats(t, encode.NanosecondsPerOperation, []float64{12.5, 10.5})
	assertFloats(t, encode.BytesPerOperation, []float64{32, 28})
	assertFloats(t, encode.AllocationsPerOperation, []float64{2, 1})

	decode := results["BenchmarkDecode"]
	assertFloats(t, decode.NanosecondsPerOperation, []float64{30})
	if len(decode.BytesPerOperation) != 0 || len(decode.AllocationsPerOperation) != 0 {
		t.Fatalf("unexpected memory samples for BenchmarkDecode: %+v", decode)
	}
}

func TestParseRejectsMalformedBenchmarkResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "invalid iterations",
			input:   "BenchmarkBroken-8 nope 1 ns/op\n",
			wantErr: "invalid iteration count",
		},
		{
			name:    "unpaired metric",
			input:   "BenchmarkBroken-8 1 12 ns/op 4\n",
			wantErr: "metric value without unit",
		},
		{
			name:    "invalid metric value",
			input:   "BenchmarkBroken-8 1 nope ns/op\n",
			wantErr: "invalid metric value",
		},
		{
			name:    "unsupported metrics only",
			input:   "BenchmarkBroken-8 1 12 MB/s\n",
			wantErr: "no supported metrics",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Parse() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestParseRejectsMissingBenchmark(t *testing.T) {
	t.Parallel()

	_, err := Parse(strings.NewReader("ok example.test/bench 0.001s\n"))
	if err == nil || !strings.Contains(err.Error(), "no benchmark results found") {
		t.Fatalf("Parse() error = %v, want missing benchmark error", err)
	}
}

func TestParseReportsReaderErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("read failed")
	_, err := Parse(errorReader{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("Parse() error = %v, want wrapping %v", err, want)
	}
}

func TestMedian(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []float64
		want   float64
	}{
		{name: "odd", values: []float64{9, 1, 3}, want: 3},
		{name: "even", values: []float64{10, 2, 6, 4}, want: 5},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			original := append([]float64(nil), test.values...)
			got, err := Median(test.values)
			if err != nil {
				t.Fatalf("Median() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Median() = %v, want %v", got, test.want)
			}
			assertFloats(t, test.values, original)
		})
	}
}

func TestSummarizeReportsQuartilesAndDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	values := []float64{4, 1, 3, 2}
	original := append([]float64(nil), values...)
	got, err := Summarize(values)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	want := Distribution{Count: 4, First: 1.75, Median: 2.5, Third: 3.25}
	if got != want {
		t.Fatalf("Summarize() = %+v, want %+v", got, want)
	}
	assertFloats(t, values, original)
}

func TestValidateCompleteness(t *testing.T) {
	t.Parallel()

	complete := Results{
		"BenchmarkOne": {
			NanosecondsPerOperation: []float64{1, 2},
			BytesPerOperation:       []float64{3, 4},
			AllocationsPerOperation: []float64{5, 6},
		},
	}
	if err := ValidateCompleteness(complete, 1, 2); err != nil {
		t.Fatalf("ValidateCompleteness() error = %v", err)
	}
	for _, test := range []struct {
		name               string
		results            Results
		expectedBenchmarks int
		expectedSamples    int
		want               string
	}{
		{name: "benchmark count", results: complete, expectedBenchmarks: 2, expectedSamples: 2, want: "got 1 benchmarks, want 2"},
		{name: "sample count", results: complete, expectedBenchmarks: 1, expectedSamples: 3, want: "has 2 samples, want 3"},
		{name: "missing metric", results: Results{"BenchmarkOne": {NanosecondsPerOperation: []float64{1}}}, expectedBenchmarks: 1, expectedSamples: 1, want: "B/op has 0 samples, want 1"},
		{name: "negative expectation", results: complete, expectedBenchmarks: -1, want: "must be non-negative"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCompleteness(test.results, test.expectedBenchmarks, test.expectedSamples)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateCompleteness() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateExpectedNames(t *testing.T) {
	t.Parallel()

	results := Results{
		"BenchmarkAlpha": {},
		"BenchmarkBeta":  {},
	}
	if err := ValidateExpectedNames(results, []string{"BenchmarkBeta", "BenchmarkAlpha"}); err != nil {
		t.Fatalf("ValidateExpectedNames() error = %v", err)
	}
	if err := ValidateExpectedNames(results, nil); err != nil {
		t.Fatalf("ValidateExpectedNames() disabled error = %v", err)
	}

	for _, test := range []struct {
		name     string
		expected []string
		want     []string
	}{
		{
			name:     "different names with same count",
			expected: []string{"BenchmarkAlpha", "BenchmarkGamma"},
			want:     []string{"missing: BenchmarkGamma", "unexpected: BenchmarkBeta"},
		},
		{
			name:     "duplicate manifest entry",
			expected: []string{"BenchmarkAlpha", "BenchmarkAlpha"},
			want:     []string{`duplicate expected name "BenchmarkAlpha"`},
		},
		{
			name:     "CPU suffix is not normalized",
			expected: []string{"BenchmarkAlpha-8", "BenchmarkBeta"},
			want:     []string{`expected name "BenchmarkAlpha-8" is not normalized`, `use "BenchmarkAlpha"`},
		},
		{
			name:     "empty manifest entry",
			expected: []string{"BenchmarkAlpha", ""},
			want:     []string{"invalid expected name"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateExpectedNames(results, test.expected)
			for _, want := range test.want {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("ValidateExpectedNames() error = %v, want containing %q", err, want)
				}
			}
		})
	}
}

func assertFloats(t *testing.T, got []float64, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("samples = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("samples = %v, want %v", got, want)
		}
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}
