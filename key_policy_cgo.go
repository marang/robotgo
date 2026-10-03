//go:build cgo

package robotgo

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	inputportal "github.com/marang/robotgo/input/portal"
)

var errInvalidKeyFlag = errors.New("invalid key flag specified")

var (
	errWaylandKeyboardUnavailable = errors.New("wayland virtual keyboard unavailable")
	errWaylandKeyboardNotBuilt    = errors.New("wayland session detected but robotgo was built without wayland keyboard backend (build with -tags wayland)")
	errWaylandKeyboardNoDisplay   = errors.New("wayland display connection failed")
	errWaylandKeyboardNoSeat      = errors.New("wayland seat not found")
	errWaylandKeyboardNoManager   = errors.New("zwp_virtual_keyboard_manager_v1 not available")
	errWaylandKeyboardCreate      = errors.New("failed to create virtual keyboard")
	errWaylandKeyboardXKB         = errors.New("failed to initialize xkb context")
	errWaylandKeyboardKeymap      = errors.New("failed to build xkb keymap")
	errWaylandKeyboardMemfd       = errors.New("failed to setup wayland keymap memfd")
	errWaylandKeyboardKeysym      = errors.New("key symbol not present in wayland keymap")
)

var linuxKeyboardMu sync.Mutex

func lockLinuxKeyboard() func() {
	if runtime.GOOS == "linux" {
		linuxKeyboardMu.Lock()
		return linuxKeyboardMu.Unlock
	}
	return func() {}
}

func lockNativeKeyboardDisplay(server DisplayServer) func() {
	if runtime.GOOS == "linux" && nativeX11BackendCompiled() &&
		server == DisplayServerX11 {
		return lockNativeX11Display()
	}
	return func() {}
}

// runNativeKeyboardOperation keeps the X11 display lease scoped to the native
// probe and operation. Callers keep linuxKeyboardMu while deciding whether to
// use the RemoteDesktop portal, but portal I/O never runs under the X11 lease.
func runNativeKeyboardOperation(server DisplayServer, operation func() error) (ready bool, err error) {
	unlockDisplay := lockNativeKeyboardDisplay(server)
	defer unlockDisplay()
	if err := ensureWaylandKeyboardReady(server); err != nil {
		return false, err
	}
	return true, operation()
}

func shouldTryRemoteDesktopAfterNative(server DisplayServer, ready bool, err error) bool {
	return runtime.GOOS == "linux" && server == DisplayServerWayland &&
		(!ready || errors.Is(err, ErrNotSupported))
}

func ensureWaylandKeyboardReady(server DisplayServer) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	switch server {
	case DisplayServerX11:
		return nativeX11InputReadyLocked()
	case DisplayServerWayland:
		if !waylandKeyboardBackendCompiled() {
			return errWaylandKeyboardNotBuilt
		}
		return nativeWaylandKeyboardProbe()
	default:
		return fmt.Errorf("%w: no supported display server is selected", ErrNotSupported)
	}
}

// KeyboardReady reports whether the active display backend can inject
// keyboard input. On Wayland it performs a real virtual-keyboard probe.
func KeyboardReady() error {
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	server := selectedDisplayServer()
	_, nativeErr := runNativeKeyboardOperation(server, func() error { return nil })
	if nativeErr == nil {
		return nil
	}
	if server == DisplayServerWayland {
		if used, err := withRemoteDesktopInput(inputportal.DeviceKeyboard, func(remoteDesktopInputSession) error { return nil }); used {
			return err
		}
	}
	return nativeErr
}

func nativeWaylandKeyboardReady() error {
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	server := selectedDisplayServer()
	_, err := runNativeKeyboardOperation(server, func() error { return nil })
	return err
}

func keyTaps(k string, keyArr []string, pid int) error {
	flags, err := nativeKeyFlags(keyArr)
	if err != nil {
		return err
	}
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	server := selectedDisplayServer()
	ready, nativeErr := runNativeKeyboardOperation(server, func() error {
		return nativeTapKey(k, flags, pid)
	})
	if nativeErr != nil {
		if !shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			return nativeErr
		}
		if used, err := tryPortalKeyTap(server, k, keyArr, pid); used {
			if err == nil {
				MilliSleep(currentKeyDelay())
			}
			return err
		}
		return nativeErr
	}
	MilliSleep(currentKeyDelay())
	return nil
}

func keyTogglesB(k string, down bool, keyArr []string, pid int, applyDelay bool) error {
	flags, err := nativeKeyFlags(keyArr)
	if err != nil {
		return err
	}
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	if runtime.GOOS != "linux" {
		nativeErr := nativeToggleKey(k, down, flags, pid)
		if nativeErr == nil && applyDelay {
			MilliSleep(currentKeyDelay())
		}
		return nativeErr
	}
	id, err := keyboardHoldIdentity(k, flags, pid)
	if err != nil {
		return err
	}
	server := selectedDisplayServer()

	if !down {
		hold, ok := keyboardHolds[id]
		if !ok {
			return ErrInputOwnership
		}
		if hold.backend == persistentInputBackendPortal {
			_, err := tryPortalKeyUp(hold)
			delete(keyboardHolds, id)
			if err == nil && applyDelay {
				MilliSleep(currentKeyDelay())
			}
			return err
		}
		_, nativeErr := runNativeKeyboardOperation(hold.server, func() error {
			return nativeToggleKey(k, false, flags, pid)
		})
		if nativeErr == nil || errors.Is(nativeErr, ErrInputOwnership) {
			delete(keyboardHolds, id)
		}
		if nativeErr == nil && applyDelay {
			MilliSleep(currentKeyDelay())
		}
		return nativeErr
	}

	if existing, ok := keyboardHolds[id]; ok {
		if existing.backend != persistentInputBackendPortal ||
			existing.portalGeneration == remoteDesktopInputGeneration() {
			return ErrInputOwnership
		}
		clearPortalKeyboardGenerationLocked(existing.portalGeneration)
	}

	ready, nativeErr := runNativeKeyboardOperation(server, func() error {
		return nativeToggleKey(k, true, flags, pid)
	})
	if nativeErr != nil {
		if !shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			return nativeErr
		}
		if used, hold, err := tryPortalKeyDown(server, k, keyArr, pid); used {
			if err == nil {
				keyboardHolds[id] = hold
			}
			if err == nil && applyDelay {
				MilliSleep(currentKeyDelay())
			}
			return err
		}
		return nativeErr
	}
	keyboardHolds[id] = keyboardHold{
		backend: persistentInputBackendNative,
		server:  server,
	}
	if applyDelay {
		MilliSleep(currentKeyDelay())
	}
	return nil
}

func appendShift(key string, args ...interface{}) (string, []interface{}) {
	if uppercaseSingleRuneKey(key) {
		args = append(args, "shift")
	}

	key = strings.ToLower(key)
	if spec := CurrentSpecialTable(); spec != nil {
		if v, ok := spec[key]; ok {
			key = v
			args = append(args, "shift")
			return key, args
		}
	}
	return key, args
}

func keyToggle(key string, applyDelay bool, args ...interface{}) error {
	key, args = appendShift(key, args...)
	pid, down, keyArr, err := parseKeyArguments(args, true)
	if err != nil {
		return err
	}
	keyArr, err = normalizeKeyModifiers(keyArr)
	if err != nil {
		return err
	}
	if err := validateKeyArgument(key); err != nil {
		return err
	}
	return keyTogglesB(key, down, keyArr, pid, applyDelay)
}
