//go:build linux

package portal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOpenRemoteDesktopLifecycleAndNotify(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	session, err := openTestSession(context.Background(), portal, DeviceKeyboard|DevicePointer)
	if err != nil {
		t.Fatalf("openRemoteDesktop error: %v", err)
	}
	if got := session.Devices(); got != DeviceKeyboard|DevicePointer {
		t.Fatalf("granted devices = %d, want keyboard+pointer", got)
	}
	if err := session.PointerMotion(context.Background(), 4.5, -2); err != nil {
		t.Fatalf("PointerMotion error: %v", err)
	}
	if err := session.KeyboardKeycode(context.Background(), 30, true); err != nil {
		t.Fatalf("KeyboardKeycode error: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}

	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.notifications) != 2 {
		t.Fatalf("notification count = %d, want 2", len(portal.notifications))
	}
	if portal.notifications[0].method != notifyPointerMotion || portal.notifications[1].method != notifyKeyboardKeycode {
		t.Fatalf("unexpected notifications: %#v", portal.notifications)
	}
	if len(portal.closeSessions) != 1 {
		t.Fatalf("session close count = %d, want 1", len(portal.closeSessions))
	}
	if !portal.closed {
		t.Fatal("portal connection was not closed")
	}
}

func TestOpenRemoteDesktopAcceptsImmediateResponseOnReturnedPath(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.createRequest = "/org/freedesktop/portal/desktop/request/1_42/returned"
	session, err := openTestSession(context.Background(), portal, DevicePointer)
	if err != nil {
		t.Fatalf("openRemoteDesktop error: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

func TestOpenRemoteDesktopRequestMatchCleanupFailureClosesCreatedSession(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	cleanupErr := errors.New("remove request match failed")
	portal.removeRequestErr = cleanupErr

	_, err := openTestSession(context.Background(), portal, DevicePointer)
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("openRemoteDesktop error = %v, want request-match cleanup error", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 1 {
		t.Fatalf("session close count = %d, want created session cleanup", len(portal.closeSessions))
	}
	if !portal.closed {
		t.Fatal("portal connection was not closed after request-match cleanup failure")
	}
}

func TestOpenRemoteDesktopJoinsRequestAndMatchCleanupFailures(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.createCode = 2
	cleanupErr := errors.New("remove request match failed")
	portal.removeRequestErr = cleanupErr

	_, err := openTestSession(context.Background(), portal, DevicePointer)
	if !errors.Is(err, ErrRejected) || !errors.Is(err, cleanupErr) {
		t.Fatalf("openRemoteDesktop error = %v, want rejection and request-match cleanup error", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 0 {
		t.Fatalf("rejected CreateSession unexpectedly closed session paths: %v", portal.closeSessions)
	}
	if !portal.closed {
		t.Fatal("portal connection was not closed after combined request failure")
	}
}

func TestOpenRemoteDesktopObservesEarlyCloseOnReturnedSessionPath(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.createdSessionPath = "/org/freedesktop/portal/desktop/session/1_42/returned"
	portal.closeOnCreate = true
	_, err := openTestSession(context.Background(), portal, DevicePointer)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("openRemoteDesktop error = %v, want ErrClosed", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 0 {
		t.Fatalf("portal-closed session received redundant Close calls: %v", portal.closeSessions)
	}
	if !portal.closed {
		t.Fatal("portal connection was not closed after early Session.Closed")
	}
}

func TestOpenRemoteDesktopDoesNotClosePortalClosedSessionDuringNegotiation(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*fakeRemoteDesktopPortal) OpenOptions
	}{
		{
			name: "select devices",
			prepare: func(portal *fakeRemoteDesktopPortal) OpenOptions {
				portal.closeOnSelect = true
				return OpenOptions{Devices: DevicePointer}
			},
		},
		{
			name: "select sources",
			prepare: func(portal *fakeRemoteDesktopPortal) OpenOptions {
				portal.closeOnSources = true
				return OpenOptions{Devices: DevicePointer, Sources: SourceMonitor}
			},
		},
		{
			name: "start",
			prepare: func(portal *fakeRemoteDesktopPortal) OpenOptions {
				portal.closeOnStart = true
				return OpenOptions{Devices: DevicePointer}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			portal := newFakeRemoteDesktopPortal()
			options := test.prepare(portal)
			_, err := openTestSessionWithOptions(context.Background(), portal, options)
			if !errors.Is(err, ErrClosed) {
				t.Fatalf("openRemoteDesktop error = %v, want ErrClosed", err)
			}
			portal.mu.Lock()
			defer portal.mu.Unlock()
			if len(portal.closeSessions) != 0 {
				t.Fatalf("portal-closed session received redundant Close calls: %v", portal.closeSessions)
			}
			if !portal.closed {
				t.Fatal("portal connection was not closed")
			}
		})
	}
}

func TestOpenRemoteDesktopInvalidRequestPathClosesPrediction(t *testing.T) {
	tests := []struct {
		name         string
		configure    func(*fakeRemoteDesktopPortal)
		wantToken    string
		wantSessions int
	}{
		{
			name: "create",
			configure: func(portal *fakeRemoteDesktopPortal) {
				portal.invalidCreatePath = true
			},
			wantToken: "create",
		},
		{
			name: "select devices",
			configure: func(portal *fakeRemoteDesktopPortal) {
				portal.invalidSelectPath = true
			},
			wantToken:    "select",
			wantSessions: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			portal := newFakeRemoteDesktopPortal()
			cleanupErr := errors.New("close predicted request failed")
			portal.closeRequestErr = cleanupErr
			test.configure(portal)
			_, err := openTestSession(context.Background(), portal, DevicePointer)
			if err == nil || !strings.Contains(err.Error(), "invalid request path") || !errors.Is(err, cleanupErr) {
				t.Fatalf("openRemoteDesktop error = %v, want invalid-path and cleanup errors", err)
			}
			portal.mu.Lock()
			defer portal.mu.Unlock()
			wantRequest := requestPath(portal.name, test.wantToken)
			if len(portal.closeRequests) != 1 || portal.closeRequests[0] != wantRequest {
				t.Fatalf("closed requests = %v, want %s", portal.closeRequests, wantRequest)
			}
			if len(portal.closeSessions) != test.wantSessions {
				t.Fatalf("closed sessions = %v, want count %d", portal.closeSessions, test.wantSessions)
			}
		})
	}
}

func TestOpenRemoteDesktopRejectedRequestCleansUp(t *testing.T) {
	tests := []struct {
		name string
		code uint32
		want error
	}{
		{name: "cancelled", code: 1, want: ErrCancelled},
		{name: "rejected", code: 2, want: ErrRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			portal := newFakeRemoteDesktopPortal()
			portal.selectCode = test.code
			_, err := openTestSession(context.Background(), portal, DevicePointer)
			if !errors.Is(err, test.want) {
				t.Fatalf("openRemoteDesktop error = %v, want %v", err, test.want)
			}
			portal.mu.Lock()
			defer portal.mu.Unlock()
			if len(portal.closeSessions) != 1 {
				t.Fatalf("session close count = %d, want 1", len(portal.closeSessions))
			}
			if !portal.closed {
				t.Fatal("portal connection was not closed after rejected request")
			}
		})
	}
}

func TestOpenRemoteDesktopTimeoutClosesRequest(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.holdCreate = true
	cleanupErr := errors.New("close timed-out request failed")
	portal.closeRequestErr = cleanupErr
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := openTestSession(ctx, portal, DevicePointer)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, cleanupErr) {
		t.Fatalf("openRemoteDesktop error = %v, want deadline and request-cleanup errors", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeRequests) != 1 || portal.closeRequests[0] != requestPath(portal.name, "create") {
		t.Fatalf("closed requests = %v, want create request", portal.closeRequests)
	}
	if len(portal.closeSessions) != 0 {
		t.Fatalf("session close count = %d, want 0", len(portal.closeSessions))
	}
	if !portal.closed {
		t.Fatal("portal connection was not closed after timeout")
	}
}

func TestOpenRemoteDesktopConnectionLossUnblocksRequest(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.holdCreate = true
	portal.disconnect()

	_, err := openTestSession(context.Background(), portal, DevicePointer)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("openRemoteDesktop error = %v, want ErrUnavailable", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeRequests) != 0 {
		t.Fatalf("disconnected portal received request cleanup calls: %v", portal.closeRequests)
	}
	if !portal.closed {
		t.Fatal("portal transport was not closed after connection loss")
	}
}

func TestOpenRemoteDesktopInvokeFailureAfterCancellationJoinsCleanupError(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.createErr = context.Canceled
	cleanupErr := errors.New("close cancelled request failed")
	portal.closeRequestErr = cleanupErr
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := openTestSession(ctx, portal, DevicePointer)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) {
		t.Fatalf("openRemoteDesktop error = %v, want cancellation and request-cleanup errors", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	wantRequest := requestPath(portal.name, "create")
	if len(portal.closeRequests) != 1 || portal.closeRequests[0] != wantRequest {
		t.Fatalf("closed requests = %v, want %s", portal.closeRequests, wantRequest)
	}
	if !portal.closed {
		t.Fatal("portal connection was not closed after cancelled invoke")
	}
}

func TestOpenRemoteDesktopRequiresAllRequestedDevices(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.granted = DeviceKeyboard
	_, err := openTestSession(context.Background(), portal, DeviceKeyboard|DevicePointer)
	if !errors.Is(err, ErrDeviceNotGranted) {
		t.Fatalf("openRemoteDesktop error = %v, want ErrDeviceNotGranted", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 1 {
		t.Fatalf("session close count = %d, want 1", len(portal.closeSessions))
	}
}

func TestRemoteDesktopPortalClosedSignal(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	session, err := openTestSession(context.Background(), portal, DevicePointer)
	if err != nil {
		t.Fatalf("openRemoteDesktop error: %v", err)
	}
	if err := portal.emitSessionClosed(session.path); err != nil {
		t.Fatalf("emitSessionClosed error: %v", err)
	}
	select {
	case <-session.Closed():
	case <-time.After(time.Second):
		t.Fatal("session did not observe portal Closed signal")
	}
	if err := session.PointerMotion(context.Background(), 1, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("PointerMotion error = %v, want ErrClosed", err)
	}
	if !portal.waitClosed(time.Second) {
		t.Fatal("portal connection was not closed after Closed signal")
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 0 {
		t.Fatalf("portal-closed session sent Close: %v", portal.closeSessions)
	}
}

func TestRemoteDesktopConnectionLossClosesSession(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	session, err := openTestSession(context.Background(), portal, DevicePointer)
	if err != nil {
		t.Fatalf("openRemoteDesktop error: %v", err)
	}
	portal.disconnect()
	select {
	case <-session.Closed():
	case <-time.After(time.Second):
		t.Fatal("session did not observe D-Bus connection loss")
	}
	if err := session.PointerMotion(context.Background(), 1, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("PointerMotion error = %v, want ErrClosed", err)
	}
}

func TestRemoteDesktopNotifyFailureClosesSession(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	session, err := openTestSession(context.Background(), portal, DevicePointer)
	if err != nil {
		t.Fatalf("openRemoteDesktop error: %v", err)
	}
	portal.mu.Lock()
	portal.notifyErr = errors.New("transport failed")
	portal.mu.Unlock()
	if err := session.PointerMotion(context.Background(), 1, 1); err == nil {
		t.Fatal("PointerMotion unexpectedly succeeded")
	}
	select {
	case <-session.Closed():
	default:
		t.Fatal("failed notification did not close the local session")
	}
}
