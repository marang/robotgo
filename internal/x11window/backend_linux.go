//go:build linux

// Package x11window implements the Pure-Go X11 window contract.
package x11window

import (
	"fmt"
	"math"
	"sync"

	"github.com/marang/robotgo/internal/windowbackend"
)

var (
	// ErrWindowNotFound reports that no X11 client matched the target.
	ErrWindowNotFound = windowbackend.ErrWindowNotFound
	// ErrInvalidWindow reports a zero, stale, or invalid XID.
	ErrInvalidWindow = windowbackend.ErrInvalidWindow
	// ErrOperation reports a failed X11 window operation.
	ErrOperation = windowbackend.ErrOperation
)

// System abstracts X11 discovery, property queries, and EWMH requests.
type System interface {
	ActiveWindow() (windowbackend.Handle, error)
	WindowExists(windowbackend.Handle) (bool, error)
	FindWindowByPID(uint32) (windowbackend.Handle, error)
	WindowProcessID(windowbackend.Handle) (uint32, error)
	WindowText(windowbackend.Handle) (string, error)
	WindowRect(windowbackend.Handle) (windowbackend.Rect, error)
	ClientRect(windowbackend.Handle) (windowbackend.Rect, error)
	ActivateWindow(windowbackend.Handle) error
	SetWindowState(windowbackend.Handle, windowbackend.State, bool) error
	WindowState(windowbackend.Handle, windowbackend.State) (bool, error)
	IsTopMost(windowbackend.Handle) (bool, error)
	SetTopMost(windowbackend.Handle, bool) error
	CloseWindow(windowbackend.Handle) error
}

// Backend resolves public targets and validates every operation before it is
// delegated to X11.
type Backend struct {
	system System

	mu       sync.RWMutex
	selected windowbackend.Handle
}

var _ windowbackend.Backend = (*Backend)(nil)

// New constructs a Pure-Go X11 window backend.
func New(system System) *Backend {
	return &Backend{system: system}
}

// Active returns the current valid active or focused X11 window.
func (backend *Backend) Active() (windowbackend.Handle, error) {
	handle, err := backend.system.ActiveWindow()
	if err != nil {
		return 0, fmt.Errorf("%w: active X11 window: %w", ErrOperation, err)
	}
	if err := backend.validate(handle); err != nil {
		return 0, fmt.Errorf("%w: active X11 window", err)
	}
	return handle, nil
}

// Resolve maps a positive process ID or explicit XID to a valid window.
func (backend *Backend) Resolve(target int, isHandle bool) (windowbackend.Handle, error) {
	if target <= 0 {
		return 0, fmt.Errorf("%w: target must be positive, got %d", ErrWindowNotFound, target)
	}
	if uint64(target) > math.MaxUint32 {
		return 0, fmt.Errorf("%w: target %d exceeds the X11 CARD32 range", ErrWindowNotFound, target)
	}
	if isHandle {
		handle := windowbackend.Handle(uintptr(target))
		if err := backend.validate(handle); err != nil {
			return 0, err
		}
		return handle, nil
	}
	handle, err := backend.system.FindWindowByPID(uint32(target))
	if err != nil {
		return 0, fmt.Errorf("%w: pid %d: %w", ErrWindowNotFound, target, err)
	}
	if err := backend.validate(handle); err != nil {
		return 0, fmt.Errorf("%w: pid %d resolved to %#x", err, target, uintptr(handle))
	}
	return handle, nil
}

// Select stores a resolved compatibility handle.
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
func (backend *Backend) Selected() windowbackend.Handle {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	return backend.selected
}

// PID returns the owning process ID from _NET_WM_PID.
func (backend *Backend) PID(handle windowbackend.Handle) (int, error) {
	if err := backend.validate(handle); err != nil {
		return 0, err
	}
	pid, err := backend.system.WindowProcessID(handle)
	if err != nil {
		return 0, fmt.Errorf("%w: get pid for XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	if pid == 0 {
		return 0, fmt.Errorf("%w: XID %#x has no process", ErrWindowNotFound, uintptr(handle))
	}
	if uint64(pid) > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("%w: pid %d exceeds the Go int range", ErrOperation, pid)
	}
	return int(pid), nil
}

