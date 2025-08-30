//go:build linux && wayland

package window

import (
	"testing"

	"github.com/marang/robotgo"
	"github.com/marang/robotgo/base"
)

func TestGetBoundsWayland(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")
	if ds := base.DetectDisplayServer(); ds != base.Wayland {
		t.Fatalf("expected Wayland, got %v", ds)
	}
	_, _, _, _ = robotgo.GetBounds(0)
}
