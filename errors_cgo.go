//go:build cgo

package robotgo

import (
	"errors"
)

var (
	ErrWaylandDisplay     = errors.New("wayland connect failed")
	ErrNoScreencopy       = errors.New("screencopy manager not available")
	ErrNoOutputs          = errors.New("no outputs")
	ErrDmabufDevice       = errors.New("screencopy dmabuf device unsupported")
	ErrDmabufModifiers    = errors.New("screencopy dmabuf modifiers unsupported")
	ErrDmabufImport       = errors.New("screencopy dmabuf import failed")
	ErrDmabufMap          = errors.New("screencopy dmabuf map failed")
	ErrWaylandPixelFormat = errors.New("screencopy pixel format unsupported")
	ErrWaylandFailed      = errors.New("wayland capture failed")
	ErrPortalFailed       = errors.New("portal capture failed")
	ErrNotSupported       = errors.New("operation not supported on current platform/backend")
	ErrPermissionDenied   = errors.New("permission denied by desktop security policy")
)
