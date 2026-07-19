#include "mouse.h"
#include "../base/deadbeef_rand.h"
#include "../base/microsleep.h"

#include <math.h> /* For floor() */
#include <string.h>

enum RobotGoMouseStatus {
	ROBOTGO_MOUSE_OK = 0,
	ROBOTGO_MOUSE_NO_DISPLAY = 1,
	ROBOTGO_MOUSE_UNSUPPORTED = 2,
	ROBOTGO_MOUSE_INJECTION_FAILED = 3,
	ROBOTGO_MOUSE_OWNERSHIP_CONFLICT = 4,
	ROBOTGO_MOUSE_INVALID = 5
};
#if defined(IS_MACOSX)
	// #include </System/Library/Frameworks/ApplicationServices.framework/Headers/ApplicationServices.h>
	#include <ApplicationServices/ApplicationServices.h>
	// #include </System/Library/Frameworks/ApplicationServices.framework/Versions/A/Headers/ApplicationServices.h>
#elif defined(IS_LINUX)
#if !defined(DISPLAY_SERVER_WAYLAND)
        #include <X11/Xlib.h>
        #include <X11/extensions/XTest.h>
	Display *XGetMainDisplay(void);
	unsigned long XGetMainDisplayGeneration(void);

	enum { ROBOTGO_X11_MOUSE_BUTTON_COUNT = 8 };
	static bool rg_x11_owned_buttons[ROBOTGO_X11_MOUSE_BUTTON_COUNT];
	static unsigned long rg_x11_mouse_generation = 0;

	static void rg_x11_sync_mouse_generation(void) {
		unsigned long generation = XGetMainDisplayGeneration();
		if (rg_x11_mouse_generation != generation) {
			memset(rg_x11_owned_buttons, 0, sizeof(rg_x11_owned_buttons));
			rg_x11_mouse_generation = generation;
		}
	}

	static unsigned int rg_x11_button_mask(MMMouseButton button) {
		switch (button) {
		case LEFT_BUTTON: return Button1Mask;
		case CENTER_BUTTON: return Button2Mask;
		case RIGHT_BUTTON: return Button3Mask;
		case WheelDown: return Button4Mask;
		case WheelUp: return Button5Mask;
		default: return 0;
		}
	}

	static int rg_x11_toggle_mouse(Display *display, bool down,
	                               MMMouseButton button) {
		if (display == NULL) {
			return ROBOTGO_MOUSE_NO_DISPLAY;
		}
		if (button == 0 || button >= ROBOTGO_X11_MOUSE_BUTTON_COUNT) {
			return ROBOTGO_MOUSE_INVALID;
		}
		rg_x11_sync_mouse_generation();
		unsigned int expected_mask = rg_x11_button_mask(button);
		if (expected_mask == 0) {
			/* XQueryPointer exposes ownership only for core buttons 1-5.
			 * Stateful horizontal-wheel toggles therefore cannot be made
			 * ownership-safe. Stateless clicks use a separate path below. */
			return ROBOTGO_MOUSE_UNSUPPORTED;
		}
		if (down) {
			if (rg_x11_owned_buttons[button]) {
				return ROBOTGO_MOUSE_OWNERSHIP_CONFLICT;
			}
			Window root = 0, child = 0;
			int root_x = 0, root_y = 0, win_x = 0, win_y = 0;
			unsigned int state = 0;
			if (!XQueryPointer(display, DefaultRootWindow(display),
			                   &root, &child, &root_x, &root_y,
			                   &win_x, &win_y, &state)) {
				return ROBOTGO_MOUSE_INJECTION_FAILED;
			}
			if ((state & expected_mask) != 0) {
				return ROBOTGO_MOUSE_OWNERSHIP_CONFLICT;
			}
		} else if (!rg_x11_owned_buttons[button]) {
			return ROBOTGO_MOUSE_OWNERSHIP_CONFLICT;
		}
		if (!XTestFakeButtonEvent(display, button,
		                              down ? True : False, CurrentTime)) {
			return ROBOTGO_MOUSE_INJECTION_FAILED;
		}
		XSync(display, false);
		rg_x11_owned_buttons[button] = down;
		return ROBOTGO_MOUSE_OK;
	}

	static int robotgo_x11_release_owned_buttons(void) {
		rg_x11_sync_mouse_generation();
		bool has_owned_buttons = false;
		for (MMMouseButton button = 1;
		     button < ROBOTGO_X11_MOUSE_BUTTON_COUNT; button++) {
			if (rg_x11_owned_buttons[button]) {
				has_owned_buttons = true;
				break;
			}
		}
		if (!has_owned_buttons) {
			rg_x11_mouse_generation = XGetMainDisplayGeneration();
			return ROBOTGO_MOUSE_OK;
		}
		Display *display = XGetMainDisplay();
		if (display == NULL) {
			memset(rg_x11_owned_buttons, 0, sizeof(rg_x11_owned_buttons));
			rg_x11_mouse_generation = XGetMainDisplayGeneration();
			return ROBOTGO_MOUSE_NO_DISPLAY;
		}
		int first_error = ROBOTGO_MOUSE_OK;
		for (MMMouseButton button = 1;
		     button < ROBOTGO_X11_MOUSE_BUTTON_COUNT; button++) {
			if (!rg_x11_owned_buttons[button]) {
				continue;
			}
			int status = rg_x11_toggle_mouse(display, false, button);
			if (first_error == ROBOTGO_MOUSE_OK &&
			    status != ROBOTGO_MOUSE_OK) {
				first_error = status;
			}
		}
		return first_error;
	}

	static int rg_x11_click_unobservable_button(Display *display,
	                                            MMMouseButton button) {
		if (display == NULL) {
			return ROBOTGO_MOUSE_NO_DISPLAY;
		}
		if (button != WheelLeft && button != WheelRight) {
			return ROBOTGO_MOUSE_INVALID;
		}
		if (!XTestFakeButtonEvent(display, button, True, CurrentTime) ||
		    !XTestFakeButtonEvent(display, button, False, CurrentTime)) {
			XSync(display, false);
			return ROBOTGO_MOUSE_INJECTION_FAILED;
		}
		XSync(display, false);
		return ROBOTGO_MOUSE_OK;
	}
