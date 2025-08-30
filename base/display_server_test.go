//go:build linux

package base

import (
	"testing"
)

func TestDetectDisplayServer(t *testing.T) {
	t.Run("Wayland", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")
		t.Setenv("DISPLAY", "")
		if ds := DetectDisplayServer(); ds != Wayland {
			t.Fatalf("expected Wayland, got %v", ds)
		}
	})

	t.Run("X11", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", ":0")
		if ds := DetectDisplayServer(); ds != X11 {
			t.Fatalf("expected X11, got %v", ds)
		}
	})

	t.Run("Unknown", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", "")
		if ds := DetectDisplayServer(); ds != Unknown {
			t.Fatalf("expected Unknown, got %v", ds)
		}
	})
}
