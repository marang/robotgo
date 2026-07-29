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
	maximumSourceArchive    = 128 * 1024 * 1024
	gnomePortalDialogSettle = 2 * time.Second
	kdePortalDialogSettle   = 5 * time.Second
	gnomePortalDialogWait   = 45 * time.Second
	gnomePortalOutputLimit  = 64
	consentPollInterval     = 250 * time.Millisecond
	hostedTestTimeout       = 4 * time.Minute
	hostedTestLogLimit      = 2 * 1024 * 1024
	maximumPortalGeometry   = 256
	hostedXvfbEnvKey        = "ROBOTGO_HOSTED_XVFB"
	hostedBoundsStageMarker = "ROBOTGO_HOSTED_BOUNDS_STAGE="

	hostedBoundsStageBuild        = "build"
	hostedBoundsStageEnvironment  = "environment"
	hostedBoundsStageTopology     = "topology"
	hostedBoundsStageDisplayCount = "display-count"
	hostedBoundsStageMainDisplay  = "main-display"
	hostedBoundsStageDisplayZero  = "display-zero"
	hostedBoundsStageDisplayOne   = "display-one"
	hostedBoundsStageInvalidIndex = "invalid-index"
	hostedBoundsStageAggregate    = "aggregate"
	hostedBoundsStagePrimarySize  = "primary-size"
	hostedBoundsStageComplete     = "complete"

	// HostedCellRemoteDesktop exercises portal-backed pointer and keyboard
	// input in a disposable desktop guest.
	HostedCellRemoteDesktop = "remote-desktop"
	// HostedCellScreenCast exercises persistent portal-backed capture in a
	// disposable desktop guest.
	HostedCellScreenCast = "screencast"
	// HostedCellDisplayBounds exercises read-only public display geometry
	// without opening a portal session or exposing X11 to the test process.
	HostedCellDisplayBounds = "display-bounds"

	// HostedTopologySingle preserves the established one-output portal run.
	HostedTopologySingle = "single-output"
	// HostedTopologyMulti enables the manifest-bound two-output experiment.
	HostedTopologyMulti = "multi-output"
)

var portalFailureStagePattern = regexp.MustCompile(
	`ROBOTGO_PORTAL_STAGE=([a-z0-9-]{1,32})`,
)

var hostedBoundsFailureStagePattern = regexp.MustCompile(
	hostedBoundsStageMarker + `([a-z0-9-]{1,48})`,
)

var hostedBoundsFailureStages = func() map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, variant := range []string{
		HostedBoundsVariantNativeCGO,
		HostedBoundsVariantPureGo,
	} {
		for _, stage := range []string{
			hostedBoundsStageBuild,
			hostedBoundsStageEnvironment,
			hostedBoundsStageTopology,
			hostedBoundsStageDisplayCount,
			hostedBoundsStageMainDisplay,
			hostedBoundsStageDisplayZero,
			hostedBoundsStageDisplayOne,
			hostedBoundsStageInvalidIndex,
			hostedBoundsStageAggregate,
			hostedBoundsStagePrimarySize,
			hostedBoundsStageComplete,
		} {
			allowed[variant+"-"+stage] = struct{}{}
		}
	}
	return allowed
}()

var hostedDisplayFailureStagePattern = regexp.MustCompile(
	hostedDisplayFailureMarker + `([a-z0-9-]{1,32})`,
)
var hostedXvfbDisplayPattern = regexp.MustCompile(`^:[0-9]{1,5}$`)

var sessionFailureStagePattern = regexp.MustCompile(
	`ROBOTGO_SESSION_STAGE=([a-z0-9-]{1,32})`,
)

var errHostedPortalTestExitedBeforeConsent = errors.New(
	"hosted portal test exited before requesting consent",
)

var kdeLocatorFailureStages = map[string]struct{}{
	"bridge-unavailable":     {},
	"compositor-unavailable": {},
	"window-unavailable":     {},
}

// HostedRuntimeOptions identifies one credential-free integration test in a
// disposable desktop guest running inside a GitHub-hosted Linux runner.
type HostedRuntimeOptions struct {
	ManifestPath   string
	RepositoryRoot string
	StateRoot      string
	GuestFiles     string
	SSHPort        int
	Commit         string
	Cell           string
	Topology       string
	Output         io.Writer
	Commands       CommandExecutor
}

type hostedProcessResult struct {
	done chan struct{}
	err  error
}

type hostedPortalGeometry struct {
	width        int
	height       int
	dialogX      int
	dialogY      int
	dialogWidth  int
	dialogHeight int
	cursorX      int
	cursorY      int
}

type hostedPortalPoint struct {
	x int
	y int
}

