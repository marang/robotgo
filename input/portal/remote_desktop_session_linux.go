//go:build linux

package portal

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

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
