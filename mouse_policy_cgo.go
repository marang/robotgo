//go:build cgo

package robotgo

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	inputportal "github.com/marang/robotgo/input/portal"
)

func lockLinuxMouse() func() {
	// This lock originated with the Wayland/X11 fallback transaction. Native
	// macOS and Windows now use the same lock because their Go ownership ledger
	// must also serialize package-level holds, clicks, and agent drags.
	waylandMouseMu.Lock()
	return waylandMouseMu.Unlock
}

func lockNativeMouseDisplay(server DisplayServer) func() {
	if runtime.GOOS == "linux" && nativeX11BackendCompiled() &&
		server == DisplayServerX11 {
		return lockNativeX11Display()
	}
	return func() {}
}

// runNativeMouseOperation scopes the shared X11 display lease to the native
// probe and event only. Callers retain the Linux mouse transaction lock while
// selecting a portal fallback, but portal I/O never holds nativeX11DisplayMu.
func runNativeMouseOperation(server DisplayServer, operation func() error) (ready bool, err error) {
	unlockDisplay := lockNativeMouseDisplay(server)
	defer unlockDisplay()
	if err := ensureWaylandMouseReady(server); err != nil {
		return false, err
	}
	return true, operation()
}

func ensureWaylandMouseReady(server DisplayServer) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	switch server {
	case DisplayServerX11:
		return nativeX11InputReadyLocked()
	case DisplayServerWayland:
		if !nativeWaylandMouseCompiled() {
			return fmt.Errorf("%w: robotgo was built without the Wayland mouse backend (build with -tags wayland)", ErrNotSupported)
		}
		if !nativeWaylandMouseProbe() {
			return fmt.Errorf("%w: zwlr_virtual_pointer_v1 is unavailable", ErrNotSupported)
		}
		return nil
	default:
		return fmt.Errorf("%w: no supported display server is selected", ErrNotSupported)
	}
}

// MouseReady reports whether the active display backend can inject mouse
// input. On Wayland it performs a real virtual-pointer protocol probe.
func MouseReady() error {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	_, nativeErr := runNativeMouseOperation(server, func() error { return nil })
	if nativeErr == nil {
		return nil
	}
	if server == DisplayServerWayland {
		if used, err := withRemoteDesktopInput(inputportal.DevicePointer, func(remoteDesktopInputSession) error { return nil }); used {
			return err
		}
	}
	return nativeErr
}

func nativeWaylandMouseReady() error {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	_, err := runNativeMouseOperation(server, func() error { return nil })
	return err
}

func moveAbsoluteWithFallback(
	x, y int,
	displayID []int,
	server DisplayServer,
	native func() (bool, error),
	portal func(int, int, []int) (bool, error),
) (usedPortal bool, err error) {
	ready, nativeErr := native()
	if nativeErr == nil {
		return false, nil
	}
	if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
		used, portalErr := portal(x, y, displayID)
		if used {
			return true, portalErr
		}
	}
	return false, nativeErr
}

func moveE(x, y int, applyDelay bool, displayId ...int) error {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	usedPortal, err := moveAbsoluteWithFallback(
		x, y, displayId, server,
		func() (bool, error) {
			return runNativeMouseOperation(server, func() error {
				nativeX, nativeY := moveScaleLocked(x, y, displayId...)
				return nativeMoveMouse(nativeX, nativeY)
			})
		},
		tryRemoteDesktopMoveAbsolute,
	)
	if usedPortal {
		if applyDelay {
			return finishRemoteDesktopMouseEvent(err, 0)
		}
		return err
	}
	if err != nil {
		return err
	}

	if applyDelay {
		MilliSleep(currentMouseDelay())
	}
	return nil
}

// MoveRelativeE moves the mouse by a relative delta and reports backend
// availability errors.
func MoveRelativeE(x, y int) error {
	if runtime.GOOS != "linux" {
		return MoveE(MoveArgs(x, y))
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		dx, dy := moveScaleLocked(x, y)
		nativeMoveMouseRelative(dx, dy)
		return nil
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			used, err := tryRemoteDesktopMoveRelative(x, y)
			if used {
				return finishRemoteDesktopMouseEvent(err, 0)
			}
		}
		return nativeErr
	}
	MilliSleep(currentMouseDelay())
	return nil
}

