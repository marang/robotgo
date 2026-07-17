//go:build !cgo

package robotgo

import (
	"runtime"
	"testing"
)

func TestPureGoWaylandCompositorDetectionMatchesNativeContract(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Wayland compositor detection is Linux-specific")
	}
	t.Setenv(envWaylandDisplay, "wayland-test")
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envHyprlandSignature, "")
	t.Setenv(envSwaySock, "sway-test.sock")
	if got := detectWaylandCompositor(); got != compositorSway {
		t.Fatalf("SWAYSOCK compositor = %q, want %q", got, compositorSway)
	}

	t.Setenv(envSwaySock, "")
	t.Setenv(envDesktop, "GNOME")
	if got := detectWaylandCompositor(); got != compositorMutter {
		t.Fatalf("GNOME compositor = %q, want %q", got, compositorMutter)
	}
}