// RunHosted transfers the exact clean commit into a disposable guest and runs
// one integration cell. Portal cells drive desktop consent independently
// through QMP; read-only cells do not create a portal session. The runner does
// not register a GitHub Actions runner or consume tokens.
func RunHosted(
	ctx context.Context,
	options HostedRuntimeOptions,
) (returnError error) {
	manifest, err := LoadManifest(options.ManifestPath)
	if err != nil {
		return err
	}
	if err := validateHostedIdentity(
		options.Commit,
		options.Cell,
		options.Topology,
	); err != nil {
		return err
	}
	if err := validateHostedHostDisplay(options.Topology); err != nil {
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
	if err := validateGuestFiles(options.GuestFiles, manifest.Lane); err != nil {
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
		manifest.Lane,
	)
	if err != nil {
		return err
	}
	imagePath := filepath.Join(
		options.StateRoot,
		"images",
		manifest.Lane+"-"+imageID+".qcow2",
	)
	if !reusableImage(imagePath, imagePath+".build.json", buildMetadata) {
		return fmt.Errorf("hosted %s image is unavailable or stale", manifest.Lane)
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
		userData: []byte(cloudConfig),
		metaData: []byte(
			"instance-id: " + instanceID +
				"\nlocal-hostname: robotgo-" + manifest.Lane + "-hosted\n",
		),
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
			options.Topology,
		)...,
	)
	qemuCommand.Stdout = logWriter
	qemuCommand.Stderr = logWriter
	configureHostedProcess(qemuCommand)
	if err := qemuCommand.Start(); err != nil {
		return fmt.Errorf("start hosted %s VM", manifest.Lane)
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
		return fmt.Errorf("wait for hosted %s VM: %w", manifest.Lane, err)
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
		collectHostedDiagnostics(
			guestContext,
			options.Commands,
			sshArguments,
			logWriter,
			manifest.Lane,
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
	if options.Topology == HostedTopologyMulti {
		if err := configureHostedGuestDisplay(
			guestContext,
			options.Commands,
			sshArguments,
			manifest.Lane,
			logWriter,
		); err != nil {
			return err
		}
	}

	marker := ""
	if hostedCellUsesPortal(options.Cell) {
		marker = "/run/user/1100/robotgo-portal-consent-" +
			options.Cell + ".ready"
	}
	testLogPath := filepath.Join(runDirectory, "hosted-test.log")
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
	portalTestCommand, err := hostedPortalTestCommandForTopology(
		manifest.Lane,
		options.Cell,
		marker,
		options.Topology,
		manifest.HostedDisplay,
	)
	if err != nil {
		return err
	}
	testCommand := exec.CommandContext(
		guestContext,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			portalTestCommand,
		)...,
	)
	testOutput := io.MultiWriter(logWriter, testLogWriter)
	testCommand.Stdout = testOutput
	testCommand.Stderr = testOutput
	configureHostedProcess(testCommand)
	if err := testCommand.Start(); err != nil {
		return errors.New("start hosted integration test")
	}
	testResult := &hostedProcessResult{done: make(chan struct{})}
	go func() {
		testResult.err = testCommand.Wait()
		close(testResult.done)
	}()

	if hostedPortalApprovalRequired(
		manifest.Lane,
		options.Cell,
		options.Topology,
	) {
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
			if errors.Is(err, errHostedPortalTestExitedBeforeConsent) {
				if stage := readPortalFailureStage(testLogPath); stage != "" {
					return fmt.Errorf(
						"hosted portal test exited before consent at stage %q",
						stage,
					)
				}
			}
			return err
		}
		if manifest.Lane == portalLaneGNOME {
			if err := waitForHostedGNOMEPortalDialog(
				guestContext,
				options.Commands,
				sshArguments,
				options.Cell,
			); err != nil {
				stopProcessGroup(testCommand, testResult.done)
				return err
			}
		}
		if err := waitForPortalDialog(
			guestContext,
			testResult.done,
			portalDialogSettle(manifest.Lane),
		); err != nil {
			stopProcessGroup(testCommand, testResult.done)
			return err
		}
		if err := approveHostedPortal(
			guestContext,
			options.Commands,
			sshArguments,
			qmpSocket,
			manifest.Lane,
			options.Cell,
			options.Topology,
			manifest.HostedDisplay,
			options.Output,
		); err != nil {
			stopProcessGroup(testCommand, testResult.done)
			return err
		}
	}
	<-testResult.done
	testError := testResult.err
	var cleanupError error
	if marker != "" {
		cleanupError = assertConsentMarkerRemoved(
			guestContext,
			options.Commands,
			sshArguments,
			marker,
			logWriter,
		)
	}
	if testError != nil {
		if hostedCellUsesPortal(options.Cell) {
			stage := readPortalFailureStage(testLogPath)
			if stage == "" {
				testError = errors.New(
					"hosted portal integration test failed",
				)
			} else {
				testError = fmt.Errorf(
					"hosted portal integration test failed at stage %q",
					stage,
				)
			}
		} else {
			stage := readHostedBoundsFailureStage(testLogPath)
			if stage == "" {
				testError = errors.New(
					"hosted display-bounds integration test failed",
				)
			} else {
				testError = fmt.Errorf(
					"hosted display-bounds integration test failed at stage %q",
					stage,
				)
			}
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
			return fmt.Errorf("hosted %s VM exited unsuccessfully", manifest.Lane)
		}
	case <-runtimeContext.Done():
		return fmt.Errorf("hosted integration lifetime ended: %w", runtimeContext.Err())
	}
	return writeStatus(
		options.Output,
		"hosted test complete cell=%s commit=%s\n",
		options.Cell,
		options.Commit,
	)
}

