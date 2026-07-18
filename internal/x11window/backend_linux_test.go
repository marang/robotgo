//go:build linux

package x11window

import (
	"errors"
	"math"
	"testing"

	"github.com/marang/robotgo/internal/windowbackend"
)

type fakeSystem struct {
	active        windowbackend.Handle
	valid         map[windowbackend.Handle]bool
	byPID         map[uint32]windowbackend.Handle
	pids          map[windowbackend.Handle]uint32
	titles        map[windowbackend.Handle]string
	windowRects   map[windowbackend.Handle]windowbackend.Rect
	clientRects   map[windowbackend.Handle]windowbackend.Rect
	states        map[windowbackend.Handle]map[windowbackend.State]bool
	topMost       map[windowbackend.Handle]bool
	err           error
	activated     windowbackend.Handle
	stateHandle   windowbackend.Handle
	state         windowbackend.State
	stateEnabled  bool
	topHandle     windowbackend.Handle
	topEnabled    bool
	closed        windowbackend.Handle
	validationErr error
}

func (system *fakeSystem) ActiveWindow() (windowbackend.Handle, error) {
	return system.active, system.err
}
func (system *fakeSystem) WindowExists(handle windowbackend.Handle) (bool, error) {
	return system.valid[handle], system.validationErr
}
func (system *fakeSystem) FindWindowByPID(pid uint32) (windowbackend.Handle, error) {
	if system.err != nil {
		return 0, system.err
	}
	return system.byPID[pid], nil
}
func (system *fakeSystem) WindowProcessID(handle windowbackend.Handle) (uint32, error) {
	return system.pids[handle], system.err
}
func (system *fakeSystem) WindowText(handle windowbackend.Handle) (string, error) {
	return system.titles[handle], system.err
}
func (system *fakeSystem) WindowRect(handle windowbackend.Handle) (windowbackend.Rect, error) {
	return system.windowRects[handle], system.err
}
func (system *fakeSystem) ClientRect(handle windowbackend.Handle) (windowbackend.Rect, error) {
	return system.clientRects[handle], system.err
}
func (system *fakeSystem) ActivateWindow(handle windowbackend.Handle) error {
	system.activated = handle
	return system.err
}
func (system *fakeSystem) SetWindowState(handle windowbackend.Handle, state windowbackend.State, enabled bool) error {
	system.stateHandle, system.state, system.stateEnabled = handle, state, enabled
	return system.err
}
func (system *fakeSystem) WindowState(handle windowbackend.Handle, state windowbackend.State) (bool, error) {
	return system.states[handle][state], system.err
}
func (system *fakeSystem) IsTopMost(handle windowbackend.Handle) (bool, error) {
	return system.topMost[handle], system.err
}
func (system *fakeSystem) SetTopMost(handle windowbackend.Handle, enabled bool) error {
	system.topHandle, system.topEnabled = handle, enabled
	return system.err
}
func (system *fakeSystem) CloseWindow(handle windowbackend.Handle) error {
	system.closed = handle
	return system.err
}

func newFakeBackend() (*Backend, *fakeSystem) {
	const handle = windowbackend.Handle(0x42)
	system := &fakeSystem{
		active:      handle,
		valid:       map[windowbackend.Handle]bool{handle: true},
		byPID:       map[uint32]windowbackend.Handle{1234: handle},
		pids:        map[windowbackend.Handle]uint32{handle: 1234},
		titles:      map[windowbackend.Handle]string{handle: "RobotGo X11"},
		windowRects: map[windowbackend.Handle]windowbackend.Rect{handle: {X: 8, Y: 9, Width: 640, Height: 480}},
		clientRects: map[windowbackend.Handle]windowbackend.Rect{handle: {X: 12, Y: 30, Width: 620, Height: 450}},
		states: map[windowbackend.Handle]map[windowbackend.State]bool{
			handle: {
				windowbackend.StateMinimized: true,
				windowbackend.StateMaximized: false,
			},
		},
		topMost: map[windowbackend.Handle]bool{handle: true},
	}
	return New(system), system
}

