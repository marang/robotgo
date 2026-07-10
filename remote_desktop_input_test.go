package robotgo

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
)

type fakeHighLevelPortalSession struct {
	mu       sync.Mutex
	devices  inputportal.DeviceType
	events   []string
	closed   int
	closeErr error
	deadline bool
	done     chan struct{}
}

func (s *fakeHighLevelPortalSession) Devices() inputportal.DeviceType { return s.devices }

func (s *fakeHighLevelPortalSession) Closed() <-chan struct{} { return s.done }

func (s *fakeHighLevelPortalSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return s.closeErr
}

func (s *fakeHighLevelPortalSession) record(format string, args ...interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, fmt.Sprintf(format, args...))
	return nil
}

func (s *fakeHighLevelPortalSession) PointerMotion(ctx context.Context, dx, dy float64) error {
	_, s.deadline = ctx.Deadline()
	return s.record("motion:%g:%g", dx, dy)
}

func (s *fakeHighLevelPortalSession) PointerButton(_ context.Context, button int32, pressed bool) error {
	return s.record("button:%d:%t", button, pressed)
}

func (s *fakeHighLevelPortalSession) PointerAxisDiscrete(_ context.Context, axis inputportal.PointerAxis, steps int32) error {
	return s.record("axis:%d:%d", axis, steps)
}

func (s *fakeHighLevelPortalSession) KeyboardKeysym(_ context.Context, keysym int32, pressed bool) error {
	return s.record("keysym:%d:%t", keysym, pressed)
}

func installFakeHighLevelPortalSession(t *testing.T, devices inputportal.DeviceType) *fakeHighLevelPortalSession {
	t.Helper()
	session := &fakeHighLevelPortalSession{devices: devices}
	remoteDesktopInputState.Lock()
	previous := remoteDesktopInputState.session
	remoteDesktopInputState.session = session
	remoteDesktopInputState.Unlock()
	t.Cleanup(func() {
		remoteDesktopInputState.Lock()
		if remoteDesktopInputState.session == session {
			remoteDesktopInputState.session = previous
		}
		remoteDesktopInputState.Unlock()
	})
	return session
}

func (s *fakeHighLevelPortalSession) snapshot() ([]string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...), s.closed
}

func (s *fakeHighLevelPortalSession) hasDeadline() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadline
}

func TestStartRemoteDesktopInputReplacesAndClosesSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	first := &fakeHighLevelPortalSession{devices: inputportal.DeviceKeyboard | inputportal.DevicePointer}
	second := &fakeHighLevelPortalSession{devices: inputportal.DevicePointer}
	oldOpen := remoteDesktopInputOpen
	openCount := 0
	remoteDesktopInputOpen = func(_ context.Context, _ inputportal.DeviceType) (remoteDesktopInputSession, error) {
		openCount++
		if openCount == 1 {
			return first, nil
		}
		return second, nil
	}
	t.Cleanup(func() {
		_ = CloseRemoteDesktopInput()
		remoteDesktopInputOpen = oldOpen
	})

	if err := StartRemoteDesktopInput(context.Background(), RemoteDesktopKeyboard|RemoteDesktopPointer); err != nil {
		t.Fatalf("first StartRemoteDesktopInput error: %v", err)
	}
	if err := RemoteDesktopInputReady(RemoteDesktopKeyboard | RemoteDesktopPointer); err != nil {
		t.Fatalf("RemoteDesktopInputReady error: %v", err)
	}
	if err := StartRemoteDesktopInput(context.Background(), RemoteDesktopPointer); err != nil {
		t.Fatalf("second StartRemoteDesktopInput error: %v", err)
	}
	if _, closed := first.snapshot(); closed != 1 {
		t.Fatalf("replaced session close count = %d, want 1", closed)
	}
	if err := RemoteDesktopInputReady(RemoteDesktopKeyboard); !errors.Is(err, inputportal.ErrDeviceNotGranted) {
		t.Fatalf("keyboard readiness error = %v, want ErrDeviceNotGranted", err)
	}
	closeErrs := make(chan error, 2)
	for range 2 {
		go func() { closeErrs <- CloseRemoteDesktopInput() }()
	}
	for range 2 {
		if err := <-closeErrs; err != nil {
			t.Fatalf("CloseRemoteDesktopInput error: %v", err)
		}
	}
	if _, closed := second.snapshot(); closed != 1 {
		t.Fatalf("active session close count = %d, want 1", closed)
	}
}

