//go:build linux

package portal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	portalDestination       = "org.freedesktop.portal.Desktop"
	portalObjectPath        = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	remoteDesktopInterface  = "org.freedesktop.portal.RemoteDesktop"
	requestInterface        = "org.freedesktop.portal.Request"
	requestResponseSignal   = requestInterface + ".Response"
	sessionInterface        = "org.freedesktop.portal.Session"
	sessionClosedSignal     = sessionInterface + ".Closed"
	propertiesGetMethod     = "org.freedesktop.DBus.Properties.Get"
	createSessionMethod     = remoteDesktopInterface + ".CreateSession"
	selectDevicesMethod     = remoteDesktopInterface + ".SelectDevices"
	startMethod             = remoteDesktopInterface + ".Start"
	closeRequestMethod      = requestInterface + ".Close"
	closeSessionMethod      = sessionInterface + ".Close"
	notifyPointerMotion     = remoteDesktopInterface + ".NotifyPointerMotion"
	notifyPointerButton     = remoteDesktopInterface + ".NotifyPointerButton"
	notifyPointerAxis       = remoteDesktopInterface + ".NotifyPointerAxis"
	notifyPointerDiscrete   = remoteDesktopInterface + ".NotifyPointerAxisDiscrete"
	notifyKeyboardKeycode   = remoteDesktopInterface + ".NotifyKeyboardKeycode"
	notifyKeyboardKeysym    = remoteDesktopInterface + ".NotifyKeyboardKeysym"
	requestCleanupTimeout   = time.Second
	sessionCleanupTimeout   = 2 * time.Second
	requestTokenPrefix      = "robotgo_rd_req_"
	sessionTokenPrefix      = "robotgo_rd_session_"
	remoteDesktopVersionKey = "version"
	availableDevicesKey     = "AvailableDeviceTypes"
)

type remoteDesktopPortal interface {
	uniqueName() string
	capability(context.Context) (Capability, error)
	addRequestMatch() error
	removeRequestMatch() error
	addSessionMatch() error
	removeSessionMatch() error
	registerSignals(chan<- *dbus.Signal)
	removeSignals(chan<- *dbus.Signal)
	connectionDone() <-chan struct{}
	createSession(context.Context, map[string]dbus.Variant) (dbus.ObjectPath, error)
	selectDevices(context.Context, dbus.ObjectPath, map[string]dbus.Variant) (dbus.ObjectPath, error)
	start(context.Context, dbus.ObjectPath, map[string]dbus.Variant) (dbus.ObjectPath, error)
	closeRequest(context.Context, dbus.ObjectPath) error
	closeSession(context.Context, dbus.ObjectPath) error
	notify(context.Context, string, ...interface{}) error
	close() error
}

type dbusRemoteDesktopPortal struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

// Session is an authorized RemoteDesktop portal input session.
type Session struct {
	portal  remoteDesktopPortal
	path    dbus.ObjectPath
	devices DeviceType
	signals chan *dbus.Signal
	done    chan struct{}

	finishOnce sync.Once
	finishErr  error
}

// Probe queries the live RemoteDesktop interface without starting a session or
// presenting a consent dialog.
func Probe(ctx context.Context) (capability Capability, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	portal, err := connectRemoteDesktopPortal()
	if err != nil {
		return Capability{}, err
	}
	defer func() {
		if err := portal.close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("remote desktop portal: close session bus: %w", err))
		}
	}()
	return portal.capability(ctx)
}

// Open requests the selected input devices and starts a consent-aware remote
// desktop session. The caller controls how long user interaction may take via
// ctx and must close the returned session.
func Open(ctx context.Context, devices DeviceType) (*Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateDevices(devices); err != nil {
		return nil, err
	}
	portal, err := connectRemoteDesktopPortal()
	if err != nil {
		return nil, err
	}
	session, err := openRemoteDesktop(ctx, portal, devices, randomToken)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func connectRemoteDesktopPortal() (remoteDesktopPortal, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("%w: connect session bus: %v", ErrUnavailable, err)
	}
	return &dbusRemoteDesktopPortal{
		conn: conn,
		obj:  conn.Object(portalDestination, portalObjectPath),
	}, nil
}

func (p *dbusRemoteDesktopPortal) uniqueName() string { return p.conn.Names()[0] }

