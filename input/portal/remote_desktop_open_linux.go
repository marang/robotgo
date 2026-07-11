//go:build linux

package portal

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

type tokenFunc func(string) (string, error)
type requestFunc func() (dbus.ObjectPath, error)

type remoteDesktopNegotiation struct {
	ctx            context.Context
	portal         remoteDesktopPortal
	signals        <-chan *dbus.Signal
	token          tokenFunc
	closedSessions map[dbus.ObjectPath]struct{}
}

func openRemoteDesktop(ctx context.Context, portal remoteDesktopPortal, options OpenOptions, token tokenFunc) (_ *Session, retErr error) {
	devices := options.Devices
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
	negotiation := remoteDesktopNegotiation{ctx: ctx, portal: portal, signals: signals, token: token, closedSessions: closedSessions}
	actualSession, err := negotiation.createSession()
	// A valid path normally transfers cleanup ownership to this function. When
	// CreateSession already delivered Session.Closed, the portal owns teardown
	// and must not receive a redundant Close call.
	sessionCreated := actualSession.IsValid() && !errors.Is(err, ErrClosed)
	defer func() {
		if sessionCreated {
			if _, closed := closedSessions[actualSession]; closed {
				return
			}
			cleanupCtx, cancel := context.WithTimeout(context.Background(), sessionCleanupTimeout)
			defer cancel()
			if err := portal.closeSession(cleanupCtx, actualSession); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("remote desktop portal: close failed session: %w", err))
			}
		}
	}()
	if err != nil {
		return nil, err
	}

	if err := negotiation.selectDevices(actualSession, options); err != nil {
		return nil, err
	}
	if err := negotiation.selectSources(actualSession, options); err != nil {
		return nil, err
	}
	startResults, err := negotiation.start(actualSession)
	if err != nil {
		return nil, err
	}
	granted, err := resultDevices(startResults)
	if err != nil {
		return nil, err
	}
	if granted&devices != devices {
		return nil, fmt.Errorf("%w: requested=%d granted=%d", ErrDeviceNotGranted, devices, granted)
	}
	streams, err := resultStreams(startResults)
	if err != nil {
		return nil, err
	}
	if options.Sources != 0 && len(streams) == 0 {
		return nil, fmt.Errorf("%w: portal returned no streams", ErrScreenCastRequired)
	}
	restoreToken, err := resultOptionalString(startResults, "restore_token")
	if err != nil {
		return nil, err
	}

	session := &Session{
		portal:       portal,
		path:         actualSession,
		devices:      granted,
		streams:      streams,
		restoreToken: restoreToken,
		signals:      signals,
		done:         make(chan struct{}),
	}
	cleanupPortal = false
	removeSessionMatch = false
	sessionCreated = false
	go session.monitor()
	return session, nil
}

func (n remoteDesktopNegotiation) createSession() (dbus.ObjectPath, error) {
	requestToken, err := n.token(requestTokenPrefix)
	if err != nil {
		return "", err
	}
	sessionToken, err := n.token(sessionTokenPrefix)
	if err != nil {
		return "", err
	}
	predicted := sessionPath(n.portal.uniqueName(), sessionToken)
	invoked := false
	results, requestErr := n.waitRequest(requestToken, map[dbus.ObjectPath]struct{}{predicted: {}}, func() (dbus.ObjectPath, error) {
		invoked = true
		return n.portal.createSession(n.ctx, map[string]dbus.Variant{
			"handle_token":         dbus.MakeVariant(requestToken),
			"session_handle_token": dbus.MakeVariant(sessionToken),
		})
	})
	if results == nil && requestErr != nil {
		// CreateSession can create the token-derived session before its method
		// reply or Response signal reaches the client. Preserve the predictable
		// path for outer cleanup whenever the outcome is ambiguous.
		if invoked && !errors.Is(requestErr, ErrCancelled) &&
			!errors.Is(requestErr, ErrRejected) &&
			!errors.Is(requestErr, ErrClosed) &&
			!errors.Is(requestErr, ErrUnavailable) {
			return predicted, fmt.Errorf("remote desktop portal: create session: %w", requestErr)
		}
		return "", fmt.Errorf("remote desktop portal: create session: %w", requestErr)
	}
	actual, err := resultSessionPath(results)
	if err != nil {
		// A malformed success response can still correspond to the predictable
		// session created from session_handle_token.
		return predicted, errors.Join(err, requestErr)
	}
	if _, closed := n.closedSessions[actual]; closed {
		return actual, errors.Join(fmt.Errorf("remote desktop portal: returned session closed during creation: %w", ErrClosed), requestErr)
	}
	if requestErr != nil {
		return actual, fmt.Errorf("remote desktop portal: create session: %w", requestErr)
	}
	return actual, nil
}

