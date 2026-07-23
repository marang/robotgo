//go:build cgo

package robotgo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestSwayWindowBackendGeometry(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdSwayMsg {
			t.Fatalf("expected %q, got %q", cmdSwayMsg, name)
		}
		if want := []string{argType, argGetTree, argRawJSON}; fmt.Sprint(args) != fmt.Sprint(want) {
			t.Fatalf("unexpected args: %#v", args)
		}
		return []byte(`{
			"focused":false,
			"nodes":[{
				"type":"con",
				"focused":true,
				"name":"Terminal",
				"rect":{"x":-400,"y":25,"width":800,"height":600},
				"window_rect":{"x":2,"y":30,"width":796,"height":568}
			}]
		}`), nil
	}

	backend := newSwayWindowBackend()
	bounds, err := backend.Bounds(0, false, false)
	if err != nil {
		t.Fatalf("node bounds: %v", err)
	}
	if want := (Rect{Point: Point{X: -400, Y: 25}, Size: Size{W: 800, H: 600}}); bounds != want {
		t.Fatalf("node bounds = %+v, want %+v", bounds, want)
	}
	client, err := backend.Bounds(0, false, true)
	if err != nil {
		t.Fatalf("client bounds: %v", err)
	}
	if want := (Rect{Point: Point{X: -398, Y: 55}, Size: Size{W: 796, H: 568}}); client != want {
		t.Fatalf("client bounds = %+v, want %+v", client, want)
	}

	for _, call := range []struct {
		name     string
		target   int
		isHandle bool
	}{
		{name: "pid", target: 1234},
		{name: "handle", isHandle: true},
	} {
		t.Run(call.name, func(t *testing.T) {
			if _, err := backend.Bounds(call.target, call.isHandle, false); !errors.Is(err, ErrNotSupported) {
				t.Fatalf("Bounds error = %v, want ErrNotSupported", err)
			}
		})
	}
}

func TestSwayWindowBackendGeometryRejectsInvalidResponses(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	tests := []struct {
		name   string
		json   string
		client bool
	}{
		{name: "malformed", json: `{"focused":`},
		{name: "no focused window", json: `{"focused":false,"nodes":[]}`},
		{
			name: "focused workspace is not a window",
			json: `{
				"type":"workspace",
				"focused":true,
				"rect":{"x":0,"y":0,"width":800,"height":600}
			}`,
		},
		{
			name: "invalid node size",
			json: `{
				"type":"con",
				"focused":true,
				"rect":{"x":0,"y":0,"width":0,"height":600},
				"window_rect":{"x":0,"y":0,"width":800,"height":600}
			}`,
		},
		{
			name:   "invalid client size",
			client: true,
			json: `{
				"type":"con",
				"focused":true,
				"rect":{"x":0,"y":0,"width":800,"height":600},
				"window_rect":{"x":0,"y":0,"width":800,"height":0}
			}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runWindowCommand = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(test.json), nil
			}
			if _, err := newSwayWindowBackend().Bounds(0, false, test.client); !errors.Is(err, errWindowGeometryUnavailable) {
				t.Fatalf("Bounds error = %v, want errWindowGeometryUnavailable", err)
			}
		})
	}
}

func TestSwayWindowBoundsDoNotRequireClientMetadata(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })
	runWindowCommand = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{
			"type":"con",
			"focused":true,
			"rect":{"x":10,"y":20,"width":800,"height":600}
		}`), nil
	}

	bounds, err := newSwayWindowBackend().Bounds(0, false, false)
	if err != nil {
		t.Fatalf("Bounds: %v", err)
	}
	if want := (Rect{Point: Point{X: 10, Y: 20}, Size: Size{W: 800, H: 600}}); bounds != want {
		t.Fatalf("Bounds = %+v, want %+v", bounds, want)
	}
}

func TestCheckedWindowCoordinateRejectsOverflow(t *testing.T) {
	for _, test := range []struct {
		name     string
		base     int
		relative int
	}{
		{name: "positive overflow", base: math.MaxInt, relative: 1},
		{name: "negative overflow", base: math.MinInt, relative: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := checkedWindowCoordinate(test.base, test.relative); err == nil {
				t.Fatal("checkedWindowCoordinate accepted overflowing sum")
			}
		})
	}
}

func TestHyprlandWindowBackendGeometry(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		if want := []string{argActiveWindow, argJSON}; fmt.Sprint(args) != fmt.Sprint(want) {
			t.Fatalf("unexpected args: %#v", args)
		}
		return []byte(`{"at":[-1280,40],"size":[1200,700]}`), nil
	}

	backend := newHyprlandWindowBackend()
	bounds, err := backend.Bounds(0, false, false)
	if err != nil {
		t.Fatalf("Bounds: %v", err)
	}
	if want := (Rect{Point: Point{X: -1280, Y: 40}, Size: Size{W: 1200, H: 700}}); bounds != want {
		t.Fatalf("Bounds = %+v, want %+v", bounds, want)
	}
	if _, err := backend.Bounds(0, false, true); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("client Bounds error = %v, want ErrNotSupported", err)
	}
	if _, err := backend.Bounds(1234, false, false); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("pid Bounds error = %v, want ErrNotSupported", err)
	}
}

func TestHyprlandWindowBackendGeometryRejectsInvalidResponses(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })
	for _, test := range []struct {
		name string
		json string
	}{
		{name: "malformed", json: `{"at":`},
		{name: "missing size", json: `{"at":[0,0]}`},
		{name: "short position", json: `{"at":[0],"size":[800,600]}`},
		{name: "invalid size", json: `{"at":[0,0],"size":[800,0]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			runWindowCommand = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(test.json), nil
			}
			if _, err := newHyprlandWindowBackend().Bounds(0, false, false); !errors.Is(err, errWindowGeometryUnavailable) {
				t.Fatalf("Bounds error = %v, want errWindowGeometryUnavailable", err)
			}
		})
	}
}

func TestWaylandBackendsWithoutGeometryReturnUnsupported(t *testing.T) {
	backends := []windowBackend{
		waylandCoreWindowBackend{compositor: compositorMutter},
		newWlrootsGenericWindowBackend(),
	}
	for _, backend := range backends {
		for _, client := range []bool{false, true} {
			if _, err := backend.Bounds(0, false, client); !errors.Is(err, ErrNotSupported) {
				t.Fatalf(
					"%s Bounds(client=%v) error = %v, want ErrNotSupported",
					backend.Name(),
					client,
					err,
				)
			}
		}
	}
}