#endif
        #include <stdlib.h>
        #include "../base/os.h"

#ifdef ROBOTGO_USE_WAYLAND
        /* Wayland support */
        #include <string.h>
        #include <wayland-client.h>
        #include <linux/input-event-codes.h>
        #include "wlr-virtual-pointer-unstable-v1-client-protocol.h"
        #include "../window/get_bounds_wayland.h"

	        static struct wl_display *rg_wl_display = NULL;
	        static struct wl_registry *rg_wl_registry = NULL;
        static struct wl_seat *rg_wl_seat = NULL;
        static struct zwlr_virtual_pointer_manager_v1 *rg_wl_vptr_mgr = NULL;
        static struct zwlr_virtual_pointer_v1 *rg_wl_vptr = NULL;
        static int rg_wl_width = 0;
        static int rg_wl_height = 0;
        static int rg_wl_inited = 0;
        static int rg_wl_last_x = 0;
        static int rg_wl_last_y = 0;
	static bool rg_wl_owned_buttons[3];
        #include <wayland-client-protocol.h>

	static bool robotgo_wayland_mouse_backend_selected(void) {
#if defined(DISPLAY_SERVER_WAYLAND)
		/* Go selected this build's only native input backend before entering
		 * C. Keep one transaction independent of concurrent environment
		 * changes. */
		return true;
#else
		return detectDisplayServer() == Wayland;
#endif
	}

	static int robotgo_wayland_mouse_button_code(
		MMMouseButton button, uint32_t *code, unsigned int *index) {
		if (code == NULL || index == NULL) {
			return ROBOTGO_MOUSE_INVALID;
		}
		*code = 0;
		*index = 0;
		switch (button) {
		case LEFT_BUTTON:
			*code = BTN_LEFT;
			*index = 0;
			return ROBOTGO_MOUSE_OK;
		case RIGHT_BUTTON:
			*code = BTN_RIGHT;
			*index = 1;
			return ROBOTGO_MOUSE_OK;
		case CENTER_BUTTON:
			*code = BTN_MIDDLE;
			*index = 2;
			return ROBOTGO_MOUSE_OK;
		default:
			return ROBOTGO_MOUSE_UNSUPPORTED;
		}
	}

        static void rg_wl_seat_handle_capabilities(void *data, struct wl_seat *seat, uint32_t caps) {
                (void)data;
                (void)seat;
                (void)caps;
        }

        static const struct wl_seat_listener rg_wl_seat_listener = {
                rg_wl_seat_handle_capabilities,
                NULL
        };

        static void rg_wl_registry_handle_global(void *data, struct wl_registry *registry, uint32_t name, const char *interface, uint32_t version) {
                (void)data; (void)version;
                if (strcmp(interface, "wl_seat") == 0) {
                        rg_wl_seat = wl_registry_bind(registry, name, &wl_seat_interface, 1);
                        wl_seat_add_listener(rg_wl_seat, &rg_wl_seat_listener, NULL);
                } else if (strcmp(interface, "zwlr_virtual_pointer_manager_v1") == 0) {
                        rg_wl_vptr_mgr = wl_registry_bind(registry, name, &zwlr_virtual_pointer_manager_v1_interface, 1);
                }
        }

	        static const struct wl_registry_listener rg_wl_registry_listener = {
                rg_wl_registry_handle_global,
                NULL
	        };

	        static void rg_cleanup_wayland(void) {
	                if (rg_wl_vptr) {
	                        zwlr_virtual_pointer_v1_destroy(rg_wl_vptr);
	                        rg_wl_vptr = NULL;
	                }
	                if (rg_wl_vptr_mgr) {
	                        zwlr_virtual_pointer_manager_v1_destroy(rg_wl_vptr_mgr);
	                        rg_wl_vptr_mgr = NULL;
	                }
	                if (rg_wl_seat) {
	                        wl_seat_destroy(rg_wl_seat);
	                        rg_wl_seat = NULL;
	                }
	                if (rg_wl_registry) {
	                        wl_registry_destroy(rg_wl_registry);
	                        rg_wl_registry = NULL;
	                }
	                if (rg_wl_display) {
	                        wl_display_disconnect(rg_wl_display);
	                        rg_wl_display = NULL;
	                }
	                rg_wl_width = 0;
	                rg_wl_height = 0;
	                rg_wl_inited = 0;
	                memset(rg_wl_owned_buttons, 0,
	                       sizeof(rg_wl_owned_buttons));
	        }

	        static int rg_init_wayland(void) {
	                if (rg_wl_inited && rg_wl_display && rg_wl_vptr) {
	                        return 1;
	                }
	                rg_cleanup_wayland();
	                rg_wl_display = wl_display_connect(NULL);
	                if (!rg_wl_display) {
	                        return 0;
	                }
	                rg_wl_registry = wl_display_get_registry(rg_wl_display);
	                if (!rg_wl_registry) {
	                        rg_cleanup_wayland();
	                        return 0;
	                }
	                wl_registry_add_listener(rg_wl_registry, &rg_wl_registry_listener, NULL);
	                if (wl_display_roundtrip(rg_wl_display) < 0) {
	                        rg_cleanup_wayland();
	                        return 0;
	                }
	                if (!rg_wl_seat || !rg_wl_vptr_mgr) {
	                        rg_cleanup_wayland();
	                        return 0;
	                }
	                rg_wl_vptr = zwlr_virtual_pointer_manager_v1_create_virtual_pointer(rg_wl_vptr_mgr, rg_wl_seat);
	                if (!rg_wl_vptr) {
	                        rg_cleanup_wayland();
	                        return 0;
	                }
	                get_bounds_wayland(rg_wl_display, &rg_wl_width, &rg_wl_height);
	                rg_wl_inited = 1;
	                return 1;
	        }

	        static int robotgo_wayland_mouse_backend_enabled(void) { return 1; }
	        static int robotgo_wayland_mouse_ready(void) {
	                if (!robotgo_wayland_mouse_backend_selected()) {
	                        return 0;
	                }
	                return rg_init_wayland();
	        }
	        static uint32_t robotgo_wayland_mouse_protocol_version(void) {
	                return rg_wl_vptr_mgr == NULL
	                        ? 0
	                        : zwlr_virtual_pointer_manager_v1_get_version(rg_wl_vptr_mgr);
	        }
	        static void robotgo_wayland_mouse_close(void) { rg_cleanup_wayland(); }
