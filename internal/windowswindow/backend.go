package windowswindow

import (
	"errors"
	"fmt"
	"sync"

	"github.com/marang/robotgo/internal/windowbackend"
)

var (
	// ErrWindowNotFound reports that no window matched the requested target.
	ErrWindowNotFound = errors.New("windows window not found")
	// ErrInvalidWindow reports a zero, stale, or otherwise invalid HWND.
	ErrInvalidWindow = errors.New("invalid Windows window handle")
	// ErrOperation reports a failed Win32 window operation.
	ErrOperation = errors.New("windows window operation failed")
)

// Handle is a platform-neutral representation of a Windows HWND.
type Handle = windowbackend.Handle

// Rect contains screen-relative window geometry.
type Rect = windowbackend.Rect

// State identifies a queryable and mutable Windows presentation state.
type State = windowbackend.State

const (
	// StateMinimized identifies the minimized/iconic state.
	StateMinimized = windowbackend.StateMinimized
	// StateMaximized identifies the maximized/zoomed state.
	StateMaximized = windowbackend.StateMaximized
)

// System abstracts the Win32 calls used by Backend.
type System interface {
	ForegroundWindow() Handle
	IsWindow(Handle) bool
	FindWindowByPID(uint32) (Handle, error)
	WindowProcessID(Handle) (uint32, error)
	WindowText(Handle) (string, error)
	WindowRect(Handle) (Rect, error)
	ClientRect(Handle) (Rect, error)
	SetForegroundWindow(Handle) error
	SetWindowState(Handle, State, bool) error
	WindowState(Handle, State) (bool, error)
	IsTopMost(Handle) (bool, error)
	SetTopMost(Handle, bool) error
	CloseWindow(Handle) error
}

// Backend resolves window targets and enforces the public operation contract.
type Backend struct {
	system System

	mu       sync.RWMutex
	selected Handle
}

var _ windowbackend.Backend = (*Backend)(nil)

// New constructs a Backend around a Win32 system implementation.
func New(system System) *Backend {
	return &Backend{system: system}
}

// Active returns the current valid foreground window.
func (backend *Backend) Active() (Handle, error) {
	handle := backend.system.ForegroundWindow()
	if err := backend.validate(handle); err != nil {
		return 0, fmt.Errorf("%w: foreground window", err)
	}
	return handle, nil
}

// Resolve maps either a PID or an explicit HWND to a valid window.
func (backend *Backend) Resolve(target int, isHandle bool) (Handle, error) {
	if target <= 0 {
		return 0, fmt.Errorf("%w: target must be positive, got %d", ErrWindowNotFound, target)
	}
	if isHandle {
		handle := Handle(uintptr(target))
		if err := backend.validate(handle); err != nil {
			return 0, err
		}
		return handle, nil
	}
	if uint64(target) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("%w: pid %d exceeds the Windows DWORD range", ErrWindowNotFound, target)
	}
	handle, err := backend.system.FindWindowByPID(uint32(target))
	if err != nil {
		return 0, fmt.Errorf("%w: pid %d: %v", ErrWindowNotFound, target, err)
	}
	if err := backend.validate(handle); err != nil {
		return 0, fmt.Errorf("%w: pid %d resolved to %#x", err, target, uintptr(handle))
	}
	return handle, nil
}

// Select stores a successfully resolved compatibility handle.
func (backend *Backend) Select(target int, isHandle bool) error {
	handle, err := backend.Resolve(target, isHandle)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	backend.selected = handle
	backend.mu.Unlock()
	return nil
}

// Selected returns the compatibility handle stored by Select.
func (backend *Backend) Selected() Handle {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	return backend.selected
}

// PID returns the owning process ID for a valid window.
func (backend *Backend) PID(handle Handle) (int, error) {
	if err := backend.validate(handle); err != nil {
		return 0, err
	}
	pid, err := backend.system.WindowProcessID(handle)
	if err != nil {
		return 0, fmt.Errorf("%w: get pid for handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	if pid == 0 {
		return 0, fmt.Errorf("%w: handle %#x has no process", ErrWindowNotFound, uintptr(handle))
	}
	if uint64(pid) > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("%w: pid %d exceeds the Go int range", ErrOperation, pid)
	}
	return int(pid), nil
}

// Title returns a non-empty title for a valid window.
func (backend *Backend) Title(handle Handle) (string, error) {
	if err := backend.validate(handle); err != nil {
		return "", err
	}
	title, err := backend.system.WindowText(handle)
	if err != nil {
		return "", fmt.Errorf("%w: get title for handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	if title == "" {
		return "", fmt.Errorf("%w: handle %#x has no title", ErrWindowNotFound, uintptr(handle))
	}
	return title, nil
}

// Bounds returns outer or client geometry for a valid window.
func (backend *Backend) Bounds(handle Handle, client bool) (Rect, error) {
	if err := backend.validate(handle); err != nil {
		return Rect{}, err
	}
	var (
		rect Rect
		err  error
	)
	if client {
		rect, err = backend.system.ClientRect(handle)
	} else {
		rect, err = backend.system.WindowRect(handle)
	}
	if err != nil {
		return Rect{}, fmt.Errorf("%w: get bounds for handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	if rect.Width <= 0 || rect.Height <= 0 {
		return Rect{}, fmt.Errorf("%w: handle %#x returned invalid bounds %+v", ErrOperation, uintptr(handle), rect)
	}
	return rect, nil
}

// Activate requests foreground activation for a valid window.
func (backend *Backend) Activate(handle Handle) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.SetForegroundWindow(handle); err != nil {
		return fmt.Errorf("%w: activate handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	return nil
}

// SetState changes a minimized or maximized window state.
func (backend *Backend) SetState(handle Handle, state State, enabled bool) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.SetWindowState(handle, state, enabled); err != nil {
		return fmt.Errorf("%w: change state for handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	return nil
}

// State queries a minimized or maximized window state.
func (backend *Backend) State(handle Handle, state State) (bool, error) {
	if err := backend.validate(handle); err != nil {
		return false, err
	}
	enabled, err := backend.system.WindowState(handle, state)
	if err != nil {
		return false, fmt.Errorf("%w: query state for handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	return enabled, nil
}

// TopMost reports whether a window has topmost extended style.
func (backend *Backend) TopMost(handle Handle) (bool, error) {
	if err := backend.validate(handle); err != nil {
		return false, err
	}
	enabled, err := backend.system.IsTopMost(handle)
	if err != nil {
		return false, fmt.Errorf("%w: query topmost for handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	return enabled, nil
}

// SetTopMost changes a window's topmost state without moving or resizing it.
func (backend *Backend) SetTopMost(handle Handle, enabled bool) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.SetTopMost(handle, enabled); err != nil {
		return fmt.Errorf("%w: set topmost for handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	return nil
}

// Close posts WM_CLOSE to a valid window.
func (backend *Backend) Close(handle Handle) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.CloseWindow(handle); err != nil {
		return fmt.Errorf("%w: close handle %#x: %v", ErrOperation, uintptr(handle), err)
	}
	return nil
}

func (backend *Backend) validate(handle Handle) error {
	if handle == 0 || !backend.system.IsWindow(handle) {
		return fmt.Errorf("%w: %#x", ErrInvalidWindow, uintptr(handle))
	}
	return nil
}
