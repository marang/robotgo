//go:build linux && cgo && !wayland
// +build linux,cgo,!wayland

package robotgo

/*
#include <X11/Xlib.h>
#include <X11/extensions/XTest.h>

Display *XGetMainDisplay(void);

enum RobotGoX11ProbeStatus {
	ROBOTGO_X11_PROBE_OK = 0,
	ROBOTGO_X11_PROBE_NO_DISPLAY = 1,
	ROBOTGO_X11_PROBE_NO_XTEST = 2,
	ROBOTGO_X11_PROBE_OLD_XTEST = 3
};

static int robotgo_x11_probe_display(void) {
	return XGetMainDisplay() == NULL ? ROBOTGO_X11_PROBE_NO_DISPLAY : ROBOTGO_X11_PROBE_OK;
}

static int robotgo_x11_probe_input(int *major, int *minor) {
	Display *display = XGetMainDisplay();
	int event_base = 0;
	int error_base = 0;

	*major = 0;
	*minor = 0;
	if (display == NULL) {
		return ROBOTGO_X11_PROBE_NO_DISPLAY;
	}
	if (!XTestQueryExtension(display, &event_base, &error_base, major, minor)) {
		return ROBOTGO_X11_PROBE_NO_XTEST;
	}
	if (*major < 2 || (*major == 2 && *minor < 2)) {
		return ROBOTGO_X11_PROBE_OLD_XTEST;
	}
	return ROBOTGO_X11_PROBE_OK;
}
*/
import "C"

import (
	"fmt"
	"sync"
)

var (
	nativeX11DisplayMu             sync.Mutex
	errNativeX11DisplayUnavailable = fmt.Errorf("%w: X11 display is unavailable", ErrNotSupported)
)

func nativeX11BackendCompiled() bool { return true }

func lockNativeX11Display() func() {
	nativeX11DisplayMu.Lock()
	return nativeX11DisplayMu.Unlock
}

func configuredX11DisplaySelected() bool {
	unlock := lockNativeX11Display()
	defer unlock()
	return getXDisplayNameLocked() != ""
}

func nativeX11DisplayReadyLocked() error {
	if int(C.robotgo_x11_probe_display()) != int(C.ROBOTGO_X11_PROBE_OK) {
		return errNativeX11DisplayUnavailable
	}
	return nil
}

func nativeX11InputReadyLocked() error {
	var major C.int
	var minor C.int
	status := int(C.robotgo_x11_probe_input(&major, &minor))
	switch status {
	case int(C.ROBOTGO_X11_PROBE_OK):
		return nil
	case int(C.ROBOTGO_X11_PROBE_NO_DISPLAY):
		return errNativeX11DisplayUnavailable
	case int(C.ROBOTGO_X11_PROBE_NO_XTEST):
		return fmt.Errorf("%w: X11 XTEST extension is unavailable", ErrNotSupported)
	case int(C.ROBOTGO_X11_PROBE_OLD_XTEST):
		return fmt.Errorf("%w: X11 XTEST version %d.%d is older than required version 2.2", ErrNotSupported, int(major), int(minor))
	default:
		return fmt.Errorf("robotgo: unknown X11 input probe status %d", status)
	}
}

func nativeX11CapabilityErrors() (displayErr error, inputErr error) {
	unlock := lockNativeX11Display()
	defer unlock()
	displayErr = nativeX11DisplayReadyLocked()
	if displayErr != nil {
		return displayErr, displayErr
	}
	return nil, nativeX11InputReadyLocked()
}

func runtimeX11CapabilityErrors() (displayErr error, inputErr error) {
	return nativeX11CapabilityErrors()
}

func nativeX11ProtocolVersion() (major, minor int, negotiated bool) {
	unlock := lockNativeX11Display()
	defer unlock()
	var cMajor C.int
	var cMinor C.int
	if int(C.robotgo_x11_probe_input(&cMajor, &cMinor)) != int(C.ROBOTGO_X11_PROBE_OK) {
		return int(cMajor), int(cMinor), false
	}
	return int(cMajor), int(cMinor), true
}

func x11MainDisplayAvailableLocked() bool {
	return nativeX11DisplayReadyLocked() == nil
}
