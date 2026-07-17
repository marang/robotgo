package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/marang/robotgo/internal/releaseevidence"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "releaseevidence: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("expected generate or verify subcommand")
	}
	switch arguments[0] {
	case "generate":
		return runGenerate(arguments[1:], stdout, stderr)
	case "verify":
		return runVerify(arguments[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown subcommand %q", arguments[0])
	}
}

func runGenerate(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("releaseevidence generate", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var (
		outputPath  string
		testLogPath string
		commit      string
		tree        string
		ref         string
		runID       string
		matrix      string
		testCommand string
		expectedCGO string
		runAttempt  int
	)
	flags.StringVar(&outputPath, "out", "", "output evidence JSON path")
	flags.StringVar(&testLogPath, "test-log", "", "complete passed test log")
	flags.StringVar(&commit, "commit", "", "checked-out Git commit object ID")
	flags.StringVar(&tree, "tree", "", "checked-out Git tree object ID")
	flags.StringVar(&ref, "ref", "", "checked-out full Git ref")
	flags.StringVar(&runID, "run-id", "", "GitHub Actions run ID")
	flags.IntVar(&runAttempt, "run-attempt", 0, "GitHub Actions run attempt")
	flags.StringVar(&matrix, "matrix", "", "release matrix cell label")
	flags.StringVar(&testCommand, "test-command", "", "test command represented by the log")
	flags.StringVar(&expectedCGO, "expected-cgo", "", "required runtime CGO state: true or false")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if outputPath == "" {
		return errors.New("-out is required")
	}
	if testLogPath == "" {
		return errors.New("-test-log is required")
	}
	cgoEnabled, err := strconv.ParseBool(expectedCGO)
	if err != nil || (expectedCGO != "true" && expectedCGO != "false") {
		return errors.New("-expected-cgo must be true or false")
	}

	snapshot, err := releaseevidence.Generate(context.Background(), releaseevidence.Config{
		Commit:      commit,
		Tree:        tree,
		Ref:         ref,
		RunID:       runID,
		RunAttempt:  runAttempt,
		Matrix:      matrix,
		TestCommand: testCommand,
		TestLogPath: testLogPath,
		ExpectedCGO: cgoEnabled,
	})
	if err != nil {
		return err
	}
	if err := releaseevidence.Write(outputPath, snapshot); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "wrote release evidence %s\n", outputPath); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

func runVerify(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("releaseevidence verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		evidencePath        string
		expectedMatrix      string
		expectedCommit      string
		expectedTree        string
		expectedRef         string
		expectedTestCommand string
	)
	flags.StringVar(&evidencePath, "evidence", "", "release evidence JSON path")
	flags.StringVar(&expectedMatrix, "expected-matrix", "", "required release matrix cell")
	flags.StringVar(&expectedCommit, "expected-commit", "", "optional exact Git commit")
	flags.StringVar(&expectedTree, "expected-tree", "", "optional exact Git tree")
	flags.StringVar(&expectedRef, "expected-ref", "", "optional exact full Git ref")
	flags.StringVar(&expectedTestCommand, "expected-test-command", "", "optional exact test command")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if evidencePath == "" {
		return errors.New("-evidence is required")
	}
	if expectedMatrix == "" {
		return errors.New("-expected-matrix is required")
	}
	snapshot, err := releaseevidence.Verify(evidencePath, releaseevidence.Expectations{
		Matrix:      expectedMatrix,
		Commit:      expectedCommit,
		Tree:        expectedTree,
		Ref:         expectedRef,
		TestCommand: expectedTestCommand,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"verified release evidence schema=%s commit=%s matrix=%s\n",
		snapshot.SchemaVersion,
		snapshot.Source.Commit,
		snapshot.CI.Matrix,
	); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}
