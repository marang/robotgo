//go:build linux

package portalrunner

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	maximumSourceArchive = 128 * 1024 * 1024
	portalDialogSettle   = 2 * time.Second
	consentPollInterval  = 250 * time.Millisecond
	hostedTestTimeout    = 4 * time.Minute
	hostedTestLogLimit   = 2 * 1024 * 1024
)

var portalFailureStagePattern = regexp.MustCompile(
	`ROBOTGO_PORTAL_STAGE=([a-z0-9-]{1,32})`,
)

// HostedRuntimeOptions identifies one credential-free portal test in a
// disposable GNOME guest running inside a GitHub-hosted Linux runner.
type HostedRuntimeOptions struct {
	ManifestPath   string
	RepositoryRoot string
	StateRoot      string
	GuestFiles     string
	SSHPort        int
	Commit         string
	Cell           string
	Output         io.Writer
	Commands       CommandExecutor
}

type hostedProcessResult struct {
	done chan struct{}
	err  error
}

// RunHostedGNOME transfers the exact clean commit into a disposable guest,
// runs one portal integration cell, and drives GNOME consent independently
// through QMP. It does not register a GitHub Actions runner or consume tokens.
func RunHostedGNOME(
	ctx context.Context,
	options HostedRuntimeOptions,
) (returnError error) {
	manifest, err := LoadManifest(options.ManifestPath)
	if err != nil {
		return err
	}
	if err := validateHostedIdentity(options.Commit, options.Cell); err != nil {
		return err
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Commands == nil {
		options.Commands = systemCommandExecutor{}
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
	if err := validateGuestFiles(options.GuestFiles); err != nil {
		return err
	}
	for _, executable := range []string{
		"cloud-localds",
		"git",
		"qemu-img",
		"qemu-system-x86_64",
		"scp",
		"ssh",
		"ssh-keygen",
	} {
		if _, err := exec.LookPath(executable); err != nil {
			return fmt.Errorf(
				"required hosted portal command %q is unavailable",
				executable,
			)
		}
	}
	if err := validateExactCleanCommit(
		ctx,
		options.Commands,
		options.RepositoryRoot,
		options.Commit,
	); err != nil {
		return err
	}

	imageID, buildMetadata, _, err := computeImageIdentity(
		options.ManifestPath,
		options.RepositoryRoot,
		options.GuestFiles,
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
		return errors.New("hosted GNOME image is unavailable or stale")
	}

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
		filepath.Join(runDirectory, "hosted.log"),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create hosted portal log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	logWriter := &boundedWriter{destination: logFile, remaining: buildLogLimit}

	sourceArchive := filepath.Join(runDirectory, "source.tar")
	if err := createSourceArchive(
		runtimeContext,
		options.Commands,
		options.RepositoryRoot,
		options.Commit,
		sourceArchive,
		logWriter,
	); err != nil {
		return err
	}

	overlay := filepath.Join(runDirectory, "hosted.qcow2")
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
		return fmt.Errorf("protect hosted portal overlay: %w", err)
	}

	privateKey := filepath.Join(runDirectory, "hosted-ssh")
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
		return fmt.Errorf("read hosted portal public key: %w", err)
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
		metaData:    []byte("instance-id: " + instanceID + "\nlocal-hostname: robotgo-gnome-hosted\n"),
		networkData: []byte(runtimeNetworkConfig()),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return fmt.Errorf("write hosted portal cloud-init input: %w", err)
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
		return fmt.Errorf("listen for hosted portal proxy: %w", err)
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

	pidFile := filepath.Join(runDirectory, "qemu.pid")
	serialLog := filepath.Join(runDirectory, "serial.log")
	qmpSocket := filepath.Join(runDirectory, "qmp.sock")
	qemuCommand := exec.CommandContext(
		runtimeContext,
		"qemu-system-x86_64",
		buildHostedQEMUArguments(
			manifest,
			overlay,
			seedImage,
			pidFile,
			serialLog,
			options.SSHPort,
			qmpSocket,
		)...,
	)
	qemuCommand.Stdout = logWriter
	qemuCommand.Stderr = logWriter
	configureHostedProcess(qemuCommand)
	if err := qemuCommand.Start(); err != nil {
		return errors.New("start hosted GNOME VM")
	}
	vmResult := &hostedProcessResult{done: make(chan struct{})}
	go func() {
		vmResult.err = qemuCommand.Wait()
		close(vmResult.done)
	}()
	vmExited := false
	defer func() {
		if vmExited {
			return
		}
		stopProcessGroup(qemuCommand, vmResult.done)
	}()
	if err := writeStatus(
		options.Output,
		"hosted VM started id=%s cell=%s\n",
		imageID[:16],
		options.Cell,
	); err != nil {
		return err
	}

	guestContext, cancelGuest := context.WithCancel(runtimeContext)
	defer cancelGuest()
	go func() {
		select {
		case <-vmResult.done:
			cancelGuest()
		case <-guestContext.Done():
		}
	}()
	sshArguments := hostedSSHArguments(
		privateKey,
		runDirectory,
		options.SSHPort,
	)
	if err := waitForSSH(
		guestContext,
		options.Commands,
		sshArguments,
		logWriter,
	); err != nil {
		return fmt.Errorf("wait for hosted GNOME VM: %w", err)
	}
	if err := enforceHostedEgressBoundary(
		guestContext,
		options.Commands,
		sshArguments,
		logWriter,
	); err != nil {
		return err
	}
	if err := waitForHostedSession(
		guestContext,
		options.Commands,
		sshArguments,
		logWriter,
	); err != nil {
		collectGNOMEDiagnostics(
			guestContext,
			options.Commands,
			sshArguments,
			logWriter,
		)
		return err
	}
	if err := transferSourceArchive(
		guestContext,
		options.Commands,
		sshArguments,
		sourceArchive,
		logWriter,
	); err != nil {
		return err
	}

	qmp, err := connectQMP(guestContext, qmpSocket)
	if err != nil {
		return err
	}
	defer func() { _ = qmp.close() }()

	marker := "/run/user/1100/robotgo-portal-consent-" +
		options.Cell + ".ready"
	testLogPath := filepath.Join(runDirectory, "portal-test.log")
	testLog, err := os.OpenFile(
		testLogPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create hosted portal test log: %w", err)
	}
	defer func() { _ = testLog.Close() }()
	testLogWriter := &boundedWriter{
		destination: testLog,
		remaining:   hostedTestLogLimit,
	}
	testCommand := exec.CommandContext(
		guestContext,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			hostedPortalTestCommand(options.Cell, marker),
		)...,
	)
	testOutput := io.MultiWriter(logWriter, testLogWriter)
	testCommand.Stdout = testOutput
	testCommand.Stderr = testOutput
	configureHostedProcess(testCommand)
	if err := testCommand.Start(); err != nil {
		return errors.New("start hosted portal integration test")
	}
	testResult := &hostedProcessResult{done: make(chan struct{})}
	go func() {
		testResult.err = testCommand.Wait()
		close(testResult.done)
	}()

	if err := waitForConsentMarker(
		guestContext,
		options.Commands,
		sshArguments,
		marker,
		options.Cell,
		logWriter,
		testResult.done,
	); err != nil {
		stopProcessGroup(testCommand, testResult.done)
		return err
	}
	if err := waitForPortalDialog(guestContext, testResult.done); err != nil {
		stopProcessGroup(testCommand, testResult.done)
		return err
	}
	if err := qmp.approvePortal(guestContext, options.Cell); err != nil {
		stopProcessGroup(testCommand, testResult.done)
		return err
	}
	<-testResult.done
	testError := testResult.err
	cleanupError := assertConsentMarkerRemoved(
		guestContext,
		options.Commands,
		sshArguments,
		marker,
		logWriter,
	)
	if testError != nil {
		stage := readPortalFailureStage(testLogPath)
		if stage == "" {
			testError = errors.New("hosted portal integration test failed")
		} else {
			testError = fmt.Errorf(
				"hosted portal integration test failed at stage %q",
				stage,
			)
		}
	}
	if err := errors.Join(testError, cleanupError); err != nil {
		return err
	}

	if err := shutdownHostedGuest(
		guestContext,
		options.Commands,
		sshArguments,
		logWriter,
	); err != nil {
		return err
	}
	select {
	case <-vmResult.done:
		vmExited = true
		if vmResult.err != nil {
			return errors.New("hosted GNOME VM exited unsuccessfully")
		}
	case <-runtimeContext.Done():
		return fmt.Errorf("hosted portal lifetime ended: %w", runtimeContext.Err())
	}
	return writeStatus(
		options.Output,
		"hosted portal test complete cell=%s commit=%s\n",
		options.Cell,
		options.Commit,
	)
}

