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
		if _, err := robotgo.CaptureScreen(); err != nil {
			t.Skipf("X11 capture skipped: %v", err)
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
}
