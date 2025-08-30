#include "keycode.h"
#include <stdlib.h>
#if defined(IS_LINUX) && defined(DISPLAY_SERVER_WAYLAND)
#include <xkbcommon/xkbcommon.h>

/*
 * keysym_to_keycode converts an XKB keysym to a keycode using the
 * supplied keymap. The function iterates over all keycodes in the
 * keymap searching for the first entry that produces the requested
 * keysym. It returns XKB_KEY_NoSymbol when no match is found.
 */
static xkb_keycode_t keysym_to_keycode(struct xkb_keymap *keymap, xkb_keysym_t keysym) {
    if (!keymap) {
        return XKB_KEY_NoSymbol;
    }
    xkb_keycode_t min = xkb_keymap_min_keycode(keymap);
    xkb_keycode_t max = xkb_keymap_max_keycode(keymap);
    for (xkb_keycode_t code = min; code <= max; code++) {
        const xkb_keysym_t *syms = NULL;
        int n = xkb_keymap_key_get_syms_by_level(keymap, code, 0, 0, &syms);
        for (int i = 0; i < n; i++) {
            if (syms[i] == keysym) {
                return code;
            }
        }
    }
    return XKB_KEY_NoSymbol;
}
#endif

#if defined(IS_MACOSX)
	#include <CoreFoundation/CoreFoundation.h>
	#include <Carbon/Carbon.h> /* For kVK_ constants, and TIS functions. */

	/* Returns string representation of key, if it is printable. 
	Ownership follows the Create Rule; 
	it is the caller's responsibility to release the returned object. */
	CFStringRef createStringForKey(CGKeyCode keyCode);
#endif

MMKeyCode keyCodeForChar(const char c) {
	#if defined(IS_MACOSX)
		/* OS X does not appear to have a built-in function for this, 
		so instead it to write our own. */
		static CFMutableDictionaryRef charToCodeDict = NULL;
		CGKeyCode code;
		UniChar character = c;
		CFStringRef charStr = NULL;

		/* Generate table of keycodes and characters. */
		if (charToCodeDict == NULL) {
			size_t i;
			charToCodeDict = CFDictionaryCreateMutable(kCFAllocatorDefault, 128,
				&kCFCopyStringDictionaryKeyCallBacks, NULL);
			if (charToCodeDict == NULL) { return K_NOT_A_KEY; }

			/* Loop through every keycode (0 - 127) to find its current mapping. */
			for (i = 0; i < 128; ++i) {
				CFStringRef string = createStringForKey((CGKeyCode)i);
				if (string != NULL) {
					CFDictionaryAddValue(charToCodeDict, string, (const void *)i);
					CFRelease(string);
				}
			}
		}

		charStr = CFStringCreateWithCharacters(kCFAllocatorDefault, &character, 1);
		/* Our values may be NULL (0), so we need to use this function. */
		if (!CFDictionaryGetValueIfPresent(charToCodeDict, charStr, (const void **)&code)) {
			code = UINT16_MAX; /* Error */
		}
		CFRelease(charStr);

		// TISGetInputSourceProperty may return nil so we need fallback
		if (code == UINT16_MAX) {
			return K_NOT_A_KEY;
		}

		return (MMKeyCode)code;
	#elif defined(IS_WINDOWS)
		MMKeyCode code;
		code = VkKeyScan(c);
		if (code == 0xFFFF) {
			return K_NOT_A_KEY;
		}

		return code;
        #elif defined(IS_LINUX)
                const char* wayland = getenv("WAYLAND_DISPLAY");
                const char* x11 = getenv("DISPLAY");
#if defined(DISPLAY_SERVER_WAYLAND)
                if (wayland && (!x11 || *x11 == '\0')) {
                        char buf[2];
                        buf[0] = c;
                        buf[1] = '\0';

                        MMKeyCode code = xkb_utf8_to_keysym(buf);
                        if (code == XKB_KEY_NoSymbol) {
                                struct XSpecialCharacterMapping* xs = XSpecialCharacterTable;
                                while (xs->name) {
                                        if (c == xs->name) {
                                                code = xs->code;
                                                break;
                                        }
                                        xs++;
                                }
                        }

                        if (code == XKB_KEY_NoSymbol) {
                                return K_NOT_A_KEY;
                        }
                        return code;
                }
#endif
                {
                        char buf[2];
                        buf[0] = c;
                        buf[1] = '\0';

                        MMKeyCode code = XStringToKeysym(buf);
                        if (code == NoSymbol) {
                                struct XSpecialCharacterMapping* xs = XSpecialCharacterTable;
                                while (xs->name) {
                                        if (c == xs->name) {
                                                code = xs->code;
                                                break;
                                        }
                                        xs++;
                                }
                        }

                        if (code == NoSymbol) {
                                return K_NOT_A_KEY;
                        }

                        if (c == 60) {
                                code = 44;
                        }
                        return code;
                }
        #endif
}

#if defined(IS_MACOSX)
	CFStringRef createStringForKey(CGKeyCode keyCode){
		// TISInputSourceRef currentKeyboard = TISCopyCurrentASCIICapableKeyboardInputSource();
		TISInputSourceRef currentKeyboard = TISCopyCurrentKeyboardLayoutInputSource();
		CFDataRef layoutData = (CFDataRef) TISGetInputSourceProperty(
			currentKeyboard, kTISPropertyUnicodeKeyLayoutData);

		if (layoutData == nil) { return 0; }

		const UCKeyboardLayout *keyboardLayout = (const UCKeyboardLayout *) CFDataGetBytePtr(layoutData);
		UInt32 keysDown = 0;
		UniChar chars[4];
		UniCharCount realLength;

		UCKeyTranslate(keyboardLayout, keyCode, kUCKeyActionDisplay, 0, LMGetKbdType(),
					kUCKeyTranslateNoDeadKeysBit, &keysDown,
					sizeof(chars) / sizeof(chars[0]), &realLength, chars);
		CFRelease(currentKeyboard);

		return CFStringCreateWithCharacters(kCFAllocatorDefault, chars, 1);
	}
#endif
