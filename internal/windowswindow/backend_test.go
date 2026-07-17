package windowswindow

import (
	"errors"
	"testing"
)

type fakeSystem struct {
	foreground Handle
	valid      map[Handle]bool
	byPID      map[uint32]Handle
	pids       map[Handle]uint32
	titles     map[Handle]string
	windowRect Rect
	clientRect Rect
	style      map[State]bool
	topMost    bool
	activated  Handle
	stateSet   State
	stateValue bool
	topSet     bool
	closed     Handle
	err        error
}

func (system *fakeSystem) ForegroundWindow() Handle { return system.foreground }
func (system *fakeSystem) IsWindow(handle Handle) bool {
	return system.valid[handle]
}
func (system *fakeSystem) FindWindowByPID(pid uint32) (Handle, error) {
	if system.err != nil {
		return 0, system.err
	}
	return system.byPID[pid], nil
}
func (system *fakeSystem) WindowProcessID(handle Handle) (uint32, error) {
	return system.pids[handle], system.err
}
func (system *fakeSystem) WindowText(handle Handle) (string, error) {
	return system.titles[handle], system.err
}
func (system *fakeSystem) WindowRect(Handle) (Rect, error) {
	return system.windowRect, system.err
}
func (system *fakeSystem) ClientRect(Handle) (Rect, error) {
	return system.clientRect, system.err
}
func (system *fakeSystem) SetForegroundWindow(handle Handle) error {
	system.activated = handle
	return system.err
}
func (system *fakeSystem) SetWindowState(_ Handle, state State, enabled bool) error {
	system.stateSet = state
	system.stateValue = enabled
	return system.err
}
func (system *fakeSystem) WindowState(_ Handle, state State) (bool, error) {
	return system.style[state], system.err
}
func (system *fakeSystem) IsTopMost(Handle) (bool, error) {
	return system.topMost, system.err
}
func (system *fakeSystem) SetTopMost(_ Handle, enabled bool) error {
	system.topSet = enabled
	return system.err
}
func (system *fakeSystem) CloseWindow(handle Handle) error {
	system.closed = handle
	return system.err
}

func newFakeBackend() (*Backend, *fakeSystem) {
	system := &fakeSystem{
		foreground: 7,
		valid:      map[Handle]bool{7: true, 8: true},
		byPID:      map[uint32]Handle{42: 8},
		pids:       map[Handle]uint32{7: 41, 8: 42},
		titles:     map[Handle]string{7: "active", 8: "target"},
		windowRect: Rect{X: 1, Y: 2, Width: 300, Height: 200},
		clientRect: Rect{X: 3, Y: 4, Width: 280, Height: 160},
		style:      map[State]bool{StateMinimized: true},
		topMost:    true,
	}
	return New(system), system
}

func TestBackendResolvesActivePIDAndHandle(t *testing.T) {
	t.Parallel()
	backend, _ := newFakeBackend()

	active, err := backend.Active()
	if err != nil || active != 7 {
		t.Fatalf("Active() = %#x, %v", active, err)
	}
	byPID, err := backend.Resolve(42, false)
	if err != nil || byPID != 8 {
		t.Fatalf("Resolve(pid) = %#x, %v", byPID, err)
	}
	byHandle, err := backend.Resolve(8, true)
	if err != nil || byHandle != 8 {
		t.Fatalf("Resolve(handle) = %#x, %v", byHandle, err)
	}
	pid, err := backend.PID(active)
	if err != nil || pid != 41 {
		t.Fatalf("PID() = %d, %v", pid, err)
	}
}

func TestBackendRejectsMissingAndInvalidTargets(t *testing.T) {
	t.Parallel()
	backend, system := newFakeBackend()

	if _, err := backend.Resolve(0, false); !errors.Is(err, ErrWindowNotFound) {
		t.Fatalf("Resolve(0) error = %v", err)
	}
	if _, err := backend.Resolve(99, true); !errors.Is(err, ErrInvalidWindow) {
		t.Fatalf("Resolve(invalid handle) error = %v", err)
	}
	system.err = errors.New("enumeration failed")
	if _, err := backend.Resolve(99, false); !errors.Is(err, ErrWindowNotFound) {
		t.Fatalf("Resolve(missing pid) error = %v", err)
	}
}

