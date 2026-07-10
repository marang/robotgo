package robotgo

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func resetWaylandBoundsCacheForTest() {
	waylandBoundsMu.Lock()
	waylandBoundsCached = Rect{}
	waylandBoundsValid = false
	waylandBoundsProbed = false
	waylandBoundsMu.Unlock()
}

func writeWaylandInfoStub(t *testing.T, dir string) {
	t.Helper()
	stub := filepath.Join(dir, "wayland-info")
	content := `#!/bin/sh
cat <<'OUT'
interface: 'zxdg_output_manager_v1',                     version:  3, name:  8
	xdg_output_v1
		output: 58
		logical_x: 10, logical_y: 20
		logical_width: 800, logical_height: 600
interface: 'wl_output',                                  version:  4, name: 58
	x: 0, y: 0, scale: 1,
	mode:
		width: 800 px, height: 600 px, refresh: 60.000 Hz,
		flags: current
OUT
`
	if err := os.WriteFile(stub, []byte(content), 0o755); err != nil {
		t.Fatalf("write wayland-info stub: %v", err)
	}
}

func writeWaylandInfoInvalidStub(t *testing.T, dir string) {
	t.Helper()
	stub := filepath.Join(dir, "wayland-info")
	content := `#!/bin/sh
cat <<'OUT'
interface: 'wl_output', version: 4, name: 58
	x: 0, y: 0, scale: 1,
	mode:
		width: 0 px, height: 0 px, refresh: 60.000 Hz,
		flags: current
OUT
`
	if err := os.WriteFile(stub, []byte(content), 0o755); err != nil {
		t.Fatalf("write invalid wayland-info stub: %v", err)
	}
}

func TestParseWaylandInfoBounds(t *testing.T) {
	raw := `interface: 'zxdg_output_manager_v1',                     version:  3, name:  8
	xdg_output_v1
		output: 58
		logical_x: 10, logical_y: 20
		logical_width: 800, logical_height: 600
interface: 'wl_output',                                  version:  4, name: 58
	x: 0, y: 0, scale: 1,
	mode:
		width: 800 px, height: 600 px, refresh: 60.000 Hz,
		flags: current
`
	rect, ok := parseWaylandInfoBounds(raw)
	if !ok {
		t.Fatalf("expected parse success")
	}
	if rect.X != 10 || rect.Y != 20 || rect.W != 800 || rect.H != 600 {
		t.Fatalf("unexpected bounds: %+v", rect)
	}
}

func TestWaylandScreenFallbackWithoutX11(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	tmp := t.TempDir()
	writeWaylandInfoStub(t, tmp)

	oldPath := os.Getenv(envPath)
	t.Setenv(envPath, tmp+":"+oldPath)
	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envSwaySock, "")
	t.Setenv(envHyprlandSignature, "")

	resetWaylandBoundsCacheForTest()
	defer resetWaylandBoundsCacheForTest()

	w, h := GetScreenSize()
	if w != 800 || h != 600 {
		t.Fatalf("expected 800x600 from wayland-info fallback, got %dx%d", w, h)
	}

	rect := GetScreenRect()
	if rect.X != 10 || rect.Y != 20 || rect.W != 800 || rect.H != 600 {
		t.Fatalf("unexpected screen rect from fallback: %+v", rect)
	}
}

func TestGetLinuxCapabilitiesWaylandFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	tmp := t.TempDir()
	writeWaylandInfoStub(t, tmp)

	oldPath := os.Getenv(envPath)
	t.Setenv(envPath, tmp+":"+oldPath)
	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envSwaySock, "")
	t.Setenv(envHyprlandSignature, "")

	resetWaylandBoundsCacheForTest()
	defer resetWaylandBoundsCacheForTest()

	c := GetLinuxCapabilities()
	if c.DisplayServer != DisplayServerWayland {
		t.Fatalf("expected wayland display server, got %v", c.DisplayServer)
	}
	if !c.Capture.Available {
		t.Fatalf("expected capture available in wayland session")
	}
	if !c.Bounds.Available {
		t.Fatalf("expected bounds available in wayland session")
	}
	if c.Bounds.Backend != cmdWaylandInfo {
		t.Fatalf("expected bounds backend %q, got %q", cmdWaylandInfo, c.Bounds.Backend)
	}
	if c.Bounds.Backend == "" {
		t.Fatalf("expected bounds backend annotation")
	}
	if c.Compositor == "" {
		t.Fatalf("expected compositor annotation")
	}
	if c.Window.Available {
		t.Fatalf("expected window capability unavailable in wayland core path")
	}
	if c.Window.Backend == "" {
		t.Fatalf("expected window backend annotation")
	}
	if c.Hook.Available {
		t.Fatalf("expected hook capability unavailable in wayland core path")
	}
	if c.Hook.Reason == "" {
		t.Fatalf("expected hook unsupported reason")
	}
	if c.Events != c.Hook {
		t.Fatalf("expected event capability to mirror hook capability")
	}
}

func TestGetLinuxCapabilitiesWaylandInvalidFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	tmp := t.TempDir()
	writeWaylandInfoInvalidStub(t, tmp)

	oldPath := os.Getenv(envPath)
	t.Setenv(envPath, tmp+":"+oldPath)
	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envSwaySock, "")
	t.Setenv(envHyprlandSignature, "")

	resetWaylandBoundsCacheForTest()
	defer resetWaylandBoundsCacheForTest()

	c := GetLinuxCapabilities()
	if c.DisplayServer != DisplayServerWayland {
		t.Fatalf("expected wayland display server, got %v", c.DisplayServer)
	}
	if c.Bounds.Available {
		t.Fatalf("expected bounds unavailable when wayland-info returns invalid geometry")
	}
	if c.Bounds.Backend != cmdWaylandInfo {
		t.Fatalf("expected bounds backend %q, got %q", cmdWaylandInfo, c.Bounds.Backend)
	}
	if c.Bounds.Fallback {
		t.Fatalf("expected no fallback capability when wayland-info is invalid")
	}
}

func TestLinuxWindowStateAPIsReturnUnsupported(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")

	stateChecks := []struct {
		name string
		fn   func() (bool, error)
	}{
		{"IsTopMostE", IsTopMostE},
		{"IsMinimizedE", IsMinimizedE},
		{"IsMaximizedE", IsMaximizedE},
	}
	for _, tc := range stateChecks {
		_, err := tc.fn()
		if !errors.Is(err, ErrNotSupported) {
			t.Fatalf("%s expected ErrNotSupported, got %v", tc.name, err)
		}
	}
	if err := SetTopMostE(true); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("SetTopMostE expected ErrNotSupported, got %v", err)
	}
}
