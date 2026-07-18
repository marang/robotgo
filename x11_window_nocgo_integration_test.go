//go:build linux && !cgo && x11integration && !wayland

package robotgo_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/marang/robotgo"
)

const x11WindowStatePollTimeout = 2 * time.Second

var testX11SupportedWindowAtoms = []string{
	"_NET_ACTIVE_WINDOW",
	"_NET_CLOSE_WINDOW",
	"_NET_WM_STATE",
	"_NET_WM_STATE_ABOVE",
	"_NET_WM_STATE_HIDDEN",
	"_NET_WM_STATE_MAXIMIZED_HORZ",
	"_NET_WM_STATE_MAXIMIZED_VERT",
}

type testX11WindowManager struct {
	t          *testing.T
	xu         *xgbutil.XUtil
	check      xproto.Window
	activeAtom xproto.Atom
	changeAtom xproto.Atom
	closeAtom  xproto.Atom
	stateAtom  xproto.Atom
	events     chan xproto.Atom
	done       chan struct{}
}

func newTestX11WindowManager(
	t *testing.T,
	harness *x11InputHarness,
) *testX11WindowManager {
	t.Helper()
	xu, err := xgbutil.NewConnDisplay(os.Getenv("DISPLAY"))
	if err != nil {
		t.Fatalf("connect test X11 window manager: %v", err)
	}
	check, err := xproto.NewWindowId(xu.Conn())
	if err != nil {
		xu.Conn().Close()
		t.Fatalf("allocate X11 window-manager check window: %v", err)
	}
	screen := xu.Screen()
	if err := xproto.CreateWindowChecked(
		xu.Conn(),
		screen.RootDepth,
		check,
		screen.Root,
		0,
		0,
		1,
		1,
		0,
		xproto.WindowClassInputOutput,
		screen.RootVisual,
		0,
		nil,
	).Check(); err != nil {
		xu.Conn().Close()
		t.Fatalf("create X11 window-manager check window: %v", err)
	}
	if err := xproto.ChangeWindowAttributesChecked(
		xu.Conn(),
		screen.Root,
		xproto.CwEventMask,
		[]uint32{
			xproto.EventMaskSubstructureNotify |
				xproto.EventMaskSubstructureRedirect,
		},
	).Check(); err != nil {
		_ = xproto.DestroyWindowChecked(xu.Conn(), check).Check()
		xu.Conn().Close()
		t.Fatalf("claim X11 window-manager root events: %v", err)
	}
	ownershipTransferred := false
	defer func() {
		if ownershipTransferred {
			return
		}
		_ = xproto.DestroyWindowChecked(xu.Conn(), check).Check()
		xu.Conn().Close()
	}()
	if err := ewmh.SupportingWmCheckSet(xu, screen.Root, check); err != nil {
		t.Fatalf("set root EWMH window-manager check: %v", err)
	}
	if err := ewmh.SupportingWmCheckSet(xu, check, check); err != nil {
		t.Fatalf("set self EWMH window-manager check: %v", err)
	}
	if err := ewmh.SupportedSet(xu, testX11SupportedWindowAtoms); err != nil {
		t.Fatalf("set supported EWMH operations: %v", err)
	}
	if err := ewmh.ClientListSet(xu, []xproto.Window{harness.window}); err != nil {
		t.Fatalf("set EWMH client list: %v", err)
	}
	if err := ewmh.ActiveWindowSet(xu, harness.window); err != nil {
		t.Fatalf("set EWMH active window: %v", err)
	}
	if err := ewmh.WmPidSet(xu, harness.window, uint(os.Getpid())); err != nil {
		t.Fatalf("set EWMH window pid: %v", err)
	}
	if err := ewmh.WmNameSet(xu, harness.window, "RobotGo Pure-Go X11"); err != nil {
		t.Fatalf("set EWMH window title: %v", err)
	}
	if err := ewmh.FrameExtentsSet(xu, harness.window, &ewmh.FrameExtents{
		Left:   5,
		Right:  7,
		Top:    20,
		Bottom: 8,
	}); err != nil {
		t.Fatalf("set EWMH frame extents: %v", err)
	}
	if err := ewmh.WmStateSet(xu, harness.window, nil); err != nil {
		t.Fatalf("initialize EWMH window state: %v", err)
	}
	manager := &testX11WindowManager{
		t:          t,
		xu:         xu,
		check:      check,
		activeAtom: testX11Atom(t, xu.Conn(), "_NET_ACTIVE_WINDOW"),
		changeAtom: testX11Atom(t, xu.Conn(), "WM_CHANGE_STATE"),
		closeAtom:  testX11Atom(t, xu.Conn(), "_NET_CLOSE_WINDOW"),
		stateAtom:  testX11Atom(t, xu.Conn(), "_NET_WM_STATE"),
		events:     make(chan xproto.Atom, 16),
		done:       make(chan struct{}),
	}
	xu.Conn().Sync()
	go manager.run()
	t.Cleanup(manager.close)
	ownershipTransferred = true
	return manager
}

