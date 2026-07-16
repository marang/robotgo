// Copyright 2016 The marang Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

package robotgo

/*
// #include "key/keycode.h"
#include "key/keypress_c.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/marang/robotgo/clipboard"
	inputportal "github.com/marang/robotgo/input/portal"
)

// Defining a bunch of constants.
const (
	// KeyA define key "a"
	KeyA = "a"
	KeyB = "b"
	KeyC = "c"
	KeyD = "d"
	KeyE = "e"
	KeyF = "f"
	KeyG = "g"
	KeyH = "h"
	KeyI = "i"
	KeyJ = "j"
	KeyK = "k"
	KeyL = "l"
	KeyM = "m"
	KeyN = "n"
	KeyO = "o"
	KeyP = "p"
	KeyQ = "q"
	KeyR = "r"
	KeyS = "s"
	KeyT = "t"
	KeyU = "u"
	KeyV = "v"
	KeyW = "w"
	KeyX = "x"
	KeyY = "y"
	KeyZ = "z"
	//
	CapA = "A"
	CapB = "B"
	CapC = "C"
	CapD = "D"
	CapE = "E"
	CapF = "F"
	CapG = "G"
	CapH = "H"
	CapI = "I"
	CapJ = "J"
	CapK = "K"
	CapL = "L"
	CapM = "M"
	CapN = "N"
	CapO = "O"
	CapP = "P"
	CapQ = "Q"
	CapR = "R"
	CapS = "S"
	CapT = "T"
	CapU = "U"
	CapV = "V"
	CapW = "W"
	CapX = "X"
	CapY = "Y"
	CapZ = "Z"
	//
	Key0           = "0"
	Key1           = "1"
	Key2           = "2"
	Key3           = "3"
	Key4           = "4"
	Key5           = "5"
	Key6           = "6"
	Key7           = "7"
	Key8           = "8"
	Key9           = "9"
	KeyGrave       = "`"
	KeyQuote       = "'"
	KeyDoubleQuote = "\""
	KeyQuoter      = KeyDoubleQuote

	// Backspace backspace key string
	Backspace = "backspace"
	Delete    = "delete"
	Enter     = "enter"
	Tab       = "tab"
	Esc       = "esc"
	Escape    = "escape"
	Up        = "up"    // Up arrow key
	Down      = "down"  // Down arrow key
	Right     = "right" // Right arrow key
	Left      = "left"  // Left arrow key
	Home      = "home"
	End       = "end"
	Pageup    = "pageup"
	Pagedown  = "pagedown"

	F1  = "f1"
	F2  = "f2"
	F3  = "f3"
	F4  = "f4"
	F5  = "f5"
	F6  = "f6"
	F7  = "f7"
	F8  = "f8"
	F9  = "f9"
	F10 = "f10"
	F11 = "f11"
	F12 = "f12"
	F13 = "f13"
	F14 = "f14"
	F15 = "f15"
	F16 = "f16"
	F17 = "f17"
	F18 = "f18"
	F19 = "f19"
	F20 = "f20"
	F21 = "f21"
	F22 = "f22"
	F23 = "f23"
	F24 = "f24"

	Cmd  = "cmd"  // is the "win" key for windows
	Lcmd = "lcmd" // left command
	Rcmd = "rcmd" // right command
	// "command"
	Alt     = "alt"
	Lalt    = "lalt" // left alt
	Ralt    = "ralt" // right alt
	Ctrl    = "ctrl"
	Lctrl   = "lctrl" // left ctrl
	Rctrl   = "rctrl" // right ctrl
	Control = "control"
	Shift   = "shift"
	Lshift  = "lshift" // left shift
	Rshift  = "rshift" // right shift
	// "right_shift"
	Capslock    = "capslock"
	Space       = "space"
	Print       = "print"
	Printscreen = "printscreen" // No Mac support
	Insert      = "insert"
	Menu        = "menu" // Windows only

	AudioMute    = "audio_mute"     // Mute the volume
	AudioVolDown = "audio_vol_down" // Lower the volume
	AudioVolUp   = "audio_vol_up"   // Increase the volume
	AudioPlay    = "audio_play"
	AudioStop    = "audio_stop"
	AudioPause   = "audio_pause"
	AudioPrev    = "audio_prev"    // Previous Track
	AudioNext    = "audio_next"    // Next Track
	AudioRewind  = "audio_rewind"  // Linux only
	AudioForward = "audio_forward" // Linux only
	AudioRepeat  = "audio_repeat"  //  Linux only
	AudioRandom  = "audio_random"  //  Linux only

	Num0    = "num0" // numpad 0
	Num1    = "num1"
	Num2    = "num2"
	Num3    = "num3"
	Num4    = "num4"
	Num5    = "num5"
	Num6    = "num6"
	Num7    = "num7"
	Num8    = "num8"
	Num9    = "num9"
	NumLock = "num_lock"

	ScrollLock = "scroll_lock"
	PauseBreak = "pause_break"

	NumDecimal = "num."
	NumPlus    = "num+"
	NumMinus   = "num-"
	NumMul     = "num*"
	NumDiv     = "num/"
	NumClear   = "num_clear"
	NumEnter   = "num_enter"
	NumEqual   = "num_equal"

	LightsMonUp     = "lights_mon_up"     // Turn up monitor brightness			No Windows support
	LightsMonDown   = "lights_mon_down"   // Turn down monitor brightness		No Windows support
	LightsKbdToggle = "lights_kbd_toggle" // Toggle keyboard backlight on/off		No Windows support
	LightsKbdUp     = "lights_kbd_up"     // Turn up keyboard backlight brightness	No Windows support
	LightsKbdDown   = "lights_kbd_down"
)

// keyNames define a map of key names to MMKeyCode
var keyNames = map[string]C.MMKeyCode{
	"backspace": C.K_BACKSPACE,
	"delete":    C.K_DELETE,
	"enter":     C.K_RETURN,
	"tab":       C.K_TAB,
	"esc":       C.K_ESCAPE,
	"escape":    C.K_ESCAPE,
	"up":        C.K_UP,
	"down":      C.K_DOWN,
	"right":     C.K_RIGHT,
	"left":      C.K_LEFT,
	"home":      C.K_HOME,
	"end":       C.K_END,
	"pageup":    C.K_PAGEUP,
	"pagedown":  C.K_PAGEDOWN,
	//
	"f1":  C.K_F1,
	"f2":  C.K_F2,
	"f3":  C.K_F3,
	"f4":  C.K_F4,
	"f5":  C.K_F5,
	"f6":  C.K_F6,
	"f7":  C.K_F7,
	"f8":  C.K_F8,
	"f9":  C.K_F9,
	"f10": C.K_F10,
	"f11": C.K_F11,
	"f12": C.K_F12,
	"f13": C.K_F13,
	"f14": C.K_F14,
	"f15": C.K_F15,
	"f16": C.K_F16,
	"f17": C.K_F17,
	"f18": C.K_F18,
	"f19": C.K_F19,
	"f20": C.K_F20,
	"f21": C.K_F21,
	"f22": C.K_F22,
	"f23": C.K_F23,
	"f24": C.K_F24,
	//
	"cmd":         C.K_META,
	"lcmd":        C.K_LMETA,
	"rcmd":        C.K_RMETA,
	"command":     C.K_META,
	"alt":         C.K_ALT,
	"lalt":        C.K_LALT,
	"ralt":        C.K_RALT,
	"ctrl":        C.K_CONTROL,
	"lctrl":       C.K_LCONTROL,
	"rctrl":       C.K_RCONTROL,
	"control":     C.K_CONTROL,
	"shift":       C.K_SHIFT,
	"lshift":      C.K_LSHIFT,
	"rshift":      C.K_RSHIFT,
	"right_shift": C.K_RSHIFT,
	"capslock":    C.K_CAPSLOCK,
	"space":       C.K_SPACE,
	"print":       C.K_PRINTSCREEN,
	"printscreen": C.K_PRINTSCREEN,
	"insert":      C.K_INSERT,
	"menu":        C.K_MENU,

	"audio_mute":     C.K_AUDIO_VOLUME_MUTE,
	"audio_vol_down": C.K_AUDIO_VOLUME_DOWN,
	"audio_vol_up":   C.K_AUDIO_VOLUME_UP,
	"audio_play":     C.K_AUDIO_PLAY,
	"audio_stop":     C.K_AUDIO_STOP,
	"audio_pause":    C.K_AUDIO_PAUSE,
	"audio_prev":     C.K_AUDIO_PREV,
	"audio_next":     C.K_AUDIO_NEXT,
	"audio_rewind":   C.K_AUDIO_REWIND,
	"audio_forward":  C.K_AUDIO_FORWARD,
	"audio_repeat":   C.K_AUDIO_REPEAT,
	"audio_random":   C.K_AUDIO_RANDOM,

	"num0":     C.K_NUMPAD_0,
	"num1":     C.K_NUMPAD_1,
	"num2":     C.K_NUMPAD_2,
	"num3":     C.K_NUMPAD_3,
	"num4":     C.K_NUMPAD_4,
	"num5":     C.K_NUMPAD_5,
	"num6":     C.K_NUMPAD_6,
	"num7":     C.K_NUMPAD_7,
	"num8":     C.K_NUMPAD_8,
	"num9":     C.K_NUMPAD_9,
	"num_lock": C.K_NUMPAD_LOCK,

	ScrollLock: C.K_SCROLL_LOCK,
	PauseBreak: C.K_PAUSE,

	// todo: removed
	"numpad_0":    C.K_NUMPAD_0,
	"numpad_1":    C.K_NUMPAD_1,
	"numpad_2":    C.K_NUMPAD_2,
	"numpad_3":    C.K_NUMPAD_3,
	"numpad_4":    C.K_NUMPAD_4,
	"numpad_5":    C.K_NUMPAD_5,
	"numpad_6":    C.K_NUMPAD_6,
	"numpad_7":    C.K_NUMPAD_7,
	"numpad_8":    C.K_NUMPAD_8,
	"numpad_9":    C.K_NUMPAD_9,
	"numpad_lock": C.K_NUMPAD_LOCK,

	"num.":      C.K_NUMPAD_DECIMAL,
	"num+":      C.K_NUMPAD_PLUS,
	"num-":      C.K_NUMPAD_MINUS,
	"num*":      C.K_NUMPAD_MUL,
	"num/":      C.K_NUMPAD_DIV,
	"num_clear": C.K_NUMPAD_CLEAR,
	"num_enter": C.K_NUMPAD_ENTER,
	"num_equal": C.K_NUMPAD_EQUAL,

	"lights_mon_up":     C.K_LIGHTS_MON_UP,
	"lights_mon_down":   C.K_LIGHTS_MON_DOWN,
	"lights_kbd_toggle": C.K_LIGHTS_KBD_TOGGLE,
	"lights_kbd_up":     C.K_LIGHTS_KBD_UP,
	"lights_kbd_down":   C.K_LIGHTS_KBD_DOWN,

	// { NULL:              C.K_NOT_A_KEY }
}

// CmdCtrl returns "cmd" on macOS and "ctrl" on other platforms.
func CmdCtrl() string {
	if runtime.GOOS == "darwin" {
		return "cmd"
	}
	return "ctrl"
}

// It sends a key press and release to the active application
func tapKeyCode(code C.MMKeyCode, flags C.MMKeyFlags, pid C.uintptr) error {
	return nativeKeyStatusError(C.robotgo_tap_key_code(code, flags, pid), "tap key")
}

var errInvalidKeyFlag = errors.New("invalid key flag specified")

func nativeKeyStatusError(status C.int, operation string) error {
	switch int(status) {
	case int(C.ROBOTGO_KEY_OK):
		return nil
	case int(C.ROBOTGO_KEY_UNMAPPED):
		return fmt.Errorf("%w: %s: key is absent from the active keymap", ErrNotSupported, operation)
	case int(C.ROBOTGO_KEY_UNSUPPORTED):
		return fmt.Errorf("%w: %s", ErrNotSupported, operation)
	case int(C.ROBOTGO_KEY_NO_DISPLAY):
		return fmt.Errorf("robotgo: %s: X11 display is unavailable", operation)
	case int(C.ROBOTGO_KEY_INJECTION_FAILED):
		return fmt.Errorf("robotgo: %s: native keyboard injection failed", operation)
	case int(C.ROBOTGO_KEY_INVALID):
		return fmt.Errorf("robotgo: %s: invalid key value", operation)
	case int(C.ROBOTGO_KEY_STATE_CONFLICT):
		return fmt.Errorf("robotgo: %s: active modifier or lock state cannot safely produce the requested input", operation)
	case int(C.ROBOTGO_KEY_OWNERSHIP_CONFLICT):
		return fmt.Errorf("%w: %s: key state is owned by another input source or has no matching RobotGo key-down", ErrInputOwnership, operation)
	default:
		return fmt.Errorf("robotgo: %s: unknown native keyboard status %d", operation, int(status))
	}
}

var (
	errWaylandKeyboardUnavailable = errors.New("wayland virtual keyboard unavailable")
	errWaylandKeyboardNotBuilt    = errors.New("wayland session detected but robotgo was built without wayland keyboard backend (build with -tags wayland)")
	errWaylandKeyboardNoDisplay   = errors.New("wayland display connection failed")
	errWaylandKeyboardNoSeat      = errors.New("wayland seat with keyboard capability not found")
	errWaylandKeyboardNoManager   = errors.New("zwp_virtual_keyboard_manager_v1 not available")
	errWaylandKeyboardCreate      = errors.New("failed to create virtual keyboard")
	errWaylandKeyboardXKB         = errors.New("failed to initialize xkb context")
	errWaylandKeyboardKeymap      = errors.New("failed to build xkb keymap")
	errWaylandKeyboardMemfd       = errors.New("failed to setup wayland keymap memfd")
	errWaylandKeyboardKeysym      = errors.New("key symbol not present in wayland keymap")
)

func waylandKeyboardBackendCompiled() bool {
	return int(C.robotgo_wayland_keyboard_backend_enabled()) != 0
}

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
		if int(C.robotgo_wayland_keyboard_ready()) == 0 {
			return nil
		}
	default:
		return fmt.Errorf("%w: no supported display server is selected", ErrNotSupported)
	}

	code := int(C.robotgo_wayland_keyboard_last_error())
	switch code {
	case 1:
		return errWaylandKeyboardNoDisplay
	case 2:
		return errWaylandKeyboardNoSeat
	case 3:
		return errWaylandKeyboardNoManager
	case 4:
		return errWaylandKeyboardCreate
	case 5:
		return errWaylandKeyboardXKB
	case 6:
		return errWaylandKeyboardKeymap
	case 7:
		return errWaylandKeyboardMemfd
	case 8:
		return errWaylandKeyboardKeysym
	default:
		return fmt.Errorf("%w (code=%d)", errWaylandKeyboardUnavailable, code)
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

func closeWaylandKeyboard() { C.robotgo_wayland_keyboard_close() }

func syncWaylandKeyboardForTest() int {
	return int(C.robotgo_wayland_keyboard_sync())
}

// releaseNativeX11KeyboardOwnershipLocked releases RobotGo-owned XTEST keys
// before the shared display connection is closed or retargeted. The caller
// holds linuxKeyboardMu followed by nativeX11DisplayMu.
func releaseNativeX11KeyboardOwnershipLocked() error {
	return nativeKeyStatusError(
		C.robotgo_x11_release_owned_keys(),
		"release owned X11 keys",
	)
}

func checkKeyCodes(k string) (key C.MMKeyCode, err error) {
	if k == "" {
		return C.K_NOT_A_KEY, errInvalidKeyFlag
	}

	if len(k) == 1 {
		val1 := C.CString(k)
		defer C.free(unsafe.Pointer(val1))

		key = C.keyCodeForChar(*val1)
		if key == C.K_NOT_A_KEY {
			err = errInvalidKeyFlag
			return
		}
		return
	}

	if v, ok := keyNames[k]; ok {
		key = v
		if key == C.K_NOT_A_KEY {
			err = errInvalidKeyFlag
			return
		}
		return key, nil
	}
	return C.K_NOT_A_KEY, errInvalidKeyFlag
}

func checkKeyFlags(f string) (C.MMKeyFlags, error) {
	m := map[string]C.MMKeyFlags{
		"alt":         C.MOD_ALT,
		"ralt":        C.MOD_ALT,
		"lalt":        C.MOD_ALT,
		"cmd":         C.MOD_META,
		"command":     C.MOD_META,
		"rcmd":        C.MOD_META,
		"lcmd":        C.MOD_META,
		"ctrl":        C.MOD_CONTROL,
		Control:       C.MOD_CONTROL,
		"rctrl":       C.MOD_CONTROL,
		"lctrl":       C.MOD_CONTROL,
		"shift":       C.MOD_SHIFT,
		"rshift":      C.MOD_SHIFT,
		"lshift":      C.MOD_SHIFT,
		"right_shift": C.MOD_SHIFT,
		"none":        C.MOD_NONE,
	}

	if value, ok := m[strings.ToLower(f)]; ok {
		return value, nil
	}
	return C.MOD_NONE, fmt.Errorf("robotgo: unsupported key modifier %q", f)
}

func getFlagsFromValue(value []string) (flags C.MMKeyFlags, err error) {
	if len(value) <= 0 {
		return flags, nil
	}

	for _, modifier := range value {
		f, flagErr := checkKeyFlags(modifier)
		if flagErr != nil {
			return C.MOD_NONE, flagErr
		}
		flags = (C.MMKeyFlags)(flags | f)
	}

	return flags, nil
}

type keyboardHoldID struct {
	keysym int32
	flags  uint32
	pid    int
}

type keyboardHold struct {
	backend          persistentInputBackend
	server           DisplayServer
	portalGeneration uint64
	portalMain       int32
	portalModifiers  []int32
}

type portalKeyboardRefID struct {
	generation uint64
	keysym     int32
}

var (
	keyboardHolds         = make(map[keyboardHoldID]keyboardHold)
	portalKeyboardKeyRefs = make(map[portalKeyboardRefID]uint)
)

func keyboardHoldIdentity(key string, flags C.MMKeyFlags, pid int) (keyboardHoldID, error) {
	keysym, _, err := portalKeysymsPure(key, nil)
	if err != nil {
		return keyboardHoldID{}, err
	}
	return keyboardHoldID{keysym: keysym, flags: uint32(flags), pid: pid}, nil
}

func clearPortalKeyboardStateLocked() {
	clear(portalKeyboardKeyRefs)
	for id, hold := range keyboardHolds {
		if hold.backend == persistentInputBackendPortal {
			delete(keyboardHolds, id)
		}
	}
}

func clearNativeKeyboardStateLocked(server DisplayServer) {
	for id, hold := range keyboardHolds {
		matches := hold.server == server ||
			server == DisplayServerX11 && hold.server != DisplayServerWayland
		if hold.backend == persistentInputBackendNative && matches {
			delete(keyboardHolds, id)
		}
	}
}

func clearPortalKeyboardGenerationLocked(generation uint64) {
	for id := range portalKeyboardKeyRefs {
		if id.generation == generation {
			delete(portalKeyboardKeyRefs, id)
		}
	}
	for id, hold := range keyboardHolds {
		if hold.backend == persistentInputBackendPortal &&
			hold.portalGeneration == generation {
			delete(keyboardHolds, id)
		}
	}
}

func releasePortalKeyboardRef(
	session remoteDesktopInputSession, generation uint64, keysym int32,
) error {
	id := portalKeyboardRefID{generation: generation, keysym: keysym}
	owners := portalKeyboardKeyRefs[id]
	if owners == 0 {
		return ErrInputOwnership
	}
	if owners > 1 {
		portalKeyboardKeyRefs[id] = owners - 1
		return nil
	}
	if err := remoteDesktopEvent(func(ctx context.Context) error {
		return session.KeyboardKeysym(ctx, keysym, false)
	}); err != nil {
		return err
	}
	delete(portalKeyboardKeyRefs, id)
	return nil
}

func pressPortalKeyboardHold(
	session remoteDesktopInputSession,
	generation uint64,
	mainKey int32,
	modifiers []int32,
) (keyboardHold, error) {
	hold := keyboardHold{
		backend:          persistentInputBackendPortal,
		server:           DisplayServerWayland,
		portalGeneration: generation,
		portalMain:       mainKey,
		portalModifiers:  make([]int32, 0, len(modifiers)),
	}
	mainID := portalKeyboardRefID{generation: generation, keysym: mainKey}
	if portalKeyboardKeyRefs[mainID] != 0 {
		return keyboardHold{}, ErrInputOwnership
	}

	seen := make(map[int32]struct{}, len(modifiers))
	for _, modifier := range modifiers {
		if modifier == mainKey {
			continue
		}
		if _, duplicate := seen[modifier]; duplicate {
			continue
		}
		seen[modifier] = struct{}{}
		id := portalKeyboardRefID{generation: generation, keysym: modifier}
		if portalKeyboardKeyRefs[id] == 0 {
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.KeyboardKeysym(ctx, modifier, true)
			}); err != nil {
				var rollbackErr error
				for index := len(hold.portalModifiers) - 1; index >= 0; index-- {
					rollbackErr = errors.Join(rollbackErr, releasePortalKeyboardRef(
						session, generation, hold.portalModifiers[index],
					))
				}
				return keyboardHold{}, errors.Join(err, rollbackErr)
			}
		}
		portalKeyboardKeyRefs[id]++
		hold.portalModifiers = append(hold.portalModifiers, modifier)
	}

	if err := remoteDesktopEvent(func(ctx context.Context) error {
		return session.KeyboardKeysym(ctx, mainKey, true)
	}); err != nil {
		var rollbackErr error
		for index := len(hold.portalModifiers) - 1; index >= 0; index-- {
			rollbackErr = errors.Join(rollbackErr, releasePortalKeyboardRef(
				session, generation, hold.portalModifiers[index],
			))
		}
		return keyboardHold{}, errors.Join(err, rollbackErr)
	}
	portalKeyboardKeyRefs[mainID] = 1
	return hold, nil
}

func releasePortalKeyboardHold(
	session remoteDesktopInputSession, hold keyboardHold,
) error {
	firstErr := releasePortalKeyboardRef(
		session, hold.portalGeneration, hold.portalMain,
	)
	for index := len(hold.portalModifiers) - 1; index >= 0; index-- {
		firstErr = errors.Join(firstErr, releasePortalKeyboardRef(
			session, hold.portalGeneration, hold.portalModifiers[index],
		))
	}
	return firstErr
}

func portalKeyboardPayload(key string, modifiers []string, pid int) (int32, []int32, error) {
	if pid != 0 {
		return 0, nil, fmt.Errorf("%w: RemoteDesktop portal input cannot target a process", ErrNotSupported)
	}
	return portalKeysymsPure(key, modifiers)
}

func tryPortalKeyTap(
	server DisplayServer, key string, modifiers []string, pid int,
) (bool, error) {
	if runtime.GOOS != "linux" || server != DisplayServerWayland {
		return false, nil
	}
	used, generation, err := withRemoteDesktopInputLease(
		inputportal.DeviceKeyboard, nil,
		func(session remoteDesktopInputSession, generation uint64) error {
			mainKey, portalModifiers, err := portalKeyboardPayload(key, modifiers, pid)
			if err != nil {
				return err
			}
			hold, err := pressPortalKeyboardHold(
				session, generation, mainKey, portalModifiers,
			)
			if err != nil {
				return err
			}
			return releasePortalKeyboardHold(session, hold)
		},
	)
	if used && portalInputFailureInvalidatesSession(err) {
		closeErr := CloseRemoteDesktopInput()
		clearPortalKeyboardGenerationLocked(generation)
		return true, errors.Join(err, closeErr)
	}
	return used, err
}

func tryPortalKeyDown(
	server DisplayServer, key string, modifiers []string, pid int,
) (bool, keyboardHold, error) {
	if runtime.GOOS != "linux" || server != DisplayServerWayland {
		return false, keyboardHold{}, nil
	}
	var hold keyboardHold
	used, generation, err := withRemoteDesktopInputLease(
		inputportal.DeviceKeyboard, nil,
		func(session remoteDesktopInputSession, generation uint64) error {
			mainKey, portalModifiers, err := portalKeyboardPayload(key, modifiers, pid)
			if err != nil {
				return err
			}
			hold, err = pressPortalKeyboardHold(
				session, generation, mainKey, portalModifiers,
			)
			return err
		},
	)
	if used && portalInputFailureInvalidatesSession(err) {
		closeErr := CloseRemoteDesktopInput()
		clearPortalKeyboardGenerationLocked(generation)
		return true, keyboardHold{}, errors.Join(err, closeErr)
	}
	return used, hold, err
}

func tryPortalKeyUp(hold keyboardHold) (bool, error) {
	expected := hold.portalGeneration
	used, currentGeneration, err := withRemoteDesktopInputLease(
		inputportal.DeviceKeyboard, &expected,
		func(session remoteDesktopInputSession, _ uint64) error {
			return releasePortalKeyboardHold(session, hold)
		},
	)
	if errors.Is(err, ErrInputOwnership) || errors.Is(err, inputportal.ErrClosed) || !used {
		clearPortalKeyboardGenerationLocked(hold.portalGeneration)
		return used, errors.Join(ErrInputOwnership, err)
	}
	if err != nil {
		closeErr := CloseRemoteDesktopInput()
		clearPortalKeyboardGenerationLocked(currentGeneration)
		return true, errors.Join(err, closeErr)
	}
	return true, nil
}

func keyTaps(k string, keyArr []string, pid int) error {
	flags, err := getFlagsFromValue(keyArr)
	if err != nil {
		return err
	}
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	server := selectedDisplayServer()
	ready, nativeErr := runNativeKeyboardOperation(server, func() error {
		key, err := checkKeyCodes(k)
		if err != nil {
			return fmt.Errorf("%w: native keyboard backend cannot map key %q; use TypeStrE or UnicodeTypeE for text", ErrNotSupported, k)
		}
		return tapKeyCode(key, flags, C.uintptr(pid))
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

func keyTogglesB(k string, down bool, keyArr []string, pid int) error {
	flags, err := getFlagsFromValue(keyArr)
	if err != nil {
		return err
	}
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	if runtime.GOOS != "linux" {
		key, err := checkKeyCodes(k)
		if err != nil {
			return fmt.Errorf("%w: native keyboard backend cannot map key %q; use TypeStrE or UnicodeTypeE for text", ErrNotSupported, k)
		}
		nativeErr := nativeKeyStatusError(
			C.toggleKeyCode(key, C.bool(down), flags, C.uintptr(pid)),
			"toggle key",
		)
		if nativeErr == nil {
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
			if err == nil {
				MilliSleep(currentKeyDelay())
			}
			return err
		}
		_, nativeErr := runNativeKeyboardOperation(hold.server, func() error {
			key, err := checkKeyCodes(k)
			if err != nil {
				return fmt.Errorf("%w: native keyboard backend cannot map key %q; use TypeStrE or UnicodeTypeE for text", ErrNotSupported, k)
			}
			return nativeKeyStatusError(
				C.toggleKeyCode(key, C.bool(false), flags, C.uintptr(pid)),
				"toggle key",
			)
		})
		if nativeErr == nil || errors.Is(nativeErr, ErrInputOwnership) {
			delete(keyboardHolds, id)
		}
		if nativeErr == nil {
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
		key, err := checkKeyCodes(k)
		if err != nil {
			return fmt.Errorf("%w: native keyboard backend cannot map key %q; use TypeStrE or UnicodeTypeE for text", ErrNotSupported, k)
		}
		return nativeKeyStatusError(
			C.toggleKeyCode(key, C.bool(true), flags, C.uintptr(pid)),
			"toggle key",
		)
	})
	if nativeErr != nil {
		if !shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			return nativeErr
		}
		if used, hold, err := tryPortalKeyDown(server, k, keyArr, pid); used {
			if err == nil {
				keyboardHolds[id] = hold
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
	MilliSleep(currentKeyDelay())
	return nil
}

/*
 __  ___  ___________    ____ .______     ______        ___      .______       _______
|  |/  / |   ____\   \  /   / |   _  \   /  __  \      /   \     |   _  \     |       \
|  '  /  |  |__   \   \/   /  |  |_)  | |  |  |  |    /  ^  \    |  |_)  |    |  .--.  |
|    <   |   __|   \_    _/   |   _  <  |  |  |  |   /  /_\  \   |      /     |  |  |  |
|  .  \  |  |____    |  |     |  |_)  | |  `--'  |  /  _____  \  |  |\  \----.|  '--'  |
|__|\__\ |_______|   |__|     |______/   \______/  /__/     \__\ | _| `._____||_______/

*/

