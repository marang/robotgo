//go:build linux && !cgo && x11integration

package robotgo_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
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
	x11RequiredEnv  = "ROBOTGO_REQUIRE_X11_INTEGRATION"

	x11WindowX      = 100
	x11WindowY      = 100
	x11WindowWidth  = 400
	x11WindowHeight = 300

	x11ButtonLeft      = 1
	x11ButtonRight     = 3
	x11ButtonWheelUp   = 4
	x11ButtonWheelLeft = 6

	x11KeysymEnter  = 0xff0d
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
		x11Unavailable(t, "X11 integration test requires DISPLAY")
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		x11Unavailable(t, "X11 integration test requires an X11-primary session; unset WAYLAND_DISPLAY")
	}
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		x11Unavailable(t, "X11 integration test cannot connect to DISPLAY %q: %v", display, err)
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		x11Unavailable(t, "X11 integration test requires the XTEST extension: %v", err)
	}
	if _, err := xtest.GetVersion(conn, 2, 2).Reply(); err != nil {
		conn.Close()
		x11Unavailable(t, "X11 integration test cannot query the XTEST extension: %v", err)
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
	t.Cleanup(func() {
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Errorf("CloseMainDisplayE cleanup: %v", err)
		}
	})
	return harness
}

func x11Unavailable(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	if os.Getenv(x11RequiredEnv) == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func TestPureGoX11MultiLayoutConfiguration(t *testing.T) {
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		x11Unavailable(t, "multi-layout integration test requires an X11-primary DISPLAY")
	}
	path, err := exec.LookPath("setxkbmap")
	if err != nil {
		x11Unavailable(t, "multi-layout integration test requires setxkbmap: %v", err)
	}
	output, err := exec.Command(path, "-query").CombinedOutput()
	if err != nil {
		x11Unavailable(t, "query active X11 keymap: %v: %s", err, strings.TrimSpace(string(output)))
	}
	for _, line := range strings.Split(string(output), "\n") {
		name, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(name) == "layout" && strings.TrimSpace(value) == "us,de" {
			return
		}
	}
	x11Unavailable(t, "active X11 keymap is not the required us,de configuration: %s", strings.TrimSpace(string(output)))
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

func (h *x11InputHarness) assertNoMatchingEvent(description string, duration time.Duration, match func(xgb.Event) bool) {
	h.t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		event, err := h.conn.PollForEvent()
		if err != nil {
			h.t.Fatalf("poll while checking for unexpected %s: %v", description, err)
		}
		if event != nil && match(event) {
			h.t.Fatalf("received unexpected %s: %T %+v", description, event, event)
		}
		time.Sleep(x11PollInterval)
	}
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

func (h *x11InputHarness) keyPressed(keycode xproto.Keycode) bool {
	h.t.Helper()
	reply, err := xproto.QueryKeymap(h.conn).Reply()
	if err != nil {
		h.t.Fatalf("query X11 pressed keys: %v", err)
	}
	return reply.Keys[int(keycode)/8]&(1<<uint(keycode%8)) != 0
}

func (h *x11InputHarness) fakeKey(keycode xproto.Keycode, down bool) {
	h.t.Helper()
	eventType := byte(xproto.KeyRelease)
	if down {
		eventType = byte(xproto.KeyPress)
	}
	if err := xtest.FakeInputChecked(h.conn, eventType, byte(keycode), 0, h.root, 0, 0, 0).Check(); err != nil {
		h.t.Fatalf("inject independent X11 key state: %v", err)
	}
}

func (h *x11InputHarness) fakeButton(button byte, down bool) {
	h.t.Helper()
	eventType := byte(xproto.ButtonRelease)
	if down {
		eventType = byte(xproto.ButtonPress)
	}
	if err := xtest.FakeInputChecked(h.conn, eventType, button, 0, h.root, 0, 0, 0).Check(); err != nil {
		h.t.Fatalf("inject independent X11 button state: %v", err)
	}
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

func (h *x11InputHarness) keysyms(keycode xproto.Keycode) []xproto.Keysym {
	h.t.Helper()
	reply, err := xproto.GetKeyboardMapping(h.conn, keycode, 1).Reply()
	if err != nil {
		h.t.Fatalf("query mapping for X11 keycode %d: %v", keycode, err)
	}
	return append([]xproto.Keysym(nil), reply.Keysyms...)
}

func (h *x11InputHarness) findKeycode(keysym uint32) (xproto.Keycode, []xproto.Keysym) {
	h.t.Helper()
	setup := xproto.Setup(h.conn)
	if setup == nil {
		h.t.Fatal("X11 connection has no setup while finding a keysym")
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	reply, err := xproto.GetKeyboardMapping(h.conn, setup.MinKeycode, byte(count)).Reply()
	if err != nil || reply == nil || reply.KeysymsPerKeycode == 0 {
		h.t.Fatalf("query X11 keymap while finding keysym %#x: reply=%+v err=%v", keysym, reply, err)
	}
	per := int(reply.KeysymsPerKeycode)
	for offset := 0; offset+per <= len(reply.Keysyms); offset += per {
		for _, value := range reply.Keysyms[offset : offset+per] {
			if uint32(value) == keysym {
				mapping := append([]xproto.Keysym(nil), reply.Keysyms[offset:offset+per]...)
				return setup.MinKeycode + xproto.Keycode(offset/per), mapping
			}
		}
	}
	h.t.Fatalf("X11 keymap does not contain keysym %#x", keysym)
	return 0, nil
}

func (h *x11InputHarness) findEmptyNonModifierKeycode() (xproto.Keycode, []xproto.Keysym) {
	h.t.Helper()
	setup := xproto.Setup(h.conn)
	if setup == nil {
		h.t.Fatal("X11 connection has no setup while finding an empty keycode")
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	reply, err := xproto.GetKeyboardMapping(h.conn, setup.MinKeycode, byte(count)).Reply()
	if err != nil || reply == nil || reply.KeysymsPerKeycode == 0 {
		h.t.Fatalf("query X11 keymap while finding empty keycode: reply=%+v err=%v", reply, err)
	}
	modifierReply, err := xproto.GetModifierMapping(h.conn).Reply()
	if err != nil || modifierReply == nil {
		h.t.Fatalf("query X11 modifier map while finding empty keycode: reply=%+v err=%v", modifierReply, err)
	}
	modifiers := make(map[xproto.Keycode]struct{})
	for _, code := range modifierReply.Keycodes {
		if code != 0 {
			modifiers[code] = struct{}{}
		}
	}
	per := int(reply.KeysymsPerKeycode)
	for offset := 0; offset+per <= len(reply.Keysyms); offset += per {
		code := setup.MinKeycode + xproto.Keycode(offset/per)
		if _, modifier := modifiers[code]; modifier {
			continue
		}
		mapping := reply.Keysyms[offset : offset+per]
		empty := true
		for _, keysym := range mapping {
			if keysym != 0 {
				empty = false
				break
			}
		}
		if empty {
			return code, append([]xproto.Keysym(nil), mapping...)
		}
	}
	h.t.Fatal("X11 keymap has no empty non-modifier keycode")
	return 0, nil
}

func (h *x11InputHarness) modifierMapping() (byte, []xproto.Keycode) {
	h.t.Helper()
	reply, err := xproto.GetModifierMapping(h.conn).Reply()
	if err != nil || reply == nil || reply.KeycodesPerModifier == 0 {
		h.t.Fatalf("query X11 modifier map: reply=%+v err=%v", reply, err)
	}
	return reply.KeycodesPerModifier, append([]xproto.Keycode(nil), reply.Keycodes...)
}

func (h *x11InputHarness) setModifierMapping(per byte, keycodes []xproto.Keycode) {
	h.t.Helper()
	reply, err := xproto.SetModifierMapping(h.conn, per, keycodes).Reply()
	if err != nil || reply == nil || reply.Status != xproto.MappingStatusSuccess {
		h.t.Fatalf("set X11 modifier map: reply=%+v err=%v", reply, err)
	}
}

func (h *x11InputHarness) keymapContains(keysym uint32) bool {
	h.t.Helper()
	setup := xproto.Setup(h.conn)
	if setup == nil {
		h.t.Fatal("X11 connection has no setup while scanning the keymap")
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	reply, err := xproto.GetKeyboardMapping(h.conn, setup.MinKeycode, byte(count)).Reply()
	if err != nil || reply == nil {
		h.t.Fatalf("scan X11 keymap: reply=%+v err=%v", reply, err)
	}
	for _, value := range reply.Keysyms {
		if uint32(value) == keysym {
			return true
		}
	}
	return false
}

func (h *x11InputHarness) rootChildren() map[xproto.Window]struct{} {
	h.t.Helper()
	reply, err := xproto.QueryTree(h.conn, h.root).Reply()
	if err != nil {
		h.t.Fatalf("query X11 root children: %v", err)
	}
	children := make(map[xproto.Window]struct{}, len(reply.Children))
	for _, child := range reply.Children {
		children[child] = struct{}{}
	}
	return children
}

func (h *x11InputHarness) waitForNewRootChild(previous map[xproto.Window]struct{}) xproto.Window {
	h.t.Helper()
	deadline := time.Now().Add(x11EventTimeout)
	for time.Now().Before(deadline) {
		for child := range h.rootChildren() {
			if _, existed := previous[child]; !existed {
				return child
			}
		}
		time.Sleep(x11PollInterval)
	}
	h.t.Fatalf("timed out after %s waiting for XKB oracle window", x11EventTimeout)
	return 0
}

func startXKBOracle(t *testing.T, harness *x11InputHarness) (xproto.Window, <-chan string, *os.Process) {
	t.Helper()
	path, err := exec.LookPath("xkbcli")
	if err != nil {
		x11Unavailable(t, "X11 text integration requires xkbcli: %v", err)
	}
	stdbufPath, err := exec.LookPath("stdbuf")
	if err != nil {
		x11Unavailable(t, "X11 text integration requires stdbuf: %v", err)
	}
	previousChildren := harness.rootChildren()
	command := exec.Command(stdbufPath, "-oL", path, "interactive-x11", "--uniline")
	command.Env = os.Environ()
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("create xkbcli stdout pipe: %v", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		x11Unavailable(t, "start xkbcli X11 oracle: %v", err)
	}
	lines := make(chan string, 32)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGCONT)
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	window := harness.waitForNewRootChild(previousChildren)
	if err := xproto.SetInputFocusChecked(
		harness.conn, xproto.InputFocusPointerRoot, window, xproto.TimeCurrentTime,
	).Check(); err != nil {
		t.Fatalf("focus xkbcli oracle window: %v", err)
	}
	harness.conn.Sync()
	return window, lines, command.Process
}

func waitForProcessStopped(t *testing.T, process *os.Process) {
	t.Helper()
	statusPath := fmt.Sprintf("/proc/%d/status", process.Pid)
	deadline := time.Now().Add(x11EventTimeout)
	for time.Now().Before(deadline) {
		status, err := os.ReadFile(statusPath)
		if err != nil {
			t.Fatalf("read XKB oracle process status: %v", err)
		}
		if strings.Contains(string(status), "\nState:\tT") {
			return
		}
		time.Sleep(x11PollInterval)
	}
	t.Fatalf("XKB oracle process %d did not stop", process.Pid)
}

func waitForXKBOracleText(t *testing.T, lines <-chan string, count int) []rune {
	t.Helper()
	result := make([]rune, 0, count)
	observed := make([]string, 0, count*2)
	deadline := time.NewTimer(x11EventTimeout)
	defer deadline.Stop()
	for len(result) < count {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("xkbcli oracle exited after text %q", string(result))
			}
			observed = append(observed, line)
			if !strings.Contains(line, "down") {
				continue
			}
			const unicodeMarker = "unicode [ "
			unicodeIndex := strings.Index(line, unicodeMarker)
			if unicodeIndex < 0 {
				continue
			}
			unicodeText := line[unicodeIndex+len(unicodeMarker):]
			end := strings.Index(unicodeText, " ]")
			if end < 0 {
				continue
			}
			unicodeText = strings.TrimSpace(unicodeText[:end])
			if strings.HasPrefix(unicodeText, "U+") {
				var value uint32
				if _, err := fmt.Sscanf(unicodeText, "U+%X", &value); err == nil {
					result = append(result, rune(value))
				}
				continue
			}
			if values := []rune(unicodeText); len(values) == 1 {
				result = append(result, values[0])
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for %d XKB characters; got %q from lines %q", count, string(result), observed)
		}
	}
	return result
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
	if err := robotgo.Toggle("wheelLeft", "down"); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("persistent horizontal-wheel toggle error = %v, want ErrNotSupported", err)
	}
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

	const smoothX, smoothY = 260, 240
	if !robotgo.MoveSmooth(smoothX, smoothY, 0.0, 0.0, 0) {
		t.Fatal("MoveSmooth returned false")
	}
	harness.waitForEvent("final smooth pointer motion", func(event xgb.Event) bool {
		motion, ok := event.(xproto.MotionNotifyEvent)
		return ok && int(motion.RootX) == smoothX && int(motion.RootY) == smoothY
	})
	assertPointerLocation(t, harness, smoothX, smoothY)

	const dragX, dragY = 300, 270
	robotgo.DragSmooth(dragX, dragY, 0.0, 0.0, 0)
	if got, want := harness.waitForButtonEvents(2), []x11ButtonEvent{
		{pressed: true, button: x11ButtonLeft},
		{button: x11ButtonLeft},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("drag button events = %+v, want %+v", got, want)
	}
	assertPointerLocation(t, harness, dragX, dragY)

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
		{pressed: true, button: x11ButtonWheelLeft},
		{button: x11ButtonWheelLeft},
		{pressed: true, button: x11ButtonWheelUp},
		{button: x11ButtonWheelUp},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scroll events = %+v, want %+v", got, want)
	}
	if err := robotgo.Toggle("wheelUp", "down"); err != nil {
		t.Fatalf("hold wheel button before ScrollE: %v", err)
	}
	harness.waitForButtonEvents(1)
	if err := robotgo.ScrollE(0, 1, 0); err == nil || errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("ScrollE over RobotGo-held wheel button error = %v, want state error", err)
	}
	if reply, err := xproto.QueryPointer(harness.conn, harness.root).Reply(); err != nil {
		t.Fatalf("query owned wheel-button state: %v", err)
	} else if reply.Mask&xproto.ButtonMask4 == 0 {
		t.Fatal("ScrollE released the RobotGo-held wheel button")
	}
	if err := robotgo.Toggle("wheelUp", "up"); err != nil {
		t.Fatalf("release held wheel button after ScrollE: %v", err)
	}
	harness.waitForButtonEvents(1)
}

