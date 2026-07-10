//go:build linux

package portal

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeNotification struct {
	method string
	args   []interface{}
}

type fakeRemoteDesktopPortal struct {
	mu sync.Mutex

	name               string
	signals            chan<- *dbus.Signal
	requestMatch       bool
	sessionMatch       bool
	connection         chan struct{}
	connectionOnce     sync.Once
	createCode         uint32
	createRequest      dbus.ObjectPath
	createdSessionPath dbus.ObjectPath
	closeOnCreate      bool
	selectCode         uint32
	startCode          uint32
	granted            DeviceType
	holdCreate         bool
	closeRequests      []dbus.ObjectPath
	closeSessions      []dbus.ObjectPath
	notifications      []fakeNotification
	notifyErr          error
	closed             bool
}

func newFakeRemoteDesktopPortal() *fakeRemoteDesktopPortal {
	return &fakeRemoteDesktopPortal{
		name:       ":1.42",
		connection: make(chan struct{}),
		granted:    DeviceKeyboard | DevicePointer,
	}
}

func (p *fakeRemoteDesktopPortal) uniqueName() string { return p.name }

func (p *fakeRemoteDesktopPortal) capability(context.Context) (Capability, error) {
	return Capability{Version: 2, AvailableDevices: allDeviceTypes}, nil
}

func (p *fakeRemoteDesktopPortal) addRequestMatch() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requestMatch = true
	return nil
}

func (p *fakeRemoteDesktopPortal) removeRequestMatch() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requestMatch = false
	return nil
}

func (p *fakeRemoteDesktopPortal) addSessionMatch() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionMatch = true
	return nil
}

func (p *fakeRemoteDesktopPortal) removeSessionMatch() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionMatch = false
	return nil
}

func (p *fakeRemoteDesktopPortal) registerSignals(ch chan<- *dbus.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signals = ch
}

func (p *fakeRemoteDesktopPortal) removeSignals(ch chan<- *dbus.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.signals == ch {
		p.signals = nil
	}
}

func (p *fakeRemoteDesktopPortal) connectionDone() <-chan struct{} { return p.connection }

func (p *fakeRemoteDesktopPortal) createSession(_ context.Context, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	request := requestPath(p.name, variantString(options, "handle_token"))
	if p.createRequest.IsValid() {
		request = p.createRequest
	}
	session := sessionPath(p.name, variantString(options, "session_handle_token"))
	if p.createdSessionPath.IsValid() {
		session = p.createdSessionPath
	}
	if p.holdCreate {
		return request, nil
	}
	if p.closeOnCreate {
		if err := p.emitSessionClosed(session); err != nil {
			return "", err
		}
	}
	if err := p.emitResponse(request, p.createCode, map[string]dbus.Variant{
		"session_handle": dbus.MakeVariant(string(session)),
	}); err != nil {
		return "", err
	}
	return request, nil
}

func (p *fakeRemoteDesktopPortal) selectDevices(_ context.Context, _ dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	request := requestPath(p.name, variantString(options, "handle_token"))
	if err := p.emitResponse(request, p.selectCode, map[string]dbus.Variant{}); err != nil {
		return "", err
	}
	return request, nil
}

func (p *fakeRemoteDesktopPortal) start(_ context.Context, _ dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	request := requestPath(p.name, variantString(options, "handle_token"))
	if err := p.emitResponse(request, p.startCode, map[string]dbus.Variant{
		"devices": dbus.MakeVariant(uint32(p.granted)),
	}); err != nil {
		return "", err
	}
	return request, nil
}

func (p *fakeRemoteDesktopPortal) emitResponse(path dbus.ObjectPath, code uint32, results map[string]dbus.Variant) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.requestMatch {
		return fmt.Errorf("response match was not installed before request %s", path)
	}
	if p.signals == nil {
		return errors.New("signal channel was not registered before the request")
	}
	p.signals <- &dbus.Signal{
		Name: requestResponseSignal,
		Path: path,
		Body: []interface{}{code, results},
	}
	return nil
}

func (p *fakeRemoteDesktopPortal) emitSessionClosed(path dbus.ObjectPath) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sessionMatch {
		return fmt.Errorf("session match is not installed for %s", path)
	}
	if p.signals == nil {
		return errors.New("signal channel is not registered")
	}
	p.signals <- &dbus.Signal{Name: sessionClosedSignal, Path: path, Body: []interface{}{map[string]dbus.Variant{}}}
	return nil
}

func (p *fakeRemoteDesktopPortal) closeRequest(_ context.Context, path dbus.ObjectPath) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeRequests = append(p.closeRequests, path)
	return nil
}

func (p *fakeRemoteDesktopPortal) closeSession(_ context.Context, path dbus.ObjectPath) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeSessions = append(p.closeSessions, path)
	return nil
}

func (p *fakeRemoteDesktopPortal) notify(_ context.Context, method string, args ...interface{}) error {
	p.mu.Lock()
	err := p.notifyErr
	if err == nil {
		p.notifications = append(p.notifications, fakeNotification{method: method, args: args})
	}
	p.mu.Unlock()
	return err
}

func (p *fakeRemoteDesktopPortal) close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.connectionOnce.Do(func() { close(p.connection) })
	return nil
}

func (p *fakeRemoteDesktopPortal) disconnect() {
	p.connectionOnce.Do(func() { close(p.connection) })
}

func variantString(options map[string]dbus.Variant, key string) string {
	value, ok := options[key]
	if !ok {
		return ""
	}
	text, _ := value.Value().(string)
	return text
}

func fixedTokens(tokens ...string) tokenFunc {
	index := 0
	return func(string) (string, error) {
		if index >= len(tokens) {
			return "", errors.New("test token source exhausted")
		}
		token := tokens[index]
		index++
		return token, nil
	}
}

func openTestSession(ctx context.Context, portal *fakeRemoteDesktopPortal, devices DeviceType) (*Session, error) {
	return openRemoteDesktop(ctx, portal, devices, fixedTokens("create", "session", "select", "start"))
}

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

func TestOpenRemoteDesktopObservesEarlyCloseOnReturnedSessionPath(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.createdSessionPath = "/org/freedesktop/portal/desktop/session/1_42/returned"
	portal.closeOnCreate = true
	_, err := openTestSession(context.Background(), portal, DevicePointer)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("openRemoteDesktop error = %v, want ErrClosed", err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := openTestSession(ctx, portal, DevicePointer)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("openRemoteDesktop error = %v, want deadline exceeded", err)
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
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 0 {
		t.Fatalf("portal-closed session sent Close: %v", portal.closeSessions)
	}
	if !portal.closed {
		t.Fatal("portal connection was not closed after Closed signal")
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

func TestCapabilitySupports(t *testing.T) {
	capability := Capability{Version: 2, AvailableDevices: DeviceKeyboard | DevicePointer}
	if !capability.Supports(DeviceKeyboard | DevicePointer) {
		t.Fatal("expected keyboard and pointer to be supported")
	}
}

func TestValidateDevicesRejectsUnmappedScreenCastDevices(t *testing.T) {
	if err := validateDevices(DeviceType(4)); err == nil {
		t.Fatal("touchscreen device unexpectedly accepted before ScreenCast integration")
	}
}