// ToInterfaces convert []string to []interface{}
func ToInterfaces(fields []string) []interface{} {
	res := make([]interface{}, 0, len(fields))
	for _, s := range fields {
		res = append(res, s)
	}
	return res
}

// ToStrings convert []interface{} to []string
func ToStrings(fields []interface{}) []string {
	res, _ := toStringsE(fields)
	return res
}

func toStringsE(fields []interface{}) ([]string, error) {
	res := make([]string, 0, len(fields))
	for _, field := range fields {
		value, ok := field.(string)
		if !ok {
			return nil, fmt.Errorf("robotgo: key modifier must be a string, got %T", field)
		}
		res = append(res, value)
	}
	return res, nil
}

// toErr it converts a C string to a Go error
func toErr(str *C.char) error {
	gstr := C.GoString(str)
	if gstr == "" {
		return nil
	}
	return errors.New(gstr)
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

// KeyTap taps the keyboard code;
//
// See keys supported:
//
//	https://github.com/marang/robotgo/blob/master/docs/keys.md#keys
//
// Examples:
//
//	robotgo.KeySleep = 100 // 100 millisecond
//	robotgo.KeyTap("a")
//	robotgo.KeyTap("i", "alt", "command")
//
//	arr := []string{"alt", "command"}
//	robotgo.KeyTap("i", arr)
//
//	robotgo.KeyTap("k", pid int)
func KeyTap(key string, args ...interface{}) error {
	key, args = appendShift(key, args...)
	pid, _, keyArr, err := parseKeyArguments(args, false)
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
	return keyTaps(key, keyArr, pid)
}

// KeyToggle toggles the keyboard, if there not have args default is "down"
//
// See keys:
//
//	https://github.com/marang/robotgo/blob/master/docs/keys.md#keys
//
// Examples:
//
//	robotgo.KeyToggle("a")
//	robotgo.KeyToggle("a", "up")
//
//	robotgo.KeyToggle("a", "up", "alt", "cmd")
//	robotgo.KeyToggle("k", pid int)
func KeyToggle(key string, args ...interface{}) error {
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
	return keyTogglesB(key, down, keyArr, pid)
}

// KeyPress presses and releases a key as one backend transaction. It is
// equivalent to KeyTap.
func KeyPress(key string, args ...interface{}) error {
	return KeyTap(key, args...)
}

// KeyDown press down a key
func KeyDown(key string, args ...interface{}) error {
	return KeyToggle(key, args...)
}

// KeyUp press up a key
func KeyUp(key string, args ...interface{}) error {
	arr := []interface{}{"up"}
	arr = append(arr, args...)
	return KeyToggle(key, arr...)
}

// ReadAll read string from clipboard
func ReadAll() (string, error) {
	return clipboard.ReadAll()
}

// WriteAll write string to clipboard
func WriteAll(text string) error {
	return clipboard.WriteAll(text)
}

// CharCodeAt char code at utf-8
func CharCodeAt(s string, n int) rune {
	i := 0
	for _, r := range s {
		if i == n {
			return r
		}
		i++
	}

	return 0
}

// UnicodeType tap the uint32 unicode
func UnicodeType(str uint32, args ...int) {
	_ = UnicodeTypeE(str, args...)
}

// UnicodeTypeE types one Unicode code point and reports backend availability
// errors.
func UnicodeTypeE(str uint32, args ...int) error {
	if err := validateUnicodeScalar(str); err != nil {
		return err
	}
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	server := selectedDisplayServer()
	if usesNativeX11KeyboardFor(server) && (str < minNativeX11DirectRune || str > maxNativeX11DirectRune) {
		return fmt.Errorf("%w: native X11 Unicode input cannot safely map %U without changing the server keymap; use a Pure-Go build for full Unicode input", ErrNotSupported, str)
	}
	ready, nativeErr := runNativeKeyboardOperation(server, func() error {
		return unicodeType(str, args...)
	})
	if nativeErr != nil && shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
		used, err := tryRemoteDesktopUnicode(rune(str), args)
		if used {
			return err
		}
	}
	return nativeErr
}