func TestBackendIntrospectionContract(t *testing.T) {
	backend, _ := newFakeBackend()

	handle, err := backend.Active()
	if err != nil || handle != 0x42 {
		t.Fatalf("Active() = %#x, %v", handle, err)
	}
	if resolved, err := backend.Resolve(1234, false); err != nil || resolved != handle {
		t.Fatalf("Resolve(pid) = %#x, %v", resolved, err)
	}
	if resolved, err := backend.Resolve(int(handle), true); err != nil || resolved != handle {
		t.Fatalf("Resolve(handle) = %#x, %v", resolved, err)
	}
	if err := backend.Select(int(handle), true); err != nil || backend.Selected() != handle {
		t.Fatalf("Select/Selected = %#x, %v", backend.Selected(), err)
	}
	if pid, err := backend.PID(handle); err != nil || pid != 1234 {
		t.Fatalf("PID() = %d, %v", pid, err)
	}
	if title, err := backend.Title(handle); err != nil || title != "RobotGo X11" {
		t.Fatalf("Title() = %q, %v", title, err)
	}
	if rect, err := backend.Bounds(handle, false); err != nil ||
		rect != (windowbackend.Rect{X: 8, Y: 9, Width: 640, Height: 480}) {
		t.Fatalf("Bounds(window) = %+v, %v", rect, err)
	}
	if rect, err := backend.Bounds(handle, true); err != nil ||
		rect != (windowbackend.Rect{X: 12, Y: 30, Width: 620, Height: 450}) {
		t.Fatalf("Bounds(client) = %+v, %v", rect, err)
	}
	if state, err := backend.State(handle, windowbackend.StateMinimized); err != nil || !state {
		t.Fatalf("State(minimized) = %v, %v", state, err)
	}
	if state, err := backend.State(handle, windowbackend.StateMaximized); err != nil || state {
		t.Fatalf("State(maximized) = %v, %v", state, err)
	}
	if state, err := backend.TopMost(handle); err != nil || !state {
		t.Fatalf("TopMost() = %v, %v", state, err)
	}
}

func TestBackendMutationContract(t *testing.T) {
	backend, system := newFakeBackend()
	const handle = windowbackend.Handle(0x42)

	if err := backend.Activate(handle); err != nil || system.activated != handle {
		t.Fatalf("Activate() target = %#x, err=%v", system.activated, err)
	}
	if err := backend.SetState(handle, windowbackend.StateMaximized, true); err != nil ||
		system.stateHandle != handle || system.state != windowbackend.StateMaximized || !system.stateEnabled {
		t.Fatalf(
			"SetState() = handle %#x state %d enabled %v, err=%v",
			system.stateHandle,
			system.state,
			system.stateEnabled,
			err,
		)
	}
	if err := backend.SetTopMost(handle, false); err != nil ||
		system.topHandle != handle || system.topEnabled {
		t.Fatalf("SetTopMost() = handle %#x enabled %v, err=%v", system.topHandle, system.topEnabled, err)
	}
	if err := backend.Close(handle); err != nil || system.closed != handle {
		t.Fatalf("Close() target = %#x, err=%v", system.closed, err)
	}
}

