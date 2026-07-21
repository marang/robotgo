//go:build cgo && linux

package robotgo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	inputportal "github.com/marang/robotgo/input/portal"
	portalpkg "github.com/marang/robotgo/screen/portal"
)

func TestCaptureStateConcurrentAccess(t *testing.T) {
	t.Cleanup(func() {
		SetWaylandBackend(WaylandBackendAuto)
		setLastBackend(BackendNone)
	})

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			SetWaylandBackend([]WaylandBackend{WaylandBackendAuto, WaylandBackendDmabuf, WaylandBackendWlShm}[i%3])
			setLastBackend([]CaptureBackend{BackendScreencopy, BackendPortal, BackendX11}[i%3])
			_ = selectedWaylandBackend()
			_ = LastBackend()
		}(i)
	}
	wg.Wait()
}

func resetWaylandBoundsCacheForTest() {
	waylandBoundsMu.Lock()
	waylandBoundsCached = Rect{}
	waylandBoundsValid = false
	waylandBoundsProbed = false
	waylandBoundsAt = time.Time{}
	waylandBoundsMu.Unlock()
}

func stubCaptureCapabilityProbes(t *testing.T, native, portal bool) {
	t.Helper()
	oldNative := waylandCaptureAvailabilityProbe
	oldPortal := portalAvailabilityProbe
	oldRemoteDesktop := remoteDesktopCapabilityProbe
	oldScreenCast := screenCastCapabilityProbe
	waylandCaptureAvailabilityProbe = func() bool { return native }
	portalAvailabilityProbe = func() bool { return portal }
	remoteDesktopCapabilityProbe = func() (inputportal.Capability, error) {
		return inputportal.Capability{}, inputportal.ErrUnavailable
	}
	screenCastCapabilityProbe = func() (portalpkg.ScreenCastCapability, error) {
		return portalpkg.ScreenCastCapability{
			Version:       5,
			Sources:       portalpkg.ScreenCastSourceMonitor,
			CursorModes:   portalpkg.ScreenCastCursorEmbedded,
			PipeWireReady: true,
		}, nil
	}
	t.Cleanup(func() {
		waylandCaptureAvailabilityProbe = oldNative
		portalAvailabilityProbe = oldPortal
		remoteDesktopCapabilityProbe = oldRemoteDesktop
		screenCastCapabilityProbe = oldScreenCast
	})
}

func TestGetLinuxCapabilitiesReportsActiveScreenCast(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	prepareScreenCastCaptureTest(t)
	stubCaptureCapabilityProbes(t, true, true)
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = &fakeScreenCastCapture{}
	screenCastCaptureState.Unlock()

	capabilities := GetLinuxCapabilities()
	if !capabilities.Capture.Available || capabilities.Capture.Backend != FeatureBackendScreenCast {
		t.Fatalf("active ScreenCast capability = %+v", capabilities.Capture)
	}
	if !capabilities.Capture.Fallback {
		t.Fatal("active ScreenCast capability did not report available fallback paths")
	}
	if !strings.Contains(capabilities.Capture.Notes, "interface version=5") {
		t.Fatalf("ScreenCast protocol diagnostics missing: %q", capabilities.Capture.Notes)
	}

	t.Setenv(envDisablePortal, "1")
	capabilities = GetLinuxCapabilities()
	if !capabilities.Capture.Available || capabilities.Capture.Backend != "wayland+screencopy" || capabilities.Capture.Fallback {
		t.Fatalf("native-only capability = %+v", capabilities.Capture)
	}
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

func TestParseWaylandInfoBoundsWithoutOutputIdentifiers(t *testing.T) {
	raw := `interface: 'zxdg_output_manager_v1', version: 3
	xdg_output_v1
		logical_x: -1280, logical_y: 0
		logical_width: 1280, logical_height: 720
	xdg_output_v1
		logical_x: 0, logical_y: -900
		logical_width: 1600, logical_height: 900
interface: 'wl_output', version: 4
	x: -1920, y: 0, scale: 1,
	mode:
		width: 1920 px, height: 1080 px, refresh: 60.000 Hz,
		flags: current
interface: 'wl_output', version: 4
	x: 0, y: -900, scale: 1,
	mode:
		width: 2400 px, height: 1350 px, refresh: 60.000 Hz,
		flags: current
`
	rect, ok := parseWaylandInfoBounds(raw)
	if !ok {
		t.Fatal("expected identifier-free xdg-output parse success")
	}
	if want := (Rect{
		Point: Point{X: -1280, Y: -900},
		Size:  Size{W: 2880, H: 1620},
	}); rect != want {
		t.Fatalf("identifier-free logical bounds = %+v, want %+v", rect, want)
	}
}

func TestParseWaylandInfoBoundsCoreFallbackScaleAndTransform(t *testing.T) {
	raw := `interface: 'wl_output', version: 4
	x: -800, y: 20, physical_width: 600 mm, physical_height: 340 mm,
	transform: 90,
	scale: 2
	mode:
		width: 2400 px, height: 1600 px, refresh: 60.000 Hz,
		flags: current
`
	rect, ok := parseWaylandInfoBounds(raw)
	if !ok {
		t.Fatal("expected core-output fallback parse success")
	}
	if want := (Rect{
		Point: Point{X: -800, Y: 20},
		Size:  Size{W: 800, H: 1200},
	}); rect != want {
		t.Fatalf("scaled transformed bounds = %+v, want %+v", rect, want)
	}
}

func TestParseWaylandInfoBoundsRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	raw := fmt.Sprintf(`interface: 'zxdg_output_manager_v1', version: 3
	xdg_output_v1
		logical_x: %d, logical_y: 0
		logical_width: 2, logical_height: 1
`, maxInt)
	if rect, ok := parseWaylandInfoBounds(raw); ok {
		t.Fatalf("overflowing bounds unexpectedly parsed as %+v", rect)
	}
}

