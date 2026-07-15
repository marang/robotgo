//go:build linux && !cgo && x11integration

package robotgo_test

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
	"github.com/marang/robotgo"
)

const (
	x11EventTimeout = 3 * time.Second
	x11PollInterval = 5 * time.Millisecond

	x11WindowX      = 100
	x11WindowY      = 100
	x11WindowWidth  = 400
	x11WindowHeight = 300

	x11ButtonLeft       = 1
	x11ButtonRight      = 3
	x11ButtonWheelUp    = 4
	x11ButtonWheelRight = 6

	x11KeysymA      = 0x0061
	x11KeysymZ      = 0x007a
	x11KeysymShiftL = 0xffe1
	x11KeysymShiftR = 0xffe2
)

type x11InputHarness struct {
	t    *testing.T
	conn *xgb.Conn
	root xproto.Window
}

type x11ButtonEvent struct {
	pressed bool
	button  xproto.Button
}

func newX11InputHarness(t *testing.T) *x11InputHarness {
	t.Helper()
	display := os.Getenv("DISPLAY")
	if display == "" {
		t.Skip("X11 integration test requires DISPLAY")
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("X11 integration test requires an X11-primary session; unset WAYLAND_DISPLAY")
	}
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		t.Skipf("X11 integration test cannot connect to DISPLAY %q: %v", display, err)
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		t.Skipf("X11 integration test requires the XTEST extension: %v", err)
	}
	if _, err := xtest.GetVersion(conn, 2, 2).Reply(); err != nil {
		conn.Close()
		t.Skipf("X11 integration test cannot query the XTEST extension: %v", err)
	}

	screen := xproto.Setup(conn).DefaultScreen(conn)
	if screen == nil {
		conn.Close()
		t.Fatal("X11 connection has no default screen")
	}
	windowID, err := xproto.NewWindowId(conn)
	if err != nil {
		conn.Close()
		t.Fatalf("allocate X11 test window: %v", err)
	}
	eventMask := uint32(
		xproto.EventMaskKeyPress |
			xproto.EventMaskKeyRelease |
			xproto.EventMaskButtonPress |
			xproto.EventMaskButtonRelease |
			xproto.EventMaskPointerMotion |
			xproto.EventMaskStructureNotify,
	)
	if err := xproto.CreateWindowChecked(
		conn,
		screen.RootDepth,
		windowID,
		screen.Root,
		x11WindowX,
		x11WindowY,
		x11WindowWidth,
		x11WindowHeight,
		0,
		xproto.WindowClassInputOutput,
		screen.RootVisual,
		xproto.CwBackPixel|xproto.CwEventMask,
		[]uint32{screen.BlackPixel, eventMask},
	).Check(); err != nil {
		conn.Close()
		t.Fatalf("create X11 test window: %v", err)
	}
	if err := xproto.MapWindowChecked(conn, windowID).Check(); err != nil {
		conn.Close()
		t.Fatalf("map X11 test window: %v", err)
	}
	deadline := time.Now().Add(x11EventTimeout)
	mapped := false
	for time.Now().Before(deadline) {
		event, err := conn.PollForEvent()
		if err != nil {
			conn.Close()
			t.Fatalf("wait for X11 test window to map: %v", err)
		}
		if notify, ok := event.(xproto.MapNotifyEvent); ok && notify.Window == windowID {
			mapped = true
			break
		}
		time.Sleep(x11PollInterval)
	}
	if !mapped {
		conn.Close()
		t.Fatalf("X11 test window did not map within %s", x11EventTimeout)
	}
	if err := xproto.SetInputFocusChecked(
		conn,
		xproto.InputFocusPointerRoot,
		windowID,
		xproto.TimeCurrentTime,
	).Check(); err != nil {
		conn.Close()
		t.Fatalf("focus X11 test window: %v", err)
	}
	conn.Sync()

	harness := &x11InputHarness{
		t:    t,
		conn: conn,
		root: screen.Root,
	}
	harness.drainEvents()
	t.Cleanup(func() {
		_ = xproto.DestroyWindowChecked(conn, windowID).Check()
		conn.Close()
	})
	t.Cleanup(robotgo.CloseMainDisplay)
	return harness
}

