// Package darwinwindow provides RobotGo's Pure-Go macOS window backend.
package darwinwindow

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/marang/robotgo/internal/windowbackend"
)

var (
	// ErrPermission reports missing macOS Accessibility permission.
	ErrPermission = errors.New("Pure-Go macOS window control requires Accessibility permission")
	// ErrUnsupported reports a window operation without a trustworthy macOS equivalent.
	ErrUnsupported = errors.New("Pure-Go macOS window operation is unsupported")
)

// System abstracts the macOS Accessibility and CoreGraphics calls used by
// Backend.
type System interface {
	Ready() error
	ActiveWindow() (windowbackend.Handle, error)
	WindowExists(windowbackend.Handle) (bool, error)
	FindWindowByPID(int32) (windowbackend.Handle, error)
	WindowPID(windowbackend.Handle) (int32, error)
	WindowTitle(windowbackend.Handle) (string, error)
	WindowRect(windowbackend.Handle) (windowbackend.Rect, error)
	RaiseWindow(windowbackend.Handle) error
	SetMinimized(windowbackend.Handle, bool) error
	IsMinimized(windowbackend.Handle) (bool, error)
	CloseWindow(windowbackend.Handle) error
	Close() error
}

// Backend resolves macOS window targets and enforces RobotGo's public window
// contract.
type Backend struct {
	system System

	mu       sync.RWMutex
	selected windowbackend.Handle
}

var _ windowbackend.Backend = (*Backend)(nil)

// New constructs a Backend around a macOS window system implementation.
func New(system System) *Backend {
	return &Backend{system: system}
}

// NewNative constructs a lazily initialized backend backed by macOS system
// frameworks.
func NewNative() *Backend {
	return New(newNativeSystem())
}

// Ready resolves the required system symbols and performs a non-prompting
// Accessibility permission preflight.
func (backend *Backend) Ready() error {
	if backend == nil || backend.system == nil {
		return errors.Join(
			windowbackend.ErrUnsupported,
			fmt.Errorf("%w: backend has no system implementation", ErrUnsupported),
		)
	}
	if err := backend.system.Ready(); err != nil {
		switch {
		case errors.Is(err, ErrPermission):
			return errors.Join(windowbackend.ErrPermission, err)
		case errors.Is(err, ErrUnsupported):
			return errors.Join(windowbackend.ErrUnsupported, err)
		}
		return err
	}
	return nil
}

// CloseSystem releases dynamically loaded framework references. A later
// operation reopens them lazily.
func (backend *Backend) CloseSystem() error {
	if backend == nil || backend.system == nil {
		return nil
	}
	return backend.system.Close()
}

// Active returns the focused window of the frontmost application.
func (backend *Backend) Active() (windowbackend.Handle, error) {
	if err := backend.Ready(); err != nil {
		return 0, err
	}
	handle, err := backend.system.ActiveWindow()
	if err != nil {
		return 0, fmt.Errorf(
			"%w: active macOS window: %w",
			windowbackend.ErrWindowNotFound,
			classifySystemError(err),
		)
	}
	if err := backend.validateReady(handle); err != nil {
		return 0, fmt.Errorf("%w: active macOS window", err)
	}
	return handle, nil
}

