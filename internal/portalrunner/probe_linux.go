//go:build linux

package portalrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	probeTimeout               = 10 * time.Minute
	runnerJobStartedHookPath   = "/usr/local/libexec/robotgo-runner-job-started-hook.sh"
	runnerJobCompletedHookPath = "/usr/local/libexec/robotgo-runner-job-completed-hook.sh"
)

// ImageProbeOptions defines a private non-runner guest boot.
type ImageProbeOptions struct {
	ManifestPath   string
	RepositoryRoot string
	StateRoot      string
	GuestFiles     string
	SSHPort        int
	Output         io.Writer
	Commands       CommandExecutor
}

// ProbeImage proves that the pinned image starts the required local GNOME
// portal session and can reach an allowlisted HTTPS endpoint only via proxy.
func ProbeImage(
	ctx context.Context,
	options ImageProbeOptions,
) (returnError error) {
	manifest, err := LoadManifest(options.ManifestPath)
	if err != nil {
		return err
	}
	if manifest.Lane != portalLaneGNOME {
		return errors.New("interactive image probe supports only the GNOME lane")
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
	imageID, buildMetadata, _, err := computeImageIdentity(
		options.ManifestPath,
		options.RepositoryRoot,
		options.GuestFiles,
		manifest.Lane,
	)
	if err != nil {
		return err
	}
	imagePath := filepath.Join(options.StateRoot, "images", "gnome-"+imageID+".qcow2")
	manifestCopy := imagePath + ".build.json"
	if !reusableImage(imagePath, manifestCopy, buildMetadata) {
		return errors.New("protected GNOME runner image is unavailable or does not match its manifest")
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
	probeContext, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	logPath := filepath.Join(runDirectory, "probe.log")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create portal runner probe log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	logWriter := &boundedWriter{destination: logFile, remaining: buildLogLimit}

	overlay := filepath.Join(runDirectory, "probe.qcow2")
	if err := options.Commands.Run(
		probeContext,
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
		return fmt.Errorf("protect portal runner probe overlay: %w", err)
	}

	privateKey := filepath.Join(runDirectory, "probe-ssh")
	if err := options.Commands.Run(
		probeContext,
		"ssh-keygen",
		[]string{"-q", "-t", "ed25519", "-N", "", "-f", privateKey},
		nil,
		logWriter,
	); err != nil {
		return err
	}
	publicKey, err := os.ReadFile(privateKey + ".pub")
	if err != nil {
		return fmt.Errorf("read ephemeral probe public key: %w", err)
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
	if err := os.WriteFile(
		userData,
		[]byte(cloudConfig),
		0o600,
	); err != nil {
		return fmt.Errorf("write portal runner probe cloud config: %w", err)
	}
	if err := os.WriteFile(
		metaData,
		[]byte("instance-id: "+instanceID+"\nlocal-hostname: robotgo-gnome-probe\n"),
		0o600,
	); err != nil {
		return fmt.Errorf("write portal runner probe metadata: %w", err)
	}
	if err := os.WriteFile(
		networkData,
		[]byte(runtimeNetworkConfig()),
		0o600,
	); err != nil {
		return fmt.Errorf("write portal runner probe network config: %w", err)
	}
	if err := options.Commands.Run(
		probeContext,
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
		return fmt.Errorf("listen for portal runner probe proxy: %w", err)
	}
	proxyContext, stopProxy := context.WithCancel(probeContext)
	proxyDone := make(chan error, 1)
	go func() { proxyDone <- proxy.Serve(proxyContext, proxyListener) }()
	defer func() {
		stopProxy()
		_ = proxyListener.Close()
		<-proxyDone
	}()

	pidFile := filepath.Join(runDirectory, "qemu.pid")
	serialLog := filepath.Join(runDirectory, "serial.log")
	serialFile, err := os.OpenFile(
		serialLog,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create portal runner probe serial log: %w", err)
	}
	if err := serialFile.Close(); err != nil {
		return fmt.Errorf("close portal runner probe serial log: %w", err)
	}
	if err := writeStatus(options.Output, "probe guest boot\n"); err != nil {
		return err
	}
	if err := options.Commands.Run(
		probeContext,
		"qemu-system-x86_64",
		buildQEMUArguments(
			manifest,
			overlay,
			seedImage,
			pidFile,
			serialLog,
			options.SSHPort,
			true,
		),
		nil,
		logWriter,
	); err != nil {
		return err
	}
	qemuPID, err := readPID(pidFile)
	if err != nil {
		return err
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = syscall.Kill(qemuPID, syscall.SIGKILL)
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
	if err := waitForSSH(
		probeContext,
		options.Commands,
		sshArguments,
		logWriter,
	); err != nil {
		return err
	}
	if err := writeStatus(options.Output, "probe guest session\n"); err != nil {
		return err
	}
	proxyURL := "http://" + manifest.ProxyAddress()
	sessionEnvironment := "XDG_RUNTIME_DIR=/run/user/1100 " +
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1100/bus"
	sessionCommand := "runuser -u robotgo -- env " + sessionEnvironment + " "
	checks := []struct {
		name    string
		command string
	}{
		{"image marker", "test -f /var/lib/robotgo-runner/image-ready"},
		{
			"go version",
			"test \"$(go env GOVERSION)\" = " + shellQuote(manifest.Go.Version),
		},
		{
			"kernel release",
			"test \"$(uname -r)\" = " + shellQuote(manifest.VM.KernelRelease),
		},
		{"actions runner", "test -x /opt/actions-runner/config.sh"},
		{
			"actions runner job hooks",
			"test -x " + shellQuote(runnerJobStartedHookPath) + " && " +
				"test -x " + shellQuote(runnerJobCompletedHookPath) + " && " +
				"systemctl cat robotgo-runner.service | grep -Fx " +
				shellQuote(
					"Environment=ACTIONS_RUNNER_HOOK_JOB_STARTED="+
						runnerJobStartedHookPath,
				) + " >/dev/null && " +
				"systemctl cat robotgo-runner.service | grep -Fx " +
				shellQuote(
					"Environment=ACTIONS_RUNNER_HOOK_JOB_COMPLETED="+
						runnerJobCompletedHookPath,
				) + " >/dev/null",
		},
		{
			"actions runner registration flags",
			"runuser -u robotgo -- /opt/actions-runner/config.sh --help " +
				"| grep -F -- '--ephemeral' >/dev/null && " +
				"runuser -u robotgo -- /opt/actions-runner/config.sh --help " +
				"| grep -F -- '--disableupdate' >/dev/null && " +
				"runuser -u robotgo -- /opt/actions-runner/config.sh --help " +
				"| grep -F -- '--no-default-labels' >/dev/null",
		},
		{"GDM", "systemctl is-active --quiet gdm3"},
		{
			"GNOME portal session readiness",
			sessionCommand +
				"timeout 130 /usr/local/libexec/robotgo-runner-wait-session",
		},
		{
			"PipeWire",
			sessionCommand + "systemctl --user is-active --quiet pipewire.service",
		},
		{
			"WirePlumber",
			sessionCommand + "systemctl --user is-active --quiet wireplumber.service",
		},
		{
			"RemoteDesktop portal",
			sessionCommand +
				"busctl --address=unix:path=/run/user/1100/bus " +
				"introspect org.freedesktop.portal.Desktop " +
				"/org/freedesktop/portal/desktop " +
				"org.freedesktop.portal.RemoteDesktop >/dev/null",
		},
		{
			"ScreenCast portal",
			sessionCommand +
				"busctl --address=unix:path=/run/user/1100/bus " +
				"introspect org.freedesktop.portal.Desktop " +
				"/org/freedesktop/portal/desktop " +
				"org.freedesktop.portal.ScreenCast >/dev/null",
		},
		{
			"operator attestation contract",
			"install -d -m 0700 -o root -g root /run/robotgo-operator && " +
				"printf '%s\\n' " +
				shellQuote(
					"ready commit="+strings.Repeat("1", 40)+
						" run=1 attempt=1 lane=gnome cell=remote-desktop",
				) +
				" > /run/robotgo-operator/console-ready && " +
				"chmod 0600 /run/robotgo-operator/console-ready && " +
				"/usr/local/sbin/robotgo-runner-job-started " +
				strings.Repeat("1", 40) + " 1 1 " +
				shellQuote("RemoteDesktop E2E") + " && " +
				"test \"$(stat -c '%u:%a:%F' " +
				"/run/robotgo-evidence/operator-ready)\" = " +
				shellQuote("0:444:regular file") + " && " +
				"runuser -u robotgo -- grep -Fx " +
				shellQuote(
					"ready commit="+strings.Repeat("1", 40)+
						" run=1 attempt=1 lane=gnome cell=remote-desktop",
				) +
				" /run/robotgo-evidence/operator-ready >/dev/null && " +
				"/usr/local/sbin/robotgo-runner-job-completed && " +
				"test ! -e /run/robotgo-evidence && " +
				"test ! -e /run/robotgo-operator",
		},
		{
			"operator attestation mismatch rejection",
			"install -d -m 0700 -o root -g root /run/robotgo-operator && " +
				"printf '%s\\n' wrong " +
				"> /run/robotgo-operator/console-ready && " +
				"chmod 0600 /run/robotgo-operator/console-ready && " +
				"! /usr/local/sbin/robotgo-runner-job-started " +
				strings.Repeat("1", 40) + " 1 1 " +
				shellQuote("RemoteDesktop E2E") + " && " +
				"/usr/local/sbin/robotgo-runner-job-completed",
		},
		{
			"egress boundary",
			"/usr/local/sbin/robotgo-runner-configure-egress",
		},
		{
			"allowlisted CONNECT",
			"HTTPS_PROXY=" + shellQuote(proxyURL) +
				" HTTP_PROXY=" + shellQuote(proxyURL) +
				" curl --connect-timeout 10 --fail --silent --show-error " +
				"--head https://api.github.com/ >/dev/null",
		},
		{
			"direct egress denial",
			"! env -u HTTPS_PROXY -u HTTP_PROXY -u https_proxy -u http_proxy " +
				"curl --connect-timeout 3 --fail --silent --show-error " +
				"--head https://api.github.com/ >/dev/null 2>&1",
		},
	}
	for _, check := range checks {
		if err := options.Commands.Run(
			probeContext,
			"ssh",
			append(
				append([]string{}, sshArguments...),
				"root@127.0.0.1",
				"set -euo pipefail; "+check.command,
			),
			nil,
			logWriter,
		); err != nil {
			if check.name == "GNOME portal session readiness" {
				collectGNOMEDiagnostics(
					probeContext,
					options.Commands,
					sshArguments,
					logWriter,
				)
			}
			return fmt.Errorf("protected GNOME guest probe %q failed: %w", check.name, err)
		}
		if err := writeStatus(
			options.Output,
			"probe check ready name=%q\n",
			check.name,
		); err != nil {
			return err
		}
	}
	probeCommand := "echo ROBOTGO_GNOME_PROBE_READY && systemctl poweroff --no-wall"
	var probeOutput strings.Builder
	combinedOutput := io.MultiWriter(logWriter, &probeOutput)
	if err := options.Commands.Run(
		probeContext,
		"ssh",
		append(sshArguments, "root@127.0.0.1", probeCommand),
		nil,
		combinedOutput,
	); err != nil && !strings.Contains(probeOutput.String(), "ROBOTGO_GNOME_PROBE_READY") {
		return err
	}
	if !strings.Contains(probeOutput.String(), "ROBOTGO_GNOME_PROBE_READY") {
		return errors.New("protected GNOME guest did not complete its local probe")
	}
	if err := waitForProcessExit(probeContext, qemuPID, qemuStopTimeout); err != nil {
		return err
	}
	stopped = true
	return writeStatus(options.Output, "probe ready id=%s\n", imageID[:16])
}

func collectGNOMEDiagnostics(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	output io.Writer,
) {
	const diagnosticCommand = `set +e
echo ROBOTGO_GNOME_DIAGNOSTICS
systemctl status gdm3 --no-pager --full
loginctl list-sessions --no-legend
loginctl user-status robotgo --no-pager
test -d /run/user/1100 && find /run/user/1100 -maxdepth 1 -type s -printf '%f\n'
journalctl --boot --unit=gdm3 --no-pager --lines=120
journalctl --boot _UID=1100 --no-pager --lines=120
echo ROBOTGO_GNOME_DIAGNOSTICS_END`
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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