func (h *x11InputHarness) drainEvents() {
	h.t.Helper()
	for {
		event, err := h.conn.PollForEvent()
		if err != nil {
			h.t.Fatalf("drain X11 events: %v", err)
		}
		if event == nil {
			return
		}
	}
}

func (h *x11InputHarness) waitForEvent(description string, match func(xgb.Event) bool) xgb.Event {
	h.t.Helper()
	deadline := time.Now().Add(x11EventTimeout)
	for time.Now().Before(deadline) {
		event, err := h.conn.PollForEvent()
		if err != nil {
			h.t.Fatalf("poll for %s: %v", description, err)
		}
		if event != nil && match(event) {
			return event
		}
		time.Sleep(x11PollInterval)
	}
	h.t.Fatalf("timed out after %s waiting for %s", x11EventTimeout, description)
	return nil
}

func (h *x11InputHarness) waitForKeyPress(description string) xproto.KeyPressEvent {
	h.t.Helper()
	event := h.waitForEvent(description, func(event xgb.Event) bool {
		_, ok := event.(xproto.KeyPressEvent)
		return ok
	})
	return event.(xproto.KeyPressEvent)
}

func (h *x11InputHarness) waitForKeyRelease(description string) xproto.KeyReleaseEvent {
	h.t.Helper()
	event := h.waitForEvent(description, func(event xgb.Event) bool {
		_, ok := event.(xproto.KeyReleaseEvent)
		return ok
	})
	return event.(xproto.KeyReleaseEvent)
}

func (h *x11InputHarness) waitForButtonEvents(count int) []x11ButtonEvent {
	h.t.Helper()
	events := make([]x11ButtonEvent, 0, count)
	for len(events) < count {
		event := h.waitForEvent("X11 button event", func(event xgb.Event) bool {
			switch event.(type) {
			case xproto.ButtonPressEvent, xproto.ButtonReleaseEvent:
				return true
			default:
				return false
			}
		})
		switch value := event.(type) {
		case xproto.ButtonPressEvent:
			events = append(events, x11ButtonEvent{pressed: true, button: value.Detail})
		case xproto.ButtonReleaseEvent:
			events = append(events, x11ButtonEvent{button: value.Detail})
		}
	}
	return events
}

func (h *x11InputHarness) queryPointer() (int, int) {
	h.t.Helper()
	reply, err := xproto.QueryPointer(h.conn, h.root).Reply()
	if err != nil {
		h.t.Fatalf("query X11 pointer: %v", err)
	}
	return int(reply.RootX), int(reply.RootY)
}

func (h *x11InputHarness) keysym(keycode xproto.Keycode) xproto.Keysym {
	h.t.Helper()
	reply, err := xproto.GetKeyboardMapping(h.conn, keycode, 1).Reply()
	if err != nil {
		h.t.Fatalf("query mapping for X11 keycode %d: %v", keycode, err)
	}
	if len(reply.Keysyms) == 0 {
		h.t.Fatalf("X11 keycode %d has no keysyms", keycode)
	}
	return reply.Keysyms[0]
}