// Resolve maps either a PID or explicit CGWindowID to a valid AX window.
func (backend *Backend) Resolve(target int, isHandle bool) (windowbackend.Handle, error) {
	if target <= 0 {
		return 0, fmt.Errorf("%w: target must be positive, got %d", windowbackend.ErrWindowNotFound, target)
	}
	if isHandle {
		handle := windowbackend.Handle(uintptr(target))
		if uint64(handle) > math.MaxUint32 {
			return 0, fmt.Errorf("%w: CGWindowID %#x exceeds uint32", windowbackend.ErrInvalidWindow, uintptr(handle))
		}
		if err := backend.Ready(); err != nil {
			return 0, err
		}
		if err := backend.validateReady(handle); err != nil {
			return 0, err
		}
		return handle, nil
	}
	if int64(target) > math.MaxInt32 {
		return 0, fmt.Errorf("%w: pid %d exceeds macOS pid_t", windowbackend.ErrWindowNotFound, target)
	}
	if err := backend.Ready(); err != nil {
		return 0, err
	}
	handle, err := backend.system.FindWindowByPID(int32(target))
	if err != nil {
		return 0, fmt.Errorf(
			"%w: pid %d: %w",
			windowbackend.ErrWindowNotFound,
			target,
			classifySystemError(err),
		)
	}
	if err := backend.validateReady(handle); err != nil {
		return 0, fmt.Errorf("%w: pid %d resolved to CGWindowID %#x", err, target, uintptr(handle))
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
func (backend *Backend) Selected() windowbackend.Handle {
	backend.mu.RLock()
	defer backend.mu.RUnlock()
	return backend.selected
}

// PID returns the owning process ID for a valid window.
func (backend *Backend) PID(handle windowbackend.Handle) (int, error) {
	if err := backend.validate(handle); err != nil {
		return 0, err
	}
	pid, err := backend.system.WindowPID(handle)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: get pid for CGWindowID %#x: %w",
			windowbackend.ErrOperation,
			uintptr(handle),
			classifySystemError(err),
		)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("%w: CGWindowID %#x has no process", windowbackend.ErrWindowNotFound, uintptr(handle))
	}
	return int(pid), nil
}

// Title returns the Accessibility title for a valid window.
func (backend *Backend) Title(handle windowbackend.Handle) (string, error) {
	if err := backend.validate(handle); err != nil {
		return "", err
	}
	title, err := backend.system.WindowTitle(handle)
	if err != nil {
		return "", fmt.Errorf(
			"%w: get title for CGWindowID %#x: %w",
			windowbackend.ErrOperation,
			uintptr(handle),
			classifySystemError(err),
		)
	}
	if title == "" {
		return "", fmt.Errorf("%w: CGWindowID %#x has no title", windowbackend.ErrWindowNotFound, uintptr(handle))
	}
	return title, nil
}

// Bounds returns AX window-frame geometry. macOS Accessibility does not expose
// a separate cross-application client rectangle, so client and outer requests
// intentionally return the same frame.
func (backend *Backend) Bounds(handle windowbackend.Handle, _ bool) (windowbackend.Rect, error) {
	if err := backend.validate(handle); err != nil {
		return windowbackend.Rect{}, err
	}
	rect, err := backend.system.WindowRect(handle)
	if err != nil {
		return windowbackend.Rect{}, fmt.Errorf(
			"%w: get bounds for CGWindowID %#x: %w",
			windowbackend.ErrOperation,
			uintptr(handle),
			classifySystemError(err),
		)
	}
	if rect.Width <= 0 || rect.Height <= 0 {
		return windowbackend.Rect{}, fmt.Errorf(
			"%w: CGWindowID %#x returned invalid bounds %+v",
			windowbackend.ErrOperation,
			uintptr(handle),
			rect,
		)
	}
	return rect, nil
}

// Activate raises a valid window through macOS Accessibility.
func (backend *Backend) Activate(handle windowbackend.Handle) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.RaiseWindow(handle); err != nil {
		return fmt.Errorf(
			"%w: activate CGWindowID %#x: %w",
			windowbackend.ErrOperation,
			uintptr(handle),
			classifySystemError(err),
		)
	}
	return nil
}

// SetState supports minimized state. macOS does not expose a stable
// cross-application maximized state equivalent to RobotGo's contract.
func (backend *Backend) SetState(
	handle windowbackend.Handle,
	state windowbackend.State,
	enabled bool,
) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	switch state {
	case windowbackend.StateMinimized:
		if err := backend.system.SetMinimized(handle, enabled); err != nil {
			return fmt.Errorf(
				"%w: set minimized state for CGWindowID %#x: %w",
				windowbackend.ErrOperation,
				uintptr(handle),
				classifySystemError(err),
			)
		}
		return nil
	case windowbackend.StateMaximized:
		return fmt.Errorf("%w: %w: maximized state has no reliable macOS Accessibility equivalent", windowbackend.ErrUnsupported, ErrUnsupported)
	default:
		return fmt.Errorf("%w: unknown macOS window state %d", windowbackend.ErrOperation, state)
	}
}

