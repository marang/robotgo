//go:build linux

package portalrunner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildFailureLogTailIsBounded(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "build.log")
	prefix := bytes.Repeat([]byte("p"), 1024)
	tail := bytes.Repeat([]byte("t"), buildLogTailLimit)
	if err := os.WriteFile(path, append(prefix, tail...), 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := writeBuildLogTail(&output, path); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, string(prefix)) {
		t.Fatal("failure diagnostic exposed data before the bounded tail")
	}
	if !strings.Contains(got, string(tail)) {
		t.Fatal("failure diagnostic omitted the bounded log tail")
	}
	if !strings.Contains(
		got,
		"portal runner build failure log (last 65536 bytes)",
	) || !strings.HasSuffix(got, "end portal runner build failure log\n") {
		t.Fatalf("failure diagnostic delimiters missing:\n%s", got)
	}
}

func TestGuestInstallTimeoutPrecedesHostedWorkflowGuard(t *testing.T) {
	t.Parallel()

	if guestInstallTimeout != 20*time.Minute {
		t.Fatalf("guestInstallTimeout = %s, want 20m", guestInstallTimeout)
	}
	if guestInstallTimeout >= 30*time.Minute {
		t.Fatal("guest install timeout must precede the hosted workflow guard")
	}
}

func TestComputeImageIdentityBindsEveryBuildInput(t *testing.T) {
	t.Parallel()

	repositoryRoot := t.TempDir()
	guestRoot := filepath.Join(repositoryRoot, "infrastructure", "gnome")
	writeImageIdentityFixture(t, repositoryRoot, guestRoot)
	manifestPath := filepath.Join(guestRoot, "manifest.json")

	firstID, firstMetadata, _, err := computeImageIdentity(
		manifestPath,
		repositoryRoot,
		guestRoot,
		portalLaneGNOME,
	)
	if err != nil {
		t.Fatalf("compute initial image identity: %v", err)
	}
	changedPath := filepath.Join(guestRoot, commonGuestImageFiles[0])
	if err := os.WriteFile(changedPath, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("change guest input: %v", err)
	}
	secondID, secondMetadata, _, err := computeImageIdentity(
		manifestPath,
		repositoryRoot,
		guestRoot,
		portalLaneGNOME,
	)
	if err != nil {
		t.Fatalf("compute changed image identity: %v", err)
	}
	if firstID == secondID {
		t.Fatal("guest input change did not change image identity")
	}
	if string(firstMetadata) == string(secondMetadata) {
		t.Fatal("guest input change did not change build metadata")
	}

	builderPath := filepath.Join(repositoryRoot, "internal", "portalrunner", "image_linux.go")
	if err := os.WriteFile(builderPath, []byte("changed builder\n"), 0o644); err != nil {
		t.Fatalf("change builder input: %v", err)
	}
	thirdID, _, _, err := computeImageIdentity(
		manifestPath,
		repositoryRoot,
		guestRoot,
		portalLaneGNOME,
	)
	if err != nil {
		t.Fatalf("compute builder-changed image identity: %v", err)
	}
	if secondID == thirdID {
		t.Fatal("builder input change did not change image identity")
	}
}

func TestComputeImageIdentityBindsExecutableMode(t *testing.T) {
	t.Parallel()

	repositoryRoot := t.TempDir()
	guestRoot := filepath.Join(repositoryRoot, "infrastructure", "gnome")
	writeImageIdentityFixture(t, repositoryRoot, guestRoot)
	manifestPath := filepath.Join(guestRoot, "manifest.json")

	firstID, _, _, err := computeImageIdentity(
		manifestPath,
		repositoryRoot,
		guestRoot,
		portalLaneGNOME,
	)
	if err != nil {
		t.Fatalf("compute initial image identity: %v", err)
	}
	changedPath := filepath.Join(guestRoot, commonGuestImageFiles[0])
	if err := os.Chmod(changedPath, 0o700); err != nil {
		t.Fatalf("change guest input mode: %v", err)
	}
	secondID, _, _, err := computeImageIdentity(
		manifestPath,
		repositoryRoot,
		guestRoot,
		portalLaneGNOME,
	)
	if err != nil {
		t.Fatalf("compute mode-changed image identity: %v", err)
	}
	if firstID == secondID {
		t.Fatal("guest input mode change did not change image identity")
	}
}

