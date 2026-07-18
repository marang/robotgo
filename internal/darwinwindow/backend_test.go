package darwinwindow

import (
	"errors"
	"math"
	"testing"

	"github.com/marang/robotgo/internal/windowbackend"
)

type fakeSystem struct {
	readyErr  error
	active    windowbackend.Handle
	activeErr error
	exists    map[windowbackend.Handle]bool
	existsErr error
	byPID     map[int32]windowbackend.Handle
	findErr   error
	pids      map[windowbackend.Handle]int32
	pidErr    error
	titles    map[windowbackend.Handle]string
	titleErr  error
	rects     map[windowbackend.Handle]windowbackend.Rect
	rectErr   error
	minimized map[windowbackend.Handle]bool
	stateErr  error
	raiseErr  error
	closeErr  error
	closed    bool
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{
		active: 41,
		exists: map[windowbackend.Handle]bool{41: true, 42: true},
		byPID:  map[int32]windowbackend.Handle{1001: 41, 1002: 42},
		pids:   map[windowbackend.Handle]int32{41: 1001, 42: 1002},
		titles: map[windowbackend.Handle]string{41: "RobotGo", 42: "Second"},
		rects: map[windowbackend.Handle]windowbackend.Rect{
			41: {X: -20, Y: 10, Width: 800, Height: 600},
			42: {X: 40, Y: 50, Width: 320, Height: 240},
		},
		minimized: make(map[windowbackend.Handle]bool),
	}
}

func (system *fakeSystem) Ready() error { return system.readyErr }
func (system *fakeSystem) ActiveWindow() (windowbackend.Handle, error) {
	return system.active, system.activeErr
}
func (system *fakeSystem) WindowExists(handle windowbackend.Handle) (bool, error) {
	return system.exists[handle], system.existsErr
}
func (system *fakeSystem) FindWindowByPID(pid int32) (windowbackend.Handle, error) {
	return system.byPID[pid], system.findErr
}
func (system *fakeSystem) WindowPID(handle windowbackend.Handle) (int32, error) {
	return system.pids[handle], system.pidErr
}
func (system *fakeSystem) WindowTitle(handle windowbackend.Handle) (string, error) {
	return system.titles[handle], system.titleErr
}
func (system *fakeSystem) WindowRect(handle windowbackend.Handle) (windowbackend.Rect, error) {
	return system.rects[handle], system.rectErr
}
func (system *fakeSystem) RaiseWindow(windowbackend.Handle) error { return system.raiseErr }
func (system *fakeSystem) SetMinimized(handle windowbackend.Handle, enabled bool) error {
	if system.stateErr == nil {
		system.minimized[handle] = enabled
	}
	return system.stateErr
}
func (system *fakeSystem) IsMinimized(handle windowbackend.Handle) (bool, error) {
	return system.minimized[handle], system.stateErr
}
func (system *fakeSystem) CloseWindow(windowbackend.Handle) error { return system.closeErr }
func (system *fakeSystem) Close() error {
	system.closed = true
	return nil
}

func TestBackendIntrospectionAndSelection(t *testing.T) {
	system := newFakeSystem()
	backend := New(system)

	active, err := backend.Active()
	if err != nil || active != 41 {
		t.Fatalf("Active() = %#x, %v", active, err)
	}
	resolved, err := backend.Resolve(1002, false)
	if err != nil || resolved != 42 {
		t.Fatalf("Resolve(pid) = %#x, %v", resolved, err)
	}
	resolved, err = backend.Resolve(41, true)
	if err != nil || resolved != 41 {
		t.Fatalf("Resolve(handle) = %#x, %v", resolved, err)
	}
	if err := backend.Select(1002, false); err != nil {
		t.Fatalf("Select(pid): %v", err)
	}
	if selected := backend.Selected(); selected != 42 {
		t.Fatalf("Selected() = %#x, want 0x2a", selected)
	}
	pid, err := backend.PID(41)
	if err != nil || pid != 1001 {
		t.Fatalf("PID() = %d, %v", pid, err)
	}
	title, err := backend.Title(41)
	if err != nil || title != "RobotGo" {
		t.Fatalf("Title() = %q, %v", title, err)
	}
	for _, client := range []bool{false, true} {
		rect, err := backend.Bounds(41, client)
		if err != nil || rect != system.rects[41] {
			t.Fatalf("Bounds(client=%v) = %+v, %v", client, rect, err)
		}
	}
}

