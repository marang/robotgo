//go:build linux && cgo && x11integration && !wayland

package robotgo_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/marang/robotgo"
)

const x11DestroyedWindowHelperEnv = "ROBOTGO_X11_DESTROYED_WINDOW_HELPER"

func x11InternAtom(t *testing.T, conn *xgb.Conn, name string) xproto.Atom {
	t.Helper()
	reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
	if err != nil || reply == nil {
		t.Fatalf("intern X11 atom %q: reply=%+v err=%v", name, reply, err)
	}
	return reply.Atom
}

func configureX11WindowMetadata(t *testing.T, harness *x11InputHarness, title string) {
	t.Helper()
	pidAtom := x11InternAtom(t, harness.conn, "_NET_WM_PID")
	cardinalAtom := x11InternAtom(t, harness.conn, "CARDINAL")
	payload := make([]byte, 4)
	xgb.Put32(payload, uint32(os.Getpid()))
	if err := xproto.ChangePropertyChecked(
		harness.conn, xproto.PropModeReplace, harness.window, pidAtom,
		cardinalAtom, 32, 1, payload,
	).Check(); err != nil {
		t.Fatalf("set _NET_WM_PID: %v", err)
	}

	nameAtom := x11InternAtom(t, harness.conn, "_NET_WM_NAME")
	utf8Atom := x11InternAtom(t, harness.conn, "UTF8_STRING")
	if err := xproto.ChangePropertyChecked(
		harness.conn, xproto.PropModeReplace, harness.window, nameAtom,
		utf8Atom, 8, uint32(len(title)), []byte(title),
	).Check(); err != nil {
		t.Fatalf("set _NET_WM_NAME: %v", err)
	}
	harness.conn.Sync()
}

func setMalformedX11Property(
	t *testing.T,
	harness *x11InputHarness,
	window xproto.Window,
	name string,
	payload []byte,
) {
	t.Helper()
	property := x11InternAtom(t, harness.conn, name)
	if err := xproto.ChangePropertyChecked(
		harness.conn,
		xproto.PropModeReplace,
		window,
		property,
		xproto.AtomString,
		8,
		uint32(len(payload)),
		payload,
	).Check(); err != nil {
		t.Fatalf("set malformed %s: %v", name, err)
	}
	harness.conn.Sync()
}

func setX11ClientList(t *testing.T, harness *x11InputHarness, windows ...xproto.Window) {
	t.Helper()
	property := x11InternAtom(t, harness.conn, "_NET_CLIENT_LIST")
	payload := make([]byte, 4*len(windows))
	for index, window := range windows {
		xgb.Put32(payload[index*4:], uint32(window))
	}
	if err := xproto.ChangePropertyChecked(
		harness.conn,
		xproto.PropModeReplace,
		harness.root,
		property,
		xproto.AtomWindow,
		32,
		uint32(len(windows)),
		payload,
	).Check(); err != nil {
		t.Fatalf("set _NET_CLIENT_LIST: %v", err)
	}
	harness.conn.Sync()
}