#endif /* ROBOTGO_USE_WAYLAND */
	#ifndef ROBOTGO_USE_WAYLAND
	        static bool robotgo_wayland_mouse_backend_selected(void) { return false; }
	        static int robotgo_wayland_mouse_backend_enabled(void) { return 0; }
	        static int robotgo_wayland_mouse_ready(void) { return 0; }
	        static uint32_t robotgo_wayland_mouse_protocol_version(void) { return 0; }
	        static void robotgo_wayland_mouse_close(void) { }
	        static int robotgo_wayland_mouse_button_code(
	                MMMouseButton button, uint32_t *code, unsigned int *index) {
	                (void)button;
	                if (code != NULL) { *code = 0; }
	                if (index != NULL) { *index = 0; }
	                return ROBOTGO_MOUSE_UNSUPPORTED;
	        }
	#endif
	#if defined(DISPLAY_SERVER_WAYLAND)
	        static int robotgo_x11_release_owned_buttons(void) {
	                return ROBOTGO_MOUSE_OK;
	        }
	#endif
#endif

#if !defined(IS_LINUX)
static bool robotgo_wayland_mouse_backend_selected(void) { return false; }
static int robotgo_wayland_mouse_backend_enabled(void) { return 0; }
static int robotgo_wayland_mouse_ready(void) { return 0; }
static uint32_t robotgo_wayland_mouse_protocol_version(void) { return 0; }
static void robotgo_wayland_mouse_close(void) { }
static int robotgo_wayland_mouse_button_code(
	MMMouseButton button, uint32_t *code, unsigned int *index) {
	(void)button;
	if (code != NULL) { *code = 0; }
	if (index != NULL) { *index = 0; }
	return ROBOTGO_MOUSE_UNSUPPORTED;
}
static int robotgo_x11_release_owned_buttons(void) { return ROBOTGO_MOUSE_OK; }
#endif