func TestPureGoX11KeyboardInput(t *testing.T) {
	harness := newX11InputHarness(t)
	if err := robotgo.KeyTap("enter"); err != nil {
		t.Fatalf("KeyTap: %v", err)
	}
	press := harness.waitForKeyPress("KeyTap press")
	release := harness.waitForKeyRelease("KeyTap release")
	if press.Detail != release.Detail {
		t.Fatalf("KeyTap keycodes differ: press=%d release=%d", press.Detail, release.Detail)
	}
	if got := harness.keysym(press.Detail); got != x11KeysymEnter {
		t.Fatalf("KeyTap keysym = %#x, want %#x (Enter)", got, x11KeysymEnter)
	}

	if err := robotgo.KeyTap("A", "ctrl"); err != nil {
		t.Fatalf("uppercase KeyTap with modifier: %v", err)
	}
	uppercase := harness.waitForEvent("uppercase KeyTap main press", func(event xgb.Event) bool {
		press, ok := event.(xproto.KeyPressEvent)
		return ok && uint32(harness.keysym(press.Detail)) == 'A'
	}).(xproto.KeyPressEvent)
	if want := uint16(xproto.ModMaskControl | xproto.ModMaskShift); uppercase.State&want != want {
		t.Fatalf("uppercase KeyTap state = %#x, want Ctrl+Shift bits %#x", uppercase.State, want)
	}
	harness.drainEvents()

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
	if err := robotgo.KeyToggle("a", "down"); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("persistent literal KeyToggle error = %v, want ErrNotSupported", err)
	}
	if err := robotgo.KeyTap("enter", "y"); err == nil || errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("literal modifier KeyTap error = %v, want argument-validation error", err)
	}

	restoreDelay := robotgo.SetX11KeyHoldDelayForIntegrationTest(150 * time.Millisecond)
	defer restoreDelay()
	text := "Aä€😀"
	typeDone := make(chan error, 1)
	go func() { typeDone <- robotgo.TypeStrE(text) }()
	var scratchCode xproto.Keycode
	scratchCodes := make(map[xproto.Keycode]struct{})
	for index, value := range []rune(text) {
		press := harness.waitForKeyPress(fmt.Sprintf("TypeStrE character %d press", index))
		scratchCode = press.Detail
		scratchCodes[press.Detail] = struct{}{}
		want := x11KeysymForRune(value)
		for column, got := range harness.keysyms(press.Detail) {
			if column < 4 && uint32(got) != want {
				t.Fatalf("TypeStrE character %d column %d keysym = %#x, want %#x", index, column, got, want)
			}
			if column >= 4 && got != 0 && uint32(got) != want {
				t.Fatalf("TypeStrE character %d column %d has unexpected keysym %#x", index, column, got)
			}
		}
		release := harness.waitForKeyRelease(fmt.Sprintf("TypeStrE character %d release", index))
		if press.Detail != release.Detail {
			t.Fatalf("TypeStrE character %d keycodes differ: press=%d release=%d", index, press.Detail, release.Detail)
		}
	}
	if err := <-typeDone; err != nil {
		t.Fatalf("TypeStrE: %v", err)
	}
	if got, want := uint32(harness.keysym(scratchCode)), x11KeysymForRune('😀'); got != want {
		t.Fatalf("stable Unicode key mapping = %#x, want %#x before cleanup", got, want)
	}
	modifierMap, err := xproto.GetModifierMapping(harness.conn).Reply()
	if err != nil || modifierMap == nil {
		t.Fatalf("query modifier map after Unicode input: reply=%+v err=%v", modifierMap, err)
	}
	for _, code := range modifierMap.Keycodes {
		if _, scratch := scratchCodes[code]; scratch && code != 0 {
			t.Fatalf("Unicode scratch keycode %d is also present in the X11 modifier map", code)
		}
	}
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE after Unicode input: %v", err)
	}
	if got := harness.keysym(scratchCode); got != 0 {
		t.Fatalf("Unicode key mapping was not restored by CloseMainDisplay: keysym=%#x", got)
	}
}

