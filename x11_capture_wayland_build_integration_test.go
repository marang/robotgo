//go:build linux && cgo && wayland && x11integration

package robotgo_test

import (
	"testing"

	"github.com/marang/robotgo"
)

// TestWaylandBuildPreservesX11Capture proves that enabling the Wayland CGO
// backend at build time does not remove X11 capture from an X11 runtime.
func TestWaylandBuildPreservesX11Capture(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "")

	capabilities := robotgo.GetRuntimeCapabilities()
	if !capabilities.Capture.Available || capabilities.Capture.Backend != "x11" {
		t.Fatalf("capture capability = %+v, want available X11", capabilities.Capture)
	}
	if !capabilities.Bounds.Available || capabilities.Bounds.Backend != "x11" {
		t.Fatalf("bounds capability = %+v, want available X11", capabilities.Bounds)
	}

	const width, height = 8, 8
	img, err := robotgo.Capture(0, 0, width, height)
	if err != nil {
		t.Fatalf("Capture from X11 runtime: %v", err)
	}
	if got := img.Bounds(); got.Min.X != 0 || got.Min.Y != 0 ||
		got.Dx() != width || got.Dy() != height {
		t.Fatalf("Capture bounds = %v, want %dx%d at zero origin", got, width, height)
	}
	if got := robotgo.LastBackend(); got != robotgo.BackendX11 {
		t.Fatalf("LastBackend = %q, want %q", got, robotgo.BackendX11)
	}

	capturedImage, err := robotgo.CaptureImg(0, 0, width, height)
	if err != nil {
		t.Fatalf("CaptureImg from X11 runtime: %v", err)
	}
	if got := capturedImage.Bounds(); got.Dx() != width || got.Dy() != height {
		t.Fatalf("CaptureImg bounds = %v, want %dx%d", got, width, height)
	}
	if got := robotgo.LastBackend(); got != robotgo.BackendX11 {
		t.Fatalf("CaptureImg LastBackend = %q, want %q", got, robotgo.BackendX11)
	}
}