/* Some convenience macros for converting our enums to the system API types. */
#if defined(IS_MACOSX)
	CGEventType MMMouseDownToCGEventType(MMMouseButton button) {
		if (button == LEFT_BUTTON) {
			return kCGEventLeftMouseDown;
		}
		if (button == RIGHT_BUTTON) { 
			return kCGEventRightMouseDown;
		}
		return kCGEventOtherMouseDown;
	}

	CGEventType MMMouseUpToCGEventType(MMMouseButton button) {
		if (button == LEFT_BUTTON) { return kCGEventLeftMouseUp; }
		if (button == RIGHT_BUTTON) { return kCGEventRightMouseUp; }		
		return kCGEventOtherMouseUp;
	}

	CGEventType MMMouseDragToCGEventType(MMMouseButton button) {
		if (button == LEFT_BUTTON) { return kCGEventLeftMouseDragged; }
		if (button == RIGHT_BUTTON) { return kCGEventRightMouseDragged; }
		return kCGEventOtherMouseDragged;
	}

	CGEventType MMMouseToCGEventType(bool down, MMMouseButton button) {
		if (down) { return MMMouseDownToCGEventType(button); }
		return MMMouseUpToCGEventType(button);
	}

#elif defined(IS_WINDOWS)
 
	DWORD MMMouseUpToMEventF(MMMouseButton button) {
		if (button == LEFT_BUTTON) { return MOUSEEVENTF_LEFTUP; }
		if (button == RIGHT_BUTTON) { return MOUSEEVENTF_RIGHTUP; } 
		return MOUSEEVENTF_MIDDLEUP;
	}

	DWORD MMMouseDownToMEventF(MMMouseButton button) {
		if (button == LEFT_BUTTON) { return MOUSEEVENTF_LEFTDOWN; }
		if (button == RIGHT_BUTTON) { return MOUSEEVENTF_RIGHTDOWN; } 
		return MOUSEEVENTF_MIDDLEDOWN;
	}

	DWORD MMMouseToMEventF(bool down, MMMouseButton button) {
		if (down) { return MMMouseDownToMEventF(button); }
		return MMMouseUpToMEventF(button);
	}
#endif

#if defined(IS_MACOSX)
	/* Calculate the delta for a mouse move and add them to the event. */
	void calculateDeltas(CGEventRef *event, MMPointInt32 point) {
		/* The next few lines are a workaround for games not detecting mouse moves. */
		CGEventRef get = CGEventCreate(NULL);
		CGPoint mouse = CGEventGetLocation(get);

		// Calculate the deltas.
		int64_t deltaX = point.x - mouse.x;
		int64_t deltaY = point.y - mouse.y;

		CGEventSetIntegerValueField(*event, kCGMouseEventDeltaX, deltaX);
		CGEventSetIntegerValueField(*event, kCGMouseEventDeltaY, deltaY);

		CFRelease(get);
	}
#endif

