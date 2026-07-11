//go:build linux

package portal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const (
	screenCastInterface            = "org.freedesktop.portal.ScreenCast"
	screenCastCreate               = screenCastInterface + ".CreateSession"
	screenCastSelectSources        = screenCastInterface + ".SelectSources"
	screenCastStart                = screenCastInterface + ".Start"
	screenCastOpenPipeWire         = screenCastInterface + ".OpenPipeWireRemote"
	screenCastPropertiesGet        = "org.freedesktop.DBus.Properties.Get"
	portalSessionIF                = "org.freedesktop.portal.Session"
	portalSessionClosed            = portalSessionIF + ".Closed"
	portalRequestClose             = portalRequestIF + ".Close"
	portalSessionClose             = portalSessionIF + ".Close"
	screenCastRequestTimeout       = time.Second
	screenCastSessionTimeout       = 2 * time.Second
	screenCastRequestPrefix        = "robotgo_sc_req_"
	screenCastSessionPrefix        = "robotgo_sc_session_"
	screenCastPropertyVersion      = "version"
	screenCastPropertySources      = "AvailableSourceTypes"
	screenCastPropertyCursors      = "AvailableCursorModes"
	screenCastOptionHandle         = "handle_token"
	screenCastOptionSessionHandle  = "session_handle_token"
	screenCastOptionTypes          = "types"
	screenCastOptionMultiple       = "multiple"
	screenCastOptionCursor         = "cursor_mode"
	screenCastOptionPersist        = "persist_mode"
	screenCastOptionRestoreToken   = "restore_token"
	screenCastResultSession        = "session_handle"
	screenCastResultStreams        = "streams"
	screenCastStreamID             = "id"
	screenCastStreamPosition       = "position"
	screenCastStreamSize           = "size"
	screenCastStreamSourceType     = "source_type"
	screenCastStreamMappingID      = "mapping_id"
	screenCastStreamPipeWireSerial = "pipewire-serial"
)

func probeScreenCast(ctx context.Context) (capability ScreenCastCapability, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return capability, fmt.Errorf("%w: connect session bus: %v", ErrScreenCastUnavailable, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("screencast portal: close probe bus: %w", err))
		}
	}()
	obj := conn.Object(portalDestination, portalObjectPath)
	property := func(name string) (uint32, error) {
		call := obj.CallWithContext(ctx, screenCastPropertiesGet, 0, screenCastInterface, name)
		if call.Err != nil {
			return 0, call.Err
		}
		var value dbus.Variant
		if err := call.Store(&value); err != nil {
			return 0, err
		}
		result, ok := value.Value().(uint32)
		if !ok {
			return 0, fmt.Errorf("property %s has type %T", name, value.Value())
		}
		return result, nil
	}
	version, err := property(screenCastPropertyVersion)
	if err != nil {
		return capability, fmt.Errorf("%w: query version: %w", ErrScreenCastUnavailable, err)
	}
	sources, err := property(screenCastPropertySources)
	if err != nil {
		return capability, fmt.Errorf("%w: query source types: %w", ErrScreenCastUnavailable, err)
	}
	capability.Version = version
	capability.Sources = ScreenCastSource(sources) & screenCastSourceAll
	capability.PipeWireReady = conn.SupportsUnixFDs()
	cursors, err := property(screenCastPropertyCursors)
	if err != nil {
		var dbusErr *dbus.Error
		if errors.As(err, &dbusErr) && dbusErr.Name == "org.freedesktop.DBus.Error.UnknownProperty" {
			capability.CursorModes = ScreenCastCursorHidden
			return capability, nil
		}
		return capability, fmt.Errorf("%w: query cursor modes: %w", ErrScreenCastUnavailable, err)
	}
	capability.CursorModes = ScreenCastCursor(cursors) & screenCastCursorAll
	return capability, nil
}

type screenCastRawStream struct {
	NodeID     uint32
	Properties map[string]dbus.Variant
}

type screenCastDBusPoint struct{ X, Y int32 }