func validateHostedHostDisplay(topology string) error {
	if topology == HostedTopologySingle {
		return nil
	}
	if topology != HostedTopologyMulti ||
		os.Getenv(hostedXvfbEnvKey) != "1" ||
		!hostedXvfbDisplayPattern.MatchString(os.Getenv("DISPLAY")) {
		return errors.New(
			"hosted multi-output requires an isolated Xvfb display",
		)
	}
	return nil
}

func configureHostedGuestDisplay(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	lane string,
	output io.Writer,
) error {
	currentDesktop, sessionDesktop, err := hostedDesktopEnvironment(lane)
	if err != nil {
		return err
	}
	command := "cd /home/robotgo/robotgo; exec env " +
		HostedGuestEnvKey + "=1 " +
		"go run ./internal/cmd/portalrunner guest-display " +
		"-manifest infrastructure/portal-runner/" + lane +
		"/manifest.json"
	var commandOutput bytes.Buffer
	if err := commands.Run(
		ctx,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			"exec runuser -u robotgo -- env "+
				"HOME=/home/robotgo "+
				"XDG_RUNTIME_DIR=/run/user/1100 "+
				"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus "+
				"WAYLAND_DISPLAY=wayland-0 "+
				"XDG_CURRENT_DESKTOP="+currentDesktop+" "+
				"XDG_SESSION_DESKTOP="+sessionDesktop+" "+
				"XDG_SESSION_TYPE=wayland "+
				"QT_QPA_PLATFORM=wayland "+
				"DISPLAY=:0 "+
				"HTTP_PROXY=http://10.0.2.2:3128 "+
				"HTTPS_PROXY=http://10.0.2.2:3128 "+
				"http_proxy=http://10.0.2.2:3128 "+
				"https_proxy=http://10.0.2.2:3128 "+
				"NO_PROXY=localhost,127.0.0.1 "+
				"no_proxy=localhost,127.0.0.1 "+
				"bash -c "+shellQuote(command),
		),
		nil,
		&boundedWriter{
			destination: &commandOutput,
			remaining:   hostedTopologyOutput,
		},
	); err != nil {
		if stage := parseHostedDisplayFailureStage(
			commandOutput.Bytes(),
		); stage != "" {
			return fmt.Errorf(
				"configure hosted display topology at stage %q",
				stage,
			)
		}
		return errors.New("configure hosted display topology")
	}
	return writeStatus(
		output,
		"hosted display topology configured lane=%s\n",
		lane,
	)
}