func TestBackendRejectsPIDOutsideDWORDOn64Bit(t *testing.T) {
	if ^uint(0)>>32 == 0 {
		t.Skip("Go int cannot represent a value above DWORD on this architecture")
	}
	t.Parallel()
	backend, _ := newFakeBackend()
	overflow := uint64(^uint32(0))
	overflow++

	if _, err := backend.Resolve(int(overflow), false); !errors.Is(err, ErrWindowNotFound) {
		t.Fatalf("Resolve(overflow pid) error = %v", err)
	}
}

func TestBackendReadsTitleAndBounds(t *testing.T) {
	t.Parallel()
	backend, _ := newFakeBackend()

	title, err := backend.Title(8)
	if err != nil || title != "target" {
		t.Fatalf("Title() = %q, %v", title, err)
	}
	window, err := backend.Bounds(8, false)
	if err != nil || window.Width != 300 {
		t.Fatalf("Bounds(window) = %+v, %v", window, err)
	}
	client, err := backend.Bounds(8, true)
	if err != nil || client.X != 3 || client.Height != 160 {
		t.Fatalf("Bounds(client) = %+v, %v", client, err)
	}
}

func TestBackendRejectsEmptyTitleAndInvalidBounds(t *testing.T) {
	t.Parallel()
	backend, system := newFakeBackend()

	system.titles[8] = ""
	if _, err := backend.Title(8); !errors.Is(err, ErrWindowNotFound) {
		t.Fatalf("empty title error = %v", err)
	}
	system.windowRect = Rect{}
	if _, err := backend.Bounds(8, false); !errors.Is(err, ErrOperation) {
		t.Fatalf("empty bounds error = %v", err)
	}
}

func TestBackendControlsResolvedWindow(t *testing.T) {
	t.Parallel()
	backend, system := newFakeBackend()

	if err := backend.Activate(8); err != nil || system.activated != 8 {
		t.Fatalf("Activate() error = %v, target = %#x", err, system.activated)
	}
	if err := backend.SetState(8, StateMaximized, true); err != nil {
		t.Fatalf("SetState() error = %v", err)
	}
	if system.stateSet != StateMaximized || !system.stateValue {
		t.Fatalf("state mutation = %v, %v", system.stateSet, system.stateValue)
	}
	minimized, err := backend.State(8, StateMinimized)
	if err != nil || !minimized {
		t.Fatalf("State() = %v, %v", minimized, err)
	}
	topMost, err := backend.TopMost(8)
	if err != nil || !topMost {
		t.Fatalf("TopMost() = %v, %v", topMost, err)
	}
	if err := backend.SetTopMost(8, false); err != nil || system.topSet {
		t.Fatalf("SetTopMost(false) error = %v, value = %v", err, system.topSet)
	}
	if err := backend.Close(8); err != nil || system.closed != 8 {
		t.Fatalf("Close() error = %v, target = %#x", err, system.closed)
	}
}

func TestBackendPreservesOperationErrors(t *testing.T) {
	t.Parallel()
	backend, system := newFakeBackend()
	system.err = errors.New("denied")

	if err := backend.Activate(8); !errors.Is(err, ErrOperation) {
		t.Fatalf("Activate() error = %v", err)
	}
	if _, err := backend.State(8, StateMaximized); !errors.Is(err, ErrOperation) {
		t.Fatalf("State() error = %v", err)
	}
	if err := backend.Close(8); !errors.Is(err, ErrOperation) {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestBackendSelectionOnlyChangesAfterSuccessfulResolution(t *testing.T) {
	t.Parallel()
	backend, _ := newFakeBackend()

	if err := backend.Select(42, false); err != nil {
		t.Fatalf("Select(pid): %v", err)
	}
	if got := backend.Selected(); got != 8 {
		t.Fatalf("Selected() = %#x, want 8", got)
	}
	if err := backend.Select(99, true); err == nil {
		t.Fatal("Select(invalid) succeeded")
	}
	if got := backend.Selected(); got != 8 {
		t.Fatalf("failed Select changed selection to %#x", got)
	}
}