type screenCastPortal interface {
	uniqueName() string
	addRequestMatch() error
	removeRequestMatch() error
	addSessionMatch() error
	removeSessionMatch() error
	registerSignals(chan<- *dbus.Signal)
	removeSignals(chan<- *dbus.Signal)
	connectionDone() <-chan struct{}
	createSession(context.Context, map[string]dbus.Variant) (dbus.ObjectPath, error)
	selectSources(context.Context, dbus.ObjectPath, map[string]dbus.Variant) (dbus.ObjectPath, error)
	start(context.Context, dbus.ObjectPath, map[string]dbus.Variant) (dbus.ObjectPath, error)
	openPipeWire(context.Context, dbus.ObjectPath) (int, error)
	closeRequest(context.Context, dbus.ObjectPath) error
	closeSession(context.Context, dbus.ObjectPath) error
	close() error
}

type dbusScreenCastPortal struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

type screenCastSession struct {
	portal       screenCastPortal
	path         dbus.ObjectPath
	streams      []ScreenCastStream
	restoreToken string
	signals      chan *dbus.Signal
	done         chan struct{}

	fdMu       sync.Mutex
	pipeWireFD int
	finishOnce sync.Once
	finishErr  error
}

type screenCastNegotiation struct {
	ctx            context.Context
	portal         screenCastPortal
	signals        <-chan *dbus.Signal
	closedSessions map[dbus.ObjectPath]struct{}
}

func openScreenCast(ctx context.Context, options ScreenCastOptions) (ScreenCast, error) {
	if os.Getenv(envDisablePortal) != "" {
		return nil, ErrScreenCastUnavailable
	}
	if err := validateScreenCastOptions(options); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	portal, err := connectScreenCastPortal()
	if err != nil {
		return nil, err
	}
	return openScreenCastWithPortal(ctx, portal, options)
}

func connectScreenCastPortal() (screenCastPortal, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("%w: connect session bus: %v", ErrScreenCastUnavailable, err)
	}
	if !conn.SupportsUnixFDs() {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: session bus does not support Unix FD passing", ErrPipeWireUnavailable)
	}
	return &dbusScreenCastPortal{conn: conn, obj: conn.Object(portalDestination, portalObjectPath)}, nil
}

func openScreenCastWithPortal(ctx context.Context, portal screenCastPortal, options ScreenCastOptions) (_ ScreenCast, retErr error) {
	signals := make(chan *dbus.Signal, 16)
	portal.registerSignals(signals)
	cleanupPortal := true
	defer func() {
		if cleanupPortal {
			portal.removeSignals(signals)
			if err := portal.close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("screencast portal: close session bus: %w", err))
			}
		}
	}()
	if err := portal.addSessionMatch(); err != nil {
		return nil, fmt.Errorf("screencast portal: watch session: %w", err)
	}
	removeSessionMatch := true
	defer func() {
		if removeSessionMatch {
			retErr = errors.Join(retErr, portal.removeSessionMatch())
		}
	}()

	negotiation := screenCastNegotiation{
		ctx: ctx, portal: portal, signals: signals,
		closedSessions: make(map[dbus.ObjectPath]struct{}),
	}
	sessionPath, err := negotiation.createSession()
	sessionCreated := sessionPath.IsValid() && !errors.Is(err, ErrScreenCastClosed)
	defer func() {
		if !sessionCreated {
			return
		}
		if _, closed := negotiation.closedSessions[sessionPath]; closed {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), screenCastSessionTimeout)
		defer cancel()
		if closeErr := portal.closeSession(cleanupCtx, sessionPath); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("screencast portal: close failed session: %w", closeErr))
		}
	}()
	if err != nil {
		return nil, err
	}
	if err := negotiation.selectSources(sessionPath, options); err != nil {
		return nil, err
	}
	results, err := negotiation.start(sessionPath)
	if err != nil {
		return nil, err
	}
	streams, err := decodeScreenCastStreams(results)
	if err != nil {
		return nil, err
	}
	if len(streams) == 0 {
		return nil, ErrScreenCastNoStreams
	}
	restoreToken, err := screenCastOptionalString(results, screenCastOptionRestoreToken)
	if err != nil {
		return nil, err
	}
	fd, err := portal.openPipeWire(ctx, sessionPath)
	if err != nil {
		return nil, fmt.Errorf("screencast portal: open PipeWire remote: %w", err)
	}
	if fd < 0 {
		return nil, fmt.Errorf("%w: portal returned invalid fd %d", ErrPipeWireUnavailable, fd)
	}

	session := &screenCastSession{
		portal: portal, path: sessionPath, streams: streams,
		restoreToken: restoreToken, signals: signals, done: make(chan struct{}),
		pipeWireFD: fd,
	}
	cleanupPortal = false
	removeSessionMatch = false
	sessionCreated = false
	go session.monitor()
	return session, nil
}

