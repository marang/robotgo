//go:build linux

package x11input

import (
	"errors"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

type xgbDialer struct{}

var _ Dialer = xgbDialer{}

func (xgbDialer) Dial(display string) (Connection, error) {
	connection, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, err
	}
	return &xgbConnection{connection: connection, events: connection}, nil
}

type xgbEventSource interface {
	Close()
	WaitForEvent() (xgb.Event, xgb.Error)
}

type xgbConnection struct {
	connection *xgb.Conn
	events     xgbEventSource
	closeOnce  sync.Once
}

var _ Connection = (*xgbConnection)(nil)

func (connection *xgbConnection) Close() error {
	connection.closeOnce.Do(func() {
		// xgb.Conn.Close only queues the close request. Drain until xgb's
		// event channel is actually closed so callers know that no event-pump
		// goroutine can still be using this transport when Close returns.
		connection.events.Close()
		for {
			event, err := connection.events.WaitForEvent()
			if event == nil && err == nil {
				return
			}
		}
	})
	return nil
}

func (connection *xgbConnection) WaitForEvent() (bool, error) {
	event, err := connection.events.WaitForEvent()
	return event != nil || err != nil, err
}

func (connection *xgbConnection) Setup() (Setup, error) {
	setup := xproto.Setup(connection.connection)
	if setup == nil {
		return Setup{}, errors.New("X11 returned no setup information")
	}
	screen := setup.DefaultScreen(connection.connection)
	if screen == nil || screen.Root == 0 {
		return Setup{}, errors.New("X11 returned no default root window")
	}
	return Setup{Root: screen.Root, MinKeycode: setup.MinKeycode, MaxKeycode: setup.MaxKeycode}, nil
}

func (connection *xgbConnection) InitXTest() error { return xtest.Init(connection.connection) }

func (connection *xgbConnection) XTestVersion(major byte, minor uint16) (XTestVersion, error) {
	reply, err := xtest.GetVersion(connection.connection, major, minor).Reply()
	if err != nil {
		return XTestVersion{}, err
	}
	if reply == nil {
		return XTestVersion{}, nil
	}
	return XTestVersion{Major: reply.MajorVersion, Minor: reply.MinorVersion, Valid: true}, nil
}

func (connection *xgbConnection) GrabServer() error {
	return xproto.GrabServerChecked(connection.connection).Check()
}

func (connection *xgbConnection) UngrabServer() error {
	return xproto.UngrabServerChecked(connection.connection).Check()
}

func (connection *xgbConnection) KeyboardMapping(first xproto.Keycode, count byte) (KeyboardMapping, error) {
	reply, err := xproto.GetKeyboardMapping(connection.connection, first, count).Reply()
	if err != nil {
		return KeyboardMapping{}, err
	}
	if reply == nil {
		return KeyboardMapping{}, errors.New("server returned no keyboard map")
	}
	return KeyboardMapping{
		KeysymsPerKeycode: reply.KeysymsPerKeycode,
		Keysyms:           append([]xproto.Keysym(nil), reply.Keysyms...),
	}, nil
}

func (connection *xgbConnection) ModifierMapping() ([]xproto.Keycode, error) {
	reply, err := xproto.GetModifierMapping(connection.connection).Reply()
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, errors.New("server returned an empty modifier map")
	}
	return append([]xproto.Keycode(nil), reply.Keycodes...), nil
}

func (connection *xgbConnection) ChangeKeyboardMapping(first xproto.Keycode, perKeycode byte, keysyms []xproto.Keysym) error {
	return xproto.ChangeKeyboardMappingChecked(
		connection.connection, 1, first, perKeycode, keysyms,
	).Check()
}

func (connection *xgbConnection) PressedKeys() ([]byte, error) {
	reply, err := xproto.QueryKeymap(connection.connection).Reply()
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, errors.New("X11 returned no pressed-key map")
	}
	return append([]byte(nil), reply.Keys...), nil
}

func (connection *xgbConnection) QueryPointer(root xproto.Window) (PointerState, error) {
	reply, err := xproto.QueryPointer(connection.connection, root).Reply()
	if err != nil {
		return PointerState{}, err
	}
	if reply == nil {
		return PointerState{}, errors.New("server returned no pointer reply")
	}
	return PointerState{RootX: reply.RootX, RootY: reply.RootY, Mask: reply.Mask}, nil
}

func (connection *xgbConnection) FakeInput(eventType, detail byte, root xproto.Window, x, y int16) error {
	return xtest.FakeInputChecked(
		connection.connection, eventType, detail, 0, root, x, y, 0,
	).Check()
}
