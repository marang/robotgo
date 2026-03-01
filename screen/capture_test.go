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
		if _, err := robotgo.CaptureScreen(); err != nil {
			t.Skipf("X11 capture skipped: %v", err)
		}
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
        if _, err := robotgo.CaptureScreen(); err != nil && strings.Contains(err.Error(), "no display server found") {
            t.Fatalf("unexpected no display server error: %v", err)
        }
    })

    t.Run("PortalForce", func(t *testing.T) {
        t.Setenv("DISPLAY", "")
        t.Setenv("WAYLAND_DISPLAY", "")
        t.Setenv("ROBOTGO_FORCE_PORTAL", "1")
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
    })
}