func TestPureGoX11TextReachesDelayedXKBClient(t *testing.T) {
	harness := newX11InputHarness(t)
	_, lines, process := startXKBOracle(t, harness)
	if err := process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatalf("stop XKB oracle: %v", err)
	}
	waitForProcessStopped(t, process)
	// The oracle must process MappingNotify and key events only after the
	// RobotGo transaction finishes; this catches mappings whose lifetime is too
	// short for a delayed target client.
	const text = "Aä€😀"
	if err := robotgo.TypeStrE(text); err != nil {
		t.Fatalf("TypeStrE while XKB oracle is stopped: %v", err)
	}
	if err := process.Signal(syscall.SIGCONT); err != nil {
		t.Fatalf("resume XKB oracle: %v", err)
	}
	if got := string(waitForXKBOracleText(t, lines, len([]rune(text)))); got != text {
		t.Fatalf("XKB oracle text = %q, want %q", got, text)
	}
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE after delayed XKB input: %v", err)
	}
}

func TestPureGoX11ExplicitShiftReachesXKBClient(t *testing.T) {
	harness := newX11InputHarness(t)
	_, lines, _ := startXKBOracle(t, harness)
	for _, test := range []struct {
		key       string
		modifiers []interface{}
		want      rune
	}{
		{key: "a", modifiers: []interface{}{"shift"}, want: 'A'},
		{key: "1", modifiers: []interface{}{"right_shift"}, want: '!'},
		{key: "+", want: '+'},
	} {
		if err := robotgo.KeyTap(test.key, test.modifiers...); err != nil {
			t.Fatalf("KeyTap(%q,%v): %v", test.key, test.modifiers, err)
		}
		if got := waitForXKBOracleText(t, lines, 1); len(got) != 1 || got[0] != test.want {
			t.Fatalf("XKB oracle KeyTap(%q,%v) = %q, want %q", test.key, test.modifiers, string(got), string(test.want))
		}
	}
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE after shifted literal input: %v", err)
	}
}