/* Move the mouse to a specific point. */
void moveMouse(MMPointInt32 point){
        #if defined(IS_MACOSX)
		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef move = CGEventCreateMouseEvent(source, kCGEventMouseMoved, 
								CGPointFromMMPointInt32(point), kCGMouseButtonLeft);

		calculateDeltas(&move, point);

		CGEventPost(kCGHIDEventTap, move);
		CFRelease(move);
		CFRelease(source);
        #elif defined(IS_LINUX)
#ifdef ROBOTGO_USE_WAYLAND
                if (robotgo_wayland_mouse_backend_selected()) {
                        if (!rg_init_wayland()) {
#if !defined(DISPLAY_SERVER_WAYLAND)
                                Display *display = XGetMainDisplay();
                                if (display == NULL) { return; }
                                XWarpPointer(display, None, DefaultRootWindow(display), 0, 0, 0, 0, point.x, point.y);
                                XSync(display, false);
#endif
                        } else {
                                if (rg_wl_width > 0 && rg_wl_height > 0) {
                                        uint32_t sx = (uint32_t)((double)point.x * 65535.0 / rg_wl_width);
                                        uint32_t sy = (uint32_t)((double)point.y * 65535.0 / rg_wl_height);
                                        zwlr_virtual_pointer_v1_motion_absolute(rg_wl_vptr, 0, sx, sy, 65535, 65535);
                                        wl_display_flush(rg_wl_display);
                                        rg_wl_last_x = point.x;
                                        rg_wl_last_y = point.y;
                                }
                        }
                }
#if !defined(DISPLAY_SERVER_WAYLAND)
                else {
                        Display *display = XGetMainDisplay();
                        if (display == NULL) { return; }
                        XWarpPointer(display, None, DefaultRootWindow(display), 0, 0, 0, 0, point.x, point.y);
                        XSync(display, false);
                }
#endif
#else
                Display *display = XGetMainDisplay();
                if (display == NULL) { return; }
                XWarpPointer(display, None, DefaultRootWindow(display), 0, 0, 0, 0, point.x, point.y);
                XSync(display, false);
#endif
        #elif defined(IS_WINDOWS)
                SetCursorPos(point.x, point.y);
        #endif
}

void dragMouse(MMPointInt32 point, const MMMouseButton button){
	#if defined(IS_MACOSX)
		const CGEventType dragType = MMMouseDragToCGEventType(button);
		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef drag = CGEventCreateMouseEvent(source, dragType, 
								CGPointFromMMPointInt32(point), (CGMouseButton)button);

		calculateDeltas(&drag, point);

		CGEventPost(kCGHIDEventTap, drag);
		CFRelease(drag);
		CFRelease(source);
	#else
		moveMouse(point);
	#endif
}

MMPointInt32 location() {
	#if defined(IS_MACOSX)
		CGEventRef event = CGEventCreate(NULL);
		CGPoint point = CGEventGetLocation(event);
		CFRelease(event);

		return MMPointInt32FromCGPoint(point);
	#elif defined(IS_LINUX)
		#ifdef ROBOTGO_USE_WAYLAND
		if (robotgo_wayland_mouse_backend_selected()) {
			return MMPointInt32Make(rg_wl_last_x, rg_wl_last_y);
		}
		#endif
#if !defined(DISPLAY_SERVER_WAYLAND)
		int x, y; 	/* This is all we care about. Seriously. */
		Window garb1, garb2; 	/* Why you can't specify NULL as a parameter */
		int garb_x, garb_y;  	/* is beyond me. */
		unsigned int more_garbage;

		Display *display = XGetMainDisplay();
		if (display == NULL) { return MMPointInt32Make(0, 0); }
		XQueryPointer(display, XDefaultRootWindow(display), &garb1, &garb2, &x, &y, 
						&garb_x, &garb_y, &more_garbage);

		return MMPointInt32Make(x, y);
#else
		return MMPointInt32Make(rg_wl_last_x, rg_wl_last_y);
#endif
	#elif defined(IS_WINDOWS)
		POINT point;
		GetCursorPos(&point);
		return MMPointInt32FromPOINT(point);
	#endif
}

/* Move the mouse by a relative delta. */
void moveMouseRelative(int dx, int dy) {
#if defined(IS_MACOSX)
        MMPointInt32 pos = location();
        pos.x += dx;
        pos.y += dy;
        moveMouse(pos);
#elif defined(IS_LINUX)
#ifdef ROBOTGO_USE_WAYLAND
        if (robotgo_wayland_mouse_backend_selected()) {
                if (rg_init_wayland()) {
                        wl_fixed_t fdx = wl_fixed_from_double((double)dx);
                        wl_fixed_t fdy = wl_fixed_from_double((double)dy);
                        zwlr_virtual_pointer_v1_motion(rg_wl_vptr, 0, fdx, fdy);
                        zwlr_virtual_pointer_v1_frame(rg_wl_vptr);
                        wl_display_flush(rg_wl_display);
                        rg_wl_last_x += dx;
                        rg_wl_last_y += dy;
                        return;
                }
        }
#endif
        MMPointInt32 pos = location();
        pos.x += dx;
        pos.y += dy;
        moveMouse(pos);
#elif defined(IS_WINDOWS)
        MMPointInt32 pos = location();
        pos.x += dx;
        pos.y += dy;
        moveMouse(pos);
#endif
}