func (manager *testX11WindowManager) run() {
	defer close(manager.done)
	for {
		event, err := manager.xu.Conn().WaitForEvent()
		if event == nil && err == nil {
			return
		}
		if err != nil {
			return
		}
		message, ok := event.(xproto.ClientMessageEvent)
		if !ok {
			continue
		}
		if err := manager.apply(message); err != nil {
			manager.t.Errorf("apply X11 window-manager request: %v", err)
			continue
		}
		manager.xu.Conn().Sync()
		select {
		case manager.events <- message.Type:
		default:
			manager.t.Error("X11 window-manager event acknowledgement queue is full")
		}
	}
}

func (manager *testX11WindowManager) apply(message xproto.ClientMessageEvent) error {
	switch message.Type {
	case manager.activeAtom:
		if err := ewmh.ActiveWindowSet(manager.xu, message.Window); err != nil {
			return err
		}
		return manager.updateState(message.Window, ewmh.StateRemove, []string{
			"_NET_WM_STATE_HIDDEN",
		})
	case manager.changeAtom:
		if len(message.Data.Data32) == 0 || message.Data.Data32[0] != 3 {
			return errors.New("unexpected WM_CHANGE_STATE payload")
		}
		return manager.updateState(message.Window, ewmh.StateAdd, []string{
			"_NET_WM_STATE_HIDDEN",
		})
	case manager.stateAtom:
		if len(message.Data.Data32) < 3 {
			return errors.New("short _NET_WM_STATE payload")
		}
		names := make([]string, 0, 2)
		for _, atom := range message.Data.Data32[1:3] {
			if atom == 0 {
				continue
			}
			name, err := xproto.GetAtomName(manager.xu.Conn(), xproto.Atom(atom)).Reply()
			if err != nil || name == nil {
				return errors.New("resolve _NET_WM_STATE atom")
			}
			names = append(names, string(name.Name))
		}
		return manager.updateState(message.Window, int(message.Data.Data32[0]), names)
	case manager.closeAtom:
		return nil
	default:
		return errors.New("unexpected X11 client message")
	}
}

func (manager *testX11WindowManager) updateState(
	window xproto.Window,
	action int,
	names []string,
) error {
	current, err := ewmh.WmStateGet(manager.xu, window)
	if err != nil {
		current = nil
	}
	state := make(map[string]bool, len(current)+len(names))
	for _, name := range current {
		state[name] = true
	}
	for _, name := range names {
		switch action {
		case ewmh.StateRemove:
			delete(state, name)
		case ewmh.StateAdd:
			state[name] = true
		case ewmh.StateToggle:
			state[name] = !state[name]
		default:
			return errors.New("unknown _NET_WM_STATE action")
		}
	}
	result := make([]string, 0, len(state))
	for name, enabled := range state {
		if enabled {
			result = append(result, name)
		}
	}
	return ewmh.WmStateSet(manager.xu, window, result)
}

func (manager *testX11WindowManager) waitFor(atom xproto.Atom) {
	manager.t.Helper()
	select {
	case got := <-manager.events:
		if got != atom {
			manager.t.Fatalf("X11 window-manager request atom = %#x, want %#x", got, atom)
		}
	case <-time.After(x11WindowStatePollTimeout):
		manager.t.Fatalf("X11 window-manager request %#x timed out", atom)
	}
}

func (manager *testX11WindowManager) removeCheck() {
	manager.t.Helper()
	property := testX11Atom(manager.t, manager.xu.Conn(), "_NET_SUPPORTING_WM_CHECK")
	if err := xproto.DeletePropertyChecked(
		manager.xu.Conn(),
		manager.xu.RootWin(),
		property,
	).Check(); err != nil {
		manager.t.Fatalf("remove EWMH manager check: %v", err)
	}
	manager.xu.Conn().Sync()
}