func (n screenCastNegotiation) createSession() (dbus.ObjectPath, error) {
	requestToken, err := newScreenCastToken(screenCastRequestPrefix)
	if err != nil {
		return "", err
	}
	sessionToken, err := newScreenCastToken(screenCastSessionPrefix)
	if err != nil {
		return "", err
	}
	predictedSession := screenCastSessionPath(n.portal.uniqueName(), sessionToken)
	results, requestErr := n.waitRequest(requestToken, map[dbus.ObjectPath]struct{}{predictedSession: {}}, func() (dbus.ObjectPath, error) {
		return n.portal.createSession(n.ctx, map[string]dbus.Variant{
			screenCastOptionHandle: dbus.MakeVariant(requestToken), screenCastOptionSessionHandle: dbus.MakeVariant(sessionToken),
		})
	})
	if results == nil && requestErr != nil {
		if errors.Is(requestErr, context.Canceled) || errors.Is(requestErr, context.DeadlineExceeded) {
			// CreateSession may have created the predictable session before the
			// caller stopped waiting for its response. Return the predicted path
			// so the outer cleanup can close it defensively.
			return predictedSession, fmt.Errorf("screencast portal: create session: %w", requestErr)
		}
		return "", fmt.Errorf("screencast portal: create session: %w", requestErr)
	}
	path, err := screenCastSessionHandle(results)
	if err != nil {
		// The portal must create this predictable path from our session token.
		// Return it on a malformed response so the caller can still close the
		// remotely created session instead of leaking it.
		return predictedSession, errors.Join(err, requestErr)
	}
	if _, closed := n.closedSessions[path]; closed {
		return path, errors.Join(ErrScreenCastClosed, requestErr)
	}
	return path, requestErr
}

func (n screenCastNegotiation) selectSources(session dbus.ObjectPath, options ScreenCastOptions) error {
	_, err := n.request(session, func(token string) (dbus.ObjectPath, error) {
		values := map[string]dbus.Variant{
			screenCastOptionHandle: dbus.MakeVariant(token), screenCastOptionTypes: dbus.MakeVariant(uint32(options.Sources)),
			screenCastOptionMultiple: dbus.MakeVariant(options.Multiple),
		}
		if options.Cursor != 0 {
			values[screenCastOptionCursor] = dbus.MakeVariant(uint32(options.Cursor))
		}
		if options.Persist != ScreenCastPersistNone {
			values[screenCastOptionPersist] = dbus.MakeVariant(uint32(options.Persist))
		}
		if options.RestoreToken != "" {
			values[screenCastOptionRestoreToken] = dbus.MakeVariant(options.RestoreToken)
		}
		return n.portal.selectSources(n.ctx, session, values)
	})
	if err != nil {
		return fmt.Errorf("screencast portal: select sources: %w", err)
	}
	return nil
}

func (n screenCastNegotiation) start(session dbus.ObjectPath) (map[string]dbus.Variant, error) {
	results, err := n.request(session, func(token string) (dbus.ObjectPath, error) {
		return n.portal.start(n.ctx, session, map[string]dbus.Variant{screenCastOptionHandle: dbus.MakeVariant(token)})
	})
	if err != nil {
		return nil, fmt.Errorf("screencast portal: start session: %w", err)
	}
	return results, nil
}

func (n screenCastNegotiation) request(session dbus.ObjectPath, invoke func(string) (dbus.ObjectPath, error)) (map[string]dbus.Variant, error) {
	token, err := newScreenCastToken(screenCastRequestPrefix)
	if err != nil {
		return nil, err
	}
	return n.waitRequest(token, map[dbus.ObjectPath]struct{}{session: {}}, func() (dbus.ObjectPath, error) { return invoke(token) })
}

