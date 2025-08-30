// Copyright 2016 The go-vgo Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0> 
//
// This file may not be copied, modified, or distributed
// except according to those terms.

#include "../base/deadbeef_rand_c.h"
#include "../base/microsleep.h"
#include "keypress.h"
#include "keycode_c.h"

#include <ctype.h> /* For isupper() */
#if defined(IS_MACOSX)
	#include <ApplicationServices/ApplicationServices.h>
	#import <IOKit/hidsystem/IOHIDLib.h>
	#import <IOKit/hidsystem/ev_keymap.h>
#elif defined(IS_LINUX)
        #include <X11/extensions/XTest.h>
        #include <stdlib.h>
        // #include "../base/xdisplay_c.h"
#if defined(DISPLAY_SERVER_WAYLAND) || defined(USE_WAYLAND)
        #include <wayland-client.h>
        #include <xkbcommon/xkbcommon.h>
#endif
#endif

/* Convenience wrappers around ugly APIs. */
#if defined(IS_WINDOWS)
	HWND GetHwndByPid(DWORD dwProcessId);

	HWND getHwnd(uintptr pid, int8_t isPid) { 
		HWND hwnd = (HWND) pid;
		if (isPid == 0) { 
			hwnd = GetHwndByPid(pid);
		}
		return hwnd;	
	}

	void WIN32_KEY_EVENT_WAIT(MMKeyCode key, DWORD flags, uintptr pid) {
		win32KeyEvent(key, flags, pid, 0); 
		Sleep(DEADBEEF_RANDRANGE(0, 1));
	}
#elif defined(IS_LINUX)
        Display *XGetMainDisplay(void);

        void X_KEY_EVENT(Display *display, MMKeyCode key, bool is_press) {
                XTestFakeKeyEvent(display, XKeysymToKeycode(display, key), is_press, CurrentTime);
                XSync(display, false);
        }

        void X_KEY_EVENT_WAIT(Display *display, MMKeyCode key, bool is_press) {
                X_KEY_EVENT(display, key, is_press);
                microsleep(DEADBEEF_UNIFORM(0.0, 0.5));
        }

#if defined(DISPLAY_SERVER_WAYLAND) || defined(USE_WAYLAND)
        #include <string.h>
        static struct wl_display *wk_display = NULL;
        static struct wl_registry *wk_registry = NULL;
        static struct wl_seat *wk_seat = NULL;
        static struct wl_keyboard *wk_keyboard = NULL;

        static void wk_registry_global(void *data, struct wl_registry *registry,
                                       uint32_t name, const char *interface,
                                       uint32_t version) {
                (void)data; (void)version;
                if (strcmp(interface, "wl_seat") == 0) {
                        wk_seat = wl_registry_bind(registry, name, &wl_seat_interface, 1);
                }
        }

        static void wk_registry_remove(void *data, struct wl_registry *registry,
                                       uint32_t name) {
                (void)data; (void)registry; (void)name;
        }

        static const struct wl_registry_listener wk_registry_listener = {
                wk_registry_global,
                wk_registry_remove
        };

        static void wk_seat_capabilities(void *data, struct wl_seat *seat,
                                         enum wl_seat_capability caps) {
                (void)data;
                if ((caps & WL_SEAT_CAPABILITY_KEYBOARD) && !wk_keyboard) {
                        wk_keyboard = wl_seat_get_keyboard(seat);
                }
        }

        static void wk_seat_name(void *data, struct wl_seat *seat, const char *name) {
                (void)data; (void)seat; (void)name;
        }

        static const struct wl_seat_listener wk_seat_listener = {
                wk_seat_capabilities,
                wk_seat_name
        };

        static int ensure_wayland_keyboard(void) {
                if (wk_keyboard) {
                        return 0;
                }
                wk_display = wl_display_connect(NULL);
                if (!wk_display) {
                        return -1;
                }
                wk_registry = wl_display_get_registry(wk_display);
                wl_registry_add_listener(wk_registry, &wk_registry_listener, NULL);
                wl_display_roundtrip(wk_display);
                if (wk_seat) {
                        wl_seat_add_listener(wk_seat, &wk_seat_listener, NULL);
                        wl_display_roundtrip(wk_display);
                }
                return wk_keyboard ? 0 : -1;
        }

        static void WL_KEY_EVENT(MMKeyCode key, bool is_press) {
                (void)key;
                (void)is_press;
                if (ensure_wayland_keyboard() != 0) {
                        return;
                }
                /* Real key injection requires the virtual-keyboard protocol. */
        }

        static void WL_KEY_EVENT_WAIT(MMKeyCode key, bool is_press) {
                WL_KEY_EVENT(key, is_press);
                microsleep(DEADBEEF_UNIFORM(0.0, 0.5));
        }