func TestRemoveStaleImagesKeepsOnlyCurrentAttestedPair(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	currentDigest := strings.Repeat("a", 64)
	staleDigest := strings.Repeat("b", 64)
	currentImage := filepath.Join(directory, "gnome-"+currentDigest+".qcow2")
	currentMetadata := currentImage + ".build.json"
	staleImage := filepath.Join(directory, "gnome-"+staleDigest+".qcow2")
	staleMetadata := staleImage + ".build.json"
	kdeImage := filepath.Join(directory, "kde-"+staleDigest+".qcow2")
	kdeMetadata := kdeImage + ".build.json"
	baseImage := filepath.Join(directory, "ubuntu-base.qcow2")
	for _, path := range []string{
		currentImage,
		currentMetadata,
		staleImage,
		staleMetadata,
		kdeImage,
		kdeMetadata,
		baseImage,
	} {
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeStaleImages(
		directory,
		portalLaneGNOME,
		currentImage,
		currentMetadata,
	); err != nil {
		t.Fatalf("removeStaleImages: %v", err)
	}
	for _, path := range []string{
		currentImage,
		currentMetadata,
		kdeImage,
		kdeMetadata,
		baseImage,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected retained image %q: %v", path, err)
		}
	}
	for _, path := range []string{staleImage, staleMetadata} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale image %q still exists: %v", path, err)
		}
	}
}

func TestRepositoryGuestFilesAreCompleteAndExecutable(t *testing.T) {
	t.Parallel()
	for _, lane := range []string{portalLaneGNOME, portalLaneKDE} {
		path, err := filepath.Abs(
			filepath.Join(
				"..",
				"..",
				"infrastructure",
				"portal-runner",
				lane,
			),
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateGuestFiles(path, lane); err != nil {
			t.Fatalf("validate %s guest files: %v", lane, err)
		}
		files, err := guestImageFilesForLane(lane)
		if err != nil {
			t.Fatal(err)
		}
		for _, relative := range files {
			info, err := os.Stat(filepath.Join(path, relative))
			if err != nil {
				t.Fatal(err)
			}
			wantMode := os.FileMode(0o755)
			if filepath.Ext(relative) == ".js" {
				wantMode = 0o644
			}
			if info.Mode().Perm() != wantMode {
				t.Errorf(
					"%s %s mode = %o, want %o",
					lane,
					relative,
					info.Mode().Perm(),
					wantMode,
				)
			}
		}
	}
}

func TestValidateGuestFilesRejectsUnboundInput(t *testing.T) {
	t.Parallel()

	repositoryRoot := t.TempDir()
	guestRoot := filepath.Join(repositoryRoot, "infrastructure", "gnome")
	writeImageIdentityFixture(t, repositoryRoot, guestRoot)
	if err := os.WriteFile(
		filepath.Join(guestRoot, "guest", "unexpected.sh"),
		[]byte("#!/bin/sh\n"),
		0o755,
	); err != nil {
		t.Fatalf("write unexpected guest input: %v", err)
	}
	if err := validateGuestFiles(guestRoot, portalLaneGNOME); err == nil ||
		!strings.Contains(err.Error(), "not identity-bound") {
		t.Fatalf("validateGuestFiles() error = %v, want identity-bound rejection", err)
	}
}

func TestBuildCloudConfigSuppressesCommentsAndFingerprints(t *testing.T) {
	t.Parallel()

	config, err := buildCloudConfig("ssh-ed25519 AAAATEST developer@example.invalid")
	if err != nil {
		t.Fatalf("build cloud config: %v", err)
	}
	for _, forbidden := range []string{
		"developer@example.invalid",
		"AAAATEST developer",
	} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("cloud config retained forbidden key metadata %q", forbidden)
		}
	}
	for _, required := range []string{
		"no_ssh_fingerprints: true",
		"emit_keys_to_console: false",
		"ssh_quiet_keygen: true",
		"ssh_publish_hostkeys:",
		"enabled: false",
		"ssh-ed25519 AAAATEST",
	} {
		if !strings.Contains(config, required) {
			t.Fatalf("cloud config omitted %q", required)
		}
	}
}

func TestRuntimeNetworkConfigUsesCloudImageRenderer(t *testing.T) {
	t.Parallel()

	config := runtimeNetworkConfig()
	if !strings.Contains(config, "renderer: networkd") {
		t.Fatal("runtime network config does not use systemd-networkd")
	}
	if strings.Contains(config, "NetworkManager") {
		t.Fatal("runtime network config unexpectedly selects NetworkManager")
	}
	if !strings.Contains(config, "dhcp4: true") {
		t.Fatal("runtime network config does not request IPv4 DHCP")
	}
}