func TestPureGoX11ScratchReservationSkipsPressedEmptyKeycode(t *testing.T) {
	harness := newX11InputHarness(t)
	heldCode, original := harness.findEmptyNonModifierKeycode()
	harness.fakeKey(heldCode, true)
	harness.waitForKeyPress("foreign press on empty keycode")
	held := true
	t.Cleanup(func() {
		if held {
			harness.fakeKey(heldCode, false)
		}
		_ = xproto.ChangeKeyboardMappingChecked(
			harness.conn, 1, heldCode, byte(len(original)), original,
		).Check()
	})
	typeDone := make(chan error, 1)
	go func() { typeDone <- robotgo.TypeStrE("😀") }()
	press := harness.waitForKeyPress("text press with foreign empty keycode held")
	harness.waitForKeyRelease("text release with foreign empty keycode held")
	if err := <-typeDone; err != nil {
		t.Fatalf("TypeStrE with a pressed empty keycode: %v", err)
	}
	if press.Detail == heldCode {
		t.Fatalf("text reused foreign-held empty keycode %d", heldCode)
	}
	for column, keysym := range harness.keysyms(heldCode) {
		if keysym != 0 {
			t.Fatalf("foreign-held keycode %d column %d was mapped to %#x", heldCode, column, keysym)
		}
	}
	if !harness.keyPressed(heldCode) {
		t.Fatal("text input released the foreign-held empty keycode")
	}
	harness.fakeKey(heldCode, false)
	held = false
	harness.waitForKeyRelease("foreign empty-keycode release")
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE after pressed scratch exclusion: %v", err)
	}
}

