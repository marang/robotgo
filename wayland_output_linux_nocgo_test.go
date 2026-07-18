//go:build linux && !cgo

package robotgo

import (
	"context"
	"errors"
	"testing"

	"github.com/marang/robotgo/internal/waylandoutput"
)

func TestPureGoWaylandBoundsAPIsUseLogicalOutputs(t *testing.T) {
	t.Setenv(envWaylandDisplay, "wayland-test")
	t.Setenv(envDisplay, "")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	setTestRuntimeDisplayID(t, -1)
	setWaylandOutputEnumeratorForTest(t, func(context.Context) (waylandoutput.Snapshot, error) {
		return waylandoutput.Snapshot{
			Outputs: []waylandoutput.Output{
				{
					GlobalName: 50,
					Name:       "HDMI-A-1",
					X:          0,
					Y:          0,
					Width:      1920,
					Height:     1080,
					Scale:      1,
					Logical:    true,
				},
				{
					GlobalName: 10,
					Name:       "DP-1",
					X:          -1280,
					Y:          0,
					Width:      1280,
					Height:     720,
					Scale:      2,
					Transform:  1,
					Logical:    true,
				},
			},
			OutputVersion:    4,
			XDGOutputVersion: 3,
		}, nil
	})

	assertDisplayBounds(t, 0, 0, 0, 1920, 1080)
	assertDisplayBounds(t, 1, -1280, 0, 1280, 720)
	if _, _, _, _, err := GetDisplayBoundsE(2); err == nil {
		t.Fatal("GetDisplayBoundsE(2) unexpectedly succeeded")
	}

	if count, err := DisplaysNumE(); err != nil || count != 2 {
		t.Fatalf("DisplaysNumE() = %d, %v, want 2, nil", count, err)
	}
	if count := DisplaysNum(); count != 2 {
		t.Fatalf("DisplaysNum() = %d, want 2", count)
	}
	if width, height, err := GetScreenSizeE(); err != nil || width != 1920 || height != 1080 {
		t.Fatalf("GetScreenSizeE() = %dx%d, %v, want 1920x1080, nil", width, height, err)
	}
	if width, height := GetScreenSize(); width != 1920 || height != 1080 {
		t.Fatalf("GetScreenSize() = %dx%d, want 1920x1080", width, height)
	}

	wantAggregate := Rect{
		Point: Point{X: -1280, Y: 0},
		Size:  Size{W: 3200, H: 1080},
	}
	if rect, err := GetScreenRectE(); err != nil || rect != wantAggregate {
		t.Fatalf("GetScreenRectE() = %+v, %v, want %+v, nil", rect, err, wantAggregate)
	}
	if rect := GetScreenRect(); rect != wantAggregate {
		t.Fatalf("GetScreenRect() = %+v, want %+v", rect, wantAggregate)
	}
	wantSecond := Rect{
		Point: Point{X: -1280, Y: 0},
		Size:  Size{W: 1280, H: 720},
	}
	if rect, err := GetScreenRectE(1); err != nil || rect != wantSecond {
		t.Fatalf("GetScreenRectE(1) = %+v, %v, want %+v, nil", rect, err, wantSecond)
	}

	capability := pureGoWaylandBoundsCapability()
	if !capability.Available || capability.Backend != featureBackendPureGoWaylandOutput {
		t.Fatalf("bounds capability = %+v", capability)
	}
	if capability.Notes != "outputs=2 wl_output=4 xdg-output=3" {
		t.Fatalf("bounds capability notes = %q", capability.Notes)
	}
}

func TestPureGoWaylandBoundsAPIsPreserveBackendErrors(t *testing.T) {
	t.Setenv(envWaylandDisplay, "wayland-test")
	t.Setenv(envDisplay, "")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	setTestRuntimeDisplayID(t, -1)
	setWaylandOutputEnumeratorForTest(t, func(context.Context) (waylandoutput.Snapshot, error) {
		return waylandoutput.Snapshot{}, waylandoutput.ErrUnavailable
	})

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
	if rect := GetScreenRect(); rect != (Rect{}) {
		t.Fatalf("GetScreenRect() = %+v, want empty legacy result", rect)
	}
	if count := DisplaysNum(); count != 0 {
		t.Fatalf("DisplaysNum() = %d, want empty legacy result", count)
	}

	capability := pureGoWaylandBoundsCapability()
	if capability.Available || capability.Backend != featureBackendPureGoWaylandOutput {
		t.Fatalf("bounds capability = %+v", capability)
	}
}

func assertDisplayBounds(t *testing.T, displayID, x, y, width, height int) {
	t.Helper()
	gotX, gotY, gotWidth, gotHeight, err := GetDisplayBoundsE(displayID)
	if err != nil {
		t.Fatalf("GetDisplayBoundsE(%d) error = %v", displayID, err)
	}
	if gotX != x || gotY != y || gotWidth != width || gotHeight != height {
		t.Fatalf(
			"GetDisplayBoundsE(%d) = %d,%d %dx%d, want %d,%d %dx%d",
			displayID,
			gotX,
			gotY,
			gotWidth,
			gotHeight,
			x,
			y,
			width,
			height,
		)
	}
}

func setWaylandOutputEnumeratorForTest(
	t *testing.T,
	enumerate func(context.Context) (waylandoutput.Snapshot, error),
) {
	t.Helper()
	previous := pureGoWaylandOutputEnumerate
	pureGoWaylandOutputEnumerate = enumerate
	t.Cleanup(func() {
		pureGoWaylandOutputEnumerate = previous
	})
}

func setTestRuntimeDisplayID(t *testing.T, displayID int) {
	t.Helper()
	previous := GetRuntimeConfig()
	config := previous
	config.DisplayID = displayID
	if err := SetRuntimeConfig(config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := SetRuntimeConfig(previous); err != nil {
			t.Errorf("restore runtime config: %v", err)
		}
	})
}
