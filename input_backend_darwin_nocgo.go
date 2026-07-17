//go:build darwin && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"time"

	"github.com/marang/robotgo/internal/darwininput"
)

const maxDarwinTextDelayMilliseconds = int64(^uint64(0)>>1) / int64(time.Millisecond)

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

func (backend *darwinInputBackend) KeyboardReady() error {
	return translatePureGoDarwinInputError(backend.core.KeyboardReady())
}

func (backend *darwinInputBackend) MouseReady() error {
	return translatePureGoDarwinInputError(backend.core.MouseReady())
}

func (backend *darwinInputBackend) Key(event pureGoKeyEvent) error {
	return translatePureGoDarwinInputError(backend.core.Key(darwininput.KeyEvent{
		Key:       event.Key,
		Modifiers: event.Modifiers,
		PID:       event.PID,
		Down:      event.Down,
		Tap:       event.Tap,
	}))
}

func (backend *darwinInputBackend) Text(event pureGoTextEvent) error {
	if event.Delay < 0 {
		return errors.New("robotgo: macOS text delay must be non-negative")
	}
	if int64(event.Delay) > maxDarwinTextDelayMilliseconds {
		return fmt.Errorf(
			"robotgo: macOS text delay %dms exceeds time.Duration",
			event.Delay,
		)
	}
	return translatePureGoDarwinInputError(backend.core.Text(darwininput.TextEvent{
		Text:  event.Text,
		PID:   event.PID,
		Delay: time.Duration(event.Delay) * time.Millisecond,
	}))
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