func parseHostedDisplayFailureStage(output []byte) string {
	matches := hostedDisplayFailureStagePattern.FindAllSubmatch(output, -1)
	if len(matches) != 1 {
		return ""
	}
	stage := string(matches[0][1])
	switch stage {
	case hostedDisplayStageManifest,
		hostedDisplayStageLane,
		hostedDisplayStageStatus,
		hostedDisplayStageGNOMEBus,
		hostedDisplayStageGNOMEState,
		hostedDisplayStageGNOMEPlan,
		hostedDisplayStageGNOMEApply,
		hostedDisplayStageGNOMESettle,
		hostedDisplayStageKDEStateRun,
		hostedDisplayStageKDEStateJSON,
		hostedDisplayStageKDEPlan,
		hostedDisplayStageKDEApply,
		hostedDisplayStageKDESettle:
		return stage
	default:
		return ""
	}
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

func readHostedBoundsFailureStage(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > hostedTestLogLimit {
		return ""
	}
	matches := hostedBoundsFailureStagePattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	stage := string(matches[len(matches)-1][1])
	if _, allowed := hostedBoundsFailureStages[stage]; !allowed {
		return ""
	}
	return stage
}

func validateHostedIdentity(commit, cell, topology string) error {
	if len(commit) != 40 {
		return errors.New("hosted portal commit is invalid")
	}
	if _, err := hex.DecodeString(commit); err != nil ||
		commit != strings.ToLower(commit) {
		return errors.New("hosted portal commit is invalid")
	}
	if cell != HostedCellRemoteDesktop &&
		cell != HostedCellScreenCast &&
		cell != HostedCellDisplayBounds {
		return errors.New("hosted portal cell is invalid")
	}
	if topology != HostedTopologySingle &&
		topology != HostedTopologyMulti {
		return errors.New("hosted portal topology is invalid")
	}
	if cell == HostedCellDisplayBounds && topology != HostedTopologyMulti {
		return errors.New(
			"hosted display-bounds evidence requires multi-output topology",
		)
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
	var diagnostic bytes.Buffer
	diagnosticWriter := &truncatingWriter{
		destination: &diagnostic,
		remaining:   maximumBuildInput,
	}
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
		io.MultiWriter(output, diagnosticWriter),
	); err != nil {
		if stage := readSessionFailureStage(diagnostic.Bytes()); stage != "" {
			return fmt.Errorf(
				"wait for hosted portal session at stage %q",
				stage,
			)
		}
		return errors.New("wait for hosted portal session")
	}
	return nil
}

func readSessionFailureStage(data []byte) string {
	if len(data) > maximumBuildInput {
		return ""
	}
	matches := sessionFailureStagePattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return string(matches[len(matches)-1][1])
}

type truncatingWriter struct {
	destination io.Writer
	remaining   int64
}

func (writer *truncatingWriter) Write(data []byte) (int, error) {
	inputLength := len(data)
	if writer.remaining <= 0 {
		return inputLength, nil
	}
	if int64(len(data)) > writer.remaining {
		data = data[:writer.remaining]
	}
	captureLength := len(data)
	written, err := writer.destination.Write(data)
	writer.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	if written != captureLength {
		return written, io.ErrShortWrite
	}
	return inputLength, nil
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
		return errors.New("enforce hosted portal egress boundary")
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

func hostedPortalTestCommand(lane, cell, marker string) string {
	command, _ := hostedPortalTestCommandForTopology(
		lane,
		cell,
		marker,
		HostedTopologySingle,
		HostedDisplay{},
	)
	return command
}

func hostedPortalTestCommandForTopology(
	lane,
	cell,
	marker,
	topology string,
	display HostedDisplay,
) (string, error) {
	currentDesktop, sessionDesktop, err := hostedDesktopEnvironment(lane)
	if err != nil {
		return "", err
	}
	if topology != HostedTopologySingle &&
		topology != HostedTopologyMulti {
		return "", errors.New("hosted portal topology is invalid")
	}
	if !hostedCellUsesPortal(cell) && cell != HostedCellDisplayBounds {
		return "", errors.New("hosted portal cell is invalid")
	}
	if hostedCellUsesPortal(cell) && marker == "" {
		return "", errors.New("hosted portal cell requires a consent marker")
	}
	if cell == HostedCellDisplayBounds {
		if marker != "" {
			return "", errors.New(
				"hosted display-bounds cell must not receive a consent marker",
			)
		}
		if topology != HostedTopologyMulti {
			return "", errors.New(
				"hosted display-bounds evidence requires multi-output topology",
			)
		}
	}
	environmentParts := []string{
		"HOME=/home/robotgo",
		"XDG_RUNTIME_DIR=/run/user/1100",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus",
		"WAYLAND_DISPLAY=wayland-0",
		"XDG_CURRENT_DESKTOP=" + currentDesktop,
		"XDG_SESSION_DESKTOP=" + sessionDesktop,
		"XDG_SESSION_TYPE=wayland",
		"HTTP_PROXY=http://10.0.2.2:3128",
		"HTTPS_PROXY=http://10.0.2.2:3128",
		"http_proxy=http://10.0.2.2:3128",
		"https_proxy=http://10.0.2.2:3128",
		"NO_PROXY=localhost,127.0.0.1",
		"no_proxy=localhost,127.0.0.1",
	}
	if hostedCellUsesPortal(cell) {
		environmentParts = append(
			environmentParts,
			"DISPLAY=:0",
			"ROBOTGO_PORTAL_CONSENT_READY_FILE="+marker,
		)
	}
	environment := strings.Join(environmentParts, " ")
	if topology == HostedTopologyMulti {
		encoded, err := display.Encode()
		if err != nil {
			return "", err
		}
		if cell == HostedCellDisplayBounds {
			environment += " " + HostedExpectedOutputsEnvKey + "=" +
				shellQuote(encoded)
		} else {
			environment += " " + PortalMultiOutputEnvKey + "=1" +
				" " + PortalExpectedOutputsEnvKey + "=" + shellQuote(encoded)
		}
	}
	var test string
	switch cell {
	case HostedCellScreenCast:
		environment += " ROBOTGO_SCREENCAST_E2E=1" +
			" ROBOTGO_SCREENCAST_REQUIRE_MONITOR=1"
		test = "go test -count=1 -timeout=3m -tags=pipewire,integration " +
			"./screen/portal " +
			"-run '^TestPipeWireCapturePersistentSessionIntegration$' -v"
	case HostedCellRemoteDesktop:
		environment += " ROBOTGO_REMOTE_DESKTOP_E2E=1"
		test = "go test -count=1 -timeout=3m -tags=integration " +
			"./input/portal " +
			"-run '^TestRemoteDesktopPortalRuntime$' -v"
	case HostedCellDisplayBounds:
		environment += " ROBOTGO_HOSTED_BOUNDS_E2E=1"
		boundsTests := strings.Join([]string{
			"printf '%s\\n' '" + hostedBoundsStageMarker +
				HostedBoundsVariantNativeCGO + "-" +
				hostedBoundsStageBuild + "' && " +
				HostedBoundsVariantEnvKey + "=" +
				HostedBoundsVariantNativeCGO +
				" go test -count=1 -timeout=3m " +
				"-tags=wayland,hostedboundsintegration . " +
				"-run '^TestHostedWaylandBoundsCGORuntime$' -v",
			"printf '%s\\n' '" + hostedBoundsStageMarker +
				HostedBoundsVariantPureGo + "-" +
				hostedBoundsStageBuild + "' && " +
				HostedBoundsVariantEnvKey + "=" +
				HostedBoundsVariantPureGo +
				" CGO_ENABLED=0 go test -count=1 -timeout=3m " +
				"-tags=hostedboundsintegration . " +
				"-run '^TestHostedWaylandBoundsPureGoRuntime$' -v",
		}, " && ")
		test = "bash -c " + shellQuote(boundsTests)
	}
	guestCommand := "cd /home/robotgo/robotgo; exec " + test
	return "set -euo pipefail; exec runuser -u robotgo -- env -u DISPLAY " +
		environment + " bash -c " + shellQuote(guestCommand), nil
}

func hostedCellUsesPortal(cell string) bool {
	return cell == HostedCellRemoteDesktop || cell == HostedCellScreenCast
}

func hostedDesktopEnvironment(lane string) (current, session string, err error) {
	switch lane {
	case portalLaneGNOME:
		return "GNOME", "gnome", nil
	case portalLaneKDE:
		return "KDE", "plasmawayland", nil
	default:
		return "", "", errors.New("hosted portal lane is invalid")
	}
}

func hostedPortalApprovalRequired(lane, cell, topology string) bool {
	return hostedCellUsesPortal(cell) &&
		(lane == portalLaneGNOME ||
			(lane == portalLaneKDE && cell == HostedCellScreenCast))
}

func approveHostedPortal(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	qmpSocket,
	lane,
	cell,
	topology string,
	display HostedDisplay,
	output io.Writer,
) error {
	if lane == portalLaneGNOME &&
		cell == "screencast" &&
		topology == HostedTopologyMulti {
		targets, err := gnomePortalPhysicalCardTargets(display)
		if err != nil {
			return err
		}
		qmp, err := connectQMP(ctx, qmpSocket)
		if err != nil {
			return err
		}
		var approvalError error
		for _, target := range targets.points {
			approvalError = qmp.clickAbsolute(
				ctx,
				target.x,
				target.y,
				target.width,
				target.height,
			)
			if approvalError != nil {
				break
			}
			approvalError = waitQMPChord(ctx)
			if approvalError != nil {
				break
			}
		}
		if approvalError == nil {
			approvalError = qmp.sendChord(ctx, qmpKeyAlt, qmpKeyS)
		}
		return errors.Join(approvalError, qmp.close())
	}
	if lane == portalLaneKDE && cell == "screencast" {
		geometry, err := locateHostedKDEScreenCast(
			ctx,
			commands,
			sshArguments,
		)
		if err != nil {
			return err
		}
		if err := writeStatus(
			output,
			"hosted KDE dialog geometry display=%dx%d dialog=%d,%d,%dx%d\n",
			geometry.width,
			geometry.height,
			geometry.dialogX,
			geometry.dialogY,
			geometry.dialogWidth,
			geometry.dialogHeight,
		); err != nil {
			return err
		}
		qmp, err := connectQMP(ctx, qmpSocket)
		if err != nil {
			return err
		}
		card, err := kdePortalCardTarget(geometry)
		if err != nil {
			return errors.Join(err, qmp.close())
		}
		approvalError := qmp.clickAbsolute(
			ctx,
			card.x,
			card.y,
			geometry.width,
			geometry.height,
		)
		if approvalError == nil {
			approvalError = waitQMPChord(ctx)
		}
		if approvalError == nil {
			var observed hostedPortalGeometry
			observed, approvalError = locateHostedKDEScreenCast(
				ctx,
				commands,
				sshArguments,
			)
			if approvalError == nil &&
				(!kdePortalSameDialog(geometry, observed) ||
					!kdePortalPointerAt(card, observed)) {
				approvalError = errors.New(
					"hosted KDE QMP pointer calibration failed",
				)
			}
		}
		if approvalError == nil {
			approvalError = writeStatus(
				output,
				"hosted KDE QMP pointer calibration passed\n",
			)
		}
		if approvalError == nil && topology == HostedTopologyMulti {
			// Plasma 5.27 orders Full Workspace, New Virtual Output, then
			// physical outputs in a two-column CardsGridView. Toggle the
			// calibrated card back off, scroll its private view to the second
			// row, then click both physical cards at digest-bound positions.
			approvalError = qmp.clickAbsolute(
				ctx,
				card.x,
				card.y,
				geometry.width,
				geometry.height,
			)
			if approvalError == nil {
				approvalError = waitQMPChord(ctx)
			}
			if approvalError == nil {
				approvalError = qmp.scrollKDEPhysicalOutputs(ctx)
			}
			if approvalError == nil {
				var targets [2]hostedPortalPoint
				targets, approvalError =
					kdePortalPhysicalCardTargets(geometry)
				for _, target := range targets {
					if approvalError != nil {
						break
					}
					approvalError = qmp.clickAbsolute(
						ctx,
						target.x,
						target.y,
						geometry.width,
						geometry.height,
					)
					if approvalError == nil {
						approvalError = waitQMPChord(ctx)
					}
				}
			}
		}
		if approvalError == nil {
			// Plasma 5.27 SystemDialog handles Return at the focused loader and
			// accepts only after the selected source enables its OK button.
			approvalError = qmp.sendChord(ctx, qmpKeyReturn)
		}
		return errors.Join(approvalError, qmp.close())
	}
	qmp, err := connectQMP(ctx, qmpSocket)
	if err != nil {
		return err
	}
	return errors.Join(
		qmp.approvePortal(ctx, lane, cell, topology),
		qmp.close(),
	)
}

const (
	gnomePortalDialogWidth        = 660
	gnomePortalDialogHeight       = 500
	gnomePortalHorizontalInset    = 43
	gnomePortalTargetYNumerator   = 3
	gnomePortalTargetYDenominator = 5
)

type hostedPortalTarget struct {
	x      int
	y      int
	width  int
	height int
}

type hostedPortalTargetSet struct {
	points [2]hostedPortalTarget
}

func gnomePortalPhysicalCardTargets(
	display HostedDisplay,
) (hostedPortalTargetSet, error) {
	if err := display.Validate(); err != nil {
		return hostedPortalTargetSet{}, err
	}
	if len(display.Outputs) != 2 {
		return hostedPortalTargetSet{}, errors.New(
			"hosted GNOME ScreenCast requires exactly two outputs",
		)
	}
	minX, minY := display.Outputs[0].X, display.Outputs[0].Y
	maxX := minX + display.Outputs[0].Width
	maxY := minY + display.Outputs[0].Height
	for _, output := range display.Outputs[1:] {
		minX = min(minX, output.X)
		minY = min(minY, output.Y)
		maxX = max(maxX, output.X+output.Width)
		maxY = max(maxY, output.Y+output.Height)
	}
	width, height := maxX-minX, maxY-minY
	primary := display.Outputs[0]
	if primary.Width < gnomePortalDialogWidth ||
		primary.Height < gnomePortalDialogHeight {
		return hostedPortalTargetSet{}, errors.New(
			"hosted GNOME primary output cannot contain the ScreenCast dialog",
		)
	}
	dialogX := primary.X - minX +
		(primary.Width-gnomePortalDialogWidth)/2
	dialogY := primary.Y - minY +
		(primary.Height-gnomePortalDialogHeight)/2
	containerX := dialogX + gnomePortalHorizontalInset
	containerWidth := gnomePortalDialogWidth -
		2*gnomePortalHorizontalInset
	targetY := dialogY +
		gnomePortalDialogHeight*gnomePortalTargetYNumerator/
			gnomePortalTargetYDenominator
	var targets hostedPortalTargetSet
	for index, output := range display.Outputs {
		targetX := containerX +
			(output.X-minX+output.Width/2)*containerWidth/width
		if targetX < dialogX ||
			targetX >= dialogX+gnomePortalDialogWidth ||
			targetY < dialogY ||
			targetY >= dialogY+gnomePortalDialogHeight {
			return hostedPortalTargetSet{}, errors.New(
				"hosted GNOME ScreenCast target is outside the expected dialog",
			)
		}
		targets.points[index] = hostedPortalTarget{
			x: targetX, y: targetY,
			width: width, height: height,
		}
	}
	return targets, nil
}

func locateHostedKDEScreenCast(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
) (hostedPortalGeometry, error) {
	approvalContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := "exec runuser -u robotgo -- env " +
		"XDG_RUNTIME_DIR=/run/user/1100 " +
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus " +
		"/usr/local/libexec/robotgo-runner-locate-screencast"
	var geometryOutput bytes.Buffer
	runError := commands.Run(
		approvalContext,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			command,
		),
		nil,
		&boundedWriter{
			destination: &geometryOutput,
			remaining:   maximumPortalGeometry,
		},
	)
	fields := strings.Fields(geometryOutput.String())
	if len(fields) == 2 && fields[0] == "error" {
		if _, allowed := kdeLocatorFailureStages[fields[1]]; allowed {
			return hostedPortalGeometry{}, fmt.Errorf(
				"locate hosted KDE ScreenCast controls at stage %q",
				fields[1],
			)
		}
	}
	if runError != nil {
		return hostedPortalGeometry{}, errors.New(
			"locate hosted KDE ScreenCast controls",
		)
	}
	if len(fields) != 9 || fields[0] != "ok" {
		return hostedPortalGeometry{}, errors.New(
			"hosted KDE ScreenCast geometry is invalid",
		)
	}
	values := make([]int, len(fields)-1)
	for index, field := range fields[1:] {
		value, err := strconv.Atoi(field)
		if err != nil {
			return hostedPortalGeometry{}, errors.New(
				"hosted KDE ScreenCast geometry is invalid",
			)
		}
		values[index] = value
	}
	geometry := hostedPortalGeometry{
		width: values[0], height: values[1],
		dialogX: values[2], dialogY: values[3],
		dialogWidth: values[4], dialogHeight: values[5],
		cursorX: values[6], cursorY: values[7],
	}
	if geometry.width < 640 ||
		geometry.width > 8192 ||
		geometry.height < 480 ||
		geometry.height > 8192 ||
		geometry.dialogX < 0 ||
		geometry.dialogX >= geometry.width ||
		geometry.dialogY < 0 ||
		geometry.dialogY >= geometry.height ||
		geometry.dialogWidth < 320 ||
		geometry.dialogHeight < 240 ||
		geometry.dialogWidth > geometry.width-geometry.dialogX ||
		geometry.dialogHeight > geometry.height-geometry.dialogY ||
		geometry.cursorX < 0 ||
		geometry.cursorX >= geometry.width ||
		geometry.cursorY < 0 ||
		geometry.cursorY >= geometry.height {
		return hostedPortalGeometry{}, errors.New(
			"hosted KDE ScreenCast geometry is invalid",
		)
	}
	return geometry, nil
}

func waitForHostedGNOMEPortalDialog(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	cell string,
) error {
	if cell != HostedCellRemoteDesktop && cell != HostedCellScreenCast {
		return errors.New("hosted GNOME portal dialog cell is invalid")
	}

	command := "exec runuser -u robotgo -- env " +
		"XDG_RUNTIME_DIR=/run/user/1100 " +
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus " +
		"/usr/local/libexec/robotgo-runner-wait-portal-dialog " +
		shellQuote(cell)

	dialogContext, cancel := context.WithTimeout(ctx, gnomePortalDialogWait)
	defer cancel()
	var output bytes.Buffer
	err := commands.Run(
		dialogContext,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			command,
		),
		nil,
		&boundedWriter{
			destination: &output,
			remaining:   gnomePortalOutputLimit,
		},
	)
	if err == nil && output.String() == "ok\n" {
		return nil
	}
	if output.String() == "error dialog-unavailable\n" {
		return errors.New(
			`locate hosted GNOME portal dialog at stage "dialog-unavailable"`,
		)
	}
	return errors.New("locate hosted GNOME portal dialog")
}