func TestPureGoX11ScratchCleanupCanRetryAfterForeignRelease(t *testing.T) {
	harness := newX11InputHarness(t)
	typeDone := make(chan error, 1)
	go func() { typeDone <- robotgo.TypeStrE("😀") }()
	press := harness.waitForKeyPress("scratch press before cleanup conflict")
	harness.waitForKeyRelease("scratch release before cleanup conflict")
	if err := <-typeDone; err != nil {
		t.Fatalf("TypeStrE before cleanup conflict: %v", err)
	}
	harness.fakeKey(press.Detail, true)
	harness.waitForKeyPress("foreign scratch-code press")
	foreignHeld := true
	t.Cleanup(func() {
		if foreignHeld {
			harness.fakeKey(press.Detail, false)
		}
	})
	if err := robotgo.CloseMainDisplayE(); err == nil {
		t.Fatal("CloseMainDisplayE restored a foreign-held scratch keycode")
	}
	if got, want := uint32(harness.keysym(press.Detail)), x11KeysymForRune('😀'); got != want {
		t.Fatalf("mapping after rejected cleanup = %#x, want %#x", got, want)
	}
	harness.fakeKey(press.Detail, false)
	foreignHeld = false
	harness.waitForKeyRelease("foreign scratch-code release")
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("retry CloseMainDisplayE after foreign release: %v", err)
	}
	if got := harness.keysym(press.Detail); got != 0 {
		t.Fatalf("retry cleanup left scratch mapping %#x", got)
	}
}

func TestPureGoX11RejectsNonModifierBeforeScratchMutation(t *testing.T) {
	harness := newX11InputHarness(t)
	per, original := harness.modifierMapping()
	cleared := make([]xproto.Keycode, len(original))
	harness.setModifierMapping(per, cleared)
	restored := false
	t.Cleanup(func() {
		if !restored {
			harness.setModifierMapping(per, original)
		}
	})
	const value = '😀'
	if err := robotgo.KeyTap(string(value), "ctrl"); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("KeyTap with a keysym that is not in the modifier map = %v, want ErrNotSupported", err)
	}
	if harness.keymapContains(x11KeysymForRune(value)) {
		t.Fatal("failed modifier preflight still installed a Unicode scratch mapping")
	}
	harness.assertNoMatchingEvent("key input after modifier preflight failure", 100*time.Millisecond, func(event xgb.Event) bool {
		_, pressed := event.(xproto.KeyPressEvent)
		return pressed
	})
	harness.setModifierMapping(per, original)
	restored = true
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE after modifier preflight failure: %v", err)
	}
}

func TestPureGoX11ScratchCleanupCanRetryAfterModifierRestore(t *testing.T) {
	harness := newX11InputHarness(t)
	typeDone := make(chan error, 1)
	go func() { typeDone <- robotgo.TypeStrE("😀") }()
	press := harness.waitForKeyPress("scratch press before modifier conflict")
	harness.waitForKeyRelease("scratch release before modifier conflict")
	if err := <-typeDone; err != nil {
		t.Fatalf("TypeStrE before modifier cleanup conflict: %v", err)
	}
	per, original := harness.modifierMapping()
	modified := append([]xproto.Keycode(nil), original...)
	inserted := false
	for index, code := range modified {
		if code == 0 {
			modified[index] = press.Detail
			inserted = true
			break
		}
	}
	if !inserted {
		t.Fatal("X11 modifier map has no empty slot for cleanup-conflict test")
	}
	harness.setModifierMapping(per, modified)
	restored := false
	t.Cleanup(func() {
		if !restored {
			harness.setModifierMapping(per, original)
		}
	})
	if err := robotgo.CloseMainDisplayE(); err == nil {
		t.Fatal("CloseMainDisplayE restored a scratch keycode that became a modifier")
	}
	if got, want := uint32(harness.keysym(press.Detail)), x11KeysymForRune('😀'); got != want {
		t.Fatalf("mapping after modifier cleanup conflict = %#x, want %#x", got, want)
	}
	harness.setModifierMapping(per, original)
	restored = true
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("retry CloseMainDisplayE after modifier restore: %v", err)
	}
	if got := harness.keysym(press.Detail); got != 0 {
		t.Fatalf("retry cleanup left modifier-conflicted scratch mapping %#x", got)
	}
}

