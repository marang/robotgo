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

const (
	portalDestination       = "org.freedesktop.portal.Desktop"
	portalObjectPath        = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	remoteDesktopInterface  = "org.freedesktop.portal.RemoteDesktop"
	screenCastInterface     = "org.freedesktop.portal.ScreenCast"
	requestInterface        = "org.freedesktop.portal.Request"
	requestResponseSignal   = requestInterface + ".Response"
	sessionInterface        = "org.freedesktop.portal.Session"
	sessionClosedSignal     = sessionInterface + ".Closed"
	propertiesGetMethod     = "org.freedesktop.DBus.Properties.Get"
	createSessionMethod     = remoteDesktopInterface + ".CreateSession"
	selectDevicesMethod     = remoteDesktopInterface + ".SelectDevices"
	startMethod             = remoteDesktopInterface + ".Start"
	selectSourcesMethod     = screenCastInterface + ".SelectSources"
	closeRequestMethod      = requestInterface + ".Close"
	closeSessionMethod      = sessionInterface + ".Close"
	notifyPointerMotion     = remoteDesktopInterface + ".NotifyPointerMotion"
	notifyPointerAbsolute   = remoteDesktopInterface + ".NotifyPointerMotionAbsolute"
	notifyPointerButton     = remoteDesktopInterface + ".NotifyPointerButton"
	notifyPointerAxis       = remoteDesktopInterface + ".NotifyPointerAxis"
	notifyPointerDiscrete   = remoteDesktopInterface + ".NotifyPointerAxisDiscrete"
	notifyKeyboardKeycode   = remoteDesktopInterface + ".NotifyKeyboardKeycode"
	notifyKeyboardKeysym    = remoteDesktopInterface + ".NotifyKeyboardKeysym"
	notifyTouchDown         = remoteDesktopInterface + ".NotifyTouchDown"
	notifyTouchMotion       = remoteDesktopInterface + ".NotifyTouchMotion"
	notifyTouchUp           = remoteDesktopInterface + ".NotifyTouchUp"
	requestCleanupTimeout   = time.Second
	sessionCleanupTimeout   = 2 * time.Second
	requestTokenPrefix      = "robotgo_rd_req_"
	sessionTokenPrefix      = "robotgo_rd_session_"
	remoteDesktopVersionKey = "version"
	availableDevicesKey     = "AvailableDeviceTypes"
	availableSourcesKey     = "AvailableSourceTypes"
	availableCursorModesKey = "AvailableCursorModes"
	dbusUnknownInterface    = "org.freedesktop.DBus.Error.UnknownInterface"
	dbusUnknownProperty     = "org.freedesktop.DBus.Error.UnknownProperty"
)

type propertyUint32Func func(context.Context, string, string) (uint32, error)

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
	selectSources(context.Context, dbus.ObjectPath, map[string]dbus.Variant) (dbus.ObjectPath, error)
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
	portal       remoteDesktopPortal
	path         dbus.ObjectPath
	devices      DeviceType
	streams      []Stream
	restoreToken string
	signals      chan *dbus.Signal
	done         chan struct{}

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
	return OpenWithOptions(ctx, OpenOptions{Devices: devices})
}

// OpenWithOptions starts a RemoteDesktop session and optionally attaches
// ScreenCast sources for absolute pointer and touch coordinates.
func OpenWithOptions(ctx context.Context, options OpenOptions) (*Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	portal, err := connectRemoteDesktopPortal()
	if err != nil {
		return nil, err
	}
	session, err := openRemoteDesktop(ctx, portal, options, randomToken)
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