func readPortalFailureStage(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > hostedTestLogLimit {
		return ""
	}
	matches := portalFailureStagePattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return string(matches[len(matches)-1][1])
}

func validateHostedIdentity(commit, cell string) error {
	if len(commit) != 40 {
		return errors.New("hosted portal commit is invalid")
	}
	if _, err := hex.DecodeString(commit); err != nil ||
		commit != strings.ToLower(commit) {
		return errors.New("hosted portal commit is invalid")
	}
	if cell != "remote-desktop" && cell != "screencast" {
		return errors.New("hosted portal cell is invalid")
	}
	return nil
}

func validateExactCleanCommit(
	ctx context.Context,
	commands CommandExecutor,
	repositoryRoot,
	expectedCommit string,
) error {
	var head bytes.Buffer
	if err := commands.Run(
		ctx,
		"git",
		[]string{"-C", repositoryRoot, "rev-parse", "--verify", "HEAD"},
		nil,
		&boundedWriter{destination: &head, remaining: maximumBuildInput},
	); err != nil {
		return errors.New("inspect hosted portal repository commit")
	}
	if strings.TrimSpace(head.String()) != expectedCommit {
		return errors.New("hosted portal repository is not at the exact commit")
	}
	var status bytes.Buffer
	if err := commands.Run(
		ctx,
		"git",
		[]string{
			"-C", repositoryRoot,
			"status", "--porcelain=v1", "-z", "--untracked-files=all",
		},
		nil,
		&boundedWriter{destination: &status, remaining: maximumBuildInput},
	); err != nil {
		return errors.New("inspect hosted portal repository status")
	}
	if status.Len() != 0 {
		return errors.New("hosted portal repository must be exactly clean")
	}
	return nil
}