func (manager *testX11WindowManager) removeSupported(name string) {
	manager.t.Helper()
	atoms := make([]string, 0, len(testX11SupportedWindowAtoms))
	for _, candidate := range testX11SupportedWindowAtoms {
		if candidate != name {
			atoms = append(atoms, candidate)
		}
	}
	if err := ewmh.SupportedSet(manager.xu, atoms); err != nil {
		manager.t.Fatalf("remove supported EWMH operation %s: %v", name, err)
	}
	manager.xu.Conn().Sync()
}

func (manager *testX11WindowManager) close() {
	_ = xproto.DestroyWindowChecked(manager.xu.Conn(), manager.check).Check()
	manager.xu.Conn().Close()
	select {
	case <-manager.done:
	case <-time.After(time.Second):
		manager.t.Error("X11 window-manager event loop did not stop")
	}
}

func testX11Atom(t *testing.T, conn *xgb.Conn, name string) xproto.Atom {
	t.Helper()
	reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
	if err != nil || reply == nil {
		t.Fatalf("intern X11 atom %q: reply=%+v err=%v", name, reply, err)
	}
	return reply.Atom
}

func TestPureGoX11WindowIntrospectionAndControl(t *testing.T) {
	harness := newX11InputHarness(t)
	manager := newTestX11WindowManager(t, harness)

	capability := robotgo.GetLinuxCapabilities().Window
	if !capability.Available || capability.Backend != "pure-go-x11" {
		t.Fatalf("Pure-Go X11 window capability = %+v", capability)
	}
	if handle := robotgo.GetHandle(); handle != int(harness.window) {
		t.Fatalf("GetHandle() = %#x, want %#x", handle, harness.window)
	}
	setPureGoMalformedX11Property(
		t,
		harness,
		harness.root,
		"_NET_ACTIVE_WINDOW",
		xproto.AtomCardinal,
		32,
		[]byte{1, 0, 0, 0},
	)
	if handle := robotgo.GetHandle(); handle != int(harness.window) {
		t.Fatalf(
			"GetHandle() with wrong _NET_ACTIVE_WINDOW type = %#x, want focused window %#x",
			handle,
			harness.window,
		)
	}
	setPureGoMalformedX11Property(
		t,
		harness,
		harness.root,
		"_NET_ACTIVE_WINDOW",
		xproto.AtomWindow,
		32,
		nil,
	)
	if handle := robotgo.GetHandle(); handle != int(harness.window) {
		t.Fatalf(
			"GetHandle() with empty _NET_ACTIVE_WINDOW = %#x, want focused window %#x",
			handle,
			harness.window,
		)
	}
	if err := ewmh.ActiveWindowSet(manager.xu, harness.window); err != nil {
		t.Fatalf("restore EWMH active window: %v", err)
	}
	if pid := robotgo.GetPid(); pid != os.Getpid() {
		t.Fatalf("GetPid() = %d, want %d", pid, os.Getpid())
	}
	if handle := robotgo.GetHWNDByPid(os.Getpid()); handle != int(harness.window) {
		t.Fatalf("GetHWNDByPid() = %#x, want %#x", handle, harness.window)
	}
	if title, err := robotgo.GetTitleE(); err != nil || title != "RobotGo Pure-Go X11" {
		t.Fatalf("GetTitleE() = %q, %v", title, err)
	}
	if title, err := robotgo.GetTitleE(os.Getpid()); err != nil || title != "RobotGo Pure-Go X11" {
		t.Fatalf("GetTitleE(pid) = %q, %v", title, err)
	}
	if title, err := robotgo.GetTitleE(int(harness.window), 1); err != nil ||
		title != "RobotGo Pure-Go X11" {
		t.Fatalf("GetTitleE(handle) = %q, %v", title, err)
	}
	if x, y, width, height := robotgo.GetClient(os.Getpid()); x != x11WindowX ||
		y != x11WindowY || width != x11WindowWidth || height != x11WindowHeight {
		t.Fatalf(
			"GetClient(pid) = (%d,%d %dx%d), want (%d,%d %dx%d)",
			x,
			y,
			width,
			height,
			x11WindowX,
			x11WindowY,
			x11WindowWidth,
			x11WindowHeight,
		)
	}
	if x, y, width, height := robotgo.GetBounds(os.Getpid()); x != x11WindowX-5 ||
		y != x11WindowY-20 || width != x11WindowWidth+12 ||
		height != x11WindowHeight+28 {
		t.Fatalf(
			"GetBounds(pid) = (%d,%d %dx%d), want (%d,%d %dx%d)",
			x,
			y,
			width,
			height,
			x11WindowX-5,
			x11WindowY-20,
			x11WindowWidth+12,
			x11WindowHeight+28,
		)
	}

	if err := robotgo.SetActiveE(robotgo.Handle(harness.window)); err != nil {
		t.Fatalf("SetActiveE: %v", err)
	}
	manager.waitFor(manager.activeAtom)

	if err := robotgo.MinWindowE(os.Getpid(), true); err != nil {
		t.Fatalf("MinWindowE(true): %v", err)
	}
	manager.waitFor(manager.changeAtom)
	if minimized, err := robotgo.IsMinimizedE(); err != nil || !minimized {
		t.Fatalf("IsMinimizedE() = %v, %v after minimize", minimized, err)
	}
	if err := robotgo.MinWindowE(os.Getpid(), false); err != nil {
		t.Fatalf("MinWindowE(false): %v", err)
	}
	manager.waitFor(manager.activeAtom)
	if minimized, err := robotgo.IsMinimizedE(); err != nil || minimized {
		t.Fatalf("IsMinimizedE() = %v, %v after restore", minimized, err)
	}

	if err := robotgo.MaxWindowE(os.Getpid(), true); err != nil {
		t.Fatalf("MaxWindowE(true): %v", err)
	}
	manager.waitFor(manager.stateAtom)
	if maximized, err := robotgo.IsMaximizedE(); err != nil || !maximized {
		t.Fatalf("IsMaximizedE() = %v, %v after maximize", maximized, err)
	}
	if err := robotgo.MaxWindowE(os.Getpid(), false); err != nil {
		t.Fatalf("MaxWindowE(false): %v", err)
	}
	manager.waitFor(manager.stateAtom)
	if maximized, err := robotgo.IsMaximizedE(); err != nil || maximized {
		t.Fatalf("IsMaximizedE() = %v, %v after restore", maximized, err)
	}

	if err := robotgo.SetTopMostE(true); err != nil {
		t.Fatalf("SetTopMostE(true): %v", err)
	}
	manager.waitFor(manager.stateAtom)
	if topMost, err := robotgo.IsTopMostE(); err != nil || !topMost {
		t.Fatalf("IsTopMostE() = %v, %v after enable", topMost, err)
	}
	if err := robotgo.SetTopMostE(false); err != nil {
		t.Fatalf("SetTopMostE(false): %v", err)
	}
	manager.waitFor(manager.stateAtom)
	if topMost, err := robotgo.IsTopMostE(); err != nil || topMost {
		t.Fatalf("IsTopMostE() = %v, %v after disable", topMost, err)
	}

	if err := robotgo.CloseWindowE(); err != nil {
		t.Fatalf("CloseWindowE(): %v", err)
	}
	manager.waitFor(manager.closeAtom)

	manager.removeSupported("_NET_WM_STATE_ABOVE")
	if _, err := robotgo.IsTopMostE(); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf(
			"IsTopMostE without advertised ABOVE support error = %v, want ErrNotSupported",
			err,
		)
	}
	if err := robotgo.SetTopMostE(true); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf(
			"SetTopMostE without advertised ABOVE support error = %v, want ErrNotSupported",
			err,
		)
	}
	manager.removeCheck()
	if err := robotgo.SetActiveE(robotgo.Handle(harness.window)); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf(
			"SetActiveE without a consistent EWMH window manager error = %v, want ErrNotSupported",
			err,
		)
	}
}

func setPureGoMalformedX11Property(
	t *testing.T,
	harness *x11InputHarness,
	window xproto.Window,
	name string,
	propertyType xproto.Atom,
	format byte,
	payload []byte,
) {
	t.Helper()
	property := testX11Atom(t, harness.conn, name)
	unitBytes := uint32(format / 8)
	valueLen := uint32(0)
	if unitBytes != 0 {
		valueLen = uint32(len(payload)) / unitBytes
	}
	if err := xproto.ChangePropertyChecked(
		harness.conn,
		xproto.PropModeReplace,
		window,
		property,
		propertyType,
		format,
		valueLen,
		payload,
	).Check(); err != nil {
		t.Fatalf("set malformed %s: %v", name, err)
	}
	harness.conn.Sync()
}