#endif

#endif

#if defined(IS_MACOSX)
	int SendTo(uintptr pid, CGEventRef event) {
		if (pid != 0) {
			CGEventPostToPid(pid, event);
		} else {
			CGEventPost(kCGHIDEventTap, event);
		}
		
		CFRelease(event);
		return 0;
	}

	static io_connect_t _getAuxiliaryKeyDriver(void) {
		static mach_port_t sEventDrvrRef = 0;
		mach_port_t masterPort, service, iter;
		kern_return_t kr;

		if (!sEventDrvrRef) {
			kr = IOMasterPort(bootstrap_port, &masterPort);
			assert(KERN_SUCCESS == kr);
			kr = IOServiceGetMatchingServices(masterPort, IOServiceMatching(kIOHIDSystemClass), &iter);
			assert(KERN_SUCCESS == kr);

			service = IOIteratorNext(iter);
			assert(service);

			kr = IOServiceOpen(service, mach_task_self(), kIOHIDParamConnectType, &sEventDrvrRef);
			assert(KERN_SUCCESS == kr);

			IOObjectRelease(service);
			IOObjectRelease(iter);
		}
		return sEventDrvrRef;
	}
#elif defined(IS_WINDOWS)

	void win32KeyEvent(int key, MMKeyFlags flags, uintptr pid, int8_t isPid) {
		int scan = MapVirtualKey(key & 0xff, MAPVK_VK_TO_VSC);

		/* Set the scan code for extended keys */
		switch (key){
			case VK_RCONTROL:
			case VK_SNAPSHOT: /* Print Screen */
			case VK_RMENU: /* Right Alt / Alt Gr */
			case VK_PAUSE: /* Pause / Break */
			case VK_HOME:
			case VK_UP:
			case VK_PRIOR: /* Page up */
			case VK_LEFT:
			case VK_RIGHT:
			case VK_END:
			case VK_DOWN:
			case VK_NEXT: /* 'Page Down' */
			case VK_INSERT:
			case VK_DELETE:
			case VK_LWIN:
			case VK_RWIN:
			case VK_APPS: /* Application */
			case VK_VOLUME_MUTE:
			case VK_VOLUME_DOWN:
			case VK_VOLUME_UP:
			case VK_MEDIA_NEXT_TRACK:
			case VK_MEDIA_PREV_TRACK:
			case VK_MEDIA_STOP:
			case VK_MEDIA_PLAY_PAUSE:
			case VK_BROWSER_BACK:
			case VK_BROWSER_FORWARD:
			case VK_BROWSER_REFRESH:
			case VK_BROWSER_STOP:
			case VK_BROWSER_SEARCH:
			case VK_BROWSER_FAVORITES:
			case VK_BROWSER_HOME:
			case VK_LAUNCH_MAIL:
			{
				flags |= KEYEVENTF_EXTENDEDKEY;
				break;
			}
		}

		// todo: test this
		if (pid != 0) {
			HWND hwnd = getHwnd(pid, isPid);

			int down = (flags == 0 ? WM_KEYDOWN : WM_KEYUP);
			// SendMessage(hwnd, down, key, 0);
			PostMessageW(hwnd, down, key, 0);
			return;
		}

		/* Set the scan code for keyup */
		// if ( flags & KEYEVENTF_KEYUP ) {
		// 	scan |= 0x80;
		// }
		// keybd_event(key, scan, flags, 0);
		
		INPUT keyInput;

		keyInput.type = INPUT_KEYBOARD;
		keyInput.ki.wVk = key;
		keyInput.ki.wScan = scan;
		keyInput.ki.dwFlags = flags;
		keyInput.ki.time = 0;
		keyInput.ki.dwExtraInfo = 0;
		SendInput(1, &keyInput, sizeof(keyInput));
	}
#endif

