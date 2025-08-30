//go:build linux

package base

import (
	"os"
	"testing"
)

func TestDetectDisplayServer(t *testing.T) {
	t.Run("Wayland", func(t *testing.T) {
		os.Setenv("WAYLAND_DISPLAY", "wayland-0")
		os.Unsetenv("DISPLAY")
		if ds := DetectDisplayServer(); ds != Wayland {
			t.Fatalf("expected Wayland, got %v", ds)
		}
		os.Unsetenv("WAYLAND_DISPLAY")
	})

	t.Run("X11", func(t *testing.T) {
		os.Unsetenv("WAYLAND_DISPLAY")
		os.Setenv("DISPLAY", ":0")
		if ds := DetectDisplayServer(); ds != X11 {
			t.Fatalf("expected X11, got %v", ds)
		}
		os.Unsetenv("DISPLAY")
	})
}