// LocationE returns the current pointer position. Native Wayland does not
// expose a trustworthy global pointer location, so it returns ErrNotSupported
// instead of presenting the last injected position as an observation.
func LocationE() (int, int, error) {
	unlock := func() {}
	if runtime.GOOS == "linux" {
		switch selectedDisplayServer() {
		case DisplayServerWayland:
			return 0, 0, fmt.Errorf("%w: global pointer location is not exposed by Wayland", ErrNotSupported)
		case DisplayServerX11:
		default:
			return 0, 0, fmt.Errorf("%w: no supported display server is selected", ErrNotSupported)
		}
		unlock = lockNativeX11Display()
		if err := nativeX11DisplayReadyLocked(); err != nil {
			unlock()
			return 0, 0, err
		}
	}
	defer unlock()
	x, y := nativeMouseLocation()

	if currentScale() || runtime.GOOS == "windows" {
		f := scaleFLocked()
		x, y = Scaled0(x, f), Scaled0(y, f)
	}

	return x, y, nil
}

func clickE(applyDelay bool, args ...interface{}) error {
	name, double, err := parseClickArguments(args)
	if err != nil {
		return err
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	name = canonicalMouseHoldName(name)
	if _, held := mouseHolds[name]; held {
		return ErrInputOwnership
	}
	server := selectedDisplayServer()
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		if runtime.GOOS == "windows" {
			return clickTrackedNativeMouseHold(
				mouseHolds,
				name,
				double,
				func(down bool) error { return nativeTrackedMouseToggle(name, down) },
				time.Sleep,
			)
		}
		return nativeClickMouse(name, double)
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			used, err := tryRemoteDesktopClick(name, double)
			if used {
				if applyDelay {
					return finishRemoteDesktopMouseEvent(err, 0)
				}
				return err
			}
		}
		return nativeErr
	}

	if applyDelay {
		MilliSleep(currentMouseDelay())
	}
	return nil
}

// Toggle toggle the mouse, support button:
//
//		"left", "center", "right",
//	 "wheelDown", "wheelUp", "wheelLeft", "wheelRight"
//
// Examples:
//
//	robotgo.Toggle("left") // default is down
//	robotgo.Toggle("left", "up")
func Toggle(key ...interface{}) error {
	name, down, err := parseToggleArguments(key)
	if err != nil {
		return err
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	name = canonicalMouseHoldName(name)
	if runtime.GOOS != "linux" {
		if err := toggleTrackedNativeMouseHold(
			mouseHolds,
			name,
			down,
			func(down bool) error { return nativeTrackedMouseToggle(name, down) },
		); err != nil {
			return err
		}
		if len(key) > 2 {
			MilliSleep(currentMouseDelay())
		}
		return nil
	}
	server := selectedDisplayServer()

	if !down {
		hold, ok := mouseHolds[name]
		if !ok {
			return ErrInputOwnership
		}
		if hold.backend == persistentInputBackendPortal {
			_, err := tryPortalMouseUp(hold)
			delete(mouseHolds, name)
			return err
		}
		_, nativeErr := runNativeMouseOperation(hold.server, func() error {
			return nativeToggleMouse(name, false, "release mouse button")
		})
		if nativeErr == nil || errors.Is(nativeErr, ErrInputOwnership) {
			delete(mouseHolds, name)
		}
		return nativeErr
	}

	if existing, ok := mouseHolds[name]; ok {
		if existing.backend != persistentInputBackendPortal ||
			existing.portalGeneration == remoteDesktopInputGeneration() {
			return ErrInputOwnership
		}
		clearPortalMouseGenerationLocked(existing.portalGeneration)
	}
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		return nativeToggleMouse(name, true, "press mouse button")
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			if used, hold, err := tryPortalMouseDown(server, name); used {
				if err == nil {
					mouseHolds[name] = hold
				}
				return err
			}
		}
		return nativeErr
	}
	mouseHolds[name] = mouseHold{
		backend: persistentInputBackendNative,
		server:  server,
	}
	if len(key) > 2 {
		MilliSleep(currentMouseDelay())
	}

	return nil
}

func scrollE(x, y int, applyDelay bool, args ...int) error {
	msDelay, validationErr := parseScrollDelay(args)
	if validationErr != nil {
		return validationErr
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		nativeScrollMouse(x, y)
		return nil
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			used, err := tryRemoteDesktopScroll(x, y)
			if used {
				if applyDelay {
					return finishRemoteDesktopMouseEvent(err, msDelay)
				}
				return err
			}
		}
		return nativeErr
	}
	if applyDelay {
		MilliSleep(currentMouseDelay() + msDelay)
	}
	return nil
}
