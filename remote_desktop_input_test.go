package robotgo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
)

type fakeHighLevelPortalSession struct {
	mu           sync.Mutex
	devices      inputportal.DeviceType
	events       []string
	closed       int
	closeErr     error
	deadline     bool
	done         chan struct{}
	streams      []inputportal.Stream
	restoreToken string
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

func (s *fakeHighLevelPortalSession) PointerMotionAbsolute(_ context.Context, stream uint32, x, y float64) error {
	return s.record("absolute:%d:%g:%g", stream, x, y)
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

func (s *fakeHighLevelPortalSession) Streams() []inputportal.Stream {
	return append([]inputportal.Stream(nil), s.streams...)
}

func (s *fakeHighLevelPortalSession) RestoreToken() string { return s.restoreToken }

func (s *fakeHighLevelPortalSession) TouchDown(_ context.Context, stream, slot uint32, x, y float64) error {
	return s.record("touch-down:%d:%d:%g:%g", stream, slot, x, y)
}

func (s *fakeHighLevelPortalSession) TouchMotion(_ context.Context, stream, slot uint32, x, y float64) error {
	return s.record("touch-motion:%d:%d:%g:%g", stream, slot, x, y)
}

func (s *fakeHighLevelPortalSession) TouchUp(_ context.Context, slot uint32) error {
	return s.record("touch-up:%d", slot)
}

func installFakeHighLevelPortalSession(t *testing.T, devices inputportal.DeviceType) *fakeHighLevelPortalSession {
	t.Helper()
	session := &fakeHighLevelPortalSession{devices: devices}
	remoteDesktopInputState.Lock()
	previous := remoteDesktopInputState.session
	previousPermission := remoteDesktopInputState.permission
	previousReason := remoteDesktopInputState.reason
	remoteDesktopInputState.session = session
	remoteDesktopInputState.permission = RemoteDesktopPermissionGranted
	remoteDesktopInputState.reason = "test session active"
	remoteDesktopInputState.Unlock()
	t.Cleanup(func() {
		remoteDesktopInputState.Lock()
		if remoteDesktopInputState.session == session {
			remoteDesktopInputState.session = previous
		}
		remoteDesktopInputState.permission = previousPermission
		remoteDesktopInputState.reason = previousReason
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

func installRemoteDesktopMouseDelayRecorder(t *testing.T) *[]int {
	t.Helper()
	previous := remoteDesktopMouseSleeper
	delays := []int{}
	remoteDesktopMouseSleeper = func(delay int) { delays = append(delays, delay) }
	t.Cleanup(func() { remoteDesktopMouseSleeper = previous })
	return &delays
}

func assertRemoteDesktopMouseDelays(t *testing.T, got []int, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("mouse delays = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mouse delay %d = %d, want %d (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestFailedRemoteDesktopMouseEventDoesNotSleep(t *testing.T) {
	oldMouseSleep := MouseSleep
	MouseSleep = 23
	t.Cleanup(func() { MouseSleep = oldMouseSleep })
	delays := installRemoteDesktopMouseDelayRecorder(t)
	wantErr := errors.New("input failed")
	if err := finishRemoteDesktopMouseEvent(wantErr, 7); !errors.Is(err, wantErr) {
		t.Fatalf("finishRemoteDesktopMouseEvent error = %v, want %v", err, wantErr)
	}
	assertRemoteDesktopMouseDelays(t, *delays, nil)
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
	remoteDesktopInputOpen = func(_ context.Context, _ inputportal.OpenOptions) (remoteDesktopInputSession, error) {
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
	previousPermission := remoteDesktopInputState.permission
	previousReason := remoteDesktopInputState.reason
	remoteDesktopInputState.session = first
	remoteDesktopInputState.permission = RemoteDesktopPermissionGranted
	remoteDesktopInputState.reason = "test session active"
	remoteDesktopInputState.Unlock()
	oldOpen := remoteDesktopInputOpen
	remoteDesktopInputOpen = func(context.Context, inputportal.OpenOptions) (remoteDesktopInputSession, error) {
		return second, nil
	}
	t.Cleanup(func() {
		remoteDesktopInputOpen = oldOpen
		remoteDesktopInputState.Lock()
		remoteDesktopInputState.session = previous
		remoteDesktopInputState.permission = previousPermission
		remoteDesktopInputState.reason = previousReason
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
	remoteDesktopInputOpen = func(ctx context.Context, _ inputportal.OpenOptions) (remoteDesktopInputSession, error) {
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

func TestStartRemoteDesktopInputRejectsCloseAlreadyInProgress(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	oldOpen := remoteDesktopInputOpen
	openCalled := false
	remoteDesktopInputOpen = func(context.Context, inputportal.OpenOptions) (remoteDesktopInputSession, error) {
		openCalled = true
		return nil, errors.New("unexpected portal open")
	}
	remoteDesktopInputPending.Lock()
	remoteDesktopInputPending.closing++
	remoteDesktopInputPending.Unlock()
	t.Cleanup(func() {
		remoteDesktopInputOpen = oldOpen
		remoteDesktopInputPending.Lock()
		remoteDesktopInputPending.closing--
		remoteDesktopInputPending.Unlock()
	})

	err := StartRemoteDesktopInput(context.Background(), RemoteDesktopPointer)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StartRemoteDesktopInput error = %v, want context.Canceled", err)
	}
	if openCalled {
		t.Fatal("portal open was called while close was already in progress")
	}
	remoteDesktopInputPending.Lock()
	defer remoteDesktopInputPending.Unlock()
	if remoteDesktopInputPending.start != nil {
		t.Fatal("cancelled start remained registered")
	}
}

func TestPortalModifiedKeyPreservesModifiersAcrossToggle(t *testing.T) {
	session := &fakeHighLevelPortalSession{devices: inputportal.DeviceKeyboard}
	if err := portalModifiedKey(session, 'a', []int32{0xffe3}, true, false); err != nil {
		t.Fatalf("portal key down error: %v", err)
	}
	events, _ := session.snapshot()
	wantDown := []string{"keysym:65507:true", "keysym:97:true"}
	if !reflect.DeepEqual(events, wantDown) {
		t.Fatalf("key-down events = %#v, want %#v", events, wantDown)
	}

	if err := portalModifiedKey(session, 'a', []int32{0xffe3}, false, false); err != nil {
		t.Fatalf("portal key up error: %v", err)
	}
	events, _ = session.snapshot()
	want := []string{"keysym:65507:true", "keysym:97:true", "keysym:97:false", "keysym:65507:false"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("toggle events = %#v, want %#v", events, want)
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

func TestRemoteDesktopStreamMetadataAndTouchDispatch(t *testing.T) {
	session := installFakeHighLevelPortalSession(t, inputportal.DevicePointer|inputportal.DeviceTouchscreen)
	session.streams = []inputportal.Stream{{NodeID: 42, Size: inputportal.Size{Width: 800, Height: 600}, HasSize: true}}
	session.restoreToken = "restore-next"

	streams, err := RemoteDesktopInputStreams()
	if err != nil || len(streams) != 1 || streams[0].NodeID != 42 {
		t.Fatalf("RemoteDesktopInputStreams = (%#v, %v)", streams, err)
	}
	if got := RemoteDesktopInputRestoreToken(); got != "restore-next" {
		t.Fatalf("RemoteDesktopInputRestoreToken = %q", got)
	}
	if err := RemoteDesktopTouchDown(42, 1, 10, 20); err != nil {
		t.Fatalf("RemoteDesktopTouchDown error: %v", err)
	}
	if err := RemoteDesktopTouchMotion(42, 1, 11, 21); err != nil {
		t.Fatalf("RemoteDesktopTouchMotion error: %v", err)
	}
	if err := RemoteDesktopTouchUp(1); err != nil {
		t.Fatalf("RemoteDesktopTouchUp error: %v", err)
	}
	events, _ := session.snapshot()
	want := []string{"touch-down:42:1:10:20", "touch-motion:42:1:11:21", "touch-up:1"}
	if len(events) != len(want) {
		t.Fatalf("events = %#v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("event %d = %q, want %q", i, events[i], want[i])
		}
	}
}

func TestRemoteDesktopTargetStreamMultiOutput(t *testing.T) {
	monitors := []inputportal.Stream{
		{NodeID: 10, Position: inputportal.Point{X: -1920, Y: 0}, HasPosition: true, Size: inputportal.Size{Width: 1920, Height: 1080}, HasSize: true},
		{NodeID: 20, Position: inputportal.Point{X: 0, Y: 0}, HasPosition: true, Size: inputportal.Size{Width: 2560, Height: 1440}, HasSize: true},
	}
	tests := []struct {
		name      string
		streams   []inputportal.Stream
		x, y      int
		displayID []int
		wantNode  uint32
		wantX     float64
		wantY     float64
		wantErr   error
	}{
		{name: "auto negative monitor", streams: monitors, x: -100, y: 200, wantNode: 10, wantX: 1820, wantY: 200},
		{name: "auto primary monitor", streams: monitors, x: 100, y: 200, wantNode: 20, wantX: 100, wantY: 200},
		{name: "explicit first stream", streams: monitors, x: -1900, y: 50, displayID: []int{0}, wantNode: 10, wantX: 20, wantY: 50},
		{name: "explicit second stream", streams: monitors, x: 300, y: 400, displayID: []int{1}, wantNode: 20, wantX: 300, wantY: 400},
		{name: "negative display means auto", streams: monitors, x: 10, y: 20, displayID: []int{-1}, wantNode: 20, wantX: 10, wantY: 20},
		{name: "unknown display", streams: monitors, displayID: []int{2}, wantErr: inputportal.ErrStreamNotFound},
		{name: "outside every monitor", streams: monitors, x: 4000, y: 200, wantErr: inputportal.ErrStreamNotFound},
		{name: "ambiguous windows", streams: []inputportal.Stream{{NodeID: 30}, {NodeID: 40}}, x: 10, y: 20, wantErr: inputportal.ErrStreamNotFound},
		{name: "single unpositioned stream", streams: []inputportal.Stream{{NodeID: 50}}, x: 10, y: 20, wantNode: 50, wantX: 10, wantY: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream, x, y, err := remoteDesktopTargetStream(test.streams, test.x, test.y, test.displayID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && (stream.NodeID != test.wantNode || x != test.wantX || y != test.wantY) {
				t.Fatalf("target = (node=%d x=%g y=%g), want (node=%d x=%g y=%g)", stream.NodeID, x, y, test.wantNode, test.wantX, test.wantY)
			}
		})
	}
}

func TestGetRemoteDesktopInputStatusReportsProtocolAndPermission(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")
	oldProbe := remoteDesktopStatusProbe
	remoteDesktopStatusProbe = func(context.Context) (inputportal.Capability, error) {
		return inputportal.Capability{
			Version: 2, AvailableDevices: inputportal.DeviceKeyboard | inputportal.DevicePointer,
			ScreenCastVersion: 6, AvailableSources: inputportal.SourceMonitor,
			AvailableCursorModes: inputportal.CursorHidden | inputportal.CursorMetadata,
			ScreenCastIssue:      "cursor metadata probe degraded",
		}, context.DeadlineExceeded
	}
	t.Cleanup(func() { remoteDesktopStatusProbe = oldProbe })
	session := installFakeHighLevelPortalSession(t, inputportal.DeviceKeyboard|inputportal.DevicePointer)
	session.streams = []inputportal.Stream{{NodeID: 9}}
	session.restoreToken = "secret-not-exposed"

	status, err := GetRemoteDesktopInputStatus(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetRemoteDesktopInputStatus error = %v, want ScreenCast deadline", err)
	}
	if !status.PortalAvailable || !status.SessionActive || status.Permission != RemoteDesktopPermissionGranted {
		t.Fatalf("status = %+v", status)
	}
	if status.PortalVersion != 2 || status.ScreenCastVersion != 6 || len(status.Streams) != 1 || !status.RestoreTokenAvailable {
		t.Fatalf("status metadata = %+v", status)
	}
	if status.ScreenCastReason != "cursor metadata probe degraded" {
		t.Fatalf("ScreenCastReason = %q", status.ScreenCastReason)
	}
}

func TestGetRemoteDesktopInputStatusReportsClosedSessionRestoreToken(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	oldProbe := remoteDesktopStatusProbe
	remoteDesktopStatusProbe = func(context.Context) (inputportal.Capability, error) {
		return inputportal.Capability{Version: 2, AvailableDevices: inputportal.DevicePointer}, nil
	}
	t.Cleanup(func() { remoteDesktopStatusProbe = oldProbe })
	session := installFakeHighLevelPortalSession(t, inputportal.DevicePointer)
	session.restoreToken = "restore-after-close"
	session.done = make(chan struct{})
	close(session.done)

	status, err := GetRemoteDesktopInputStatus(context.Background())
	if err != nil {
		t.Fatalf("GetRemoteDesktopInputStatus error: %v", err)
	}
	if status.Permission != RemoteDesktopPermissionClosed || status.SessionActive || !status.RestoreTokenAvailable {
		t.Fatalf("status = %+v", status)
	}
	if got := RemoteDesktopInputRestoreToken(); got != "restore-after-close" {
		t.Fatalf("RemoteDesktopInputRestoreToken = %q", got)
	}
}

func TestCloseRemoteDesktopInputPreservesPortalClosedReason(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	oldProbe := remoteDesktopStatusProbe
	remoteDesktopStatusProbe = func(context.Context) (inputportal.Capability, error) {
		return inputportal.Capability{Version: 2, AvailableDevices: inputportal.DevicePointer}, nil
	}
	t.Cleanup(func() { remoteDesktopStatusProbe = oldProbe })
	session := installFakeHighLevelPortalSession(t, inputportal.DevicePointer)
	session.done = make(chan struct{})
	close(session.done)

	if err := CloseRemoteDesktopInput(); err != nil {
		t.Fatalf("CloseRemoteDesktopInput error: %v", err)
	}
	status, err := GetRemoteDesktopInputStatus(context.Background())
	if err != nil {
		t.Fatalf("GetRemoteDesktopInputStatus error: %v", err)
	}
	if status.Permission != RemoteDesktopPermissionClosed || status.Reason != "portal session was already closed before application cleanup" {
		t.Fatalf("status = %+v", status)
	}
}

func TestGetRemoteDesktopInputStatusReportsLastConsentFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	oldProbe := remoteDesktopStatusProbe
	oldOpen := remoteDesktopInputOpen
	remoteDesktopInputState.Lock()
	previousSession := remoteDesktopInputState.session
	previousPermission := remoteDesktopInputState.permission
	previousReason := remoteDesktopInputState.reason
	remoteDesktopInputState.session = nil
	remoteDesktopInputState.permission = ""
	remoteDesktopInputState.reason = ""
	remoteDesktopInputState.Unlock()
	remoteDesktopStatusProbe = func(context.Context) (inputportal.Capability, error) {
		return inputportal.Capability{Version: 2, AvailableDevices: inputportal.DevicePointer}, nil
	}
	t.Cleanup(func() {
		remoteDesktopStatusProbe = oldProbe
		remoteDesktopInputOpen = oldOpen
		remoteDesktopInputState.Lock()
		remoteDesktopInputState.session = previousSession
		remoteDesktopInputState.permission = previousPermission
		remoteDesktopInputState.reason = previousReason
		remoteDesktopInputState.Unlock()
	})

	tests := []struct {
		name string
		err  error
		want RemoteDesktopPermissionStatus
	}{
		{name: "session closed", err: inputportal.ErrClosed, want: RemoteDesktopPermissionClosed},
		{name: "request failed", err: inputportal.ErrRejected, want: RemoteDesktopPermissionFailed},
		{name: "device denied", err: inputportal.ErrDeviceNotGranted, want: RemoteDesktopPermissionDenied},
		{name: "cancelled", err: inputportal.ErrCancelled, want: RemoteDesktopPermissionCancelled},
		{name: "timed out", err: context.DeadlineExceeded, want: RemoteDesktopPermissionTimedOut},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			remoteDesktopInputOpen = func(context.Context, inputportal.OpenOptions) (remoteDesktopInputSession, error) {
				return nil, test.err
			}
			if err := StartRemoteDesktopInput(context.Background(), RemoteDesktopPointer); !errors.Is(err, test.err) {
				t.Fatalf("StartRemoteDesktopInput error = %v, want %v", err, test.err)
			}
			status, err := GetRemoteDesktopInputStatus(context.Background())
			if err != nil {
				t.Fatalf("GetRemoteDesktopInputStatus error: %v", err)
			}
			if status.Permission != test.want || status.Reason != test.err.Error() {
				t.Fatalf("status = %+v, want permission %q and reason %q", status, test.want, test.err)
			}
		})
	}
}

func TestStartRemoteDesktopInputFailureReplacesClosedSessionStatus(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	session := installFakeHighLevelPortalSession(t, inputportal.DevicePointer)
	session.done = make(chan struct{})
	close(session.done)
	oldOpen := remoteDesktopInputOpen
	remoteDesktopInputOpen = func(context.Context, inputportal.OpenOptions) (remoteDesktopInputSession, error) {
		return nil, inputportal.ErrRejected
	}
	t.Cleanup(func() { remoteDesktopInputOpen = oldOpen })

	if err := StartRemoteDesktopInput(context.Background(), RemoteDesktopPointer); !errors.Is(err, inputportal.ErrRejected) {
		t.Fatalf("StartRemoteDesktopInput error = %v, want ErrRejected", err)
	}
	remoteDesktopInputState.RLock()
	active := remoteDesktopInputState.session
	permission := remoteDesktopInputState.permission
	remoteDesktopInputState.RUnlock()
	if active != nil || permission != RemoteDesktopPermissionFailed {
		t.Fatalf("state session=%v permission=%q, want nil/%q", active, permission, RemoteDesktopPermissionFailed)
	}
}

func TestStartRemoteDesktopInputFailurePreservesActiveSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	session := installFakeHighLevelPortalSession(t, inputportal.DevicePointer)
	oldOpen := remoteDesktopInputOpen
	remoteDesktopInputOpen = func(context.Context, inputportal.OpenOptions) (remoteDesktopInputSession, error) {
		return nil, inputportal.ErrRejected
	}
	t.Cleanup(func() { remoteDesktopInputOpen = oldOpen })

	if err := StartRemoteDesktopInput(context.Background(), RemoteDesktopPointer); !errors.Is(err, inputportal.ErrRejected) {
		t.Fatalf("StartRemoteDesktopInput error = %v, want ErrRejected", err)
	}
	if err := RemoteDesktopInputReady(RemoteDesktopPointer); err != nil {
		t.Fatalf("previous session is no longer ready: %v", err)
	}
	used, err := tryRemoteDesktopMoveRelative(1, 2)
	if !used || err != nil {
		t.Fatalf("previous session input = (%v, %v), want active success", used, err)
	}
	remoteDesktopInputState.RLock()
	active := remoteDesktopInputState.session
	permission := remoteDesktopInputState.permission
	remoteDesktopInputState.RUnlock()
	if active != session || permission != RemoteDesktopPermissionGranted {
		t.Fatalf("state session=%v permission=%q, want previous session/%q", active, permission, RemoteDesktopPermissionGranted)
	}
}

func TestGetRemoteDesktopInputStatusPreservesSessionReasonOnProbeFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	oldProbe := remoteDesktopStatusProbe
	remoteDesktopStatusProbe = func(context.Context) (inputportal.Capability, error) {
		return inputportal.Capability{}, context.DeadlineExceeded
	}
	t.Cleanup(func() { remoteDesktopStatusProbe = oldProbe })
	installFakeHighLevelPortalSession(t, inputportal.DevicePointer)

	status, err := GetRemoteDesktopInputStatus(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetRemoteDesktopInputStatus error = %v, want deadline exceeded", err)
	}
	if !status.PortalAvailable || !status.SessionActive || status.Permission != RemoteDesktopPermissionGranted || status.Reason != "portal consent session is active" {
		t.Fatalf("status = %+v", status)
	}
}

func TestGetRemoteDesktopInputStatusReportsPortalWithoutDevices(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RemoteDesktop portal input is Linux-specific")
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	oldProbe := remoteDesktopStatusProbe
	remoteDesktopStatusProbe = func(context.Context) (inputportal.Capability, error) {
		return inputportal.Capability{Version: 2}, nil
	}
	remoteDesktopInputState.Lock()
	previousSession := remoteDesktopInputState.session
	previousPermission := remoteDesktopInputState.permission
	previousReason := remoteDesktopInputState.reason
	remoteDesktopInputState.session = nil
	remoteDesktopInputState.permission = ""
	remoteDesktopInputState.reason = ""
	remoteDesktopInputState.Unlock()
	t.Cleanup(func() {
		remoteDesktopStatusProbe = oldProbe
		remoteDesktopInputState.Lock()
		remoteDesktopInputState.session = previousSession
		remoteDesktopInputState.permission = previousPermission
		remoteDesktopInputState.reason = previousReason
		remoteDesktopInputState.Unlock()
	})

	status, err := GetRemoteDesktopInputStatus(context.Background())
	if err != nil {
		t.Fatalf("GetRemoteDesktopInputStatus error: %v", err)
	}
	if !status.PortalAvailable || status.AvailableDevices != 0 || status.Reason != "RemoteDesktop portal is available but advertises no input devices" {
		t.Fatalf("status = %+v", status)
	}
}
