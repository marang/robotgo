package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestHyprlandWorkflowUsesOnlyVerifiedVKMSDevice(t *testing.T) {
	t.Parallel()
	workflowData, err := os.ReadFile("../.github/workflows/hyprland-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	containerData, err := os.ReadFile("run-hyprland-container.sh")
	if err != nil {
		t.Fatal(err)
	}
	runtimeData, err := os.ReadFile("run-hyprland-e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	failureStageData, err := os.ReadFile("hyprland-failure-stages.sh")
	if err != nil {
		t.Fatal(err)
	}
	contract := normalizeWorkflowText(workflowData) + "\n" +
		normalizeWorkflowText(containerData) + "\n" +
		normalizeWorkflowText(runtimeData) + "\n" +
		normalizeWorkflowText(failureStageData)
	for _, required := range []string{
		"name: Hyprland E2E",
		"  workflow_call:",
		"permissions:\n  contents: read",
		"runs-on: ubuntu-24.04",
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
		"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
		"persist-credentials: false",
		`"linux-modules-extra-$(uname -r)"`,
		"sudo modprobe vkms enable_default_device=1",
		"sudo udevadm settle",
		"/sys/module/vkms",
		"/sys/devices/faux/vkms/drm/card[0-9]*",
		"/sys/devices/platform/vkms.*/drm/card[0-9]*",
		`[[ "${card##*/}" =~ ^card[0-9]+$ ]]`,
		"vkms did not expose exactly one DRM card",
		`[ "${#vkms_cards[@]}" -ne 1 ]`,
		`[ -L "$vkms_device" ]`,
		`--device "$ROBOTGO_HYPRLAND_DRM_DEVICE:$ROBOTGO_HYPRLAND_DRM_DEVICE:rwm"`,
		"--network none",
		"--read-only",
		"--tmpfs /tmp:rw,nosuid,nodev,noexec,mode=1777,size=256m",
		"--tmpfs /run:rw,nosuid,nodev,noexec,mode=1777,size=16m",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--user \"$(id -u):$(id -g)\"",
		`--volume "$GITHUB_WORKSPACE:/workspace:ro"`,
		`-e /dev/input`,
		"ROBOTGO_HYPRLAND_DRM_DRIVER=vkms",
		"SEATD_VTBOUND='0'",
		"seatd_socket='/run/seatd.sock'",
		"ASAN_OPTIONS=detect_leaks=1:halt_on_error=1:strict_string_checks=1",
		"GIT_OPTIONAL_LOCKS=0",
		"/usr/bin/bash",
		"DBUS_SESSION_BUS_ADDRESS",
		"device-contract",
		"seat-manager",
		"go test -asan -count=1",
		"ROBOTGO_HYPRLAND_E2E_FAIL_AFTER_START=1",
		"hyprland-hyprland-window-failure-reason",
		"container-runtime",
		"trap 'exit 130' INT",
		"trap 'exit 143' TERM",
		"trap 'exit 129' HUP",
		"isolated Hyprland evidence failed at sanitized stage: unavailable",
		"induced failure retained an isolated Hyprland runtime",
		"evidence.json",
		"test.log",
		"summary.md",
		"compositorevidence cleanup",
		"-name 'robotgo-hyprland-runtime.*'",
		"-exec rm -rf -- {} +",
		"sudo modprobe -r vkms",
		"vkms survived Hyprland evidence cleanup",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("Hyprland evidence contract omits %q", required)
		}
	}
	for _, forbidden := range []string{
		"--privileged",
		"--network host",
		"/dev/dri:/dev/dri",
		"persist-credentials: true",
		"actions/checkout@v",
		"actions/setup-go@v",
		"actions/upload-artifact@v",
		"dbus-daemon",
		"export DBUS_SESSION_BUS_ADDRESS",
		"machine_id_file",
	} {
		if strings.Contains(contract, forbidden) {
			t.Errorf("Hyprland evidence contract contains unsafe token %q", forbidden)
		}
	}
	containerStop := strings.LastIndex(string(workflowData), "docker rm -f")
	evidenceCleanup := strings.LastIndex(
		string(workflowData),
		"go run ./internal/cmd/compositorevidence cleanup",
	)
	if containerStop < 0 || evidenceCleanup < 0 || containerStop > evidenceCleanup {
		t.Fatal("Hyprland fallback cleanup does not stop containers before deleting evidence")
	}
}

func TestHyprlandRunnerImageIsImmutableAndMinimal(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../infrastructure/hyprland-runner/Containerfile")
	if err != nil {
		t.Fatal(err)
	}
	containerfile := normalizeWorkflowText(data)
	for _, required := range []string{
		"FROM docker.io/library/archlinux@sha256:",
		"COPY infrastructure/hyprland-runner/mirrorlist",
		"hyprland",
		"seatd",
		"wev",
		"go mod download",
	} {
		if !strings.Contains(containerfile, required) {
			t.Errorf("Hyprland runner image omits %q", required)
		}
	}
	for _, forbidden := range []string{
		"FROM archlinux:latest",
		"\n        dbus \\",
		"\n        sway \\",
		"/usr/bin/dbus-daemon",
		"weston",
	} {
		if strings.Contains(containerfile, forbidden) {
			t.Errorf("Hyprland runner image contains %q", forbidden)
		}
	}
}