/* Press down a button, or release it. */
int toggleMouse(bool down, MMMouseButton button) {
	#if defined(IS_MACOSX)
		const CGPoint currentPos = CGPointFromMMPointInt32(location());
		const CGEventType mouseType = MMMouseToCGEventType(down, button);
		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef event = CGEventCreateMouseEvent(source, mouseType, currentPos, (CGMouseButton)button);

		CGEventPost(kCGHIDEventTap, event);
		CFRelease(event);
		CFRelease(source);
		return ROBOTGO_MOUSE_OK;
        #elif defined(IS_LINUX)
#ifdef ROBOTGO_USE_WAYLAND
                if (robotgo_wayland_mouse_backend_selected()) {
                        if (!rg_init_wayland()) {
#if !defined(DISPLAY_SERVER_WAYLAND)
                                Display *display = XGetMainDisplay();
				return rg_x11_toggle_mouse(display, down, button);
#else
				return ROBOTGO_MOUSE_NO_DISPLAY;
#endif
                        } else {
				uint32_t code = 0;
				unsigned int index = 0;
				int mapping_status = robotgo_wayland_mouse_button_code(
					button, &code, &index);
				if (mapping_status != ROBOTGO_MOUSE_OK) {
					return mapping_status;
				}
				if (down == rg_wl_owned_buttons[index]) {
					return ROBOTGO_MOUSE_OWNERSHIP_CONFLICT;
				}
                                zwlr_virtual_pointer_v1_button(rg_wl_vptr, 0, code, down ? 1 : 0);
				zwlr_virtual_pointer_v1_frame(rg_wl_vptr);
				if (wl_display_flush(rg_wl_display) < 0) {
					rg_cleanup_wayland();
					return ROBOTGO_MOUSE_INJECTION_FAILED;
				}
				rg_wl_owned_buttons[index] = down;
				return ROBOTGO_MOUSE_OK;
                        }
                }
#if !defined(DISPLAY_SERVER_WAYLAND)
                else {
                        Display *display = XGetMainDisplay();
			return rg_x11_toggle_mouse(display, down, button);
                }
#else
		return ROBOTGO_MOUSE_UNSUPPORTED;
#endif
#else
                Display *display = XGetMainDisplay();
		return rg_x11_toggle_mouse(display, down, button);
#endif
        #elif defined(IS_WINDOWS)
                // mouse_event(MMMouseToMEventF(down, button), 0, 0, 0, 0);
                INPUT mouseInput;

		mouseInput.type = INPUT_MOUSE;
		mouseInput.mi.dx = 0;
		mouseInput.mi.dy = 0;
		mouseInput.mi.dwFlags = MMMouseToMEventF(down, button);
		mouseInput.mi.time = 0;
		mouseInput.mi.dwExtraInfo = 0;
		mouseInput.mi.mouseData = 0;
		return SendInput(1, &mouseInput, sizeof(mouseInput)) == 1
			? ROBOTGO_MOUSE_OK : ROBOTGO_MOUSE_INJECTION_FAILED;
	#endif
	return ROBOTGO_MOUSE_UNSUPPORTED;
}

int clickMouse(MMMouseButton button){
#if defined(IS_LINUX) && !defined(DISPLAY_SERVER_WAYLAND)
	if (button == WheelLeft || button == WheelRight) {
		return rg_x11_click_unobservable_button(XGetMainDisplay(), button);
	}
#endif
	int status = toggleMouse(true, button);
	if (status != ROBOTGO_MOUSE_OK) {
		return status;
	}
	microsleep(5.0);
	return toggleMouse(false, button);
}

/* Special function for sending double clicks, needed for MacOS. */
int doubleClick(MMMouseButton button){
	#if defined(IS_MACOSX)
		/* Double click for Mac. */
		const CGPoint currentPos = CGPointFromMMPointInt32(location());
		const CGEventType mouseTypeDown = MMMouseToCGEventType(true, button);
		const CGEventType mouseTypeUP = MMMouseToCGEventType(false, button);

		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef event = CGEventCreateMouseEvent(source, mouseTypeDown, currentPos, kCGMouseButtonLeft);

		/* Set event to double click. */
		CGEventSetIntegerValueField(event, kCGMouseEventClickState, 2);
		CGEventPost(kCGHIDEventTap, event);

		CGEventSetType(event, mouseTypeUP);
		CGEventPost(kCGHIDEventTap, event);

		CFRelease(event);
		CFRelease(source);
		return ROBOTGO_MOUSE_OK;
	#else
		/* Double click for everything else. */
		int status = clickMouse(button);
		if (status != ROBOTGO_MOUSE_OK) {
			return status;
		}
		microsleep(200);
		return clickMouse(button);
	#endif
}

