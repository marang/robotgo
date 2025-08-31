package screen

import (
	"testing"

	robotgo "github.com/marang/robotgo"
)

func TestCaptureScreen(t *testing.T) {
	// X11 path
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "")
	if _, err := robotgo.CaptureScreen(); err != nil {
		t.Skipf("X11 capture skipped: %v", err)
	}
	if _, err := robotgo.GetPixelColor(0, 0); err != nil {
		t.Skipf("X11 pixel skipped: %v", err)
	}

	// Wayland path should return an error
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if _, err := robotgo.CaptureScreen(); err == nil {
		t.Fatalf("expected error on Wayland capture")
	}
	if _, err := robotgo.GetPixelColor(0, 0); err == nil {
		t.Fatalf("expected error on Wayland pixel")
	}
}