func TestPureGoX11TextCapacityFailsBeforeInput(t *testing.T) {
	harness := newX11InputHarness(t)
	var text strings.Builder
	for value := rune(0x400); value < 0x500; value++ {
		text.WriteRune(value)
	}
	if err := robotgo.TypeStrE(text.String()); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("oversized distinct-text error = %v, want ErrNotSupported", err)
	}
	harness.assertNoMatchingEvent("partial key input after scratch-capacity failure", 100*time.Millisecond, func(event xgb.Event) bool {
		_, pressed := event.(xproto.KeyPressEvent)
		return pressed
	})
}

func TestPureGoX11RejectsScratchReplacementBeforeTextTap(t *testing.T) {
	harness := newX11InputHarness(t)
	const value = '😀'
	keysym := x11KeysymForRune(value)
	var scratchCode xproto.Keycode
	var empty []xproto.Keysym
	restoreHook := robotgo.SetX11BeforeTextTapHookForIntegrationTest(func() {
		code, mapping := harness.findKeycode(keysym)
		scratchCode = code
		empty = make([]xproto.Keysym, len(mapping))
		foreign := make([]xproto.Keysym, len(mapping))
		for index := range foreign {
			foreign[index] = 'z'
		}
		if err := xproto.ChangeKeyboardMappingChecked(
			harness.conn, 1, code, byte(len(foreign)), foreign,
		).Check(); err != nil {
			t.Fatalf("replace scratch mapping before text tap: %v", err)
		}
	})
	defer restoreHook()
	t.Cleanup(func() {
		if scratchCode != 0 {
			_ = xproto.ChangeKeyboardMappingChecked(
				harness.conn, 1, scratchCode, byte(len(empty)), empty,
			).Check()
		}
	})
	if err := robotgo.TypeStrE(string(value)); err == nil || errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("TypeStrE after foreign scratch replacement error = %v, want ownership error", err)
	}
	harness.assertNoMatchingEvent("key input from a stale scratch mapping", 100*time.Millisecond, func(event xgb.Event) bool {
		_, pressed := event.(xproto.KeyPressEvent)
		return pressed
	})
	if got := harness.keysym(scratchCode); got != 'z' {
		t.Fatalf("cleanup overwrote adversarial scratch replacement with keysym %#x", got)
	}
}

func TestPureGoX11ClosePreservesForeignScratchReplacement(t *testing.T) {
	harness := newX11InputHarness(t)
	restoreDelay := robotgo.SetX11KeyHoldDelayForIntegrationTest(150 * time.Millisecond)
	defer restoreDelay()
	typeDone := make(chan error, 1)
	go func() { typeDone <- robotgo.TypeStrE("😀") }()
	press := harness.waitForKeyPress("scratch ownership key press")
	harness.waitForKeyRelease("scratch ownership key release")
	if err := <-typeDone; err != nil {
		t.Fatalf("TypeStrE before foreign scratch replacement: %v", err)
	}
	current := harness.keysyms(press.Detail)
	foreign := make([]xproto.Keysym, len(current))
	for index := range foreign {
		foreign[index] = 'z'
	}
	empty := make([]xproto.Keysym, len(current))
	t.Cleanup(func() {
		_ = xproto.ChangeKeyboardMappingChecked(
			harness.conn, 1, press.Detail, byte(len(empty)), empty,
		).Check()
	})
	if err := xproto.ChangeKeyboardMappingChecked(
		harness.conn, 1, press.Detail, byte(len(foreign)), foreign,
	).Check(); err != nil {
		t.Fatalf("install foreign scratch replacement: %v", err)
	}
	harness.fakeKey(press.Detail, true)
	harness.waitForKeyPress("foreign replacement press")
	foreignHeld := true
	t.Cleanup(func() {
		if foreignHeld {
			harness.fakeKey(press.Detail, false)
		}
	})
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE after pressed foreign replacement: %v", err)
	}
	if got := harness.keysym(press.Detail); got != 'z' {
		t.Fatalf("CloseMainDisplay overwrote foreign scratch mapping with keysym %#x", got)
	}
	if !harness.keyPressed(press.Detail) {
		t.Fatal("CloseMainDisplay released a pressed foreign scratch replacement")
	}
	harness.fakeKey(press.Detail, false)
	foreignHeld = false
	harness.waitForKeyRelease("foreign replacement release")
}