const (
	kdeCardLeftXNumerator  = 1
	kdeCardRightXNumerator = 3
	kdeCardXNumerator      = 3
	kdeCardXDenominator    = 4
	kdeCardYNumerator      = 1
	kdeCardYDenominator    = 2

	kdePointerTolerance = 4
)

func kdePortalPhysicalCardTargets(
	geometry hostedPortalGeometry,
) ([2]hostedPortalPoint, error) {
	targets := [2]hostedPortalPoint{
		{
			x: kdePortalRelativeCoordinate(
				geometry.dialogX,
				geometry.dialogWidth,
				kdeCardLeftXNumerator,
				kdeCardXDenominator,
			),
			y: kdePortalRelativeCoordinate(
				geometry.dialogY,
				geometry.dialogHeight,
				kdeCardYNumerator,
				kdeCardYDenominator,
			),
		},
		{
			x: kdePortalRelativeCoordinate(
				geometry.dialogX,
				geometry.dialogWidth,
				kdeCardRightXNumerator,
				kdeCardXDenominator,
			),
			y: kdePortalRelativeCoordinate(
				geometry.dialogY,
				geometry.dialogHeight,
				kdeCardYNumerator,
				kdeCardYDenominator,
			),
		},
	}
	for _, target := range targets {
		if !kdePortalPointInsideDialog(target, geometry) {
			return [2]hostedPortalPoint{}, errors.New(
				"hosted KDE ScreenCast physical target is outside the active dialog",
			)
		}
	}
	return targets, nil
}

