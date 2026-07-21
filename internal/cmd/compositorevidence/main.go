package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/marang/robotgo/internal/compositorevidence"
)

const (
	envCurrentDesktop    = "XDG_CURRENT_DESKTOP"
	envWaylandDisplay    = "WAYLAND_DISPLAY"
	envRuntimeDir        = "XDG_RUNTIME_DIR"
	envSessionBusAddress = "DBUS_SESSION_BUS_ADDRESS"
	envHome              = "HOME"
	envUser              = "USER"
	envLogName           = "LOGNAME"
	envRunnerName        = "RUNNER_NAME"
)

type getenvFunc func(string) string

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "compositorevidence: %v\n", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	getenv getenvFunc,
) error {
	if len(arguments) == 0 {
		return errors.New("expected preflight, finalize, verify, or cleanup subcommand")
	}
	switch arguments[0] {
	case "preflight":
		return runPreflight(ctx, arguments[1:], stdout, stderr, getenv)
	case "finalize":
		return runFinalize(arguments[1:], stdout, stderr, getenv)
	case "verify":
		return runVerify(arguments[1:], stdout, stderr)
	case "cleanup":
		return runCleanup(arguments[1:], stderr)
	default:
		return fmt.Errorf("unknown subcommand %q", arguments[0])
	}
}

type identityFlags struct {
	laneValue       string
	cellValue       string
	runnerTemp      string
	outputDirectory string
	expectedCommit  string
}

func (identity *identityFlags) register(flags *flag.FlagSet) {
	flags.StringVar(&identity.laneValue, "lane", "", "protected desktop lane")
	flags.StringVar(&identity.cellValue, "cell", "", "protected evidence cell")
	flags.StringVar(&identity.runnerTemp, "runner-temp", "", "runner temporary directory")
	flags.StringVar(&identity.outputDirectory, "output-dir", "", "owned evidence output directory")
	flags.StringVar(&identity.expectedCommit, "expected-commit", "", "approved exact Git commit")
}

func (identity identityFlags) parse() (compositorevidence.Lane, compositorevidence.Cell, error) {
	lane, err := compositorevidence.ParseLane(identity.laneValue)
	if err != nil {
		return "", "", err
	}
	cell, err := compositorevidence.ParseCell(identity.cellValue)
	if err != nil {
		return "", "", err
	}
	return lane, cell, nil
}