func unicodeType(str uint32, args ...int) error {
	cstr := C.uint(str)
	pid := 0
	if len(args) > 0 {
		pid = args[0]
	}

	isPid := 0
	if len(args) > 1 {
		isPid = args[1]
	}

	return nativeKeyStatusError(C.unicodeType(cstr, C.uintptr(pid), C.int8_t(isPid)), "type Unicode code point")
}

// ToUC trans string to unicode []string
func ToUC(text string) []string {
	var uc []string

	for _, r := range text {
		textQ := strconv.QuoteToASCII(string(r))
		textUnQ := textQ[1 : len(textQ)-1]

		st := strings.ReplaceAll(textUnQ, "\\u", "U")
		if st == "\\\\" {
			st = "\\"
		}
		if st == `\"` {
			st = `"`
		}
		uc = append(uc, st)
	}

	return uc
}

func inputUTF(str string) {
	_ = inputUTFUnsafe(str)
}

func inputUTFUnsafe(str string) error {
	cstr := C.CString(str)
	defer C.free(unsafe.Pointer(cstr))
	return nativeKeyStatusError(C.input_utf(cstr), "type UTF-8 text")
}

func typeWaylandTextExact(str string, delay int) error {
	codepoints := exactTextCodepoints(str)
	values := make([]C.uint32_t, len(codepoints))
	for index, value := range codepoints {
		values[index] = C.uint32_t(value)
	}
	var valuesPtr *C.uint32_t
	if len(values) > 0 {
		valuesPtr = &values[0]
	}
	return nativeKeyStatusError(
		C.robotgo_wayland_type_codepoints(valuesPtr, C.size_t(len(values)), C.uint64_t(delay)),
		"type native Wayland text",
	)
}