func TestBuildQEMUArgumentsExposeOnlyLoopbackSSH(t *testing.T) {
	t.Parallel()

	arguments := buildQEMUArguments(
		Manifest{
			Lane: "gnome",
			VM: VMConfig{
				CPUs:      4,
				MemoryMiB: 8192,
			},
		},
		"/private/disk.qcow2",
		"/private/seed.img",
		"/private/qemu.pid",
		"/private/serial.log",
		22222,
		true,
	)
	joined := strings.Join(arguments, " ")
	if !strings.Contains(joined, "hostfwd=tcp:127.0.0.1:22222-:22") {
		t.Fatal("QEMU does not bind SSH forwarding exclusively to loopback")
	}
	if !strings.Contains(joined, "-device bochs-display -display none") {
		t.Fatal("headless QEMU does not expose a DRM-capable display device")
	}
	for _, forbidden := range []string{"-virtfs", "hostfwd=tcp:0.0.0.0", "-snapshot"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("QEMU arguments contain forbidden host exposure %q", forbidden)
		}
	}
}

func TestVisibleQEMUProvidesPrivateOperatorInput(t *testing.T) {
	t.Parallel()

	arguments := buildQEMUArguments(
		Manifest{
			Lane: "gnome",
			VM: VMConfig{
				CPUs:      4,
				MemoryMiB: 8192,
			},
		},
		"/private/disk.qcow2",
		"/private/seed.img",
		"/private/qemu.pid",
		"/private/serial.log",
		22222,
		false,
	)
	joined := strings.Join(arguments, " ")
	for _, required := range []string{
		"-device virtio-vga",
		"-device qemu-xhci",
		"-device usb-kbd",
		"-device usb-tablet",
		"-display gtk,gl=off,show-cursor=on,grab-on-hover=on",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("visible QEMU omits operator device %q", required)
		}
	}
	if strings.Contains(joined, "-daemonize") {
		t.Fatal("visible QEMU must remain supervised by the host process")
	}
}