/* Function used to scroll the screen in the required direction. */
void scrollMouseXY(int x, int y) {
	#if defined(IS_WINDOWS)
		// Fix for #97, C89 needs variables declared on top of functions (mouseScrollInput)
		INPUT mouseScrollInputH;
		INPUT mouseScrollInputV;
	#endif

	#if defined(IS_MACOSX)
		CGEventSourceRef source = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
		CGEventRef event = CGEventCreateScrollWheelEvent(source, kCGScrollEventUnitPixel, 2, y, x);
		CGEventPost(kCGHIDEventTap, event);

		CFRelease(event);
		CFRelease(source);
	#elif defined(IS_LINUX)
		#ifdef ROBOTGO_USE_WAYLAND
		if (robotgo_wayland_mouse_backend_selected()) {
			if (rg_init_wayland()) {
				/* Emulate wheel scrolling using virtual pointer axis events */
				if (y != 0) {
					int steps = abs(y);
					int sign = (y > 0) ? 1 : -1; /* positive y = scroll up */
					for (int i = 0; i < steps; i++) {
						zwlr_virtual_pointer_v1_axis_source(rg_wl_vptr, WL_POINTER_AXIS_SOURCE_WHEEL);
						/* Use a fixed value per discrete step */
						wl_fixed_t val = wl_fixed_from_double(-15.0 * sign);
						zwlr_virtual_pointer_v1_axis(rg_wl_vptr, 0, WL_POINTER_AXIS_VERTICAL_SCROLL, val);
						zwlr_virtual_pointer_v1_axis_discrete(rg_wl_vptr, 0, WL_POINTER_AXIS_VERTICAL_SCROLL, val, -1 * sign);
						zwlr_virtual_pointer_v1_frame(rg_wl_vptr);
					}
				}
				if (x != 0) {
					int steps = abs(x);
					int sign = (x > 0) ? 1 : -1; /* positive x = scroll right */
					for (int i = 0; i < steps; i++) {
						zwlr_virtual_pointer_v1_axis_source(rg_wl_vptr, WL_POINTER_AXIS_SOURCE_WHEEL);
						wl_fixed_t val = wl_fixed_from_double(15.0 * sign);
						zwlr_virtual_pointer_v1_axis(rg_wl_vptr, 0, WL_POINTER_AXIS_HORIZONTAL_SCROLL, val);
						zwlr_virtual_pointer_v1_axis_discrete(rg_wl_vptr, 0, WL_POINTER_AXIS_HORIZONTAL_SCROLL, val, 1 * sign);
						zwlr_virtual_pointer_v1_frame(rg_wl_vptr);
					}
				}
				wl_display_flush(rg_wl_display);
			}
#if !defined(DISPLAY_SERVER_WAYLAND)
			else {
				Display *display = XGetMainDisplay();
				if (display == NULL) { return; }
				int ydir = 4; /* Button 4 is up, 5 is down. */
				int xdir = 6;
				if (y < 0) { ydir = 5; }
				if (x < 0) { xdir = 7; }
				for (int xi = 0; xi < abs(x); xi++) {
					XTestFakeButtonEvent(display, xdir, 1, CurrentTime);
					XTestFakeButtonEvent(display, xdir, 0, CurrentTime);
				}
				for (int yi = 0; yi < abs(y); yi++) {
					XTestFakeButtonEvent(display, ydir, 1, CurrentTime);
					XTestFakeButtonEvent(display, ydir, 0, CurrentTime);
				}
				XSync(display, false);
			}
#endif
		}
#if !defined(DISPLAY_SERVER_WAYLAND)
		else {
			Display *display = XGetMainDisplay();
			if (display == NULL) { return; }
			int ydir = 4; /* Button 4 is up, 5 is down. */
			int xdir = 6;
			if (y < 0) { ydir = 5; }
			if (x < 0) { xdir = 7; }
			for (int xi = 0; xi < abs(x); xi++) {
				XTestFakeButtonEvent(display, xdir, 1, CurrentTime);
				XTestFakeButtonEvent(display, xdir, 0, CurrentTime);
			}
			for (int yi = 0; yi < abs(y); yi++) {
				XTestFakeButtonEvent(display, ydir, 1, CurrentTime);
				XTestFakeButtonEvent(display, ydir, 0, CurrentTime);
			}
			XSync(display, false);
		}
#endif
		#else
		Display *display = XGetMainDisplay();
		if (display == NULL) { return; }
		int ydir = 4; /* Button 4 is up, 5 is down. */
		int xdir = 6;
		if (y < 0) { ydir = 5; }
		if (x < 0) { xdir = 7; }
		for (int xi = 0; xi < abs(x); xi++) {
			XTestFakeButtonEvent(display, xdir, 1, CurrentTime);
			XTestFakeButtonEvent(display, xdir, 0, CurrentTime);
		}
		for (int yi = 0; yi < abs(y); yi++) {
			XTestFakeButtonEvent(display, ydir, 1, CurrentTime);
			XTestFakeButtonEvent(display, ydir, 0, CurrentTime);
		}
		XSync(display, false);
		#endif
	#elif defined(IS_WINDOWS)
		mouseScrollInputH.type = INPUT_MOUSE;
		mouseScrollInputH.mi.dx = 0;
		mouseScrollInputH.mi.dy = 0;
		mouseScrollInputH.mi.dwFlags = MOUSEEVENTF_WHEEL;
		mouseScrollInputH.mi.time = 0;
		mouseScrollInputH.mi.dwExtraInfo = 0;
		mouseScrollInputH.mi.mouseData = WHEEL_DELTA * x;

		mouseScrollInputV.type = INPUT_MOUSE;
		mouseScrollInputV.mi.dx = 0;
		mouseScrollInputV.mi.dy = 0;
		mouseScrollInputV.mi.dwFlags = MOUSEEVENTF_WHEEL;
		mouseScrollInputV.mi.time = 0;
		mouseScrollInputV.mi.dwExtraInfo = 0;
		mouseScrollInputV.mi.mouseData = WHEEL_DELTA * y;

		SendInput(1, &mouseScrollInputH, sizeof(mouseScrollInputH));
		SendInput(1, &mouseScrollInputV, sizeof(mouseScrollInputV));
	#endif
}