func TestStartRemoteDesktopInputReplacementCloseFailureLeavesNoActiveSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	closeErr := errors.New("close failed")
	first := &fakeHighLevelPortalSession{devices: inputportal.DevicePointer, closeErr: closeErr}
	second := &fakeHighLevelPortalSession{devices: inputportal.DevicePointer}
	remoteDesktopInputState.Lock()
	previous := remoteDesktopInputState.session
	remoteDesktopInputState.session = first
	remoteDesktopInputState.Unlock()
	oldOpen := remoteDesktopInputOpen
	remoteDesktopInputOpen = func(context.Context, inputportal.DeviceType) (remoteDesktopInputSession, error) {
		return second, nil
	}
	t.Cleanup(func() {
		remoteDesktopInputOpen = oldOpen
		remoteDesktopInputState.Lock()
		remoteDesktopInputState.session = previous
		remoteDesktopInputState.Unlock()
	})

	err := StartRemoteDesktopInput(context.Background(), RemoteDesktopPointer)
	if !errors.Is(err, closeErr) {
		t.Fatalf("StartRemoteDesktopInput error = %v, want close failure", err)
	}
	if err := RemoteDesktopInputReady(RemoteDesktopPointer); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("RemoteDesktopInputReady error = %v, want ErrNotSupported", err)
	}
	if _, closed := second.snapshot(); closed != 1 {
		t.Fatalf("replacement session close count = %d, want 1", closed)
	}
}

func TestRemoteDesktopHighLevelEventsHaveDeadline(t *testing.T) {
	session := installFakeHighLevelPortalSession(t, inputportal.DevicePointer)
	used, err := tryRemoteDesktopMoveRelative(1, 2)
	if !used || err != nil {
		t.Fatalf("tryRemoteDesktopMoveRelative = (%v, %v), want (true, nil)", used, err)
	}
	if !session.hasDeadline() {
		t.Fatal("high-level portal event did not receive a deadline")
	}
}

func TestRemoteDesktopButtonValidationPreservesPortalError(t *testing.T) {
	installFakeHighLevelPortalSession(t, inputportal.DevicePointer)
	used, err := tryRemoteDesktopClick("wheelDown", false)
	if !used || !errors.Is(err, ErrNotSupported) {
		t.Fatalf("tryRemoteDesktopClick = (%v, %v), want active portal ErrNotSupported", used, err)
	}
}

func TestCloseRemoteDesktopInputCancelsPendingStart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	entered := make(chan struct{})
	oldOpen := remoteDesktopInputOpen
	remoteDesktopInputOpen = func(ctx context.Context, _ inputportal.DeviceType) (remoteDesktopInputSession, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	t.Cleanup(func() { remoteDesktopInputOpen = oldOpen })

	startErr := make(chan error, 1)
	go func() {
		startErr <- StartRemoteDesktopInput(context.Background(), RemoteDesktopPointer)
	}()
	<-entered
	if err := CloseRemoteDesktopInput(); err != nil {
		t.Fatalf("CloseRemoteDesktopInput error: %v", err)
	}
	if err := <-startErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("StartRemoteDesktopInput error = %v, want context.Canceled", err)
	}
}

func TestPortalKeysymForRune(t *testing.T) {
	tests := []struct {
		value rune
		want  int32
	}{
		{value: 'a', want: 0x61},
		{value: 'é', want: 0xe9},
		{value: '\n', want: 0xff0d},
		{value: '€', want: 0x010020ac},
		{value: '😀', want: 0x0101f600},
	}
	for _, test := range tests {
		got, err := portalKeysymForRune(test.value)
		if err != nil {
			t.Fatalf("portalKeysymForRune(%U) error: %v", test.value, err)
		}
		if got != test.want {
			t.Fatalf("portalKeysymForRune(%U) = %#x, want %#x", test.value, got, test.want)
		}
	}
}

func TestPortalKeysymsPureNormalizesAndDeduplicatesModifiers(t *testing.T) {
	key, modifiers, err := portalKeysymsPure("A", []string{"shift"})
	if err != nil {
		t.Fatalf("portalKeysymsPure error: %v", err)
	}
	if key != int32('a') {
		t.Fatalf("key = %#x, want 'a'", key)
	}
	if len(modifiers) != 1 || modifiers[0] != 0xffe1 {
		t.Fatalf("modifiers = %#v, want one left shift", modifiers)
	}
	if _, err := portalKeysymForKey(string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 key unexpectedly accepted")
	}
}

func TestRemoteDesktopInputReadyRejectsPortalClosedSession(t *testing.T) {
	session := installFakeHighLevelPortalSession(t, inputportal.DevicePointer)
	session.done = make(chan struct{})
	close(session.done)
	if err := RemoteDesktopInputReady(RemoteDesktopPointer); !errors.Is(err, inputportal.ErrClosed) {
		t.Fatalf("RemoteDesktopInputReady error = %v, want ErrClosed", err)
	}
	used, err := withRemoteDesktopInput(inputportal.DevicePointer, func(remoteDesktopInputSession) error {
		t.Fatal("closed session callback was invoked")
		return nil
	})
	if !used || !errors.Is(err, inputportal.ErrClosed) {
		t.Fatalf("withRemoteDesktopInput = (%v, %v), want (true, ErrClosed)", used, err)
	}
}