// State supports minimized-state queries and explicitly rejects maximized
// state.
func (backend *Backend) State(
	handle windowbackend.Handle,
	state windowbackend.State,
) (bool, error) {
	if err := backend.validate(handle); err != nil {
		return false, err
	}
	switch state {
	case windowbackend.StateMinimized:
		enabled, err := backend.system.IsMinimized(handle)
		if err != nil {
			return false, fmt.Errorf(
				"%w: query minimized state for CGWindowID %#x: %w",
				windowbackend.ErrOperation,
				uintptr(handle),
				classifySystemError(err),
			)
		}
		return enabled, nil
	case windowbackend.StateMaximized:
		return false, fmt.Errorf("%w: %w: maximized state has no reliable macOS Accessibility equivalent", windowbackend.ErrUnsupported, ErrUnsupported)
	default:
		return false, fmt.Errorf("%w: unknown macOS window state %d", windowbackend.ErrOperation, state)
	}
}

// TopMost returns an explicit unsupported error because macOS Accessibility
// does not expose a trustworthy global topmost state.
func (backend *Backend) TopMost(handle windowbackend.Handle) (bool, error) {
	if err := backend.validate(handle); err != nil {
		return false, err
	}
	return false, fmt.Errorf("%w: %w: topmost state is unavailable through macOS Accessibility", windowbackend.ErrUnsupported, ErrUnsupported)
}

// SetTopMost returns an explicit unsupported error because macOS Accessibility
// does not expose a trustworthy global topmost mutation.
func (backend *Backend) SetTopMost(handle windowbackend.Handle, _ bool) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	return fmt.Errorf("%w: %w: topmost state is unavailable through macOS Accessibility", windowbackend.ErrUnsupported, ErrUnsupported)
}

// Close presses the Accessibility close button for a valid window.
func (backend *Backend) Close(handle windowbackend.Handle) error {
	if err := backend.validate(handle); err != nil {
		return err
	}
	if err := backend.system.CloseWindow(handle); err != nil {
		return fmt.Errorf(
			"%w: close CGWindowID %#x: %w",
			windowbackend.ErrOperation,
			uintptr(handle),
			classifySystemError(err),
		)
	}
	return nil
}

func (backend *Backend) validate(handle windowbackend.Handle) error {
	if err := validateHandle(handle); err != nil {
		return err
	}
	if err := backend.Ready(); err != nil {
		return err
	}
	return backend.validateReady(handle)
}

func (backend *Backend) validateReady(handle windowbackend.Handle) error {
	if err := validateHandle(handle); err != nil {
		return err
	}
	valid, err := backend.system.WindowExists(handle)
	if err != nil {
		return fmt.Errorf(
			"%w: validate CGWindowID %#x: %w",
			windowbackend.ErrOperation,
			uintptr(handle),
			classifySystemError(err),
		)
	}
	if !valid {
		return fmt.Errorf("%w: CGWindowID %#x", windowbackend.ErrInvalidWindow, uintptr(handle))
	}
	return nil
}

func validateHandle(handle windowbackend.Handle) error {
	if handle == 0 || uint64(handle) > math.MaxUint32 {
		return fmt.Errorf("%w: CGWindowID %#x", windowbackend.ErrInvalidWindow, uintptr(handle))
	}
	return nil
}

func classifySystemError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPermission):
		return errors.Join(windowbackend.ErrPermission, err)
	case errors.Is(err, ErrUnsupported):
		return errors.Join(windowbackend.ErrUnsupported, err)
	default:
		return err
	}
}