func createSourceArchive(
	ctx context.Context,
	commands CommandExecutor,
	repositoryRoot,
	commit,
	archive string,
	output io.Writer,
) error {
	if err := commands.Run(
		ctx,
		"git",
		[]string{
			"-C", repositoryRoot,
			"archive",
			"--format=tar",
			"--output=" + archive,
			commit,
		},
		nil,
		output,
	); err != nil {
		return errors.New("create hosted portal source archive")
	}
	info, err := os.Lstat(archive)
	if err != nil ||
		!info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 ||
		info.Size() > maximumSourceArchive {
		return errors.New("hosted portal source archive is invalid")
	}
	if err := os.Chmod(archive, 0o600); err != nil {
		return fmt.Errorf("protect hosted portal source archive: %w", err)
	}
	return nil
}

func hostedSSHArguments(privateKey, runDirectory string, port int) []string {
	return []string{
		"-i", privateKey,
		"-p", strconv.Itoa(port),
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=" + filepath.Join(runDirectory, "known-hosts"),
		"-o", "ConnectTimeout=5",
	}
}

func waitForHostedSession(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	output io.Writer,
) error {
	command := "set -euo pipefail; runuser -u robotgo -- env " +
		"XDG_RUNTIME_DIR=/run/user/1100 " +
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus " +
		"timeout 130 /usr/local/libexec/robotgo-runner-wait-session"
	if err := commands.Run(
		ctx,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			command,
		),
		nil,
		output,
	); err != nil {
		return errors.New("wait for hosted GNOME portal session")
	}
	return nil
}

func enforceHostedEgressBoundary(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	output io.Writer,
) error {
	const command = `set -euo pipefail
systemctl is-enabled --quiet robotgo-runner-egress.service
systemctl start robotgo-runner-egress.service
systemctl is-active --quiet robotgo-runner-egress.service
nft --json list chain inet robotgo_runner output |
  jq -e 'any(.nftables[]; (.chain? // {}) as $chain |
    $chain.table == "robotgo_runner" and
    $chain.name == "output" and
    $chain.hook == "output" and
    $chain.policy == "drop")' >/dev/null`
	if err := commands.Run(
		ctx,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			command,
		),
		nil,
		output,
	); err != nil {
		return errors.New("enforce hosted GNOME egress boundary")
	}
	return nil
}

func transferSourceArchive(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	archive string,
	output io.Writer,
) error {
	scpArguments := replaceSSHPortFlag(sshArguments)
	scpArguments = append(
		scpArguments,
		archive,
		"root@127.0.0.1:/root/robotgo-source.tar",
	)
	if err := commands.Run(
		ctx,
		"scp",
		scpArguments,
		nil,
		output,
	); err != nil {
		return errors.New("transfer hosted portal source archive")
	}
	const extract = `set -euo pipefail
test "$(stat -c '%U:%a:%F' /root/robotgo-source.tar)" = "root:600:regular file"
test ! -e /home/robotgo/robotgo
test ! -e /home/robotgo/robotgo-source.tar
install -d -m 0755 -o robotgo -g robotgo /home/robotgo/robotgo
mv /root/robotgo-source.tar /home/robotgo/robotgo-source.tar
chown robotgo:robotgo /home/robotgo/robotgo-source.tar
runuser -u robotgo -- tar --no-same-owner --no-same-permissions -xf /home/robotgo/robotgo-source.tar -C /home/robotgo/robotgo
rm -f /home/robotgo/robotgo-source.tar
test -f /home/robotgo/robotgo/go.mod`
	if err := commands.Run(
		ctx,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			extract,
		),
		nil,
		output,
	); err != nil {
		return errors.New("extract hosted portal source archive")
	}
	return nil
}

