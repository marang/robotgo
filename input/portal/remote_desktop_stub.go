//go:build !linux

package portal

import (
	"context"
)

// Session is unavailable outside Linux.
type Session struct{}

// Probe reports that the RemoteDesktop portal is unavailable outside Linux.
func Probe(context.Context) (Capability, error) {
	return Capability{}, ErrUnavailable
}

// Open reports that the RemoteDesktop portal is unavailable outside Linux.
func Open(context.Context, DeviceType) (*Session, error) {
	return nil, ErrUnavailable
}

// OpenWithOptions reports that the RemoteDesktop portal is unavailable.
func OpenWithOptions(context.Context, OpenOptions) (*Session, error) {
	return nil, ErrUnavailable
}

// Close is a no-op for the unsupported stub.
func (*Session) Close() error { return nil }

// Closed returns an already-closed channel for the unsupported stub.
func (*Session) Closed() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

// Devices reports no granted devices for the unsupported stub.
func (*Session) Devices() DeviceType { return 0 }

// Streams reports no attached ScreenCast streams.
func (*Session) Streams() []Stream { return nil }

// RestoreToken reports no persistent session token.
func (*Session) RestoreToken() string { return "" }

// PointerMotion reports that portal input is unavailable.
func (*Session) PointerMotion(context.Context, float64, float64) error { return ErrUnavailable }

// PointerMotionAbsolute reports that portal input is unavailable.
func (*Session) PointerMotionAbsolute(context.Context, uint32, float64, float64) error {
	return ErrUnavailable
}

// PointerButton reports that portal input is unavailable.
func (*Session) PointerButton(context.Context, int32, bool) error { return ErrUnavailable }

// PointerAxis reports that portal input is unavailable.
func (*Session) PointerAxis(context.Context, float64, float64, bool) error { return ErrUnavailable }

// PointerAxisDiscrete reports that portal input is unavailable.
func (*Session) PointerAxisDiscrete(context.Context, PointerAxis, int32) error {
	return ErrUnavailable
}

// KeyboardKeycode reports that portal input is unavailable.
func (*Session) KeyboardKeycode(context.Context, int32, bool) error { return ErrUnavailable }

// KeyboardKeysym reports that portal input is unavailable.
func (*Session) KeyboardKeysym(context.Context, int32, bool) error { return ErrUnavailable }

// TouchDown reports that portal input is unavailable.
func (*Session) TouchDown(context.Context, uint32, uint32, float64, float64) error {
	return ErrUnavailable
}

// TouchMotion reports that portal input is unavailable.
func (*Session) TouchMotion(context.Context, uint32, uint32, float64, float64) error {
	return ErrUnavailable
}

// TouchUp reports that portal input is unavailable.
func (*Session) TouchUp(context.Context, uint32) error { return ErrUnavailable }
