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
