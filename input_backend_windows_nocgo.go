//go:build windows && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"time"

	"github.com/marang/robotgo/internal/windowsinput"
)

type windowsInputBackend struct {
	core *windowsinput.Backend
}

var pureGoWindowsInput = &windowsInputBackend{core: windowsinput.New()}

func platformPureGoInputBackend() pureGoInputBackend { return pureGoWindowsInput }

func closePureGoPlatformInput() error { return pureGoWindowsInput.Close() }

func (*windowsInputBackend) Name() string { return featureBackendPureGoWindows }

func translatePureGoWindowsInputError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, windowsinput.ErrUnsupported):
		return fmt.Errorf("%w: %w", ErrNotSupported, err)
	case errors.Is(err, windowsinput.ErrOwnership):
		return fmt.Errorf("%w: %w", ErrInputOwnership, err)
	default:
		return err
	}
}

func (backend *windowsInputBackend) KeyboardReady() error {
	return translatePureGoWindowsInputError(backend.core.KeyboardReady())
}

func (backend *windowsInputBackend) MouseReady() error {
	return translatePureGoWindowsInputError(backend.core.MouseReady())
}

func (backend *windowsInputBackend) Key(event pureGoKeyEvent) error {
	return translatePureGoWindowsInputError(backend.core.Key(windowsinput.KeyEvent{
		Key: event.Key, Modifiers: append([]string(nil), event.UserModifiers...),
		PID: event.PID, Down: event.Down, Tap: event.Tap,
	}))
}

func (backend *windowsInputBackend) Text(event pureGoTextEvent) error {
	return translatePureGoWindowsInputError(backend.core.Text(windowsinput.TextEvent{
		Text: event.Text, PID: event.PID,
		Delay: time.Duration(event.Delay) * time.Millisecond,
	}))
}

func (backend *windowsInputBackend) MoveAbsolute(x, y int, displayID []int) error {
	return translatePureGoWindowsInputError(backend.core.MoveAbsolute(x, y, displayID))
}

func (backend *windowsInputBackend) MoveRelative(x, y int) error {
	return translatePureGoWindowsInputError(backend.core.MoveRelative(x, y))
}

func (backend *windowsInputBackend) MoveSmooth(x, y int, relative bool, lowDelay, highDelay float64) error {
	return translatePureGoWindowsInputError(backend.core.MoveSmooth(x, y, relative, lowDelay, highDelay))
}

func (backend *windowsInputBackend) DragSmooth(x, y int, lowDelay, highDelay float64) error {
	return translatePureGoWindowsInputError(backend.core.DragSmooth(x, y, lowDelay, highDelay))
}

func (backend *windowsInputBackend) Location() (int, int, error) {
	x, y, err := backend.core.Location()
	return x, y, translatePureGoWindowsInputError(err)
}

func (backend *windowsInputBackend) Click(button string, double bool) error {
	return translatePureGoWindowsInputError(backend.core.Click(button, double))
}

func (backend *windowsInputBackend) Toggle(button string, down bool) error {
	return translatePureGoWindowsInputError(backend.core.Toggle(button, down))
}

func (backend *windowsInputBackend) Scroll(x, y int) error {
	return translatePureGoWindowsInputError(backend.core.Scroll(x, y))
}

func (backend *windowsInputBackend) Close() error {
	return translatePureGoWindowsInputError(backend.core.Close())
}
