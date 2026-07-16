//go:build linux

package x11input

import (
	"time"

	"github.com/jezek/xgb/xproto"
)

// Setup is the immutable X11 connection information needed by the input core.
type Setup struct {
	Root       xproto.Window
	MinKeycode xproto.Keycode
	MaxKeycode xproto.Keycode
}

// XTestVersion is the server's negotiated XTEST protocol version.
type XTestVersion struct {
	Major byte
	Minor uint16
	Valid bool
}

// KeyboardMapping is a detached snapshot of one contiguous keyboard-map range.
type KeyboardMapping struct {
	KeysymsPerKeycode byte
	Keysyms           []xproto.Keysym
}

// PointerState is the root-relative position and core button mask.
type PointerState struct {
	RootX int16
	RootY int16
	Mask  uint16
}

// Connection owns every X11 primitive used by Backend. The boundary is
// intentionally complete so another process can own the same transaction and
// lifecycle machinery without exposing *xgb.Conn to the stateful core. Close
// must synchronously finish verified transport cleanup and unblock
// WaitForEvent, even when it returns an error.
type Connection interface {
	Close() error
	WaitForEvent() (open bool, err error)
	Setup() (Setup, error)
	InitXTest() error
	XTestVersion(major byte, minor uint16) (XTestVersion, error)
	GrabServer() error
	UngrabServer() error
	KeyboardMapping(first xproto.Keycode, count byte) (KeyboardMapping, error)
	ModifierMapping() ([]xproto.Keycode, error)
	ChangeKeyboardMapping(first xproto.Keycode, perKeycode byte, keysyms []xproto.Keysym) error
	PressedKeys() ([]byte, error)
	QueryPointer(root xproto.Window) (PointerState, error)
	FakeInput(eventType, detail byte, root xproto.Window, x, y int16) error
}

// Dialer creates one complete X11 connection owned by Backend.
type Dialer interface {
	Dial(display string) (Connection, error)
}

// Config defines immutable dependencies and timings for one Backend.
type Config struct {
	ResolveDisplay func() (string, error)
	Dialer         Dialer
	KeyHoldDelay   time.Duration
	Sleep          func(time.Duration)
}
