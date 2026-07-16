package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marang/robotgo/internal/benchcmp"
)

const standardOutputPath = "-"

type options struct {
	baselinePath           string
	candidatePath          string
	baselineLabel          string
	candidateLabel         string
	outputPath             string
	expectedBenchmarks     int
	expectedBenchmarkNames benchmarkNameFlags
	expectedSamples        int
}

type benchmarkNameFlags []string

func (names *benchmarkNameFlags) Set(value string) error {
	if value == "" {
		return errors.New("benchmark name must not be empty")
	}
	*names = append(*names, value)
	return nil
}

func (names *benchmarkNameFlags) String() string {
	if names == nil {
		return ""
	}
	return strings.Join(*names, ",")
}

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return
	}
	fmt.Fprintf(os.Stderr, "benchcompare: %v\n", err)
	os.Exit(1)
}

func run(arguments []string, stdout io.Writer, stderr io.Writer) error {
	configuration, err := parseFlags(arguments, stderr)
	if err != nil {
		return err
	}

	baseline, err := parseFile(configuration.baselinePath)
	if err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	candidate, err := parseFile(configuration.candidatePath)
	if err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if err := benchcmp.ValidateExpectedNames(baseline, configuration.expectedBenchmarkNames); err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	if err := benchcmp.ValidateExpectedNames(candidate, configuration.expectedBenchmarkNames); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if err := benchcmp.ValidateCompleteness(baseline, configuration.expectedBenchmarks, configuration.expectedSamples); err != nil {
		return fmt.Errorf("baseline: %w", err)
	}
	if err := benchcmp.ValidateCompleteness(candidate, configuration.expectedBenchmarks, configuration.expectedSamples); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}

	var comparison bytes.Buffer
	if err := benchcmp.WriteMarkdown(
		&comparison,
		baseline,
		candidate,
		configuration.baselineLabel,
		configuration.candidateLabel,
	); err != nil {
		return err
	}

	if configuration.outputPath == standardOutputPath {
		if _, err := comparison.WriteTo(stdout); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(configuration.outputPath, comparison.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", configuration.outputPath, err)
	}
	return nil
}

func parseFlags(arguments []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("benchcompare", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var configuration options
	flags.StringVar(&configuration.baselinePath, "baseline", "", "path to baseline Go benchmark output")
	flags.StringVar(&configuration.candidatePath, "candidate", "", "path to candidate Go benchmark output")
	flags.StringVar(&configuration.baselineLabel, "baseline-label", "baseline", "baseline label used in Markdown")
	flags.StringVar(&configuration.candidateLabel, "candidate-label", "candidate", "candidate label used in Markdown")
	flags.StringVar(&configuration.outputPath, "out", standardOutputPath, "output Markdown path, or - for stdout")
	flags.IntVar(&configuration.expectedBenchmarks, "expected-benchmarks", 0, "required benchmark count; 0 disables the check")
	flags.Var(&configuration.expectedBenchmarkNames, "expected-benchmark", "required normalized benchmark name; repeat for an exact manifest")
	flags.IntVar(&configuration.expectedSamples, "expected-samples", 0, "required sample count per standard metric; 0 disables the check")
	if err := flags.Parse(arguments); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if configuration.baselinePath == "" {
		return options{}, errors.New("-baseline is required")
	}
	if configuration.candidatePath == "" {
		return options{}, errors.New("-candidate is required")
	}
	if configuration.expectedBenchmarks < 0 {
		return options{}, errors.New("-expected-benchmarks must be non-negative")
	}
	if configuration.expectedSamples < 0 {
		return options{}, errors.New("-expected-samples must be non-negative")
	}
	if configuration.expectedBenchmarks > 0 && len(configuration.expectedBenchmarkNames) > 0 &&
		configuration.expectedBenchmarks != len(configuration.expectedBenchmarkNames) {
		return options{}, fmt.Errorf(
			"-expected-benchmarks is %d, but %d -expected-benchmark values were provided",
			configuration.expectedBenchmarks,
			len(configuration.expectedBenchmarkNames),
		)
	}
	return configuration, nil
}

func parseFile(path string) (benchcmp.Results, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}

	results, parseErr := benchcmp.Parse(file)
	closeErr := file.Close()
	if parseErr != nil {
		return nil, fmt.Errorf("read %q: %w", path, parseErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close %q: %w", path, closeErr)
	}
	return results, nil
}
