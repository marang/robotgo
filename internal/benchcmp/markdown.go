package benchcmp

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const unavailableValue = "n/a"

// WriteMarkdown writes a deterministic comparison table. Ratios are candidate
// divided by baseline; the function deliberately makes no threshold or
// pass/fail decision.
func WriteMarkdown(
	w io.Writer,
	baseline Results,
	candidate Results,
	baselineLabel string,
	candidateLabel string,
) error {
	if w == nil {
		return errors.New("write benchmark comparison: nil writer")
	}
	if strings.TrimSpace(baselineLabel) == "" {
		return errors.New("write benchmark comparison: empty baseline label")
	}
	if strings.TrimSpace(candidateLabel) == "" {
		return errors.New("write benchmark comparison: empty candidate label")
	}
	if err := validateBenchmarkSets(baseline, candidate); err != nil {
		return err
	}

	names := make([]string, 0, len(baseline))
	for name := range baseline {
		names = append(names, name)
	}
	sort.Strings(names)

	var table strings.Builder
	fmt.Fprintf(
		&table,
		"| Benchmark | Metric | %s median [Q1–Q3] | %s median [Q1–Q3] | %s / %s | N (%s / %s) |\n",
		escapeCell(baselineLabel),
		escapeCell(candidateLabel),
		escapeCell(candidateLabel),
		escapeCell(baselineLabel),
		escapeCell(baselineLabel),
		escapeCell(candidateLabel),
	)
	table.WriteString("|---|---:|---:|---:|---:|---:|\n")
	for _, name := range names {
		for _, metric := range orderedMetrics {
			baselineValues := baseline[name].values(metric)
			candidateValues := candidate[name].values(metric)
			if len(baselineValues) == 0 && len(candidateValues) == 0 {
				continue
			}
			if len(baselineValues) == 0 || len(candidateValues) == 0 {
				return fmt.Errorf(
					"write benchmark comparison: metric %s for %s is missing from %s",
					metric,
					name,
					missingMetricSide(baselineValues, baselineLabel, candidateLabel),
				)
			}

			baselineDistribution, err := Summarize(baselineValues)
			if err != nil {
				return fmt.Errorf("write benchmark comparison: %s %s baseline: %w", name, metric, err)
			}
			candidateDistribution, err := Summarize(candidateValues)
			if err != nil {
				return fmt.Errorf("write benchmark comparison: %s %s candidate: %w", name, metric, err)
			}

			fmt.Fprintf(
				&table,
				"| %s | %s | %s | %s | %s | %d / %d |\n",
				escapeCell(name),
				metric,
				formatDistribution(baselineDistribution),
				formatDistribution(candidateDistribution),
				formatRatio(candidateDistribution.Median, baselineDistribution.Median),
				baselineDistribution.Count,
				candidateDistribution.Count,
			)
		}
	}

	if _, err := io.WriteString(w, table.String()); err != nil {
		return fmt.Errorf("write benchmark comparison: %w", err)
	}
	return nil
}

func validateBenchmarkSets(baseline Results, candidate Results) error {
	missingFromCandidate := missingNames(baseline, candidate)
	missingFromBaseline := missingNames(candidate, baseline)
	if len(missingFromCandidate) == 0 && len(missingFromBaseline) == 0 {
		return nil
	}

	parts := make([]string, 0, 2)
	if len(missingFromCandidate) != 0 {
		parts = append(parts, "missing from candidate: "+strings.Join(missingFromCandidate, ", "))
	}
	if len(missingFromBaseline) != 0 {
		parts = append(parts, "missing from baseline: "+strings.Join(missingFromBaseline, ", "))
	}
	return errors.New("benchmark sets differ: " + strings.Join(parts, "; "))
}

func missingNames(expected Results, actual Results) []string {
	missing := make([]string, 0)
	for name := range expected {
		if _, ok := actual[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func missingMetricSide(baselineValues []float64, baselineLabel string, candidateLabel string) string {
	if len(baselineValues) == 0 {
		return baselineLabel
	}
	return candidateLabel
}

func escapeCell(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", "<br>")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return strings.ReplaceAll(value, "\r", "<br>")
}

func formatValue(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func formatDistribution(distribution Distribution) string {
	return fmt.Sprintf(
		"%s [%s–%s]",
		formatValue(distribution.Median),
		formatValue(distribution.First),
		formatValue(distribution.Third),
	)
}

func formatRatio(candidate float64, baseline float64) string {
	if baseline == 0 {
		return unavailableValue
	}
	return strconv.FormatFloat(candidate/baseline, 'f', 3, 64) + "x"
}
