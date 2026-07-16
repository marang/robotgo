// Package benchcmp compares Go benchmark output without applying pass/fail
// thresholds.
package benchcmp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
)

const (
	maxBenchmarkLineBytes = 1024 * 1024
	benchmarkPrefix       = "Benchmark"
)

// Metric identifies one of the standard Go benchmark metrics compared by this
// package.
type Metric string

const (
	MetricNanosecondsPerOperation Metric = "ns/op"
	MetricBytesPerOperation       Metric = "B/op"
	MetricAllocationsPerOperation Metric = "allocs/op"
)

var orderedMetrics = [...]Metric{
	MetricNanosecondsPerOperation,
	MetricBytesPerOperation,
	MetricAllocationsPerOperation,
}

// Samples contains all parsed observations for a benchmark. Repeated result
// lines and results with different CPU suffixes are combined.
type Samples struct {
	NanosecondsPerOperation []float64
	BytesPerOperation       []float64
	AllocationsPerOperation []float64
}

// Results maps normalized benchmark names to their samples. Normalized names
// do not include Go's trailing CPU suffix (for example, "-8").
type Results map[string]Samples

// Distribution summarizes one metric without hiding its observation count or
// middle-half spread.
type Distribution struct {
	Count  int
	First  float64
	Median float64
	Third  float64
}

// Parse reads standard Go benchmark output. Non-benchmark lines and verbose
// benchmark headings are ignored; malformed benchmark result lines are
// rejected.
func Parse(r io.Reader) (Results, error) {
	if r == nil {
		return nil, errors.New("parse benchmarks: nil reader")
	}

	results := make(Results)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxBenchmarkLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], benchmarkPrefix) {
			continue
		}
		// With `go test -v -bench`, headings such as "BenchmarkEncode" are
		// printed before result lines.
		if len(fields) == 1 {
			continue
		}

		if err := addResultLine(results, fields); err != nil {
			return nil, fmt.Errorf("parse benchmarks: line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse benchmarks: %w", err)
	}
	if len(results) == 0 {
		return nil, errors.New("parse benchmarks: no benchmark results found")
	}

	return results, nil
}

func addResultLine(results Results, fields []string) error {
	if len(fields) < 4 {
		return fmt.Errorf("malformed result for %q", fields[0])
	}
	iterations, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || iterations == 0 {
		return fmt.Errorf("invalid iteration count %q for %q", fields[1], fields[0])
	}
	if (len(fields)-2)%2 != 0 {
		return fmt.Errorf("metric value without unit for %q", fields[0])
	}

	name := stripCPUSuffix(fields[0])
	samples := results[name]
	foundMetric := false
	for index := 2; index < len(fields); index += 2 {
		value, parseErr := strconv.ParseFloat(fields[index], 64)
		if parseErr != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return fmt.Errorf("invalid metric value %q for %q", fields[index], fields[0])
		}

		metric := Metric(fields[index+1])
		switch metric {
		case MetricNanosecondsPerOperation:
			samples.NanosecondsPerOperation = append(samples.NanosecondsPerOperation, value)
			foundMetric = true
		case MetricBytesPerOperation:
			samples.BytesPerOperation = append(samples.BytesPerOperation, value)
			foundMetric = true
		case MetricAllocationsPerOperation:
			samples.AllocationsPerOperation = append(samples.AllocationsPerOperation, value)
			foundMetric = true
		}
	}
	if !foundMetric {
		return fmt.Errorf("no supported metrics for %q", fields[0])
	}

	results[name] = samples
	return nil
}

func stripCPUSuffix(name string) string {
	separator := strings.LastIndexByte(name, '-')
	if separator < 0 || separator == len(name)-1 {
		return name
	}
	for _, character := range name[separator+1:] {
		if character < '0' || character > '9' {
			return name
		}
	}
	return name[:separator]
}

func (samples Samples) values(metric Metric) []float64 {
	switch metric {
	case MetricNanosecondsPerOperation:
		return samples.NanosecondsPerOperation
	case MetricBytesPerOperation:
		return samples.BytesPerOperation
	case MetricAllocationsPerOperation:
		return samples.AllocationsPerOperation
	default:
		return nil
	}
}

// Median returns the median of values without changing their order.
func Median(values []float64) (float64, error) {
	distribution, err := Summarize(values)
	if err != nil {
		return 0, err
	}
	return distribution.Median, nil
}

// Summarize returns the sample count, median, and linearly interpolated first
// and third quartiles without changing the input order.
func Summarize(values []float64) (Distribution, error) {
	if len(values) == 0 {
		return Distribution{}, errors.New("summarize: no samples")
	}

	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return Distribution{
		Count:  len(sorted),
		First:  interpolatedQuantile(sorted, 0.25),
		Median: interpolatedQuantile(sorted, 0.50),
		Third:  interpolatedQuantile(sorted, 0.75),
	}, nil
}

func interpolatedQuantile(sorted []float64, quantile float64) float64 {
	position := quantile * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return sorted[lower]
	}
	weight := position - float64(lower)
	return sorted[lower] + (sorted[upper]-sorted[lower])*weight
}

// ValidateCompleteness checks that a benchmark run contains the expected
// number of benchmarks and observations for every reported metric. A zero
// expectation disables that dimension.
func ValidateCompleteness(results Results, expectedBenchmarks int, expectedSamples int) error {
	if expectedBenchmarks < 0 || expectedSamples < 0 {
		return errors.New("validate benchmarks: expectations must be non-negative")
	}
	if expectedBenchmarks > 0 && len(results) != expectedBenchmarks {
		return fmt.Errorf("validate benchmarks: got %d benchmarks, want %d", len(results), expectedBenchmarks)
	}
	if expectedSamples == 0 {
		return nil
	}
	for name, samples := range results {
		for _, metric := range orderedMetrics {
			if count := len(samples.values(metric)); count != expectedSamples {
				return fmt.Errorf("validate benchmarks: %s %s has %d samples, want %d", name, metric, count, expectedSamples)
			}
		}
	}
	return nil
}

// ValidateExpectedNames checks that results contain exactly the normalized
// benchmark names in expected. An empty expected manifest disables the check.
func ValidateExpectedNames(results Results, expected []string) error {
	if len(expected) == 0 {
		return nil
	}

	expectedSet := make(Results, len(expected))
	for _, name := range expected {
		if name == "" || strings.TrimSpace(name) != name {
			return fmt.Errorf("validate benchmark names: invalid expected name %q", name)
		}
		if normalized := stripCPUSuffix(name); normalized != name {
			return fmt.Errorf("validate benchmark names: expected name %q is not normalized; use %q", name, normalized)
		}
		if _, duplicate := expectedSet[name]; duplicate {
			return fmt.Errorf("validate benchmark names: duplicate expected name %q", name)
		}
		expectedSet[name] = Samples{}
	}

	missing := missingNames(expectedSet, results)
	unexpected := missingNames(results, expectedSet)
	if len(missing) == 0 && len(unexpected) == 0 {
		return nil
	}

	parts := make([]string, 0, 2)
	if len(missing) != 0 {
		parts = append(parts, "missing: "+strings.Join(missing, ", "))
	}
	if len(unexpected) != 0 {
		parts = append(parts, "unexpected: "+strings.Join(unexpected, ", "))
	}
	return errors.New("validate benchmark names: " + strings.Join(parts, "; "))
}
