//go:build cgo

package robotgo

/*
// #include "key/keycode.h"
#include "key/keypress_c.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"
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

// It sends a key press and release to the active application
func tapKeyCode(code C.MMKeyCode, flags C.MMKeyFlags, pid C.uintptr) error {
	return nativeKeyStatusError(C.robotgo_tap_key_code(code, flags, pid), "tap key")
}

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
	case int(C.ROBOTGO_KEY_NOT_APPLIED):
		return fmt.Errorf("%w: %s: native keyboard injection failed before acquiring key state", ErrInputNotApplied, operation)
	default:
		return fmt.Errorf("robotgo: %s: unknown native keyboard status %d", operation, int(status))
	}
}

func waylandKeyboardBackendCompiled() bool {
	return int(C.robotgo_wayland_keyboard_backend_enabled()) != 0
}

func nativeWaylandKeyboardProbe() error {
	if int(C.robotgo_wayland_keyboard_ready()) == 0 {
		return nil
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

func nativeWaylandKeyboardProtocolVersion() uint32 {
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	return uint32(C.robotgo_wayland_keyboard_protocol_version())
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

// toErr it converts a C string to a Go error
func toErr(str *C.char) error {
	gstr := C.GoString(str)
	if gstr == "" {
		return nil
	}
	return errors.New(gstr)
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

// Keep C key codes in this owning translation unit: their width and signedness
// vary by platform. Policy passes only normalized flags and portable requests.
func nativeTapKey(name string, flags uint32, pid int) error {
	key, err := checkKeyCodes(name)
	if err != nil {
		return fmt.Errorf("%w: native keyboard backend cannot map key %q; use TypeStrE or UnicodeTypeE for text", ErrNotSupported, name)
	}
	return tapKeyCode(key, C.MMKeyFlags(flags), C.uintptr(pid))
}

func nativeKeyFlags(modifiers []string) (uint32, error) {
	flags, err := getFlagsFromValue(modifiers)
	return uint32(flags), err
}

func nativeToggleKey(name string, down bool, flags uint32, pid int) error {
	key, err := checkKeyCodes(name)
	if err != nil {
		return fmt.Errorf("%w: native keyboard backend cannot map key %q; use TypeStrE or UnicodeTypeE for text", ErrNotSupported, name)
	}
	return nativeKeyStatusError(C.toggleKeyCode(key, C.bool(down), C.MMKeyFlags(flags), C.uintptr(pid)), "toggle key")
}

func nativeTypeX11Text(text string, delay int) error {
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	return nativeKeyStatusError(C.robotgo_x11_type_text(cText, C.uint64_t(delay)), "type native X11 text")
}