func TestBackendSupportedMutations(t *testing.T) {
	system := newFakeSystem()
	backend := New(system)

	if err := backend.Activate(41); err != nil {
		t.Fatalf("Activate(): %v", err)
	}
	if err := backend.SetState(41, windowbackend.StateMinimized, true); err != nil {
		t.Fatalf("SetState(minimized): %v", err)
	}
	minimized, err := backend.State(41, windowbackend.StateMinimized)
	if err != nil || !minimized {
		t.Fatalf("State(minimized) = %v, %v", minimized, err)
	}
	if err := backend.SetState(41, windowbackend.StateMinimized, false); err != nil {
		t.Fatalf("restore minimized: %v", err)
	}
	if minimized, err := backend.State(41, windowbackend.StateMinimized); err != nil || minimized {
		t.Fatalf("restored State(minimized) = %v, %v", minimized, err)
	}
	if err := backend.Close(41); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if err := backend.CloseSystem(); err != nil || !system.closed {
		t.Fatalf("backend lifecycle Close() = %v, closed=%v", err, system.closed)
	}
}

func TestBackendUnsupportedStateIsExplicit(t *testing.T) {
	backend := New(newFakeSystem())

	if err := backend.SetState(41, windowbackend.StateMaximized, true); !errors.Is(err, windowbackend.ErrUnsupported) {
		t.Fatalf("SetState(maximized) = %v, want unsupported", err)
	}
	if _, err := backend.State(41, windowbackend.StateMaximized); !errors.Is(err, windowbackend.ErrUnsupported) {
		t.Fatalf("State(maximized) = %v, want unsupported", err)
	}
	if _, err := backend.TopMost(41); !errors.Is(err, windowbackend.ErrUnsupported) {
		t.Fatalf("TopMost() = %v, want unsupported", err)
	}
	if err := backend.SetTopMost(41, true); !errors.Is(err, windowbackend.ErrUnsupported) {
		t.Fatalf("SetTopMost() = %v, want unsupported", err)
	}
}

func TestBackendPermissionAndValidation(t *testing.T) {
	system := newFakeSystem()
	system.readyErr = ErrPermission
	backend := New(system)

	if _, err := backend.Active(); !errors.Is(err, ErrPermission) {
		t.Fatalf("Active() = %v, want permission error", err)
	}
	system.readyErr = nil

	tests := []struct {
		name     string
		target   int
		isHandle bool
		want     error
	}{
		{name: "zero pid", target: 0, want: windowbackend.ErrWindowNotFound},
		{name: "negative pid", target: -1, want: windowbackend.ErrWindowNotFound},
		{name: "missing pid", target: 9000, want: windowbackend.ErrInvalidWindow},
		{name: "missing handle", target: 9000, isHandle: true, want: windowbackend.ErrInvalidWindow},
	}
	if strconvIntSize() == 64 {
		tests = append(tests, struct {
			name     string
			target   int
			isHandle bool
			want     error
		}{
			name: "oversized handle", target: int(uint64(math.MaxUint32) + 1),
			isHandle: true, want: windowbackend.ErrInvalidWindow,
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := backend.Resolve(test.target, test.isHandle); !errors.Is(err, test.want) {
				t.Fatalf("Resolve(%d, %v) = %v, want %v", test.target, test.isHandle, err, test.want)
			}
		})
	}
}

func TestBackendWrapsOperationFailures(t *testing.T) {
	system := newFakeSystem()
	system.titleErr = errors.New("AX title failed")
	backend := New(system)
	if _, err := backend.Title(41); !errors.Is(err, windowbackend.ErrOperation) {
		t.Fatalf("Title() = %v, want operation error", err)
	}

	system.titleErr = ErrPermission
	if _, err := backend.Title(41); !errors.Is(err, windowbackend.ErrPermission) {
		t.Fatalf("Title() after permission revocation = %v, want permission error", err)
	}

	system.titleErr = ErrUnsupported
	if _, err := backend.Title(41); !errors.Is(err, windowbackend.ErrUnsupported) {
		t.Fatalf("Title() with unsupported native API = %v, want unsupported error", err)
	}

	system.titleErr = nil
	system.rects[41] = windowbackend.Rect{Width: 0, Height: 10}
	if _, err := backend.Bounds(41, false); !errors.Is(err, windowbackend.ErrOperation) {
		t.Fatalf("Bounds() = %v, want operation error", err)
	}
}

func strconvIntSize() int {
	return 32 << (^uint(0) >> 63)
}