/* A crude, fast hypot() approximation to get around the fact that hypot() is not a standard ANSI C function. */
#if !defined(M_SQRT2)
	#define M_SQRT2 1.4142135623730950488016887 /* Fix for MSVC. */
#endif

static double crude_hypot(double x, double y){
	double big = fabs(x); /* max(|x|, |y|) */
	double small = fabs(y); /* min(|x|, |y|) */

	if (big > small) {
		double temp = big;
		big = small;
		small = temp;
	}

	return ((M_SQRT2 - 1.0) * small) + big;
}

bool smoothlyMoveMouse(MMPointInt32 endPoint, double lowSpeed, double highSpeed){
	MMPointInt32 pos = location();
	// MMSizeInt32 screenSize = getMainDisplaySize();
	double velo_x = 0.0, velo_y = 0.0;
	double distance;

	while ((distance =crude_hypot((double)pos.x - endPoint.x, (double)pos.y - endPoint.y)) > 1.0) {
		double gravity = DEADBEEF_UNIFORM(5.0, 500.0);
		// double gravity = DEADBEEF_UNIFORM(lowSpeed, highSpeed);
		double veloDistance;
		velo_x += (gravity * ((double)endPoint.x - pos.x)) / distance;
		velo_y += (gravity * ((double)endPoint.y - pos.y)) / distance;

		/* Normalize velocity to get a unit vector of length 1. */
		veloDistance = crude_hypot(velo_x, velo_y);
		velo_x /= veloDistance;
		velo_y /= veloDistance;

		pos.x += floor(velo_x + 0.5);
		pos.y += floor(velo_y + 0.5);

		/* Make sure we are in the screen boundaries! (Strange things will happen if we are not.) */
		// if (pos.x >= screenSize.w || pos.y >= screenSize.h) {
		// 	return false;
		// }
		moveMouse(pos);

		/* Wait 1 - 3 milliseconds. */
		microsleep(DEADBEEF_UNIFORM(lowSpeed, highSpeed));
		// microsleep(DEADBEEF_UNIFORM(1.0, 3.0));
	}

	/* The smoothing loop intentionally stops near the destination. Finish at
	 * the exact public-API endpoint without duplicating an exact final event. */
	if (pos.x != endPoint.x || pos.y != endPoint.y) {
		moveMouse(endPoint);
	}
	return true;
}
