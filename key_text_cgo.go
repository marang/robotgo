//go:build cgo

package robotgo

import (
	"fmt"
	"runtime"
)

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

func typeStrE(str string, applyDelay bool, args ...int) error {
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
			return nativeTypeX11Text(str, tm)
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
		if applyDelay {
			MilliSleep(currentKeyDelay())
		}
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
