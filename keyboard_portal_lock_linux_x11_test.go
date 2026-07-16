//go:build linux && cgo && !wayland

package robotgo

import (
	"context"
	"sync/atomic"
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
)

type displayLeaseCheckingPortalSession struct {
	*fakeHighLevelPortalSession
	displayLeaseHeld atomic.Bool
}

func (s *displayLeaseCheckingPortalSession) checkDisplayLease() {
	if nativeX11DisplayMu.TryLock() {
		nativeX11DisplayMu.Unlock()
	} else {
		s.displayLeaseHeld.Store(true)
	}
}

func (s *displayLeaseCheckingPortalSession) KeyboardKeysym(
	ctx context.Context, keysym int32, pressed bool,
) error {
	s.checkDisplayLease()
	return s.fakeHighLevelPortalSession.KeyboardKeysym(ctx, keysym, pressed)
}

func (s *displayLeaseCheckingPortalSession) PointerMotion(
	ctx context.Context, dx, dy float64,
) error {
	s.checkDisplayLease()
	return s.fakeHighLevelPortalSession.PointerMotion(ctx, dx, dy)
}

func TestWaylandPortalKeyboardDoesNotHoldNativeX11DisplayLease(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "robotgo-portal-lock-test")
	t.Setenv("DISPLAY", "")

	base := installFakeHighLevelPortalSession(
		t, inputportal.DeviceKeyboard|inputportal.DevicePointer,
	)
	session := &displayLeaseCheckingPortalSession{fakeHighLevelPortalSession: base}
	remoteDesktopInputState.Lock()
	if remoteDesktopInputState.session != base {
		remoteDesktopInputState.Unlock()
		t.Fatal("fake RemoteDesktop session was replaced before lock-scope test")
	}
	remoteDesktopInputState.session = session
	remoteDesktopInputState.generation++
	remoteDesktopInputState.Unlock()
	t.Cleanup(func() {
		remoteDesktopInputState.Lock()
		if remoteDesktopInputState.session == session {
			remoteDesktopInputState.session = base
			remoteDesktopInputState.generation++
		}
		remoteDesktopInputState.Unlock()
	})

	if err := TypeStrE("a", 0, 0, 0); err != nil {
		t.Fatalf("TypeStrE through RemoteDesktop fallback: %v", err)
	}
	if err := MoveRelativeE(3, -2); err != nil {
		t.Fatalf("MoveRelativeE through RemoteDesktop fallback: %v", err)
	}
	if session.displayLeaseHeld.Load() {
		t.Fatal("RemoteDesktop input callback ran while nativeX11DisplayMu was held")
	}
	if events, _ := base.snapshot(); len(events) != 3 {
		t.Fatalf("RemoteDesktop input events = %v, want key press/release plus pointer motion", events)
	}
}