func (n screenCastNegotiation) waitRequest(token string, sessions map[dbus.ObjectPath]struct{}, invoke func() (dbus.ObjectPath, error)) (results map[string]dbus.Variant, retErr error) {
	predicted := screenCastRequestPath(n.portal.uniqueName(), token)
	if err := n.portal.addRequestMatch(); err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, n.portal.removeRequestMatch()) }()
	returned, err := invoke()
	if err != nil {
		if n.ctx.Err() != nil {
			return nil, errors.Join(err, n.closeRequest(predicted))
		}
		return nil, err
	}
	if !returned.IsValid() {
		return nil, errors.Join(fmt.Errorf("invalid request path %q", returned), n.closeRequest(predicted))
	}
	for {
		select {
		case signal, ok := <-n.signals:
			if !ok {
				return nil, ErrScreenCastUnavailable
			}
			if signal == nil {
				continue
			}
			if signal.Name == portalSessionClosed {
				n.closedSessions[signal.Path] = struct{}{}
				if _, watched := sessions[signal.Path]; watched {
					return nil, ErrScreenCastClosed
				}
				continue
			}
			if signal.Name != portalResponse || signal.Path != predicted && signal.Path != returned {
				continue
			}
			return parseScreenCastResponse(signal)
		case <-n.ctx.Done():
			return nil, errors.Join(n.ctx.Err(), n.closeRequest(returned))
		case <-n.portal.connectionDone():
			return nil, ErrScreenCastUnavailable
		}
	}
}

func (n screenCastNegotiation) closeRequest(path dbus.ObjectPath) error {
	ctx, cancel := context.WithTimeout(context.Background(), screenCastRequestTimeout)
	defer cancel()
	return n.portal.closeRequest(ctx, path)
}

func (s *screenCastSession) monitor() {
	for {
		select {
		case signal, ok := <-s.signals:
			if !ok || signal != nil && signal.Name == portalSessionClosed && signal.Path == s.path {
				s.finish(false)
				return
			}
		case <-s.portal.connectionDone():
			s.finish(false)
			return
		case <-s.done:
			return
		}
	}
}

func (s *screenCastSession) finish(closeRemote bool) {
	s.finishOnce.Do(func() {
		close(s.done)
		s.fdMu.Lock()
		if s.pipeWireFD >= 0 {
			s.finishErr = errors.Join(s.finishErr, unix.Close(s.pipeWireFD))
			s.pipeWireFD = -1
		}
		s.fdMu.Unlock()
		if closeRemote {
			ctx, cancel := context.WithTimeout(context.Background(), screenCastSessionTimeout)
			if err := s.portal.closeSession(ctx, s.path); err != nil {
				s.finishErr = errors.Join(s.finishErr, fmt.Errorf("screencast portal: close session: %w", err))
			}
			cancel()
		}
		s.finishErr = errors.Join(s.finishErr, s.portal.removeSessionMatch())
		s.portal.removeSignals(s.signals)
		s.finishErr = errors.Join(s.finishErr, s.portal.close())
	})
}

func (s *screenCastSession) Streams() []ScreenCastStream {
	return append([]ScreenCastStream(nil), s.streams...)
}

func (s *screenCastSession) RestoreToken() string { return s.restoreToken }

func (s *screenCastSession) PipeWireFile() (*os.File, error) {
	select {
	case <-s.done:
		return nil, ErrScreenCastClosed
	default:
	}
	s.fdMu.Lock()
	defer s.fdMu.Unlock()
	if s.pipeWireFD < 0 {
		return nil, ErrPipeWireUnavailable
	}
	fd, err := unix.Dup(s.pipeWireFD)
	if err != nil {
		return nil, fmt.Errorf("screencast portal: duplicate PipeWire fd: %w", err)
	}
	unix.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), "robotgo-pipewire-remote"), nil
}

func (s *screenCastSession) Closed() <-chan struct{} { return s.done }

func (s *screenCastSession) Close() error {
	if s == nil {
		return nil
	}
	s.finish(true)
	return s.finishErr
}

