//go:build !cgo

package robotgo

import (
	"errors"
	"testing"

	"github.com/marang/robotgo/internal/windowbackend"
)

type permissionWindowBackend struct{}

func (permissionWindowBackend) Active() (windowbackend.Handle, error) {
	return 0, windowbackend.ErrPermission
}
func (permissionWindowBackend) Resolve(int, bool) (windowbackend.Handle, error) {
	return 0, windowbackend.ErrPermission
}
func (permissionWindowBackend) Select(int, bool) error { return windowbackend.ErrPermission }
func (permissionWindowBackend) Selected() windowbackend.Handle {
	return 0
}
func (permissionWindowBackend) PID(windowbackend.Handle) (int, error) {
	return 0, windowbackend.ErrPermission
}
func (permissionWindowBackend) Title(windowbackend.Handle) (string, error) {
	return "", windowbackend.ErrPermission
}
func (permissionWindowBackend) Bounds(windowbackend.Handle, bool) (windowbackend.Rect, error) {
	return windowbackend.Rect{}, windowbackend.ErrPermission
}
func (permissionWindowBackend) Activate(windowbackend.Handle) error {
	return windowbackend.ErrPermission
}
func (permissionWindowBackend) SetState(windowbackend.Handle, windowbackend.State, bool) error {
	return windowbackend.ErrPermission
}
func (permissionWindowBackend) State(windowbackend.Handle, windowbackend.State) (bool, error) {
	return false, windowbackend.ErrPermission
}
func (permissionWindowBackend) TopMost(windowbackend.Handle) (bool, error) {
	return false, windowbackend.ErrPermission
}
func (permissionWindowBackend) SetTopMost(windowbackend.Handle, bool) error {
	return windowbackend.ErrPermission
}
func (permissionWindowBackend) Close(windowbackend.Handle) error {
	return windowbackend.ErrPermission
}

func TestPublicPureGoWindowBackendTranslatesPermissionForEveryOperation(t *testing.T) {
	backend := publicPureGoWindowBackend{Backend: permissionWindowBackend{}}
	assertPermission := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrPermissionDenied) ||
			!errors.Is(err, windowbackend.ErrPermission) {
			t.Fatalf("%s error = %v, want public and internal permission causes", name, err)
		}
	}

	_, err := backend.Active()
	assertPermission("Active", err)
	_, err = backend.Resolve(1, false)
	assertPermission("Resolve", err)
	assertPermission("Select", backend.Select(1, false))
	_, err = backend.PID(1)
	assertPermission("PID", err)
	_, err = backend.Title(1)
	assertPermission("Title", err)
	_, err = backend.Bounds(1, false)
	assertPermission("Bounds", err)
	assertPermission("Activate", backend.Activate(1))
	assertPermission("SetState", backend.SetState(1, windowbackend.StateMinimized, true))
	_, err = backend.State(1, windowbackend.StateMinimized)
	assertPermission("State", err)
	_, err = backend.TopMost(1)
	assertPermission("TopMost", err)
	assertPermission("SetTopMost", backend.SetTopMost(1, true))
	assertPermission("Close", backend.Close(1))
}
