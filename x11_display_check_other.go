//go:build cgo && !linux
// +build cgo,!linux

package robotgo

import "fmt"

func nativeX11BackendCompiled() bool { return false }

func lockNativeX11Display() func() { return func() {} }

func configuredX11DisplaySelected() bool { return false }

func nativeX11DisplayReadyLocked() error {
	return fmt.Errorf("%w: native X11 backend is not compiled", ErrNotSupported)
}

func nativeX11InputReadyLocked() error {
	return fmt.Errorf("%w: native X11 input backend is not compiled", ErrNotSupported)
}

func nativeX11CapabilityErrors() (displayErr error, inputErr error) {
	err := nativeX11DisplayReadyLocked()
	return err, err
}

func runtimeX11CapabilityErrors() (displayErr error, inputErr error) {
	return nativeX11CapabilityErrors()
}

func nativeX11ProtocolVersion() (major, minor int, negotiated bool) {
	return 0, 0, false
}

func x11MainDisplayAvailableLocked() bool { return false }
