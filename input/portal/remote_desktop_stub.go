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

// PointerMotion reports that portal input is unavailable.
func (*Session) PointerMotion(context.Context, float64, float64) error { return ErrUnavailable }

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