func (p *dbusRemoteDesktopPortal) capability(ctx context.Context) (Capability, error) {
	version, err := p.propertyUint32(ctx, remoteDesktopVersionKey)
	if err != nil {
		return Capability{}, fmt.Errorf("%w: query interface version: %v", ErrUnavailable, err)
	}
	devices, err := p.propertyUint32(ctx, availableDevicesKey)
	if err != nil {
		return Capability{}, fmt.Errorf("%w: query available devices: %v", ErrUnavailable, err)
	}
	return Capability{Version: version, AvailableDevices: DeviceType(devices) & allDeviceTypes}, nil
}

func (p *dbusRemoteDesktopPortal) propertyUint32(ctx context.Context, property string) (uint32, error) {
	call := p.obj.CallWithContext(ctx, propertiesGetMethod, 0, remoteDesktopInterface, property)
	if call.Err != nil {
		return 0, call.Err
	}
	var value dbus.Variant
	if err := call.Store(&value); err != nil {
		return 0, err
	}
	n, ok := value.Value().(uint32)
	if !ok {
		return 0, fmt.Errorf("property %s has type %T", property, value.Value())
	}
	return n, nil
}

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

type tokenFunc func(string) (string, error)
type requestFunc func() (dbus.ObjectPath, error)

func openRemoteDesktop(ctx context.Context, portal remoteDesktopPortal, devices DeviceType, token tokenFunc) (_ *Session, retErr error) {
	signals := make(chan *dbus.Signal, 16)
	portal.registerSignals(signals)
	cleanupPortal := true
	defer func() {
		if cleanupPortal {
			portal.removeSignals(signals)
			if err := portal.close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("remote desktop portal: close session bus: %w", err))
			}
		}
	}()

	requestToken, err := token(requestTokenPrefix)
	if err != nil {
		return nil, err
	}
	sessionToken, err := token(sessionTokenPrefix)
	if err != nil {
		return nil, err
	}
	predictedSession := sessionPath(portal.uniqueName(), sessionToken)
	// Subscribe before CreateSession without constraining the object path. A
	// portal may return a path other than the token-derived prediction and can
	// close that session before the method response is processed.
	if err := portal.addSessionMatch(); err != nil {
		return nil, fmt.Errorf("remote desktop portal: watch session: %w", err)
	}
	removeSessionMatch := true
	defer func() {
		if removeSessionMatch {
			if err := portal.removeSessionMatch(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("remote desktop portal: remove session match: %w", err))
			}
		}
	}()
	closedSessions := make(map[dbus.ObjectPath]struct{})

	createResults, err := waitRequest(ctx, portal, signals, requestToken, map[dbus.ObjectPath]struct{}{predictedSession: {}}, closedSessions, func() (dbus.ObjectPath, error) {
		return portal.createSession(ctx, map[string]dbus.Variant{
			"handle_token":         dbus.MakeVariant(requestToken),
			"session_handle_token": dbus.MakeVariant(sessionToken),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("remote desktop portal: create session: %w", err)
	}
	actualSession, err := resultSessionPath(createResults)
	if err != nil {
		return nil, err
	}
	if _, closed := closedSessions[actualSession]; closed {
		return nil, fmt.Errorf("remote desktop portal: returned session closed during creation: %w", ErrClosed)
	}

	sessionCreated := true
	defer func() {
		if sessionCreated {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), sessionCleanupTimeout)
			defer cancel()
			if err := portal.closeSession(cleanupCtx, actualSession); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("remote desktop portal: close failed session: %w", err))
			}
		}
	}()

	requestToken, err = token(requestTokenPrefix)
	if err != nil {
		return nil, err
	}
	if _, err := waitRequest(ctx, portal, signals, requestToken, map[dbus.ObjectPath]struct{}{actualSession: {}}, closedSessions, func() (dbus.ObjectPath, error) {
		return portal.selectDevices(ctx, actualSession, map[string]dbus.Variant{
			"handle_token": dbus.MakeVariant(requestToken),
			"types":        dbus.MakeVariant(uint32(devices)),
		})
	}); err != nil {
		return nil, fmt.Errorf("remote desktop portal: select devices: %w", err)
	}

	requestToken, err = token(requestTokenPrefix)
	if err != nil {
		return nil, err
	}
	startResults, err := waitRequest(ctx, portal, signals, requestToken, map[dbus.ObjectPath]struct{}{actualSession: {}}, closedSessions, func() (dbus.ObjectPath, error) {
		return portal.start(ctx, actualSession, map[string]dbus.Variant{
			"handle_token": dbus.MakeVariant(requestToken),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("remote desktop portal: start session: %w", err)
	}
	granted, err := resultDevices(startResults)
	if err != nil {
		return nil, err
	}
	if granted&devices != devices {
		return nil, fmt.Errorf("%w: requested=%d granted=%d", ErrDeviceNotGranted, devices, granted)
	}

	session := &Session{
		portal:  portal,
		path:    actualSession,
		devices: granted,
		signals: signals,
		done:    make(chan struct{}),
	}
	cleanupPortal = false
	removeSessionMatch = false
	sessionCreated = false
	go session.monitor()
	return session, nil
}

func waitRequest(ctx context.Context, portal remoteDesktopPortal, signals <-chan *dbus.Signal, token string, sessions map[dbus.ObjectPath]struct{}, closedSessions map[dbus.ObjectPath]struct{}, invoke requestFunc) (map[string]dbus.Variant, error) {
	predicted := requestPath(portal.uniqueName(), token)
	// Subscribe without a path constraint before invoking the method. Some portal
	// implementations return a request path other than the token-derived path,
	// and may emit Response before the method reply reaches the client.
	if err := portal.addRequestMatch(); err != nil {
		return nil, fmt.Errorf("add response match: %w", err)
	}
	defer func() {
		_ = portal.removeRequestMatch()
	}()

	returned, err := invoke()
	if err != nil {
		if ctx.Err() != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), requestCleanupTimeout)
			_ = portal.closeRequest(cleanupCtx, predicted)
			cancel()
		}
		return nil, err
	}
	if !returned.IsValid() {
		return nil, fmt.Errorf("invalid request path %q", returned)
	}
	for {
		select {
		case signal, ok := <-signals:
			if !ok {
				return nil, ErrUnavailable
			}
			if signal == nil {
				continue
			}
			if signal.Name == sessionClosedSignal {
				closedSessions[signal.Path] = struct{}{}
				if _, ok := sessions[signal.Path]; ok {
					return nil, ErrClosed
				}
				continue
			}
			if signal.Name != requestResponseSignal || (signal.Path != predicted && signal.Path != returned) {
				continue
			}
			return parseResponse(signal)
		case <-ctx.Done():
			cleanupCtx, cancel := context.WithTimeout(context.Background(), requestCleanupTimeout)
			_ = portal.closeRequest(cleanupCtx, returned)
			cancel()
			return nil, ctx.Err()
		}
	}
}