func kdePortalCardTarget(
	geometry hostedPortalGeometry,
) (hostedPortalPoint, error) {
	card := hostedPortalPoint{
		x: kdePortalRelativeCoordinate(
			geometry.dialogX,
			geometry.dialogWidth,
			kdeCardXNumerator,
			kdeCardXDenominator,
		),
		y: kdePortalRelativeCoordinate(
			geometry.dialogY,
			geometry.dialogHeight,
			kdeCardYNumerator,
			kdeCardYDenominator,
		),
	}
	if !kdePortalPointInsideDialog(card, geometry) {
		return hostedPortalPoint{}, errors.New(
			"hosted KDE ScreenCast target is outside the active dialog",
		)
	}
	return card, nil
}

func kdePortalRelativeCoordinate(
	start,
	extent,
	numerator,
	denominator int,
) int {
	return start + extent*numerator/denominator
}

func kdePortalSameDialog(
	expected,
	observed hostedPortalGeometry,
) bool {
	return expected.width == observed.width &&
		expected.height == observed.height &&
		expected.dialogX == observed.dialogX &&
		expected.dialogY == observed.dialogY &&
		expected.dialogWidth == observed.dialogWidth &&
		expected.dialogHeight == observed.dialogHeight
}

func kdePortalPointerAt(
	expected hostedPortalPoint,
	observed hostedPortalGeometry,
) bool {
	return absoluteDifference(expected.x, observed.cursorX) <=
		kdePointerTolerance &&
		absoluteDifference(expected.y, observed.cursorY) <=
			kdePointerTolerance
}

