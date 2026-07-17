//go:build !cgo

package robotgo

import (
	"fmt"

	"github.com/marang/robotgo/internal/windowswindow"
)

func pureGoWindowBackend() (*windowswindow.Backend, error) {
	backend := platformPureGoWindowBackend()
	if backend == nil {
		return nil, fmt.Errorf("%w: no Pure-Go window backend is active", ErrNotSupported)
	}
	return backend, nil
}

func pureGoWindowCapability() FeatureCapability {
	if platformPureGoWindowBackend() == nil {
		return FeatureCapability{
			Reason: ErrNotSupported.Error(),
			Notes:  "no matching Pure-Go window backend is active in this build",
		}
	}
	return FeatureCapability{
		Available: true,
		Backend:   featureBackendPureGoWindows,
		Reason:    "Pure-Go Win32 window introspection and control are available",
		Notes:     "PID targets prefer visible unowned top-level windows; Windows foreground-activation policy still applies",
	}
}

func pureGoWindowActive() (windowswindow.Handle, error) {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return 0, err
	}
	return backend.Active()
}

func pureGoWindowResolve(target int, isHandle bool) (windowswindow.Handle, error) {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return 0, err
	}
	return backend.Resolve(target, isHandle)
}

func pureGoWindowTitle(args ...int) (string, error) {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		handle, activeErr := backend.Active()
		if activeErr != nil {
			return "", activeErr
		}
		return backend.Title(handle)
	}
	handle, err := backend.Resolve(args[0], len(args) > 1 || currentTreatAsHandle())
	if err != nil {
		return "", err
	}
	return backend.Title(handle)
}

func pureGoWindowBounds(target int, isHandle, client bool) (int, int, int, int) {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return 0, 0, 0, 0
	}
	handle, err := backend.Resolve(target, isHandle)
	if err != nil {
		return 0, 0, 0, 0
	}
	rect, err := backend.Bounds(handle, client)
	if err != nil {
		return 0, 0, 0, 0
	}
	return rect.X, rect.Y, rect.Width, rect.Height
}
