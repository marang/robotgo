package screen

import (
	"os"
	"testing"

	robotgo "github.com/marang/robotgo"
)

func TestCaptureScreen(t *testing.T) {
	t.Parallel()
	originalDisplay := os.Getenv("DISPLAY")
	originalWayland := os.Getenv("WAYLAND_DISPLAY")
	defer os.Setenv("DISPLAY", originalDisplay)
	defer os.Setenv("WAYLAND_DISPLAY", originalWayland)

	// X11 path
	os.Setenv("DISPLAY", ":0")
	os.Unsetenv("WAYLAND_DISPLAY")
	if _, err := robotgo.CaptureScreen(); err != nil {
		t.Skipf("X11 capture skipped: %v", err)
	}

	// Wayland path should return an error
	os.Unsetenv("DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if _, err := robotgo.CaptureScreen(); err == nil {
		t.Fatalf("expected error on Wayland capture")
	}
}
