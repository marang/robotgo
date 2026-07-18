//go:build cgo && linux && wayland

package robotgo

import (
	"errors"
	"testing"
)

func TestWaylandBuildX11CaptureRejectsWaylandSessionConflict(t *testing.T) {
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, "xwayland-test.invalid:1")
	t.Setenv(envLinuxSessionType, linuxSessionTypeWayland)

	if _, err := platformCapture(0, 0, 1, 1); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("platformCapture error = %v, want ErrNotSupported", err)
	}
	if _, err := platformDisplayBoundsE(0); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("platformDisplayBoundsE error = %v, want ErrNotSupported", err)
	}
	capabilities := GetRuntimeCapabilities()
	if capabilities.Capture.Available {
		t.Fatalf("capture capability = %+v, want unavailable", capabilities.Capture)
	}
	if capabilities.Bounds.Available {
		t.Fatalf("bounds capability = %+v, want unavailable", capabilities.Bounds)
	}
}

func TestWaylandBuildX11CaptureImgRejectsProcessTarget(t *testing.T) {
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, "x11-test.invalid:1")
	t.Setenv(envLinuxSessionType, "")

	if _, err := CaptureImg(0, 0, 1, 1, 0, 1); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("CaptureImg error = %v, want ErrNotSupported", err)
	}
}