func (p *dbusScreenCastPortal) uniqueName() string { return p.conn.Names()[0] }
func (p *dbusScreenCastPortal) addRequestMatch() error {
	return p.conn.AddMatchSignal(dbus.WithMatchSender(portalDestination), dbus.WithMatchInterface(portalRequestIF), dbus.WithMatchMember("Response"))
}
func (p *dbusScreenCastPortal) removeRequestMatch() error {
	return p.conn.RemoveMatchSignal(dbus.WithMatchSender(portalDestination), dbus.WithMatchInterface(portalRequestIF), dbus.WithMatchMember("Response"))
}
func (p *dbusScreenCastPortal) addSessionMatch() error {
	return p.conn.AddMatchSignal(dbus.WithMatchSender(portalDestination), dbus.WithMatchInterface(portalSessionIF), dbus.WithMatchMember("Closed"))
}
func (p *dbusScreenCastPortal) removeSessionMatch() error {
	return p.conn.RemoveMatchSignal(dbus.WithMatchSender(portalDestination), dbus.WithMatchInterface(portalSessionIF), dbus.WithMatchMember("Closed"))
}
func (p *dbusScreenCastPortal) registerSignals(ch chan<- *dbus.Signal) { p.conn.Signal(ch) }
func (p *dbusScreenCastPortal) removeSignals(ch chan<- *dbus.Signal)   { p.conn.RemoveSignal(ch) }
func (p *dbusScreenCastPortal) connectionDone() <-chan struct{}        { return p.conn.Context().Done() }
func (p *dbusScreenCastPortal) createSession(ctx context.Context, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	return p.callPath(ctx, screenCastCreate, options)
}
func (p *dbusScreenCastPortal) selectSources(ctx context.Context, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	return p.callPath(ctx, screenCastSelectSources, session, options)
}
func (p *dbusScreenCastPortal) start(ctx context.Context, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	return p.callPath(ctx, screenCastStart, session, "", options)
}
func (p *dbusScreenCastPortal) callPath(ctx context.Context, method string, args ...interface{}) (dbus.ObjectPath, error) {
	call := p.obj.CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return "", call.Err
	}
	var path dbus.ObjectPath
	return path, call.Store(&path)
}
func (p *dbusScreenCastPortal) openPipeWire(ctx context.Context, session dbus.ObjectPath) (int, error) {
	call := p.obj.CallWithContext(ctx, screenCastOpenPipeWire, 0, session, map[string]dbus.Variant{})
	if call.Err != nil {
		return -1, call.Err
	}
	var fd dbus.UnixFD
	if err := call.Store(&fd); err != nil {
		return -1, err
	}
	return int(fd), nil
}
func (p *dbusScreenCastPortal) closeRequest(ctx context.Context, path dbus.ObjectPath) error {
	return p.conn.Object(portalDestination, path).CallWithContext(ctx, portalRequestClose, 0).Err
}
func (p *dbusScreenCastPortal) closeSession(ctx context.Context, path dbus.ObjectPath) error {
	return p.conn.Object(portalDestination, path).CallWithContext(ctx, portalSessionClose, 0).Err
}
func (p *dbusScreenCastPortal) close() error { return p.conn.Close() }

func newScreenCastToken(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}

func screenCastSenderPath(uniqueName string) string {
	return strings.ReplaceAll(strings.TrimPrefix(uniqueName, ":"), ".", "_")
}
func screenCastRequestPath(uniqueName, token string) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/portal/desktop/request/%s/%s", screenCastSenderPath(uniqueName), token))
}
func screenCastSessionPath(uniqueName, token string) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/portal/desktop/session/%s/%s", screenCastSenderPath(uniqueName), token))
}

func parseScreenCastResponse(signal *dbus.Signal) (map[string]dbus.Variant, error) {
	if len(signal.Body) < 2 {
		return nil, errors.New("screencast portal: malformed response")
	}
	code, ok := signal.Body[0].(uint32)
	if !ok {
		return nil, errors.New("screencast portal: malformed response code")
	}
	results, ok := signal.Body[1].(map[string]dbus.Variant)
	if !ok {
		return nil, errors.New("screencast portal: malformed response results")
	}
	switch code {
	case 0:
		return results, nil
	case 1:
		return nil, ErrScreenCastCancelled
	default:
		return nil, fmt.Errorf("%w (code=%d)", ErrScreenCastRejected, code)
	}
}