func parseResponse(signal *dbus.Signal) (map[string]dbus.Variant, error) {
	if len(signal.Body) < 2 {
		return nil, errors.New("malformed portal response")
	}
	code, ok := signal.Body[0].(uint32)
	if !ok {
		return nil, errors.New("malformed portal response code")
	}
	results, ok := signal.Body[1].(map[string]dbus.Variant)
	if !ok {
		return nil, errors.New("malformed portal response results")
	}
	switch code {
	case 0:
		return results, nil
	case 1:
		return nil, ErrCancelled
	default:
		return nil, fmt.Errorf("%w (code=%d)", ErrRejected, code)
	}
}

func resultSessionPath(results map[string]dbus.Variant) (dbus.ObjectPath, error) {
	value, ok := results["session_handle"]
	if !ok {
		return "", errors.New("remote desktop portal: missing session handle")
	}
	var path dbus.ObjectPath
	switch handle := value.Value().(type) {
	case string:
		path = dbus.ObjectPath(handle)
	case dbus.ObjectPath:
		path = handle
	default:
		return "", fmt.Errorf("remote desktop portal: invalid session handle type %T", value.Value())
	}
	if !path.IsValid() {
		return "", fmt.Errorf("remote desktop portal: invalid session handle %q", path)
	}
	return path, nil
}

func resultDevices(results map[string]dbus.Variant) (DeviceType, error) {
	value, ok := results["devices"]
	if !ok {
		return 0, errors.New("remote desktop portal: missing granted devices")
	}
	devices, ok := value.Value().(uint32)
	if !ok {
		return 0, fmt.Errorf("remote desktop portal: invalid devices type %T", value.Value())
	}
	return DeviceType(devices), nil
}

func (s *Session) monitor() {
	for {
		select {
		case signal, ok := <-s.signals:
			if !ok {
				s.finish(false)
				return
			}
			if signal != nil && signal.Path == s.path && signal.Name == sessionClosedSignal {
				s.finish(false)
				return
			}
		case <-s.done:
			return
		case <-s.portal.connectionDone():
			s.finish(false)
			return
		}
	}
}

