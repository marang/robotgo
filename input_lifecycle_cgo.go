//go:build cgo

package robotgo

import (
	"errors"
	"strings"
)

// SetXDisplayName set XDisplay name (Linux)
func SetXDisplayName(name string) error {
	if strings.IndexByte(name, 0) >= 0 {
		return errors.New("robotgo: X11 display name contains NUL")
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	unlockDisplay := lockNativeX11Display()
	defer unlockDisplay()
	mouseReleaseErr := releaseNativeMouseHoldsLocked(DisplayServerX11)
	releaseErr := releaseNativeX11KeyboardOwnershipLocked()
	clearNativeKeyboardStateLocked(DisplayServerX11)
	if err := nativeSetXDisplayName(name); err != nil {
		return errors.Join(mouseReleaseErr, releaseErr, err)
	}
	return errors.Join(mouseReleaseErr, releaseErr)
}

// CloseMainDisplay closes the main display and ignores cleanup errors for
// compatibility. Prefer CloseMainDisplayE in new code.
func CloseMainDisplay() { _ = CloseMainDisplayE() }

// CloseMainDisplayE releases RobotGo-owned native mouse holds and X11 keys,
// then closes the native main display. Failed mouse releases remain owned so a
// later call can retry them.
func CloseMainDisplayE() error {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	unlockDisplay := lockNativeX11Display()
	defer unlockDisplay()
	mouseReleaseErr := releaseNativeMouseHoldsLocked(DisplayServerX11)
	releaseErr := releaseNativeX11KeyboardOwnershipLocked()
	clearNativeKeyboardStateLocked(DisplayServerX11)
	nativeCloseMainDisplay()
	return errors.Join(mouseReleaseErr, releaseErr)
}

// CloseWaylandInput releases persistent virtual-pointer and virtual-keyboard
// protocol objects. A later input call reconnects lazily.
func CloseWaylandInput() {
	// Cancel portal I/O before waiting for a device transaction lock. Portal
	// operations hold one of these locks while using their cancellable context.
	_ = CloseRemoteDesktopInput()
	waylandMouseMu.Lock()
	defer waylandMouseMu.Unlock()
	linuxKeyboardMu.Lock()
	defer linuxKeyboardMu.Unlock()
	_ = releaseNativeMouseHoldsLocked(DisplayServerWayland)
	nativeCloseWaylandMouse()
	closeWaylandKeyboard()
	clearNativeKeyboardStateLocked(DisplayServerWayland)
	clearPortalMouseStateLocked()
	clearPortalKeyboardStateLocked()
}