func screenCastSessionHandle(results map[string]dbus.Variant) (dbus.ObjectPath, error) {
	value, ok := results[screenCastResultSession]
	if !ok {
		return "", errors.New("screencast portal: missing session handle")
	}
	var path dbus.ObjectPath
	switch handle := value.Value().(type) {
	case string:
		path = dbus.ObjectPath(handle)
	case dbus.ObjectPath:
		path = handle
	default:
		return "", fmt.Errorf("screencast portal: invalid session handle type %T", handle)
	}
	if !path.IsValid() {
		return "", fmt.Errorf("screencast portal: invalid session handle %q", path)
	}
	return path, nil
}

func decodeScreenCastStreams(results map[string]dbus.Variant) ([]ScreenCastStream, error) {
	value, ok := results[screenCastResultStreams]
	if !ok {
		return nil, nil
	}
	var raw []screenCastRawStream
	if err := value.Store(&raw); err != nil {
		return nil, fmt.Errorf("screencast portal: invalid streams: %w", err)
	}
	streams := make([]ScreenCastStream, 0, len(raw))
	seen := make(map[uint32]struct{}, len(raw))
	for _, item := range raw {
		if _, duplicate := seen[item.NodeID]; duplicate {
			return nil, fmt.Errorf("screencast portal: duplicate stream node ID %d", item.NodeID)
		}
		seen[item.NodeID] = struct{}{}
		stream := ScreenCastStream{NodeID: item.NodeID}
		if err := decodeScreenCastProperties(&stream, item.Properties); err != nil {
			return nil, fmt.Errorf("screencast portal: stream %d: %w", item.NodeID, err)
		}
		streams = append(streams, stream)
	}
	return streams, nil
}

func decodeScreenCastProperties(stream *ScreenCastStream, properties map[string]dbus.Variant) error {
	if value, ok := properties[screenCastStreamID]; ok {
		if err := value.Store(&stream.ID); err != nil {
			return fmt.Errorf("invalid id: %w", err)
		}
	}
	if value, ok := properties[screenCastStreamPosition]; ok {
		var position screenCastDBusPoint
		if err := value.Store(&position); err != nil {
			return fmt.Errorf("invalid position: %w", err)
		}
		stream.Position, stream.HasPosition = ScreenCastPoint(position), true
	}
	if value, ok := properties[screenCastStreamSize]; ok {
		var size screenCastDBusPoint
		if err := value.Store(&size); err != nil {
			return fmt.Errorf("invalid size: %w", err)
		}
		if size.X <= 0 || size.Y <= 0 {
			return fmt.Errorf("invalid size %dx%d", size.X, size.Y)
		}
		stream.Size, stream.HasSize = ScreenCastSize{Width: size.X, Height: size.Y}, true
	}
	if value, ok := properties[screenCastStreamSourceType]; ok {
		var source uint32
		if err := value.Store(&source); err != nil {
			return fmt.Errorf("invalid source_type: %w", err)
		}
		stream.SourceType = ScreenCastSource(source) & screenCastSourceAll
	}
	if value, ok := properties[screenCastStreamMappingID]; ok {
		if err := value.Store(&stream.MappingID); err != nil {
			return fmt.Errorf("invalid mapping_id: %w", err)
		}
	}
	if value, ok := properties[screenCastStreamPipeWireSerial]; ok {
		if err := value.Store(&stream.PipeWireSerial); err != nil {
			return fmt.Errorf("invalid pipewire-serial: %w", err)
		}
	}
	return nil
}

func screenCastOptionalString(results map[string]dbus.Variant, key string) (string, error) {
	value, ok := results[key]
	if !ok {
		return "", nil
	}
	var result string
	if err := value.Store(&result); err != nil {
		return "", fmt.Errorf("screencast portal: invalid %s: %w", key, err)
	}
	return result, nil
}