func (n remoteDesktopNegotiation) request(session dbus.ObjectPath, invoke func(string) (dbus.ObjectPath, error)) (map[string]dbus.Variant, error) {
	requestToken, err := n.token(requestTokenPrefix)
	if err != nil {
		return nil, err
	}
	return n.waitRequest(requestToken, map[dbus.ObjectPath]struct{}{session: {}}, func() (dbus.ObjectPath, error) {
		return invoke(requestToken)
	})
}

func (n remoteDesktopNegotiation) selectDevices(session dbus.ObjectPath, options OpenOptions) error {
	_, err := n.request(session, func(requestToken string) (dbus.ObjectPath, error) {
		selectOptions := map[string]dbus.Variant{
			"handle_token": dbus.MakeVariant(requestToken),
			"types":        dbus.MakeVariant(uint32(options.Devices)),
		}
		if options.PersistMode != PersistNone {
			selectOptions["persist_mode"] = dbus.MakeVariant(uint32(options.PersistMode))
		}
		if options.RestoreToken != "" {
			selectOptions["restore_token"] = dbus.MakeVariant(options.RestoreToken)
		}
		return n.portal.selectDevices(n.ctx, session, selectOptions)
	})
	if err != nil {
		return fmt.Errorf("remote desktop portal: select devices: %w", err)
	}
	return nil
}

func (n remoteDesktopNegotiation) selectSources(session dbus.ObjectPath, options OpenOptions) error {
	if options.Sources == 0 {
		return nil
	}
	_, err := n.request(session, func(requestToken string) (dbus.ObjectPath, error) {
		sourceOptions := map[string]dbus.Variant{
			"handle_token": dbus.MakeVariant(requestToken),
			"types":        dbus.MakeVariant(uint32(options.Sources)),
			"multiple":     dbus.MakeVariant(options.Multiple),
		}
		if options.CursorMode != 0 {
			sourceOptions["cursor_mode"] = dbus.MakeVariant(uint32(options.CursorMode))
		}
		return n.portal.selectSources(n.ctx, session, sourceOptions)
	})
	if err != nil {
		return fmt.Errorf("remote desktop portal: select ScreenCast sources: %w", err)
	}
	return nil
}

func (n remoteDesktopNegotiation) start(session dbus.ObjectPath) (map[string]dbus.Variant, error) {
	results, err := n.request(session, func(requestToken string) (dbus.ObjectPath, error) {
		return n.portal.start(n.ctx, session, map[string]dbus.Variant{"handle_token": dbus.MakeVariant(requestToken)})
	})
	if err != nil {
		return nil, fmt.Errorf("remote desktop portal: start session: %w", err)
	}
	return results, nil
}

func (n remoteDesktopNegotiation) waitRequest(token string, sessions map[dbus.ObjectPath]struct{}, invoke requestFunc) (results map[string]dbus.Variant, retErr error) {
	predicted := requestPath(n.portal.uniqueName(), token)
	// Subscribe without a path constraint before invoking the method. Some portal
	// implementations return a request path other than the token-derived path,
	// and may emit Response before the method reply reaches the client.
	if err := n.portal.addRequestMatch(); err != nil {
		return nil, fmt.Errorf("add response match: %w", err)
	}
	defer func() {
		if err := n.portal.removeRequestMatch(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("remove response match: %w", err))
		}
	}()

	returned, err := invoke()
	if err != nil {
		if n.ctx.Err() != nil {
			return nil, errors.Join(err, n.closeRequest(predicted))
		}
		return nil, err
	}
	if !returned.IsValid() {
		pathErr := fmt.Errorf("invalid request path %q", returned)
		return nil, errors.Join(pathErr, n.closeRequest(predicted))
	}
	for {
		select {
		case signal, ok := <-n.signals:
			if !ok {
				return nil, ErrUnavailable
			}
			if signal == nil {
				continue
			}
			if signal.Name == sessionClosedSignal {
				n.closedSessions[signal.Path] = struct{}{}
				if _, ok := sessions[signal.Path]; ok {
					return nil, ErrClosed
				}
				continue
			}
			if signal.Name != requestResponseSignal || (signal.Path != predicted && signal.Path != returned) {
				continue
			}
			return parseResponse(signal)
		case <-n.ctx.Done():
			return nil, errors.Join(n.ctx.Err(), n.closeRequest(returned))
		case <-n.portal.connectionDone():
			return nil, ErrUnavailable
		}
	}
}

func (n remoteDesktopNegotiation) closeRequest(path dbus.ObjectPath) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), requestCleanupTimeout)
	defer cancel()
	if err := n.portal.closeRequest(cleanupCtx, path); err != nil {
		return fmt.Errorf("close request %q: %w", path, err)
	}
	return nil
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