// TypeStr send a string (supported UTF-8)
//
// robotgo.TypeStr(string: "The string to send", int: pid, "milli_sleep time", "x11 option")
//
// Examples:
//
//	robotgo.TypeStr("abc@123, Hi galaxy, こんにちは")
//	robotgo.TypeStr("To be or not to be, this is questions.", pid int)
func TypeStr(str string, args ...int) {
	_ = TypeStrE(str, args...)
}

// TypeStrE sends a UTF-8 string and reports backend or key injection errors.
func TypeStrE(str string, args ...int) error {
	pid, tm, err := parseTextInput(str, args)
	if err != nil {
		return err
	}
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	server := selectedDisplayServer()
	if err := validateNativeX11Text(str, server); err != nil {
		return err
	}
	ready, nativeErr := runNativeKeyboardOperation(server, func() error {
		if usesNativeX11KeyboardFor(server) {
			cText := C.CString(str)
			defer C.free(unsafe.Pointer(cText))
			return nativeKeyStatusError(
				C.robotgo_x11_type_text(cText, C.uint64_t(tm)),
				"type native X11 text",
			)
		}
		if runtime.GOOS == "linux" {
			_ = pid // Native Wayland input is global and cannot target a process.
			return typeWaylandTextExact(str, tm)
		}

		for i := 0; i < len([]rune(str)); i++ {
			ustr := uint32(CharCodeAt(str, i))
			if err := unicodeType(ustr, pid); err != nil {
				return err
			}
			MilliSleep(tm)
		}
		MilliSleep(currentKeyDelay())
		return nil
	})
	if nativeErr != nil && shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
		used, err := tryRemoteDesktopText(str, args)
		if used {
			return err
		}
	}
	return nativeErr
}

