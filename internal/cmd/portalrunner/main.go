//go:build linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/marang/robotgo/internal/portalrunner"
)

const defaultManifest = "infrastructure/portal-runner/gnome/manifest.json"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("usage: portalrunner <validate|build|probe|run|proxy>")
	}
	switch arguments[0] {
	case "validate":
		return runValidate(arguments[1:], stdout, stderr)
	case "build":
		return runBuild(arguments[1:], stdout, stderr)
	case "probe":
		return runProbe(arguments[1:], stdout, stderr)
	case "run":
		return runProtected(arguments[1:], stdout, stderr)
	case "proxy":
		return runProxy(arguments[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown portalrunner command %q", arguments[0])
	}
}

func runProtected(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", defaultManifest, "runner manifest path")
	repositoryRoot := flags.String("repository-root", ".", "repository root")
	stateRoot := flags.String("state-root", "", "external private runner state root")
	sshPort := flags.Int("ssh-port", 22222, "loopback runtime SSH port")
	commit := flags.String("commit", "", "exact approved 40-character commit")
	runID := flags.String("run-id", "", "exact approved GitHub workflow run ID")
	runAttempt := flags.Int("run-attempt", 1, "exact approved workflow attempt")
	cell := flags.String(
		"cell",
		"",
		"protected test cell: remote-desktop or screencast",
	)
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("run accepts no positional arguments")
	}
	absoluteRepository, err := filepath.Abs(*repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	absoluteManifest, err := filepath.Abs(*manifestPath)
	if err != nil {
		return fmt.Errorf("resolve portal runner manifest: %w", err)
	}
	if *stateRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve portal runner state home: %w", err)
		}
		*stateRoot = filepath.Join(home, ".local", "share", "robotgo-portal-runner")
	}
	absoluteState, err := filepath.Abs(*stateRoot)
	if err != nil {
		return fmt.Errorf("resolve portal runner state: %w", err)
	}
	guestFiles := filepath.Join(
		absoluteRepository,
		"infrastructure",
		"portal-runner",
		"gnome",
	)
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()
	return portalrunner.RunProtectedGNOME(
		ctx,
		portalrunner.ProtectedRuntimeOptions{
			ManifestPath:   absoluteManifest,
			RepositoryRoot: absoluteRepository,
			StateRoot:      absoluteState,
			GuestFiles:     guestFiles,
			SSHPort:        *sshPort,
			Identity: portalrunner.ProtectedRunIdentity{
				Commit:     *commit,
				RunID:      *runID,
				RunAttempt: *runAttempt,
				Cell:       *cell,
			},
			OperatorInput: os.Stdin,
			Output:        stdout,
		},
	)
}

func runProbe(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", defaultManifest, "runner manifest path")
	repositoryRoot := flags.String("repository-root", ".", "repository root")
	stateRoot := flags.String("state-root", "", "external private runner state root")
	sshPort := flags.Int("ssh-port", 22222, "loopback probe SSH port")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("probe accepts no positional arguments")
	}
	absoluteRepository, err := filepath.Abs(*repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	absoluteManifest, err := filepath.Abs(*manifestPath)
	if err != nil {
		return fmt.Errorf("resolve portal runner manifest: %w", err)
	}
	if *stateRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve portal runner state home: %w", err)
		}
		*stateRoot = filepath.Join(home, ".local", "share", "robotgo-portal-runner")
	}
	absoluteState, err := filepath.Abs(*stateRoot)
	if err != nil {
		return fmt.Errorf("resolve portal runner state: %w", err)
	}
	guestFiles := filepath.Join(
		absoluteRepository,
		"infrastructure",
		"portal-runner",
		"gnome",
	)
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()
	return portalrunner.ProbeImage(ctx, portalrunner.ImageProbeOptions{
		ManifestPath:   absoluteManifest,
		RepositoryRoot: absoluteRepository,
		StateRoot:      absoluteState,
		GuestFiles:     guestFiles,
		SSHPort:        *sshPort,
		Output:         stdout,
	})
}

func runBuild(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", defaultManifest, "runner manifest path")
	repositoryRoot := flags.String("repository-root", ".", "repository root")
	stateRoot := flags.String("state-root", "", "external private runner state root")
	sshPort := flags.Int("ssh-port", 22222, "loopback build SSH port")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("build accepts no positional arguments")
	}
	absoluteRepository, err := filepath.Abs(*repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	absoluteManifest, err := filepath.Abs(*manifestPath)
	if err != nil {
		return fmt.Errorf("resolve portal runner manifest: %w", err)
	}
	if *stateRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve portal runner state home: %w", err)
		}
		*stateRoot = filepath.Join(home, ".local", "share", "robotgo-portal-runner")
	}
	absoluteState, err := filepath.Abs(*stateRoot)
	if err != nil {
		return fmt.Errorf("resolve portal runner state: %w", err)
	}
	guestFiles := filepath.Join(
		absoluteRepository,
		"infrastructure",
		"portal-runner",
		"gnome",
	)
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()
	_, err = portalrunner.BuildImage(ctx, portalrunner.ImageBuildOptions{
		ManifestPath:   absoluteManifest,
		RepositoryRoot: absoluteRepository,
		StateRoot:      absoluteState,
		GuestFiles:     guestFiles,
		SSHPort:        *sshPort,
		Output:         stdout,
	})
	return err
}

func runValidate(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", defaultManifest, "runner manifest path")
	repositoryRoot := flags.String("repository-root", ".", "repository root")
	stateRoot := flags.String("state-root", "", "external private runner state root")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("validate accepts no positional arguments")
	}
	manifest, err := portalrunner.LoadManifest(*manifestPath)
	if err != nil {
		return err
	}
	if *stateRoot != "" {
		absoluteRepository, err := filepath.Abs(*repositoryRoot)
		if err != nil {
			return fmt.Errorf("resolve repository root: %w", err)
		}
		absoluteState, err := filepath.Abs(*stateRoot)
		if err != nil {
			return fmt.Errorf("resolve state root: %w", err)
		}
		if err := portalrunner.PrepareStateRoot(absoluteState, absoluteRepository); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(
		stdout,
		"valid lane=%s repository=%s image=%s runner=%s\n",
		manifest.Lane,
		manifest.Repository,
		manifest.BaseImage.Version,
		manifest.ActionsRunner.Version,
	)
	return err
}

func runProxy(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("proxy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", defaultManifest, "runner manifest path")
	listenHost := flags.String("listen-host", "127.0.0.1", "loopback listen host")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("proxy accepts no positional arguments")
	}
	if *listenHost != "127.0.0.1" && *listenHost != "::1" {
		return errors.New("portal runner proxy must listen on loopback")
	}
	manifest, err := portalrunner.LoadManifest(*manifestPath)
	if err != nil {
		return err
	}
	proxy, err := portalrunner.NewCONNECTProxy(manifest.Network)
	if err != nil {
		return err
	}
	address := net.JoinHostPort(*listenHost, strconv.Itoa(manifest.Network.ProxyPort))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen for portal runner proxy: %w", err)
	}
	defer func() { _ = listener.Close() }()
	if _, err := fmt.Fprintf(stdout, "proxy ready address=%s\n", address); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()
	return proxy.Serve(ctx, listener)
}