void toggleKeyCode(MMKeyCode code, const bool down, MMKeyFlags flags, uintptr pid) {
#if defined(IS_MACOSX)
	/* The media keys all have 1000 added to them to help us detect them. */
	if (code >= 1000) {
		code = code - 1000; /* Get the real keycode. */
		NXEventData event;
		kern_return_t kr;

		IOGPoint loc = { 0, 0 };
		UInt32 evtInfo = code << 16 | (down?NX_KEYDOWN:NX_KEYUP) << 8;

		bzero(&event, sizeof(NXEventData));
		event.compound.subType = NX_SUBTYPE_AUX_CONTROL_BUTTONS;
		event.compound.misc.L[0] = evtInfo;

		kr = IOHIDPostEvent(_getAuxiliaryKeyDriver(), 
								NX_SYSDEFINED, loc, &event, kNXEventDataVersion, 0, FALSE);
		assert(KERN_SUCCESS == kr);
	} else {
		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef keyEvent = CGEventCreateKeyboardEvent(source, (CGKeyCode)code, down);
		assert(keyEvent != NULL);

		CGEventSetType(keyEvent, down ? kCGEventKeyDown : kCGEventKeyUp);
		if (flags != 0) {
			CGEventSetFlags(keyEvent, (CGEventFlags) flags);
		}
		
		SendTo(pid, keyEvent);
		CFRelease(source);
	}
#elif defined(IS_WINDOWS)
	const DWORD dwFlags = down ? 0 : KEYEVENTF_KEYUP;

	/* Parse modifier keys. */
	if (flags & MOD_META) { WIN32_KEY_EVENT_WAIT(K_META, dwFlags, pid); }
	if (flags & MOD_ALT) { WIN32_KEY_EVENT_WAIT(K_ALT, dwFlags, pid); }
	if (flags & MOD_CONTROL) { WIN32_KEY_EVENT_WAIT(K_CONTROL, dwFlags, pid); }
	if (flags & MOD_SHIFT) { WIN32_KEY_EVENT_WAIT(K_SHIFT, dwFlags, pid); }

	win32KeyEvent(code, dwFlags, pid, 0);
#elif defined(IS_LINUX)
        const char* wayland = getenv("WAYLAND_DISPLAY");
        const char* x11 = getenv("DISPLAY");
#if defined(DISPLAY_SERVER_WAYLAND) || defined(USE_WAYLAND)
        if (wayland && (!x11 || *x11 == '\0')) {
                const Bool is_press = down ? True : False;

                if (flags & MOD_META) { WL_KEY_EVENT_WAIT(K_META, is_press); }
                if (flags & MOD_ALT) { WL_KEY_EVENT_WAIT(K_ALT, is_press); }
                if (flags & MOD_CONTROL) { WL_KEY_EVENT_WAIT(K_CONTROL, is_press); }
                if (flags & MOD_SHIFT) { WL_KEY_EVENT_WAIT(K_SHIFT, is_press); }

                WL_KEY_EVENT(code, is_press);
        } else
#endif
        {
                Display *display = XGetMainDisplay();
                const Bool is_press = down ? True : False; /* Just to be safe. */

                if (flags & MOD_META) { X_KEY_EVENT_WAIT(display, K_META, is_press); }
                if (flags & MOD_ALT) { X_KEY_EVENT_WAIT(display, K_ALT, is_press); }
                if (flags & MOD_CONTROL) { X_KEY_EVENT_WAIT(display, K_CONTROL, is_press); }
                if (flags & MOD_SHIFT) { X_KEY_EVENT_WAIT(display, K_SHIFT, is_press); }

                X_KEY_EVENT(display, code, is_press);
        }
#endif
}

// void tapKeyCode(MMKeyCode code, MMKeyFlags flags){
// 	toggleKeyCode(code, true, flags);
// 	microsleep(5.0);
// 	toggleKeyCode(code, false, flags);
// }

#if defined(IS_LINUX)
	bool toUpper(char c) {
		if (isupper(c)) {
			return true;
		}

		char *special = "~!@#$%^&*()_+{}|:\"<>?";
		while (*special) {
			if (*special == c) {
				return true;
			}
			special++;
		}
		return false;
	}
#endif

