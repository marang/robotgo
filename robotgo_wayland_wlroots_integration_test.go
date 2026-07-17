//go:build cgo && linux && wayland && integration
// +build cgo,linux,wayland,integration

package robotgo

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

const envWlrootsMinMaxE2E = "ROBOTGO_WLROOTS_MINMAX_E2E"
const envSwayTitleE2E = "ROBOTGO_SWAY_TITLE_E2E"
const envHyprTitleE2E = "ROBOTGO_HYPRLAND_TITLE_E2E"
const envHyprMaximizeE2E = "ROBOTGO_HYPRLAND_MAXIMIZE_E2E"

func TestWlrootsGenericRuntimeCapabilityIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Skip("requires Wayland session")
	}

	b := resolveWindowBackend()
	if b.Name() != windowBackendWlroots {
		t.Skipf("requires wlroots generic backend runtime, got %q", b.Name())
	}
	if !hasCommand(cmdWlrCtl) {
		t.Skip("requires wlrctl command in PATH")
	}

	caps := GetLinuxCapabilities()
	if caps.DisplayServer != DisplayServerWayland {
		t.Fatalf("expected wayland display server, got %q", caps.DisplayServer)
	}
	if caps.Window.Backend != windowBackendWlroots {
		t.Fatalf("expected window backend %q, got %q", windowBackendWlroots, caps.Window.Backend)
	}
	if !caps.Window.Available {
		t.Fatalf("expected window capability available for wlroots generic runtime")
	}
}

func TestWlrootsGenericMinMaxE2EOptIn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if os.Getenv(envWlrootsMinMaxE2E) == "" {
		t.Skipf("set %s=1 to run wlroots min/max e2e integration", envWlrootsMinMaxE2E)
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Skip("requires Wayland session")
	}

	b := resolveWindowBackend()
	if b.Name() != windowBackendWlroots {
		t.Skipf("requires wlroots generic backend runtime, got %q", b.Name())
	}
	if !hasCommand(cmdWlrCtl) {
		t.Skip("requires wlrctl command in PATH")
	}

	if err := MinWindowE(0); err != nil {
		t.Fatalf("MinWindowE(0) failed in wlroots e2e mode: %v", err)
	}
	if err := MaxWindowE(0); err != nil {
		t.Fatalf("MaxWindowE(0) failed in wlroots e2e mode: %v", err)
	}
}

func TestSwayRuntimeCapabilityIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Skip("requires Wayland session")
	}

	b := resolveWindowBackend()
	if b.Name() != windowBackendSway {
		t.Skipf("requires sway backend runtime, got %q", b.Name())
	}
	if !hasCommand(cmdSwayMsg) {
		t.Skip("requires swaymsg command in PATH")
	}

	caps := GetLinuxCapabilities()
	if caps.Window.Backend != windowBackendSway {
		t.Fatalf("expected window backend %q, got %q", windowBackendSway, caps.Window.Backend)
	}
	if !caps.Window.Available {
		t.Fatalf("expected sway window capability available when swaymsg is present")
	}
}

func TestHyprlandRuntimeCapabilityIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Skip("requires Wayland session")
	}

	b := resolveWindowBackend()
	if b.Name() != windowBackendHypr {
		t.Skipf("requires hyprland backend runtime, got %q", b.Name())
	}
	if !hasCommand(cmdHyprCtl) {
		t.Skip("requires hyprctl command in PATH")
	}

	caps := GetLinuxCapabilities()
	if caps.Window.Backend != windowBackendHypr {
		t.Fatalf("expected window backend %q, got %q", windowBackendHypr, caps.Window.Backend)
	}
	if !caps.Window.Available {
		t.Fatalf("expected hyprland window capability available when hyprctl is present")
	}
}