func TestBackendRejectsInvalidTargetsAndResults(t *testing.T) {
	backend, system := newFakeBackend()
	if _, err := backend.Resolve(0, false); !errors.Is(err, ErrWindowNotFound) {
		t.Fatalf("Resolve(0) error = %v, want ErrWindowNotFound", err)
	}
	if strconvIntCanExceedUint32() {
		target := int(uint64(math.MaxUint32) + 1)
		if _, err := backend.Resolve(target, true); !errors.Is(err, ErrWindowNotFound) {
			t.Fatalf("Resolve(overflow) error = %v, want ErrWindowNotFound", err)
		}
	}
	if _, err := backend.Resolve(99, true); !errors.Is(err, ErrInvalidWindow) {
		t.Fatalf("Resolve(stale) error = %v, want ErrInvalidWindow", err)
	}
	system.active = 0
	if _, err := backend.Active(); !errors.Is(err, ErrInvalidWindow) {
		t.Fatalf("Active(zero) error = %v, want ErrInvalidWindow", err)
	}
	system.active = 0x42
	system.titles[0x42] = ""
	if _, err := backend.Title(0x42); !errors.Is(err, ErrWindowNotFound) {
		t.Fatalf("Title(empty) error = %v, want ErrWindowNotFound", err)
	}
	system.windowRects[0x42] = windowbackend.Rect{Width: 0, Height: 10}
	if _, err := backend.Bounds(0x42, false); !errors.Is(err, ErrOperation) {
		t.Fatalf("Bounds(invalid) error = %v, want ErrOperation", err)
	}
	if err := backend.SetState(0x42, windowbackend.State(99), true); !errors.Is(err, ErrOperation) {
		t.Fatalf("SetState(unknown) error = %v, want ErrOperation", err)
	}
}

func TestBackendWrapsSystemErrors(t *testing.T) {
	backend, system := newFakeBackend()
	system.err = errors.New("X11 transport failed")

	operations := []error{
		func() error { _, err := backend.Active(); return err }(),
		func() error { _, err := backend.Resolve(1234, false); return err }(),
		func() error { _, err := backend.PID(0x42); return err }(),
		func() error { _, err := backend.Title(0x42); return err }(),
		func() error { _, err := backend.Bounds(0x42, false); return err }(),
		backend.Activate(0x42),
		backend.SetState(0x42, windowbackend.StateMaximized, true),
		func() error { _, err := backend.State(0x42, windowbackend.StateMaximized); return err }(),
		func() error { _, err := backend.TopMost(0x42); return err }(),
		backend.SetTopMost(0x42, true),
		backend.Close(0x42),
	}
	for index, err := range operations {
		if !errors.Is(err, ErrOperation) && !errors.Is(err, ErrWindowNotFound) {
			t.Errorf("operation %d error = %v, want wrapped operation/not-found error", index, err)
		}
	}
}

func TestBackendPreservesUnsupportedCause(t *testing.T) {
	backend, system := newFakeBackend()
	system.err = windowbackend.ErrUnsupported

	if err := backend.SetTopMost(0x42, true); !errors.Is(err, ErrOperation) ||
		!errors.Is(err, windowbackend.ErrUnsupported) {
		t.Fatalf("SetTopMost() error = %v, want operation and unsupported causes", err)
	}
}

func TestBackendReportsValidationTransportError(t *testing.T) {
	backend, system := newFakeBackend()
	transportErr := errors.New("X11 connection closed")
	system.validationErr = transportErr

	_, err := backend.Resolve(0x42, true)
	if !errors.Is(err, ErrOperation) || !errors.Is(err, transportErr) {
		t.Fatalf("Resolve() validation error = %v, want operation and transport causes", err)
	}
}

func strconvIntCanExceedUint32() bool {
	return uint64(^uint(0)>>1) > math.MaxUint32
}

func TestAddFrameExtents(t *testing.T) {
	client := windowbackend.Rect{X: -10, Y: 20, Width: 400, Height: 300}
	got, err := addFrameExtents(client, x11FrameExtents{
		left:   5,
		right:  7,
		top:    20,
		bottom: 8,
	})
	want := windowbackend.Rect{X: -15, Y: 0, Width: 412, Height: 328}
	if err != nil || got != want {
		t.Fatalf("addFrameExtents() = %+v, %v, want %+v", got, err, want)
	}
	if _, err := addFrameExtents(client, x11FrameExtents{
		left: maxFrameExtentPixels + 1,
	}); !errors.Is(err, ErrOperation) {
		t.Fatalf("oversized frame extent error = %v, want ErrOperation", err)
	}
}
