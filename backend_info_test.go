package robotgo

import (
	"runtime"
	"testing"
)

func TestGetRuntimeBackendInfo(t *testing.T) {
	info := GetRuntimeBackendInfo()
	if info.GOOS != runtime.GOOS || info.GOARCH != runtime.GOARCH {
		t.Fatalf("platform = %s/%s, want %s/%s", info.GOOS, info.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	if info.CGOEnabled != runtimeCGOEnabled {
		t.Fatalf("CGOEnabled = %v, want %v", info.CGOEnabled, runtimeCGOEnabled)
	}
	if info.BuildImplementation != runtimeBuildImplementation {
		t.Fatalf("BuildImplementation = %q, want %q", info.BuildImplementation, runtimeBuildImplementation)
	}
	if runtime.GOOS != "linux" && info.DisplayServer != DisplayServerUnknown {
		t.Fatalf("DisplayServer = %q on %s, want unknown", info.DisplayServer, runtime.GOOS)
	}
}

func TestGetRuntimeCapabilitiesIncludeBuildAndPortableHelpers(t *testing.T) {
	capabilities := GetRuntimeCapabilities()
	if runtime.GOOS == "linux" {
		// Live Wayland capability probes may initialize reusable native protocol
		// objects. Keep this cross-platform contract test isolated from tests that
		// intentionally replace the display environment.
		CloseWaylandInput()
	}
	if capabilities.Runtime != GetRuntimeBackendInfo() {
		t.Fatalf("runtime capabilities build info = %+v, want %+v", capabilities.Runtime, GetRuntimeBackendInfo())
	}
	if !capabilities.Process.Available || capabilities.Process.Backend == "" {
		t.Fatalf("process capability = %+v, want available backend", capabilities.Process)
	}
	if !capabilities.Clipboard.Available || capabilities.Clipboard.Backend == "" {
		t.Fatalf("clipboard capability = %+v, want available backend", capabilities.Clipboard)
	}
}

func TestGetRuntimeBackendInfoDetectsLinuxDisplayServerWithoutProbes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux display-server detection test")
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", ":99")
	if got := GetRuntimeBackendInfo().DisplayServer; got != DisplayServerWayland {
		t.Fatalf("DisplayServer = %q, want %q", got, DisplayServerWayland)
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	if got := GetRuntimeBackendInfo().DisplayServer; got != DisplayServerX11 {
		t.Fatalf("DisplayServer = %q, want %q", got, DisplayServerX11)
	}
}
