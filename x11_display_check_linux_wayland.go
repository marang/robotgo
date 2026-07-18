//go:build cgo && linux && wayland

package robotgo

import "fmt"

func nativeX11BackendCompiled() bool { return false }

func lockNativeX11Display() func() { return func() {} }

func configuredX11DisplaySelected() bool { return false }

func nativeX11DisplayReadyLocked() error {
	return fmt.Errorf(
		"%w: native X11 backend is not compiled in a Wayland-enabled build",
		ErrNotSupported,
	)
}

func nativeX11InputReadyLocked() error {
	return fmt.Errorf(
		"%w: native X11 input backend is not compiled in a Wayland-enabled build",
		ErrNotSupported,
	)
}

func nativeX11CapabilityErrors() (displayErr error, inputErr error) {
	return nativeX11DisplayReadyLocked(), nativeX11InputReadyLocked()
}

func runtimeX11CapabilityErrors() (displayErr error, inputErr error) {
	if displayErr = linuxX11SessionConflictError("display capabilities"); displayErr != nil {
		return displayErr, nativeX11InputReadyLocked()
	}
	_, displayErr = x11DisplayBounds(0)
	return displayErr, nativeX11InputReadyLocked()
}

func nativeX11ProtocolVersion() (major, minor int, negotiated bool) {
	return 0, 0, false
}

func x11MainDisplayAvailableLocked() bool {
	return false
}
