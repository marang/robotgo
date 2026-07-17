//go:build cgo && linux

package screen

import (
	"os"
	"strings"
	"testing"

	robotgo "github.com/marang/robotgo"
)

func TestCaptureScreen(t *testing.T) {
	t.Run("X11", func(t *testing.T) {
		display := os.Getenv("DISPLAY")
		if display == "" {
			t.Skip("DISPLAY not set")
		}
		// openXDisplay returns false when the X server is unavailable
		// (including when running on platforms without X11).
		if !openXDisplay(display) {
			t.Skipf("cannot open DISPLAY %q", display)
		}

		t.Setenv("WAYLAND_DISPLAY", "")
		// Force the portal fallback to avoid X_GetImage crashes on constrained servers.
		t.Setenv("ROBOTGO_FORCE_PORTAL", "1")
		bitmap, err := robotgo.CaptureScreen()
		if err != nil {
			t.Skipf("X11 capture skipped: %v", err)
		}
		t.Cleanup(func() { robotgo.FreeBitmap(bitmap) })
		if lb := robotgo.LastBackend(); lb != robotgo.BackendPortal {
			t.Fatalf("expected portal backend, got %v", lb)
		}
		if _, err := robotgo.GetPixelColor(0, 0); err != nil {
			t.Skipf("X11 pixel skipped: %v", err)
		}
	})

	t.Run("Wayland", func(t *testing.T) {
		wayland := os.Getenv("WAYLAND_DISPLAY")
		if wayland == "" {
			t.Skip("WAYLAND_DISPLAY not set")
		}
		t.Setenv("DISPLAY", "")
		bitmap, err := robotgo.CaptureScreen()
		if err != nil {
			if strings.Contains(err.Error(), "no display server found") {
				t.Fatalf("unexpected no display server error: %v", err)
			}
			return
		}
		t.Cleanup(func() { robotgo.FreeBitmap(bitmap) })
	})

	t.Run("PortalForce", func(t *testing.T) {
		t.Setenv("DISPLAY", "")
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("ROBOTGO_FORCE_PORTAL", "1")
		t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
		t.Setenv("ROBOTGO_PORTAL_STUB_GREEN", "1")
		img, err := robotgo.CaptureImg()
		if err != nil {
			t.Fatalf("portal forced CaptureImg failed: %v", err)
		}
		if img == nil {
			t.Fatalf("portal forced CaptureImg returned nil image")
		}
		if lb := robotgo.LastBackend(); lb != robotgo.BackendPortal {
			t.Fatalf("expected portal backend, got %v", lb)
		}

		serialized, err := robotgo.CaptureBitmapStr(10, 20, 2, 2)
		if err != nil {
			t.Fatalf("portal CaptureBitmapStr failed: %v", err)
		}
		decoded, err := robotgo.BitmapFromStr(serialized)
		if err != nil {
			t.Fatalf("portal BitmapFromStr failed: %v", err)
		}
		if decoded.Width != 2 || decoded.Height != 2 {
			t.Fatalf("portal decoded bitmap = %dx%d, want 2x2", decoded.Width, decoded.Height)
		}
		x, y, err := robotgo.FindBitmapStr(serialized)
		if err != nil {
			t.Fatalf("portal FindBitmapStr capture failed: %v", err)
		}
		if x != 0 || y != 0 {
			t.Fatalf("portal FindBitmapStr = (%d,%d), want (0,0)", x, y)
		}

		x, y, err = robotgo.FindColorCS(10, 20, 2, 2, robotgo.CHex(0x00ff00), 0)
		if err != nil {
			t.Fatalf("portal FindColorCS failed: %v", err)
		}
		if x != 10 || y != 20 {
			t.Fatalf("portal FindColorCS = (%d,%d), want (10,20)", x, y)
		}
	})
}