func TestNativeX11ConfiguredTargetAppliesToWindowAndScale(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	configureX11WindowMetadata(t, harness, "robotgo-x11-target")

	original := robotgo.GetXDisplayName()
	display := os.Getenv("DISPLAY")
	t.Cleanup(func() {
		if err := robotgo.SetXDisplayName(original); err != nil {
			t.Errorf("restore X11 display override: %v", err)
		}
		_ = robotgo.CloseMainDisplayE()
	})

	if err := robotgo.SetXDisplayName(display); err != nil {
		t.Fatalf("configure X11 target: %v", err)
	}
	title, err := robotgo.GetTitleE(int(harness.window), 1)
	if err != nil || title != "robotgo-x11-target" {
		t.Fatalf("GetTitleE on configured X11 target = %q, %v", title, err)
	}

	// DetectDisplayServer intentionally remains environment-only, while backend
	// selection must continue to honor an explicit SetXDisplayName target.
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if detected := robotgo.DetectDisplayServer(); detected != robotgo.DisplayServerUnknown {
		t.Fatalf("environment-only display detection = %q, want unknown", detected)
	}
	if selected := robotgo.GetRuntimeBackendInfo().DisplayServer; selected != robotgo.DisplayServerX11 {
		t.Fatalf("selected runtime display server = %q, want X11", selected)
	}
	capabilities := robotgo.GetLinuxCapabilities()
	if capabilities.DisplayServer != robotgo.DisplayServerX11 || !capabilities.Capture.Available {
		t.Fatalf("capabilities with explicit X11 target = %+v, want available X11 capture", capabilities)
	}
	bitmap, err := robotgo.CaptureScreen(0, 0, 8, 8)
	if err != nil {
		t.Fatalf("CaptureScreen with explicit X11 target and empty DISPLAY: %v", err)
	}
	robotgo.FreeBitmap(bitmap)
	if err := robotgo.KeyboardReady(); err != nil {
		t.Fatalf("KeyboardReady with explicit X11 target and empty DISPLAY: %v", err)
	}
	if err := robotgo.MouseReady(); err != nil {
		t.Fatalf("MouseReady with explicit X11 target and empty DISPLAY: %v", err)
	}
	if _, _, err := robotgo.LocationE(); err != nil {
		t.Fatalf("LocationE with explicit X11 target and empty DISPLAY: %v", err)
	}

	if err := robotgo.SetXDisplayName(""); err != nil {
		t.Fatalf("clear explicit X11 target: %v", err)
	}
	if selected := robotgo.GetRuntimeBackendInfo().DisplayServer; selected != robotgo.DisplayServerUnknown {
		t.Fatalf("selected runtime display server without environment or explicit target = %q, want unknown", selected)
	}
	readinessChecks := []struct {
		operation string
		err       error
	}{
		{operation: "KeyboardReady", err: robotgo.KeyboardReady()},
		{operation: "MouseReady", err: robotgo.MouseReady()},
	}
	for _, check := range readinessChecks {
		operation, err := check.operation, check.err
		if !errors.Is(err, robotgo.ErrNotSupported) {
			t.Fatalf("%s without selected display server error = %v, want ErrNotSupported", operation, err)
		}
	}
	if _, _, err := robotgo.LocationE(); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("LocationE without selected display server error = %v, want ErrNotSupported", err)
	}
	if err := robotgo.SetXDisplayName(display); err != nil {
		t.Fatalf("restore explicit X11 target after unknown-backend checks: %v", err)
	}

	if scale := robotgo.SysScale(); scale <= 0 {
		t.Fatalf("SysScale on configured X11 target = %f, want positive", scale)
	}
	if capability := robotgo.GetLinuxCapabilities().Window; !capability.Available || capability.Backend != "x11" {
		t.Fatalf("X11 window capability on configured target = %+v", capability)
	}

	if err := robotgo.SetXDisplayName(":65535"); err != nil {
		t.Fatalf("configure invalid X11 target: %v", err)
	}
	if capability := robotgo.GetLinuxCapabilities().Window; capability.Available {
		t.Fatalf("window capability stayed available on invalid configured target: %+v", capability)
	}
	if _, err := robotgo.GetTitleE(int(harness.window), 1); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("GetTitleE on invalid configured target error = %v, want ErrNotSupported", err)
	}
	if count := robotgo.DisplaysNum(); count != 0 {
		t.Fatalf("DisplaysNum on invalid configured target = %d, want 0", count)
	}
	if id := robotgo.GetMainId(); id != -1 {
		t.Fatalf("GetMainId on invalid configured target = %d, want -1", id)
	}
	if err := robotgo.ActivePid(int(harness.window), 1); err == nil {
		t.Fatal("ActivePid silently used DISPLAY after the configured target became invalid")
	}

	if err := robotgo.SetXDisplayName(display); err != nil {
		t.Fatalf("restore configured X11 target: %v", err)
	}
	title, err = robotgo.GetTitleE(int(harness.window), 1)
	if err != nil || title != "robotgo-x11-target" {
		t.Fatalf("GetTitleE after display/atom-cache reload = %q, %v", title, err)
	}
}