func runPreflight(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	getenv getenvFunc,
) (retErr error) {
	flags := flag.NewFlagSet("compositorevidence preflight", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var identity identityFlags
	identity.register(flags)
	var (
		checkoutCommit    string
		ref               string
		workflow          string
		runID             string
		runAttempt        int
		operatorReadyPath string
		outputCount       int
		minimumOutputs    int
		requireHeadless   bool
		probeTimeout      time.Duration
	)
	flags.StringVar(&checkoutCommit, "commit", "", "checked-out Git commit")
	flags.StringVar(&ref, "ref", "", "checked-out full Git ref")
	flags.StringVar(&workflow, "workflow", "", "GitHub Actions workflow name")
	flags.StringVar(&runID, "run-id", "", "GitHub Actions run ID")
	flags.IntVar(&runAttempt, "run-attempt", 0, "GitHub Actions run attempt")
	flags.StringVar(&operatorReadyPath, "operator-ready-file", "", "orchestrator-owned consent readiness file")
	flags.IntVar(&outputCount, "output-count", 0, "declared compositor output count")
	flags.IntVar(&minimumOutputs, "minimum-outputs", 1, "minimum required output count")
	flags.BoolVar(&requireHeadless, "require-headless-sway", false, "require isolated headless Sway with no input devices")
	flags.DurationVar(&probeTimeout, "probe-timeout", 5*time.Second, "per-probe timeout")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	lane, cell, err := identity.parse()
	if err != nil {
		return err
	}
	if err := compositorevidence.PrepareOutputDirectory(
		identity.runnerTemp,
		identity.outputDirectory,
	); err != nil {
		return err
	}
	prepared := true
	defer func() {
		if !prepared || retErr == nil {
			return
		}
		cleanupErr := compositorevidence.Cleanup(identity.runnerTemp, identity.outputDirectory)
		if cleanupErr != nil {
			retErr = errors.Join(retErr, cleanupErr)
		}
	}()
	report, err := compositorevidence.Preflight(ctx, compositorevidence.PreflightConfig{
		Lane:                lane,
		Cell:                cell,
		CheckoutCommit:      checkoutCommit,
		ExpectedCommit:      identity.expectedCommit,
		Ref:                 ref,
		Workflow:            workflow,
		RunID:               runID,
		RunAttempt:          runAttempt,
		CurrentDesktop:      getenv(envCurrentDesktop),
		WaylandDisplay:      getenv(envWaylandDisplay),
		RuntimeDir:          getenv(envRuntimeDir),
		SessionBusAddress:   getenv(envSessionBusAddress),
		OperatorReadyPath:   operatorReadyPath,
		OutputCount:         outputCount,
		MinimumOutputCount:  minimumOutputs,
		RequireHeadlessSway: requireHeadless,
		ProbeTimeout:        probeTimeout,
	})
	if err != nil {
		return err
	}
	if err := compositorevidence.WritePreflight(identity.outputDirectory, report); err != nil {
		return err
	}
	prepared = false
	if _, err := fmt.Fprintf(
		stdout,
		"preflight passed lane=%s cell=%s outputs=%d\n",
		lane,
		cell,
		outputCount,
	); err != nil {
		return fmt.Errorf("write preflight summary: %w", err)
	}
	return nil
}

func runFinalize(
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	getenv getenvFunc,
) error {
	flags := flag.NewFlagSet("compositorevidence finalize", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var identity identityFlags
	identity.register(flags)
	var (
		workflow     string
		runID        string
		runAttempt   int
		testExitCode int
	)
	flags.StringVar(&workflow, "workflow", "", "GitHub Actions workflow name")
	flags.StringVar(&runID, "run-id", "", "GitHub Actions run ID")
	flags.IntVar(&runAttempt, "run-attempt", 0, "GitHub Actions run attempt")
	flags.IntVar(&testExitCode, "test-exit-code", -1, "integration test exit code")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	lane, cell, err := identity.parse()
	if err != nil {
		return err
	}
	manifest, err := compositorevidence.Finalize(compositorevidence.FinalizeConfig{
		RunnerTemp:      identity.runnerTemp,
		OutputDirectory: identity.outputDirectory,
		ExpectedCommit:  identity.expectedCommit,
		ExpectedLane:    lane,
		ExpectedCell:    cell,
		Workflow:        workflow,
		RunID:           runID,
		RunAttempt:      runAttempt,
		TestExitCode:    testExitCode,
		SensitiveValues: sensitiveValues(getenv),
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"evidence validated schema=%s lane=%s cell=%s commit=%s\n",
		manifest.SchemaVersion,
		manifest.Desktop.Lane,
		manifest.Desktop.Cell,
		manifest.Commit,
	); err != nil {
		return fmt.Errorf("write finalization summary: %w", err)
	}
	return nil
}

func runVerify(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("compositorevidence verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var identity identityFlags
	identity.register(flags)
	var (
		workflow   string
		runID      string
		runAttempt int
	)
	flags.StringVar(&workflow, "workflow", "", "expected GitHub Actions workflow")
	flags.StringVar(&runID, "run-id", "", "expected GitHub Actions run ID")
	flags.IntVar(&runAttempt, "run-attempt", 0, "expected GitHub Actions run attempt")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	lane, cell, err := identity.parse()
	if err != nil {
		return err
	}
	manifest, err := compositorevidence.VerifyDirectory(
		identity.outputDirectory,
		compositorevidence.VerifyExpectations{
			Commit:     identity.expectedCommit,
			Lane:       lane,
			Cell:       cell,
			Workflow:   workflow,
			RunID:      runID,
			RunAttempt: runAttempt,
		},
	)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"verified evidence schema=%s lane=%s cell=%s commit=%s\n",
		manifest.SchemaVersion,
		manifest.Desktop.Lane,
		manifest.Desktop.Cell,
		manifest.Commit,
	); err != nil {
		return fmt.Errorf("write verification summary: %w", err)
	}
	return nil
}

func runCleanup(arguments []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("compositorevidence cleanup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var runnerTemp, outputDirectory string
	flags.StringVar(&runnerTemp, "runner-temp", "", "runner temporary directory")
	flags.StringVar(&outputDirectory, "output-dir", "", "owned evidence output directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	return compositorevidence.Cleanup(runnerTemp, outputDirectory)
}

func sensitiveValues(getenv getenvFunc) []string {
	values := make([]string, 0, 8)
	for _, name := range []string{
		envWaylandDisplay,
		envRuntimeDir,
		envSessionBusAddress,
		envHome,
		envUser,
		envLogName,
		envRunnerName,
	} {
		values = append(values, getenv(name))
	}
	if hostname, err := os.Hostname(); err == nil {
		values = append(values, hostname)
	}
	return values
}
