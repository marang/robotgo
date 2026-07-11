//go:build linux

package portal

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeNotification struct {
	method string
	args   []interface{}
}

type fakeRemoteDesktopPortal struct {
	mu sync.Mutex

	name                string
	signals             chan<- *dbus.Signal
	requestMatch        bool
	removeRequestErr    error
	sessionMatch        bool
	connection          chan struct{}
	connectionOnce      sync.Once
	createCode          uint32
	createErr           error
	createRequest       dbus.ObjectPath
	createdSessionPath  dbus.ObjectPath
	closeOnCreate       bool
	closeOnSelect       bool
	closeOnSources      bool
	closeOnStart        bool
	invalidCreatePath   bool
	malformedCreate     bool
	invalidSelectPath   bool
	selectCode          uint32
	selectSourcesCode   uint32
	startCode           uint32
	granted             DeviceType
	streams             []rawStream
	restoreToken        string
	selectDeviceOptions map[string]dbus.Variant
	selectSourceOptions map[string]dbus.Variant
	holdCreate          bool
	closeRequests       []dbus.ObjectPath
	closeRequestErr     error
	closeSessions       []dbus.ObjectPath
	notifications       []fakeNotification
	notifyErr           error
	closed              bool
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
	return Capability{
		Version: 2, AvailableDevices: allDeviceTypes,
		ScreenCastVersion: 6, AvailableSources: allSourceTypes,
		AvailableCursorModes: allCursorModes,
	}, nil
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
	if p.removeRequestErr != nil {
		return p.removeRequestErr
	}
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
	if p.createErr != nil {
		return "", p.createErr
	}
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
	if p.invalidCreatePath {
		return dbus.ObjectPath("invalid"), nil
	}
	if p.closeOnCreate {
		if err := p.emitSessionClosed(session); err != nil {
			return "", err
		}
	}
	results := map[string]dbus.Variant{
		"session_handle": dbus.MakeVariant(string(session)),
	}
	if p.malformedCreate {
		results = map[string]dbus.Variant{}
	}
	if err := p.emitResponse(request, p.createCode, results); err != nil {
		return "", err
	}
	return request, nil
}

func (p *fakeRemoteDesktopPortal) selectDevices(_ context.Context, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	p.mu.Lock()
	p.selectDeviceOptions = options
	p.mu.Unlock()
	request := requestPath(p.name, variantString(options, "handle_token"))
	if p.invalidSelectPath {
		return dbus.ObjectPath("invalid"), nil
	}
	if p.closeOnSelect {
		if err := p.emitSessionClosed(session); err != nil {
			return "", err
		}
	}
	if err := p.emitResponse(request, p.selectCode, map[string]dbus.Variant{}); err != nil {
		return "", err
	}
	return request, nil
}

func (p *fakeRemoteDesktopPortal) selectSources(_ context.Context, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	p.mu.Lock()
	p.selectSourceOptions = options
	p.mu.Unlock()
	request := requestPath(p.name, variantString(options, "handle_token"))
	if p.closeOnSources {
		if err := p.emitSessionClosed(session); err != nil {
			return "", err
		}
	}
	if err := p.emitResponse(request, p.selectSourcesCode, map[string]dbus.Variant{}); err != nil {
		return "", err
	}
	return request, nil
}

func (p *fakeRemoteDesktopPortal) start(_ context.Context, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	request := requestPath(p.name, variantString(options, "handle_token"))
	if p.closeOnStart {
		if err := p.emitSessionClosed(session); err != nil {
			return "", err
		}
	}
	results := map[string]dbus.Variant{
		"devices": dbus.MakeVariant(uint32(p.granted)),
	}
	if p.streams != nil {
		results["streams"] = dbus.MakeVariant(p.streams)
	}
	if p.restoreToken != "" {
		results["restore_token"] = dbus.MakeVariant(p.restoreToken)
	}
	if err := p.emitResponse(request, p.startCode, results); err != nil {
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
	return p.closeRequestErr
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

func (p *fakeRemoteDesktopPortal) waitClosed(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if closed {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
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
	return openRemoteDesktop(ctx, portal, OpenOptions{Devices: devices}, fixedTokens("create", "session", "select", "start"))
}

func openTestSessionWithOptions(ctx context.Context, portal *fakeRemoteDesktopPortal, options OpenOptions) (*Session, error) {
	return openRemoteDesktop(ctx, portal, options, fixedTokens("create", "session", "select", "sources", "start"))
}