func TestPureGoX11ClosePreservesForeignModifierScratchReplacement(t *testing.T) {
	harness := newX11InputHarness(t)
	typeDone := make(chan error, 1)
	go func() { typeDone <- robotgo.TypeStrE("😀") }()
	press := harness.waitForKeyPress("scratch press before foreign modifier replacement")
	harness.waitForKeyRelease("scratch release before foreign modifier replacement")
	if err := <-typeDone; err != nil {
		t.Fatalf("TypeStrE before foreign modifier replacement: %v", err)
	}
	current := harness.keysyms(press.Detail)
	foreign := make([]xproto.Keysym, len(current))
	for index := range foreign {
		foreign[index] = 'z'
	}
	empty := make([]xproto.Keysym, len(current))
	t.Cleanup(func() {
		_ = xproto.ChangeKeyboardMappingChecked(
			harness.conn, 1, press.Detail, byte(len(empty)), empty,
		).Check()
	})
	if err := xproto.ChangeKeyboardMappingChecked(
		harness.conn, 1, press.Detail, byte(len(foreign)), foreign,
	).Check(); err != nil {
		t.Fatalf("install foreign modifier scratch replacement: %v", err)
	}
	per, original := harness.modifierMapping()
	modified := append([]xproto.Keycode(nil), original...)
	inserted := false
	for index, code := range modified {
		if code == 0 {
			modified[index] = press.Detail
			inserted = true
			break
		}
	}
	if !inserted {
		t.Fatal("X11 modifier map has no empty slot for foreign replacement test")
	}
	harness.setModifierMapping(per, modified)
	restored := false
	t.Cleanup(func() {
		if !restored {
			harness.setModifierMapping(per, original)
		}
	})
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE after foreign modifier replacement: %v", err)
	}
	if got := harness.keysym(press.Detail); got != 'z' {
		t.Fatalf("CloseMainDisplay overwrote foreign modifier mapping with keysym %#x", got)
	}
	harness.setModifierMapping(per, original)
	restored = true
}

func TestPureGoX11PreservesForeignInputState(t *testing.T) {
	harness := newX11InputHarness(t)
	if err := robotgo.KeyToggle("shift", "down"); err != nil {
		t.Fatalf("discover shift keycode: %v", err)
	}
	shift := harness.waitForKeyPress("RobotGo shift press").Detail
	if err := robotgo.KeyToggle("shift", "up"); err != nil {
		t.Fatalf("release RobotGo shift: %v", err)
	}
	harness.waitForKeyRelease("RobotGo shift release")

	harness.fakeKey(shift, true)
	harness.waitForKeyPress("independent shift press")
	if err := robotgo.KeyTap("enter", "shift"); err != nil {
		t.Fatalf("KeyTap with foreign Shift: %v", err)
	}
	harness.waitForKeyPress("Enter press with foreign Shift")
	harness.waitForKeyRelease("Enter release with foreign Shift")
	if !harness.keyPressed(shift) {
		t.Fatal("RobotGo released a Shift key held by another X11 client")
	}
	harness.fakeKey(shift, false)
	harness.waitForKeyRelease("independent shift release")

	harness.fakeButton(x11ButtonRight, true)
	harness.waitForButtonEvents(1)
	err := robotgo.ClickE("right")
	if err == nil || errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("ClickE over foreign-held button error = %v, want state error", err)
	}
	if reply, queryErr := xproto.QueryPointer(harness.conn, harness.root).Reply(); queryErr != nil {
		t.Fatalf("query foreign button state: %v", queryErr)
	} else if reply.Mask&xproto.ButtonMask3 == 0 {
		t.Fatal("RobotGo released a pointer button held by another X11 client")
	}
	harness.fakeButton(x11ButtonRight, false)
	harness.waitForButtonEvents(1)

	harness.fakeButton(x11ButtonWheelUp, true)
	harness.waitForButtonEvents(1)
	if err := robotgo.ScrollE(0, 1, 0); err == nil || errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("ScrollE over foreign-held wheel button error = %v, want state error", err)
	}
	if reply, queryErr := xproto.QueryPointer(harness.conn, harness.root).Reply(); queryErr != nil {
		t.Fatalf("query foreign wheel-button state: %v", queryErr)
	} else if reply.Mask&xproto.ButtonMask4 == 0 {
		t.Fatal("ScrollE released a wheel button held by another X11 client")
	}
	harness.fakeButton(x11ButtonWheelUp, false)
	harness.waitForButtonEvents(1)

	doubleClickDone := make(chan error, 1)
	go func() { doubleClickDone <- robotgo.ClickE("right", true) }()
	if got, want := harness.waitForButtonEvents(2), []x11ButtonEvent{
		{pressed: true, button: x11ButtonRight},
		{button: x11ButtonRight},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first half of double click = %+v, want %+v", got, want)
	}
	harness.fakeButton(x11ButtonRight, true)
	harness.waitForButtonEvents(1)
	if err := <-doubleClickDone; err == nil || errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("double click over newly foreign-held button error = %v, want state error", err)
	}
	if reply, queryErr := xproto.QueryPointer(harness.conn, harness.root).Reply(); queryErr != nil {
		t.Fatalf("query button state after rejected double click: %v", queryErr)
	} else if reply.Mask&xproto.ButtonMask3 == 0 {
		t.Fatal("second half of double click released a newly foreign-held button")
	}
	harness.fakeButton(x11ButtonRight, false)
	harness.waitForButtonEvents(1)
}

func x11KeysymForRune(value rune) uint32 {
	if value >= 0x20 && value <= 0x7e || value >= 0xa0 && value <= 0xff {
		return uint32(value)
	}
	return 0x01000000 | uint32(value)
}