func TestPureGoX11Capabilities(t *testing.T) {
	newX11InputHarness(t)

	capabilities := robotgo.GetRuntimeCapabilities()
	if capabilities.Runtime.CGOEnabled {
		t.Fatal("X11 integration suite must run with CGO_ENABLED=0")
	}
	if capabilities.Runtime.BuildImplementation != robotgo.RuntimeImplementationPureGo {
		t.Fatalf("build implementation = %q, want %q", capabilities.Runtime.BuildImplementation, robotgo.RuntimeImplementationPureGo)
	}
	if capabilities.Runtime.DisplayServer != robotgo.DisplayServerX11 {
		t.Fatalf("display server = %q, want %q", capabilities.Runtime.DisplayServer, robotgo.DisplayServerX11)
	}
	for name, capability := range map[string]robotgo.FeatureCapability{
		"keyboard": capabilities.Keyboard,
		"mouse":    capabilities.Mouse,
	} {
		if !capability.Available {
			t.Errorf("%s capability unavailable: reason=%q notes=%q", name, capability.Reason, capability.Notes)
		}
		if capability.Backend != "pure-go-x11" {
			t.Errorf("%s backend = %q, want pure-go-x11", name, capability.Backend)
		}
	}
	if err := robotgo.KeyboardReady(); err != nil {
		t.Errorf("KeyboardReady: %v", err)
	}
	if err := robotgo.MouseReady(); err != nil {
		t.Errorf("MouseReady: %v", err)
	}
}

func TestPureGoX11PointerInput(t *testing.T) {
	harness := newX11InputHarness(t)
	const targetX, targetY = 180, 170
	if err := robotgo.MoveE(targetX, targetY); err != nil {
		t.Fatalf("MoveE: %v", err)
	}
	harness.waitForEvent("absolute pointer motion", func(event xgb.Event) bool {
		motion, ok := event.(xproto.MotionNotifyEvent)
		return ok && int(motion.RootX) == targetX && int(motion.RootY) == targetY
	})
	assertPointerLocation(t, harness, targetX, targetY)

	const deltaX, deltaY = 17, 13
	if err := robotgo.MoveRelativeE(deltaX, deltaY); err != nil {
		t.Fatalf("MoveRelativeE: %v", err)
	}
	harness.waitForEvent("relative pointer motion", func(event xgb.Event) bool {
		motion, ok := event.(xproto.MotionNotifyEvent)
		return ok && int(motion.RootX) == targetX+deltaX && int(motion.RootY) == targetY+deltaY
	})
	assertPointerLocation(t, harness, targetX+deltaX, targetY+deltaY)

	if err := robotgo.ClickE("left"); err != nil {
		t.Fatalf("ClickE: %v", err)
	}
	if got, want := harness.waitForButtonEvents(2), []x11ButtonEvent{
		{pressed: true, button: x11ButtonLeft},
		{button: x11ButtonLeft},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("left-click events = %+v, want %+v", got, want)
	}

	if err := robotgo.Toggle("right", "down"); err != nil {
		t.Fatalf("Toggle right down: %v", err)
	}
	t.Cleanup(func() { _ = robotgo.Toggle("right", "up") })
	if got, want := harness.waitForButtonEvents(1), []x11ButtonEvent{{pressed: true, button: x11ButtonRight}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("right-button down events = %+v, want %+v", got, want)
	}
	if err := robotgo.Toggle("right", "up"); err != nil {
		t.Fatalf("Toggle right up: %v", err)
	}
	if got, want := harness.waitForButtonEvents(1), []x11ButtonEvent{{button: x11ButtonRight}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("right-button up events = %+v, want %+v", got, want)
	}

	if err := robotgo.ScrollE(1, 1, 0); err != nil {
		t.Fatalf("ScrollE: %v", err)
	}
	if got, want := harness.waitForButtonEvents(4), []x11ButtonEvent{
		{pressed: true, button: x11ButtonWheelRight},
		{button: x11ButtonWheelRight},
		{pressed: true, button: x11ButtonWheelUp},
		{button: x11ButtonWheelUp},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scroll events = %+v, want %+v", got, want)
	}
}