func TestRepositoryGuestSessionContract(t *testing.T) {
	t.Parallel()

	guestRoot := filepath.Join(
		"..",
		"..",
		"infrastructure",
		"portal-runner",
		"gnome",
		"guest",
	)
	waitScript, err := os.ReadFile(filepath.Join(guestRoot, "wait-session.sh"))
	if err != nil {
		t.Fatalf("read wait-session.sh: %v", err)
	}
	if strings.Contains(string(waitScript), "busctl --user --address=") {
		t.Fatal("wait-session.sh combines mutually exclusive busctl connections")
	}
	if !strings.Contains(
		string(waitScript),
		"busctl --address=unix:path=/run/user/1100/bus",
	) {
		t.Fatal("wait-session.sh does not use the protected user's explicit bus")
	}
	for _, required := range []string{
		"--no-pager call",
		"org.freedesktop.portal.Desktop",
		"org.freedesktop.DBus.Peer",
		"Ping",
	} {
		if !strings.Contains(string(waitScript), required) {
			t.Fatalf(
				"wait-session.sh omits portal activation contract %q",
				required,
			)
		}
	}
	if strings.Contains(string(waitScript), "--no-legend status") {
		t.Fatal("wait-session.sh passively inspects an on-demand portal")
	}

	installScript, err := os.ReadFile(filepath.Join(guestRoot, "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	for _, required := range []string{
		"dpkg-query -W -f='${db:Status-Status}' ubuntu-session",
		"test -f /usr/share/wayland-sessions/ubuntu.desktop",
		"test -f /usr/share/wayland-sessions/ubuntu-wayland.desktop",
		"test -f /usr/share/gnome-shell/modes/ubuntu.json",
		"test -x /usr/bin/gnome-shell",
		"/var/lib/AccountsService/users/robotgo",
		"XSession=ubuntu",
		"chmod 0600 /var/lib/AccountsService/users/robotgo",
		"/usr/local/libexec/robotgo-runner-wait-portal-dialog",
		"chmod 0644 /etc/dconf/profile/user",
		"chmod 0644 /etc/dconf/db/robotgo.d/00-runner",
		"chmod 0644 /etc/dconf/db/robotgo",
		"[org/gnome/desktop/input-sources]",
		"sources=[('xkb', 'us')]",
		"mru-sources=[('xkb', 'us')]",
		"ACTIONS_RUNNER_HOOK_JOB_STARTED=/usr/local/libexec/robotgo-runner-job-started-hook.sh",
		"ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/usr/local/libexec/robotgo-runner-job-completed-hook.sh",
		"systemctl enable robotgo-runner-egress.service",
	} {
		if !strings.Contains(string(installScript), required) {
			t.Fatalf("install.sh omits GNOME guest contract %q", required)
		}
	}
	if strings.Contains(string(installScript), "DefaultSession=") {
		t.Fatal("install.sh uses unsupported GDM default-session configuration")
	}

	dialogWait, err := os.ReadFile(
		filepath.Join(guestRoot, "wait-portal-dialog.sh"),
	)
	if err != nil {
		t.Fatal(err)
	}
	dialogScript := string(dialogWait)
	for _, required := range []string{
		"org.freedesktop.impl.portal.desktop.gnome",
		"/org/freedesktop/portal/desktop/request/",
		"remote-desktop | screencast",
		"CreateSession",
		"every Select* request has completed",
		"exact random Start request path",
		"only this exact path can satisfy readiness",
		"error dialog-unavailable",
	} {
		if !strings.Contains(dialogScript, required) {
			t.Errorf("GNOME dialog readiness omits %q", required)
		}
	}
	for _, forbidden := range []string{
		"journalctl",
		"screenshot",
		"title",
		`printf '%s' "$objects"`,
	} {
		if strings.Contains(dialogScript, forbidden) {
			t.Errorf("GNOME dialog readiness contains %q", forbidden)
		}
	}

	for _, forbidden := range []string{
		"ACTIONS_RUNNER_HOOK_JOB_STARTED=/usr/local/libexec/robotgo-runner-job-started-hook\n",
		"ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/usr/local/libexec/robotgo-runner-job-completed-hook\n",
	} {
		if strings.Contains(string(installScript), forbidden) {
			t.Fatalf("install.sh configures unsupported extensionless runner hook %q", forbidden)
		}
	}

	egressScript, err := os.ReadFile(filepath.Join(guestRoot, "configure-egress.sh"))
	if err != nil {
		t.Fatalf("read configure-egress.sh: %v", err)
	}
	if !strings.Contains(
		string(egressScript),
		"ip daddr 10.0.2.2 tcp sport 22 accept",
	) {
		t.Fatal("configure-egress.sh would sever the protected host control channel")
	}

	registerScript, err := os.ReadFile(filepath.Join(guestRoot, "register.sh"))
	if err != nil {
		t.Fatalf("read register.sh: %v", err)
	}
	for _, required := range []string{
		"--no-default-labels",
		"--ephemeral",
		"--disableupdate",
		"systemctl start --no-block robotgo-runner.service",
		`printf 'ready commit=%s run=%s attempt=%s lane=gnome cell=%s\n'`,
	} {
		if !strings.Contains(string(registerScript), required) {
			t.Fatalf("register.sh omits protected runner contract %q", required)
		}
	}

	jobStartedScript, err := os.ReadFile(filepath.Join(guestRoot, "job-started.sh"))
	if err != nil {
		t.Fatalf("read job-started.sh: %v", err)
	}
	for _, required := range []string{
		`expected="ready commit=$commit run=$run_id attempt=$run_attempt lane=gnome cell=$cell"`,
		`test "$(cat "$console_ready")" = "$expected"`,
		"install -d -m 0755 -o root -g root",
		`chmod 0444 "$temporary"`,
	} {
		if !strings.Contains(string(jobStartedScript), required) {
			t.Fatalf("job-started.sh omits exact attestation contract %q", required)
		}
	}
}

func TestRepositoryKDEGuestActivatesPortalFrontendAndBackend(t *testing.T) {
	t.Parallel()
	guestRoot := filepath.Join(
		"..",
		"..",
		"infrastructure",
		"portal-runner",
		"kde",
		"guest",
	)
	waitScript, err := os.ReadFile(filepath.Join(guestRoot, "wait-session.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(waitScript)
	for _, required := range []string{
		"busctl --address=unix:path=/run/user/1100/bus",
		"--no-pager --timeout=2s call",
		"org.freedesktop.portal.Desktop",
		"org.freedesktop.impl.portal.desktop.kde",
		"org.freedesktop.DBus.Peer",
		"Ping",
		"deadline=$((SECONDS + 60))",
		"deadline=$((SECONDS + 90))",
		"deadline=$((SECONDS + 30))",
		"deadline=$((SECONDS + 10))",
		"stable >= 3",
		"require_base_ready",
		"fail_shell_stage",
		"recover_shell_once",
		"report_shell_recovery",
		"session_state_root=/run/robotgo-session-state",
		"$session_state_root/shell-recovery-attempted",
		"$session_state_root/shell-recovery-complete",
		"$session_state_root/shell-recovery-failed",
		"$session_state_root/shell-reset-failed",
		"$session_state_root/shell-queue-failed",
		"$session_state_root/shell-start-failed",
		"$session_state_root/shell-start-result-",
		"$session_state_root/recovery-display-manager",
		"$session_state_root/recovery-runtime-directory",
		"$session_state_root/recovery-user-bus",
		"$session_state_root/recovery-wayland",
		"$session_state_root/recovery-compositor",
		"$session_state_root/recovery-session",
		"$session_state_root/session-ready",
		"$session_state_root/session-decision.lock",
		"mkdir -m 0700 \"$shell_recovery_marker\"",
		"flock --exclusive --wait 15 9",
		"claim_session_ready",
		"if ! all_ready; then\n" +
			"    release_session_decision\n" +
			"    return 2",
		"if [[ -d \"$session_ready_marker\" ]]; then\n" +
			"    release_session_decision\n" +
			"    return 0",
		"timeout --kill-after=1s 2s systemctl --user is-failed",
		"local deadline=$((SECONDS + 60))",
		"timeout --kill-after=1s 2s systemctl --user reset-failed",
		"wait_for_shell_recovery() {\n" +
			"  local stage\n" +
			"  # The recovery winner may spend up to six additional seconds",
		"  while ((SECONDS < deadline)); do\n" +
			"    require_recovery_base_ready",
		"wait_for_restarted_shell() {\n" +
			"  local deadline=$((SECONDS + 30))\n" +
			"  while ((SECONDS < deadline)); do\n" +
			"    require_recovery_base_ready",
		"if ((claim_status != 0)); then",
		"continue 2",
		"timeout --kill-after=1s 2s systemctl --user --no-block restart",
		"--property=Result --value plasma-plasmashell.service",
		"--property=ExecMainStatus --value",
		"valid_shell_status",
		"shared_shell_start_failure_stage",
		"desktop-shell-start-exit-%d",
		"desktop-shell-start-signal-%d",
		"desktop-shell-start-core-%d",
		"desktop-shell-start-timeout",
		"desktop-shell-start-limit",
		"desktop-shell-start-protocol",
		"desktop-shell-start-watchdog",
		"desktop-shell-start-oom",
		"desktop-shell-start-resources",
		"ROBOTGO_SESSION_RECOVERY=desktop-shell",
		"desktop-shell-never-seen",
		"desktop-shell-failed",
		"desktop-shell-process-missing",
		"desktop-shell-recovery-exhausted",
		"desktop-shell-recovery-failed",
		"desktop-shell-reset-failed",
		"desktop-shell-queue-failed",
		"desktop-shell-start-failed",
		"desktop-shell-unstable",
		"portal-backend-unstable",
		"fail_stage session-unstable",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("KDE session readiness omits %q", required)
		}
	}
	if strings.Contains(script, "--no-legend status") {
		t.Fatal("KDE session readiness passively inspects an on-demand portal")
	}
	installScript, err := os.ReadFile(filepath.Join(guestRoot, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"dpkg-query -W -f='${db:Status-Status}' plasma-workspace",
		"dpkg-query -W -f='${db:Status-Status}' plasma-desktop-data",
		"/usr/share/plasma/shells/org.kde.plasma.desktop/metadata.json",
		"test -f /usr/lib/systemd/user/plasma-plasmashell.service",
		"test -x /usr/bin/flock",
		"test -x /usr/bin/plasmashell",
		"d /run/robotgo-session-state 0700 robotgo robotgo -",
		"systemd-tmpfiles --create " +
			"/etc/tmpfiles.d/robotgo-session-state.conf",
		"TimeoutStartSec=400",
		"/usr/local/libexec/robotgo-runner-locate-screencast",
		"/usr/local/libexec/robotgo-runner-report-screencast-geometry",
		"/usr/local/share/robotgo/report-screencast-geometry.js",
	} {
		if !strings.Contains(string(installScript), required) {
			t.Errorf("KDE geometry approval omits %q", required)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(filepath.Dir(guestRoot), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`"python3-dbus"`,
		`"python3-gi"`,
	} {
		if !strings.Contains(string(manifest), required) {
			t.Fatalf("KDE manifest omits geometry bridge package %s", required)
		}
	}

	locator, err := os.ReadFile(filepath.Join(guestRoot, "locate-screencast.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"org.kde.KWin",
		"org.kde.kwin.Scripting",
		"org.kde.kwin.Script run",
		"unloadScript",
		`rm -f -- "$output"`,
	} {
		if !strings.Contains(string(locator), required) {
			t.Errorf("KDE geometry locator omits %q", required)
		}
	}
	for _, forbidden := range []string{"screendump", "journalctl", "title"} {
		if strings.Contains(string(locator), forbidden) {
			t.Errorf("KDE geometry locator contains %q", forbidden)
		}
	}

	reporter, err := os.ReadFile(
		filepath.Join(guestRoot, "report-screencast-geometry.js"),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"workspace.virtualScreenSize",
		"workspace.activeClient",
		"workspace.cursorPos",
		"dialog.x",
		"dialog.y",
		"dialog.width",
		"dialog.height",
	} {
		if !strings.Contains(string(reporter), required) {
			t.Errorf("KDE KWin reporter omits %q", required)
		}
	}
	for _, forbidden := range []string{
		"caption",
		"resourceName",
		"title",
		"screenshot",
	} {
		if strings.Contains(string(reporter), forbidden) {
			t.Errorf("KDE KWin reporter contains %q", forbidden)
		}
	}
}

func TestRepositoryGuestProvisioningRetriesBoundedDownloads(t *testing.T) {
	t.Parallel()

	for _, desktop := range []string{"gnome", "kde"} {
		desktop := desktop
		t.Run(desktop, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(
				"..",
				"..",
				"infrastructure",
				"portal-runner",
				desktop,
				"guest",
				"install.sh",
			)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s guest installer: %v", desktop, err)
			}
			script := string(data)
			for _, required := range []string{
				"Acquire::Retries=5",
				"Acquire::Languages=none",
				"Acquire::IndexTargets::deb::DEP-11::DefaultEnabled=false",
				"Acquire::IndexTargets::deb::CNF::DefaultEnabled=false",
				"Acquire::http::Timeout=30",
				"Acquire::https::Timeout=30",
				"DPkg::Lock::Timeout=60",
				`apt-get "${apt_options[@]}" update`,
				`apt-get "${apt_options[@]}" install`,
				`--connect-timeout 15 --max-time "$maximum_time"`,
				`--retry 5 --retry-delay 2 --retry-max-time "$maximum_time"`,
				"--retry-all-errors",
				"--proto '=https' --tlsv1.2",
				"sha256sum --check --status",
				"mirrors.kernel.org/ubuntu/pool/main/l/linux/",
				"launchpadlibrarian.net/866579255/",
				"f445185d1664025f4ea95d24757baf398fd4a47b6caadcf1bd3b15a1205929f6",
				`"$kernel_modules_url" "$kernel_modules_sha256" "$kernel_archive" 180`,
				"'.packages[] | select(. != $package)'",
				`install -y --no-install-recommends "$kernel_archive"`,
				`trap 'rm -f -- "$kernel_archive"' EXIT`,
			} {
				if !strings.Contains(script, required) {
					t.Errorf(
						"%s guest installer omits bounded retry contract %q",
						desktop,
						required,
					)
				}
			}
		})
	}
}

func writeImageIdentityFixture(t *testing.T, repositoryRoot, guestRoot string) {
	t.Helper()

	if err := os.MkdirAll(
		filepath.Join(repositoryRoot, "internal", "portalrunner"),
		0o700,
	); err != nil {
		t.Fatalf("create builder fixture directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(guestRoot, "guest"), 0o700); err != nil {
		t.Fatalf("create guest fixture directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(repositoryRoot, "internal", "portalrunner", "image_linux.go"),
		[]byte("builder\n"),
		0o644,
	); err != nil {
		t.Fatalf("write builder fixture: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(guestRoot, "manifest.json"),
		[]byte("{\"schema_version\":\"1\"}\n"),
		0o644,
	); err != nil {
		t.Fatalf("write manifest fixture: %v", err)
	}
	guestImageFiles, err := guestImageFilesForLane(portalLaneGNOME)
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range guestImageFiles {
		if err := os.WriteFile(
			filepath.Join(guestRoot, relative),
			[]byte("#!/bin/sh\nexit 0\n"),
			0o755,
		); err != nil {
			t.Fatalf("write guest fixture %q: %v", relative, err)
		}
	}
}
