// Package windowbackend defines the platform-neutral contract shared by
// Pure-Go window implementations.
package windowbackend

import "errors"

var (
	// ErrWindowNotFound reports that no window matched the requested target.
	ErrWindowNotFound = errors.New("window not found")
	// ErrInvalidWindow reports a zero, stale, or otherwise invalid native handle.
	ErrInvalidWindow = errors.New("invalid window handle")
	// ErrOperation reports a failed native window operation.
	ErrOperation = errors.New("window operation failed")
	// ErrUnsupported reports a missing platform or window-manager capability.
	ErrUnsupported = errors.New("window operation unsupported")
	// ErrPermission reports a platform security policy that denied window access.
	ErrPermission = errors.New("window operation permission denied")
)

// Handle is an opaque native top-level window identifier.
type Handle uintptr

// Rect contains screen-relative window geometry.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// State identifies a queryable and mutable presentation state.
type State uint8

const (
	// StateMinimized identifies a minimized window.
	StateMinimized State = iota
	// StateMaximized identifies a maximized window.
	StateMaximized
)

// Backend is the behavior required by the public Pure-Go window adapter.
// Implementations keep platform-specific discovery, validation, and mutation
// below this boundary.
type Backend interface {
	Active() (Handle, error)
	Resolve(target int, isHandle bool) (Handle, error)
	Select(target int, isHandle bool) error
	Selected() Handle
	PID(Handle) (int, error)
	Title(Handle) (string, error)
	Bounds(Handle, bool) (Rect, error)
	Activate(Handle) error
	SetState(Handle, State, bool) error
	State(Handle, State) (bool, error)
	TopMost(Handle) (bool, error)
	SetTopMost(Handle, bool) error
	Close(Handle) error
}