void toggleKey(char c, const bool down, MMKeyFlags flags, uintptr pid) {
	MMKeyCode keyCode = keyCodeForChar(c);

        #if defined(IS_LINUX)
                if (toUpper(c) && !(flags & MOD_SHIFT)) {
                        flags |= MOD_SHIFT; /* Not sure if this is safe for all layouts. */
                }
        #else
                if (isupper(c) && !(flags & MOD_SHIFT)) {
                        flags |= MOD_SHIFT; /* Not sure if this is safe for all layouts. */
                }
        #endif

	#if defined(IS_WINDOWS)
		int modifiers = keyCode >> 8; // Pull out modifers.

		if ((modifiers & 1) != 0) { flags |= MOD_SHIFT; } // Uptdate flags from keycode modifiers.
		if ((modifiers & 2) != 0) { flags |= MOD_CONTROL; }
		if ((modifiers & 4) != 0) { flags |= MOD_ALT; }
		keyCode = keyCode & 0xff; // Mask out modifiers.
	#endif

	toggleKeyCode(keyCode, down, flags, pid);
}

// void tapKey(char c, MMKeyFlags flags){
// 	toggleKey(c, true, flags);
// 	microsleep(5.0);
// 	toggleKey(c, false, flags);
// }

#if defined(IS_MACOSX)
	void toggleUnicode(UniChar ch, const bool down, uintptr pid) {
		/* This function relies on the convenient CGEventKeyboardSetUnicodeString(), 
		convert characters to a keycode, but does not support adding modifier flags. 
		It is only used in typeString().
		-- if you need modifier keys, use the above functions instead. */
		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef keyEvent = CGEventCreateKeyboardEvent(source, 0, down);
		if (keyEvent == NULL) {
			fputs("Could not create keyboard event.\n", stderr);
			return;
		}

		CGEventKeyboardSetUnicodeString(keyEvent, 1, &ch);

		SendTo(pid, keyEvent);
		CFRelease(source);
	}
#else
	#define toggleUniKey(c, down) toggleKey(c, down, MOD_NONE, 0)
#endif

// unicode type
void unicodeType(const unsigned value, uintptr pid, int8_t isPid) {
	#if defined(IS_MACOSX)
		UniChar ch = (UniChar)value; // Convert to unsigned char

		toggleUnicode(ch, true, pid);
		microsleep(5.0);
		toggleUnicode(ch, false, pid);
	#elif defined(IS_WINDOWS)
		if (pid != 0) {
			HWND hwnd = getHwnd(pid, isPid);

			// SendMessage(hwnd, down, value, 0);
			PostMessageW(hwnd, WM_CHAR, value, 0);
			return;
		}

		INPUT input[2];
        memset(input, 0, sizeof(input));

        input[0].type = INPUT_KEYBOARD;
  		input[0].ki.wVk = 0;
  		input[0].ki.wScan = value;
  		input[0].ki.dwFlags = 0x4; // KEYEVENTF_UNICODE;

  		input[1].type = INPUT_KEYBOARD;
  		input[1].ki.wVk = 0;
  		input[1].ki.wScan = value;
  		input[1].ki.dwFlags = KEYEVENTF_KEYUP | 0x4; // KEYEVENTF_UNICODE;

  		SendInput(2, input, sizeof(INPUT));
        #elif defined(IS_LINUX)
                toggleUniKey(value, true);
                microsleep(5.0);
                toggleUniKey(value, false);
        #endif
}

#if defined(IS_LINUX)
	int input_utf(const char *utf) {
		Display *dpy = XOpenDisplay(NULL);
		KeySym sym = XStringToKeysym(utf);
		// KeySym sym = XKeycodeToKeysym(dpy, utf);

		int min, max, numcodes;
		XDisplayKeycodes(dpy, &min, &max);
		KeySym *keysym;
		keysym = XGetKeyboardMapping(dpy, min, max-min+1, &numcodes);
		keysym[(max-min-1)*numcodes] = sym;
		XChangeKeyboardMapping(dpy, min, numcodes, keysym, (max-min));
		XFree(keysym);
		XFlush(dpy);

		KeyCode code = XKeysymToKeycode(dpy, sym);
		XTestFakeKeyEvent(dpy, code, True, 1);
		XTestFakeKeyEvent(dpy, code, False, 1);

		XFlush(dpy);
		XCloseDisplay(dpy);
		return 0;
        }

        // Wayland input_utf not implemented
#else
        int input_utf(const char *utf){
                return 0;
        }
#endif