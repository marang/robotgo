//go:build !darwin

package darwinwindow

import (
	"fmt"

	"github.com/marang/robotgo/internal/windowbackend"
)

type unsupportedSystem struct{}

func newNativeSystem() System { return unsupportedSystem{} }

func (unsupportedSystem) Ready() error {
	return fmt.Errorf("%w: macOS frameworks are unavailable", ErrUnsupported)
}

func (unsupportedSystem) ActiveWindow() (windowbackend.Handle, error) {
	return 0, ErrUnsupported
}

func (unsupportedSystem) WindowExists(windowbackend.Handle) (bool, error) {
	return false, ErrUnsupported
}

func (unsupportedSystem) FindWindowByPID(int32) (windowbackend.Handle, error) {
	return 0, ErrUnsupported
}

func (unsupportedSystem) WindowPID(windowbackend.Handle) (int32, error) {
	return 0, ErrUnsupported
}

func (unsupportedSystem) WindowTitle(windowbackend.Handle) (string, error) {
	return "", ErrUnsupported
}

func (unsupportedSystem) WindowRect(windowbackend.Handle) (windowbackend.Rect, error) {
	return windowbackend.Rect{}, ErrUnsupported
}

func (unsupportedSystem) RaiseWindow(windowbackend.Handle) error { return ErrUnsupported }
func (unsupportedSystem) SetMinimized(windowbackend.Handle, bool) error {
	return ErrUnsupported
}
func (unsupportedSystem) IsMinimized(windowbackend.Handle) (bool, error) {
	return false, ErrUnsupported
}
func (unsupportedSystem) CloseWindow(windowbackend.Handle) error { return ErrUnsupported }
func (unsupportedSystem) Close() error                           { return nil }