func absoluteDifference(first, second int) int {
	if first > second {
		return first - second
	}
	return second - first
}

func kdePortalPointInsideDialog(
	point hostedPortalPoint,
	geometry hostedPortalGeometry,
) bool {
	return point.x >= geometry.dialogX &&
		point.x < geometry.dialogX+geometry.dialogWidth &&
		point.y >= geometry.dialogY &&
		point.y < geometry.dialogY+geometry.dialogHeight
}

func collectHostedDiagnostics(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	output io.Writer,
	lane string,
) {
	unit := "gdm3"
	if lane == portalLaneKDE {
		unit = "sddm"
	}
	diagnosticCommand := `set +e
echo ROBOTGO_PORTAL_DIAGNOSTICS
systemctl status ` + unit + ` --no-pager --full
loginctl list-sessions --no-legend
loginctl user-status robotgo --no-pager
test -d /run/user/1100 && find /run/user/1100 -maxdepth 1 -type s -printf '%f\n'
journalctl --boot --unit=` + unit + ` --no-pager --lines=120
journalctl --boot _UID=1100 --no-pager --lines=120
echo ROBOTGO_PORTAL_DIAGNOSTICS_END`
	diagnosticContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_ = commands.Run(
		diagnosticContext,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			diagnosticCommand,
		),
		nil,
		output,
	)
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
			return errHostedPortalTestExitedBeforeConsent
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
			return errHostedPortalTestExitedBeforeConsent
		case <-time.After(consentPollInterval):
		}
	}
}

func portalDialogSettle(lane string) time.Duration {
	if lane == portalLaneKDE {
		return kdePortalDialogSettle
	}
	return gnomePortalDialogSettle
}

func waitForPortalDialog(
	ctx context.Context,
	testDone <-chan struct{},
	settle time.Duration,
) error {
	timer := time.NewTimer(settle)
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