func TestPureGoX11EventDrainDoesNotStall(t *testing.T) {
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		x11Unavailable(t, "X11 event-drain integration test requires an X11-primary DISPLAY")
	}
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("initial CloseMainDisplayE: %v", err)
	}
	if err := robotgo.KeyboardReady(); err != nil {
		x11Unavailable(t, "X11 keyboard backend is unavailable: %v", err)
	}
	emitter, err := xgb.NewConnDisplay(os.Getenv("DISPLAY"))
	if err != nil {
		x11Unavailable(t, "connect X11 MappingNotify emitter: %v", err)
	}
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			event, eventErr := emitter.WaitForEvent()
			if event == nil && eventErr == nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		emitter.Close()
		select {
		case <-drainDone:
		case <-time.After(time.Second):
			t.Error("MappingNotify emitter drain did not stop")
		}
	})
	setup := xproto.Setup(emitter)
	if setup == nil {
		t.Fatal("MappingNotify emitter has no X11 setup")
	}
	mapping, err := xproto.GetKeyboardMapping(emitter, setup.MinKeycode, 1).Reply()
	if err != nil || mapping == nil || mapping.KeysymsPerKeycode == 0 {
		t.Fatalf("query mapping used for event-drain stress: reply=%+v err=%v", mapping, err)
	}
	for range 6001 {
		xproto.ChangeKeyboardMapping(
			emitter, 1, setup.MinKeycode, mapping.KeysymsPerKeycode, mapping.Keysyms,
		)
	}
	emitter.Sync()

	readyDone := make(chan error, 1)
	go func() { readyDone <- robotgo.KeyboardReady() }()
	select {
	case err := <-readyDone:
		if err != nil {
			t.Fatalf("keyboard readiness after MappingNotify stress failed: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("X11 input stalled after filling the XGB event buffer")
	}
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- robotgo.CloseMainDisplayE()
	}()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("CloseMainDisplayE after event stress: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("CloseMainDisplay stalled after long Unicode input")
	}
}

func TestPureGoX11CloseMainDisplayReconnects(t *testing.T) {
	harness := newX11InputHarness(t)
	if err := robotgo.KeyTap("shift"); err != nil {
		t.Fatalf("KeyTap before held-key cleanup test: %v", err)
	}
	harness.waitForKeyPress("historical key press")
	harness.waitForKeyRelease("historical key release")
	if err := robotgo.KeyToggle("shift", "down"); err != nil {
		t.Fatalf("KeyToggle before CloseMainDisplay: %v", err)
	}
	heldKey := harness.waitForKeyPress("held key before CloseMainDisplay").Detail
	if err := robotgo.KeyTap("shift"); err == nil || errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("KeyTap over RobotGo-held key error = %v, want state error", err)
	}
	if !harness.keyPressed(heldKey) {
		t.Fatal("failed KeyTap released an existing RobotGo-held key")
	}
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE owned-key cleanup: %v", err)
	}
	if release := harness.waitForKeyRelease("owned key cleanup on CloseMainDisplay"); release.Detail != heldKey {
		t.Fatalf("CloseMainDisplay released keycode %d, want %d", release.Detail, heldKey)
	}
	harness.assertNoMatchingEvent("duplicate owned key release", 100*time.Millisecond, func(event xgb.Event) bool {
		release, ok := event.(xproto.KeyReleaseEvent)
		return ok && release.Detail == heldKey
	})

	if err := robotgo.ClickE("right"); err != nil {
		t.Fatalf("ClickE before held-button cleanup test: %v", err)
	}
	harness.waitForButtonEvents(2)
	if err := robotgo.Toggle("right", "down"); err != nil {
		t.Fatalf("Toggle before CloseMainDisplay: %v", err)
	}
	harness.waitForButtonEvents(1)
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE owned-button cleanup: %v", err)
	}
	if got, want := harness.waitForButtonEvents(1), []x11ButtonEvent{{button: x11ButtonRight}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CloseMainDisplay button cleanup = %+v, want %+v", got, want)
	}
	harness.assertNoMatchingEvent("duplicate owned button release", 100*time.Millisecond, func(event xgb.Event) bool {
		release, ok := event.(xproto.ButtonReleaseEvent)
		return ok && release.Detail == x11ButtonRight
	})

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

func TestPureGoX11SessionSwitchReleasesOwnedInput(t *testing.T) {
	harness := newX11InputHarness(t)
	if err := robotgo.KeyToggle("shift", "down"); err != nil {
		t.Fatalf("hold key before session switch: %v", err)
	}
	heldKey := harness.waitForKeyPress("held key before session switch").Detail
	if err := robotgo.Toggle("right", "down"); err != nil {
		t.Fatalf("hold button before session switch: %v", err)
	}
	harness.waitForButtonEvents(1)

	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	if err := robotgo.KeyboardReady(); err == nil {
		t.Fatal("KeyboardReady unexpectedly accepted a switched Wayland session")
	}
	if release := harness.waitForKeyRelease("owned key cleanup after session switch"); release.Detail != heldKey {
		t.Fatalf("session switch released keycode %d, want %d", release.Detail, heldKey)
	}
	if got, want := harness.waitForButtonEvents(1), []x11ButtonEvent{{button: x11ButtonRight}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("session-switch button cleanup = %+v, want %+v", got, want)
	}
	if harness.keyPressed(heldKey) {
		t.Fatal("session switch left a RobotGo-owned key held on the old X server")
	}
	if reply, err := xproto.QueryPointer(harness.conn, harness.root).Reply(); err != nil {
		t.Fatalf("query pointer after session switch: %v", err)
	} else if reply.Mask&xproto.ButtonMask3 != 0 {
		t.Fatal("session switch left a RobotGo-owned button held on the old X server")
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
