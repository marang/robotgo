//go:build !cgo

package robotgo

import (
	"errors"
	"fmt"

	"github.com/marang/robotgo/internal/windowbackend"
)

func pureGoWindowBackend() (windowbackend.Backend, error) {
	backend := platformPureGoWindowBackend()
	if backend == nil {
		return nil, fmt.Errorf("%w: no Pure-Go window backend is active", ErrNotSupported)
	}
	return publicPureGoWindowBackend{Backend: backend}, nil
}

type publicPureGoWindowBackend struct {
	windowbackend.Backend
}

func (backend publicPureGoWindowBackend) Activate(handle windowbackend.Handle) error {
	return translatePureGoWindowError(backend.Backend.Activate(handle))
}

func (backend publicPureGoWindowBackend) SetState(
	handle windowbackend.Handle,
	state windowbackend.State,
	enabled bool,
) error {
	return translatePureGoWindowError(backend.Backend.SetState(handle, state, enabled))
}

func (backend publicPureGoWindowBackend) State(
	handle windowbackend.Handle,
	state windowbackend.State,
) (bool, error) {
	enabled, err := backend.Backend.State(handle, state)
	return enabled, translatePureGoWindowError(err)
}

func (backend publicPureGoWindowBackend) TopMost(handle windowbackend.Handle) (bool, error) {
	enabled, err := backend.Backend.TopMost(handle)
	return enabled, translatePureGoWindowError(err)
}

func (backend publicPureGoWindowBackend) SetTopMost(
	handle windowbackend.Handle,
	enabled bool,
) error {
	return translatePureGoWindowError(backend.Backend.SetTopMost(handle, enabled))
}

func (backend publicPureGoWindowBackend) Close(handle windowbackend.Handle) error {
	return translatePureGoWindowError(backend.Backend.Close(handle))
}

func translatePureGoWindowError(err error) error {
	if err == nil || !errors.Is(err, windowbackend.ErrUnsupported) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrNotSupported, err)
}

func pureGoWindowCapability() FeatureCapability {
	return platformPureGoWindowCapability()
}

func pureGoWindowActive() (windowbackend.Handle, error) {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return 0, err
	}
	return backend.Active()
}

func pureGoWindowResolve(target int, isHandle bool) (windowbackend.Handle, error) {
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