const (
	minNativeX11DirectRune = 0x20
	maxNativeX11DirectRune = 0x7e
)

func validateNativeX11Text(text string, server DisplayServer) error {
	if !usesNativeX11KeyboardFor(server) {
		return nil
	}
	for _, value := range text {
		encoded := ToUC(string(value))
		if value < minNativeX11DirectRune || value > maxNativeX11DirectRune ||
			len(encoded) != 1 || len([]rune(encoded[0])) != 1 {
			return fmt.Errorf(
				"%w: native X11 text input cannot safely map %U without changing the server keymap; use a Pure-Go build for full Unicode input",
				ErrNotSupported,
				value,
			)
		}
	}
	return nil
}

func usesNativeX11KeyboardFor(server DisplayServer) bool {
	return nativeX11BackendCompiled() && server == DisplayServerX11
}

// PasteStr paste a string (support UTF-8),
// write the string to clipboard and tap `cmd + v`
func PasteStr(str string) error {
	err := clipboard.WriteAll(str)
	if err != nil {
		return err
	}

	if runtime.GOOS == "darwin" {
		return KeyTap("v", "command")
	}

	return KeyTap("v", "control")
}

// TypeStrDelay type string with delayed
// And you can use robotgo.KeySleep = 100 to delayed not this function
func TypeStrDelay(str string, delay int) {
	TypeStr(str)
	MilliSleep(delay)
}

// SetDelay sets the key and mouse delay
// robotgo.SetDelay(100) option the robotgo.KeySleep and robotgo.MouseSleep = d
func SetDelay(d ...int) {
	v := 10
	if len(d) > 0 {
		v = d[0]
	}

	_ = setInputDelays(v, v)
}