func TestNativeX11ClientCoordinatesThroughReparenting(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	configureX11WindowMetadata(t, harness, "robotgo-reparent-coordinates")
	screen := xproto.Setup(harness.conn).DefaultScreen(harness.conn)

	parent, err := xproto.NewWindowId(harness.conn)
	if err != nil {
		t.Fatalf("allocate parent window: %v", err)
	}
	child, err := xproto.NewWindowId(harness.conn)
	if err != nil {
		t.Fatalf("allocate child window: %v", err)
	}
	if err := xproto.CreateWindowChecked(
		harness.conn, screen.RootDepth, parent, harness.root, 250, 200, 120, 90,
		0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil,
	).Check(); err != nil {
		t.Fatalf("create parent window: %v", err)
	}
	if err := xproto.CreateWindowChecked(
		harness.conn, screen.RootDepth, child, parent, 15, 20, 70, 40,
		0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil,
	).Check(); err != nil {
		t.Fatalf("create child window: %v", err)
	}
	t.Cleanup(func() { _ = xproto.DestroyWindowChecked(harness.conn, parent).Check() })
	harness.conn.Sync()

	x, y, width, height := robotgo.GetClient(int(child), 1)
	if x != 265 || y != 220 || width != 70 || height != 40 {
		t.Fatalf("GetClient(reparented child) = (%d,%d %dx%d), want (265,220 70x40)", x, y, width, height)
	}
}