func hostedPortalTestCommand(cell, marker string) string {
	environment := strings.Join([]string{
		"HOME=/home/robotgo",
		"XDG_RUNTIME_DIR=/run/user/1100",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus",
		"WAYLAND_DISPLAY=wayland-0",
		"XDG_CURRENT_DESKTOP=GNOME",
		"XDG_SESSION_DESKTOP=gnome",
		"XDG_SESSION_TYPE=wayland",
		"DISPLAY=:0",
		"HTTP_PROXY=http://10.0.2.2:3128",
		"HTTPS_PROXY=http://10.0.2.2:3128",
		"http_proxy=http://10.0.2.2:3128",
		"https_proxy=http://10.0.2.2:3128",
		"NO_PROXY=localhost,127.0.0.1",
		"no_proxy=localhost,127.0.0.1",
		"ROBOTGO_PORTAL_CONSENT_READY_FILE=" + marker,
	}, " ")
	var test string
	if cell == "screencast" {
		environment += " ROBOTGO_SCREENCAST_E2E=1"
		test = "go test -count=1 -timeout=3m -tags=pipewire,integration " +
			"./screen/portal " +
			"-run '^TestPipeWireCapturePersistentSessionIntegration$' -v"
	} else {
		environment += " ROBOTGO_REMOTE_DESKTOP_E2E=1"
		test = "go test -count=1 -timeout=3m -tags=integration " +
			"./input/portal " +
			"-run '^TestRemoteDesktopPortalRuntime$' -v"
	}
	guestCommand := "cd /home/robotgo/robotgo; exec " + test
	return "set -euo pipefail; exec runuser -u robotgo -- env " +
		environment + " bash -c " + shellQuote(guestCommand)
}

func waitForConsentMarker(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	marker,
	cell string,
	output io.Writer,
	testDone <-chan struct{},
) error {
	deadline := time.Now().Add(hostedTestTimeout)
	expected := cell + "\n"
	for {
		select {
		case <-testDone:
			return errors.New("hosted portal test exited before requesting consent")
		default:
		}
		var content bytes.Buffer
		checkContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := commands.Run(
			checkContext,
			"ssh",
			append(
				append([]string{}, sshArguments...),
				"root@127.0.0.1",
				"test -f "+shellQuote(marker)+" && cat "+shellQuote(marker),
			),
			nil,
			io.MultiWriter(
				output,
				&boundedWriter{
					destination: &content,
					remaining:   maximumBuildInput,
				},
			),
		)
		cancel()
		if err == nil {
			if content.String() != expected {
				return errors.New("hosted portal consent marker is invalid")
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("hosted portal test did not request consent")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-testDone:
			return errors.New("hosted portal test exited before requesting consent")
		case <-time.After(consentPollInterval):
		}
	}
}

func waitForPortalDialog(ctx context.Context, testDone <-chan struct{}) error {
	timer := time.NewTimer(portalDialogSettle)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-testDone:
		return errors.New("hosted portal test exited before consent approval")
	case <-timer.C:
		return nil
	}
}

func assertConsentMarkerRemoved(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	marker string,
	output io.Writer,
) error {
	if err := commands.Run(
		ctx,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			"test ! -e "+shellQuote(marker),
		),
		nil,
		output,
	); err != nil {
		return errors.New("hosted portal test left its consent marker")
	}
	return nil
}

func shutdownHostedGuest(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	output io.Writer,
) error {
	var confirmation bytes.Buffer
	err := commands.Run(
		ctx,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			"echo ROBOTGO_HOSTED_SHUTDOWN; systemctl poweroff --no-wall",
		),
		nil,
		io.MultiWriter(
			output,
			&boundedWriter{
				destination: &confirmation,
				remaining:   maximumBuildInput,
			},
		),
	)
	if err != nil &&
		!strings.Contains(confirmation.String(), "ROBOTGO_HOSTED_SHUTDOWN") {
		return errors.New("request hosted portal guest shutdown")
	}
	if !strings.Contains(confirmation.String(), "ROBOTGO_HOSTED_SHUTDOWN") {
		return errors.New("hosted portal guest did not confirm shutdown")
	}
	return nil
}

func configureHostedProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = qemuExitGrace
}

func stopProcessGroup(command *exec.Cmd, done <-chan struct{}) {
	if command == nil || command.Process == nil {
		return
	}
	select {
	case <-done:
		return
	default:
	}
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(qemuExitGrace):
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-done
	}
}