func TestPureGoX11KeyboardInput(t *testing.T) {
	harness := newX11InputHarness(t)
	if err := robotgo.KeyTap("a"); err != nil {
		t.Fatalf("KeyTap: %v", err)
	}
	press := harness.waitForKeyPress("KeyTap press")
	release := harness.waitForKeyRelease("KeyTap release")
	if press.Detail != release.Detail {
		t.Fatalf("KeyTap keycodes differ: press=%d release=%d", press.Detail, release.Detail)
	}
	if got := harness.keysym(press.Detail); got != x11KeysymA {
		t.Fatalf("KeyTap keysym = %#x, want %#x (a)", got, x11KeysymA)
	}

	if err := robotgo.KeyToggle("shift", "down"); err != nil {
		t.Fatalf("KeyToggle shift down: %v", err)
	}
	t.Cleanup(func() { _ = robotgo.KeyToggle("shift", "up") })
	shiftPress := harness.waitForKeyPress("shift press")
	if got := harness.keysym(shiftPress.Detail); got != x11KeysymShiftL && got != x11KeysymShiftR {
		t.Fatalf("shift key keysym = %#x, want %#x or %#x", got, x11KeysymShiftL, x11KeysymShiftR)
	}
	if err := robotgo.KeyToggle("shift", "up"); err != nil {
		t.Fatalf("KeyToggle shift up: %v", err)
	}
	shiftRelease := harness.waitForKeyRelease("shift release")
	if shiftRelease.Detail != shiftPress.Detail {
		t.Fatalf("shift keycodes differ: press=%d release=%d", shiftPress.Detail, shiftRelease.Detail)
	}

	if err := robotgo.TypeStrE("az"); err != nil {
		t.Fatalf("TypeStrE: %v", err)
	}
	for index, want := range []xproto.Keysym{x11KeysymA, x11KeysymZ} {
		press := harness.waitForKeyPress(fmt.Sprintf("TypeStrE character %d press", index))
		release := harness.waitForKeyRelease(fmt.Sprintf("TypeStrE character %d release", index))
		if press.Detail != release.Detail {
			t.Fatalf("TypeStrE character %d keycodes differ: press=%d release=%d", index, press.Detail, release.Detail)
		}
		if got := harness.keysym(press.Detail); got != want {
			t.Fatalf("TypeStrE character %d keysym = %#x, want %#x", index, got, want)
		}
	}
}

func TestPureGoX11CloseMainDisplayReconnects(t *testing.T) {
	harness := newX11InputHarness(t)
	robotgo.CloseMainDisplay()

	if err := robotgo.KeyboardReady(); err != nil {
		t.Fatalf("KeyboardReady after CloseMainDisplay: %v", err)
	}
	if err := robotgo.MouseReady(); err != nil {
		t.Fatalf("MouseReady after CloseMainDisplay: %v", err)
	}
	const targetX, targetY = 230, 210
	if err := robotgo.MoveE(targetX, targetY); err != nil {
		t.Fatalf("MoveE after CloseMainDisplay: %v", err)
	}
	harness.waitForEvent("pointer motion after reconnect", func(event xgb.Event) bool {
		motion, ok := event.(xproto.MotionNotifyEvent)
		return ok && int(motion.RootX) == targetX && int(motion.RootY) == targetY
	})
	assertPointerLocation(t, harness, targetX, targetY)

	if err := robotgo.KeyTap("a"); err != nil {
		t.Fatalf("KeyTap after CloseMainDisplay: %v", err)
	}
	press := harness.waitForKeyPress("key press after reconnect")
	release := harness.waitForKeyRelease("key release after reconnect")
	if press.Detail != release.Detail {
		t.Fatalf("reconnected KeyTap keycodes differ: press=%d release=%d", press.Detail, release.Detail)
	}
}

func assertPointerLocation(t *testing.T, harness *x11InputHarness, wantX, wantY int) {
	t.Helper()
	x, y, err := robotgo.LocationE()
	if err != nil {
		t.Fatalf("LocationE: %v", err)
	}
	if x != wantX || y != wantY {
		t.Errorf("LocationE = (%d, %d), want (%d, %d)", x, y, wantX, wantY)
	}
	if x, y := harness.queryPointer(); x != wantX || y != wantY {
		t.Errorf("independent X11 pointer query = (%d, %d), want (%d, %d)", x, y, wantX, wantY)
	}
}