func TestNativeX11DestroyedWindowDoesNotAbort(t *testing.T) {
	if os.Getenv(x11DestroyedWindowHelperEnv) != "1" {
		cmd := exec.Command(
			"xvfb-run",
			"-a",
			"-s", "-screen 0 1280x720x24 -nolisten tcp -noreset",
			os.Args[0],
			"-test.run=^TestNativeX11DestroyedWindowDoesNotAbort$",
			"-test.v",
		)
		cmd.Env = append(os.Environ(), x11DestroyedWindowHelperEnv+"=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("destroyed-window subprocess aborted or failed: %v\n%s", err, output)
		}
		return
	}

	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	if err := robotgo.SetXDisplayName(os.Getenv("DISPLAY")); err != nil {
		t.Fatalf("configure X11 target: %v", err)
	}

	// The first query happens before any EWMH atoms or properties have been
	// created on this isolated Xvfb server. A later client must be able to add
	// those properties without forcing the shared display to reconnect.
	if _, err := robotgo.GetTitleE(int(harness.window), 1); err == nil {
		t.Fatal("GetTitleE unexpectedly succeeded before window metadata existed")
	}
	configureX11WindowMetadata(t, harness, "robotgo-destroyed-window")
	if title, err := robotgo.GetTitleE(int(harness.window), 1); err != nil || title != "robotgo-destroyed-window" {
		t.Fatalf("GetTitleE after late EWMH property creation = %q, %v", title, err)
	}

	screen := xproto.Setup(harness.conn).DefaultScreen(harness.conn)
	clientWithoutPID, err := xproto.NewWindowId(harness.conn)
	if err != nil {
		t.Fatalf("allocate client without PID: %v", err)
	}
	if err := xproto.CreateWindowChecked(
		harness.conn, screen.RootDepth, clientWithoutPID, harness.root, 10, 10, 20, 20,
		0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil,
	).Check(); err != nil {
		t.Fatalf("create client without PID: %v", err)
	}
	if err := xproto.ChangePropertyChecked(
		harness.conn,
		xproto.PropModeReplace,
		clientWithoutPID,
		x11InternAtom(t, harness.conn, "_NET_WM_PID"),
		xproto.AtomCardinal,
		32,
		0,
		nil,
	).Check(); err != nil {
		t.Fatalf("set empty _NET_WM_PID: %v", err)
	}
	t.Cleanup(func() { _ = xproto.DestroyWindowChecked(harness.conn, clientWithoutPID).Check() })
	setX11ClientList(t, harness, clientWithoutPID, harness.window)
	if xid, err := robotgo.GetXid(nil, os.Getpid()); err != nil || xid != harness.window {
		t.Fatalf("GetXid after PID-less client = %d, %v; want %d", xid, err, harness.window)
	}

	destroyed, err := xproto.NewWindowId(harness.conn)
	if err != nil {
		t.Fatalf("allocate destroyed-window target: %v", err)
	}
	if err := xproto.CreateWindowChecked(
		harness.conn, screen.RootDepth, destroyed, harness.root, 30, 30, 80, 60,
		0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil,
	).Check(); err != nil {
		t.Fatalf("create destroyed-window target: %v", err)
	}
	if err := xproto.DestroyWindowChecked(harness.conn, destroyed).Check(); err != nil {
		t.Fatalf("destroy X11 test window: %v", err)
	}
	harness.conn.Sync()

	if x, y, w, h := robotgo.GetClient(int(destroyed), 1); x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("GetClient(destroyed) = (%d,%d %dx%d), want zero bounds", x, y, w, h)
	}
	if x, y, w, h := robotgo.GetBounds(int(destroyed), 1); x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("GetBounds(destroyed) = (%d,%d %dx%d), want zero bounds", x, y, w, h)
	}
	if _, err := robotgo.GetTitleE(int(destroyed), 1); err == nil {
		t.Fatal("GetTitleE(destroyed) returned no error")
	}
	if err := robotgo.SetActiveE(robotgo.GetHandByPid(int(destroyed), 1)); err == nil {
		t.Fatal("SetActiveE(destroyed) returned no error")
	}
	if err := robotgo.ActivePidC(int(destroyed), 1); err == nil {
		t.Fatal("ActivePidC(destroyed) returned no error")
	}
	if err := robotgo.CloseWindowE(int(destroyed), 1); err == nil {
		t.Fatal("CloseWindowE(destroyed) returned no error")
	}

	// X11 properties belong to other clients and are untrusted. Numeric EWMH
	// consumers must reject a format/type mismatch instead of casting the
	// returned byte buffer to long and reading beyond it.
	setMalformedX11Property(t, harness, harness.root, "_NET_ACTIVE_WINDOW", []byte{1})
	if handle := robotgo.GetHandle(); handle != int(harness.window) {
		t.Fatalf("GetHandle with malformed _NET_ACTIVE_WINDOW = %d, want focused window %d", handle, harness.window)
	}

	setMalformedX11Property(t, harness, harness.window, "_NET_WM_PID", []byte{1})
	if pid := robotgo.GetPid(); pid != 0 {
		t.Fatalf("GetPid with malformed _NET_WM_PID = %d, want 0", pid)
	}
	configureX11WindowMetadata(t, harness, "robotgo-malformed-properties")

	setMalformedX11Property(t, harness, harness.window, "_NET_FRAME_EXTENTS", []byte{1, 2, 3, 4})
	clientX, clientY, clientWidth, clientHeight := robotgo.GetClient(int(harness.window), 1)
	boundX, boundY, boundWidth, boundHeight := robotgo.GetBounds(int(harness.window), 1)
	if boundX != clientX || boundY != clientY || boundWidth != clientWidth || boundHeight != clientHeight {
		t.Fatalf(
			"GetBounds with malformed _NET_FRAME_EXTENTS = (%d,%d %dx%d), want client bounds (%d,%d %dx%d)",
			boundX, boundY, boundWidth, boundHeight,
			clientX, clientY, clientWidth, clientHeight,
		)
	}

	setMalformedX11Property(t, harness, harness.window, "_NET_WM_DESKTOP", []byte{1})
	_ = robotgo.SetActiveE(robotgo.GetHandByPid(int(harness.window), 1))

	fmt.Fprintln(os.Stdout, "destroyed-window X11 operations returned safely")
}