func TestSwayTitleE2EOptIn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if os.Getenv(envSwayTitleE2E) == "" {
		t.Skipf("set %s=1 to run sway title e2e integration", envSwayTitleE2E)
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Skip("requires Wayland session")
	}

	b := resolveWindowBackend()
	if b.Name() != windowBackendSway {
		t.Skipf("requires sway backend runtime, got %q", b.Name())
	}
	if !hasCommand(cmdSwayMsg) {
		t.Skip("requires swaymsg command in PATH")
	}

	title, err := GetTitleE()
	if err != nil {
		t.Fatalf("GetTitleE failed in sway e2e mode: %v", err)
	}
	if title == "" {
		t.Fatalf("GetTitleE returned empty title in sway e2e mode")
	}
}

func TestHyprlandTitleE2EOptIn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if os.Getenv(envHyprTitleE2E) == "" {
		t.Skipf("set %s=1 to run hyprland title e2e integration", envHyprTitleE2E)
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Skip("requires Wayland session")
	}

	b := resolveWindowBackend()
	if b.Name() != windowBackendHypr {
		t.Skipf("requires hyprland backend runtime, got %q", b.Name())
	}
	if !hasCommand(cmdHyprCtl) {
		t.Skip("requires hyprctl command in PATH")
	}

	title, err := GetTitleE()
	if err != nil {
		t.Fatalf("GetTitleE failed in hyprland e2e mode: %v", err)
	}
	if title == "" {
		t.Fatalf("GetTitleE returned empty title in hyprland e2e mode")
	}
}

func TestHyprlandMaximizeE2EOptIn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if os.Getenv(envHyprMaximizeE2E) == "" {
		t.Skipf("set %s=1 to run hyprland maximize e2e integration", envHyprMaximizeE2E)
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Skip("requires Wayland session")
	}

	b := resolveWindowBackend()
	if b.Name() != windowBackendHypr {
		t.Skipf("requires hyprland backend runtime, got %q", b.Name())
	}
	if !hasCommand(cmdHyprCtl) {
		t.Skip("requires hyprctl command in PATH")
	}

	info, err := getHyprlandActiveWindow()
	if err != nil {
		t.Fatalf("query initial active-window state: %v", err)
	}
	if info.Fullscreen == nil || info.FullscreenClient == nil {
		t.Fatal("hyprctl activewindow response omitted internal/client fullscreen state")
	}
	if *info.Fullscreen != hyprlandFullscreenNone &&
		*info.Fullscreen != hyprlandFullscreenMaximized {
		t.Skipf(
			"active window is in fullscreen mode %d; refusing to alter a non-normal/non-maximized state",
			*info.Fullscreen,
		)
	}
	if *info.FullscreenClient != *info.Fullscreen {
		t.Skipf(
			"active window has decoupled internal/client state %d/%d; refusing to alter it",
			*info.Fullscreen,
			*info.FullscreenClient,
		)
	}

	initial := *info.Fullscreen == hyprlandFullscreenMaximized
	waitForState := func(want bool) error {
		deadline := time.Now().Add(windowCommandTimeout)
		for {
			got, err := IsMaximizedE()
			if err == nil && got == want {
				return nil
			}
			if time.Now().After(deadline) {
				if err == nil {
					return fmt.Errorf(
						"IsMaximizedE() did not become %v before timeout; got %v",
						want,
						got,
					)
				}
				return fmt.Errorf(
					"IsMaximizedE() did not become %v before timeout; got %v, %w",
					want,
					got,
					err,
				)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	t.Cleanup(func() {
		if err := MaxWindowE(0, initial); err != nil {
			t.Errorf("restore initial maximized state: %v", err)
			return
		}
		if err := waitForState(initial); err != nil {
			t.Errorf("verify restored maximized state: %v", err)
		}
	})

	target := !initial
	if err := MaxWindowE(0, target); err != nil {
		t.Fatalf("MaxWindowE(0, %v): %v", target, err)
	}

	if err := waitForState(target); err != nil {
		t.Fatal(err)
	}
}
