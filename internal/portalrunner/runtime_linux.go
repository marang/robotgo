//go:build linux

package portalrunner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	operatorInputLimit   = 64
	runnerCleanupTimeout = 2 * time.Minute
	runnerCleanupPoll    = 5 * time.Second
	runCompletionTimeout = 2 * time.Minute
	runCompletionPoll    = 3 * time.Second
	qemuExitGrace        = 15 * time.Second
)

// ProtectedRuntimeOptions identifies one operator-approved disposable runner
// invocation. OperatorInput must provide the exact line READY only after the
// private GNOME console is visible and controlled by the operator.
type ProtectedRuntimeOptions struct {
	ManifestPath   string
	RepositoryRoot string
	StateRoot      string
	GuestFiles     string
	SSHPort        int
	Identity       ProtectedRunIdentity
	OperatorInput  io.Reader
	Output         io.Writer
	Commands       CommandExecutor
	github         protectedGitHub
}

// RunProtectedGNOME boots one visible disposable GNOME VM, binds it to one
// approved GitHub workflow attempt, and destroys the VM after at most one job.
func RunProtectedGNOME(
	ctx context.Context,
	options ProtectedRuntimeOptions,
) (returnError error) {
	manifest, err := LoadManifest(options.ManifestPath)
	if err != nil {
		return err
	}
	if manifest.Lane != portalLaneGNOME {
		return errors.New("interactive protected runner supports only the GNOME lane")
	}
	if options.Identity.Repository == "" {
		options.Identity.Repository = manifest.Repository
	}
	if err := options.Identity.validate(); err != nil {
		return err
	}
	if options.Identity.Repository != manifest.Repository {
		return errors.New("protected run repository does not match runner manifest")
	}
	if options.OperatorInput == nil {
		return errors.New("protected runner requires interactive operator input")
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Commands == nil {
		options.Commands = systemCommandExecutor{}
	}
	if options.github == nil {
		options.github = newGHProtectedGitHub(options.Commands)
	}
	if options.SSHPort < 1024 || options.SSHPort > 65535 {
		return errors.New("portal runner SSH port must be unprivileged")
	}
	if err := PrepareStateRoot(options.StateRoot, options.RepositoryRoot); err != nil {
		return err
	}
	stateLock, err := AcquireStateLock(options.StateRoot)
	if err != nil {
		return err
	}
	defer func() { _ = stateLock.Close() }()
	if err := validateGuestFiles(options.GuestFiles, manifest.Lane); err != nil {
		return err
	}
	for _, executable := range []string{
		"cloud-localds",
		"gh",
		"qemu-img",
		"qemu-system-x86_64",
		"ssh",
		"ssh-keygen",
	} {
		if _, err := exec.LookPath(executable); err != nil {
			return fmt.Errorf("required protected runner host command %q is unavailable", executable)
		}
	}
	if err := validateVisibleQEMU(ctx, options.Commands); err != nil {
		return err
	}
	imageID, buildMetadata, _, err := computeImageIdentity(
		options.ManifestPath,
		options.RepositoryRoot,
		options.GuestFiles,
		manifest.Lane,
	)
	if err != nil {
		return err
	}
	imagePath := filepath.Join(
		options.StateRoot,
		"images",
		"gnome-"+imageID+".qcow2",
	)
	if !reusableImage(imagePath, imagePath+".build.json", buildMetadata) {
		return errors.New("protected GNOME runner image is unavailable or stale")
	}

	if err := writeStatus(options.Output, "protected run verify\n"); err != nil {
		return err
	}
	if err := options.github.ValidateRun(ctx, options.Identity); err != nil {
		return err
	}
	runnerName := options.Identity.runnerName(manifest.Lane)
	if err := options.github.DeleteRunner(
		ctx,
		manifest.Repository,
		runnerName,
	); err != nil {
		return fmt.Errorf("remove stale protected runner before launch: %w", err)
	}
	runnerPossible := false
	defer func() {
		if !runnerPossible {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(
			context.Background(),
			runnerCleanupTimeout,
		)
		defer cleanupCancel()
		if err := removeRegisteredRunner(
			cleanupContext,
			options.github,
			manifest.Repository,
			runnerName,
			options.Output,
		); err != nil {
			returnError = errors.Join(returnError, err)
		}
	}()

	runDirectory, err := CreateRun(options.StateRoot)
	if err != nil {
		return err
	}
	defer func() {
		if err := CleanupRun(options.StateRoot, runDirectory); err != nil {
			returnError = errors.Join(returnError, err)
		}
	}()
	runtimeContext, cancelRuntime := context.WithTimeout(
		ctx,
		manifest.MaximumLifetime(),
	)
	defer cancelRuntime()

	logFile, err := os.OpenFile(
		filepath.Join(runDirectory, "runtime.log"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create protected runner runtime log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	logWriter := &boundedWriter{destination: logFile, remaining: buildLogLimit}

	overlay := filepath.Join(runDirectory, "runner.qcow2")
	if err := options.Commands.Run(
		runtimeContext,
		"qemu-img",
		[]string{
			"create",
			"-f", "qcow2",
			"-F", "qcow2",
			"-b", imagePath,
			overlay,
		},
		nil,
		logWriter,
	); err != nil {
		return err
	}
	if err := os.Chmod(overlay, 0o600); err != nil {
		return fmt.Errorf("protect portal runner runtime overlay: %w", err)
	}

	privateKey := filepath.Join(runDirectory, "runtime-ssh")
	if err := options.Commands.Run(
		runtimeContext,
		"ssh-keygen",
		[]string{"-q", "-t", "ed25519", "-N", "", "-f", privateKey},
		nil,
		logWriter,
	); err != nil {
		return err
	}
	publicKey, err := os.ReadFile(privateKey + ".pub")
	if err != nil {
		return fmt.Errorf("read ephemeral runtime public key: %w", err)
	}
	instanceID, err := randomIdentifier()
	if err != nil {
		return err
	}
	userData := filepath.Join(runDirectory, "user-data")
	metaData := filepath.Join(runDirectory, "meta-data")
	networkData := filepath.Join(runDirectory, "network-config")
	seedImage := filepath.Join(runDirectory, "seed.img")
	cloudConfig, err := buildCloudConfig(strings.TrimSpace(string(publicKey)))
	if err != nil {
		return err
	}
	for path, data := range map[string][]byte{
		userData:    []byte(cloudConfig),
		metaData:    []byte("instance-id: " + instanceID + "\nlocal-hostname: robotgo-gnome-runner\n"),
		networkData: []byte(runtimeNetworkConfig()),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return fmt.Errorf("write protected runner cloud-init input: %w", err)
		}
	}
	if err := options.Commands.Run(
		runtimeContext,
		"cloud-localds",
		[]string{"-N", networkData, seedImage, userData, metaData},
		nil,
		logWriter,
	); err != nil {
		return err
	}

	proxy, err := NewCONNECTProxy(manifest.Network)
	if err != nil {
		return err
	}
	proxyListener, err := net.Listen(
		"tcp",
		net.JoinHostPort("127.0.0.1", strconv.Itoa(manifest.Network.ProxyPort)),
	)
	if err != nil {
		return fmt.Errorf("listen for protected runner proxy: %w", err)
	}
	proxyContext, stopProxy := context.WithCancel(runtimeContext)
	proxyDone := make(chan error, 1)
	go func() { proxyDone <- proxy.Serve(proxyContext, proxyListener) }()
	defer func() {
		stopProxy()
		_ = proxyListener.Close()
		if err := <-proxyDone; err != nil {
			returnError = errors.Join(returnError, err)
		}
	}()

	serialLog := filepath.Join(runDirectory, "serial.log")
	pidFile := filepath.Join(runDirectory, "qemu.pid")
	qemuCommand := exec.CommandContext(
		runtimeContext,
		"qemu-system-x86_64",
		buildQEMUArguments(
			manifest,
			overlay,
			seedImage,
			pidFile,
			serialLog,
			options.SSHPort,
			false,
		)...,
	)
	qemuCommand.Stdout = logWriter
	qemuCommand.Stderr = logWriter
	qemuCommand.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	qemuCommand.Cancel = func() error {
		if qemuCommand.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-qemuCommand.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	qemuCommand.WaitDelay = qemuExitGrace
	if err := qemuCommand.Start(); err != nil {
		return fmt.Errorf("start protected GNOME VM: %w", err)
	}
	if err := writeStatus(
		options.Output,
		"protected VM started id=%s\n",
		imageID[:16],
	); err != nil {
		return err
	}
	vmDone := make(chan struct{})
	var vmWaitError error
	go func() {
		vmWaitError = qemuCommand.Wait()
		close(vmDone)
	}()
	vmExited := false
	defer func() {
		if vmExited {
			return
		}
		_ = syscall.Kill(-qemuCommand.Process.Pid, syscall.SIGTERM)
		select {
		case <-vmDone:
		case <-time.After(qemuExitGrace):
			_ = syscall.Kill(-qemuCommand.Process.Pid, syscall.SIGKILL)
			<-vmDone
		}
	}()

	sshArguments := []string{
		"-i", privateKey,
		"-p", strconv.Itoa(options.SSHPort),
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=" + filepath.Join(runDirectory, "known-hosts"),
		"-o", "ConnectTimeout=5",
	}
	guestContext, guestCancel := context.WithCancel(runtimeContext)
	go func() {
		select {
		case <-vmDone:
			guestCancel()
		case <-guestContext.Done():
		}
	}()
	defer guestCancel()
	if err := waitForSSH(
		guestContext,
		options.Commands,
		sshArguments,
		logWriter,
	); err != nil {
		return fmt.Errorf("wait for protected GNOME VM: %w", err)
	}
	sessionCommand := "runuser -u robotgo -- env " +
		"XDG_RUNTIME_DIR=/run/user/1100 " +
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus " +
		"timeout 130 /usr/local/libexec/robotgo-runner-wait-session"
	if err := options.Commands.Run(
		guestContext,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			"set -euo pipefail; "+sessionCommand,
		),
		nil,
		logWriter,
	); err != nil {
		return fmt.Errorf("wait for protected GNOME operator console: %w", err)
	}

	if err := writeStatus(
		options.Output,
		"operator action required: verify the private GNOME console, then type READY\n",
	); err != nil {
		return err
	}
	if err := waitForOperator(guestContext, options.OperatorInput); err != nil {
		return err
	}
	if err := writeStatus(options.Output, "operator ready\n"); err != nil {
		return err
	}

	token, err := options.github.RegistrationToken(
		runtimeContext,
		manifest.Repository,
	)
	if err != nil {
		return err
	}
	defer clearBytes(token)
	tokenInput := append(append([]byte{}, token...), '\n')
	defer clearBytes(tokenInput)
	registrationLog, err := NewRedactingWriter(logWriter, token)
	if err != nil {
		return fmt.Errorf("protect protected runner registration log: %w", err)
	}
	runnerPossible = true

	registerCommand := "set -euo pipefail; exec " + strings.Join([]string{
		"/usr/local/sbin/robotgo-runner-register",
		shellQuote(manifest.Repository),
		shellQuote(runnerName),
		shellQuote(options.Identity.Commit),
		shellQuote(options.Identity.RunID),
		shellQuote(strconv.Itoa(options.Identity.RunAttempt)),
	}, " ")
	registrationError := options.Commands.Run(
		guestContext,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			registerCommand,
		),
		bytes.NewReader(tokenInput),
		registrationLog,
	)
	redactionError := registrationLog.Flush()
	clearBytes(token)
	clearBytes(tokenInput)
	if err := errors.Join(registrationError, redactionError); err != nil {
		return fmt.Errorf("register protected ephemeral runner: %w", err)
	}
	if err := writeStatus(
		options.Output,
		"runner registered name=%s\n",
		runnerName,
	); err != nil {
		return err
	}

	select {
	case <-vmDone:
		vmExited = true
		if vmWaitError != nil {
			return errors.New("protected GNOME VM exited unsuccessfully")
		}
		completionContext, completionCancel := context.WithTimeout(
			runtimeContext,
			runCompletionTimeout,
		)
		defer completionCancel()
		if err := waitForRunSuccess(
			completionContext,
			options.github,
			options.Identity,
		); err != nil {
			return err
		}
		return writeStatus(options.Output, "protected runner job complete\n")
	case <-runtimeContext.Done():
		return fmt.Errorf("protected runner lifetime ended: %w", runtimeContext.Err())
	}
}

func waitForRunSuccess(
	ctx context.Context,
	github protectedGitHub,
	identity ProtectedRunIdentity,
) error {
	for {
		succeeded, err := github.RunSucceeded(ctx, identity)
		if err != nil {
			return err
		}
		if succeeded {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"confirm protected runner workflow completion: %w",
				ctx.Err(),
			)
		case <-time.After(runCompletionPoll):
		}
	}
}

func validateVisibleQEMU(ctx context.Context, commands CommandExecutor) error {
	for _, check := range []struct {
		args []string
		want []string
	}{
		{
			args: []string{"-device", "help"},
			want: []string{
				`name "virtio-vga"`,
				`name "qemu-xhci"`,
				`name "usb-kbd"`,
				`name "usb-tablet"`,
			},
		},
		{
			args: []string{"-display", "help"},
			want: []string{"gtk"},
		},
	} {
		var output bytes.Buffer
		if err := commands.Run(
			ctx,
			"qemu-system-x86_64",
			check.args,
			nil,
			&boundedWriter{
				destination: &output,
				remaining:   maximumBuildInput,
			},
		); err != nil {
			return errors.New("inspect protected runner QEMU capabilities")
		}
		for _, expected := range check.want {
			if !strings.Contains(output.String(), expected) {
				return fmt.Errorf(
					"protected runner QEMU omits %q; install the desktop QEMU modules",
					expected,
				)
			}
		}
	}
	return nil
}

func waitForOperator(ctx context.Context, input io.Reader) error {
	result := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(io.LimitReader(input, operatorInputLimit+1))
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			result <- errors.New("read protected runner operator confirmation")
			return
		}
		if len(line) > operatorInputLimit ||
			(line != "READY\n" && line != "READY") {
			result <- errors.New("protected runner operator confirmation was not READY")
			return
		}
		result <- nil
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func removeRegisteredRunner(
	ctx context.Context,
	github protectedGitHub,
	repository,
	name string,
	output io.Writer,
) error {
	var lastError error
	for {
		err := github.DeleteRunner(ctx, repository, name)
		if err == nil {
			return writeStatus(output, "runner cleanup complete name=%s\n", name)
		}
		lastError = err
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"remove protected runner %q: %w",
				name,
				errors.Join(lastError, ctx.Err()),
			)
		case <-time.After(runnerCleanupPoll):
		}
	}
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
