//go:build linux && x11integration && !wayland

package robotgo_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

	x11ButtonLeft         = 1
	x11ButtonRight        = 3
	x11ButtonWheelUp      = 4
	x11ButtonWheelLeft    = 6
	x11RequiredXTestMajor = 2
	x11RequiredXTestMinor = 2

	x11KeysymEnter  = 0xff0d
	x11KeysymShiftL = 0xffe1
	x11KeysymShiftR = 0xffe2
)

type x11InputHarness struct {
	t      x11TestingT
	conn   *xgb.Conn
	root   xproto.Window
	window xproto.Window
}

type x11TestingT interface {
	Helper()
	Cleanup(func())
	Errorf(string, ...interface{})
	Fatal(...interface{})
	Fatalf(string, ...interface{})
	Skipf(string, ...interface{})
}

type x11ButtonEvent struct {
	pressed bool
	button  xproto.Button
}

type x11InputState struct {
	pressedKeys []byte
	pointerX    int
	pointerY    int
	pointerMask uint16
}

func newX11InputHarness(t x11TestingT) *x11InputHarness {
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
	version, err := xtest.GetVersion(conn, x11RequiredXTestMajor, x11RequiredXTestMinor).Reply()
	if err != nil {
		conn.Close()
		x11Unavailable(t, "X11 integration test cannot query the XTEST extension: %v", err)
	}
	if !x11VersionAtLeast(version, x11RequiredXTestMajor, x11RequiredXTestMinor) {
		conn.Close()
		if version == nil {
			x11Unavailable(t, "X11 integration test received no XTEST version reply")
		}
		x11Unavailable(
			t,
			"X11 integration test requires XTEST %d.%d or newer, got %d.%d",
			x11RequiredXTestMajor,
			x11RequiredXTestMinor,
			version.MajorVersion,
			version.MinorVersion,
		)
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
		[]uint32{screen.WhitePixel, eventMask},
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
		t:      t,
		conn:   conn,
		root:   screen.Root,
		window: windowID,
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

func x11Unavailable(t x11TestingT, format string, args ...interface{}) {
	t.Helper()
	if os.Getenv(x11RequiredEnv) == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
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

func (h *x11InputHarness) assertNoInputEvent(description string, duration time.Duration) {
	h.t.Helper()
	h.assertNoMatchingEvent(description, duration, func(event xgb.Event) bool {
		switch event.(type) {
		case xproto.KeyPressEvent,
			xproto.KeyReleaseEvent,
			xproto.ButtonPressEvent,
			xproto.ButtonReleaseEvent,
			xproto.MotionNotifyEvent:
			return true
		default:
			return false
		}
	})
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
	state := h.inputState()
	return state.pointerX, state.pointerY
}

func (h *x11InputHarness) inputState() x11InputState {
	h.t.Helper()
	keys, err := xproto.QueryKeymap(h.conn).Reply()
	if err != nil || keys == nil {
		h.t.Fatalf("query X11 pressed keys: reply=%+v err=%v", keys, err)
	}
	pointer, err := xproto.QueryPointer(h.conn, h.root).Reply()
	if err != nil || pointer == nil {
		h.t.Fatalf("query X11 pointer state: reply=%+v err=%v", pointer, err)
	}
	return x11InputState{
		pressedKeys: append([]byte(nil), keys.Keys...),
		pointerX:    int(pointer.RootX),
		pointerY:    int(pointer.RootY),
		pointerMask: pointer.Mask,
	}
}

func (h *x11InputHarness) keyPressed(keycode xproto.Keycode) bool {
	h.t.Helper()
	keys := h.inputState().pressedKeys
	return keys[int(keycode)/8]&(1<<uint(keycode%8)) != 0
}

func x11VersionAtLeast(version *xtest.GetVersionReply, requiredMajor byte, requiredMinor uint16) bool {
	if version == nil {
		return false
	}
	return version.MajorVersion > requiredMajor ||
		version.MajorVersion == requiredMajor && version.MinorVersion >= requiredMinor
}

func TestX11XTestVersionAtLeast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version *xtest.GetVersionReply
		want    bool
	}{
		{name: "missing reply"},
		{name: "older minor", version: &xtest.GetVersionReply{MajorVersion: 2, MinorVersion: 1}},
		{name: "minimum", version: &xtest.GetVersionReply{MajorVersion: 2, MinorVersion: 2}, want: true},
		{name: "newer major", version: &xtest.GetVersionReply{MajorVersion: 3}, want: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := x11VersionAtLeast(test.version, x11RequiredXTestMajor, x11RequiredXTestMinor); got != test.want {
				t.Fatalf("x11VersionAtLeast(%+v, %d, %d) = %v, want %v", test.version, x11RequiredXTestMajor, x11RequiredXTestMinor, got, test.want)
			}
		})
	}
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

func (h *x11InputHarness) waitForNewRootChild(
	previous map[xproto.Window]struct{}, processDone <-chan error, stderr *strings.Builder,
) xproto.Window {
	h.t.Helper()
	ticker := time.NewTicker(x11PollInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(x11EventTimeout)
	defer deadline.Stop()
	for {
		for child := range h.rootChildren() {
			if _, existed := previous[child]; !existed {
				return child
			}
		}
		select {
		case err := <-processDone:
			detail := strings.TrimSpace(stderr.String())
			if detail == "" {
				detail = "no stderr output"
			}
			h.t.Fatalf("XKB oracle exited before creating a window: %v (%s)", err, detail)
		case <-ticker.C:
		case <-deadline.C:
			h.t.Fatalf("timed out after %s waiting for XKB oracle window", x11EventTimeout)
		}
	}
}

func xkbOracleArgs(path string) []string {
	args := []string{"interactive-x11"}
	help, _ := exec.Command(path, "interactive-x11", "--help").CombinedOutput()
	if strings.Contains(string(help), "--uniline") {
		args = append(args, "--uniline")
	}
	return args
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
	commandArgs := append([]string{"-oL", path}, xkbOracleArgs(path)...)
	command := exec.Command(stdbufPath, commandArgs...)
	command.Env = os.Environ()
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("create xkbcli stdout pipe: %v", err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		x11Unavailable(t, "start xkbcli X11 oracle: %v", err)
	}
	lines := make(chan string, 32)
	stopLines := make(chan struct{})
	scanDone := make(chan error, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		discard := false
		for scanner.Scan() {
			if !discard {
				select {
				case <-stopLines:
					discard = true
				default:
				}
			}
			if discard {
				continue
			}
			select {
			case lines <- scanner.Text():
			case <-stopLines:
				discard = true
			}
		}
		scanDone <- scanner.Err()
	}()
	processDone := make(chan error, 1)
	go func() {
		scanErr := <-scanDone
		waitErr := command.Wait()
		if scanErr != nil {
			waitErr = errors.Join(waitErr, fmt.Errorf("read xkbcli stdout: %w", scanErr))
		}
		processDone <- waitErr
		close(processDone)
	}()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGCONT)
		close(stopLines)
		_ = command.Process.Kill()
		select {
		case <-processDone:
		case <-time.After(x11EventTimeout):
			t.Errorf("XKB oracle process %d did not exit during cleanup", command.Process.Pid)
		}
	})
	window := harness.waitForNewRootChild(previousChildren, processDone, &stderr)
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
			// Older xkbcli releases only report key presses and have no event-type
			// field. Newer releases report both directions, so keep only down.
			if !strings.Contains(line, "down") && !strings.Contains(line, "keycode [") {
				continue
			}
			if strings.Contains(line, "up") && !strings.Contains(line, "down") {
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