func TestWaylandBoundsCacheExpiresAndCanBeInvalidated(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	tmp := t.TempDir()
	writeWaylandInfoStub(t, tmp)
	t.Setenv(envPath, tmp+":"+os.Getenv(envPath))
	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")

	now := time.Unix(100, 0)
	previousNow := waylandBoundsNow
	waylandBoundsNow = func() time.Time { return now }
	t.Cleanup(func() {
		waylandBoundsNow = previousNow
		InvalidateScreenBoundsCache()
	})

	waylandBoundsMu.Lock()
	waylandBoundsCached = Rect{Point: Point{X: 1, Y: 2}, Size: Size{W: 3, H: 4}}
	waylandBoundsValid = true
	waylandBoundsProbed = true
	waylandBoundsAt = now
	waylandBoundsMu.Unlock()

	if rect, ok := waylandScreenBoundsFallback(); !ok || rect.W != 3 {
		t.Fatalf("fresh cached bounds = %+v, %v", rect, ok)
	}
	now = now.Add(waylandBoundsSuccessTTL + time.Millisecond)
	if rect, ok := waylandScreenBoundsFallback(); !ok || rect.W != 800 || rect.H != 600 {
		t.Fatalf("refreshed bounds = %+v, %v", rect, ok)
	}

	InvalidateScreenBoundsCache()
	waylandBoundsMu.Lock()
	probed := waylandBoundsProbed
	waylandBoundsMu.Unlock()
	if probed {
		t.Fatal("InvalidateScreenBoundsCache left cache marked as probed")
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
	rectE, err := GetScreenRectE()
	if err != nil {
		t.Fatalf("GetScreenRectE fallback error: %v", err)
	}
	if rectE != rect {
		t.Fatalf("GetScreenRectE fallback = %+v, want %+v", rectE, rect)
	}
	widthE, heightE, err := GetScreenSizeE()
	if err != nil {
		t.Fatalf("GetScreenSizeE fallback error: %v", err)
	}
	if widthE != 800 || heightE != 600 {
		t.Fatalf("GetScreenSizeE fallback = %dx%d, want 800x600", widthE, heightE)
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
	stubCaptureCapabilityProbes(t, false, true)

	resetWaylandBoundsCacheForTest()
	defer resetWaylandBoundsCacheForTest()

	c := GetLinuxCapabilities()
	if c.DisplayServer != DisplayServerWayland {
		t.Fatalf("expected wayland display server, got %v", c.DisplayServer)
	}
	if !c.Capture.Available {
		t.Fatalf("expected portal capture available in wayland session")
	}
	if c.Capture.Backend != "portal" {
		t.Fatalf("expected portal capture backend, got %q", c.Capture.Backend)
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
	stubCaptureCapabilityProbes(t, false, false)

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

func TestGetLinuxCapabilitiesReportsRemoteDesktopPortal(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	stubCaptureCapabilityProbes(t, false, false)
	remoteDesktopCapabilityProbe = func() (inputportal.Capability, error) {
		return inputportal.Capability{
			Version:          2,
			AvailableDevices: inputportal.DeviceKeyboard | inputportal.DevicePointer,
			ScreenCastIssue:  "ScreenCast property probe timed out",
		}, inputportal.ErrUnavailable
	}

	capabilities := GetLinuxCapabilities()
	if !capabilities.RemoteDesktop.Available {
		t.Fatalf("expected RemoteDesktop portal capability: %+v", capabilities.RemoteDesktop)
	}
	if capabilities.RemoteDesktop.Backend != "portal-remote-desktop" {
		t.Fatalf("unexpected RemoteDesktop backend %q", capabilities.RemoteDesktop.Backend)
	}
	if !strings.Contains(capabilities.RemoteDesktop.Notes, "ScreenCast property probe timed out") {
		t.Fatalf("ScreenCast degradation missing from notes: %+v", capabilities.RemoteDesktop)
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
