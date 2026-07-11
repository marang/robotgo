//go:build linux

package portal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

func (p *dbusRemoteDesktopPortal) uniqueName() string { return p.conn.Names()[0] }

func (p *dbusRemoteDesktopPortal) addRequestMatch() error {
	return p.conn.AddMatchSignal(
		dbus.WithMatchSender(portalDestination),
		dbus.WithMatchInterface(requestInterface),
		dbus.WithMatchMember("Response"),
	)
}

func (p *dbusRemoteDesktopPortal) removeRequestMatch() error {
	return p.conn.RemoveMatchSignal(
		dbus.WithMatchSender(portalDestination),
		dbus.WithMatchInterface(requestInterface),
		dbus.WithMatchMember("Response"),
	)
}

func (p *dbusRemoteDesktopPortal) addSessionMatch() error {
	return p.conn.AddMatchSignal(
		dbus.WithMatchSender(portalDestination),
		dbus.WithMatchInterface(sessionInterface),
		dbus.WithMatchMember("Closed"),
	)
}

func (p *dbusRemoteDesktopPortal) removeSessionMatch() error {
	return p.conn.RemoveMatchSignal(
		dbus.WithMatchSender(portalDestination),
		dbus.WithMatchInterface(sessionInterface),
		dbus.WithMatchMember("Closed"),
	)
}

func (p *dbusRemoteDesktopPortal) registerSignals(ch chan<- *dbus.Signal) {
	p.conn.Signal(ch)
}

func (p *dbusRemoteDesktopPortal) removeSignals(ch chan<- *dbus.Signal) {
	p.conn.RemoveSignal(ch)
}

func (p *dbusRemoteDesktopPortal) connectionDone() <-chan struct{} {
	return p.conn.Context().Done()
}

func (p *dbusRemoteDesktopPortal) createSession(ctx context.Context, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	return p.callPath(ctx, createSessionMethod, options)
}

func (p *dbusRemoteDesktopPortal) selectDevices(ctx context.Context, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	return p.callPath(ctx, selectDevicesMethod, session, options)
}

func (p *dbusRemoteDesktopPortal) selectSources(ctx context.Context, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	return p.callPath(ctx, selectSourcesMethod, session, options)
}

func (p *dbusRemoteDesktopPortal) start(ctx context.Context, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	return p.callPath(ctx, startMethod, session, "", options)
}

func (p *dbusRemoteDesktopPortal) callPath(ctx context.Context, method string, args ...interface{}) (dbus.ObjectPath, error) {
	call := p.obj.CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return "", call.Err
	}
	var path dbus.ObjectPath
	if err := call.Store(&path); err != nil {
		return "", err
	}
	return path, nil
}

func (p *dbusRemoteDesktopPortal) closeRequest(ctx context.Context, path dbus.ObjectPath) error {
	return p.conn.Object(portalDestination, path).CallWithContext(ctx, closeRequestMethod, 0).Err
}

func (p *dbusRemoteDesktopPortal) closeSession(ctx context.Context, path dbus.ObjectPath) error {
	return p.conn.Object(portalDestination, path).CallWithContext(ctx, closeSessionMethod, 0).Err
}

func (p *dbusRemoteDesktopPortal) notify(ctx context.Context, method string, args ...interface{}) error {
	return p.obj.CallWithContext(ctx, method, 0, args...).Err
}

func (p *dbusRemoteDesktopPortal) close() error { return p.conn.Close() }

func randomToken(prefix string) (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("remote desktop portal: generate token: %w", err)
	}
	return prefix + hex.EncodeToString(data[:]), nil
}

func senderPath(uniqueName string) string {
	name := strings.TrimPrefix(uniqueName, ":")
	return strings.ReplaceAll(name, ".", "_")
}

func requestPath(uniqueName, token string) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/portal/desktop/request/%s/%s", senderPath(uniqueName), token))
}

func sessionPath(uniqueName, token string) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/portal/desktop/session/%s/%s", senderPath(uniqueName), token))
}