// Title returns a non-empty EWMH or ICCCM title.
func (backend *Backend) Title(handle windowbackend.Handle) (string, error) {
	if err := backend.validate(handle); err != nil {
		return "", err
	}
	title, err := backend.system.WindowText(handle)
	if err != nil {
		return "", fmt.Errorf("%w: get title for XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	if title == "" {
		return "", fmt.Errorf("%w: XID %#x has no title", ErrWindowNotFound, uintptr(handle))
	}
	return title, nil
}

// Bounds returns outer or client geometry in root coordinates.
func (backend *Backend) Bounds(handle windowbackend.Handle, client bool) (windowbackend.Rect, error) {
	if err := backend.validate(handle); err != nil {
		return windowbackend.Rect{}, err
	}
	var (
		rect windowbackend.Rect
		err  error
	)
	if client {
		rect, err = backend.system.ClientRect(handle)
	} else {
		rect, err = backend.system.WindowRect(handle)
	}
	if err != nil {
		return windowbackend.Rect{}, fmt.Errorf("%w: get bounds for XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	if rect.Width <= 0 || rect.Height <= 0 {
		return windowbackend.Rect{}, fmt.Errorf(
			"%w: XID %#x returned invalid bounds %+v",
			ErrOperation,
			uintptr(handle),
			rect,
		)
	}
	return rect, nil
}

// Activate requests EWMH activation for a valid window.
func (backend *Backend) Activate(handle windowbackend.Handle) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.ActivateWindow(handle); err != nil {
		return fmt.Errorf("%w: activate XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	return nil
}

// SetState changes minimized or maximized state through the window manager.
func (backend *Backend) SetState(handle windowbackend.Handle, state windowbackend.State, enabled bool) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := validateState(state); err != nil {
		return err
	}
	if err := backend.system.SetWindowState(handle, state, enabled); err != nil {
		return fmt.Errorf("%w: change state for XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	return nil
}

// State queries minimized or maximized state from _NET_WM_STATE.
func (backend *Backend) State(handle windowbackend.Handle, state windowbackend.State) (bool, error) {
	if err := backend.validate(handle); err != nil {
		return false, err
	}
	if err := validateState(state); err != nil {
		return false, err
	}
	enabled, err := backend.system.WindowState(handle, state)
	if err != nil {
		return false, fmt.Errorf("%w: query state for XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	return enabled, nil
}

// TopMost reports the EWMH ABOVE state.
func (backend *Backend) TopMost(handle windowbackend.Handle) (bool, error) {
	if err := backend.validate(handle); err != nil {
		return false, err
	}
	enabled, err := backend.system.IsTopMost(handle)
	if err != nil {
		return false, fmt.Errorf("%w: query topmost for XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	return enabled, nil
}

// SetTopMost changes the EWMH ABOVE state.
func (backend *Backend) SetTopMost(handle windowbackend.Handle, enabled bool) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.SetTopMost(handle, enabled); err != nil {
		return fmt.Errorf("%w: set topmost for XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	return nil
}

// Close requests a graceful EWMH close.
func (backend *Backend) Close(handle windowbackend.Handle) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.CloseWindow(handle); err != nil {
		return fmt.Errorf("%w: close XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	return nil
}

func (backend *Backend) validate(handle windowbackend.Handle) error {
	if handle == 0 || uint64(handle) > math.MaxUint32 {
		return fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	valid, err := backend.system.WindowExists(handle)
	if err != nil {
		return fmt.Errorf("%w: validate XID %#x: %w", ErrOperation, uintptr(handle), err)
	}
	if !valid {
		return fmt.Errorf("%w: XID %#x", ErrInvalidWindow, uintptr(handle))
	}
	return nil
}

func validateState(state windowbackend.State) error {
	switch state {
	case windowbackend.StateMinimized, windowbackend.StateMaximized:
		return nil
	default:
		return fmt.Errorf("%w: unknown X11 window state %d", ErrOperation, state)
	}
}