func (s *Session) finish(closeRemote bool) {
	s.finishOnce.Do(func() {
		close(s.done)
		if closeRemote {
			ctx, cancel := context.WithTimeout(context.Background(), sessionCleanupTimeout)
			if err := s.portal.closeSession(ctx, s.path); err != nil {
				s.finishErr = errors.Join(s.finishErr, fmt.Errorf("remote desktop portal: close session: %w", err))
			}
			cancel()
		}
		if err := s.portal.removeSessionMatch(); err != nil {
			s.finishErr = errors.Join(s.finishErr, fmt.Errorf("remote desktop portal: remove session match: %w", err))
		}
		s.portal.removeSignals(s.signals)
		if err := s.portal.close(); err != nil {
			s.finishErr = errors.Join(s.finishErr, fmt.Errorf("remote desktop portal: close session bus: %w", err))
		}
	})
}

// Close ends the portal session and releases the D-Bus connection. It is safe
// to call more than once.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.finish(true)
	return s.finishErr
}

// Closed is closed when either the caller or the portal terminates the session.
func (s *Session) Closed() <-chan struct{} { return s.done }

// Devices returns the device types granted by the user.
func (s *Session) Devices() DeviceType { return s.devices }

func (s *Session) ensureDevice(device DeviceType) error {
	select {
	case <-s.done:
		return ErrClosed
	default:
	}
	if s.devices&device == 0 {
		return fmt.Errorf("%w: required=%d granted=%d", ErrDeviceNotGranted, device, s.devices)
	}
	return nil
}

func (s *Session) notify(ctx context.Context, device DeviceType, method string, args ...interface{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.ensureDevice(device); err != nil {
		return err
	}
	callArgs := []interface{}{s.path, map[string]dbus.Variant{}}
	callArgs = append(callArgs, args...)
	if err := s.portal.notify(ctx, method, callArgs...); err != nil {
		// A failed injection leaves no reliable way to distinguish a transient
		// transport error from a portal/session teardown. Retire the local session
		// so readiness never reports a session that may no longer accept input.
		s.finish(false)
		return fmt.Errorf("remote desktop portal: notify input: %w", err)
	}
	return nil
}

// PointerMotion sends relative pointer motion.
func (s *Session) PointerMotion(ctx context.Context, dx, dy float64) error {
	return s.notify(ctx, DevicePointer, notifyPointerMotion, dx, dy)
}

// PointerButton sends a Linux evdev pointer button transition.
func (s *Session) PointerButton(ctx context.Context, button int32, pressed bool) error {
	return s.notify(ctx, DevicePointer, notifyPointerButton, button, boolState(pressed))
}

// PointerAxis sends smooth pointer-axis motion.
func (s *Session) PointerAxis(ctx context.Context, dx, dy float64, finish bool) error {
	options := map[string]dbus.Variant{"finish": dbus.MakeVariant(finish)}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.ensureDevice(DevicePointer); err != nil {
		return err
	}
	if err := s.portal.notify(ctx, notifyPointerAxis, s.path, options, dx, dy); err != nil {
		s.finish(false)
		return fmt.Errorf("remote desktop portal: notify pointer axis: %w", err)
	}
	return nil
}

// PointerAxisDiscrete sends discrete wheel steps.
func (s *Session) PointerAxisDiscrete(ctx context.Context, axis PointerAxis, steps int32) error {
	if axis != PointerAxisVertical && axis != PointerAxisHorizontal {
		return errors.New("remote desktop portal: invalid pointer axis")
	}
	return s.notify(ctx, DevicePointer, notifyPointerDiscrete, uint32(axis), steps)
}

// KeyboardKeycode sends a Linux evdev keycode transition.
func (s *Session) KeyboardKeycode(ctx context.Context, keycode int32, pressed bool) error {
	return s.notify(ctx, DeviceKeyboard, notifyKeyboardKeycode, keycode, boolState(pressed))
}

// KeyboardKeysym sends an XKB keysym transition.
func (s *Session) KeyboardKeysym(ctx context.Context, keysym int32, pressed bool) error {
	return s.notify(ctx, DeviceKeyboard, notifyKeyboardKeysym, keysym, boolState(pressed))
}

func boolState(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}
