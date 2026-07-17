//go:build darwin && !cgo

package robotgo

import (
	"errors"
	"fmt"

	"github.com/marang/robotgo/internal/darwininput"
)

type darwinInputBackend struct {
	core *darwininput.Backend
}

var pureGoDarwinInput = &darwinInputBackend{core: darwininput.New()}

func platformPureGoInputBackend() pureGoInputBackend { return pureGoDarwinInput }

func closePureGoPlatformInput() error { return pureGoDarwinInput.Close() }

func (*darwinInputBackend) Name() string { return featureBackendPureGoQuartzInput }

func translatePureGoDarwinInputError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, darwininput.ErrUnsupported):
		return fmt.Errorf("%w: %w", ErrNotSupported, err)
	case errors.Is(err, darwininput.ErrPermission):
		return fmt.Errorf("%w: %w", ErrPermissionDenied, err)
	case errors.Is(err, darwininput.ErrOwnership):
		return fmt.Errorf("%w: %w", ErrInputOwnership, err)
	default:
		return err
	}
}

func (*darwinInputBackend) KeyboardReady() error {
	return fmt.Errorf("%w: Pure-Go macOS keyboard injection is not implemented", ErrNotSupported)
}

func (backend *darwinInputBackend) MouseReady() error {
	return translatePureGoDarwinInputError(backend.core.MouseReady())
}

func (*darwinInputBackend) Key(pureGoKeyEvent) error {
	return fmt.Errorf("%w: Pure-Go macOS keyboard injection is not implemented", ErrNotSupported)
}

func (*darwinInputBackend) Text(pureGoTextEvent) error {
	return fmt.Errorf("%w: Pure-Go macOS text injection is not implemented", ErrNotSupported)
}

func (backend *darwinInputBackend) MoveAbsolute(x, y int, _ []int) error {
	return translatePureGoDarwinInputError(backend.core.MoveAbsolute(x, y))
}

func (backend *darwinInputBackend) MoveRelative(x, y int) error {
	return translatePureGoDarwinInputError(backend.core.MoveRelative(x, y))
}

func (backend *darwinInputBackend) MoveSmooth(
	x, y int,
	relative bool,
	lowDelay, highDelay float64,
) error {
	return translatePureGoDarwinInputError(
		backend.core.MoveSmooth(x, y, relative, lowDelay, highDelay),
	)
}

func (backend *darwinInputBackend) DragSmooth(x, y int, lowDelay, highDelay float64) error {
	return translatePureGoDarwinInputError(backend.core.DragSmooth(x, y, lowDelay, highDelay))
}

func (backend *darwinInputBackend) Location() (int, int, error) {
	x, y, err := backend.core.Location()
	return x, y, translatePureGoDarwinInputError(err)
}

func (backend *darwinInputBackend) Click(button string, double bool) error {
	return translatePureGoDarwinInputError(backend.core.Click(button, double))
}

func (backend *darwinInputBackend) Toggle(button string, down bool) error {
	return translatePureGoDarwinInputError(backend.core.Toggle(button, down))
}

func (backend *darwinInputBackend) Scroll(x, y int) error {
	return translatePureGoDarwinInputError(backend.core.Scroll(x, y))
}

func (backend *darwinInputBackend) Close() error {
	return translatePureGoDarwinInputError(backend.core.Close())
}
