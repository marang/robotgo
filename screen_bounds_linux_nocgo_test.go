//go:build linux && !cgo

package robotgo

import (
	"errors"
	"testing"
)

func TestPureGoWaylandBoundsNeverUseX11(t *testing.T) {
	t.Setenv(envWaylandDisplay, "wayland-test")
	t.Setenv(envDisplay, ":99")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	assertPureGoWaylandBoundsUnsupported(t)
}

func TestPureGoWaylandSessionConflictBoundsNeverUseX11(t *testing.T) {
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, ":99")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	assertPureGoWaylandBoundsUnsupported(t)
}

func assertPureGoWaylandBoundsUnsupported(t *testing.T) {
	t.Helper()
	if _, _, _, _, err := GetDisplayBoundsE(0); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("GetDisplayBoundsE() error = %v, want ErrNotSupported", err)
	}
	if _, _, err := GetScreenSizeE(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("GetScreenSizeE() error = %v, want ErrNotSupported", err)
	}
	if _, err := GetScreenRectE(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("GetScreenRectE() error = %v, want ErrNotSupported", err)
	}
	if _, err := DisplaysNumE(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("DisplaysNumE() error = %v, want ErrNotSupported", err)
	}
	x, y, width, height := GetDisplayBounds(0)
	if x != 0 || y != 0 || width != 0 || height != 0 {
		t.Fatalf(
			"legacy GetDisplayBounds() = %d,%d %dx%d, want an empty result",
			x,
			y,
			width,
			height,
		)
	}
	if width, height := GetScreenSize(); width != 0 || height != 0 {
		t.Fatalf("legacy GetScreenSize() = %dx%d, want an empty result", width, height)
	}
	if rect := GetScreenRect(); rect != (Rect{}) {
		t.Fatalf("legacy GetScreenRect() = %+v, want an empty result", rect)
	}
	if count := DisplaysNum(); count != 0 {
		t.Fatalf("legacy DisplaysNum() = %d, want 0", count)
	}
}
