//go:build linux && !cgo && x11integration

package robotgo

import "time"

// SetX11KeyHoldDelayForIntegrationTest extends or removes the short key hold
// while an external X11 client inspects the server-side mapping.
func SetX11KeyHoldDelayForIntegrationTest(delay time.Duration) func() {
	previous := x11KeyHoldDelay
	x11KeyHoldDelay = delay
	return func() { x11KeyHoldDelay = previous }
}

// SetX11BeforeTextTapHookForIntegrationTest installs an adversarial hook after
// scratch reservation and before the first text key event.
func SetX11BeforeTextTapHookForIntegrationTest(hook func()) func() {
	previous := x11BeforeTextTapHook
	x11BeforeTextTapHook = hook
	return func() { x11BeforeTextTapHook = previous }
}
