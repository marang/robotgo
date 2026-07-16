//go:build linux && !cgo && x11integration

package robotgo

import "time"

// SetX11KeyHoldDelayForIntegrationTest extends or removes the short key hold
// while an external X11 client inspects the server-side mapping.
func SetX11KeyHoldDelayForIntegrationTest(delay time.Duration) func() {
	return linuxX11Input.core.SetKeyHoldDelayForIntegrationTest(delay)
}

// SetX11BeforeTextTapHookForIntegrationTest installs an adversarial hook after
// scratch reservation and before the first text key event.
func SetX11BeforeTextTapHookForIntegrationTest(hook func()) func() {
	return linuxX11Input.core.SetBeforeTextTapHookForIntegrationTest(hook)
}

// X11GuardianPIDForIntegrationTest returns the live Pure-Go X11 guardian PID.
func X11GuardianPIDForIntegrationTest() (int, bool) {
	return linuxX11Input.core.GuardianPIDForIntegrationTest()
}
