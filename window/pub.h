// Copyright (c) 2016-2025 AtomAI, All rights reserved.
// 
// See the COPYRIGHT file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0> 
//
// This file may not be copied, modified, or distributed
// except according to those terms.

// Prevent multiple inclusion across complex include graph.
#ifndef ROBOTGO_WINDOW_PUB_H_
#define ROBOTGO_WINDOW_PUB_H_

// #include "../base/os.h"
#if defined(IS_MACOSX)
	#include <dlfcn.h>
#elif defined(IS_LINUX)
	#include <X11/Xatom.h>
#endif

struct _MData{
	#if defined(IS_MACOSX)
		CGWindowID		CgID;		// Handle to a CGWindowID
		AXUIElementRef	AxID;		// Handle to a AXUIElementRef
	#elif defined(IS_LINUX)
		Window		XWin;		// Handle to an X11 window
	#elif defined(IS_WINDOWS)
		HWND			HWnd;		// Handle to a window HWND
		TCHAR 	Title[512];
	#endif
};

typedef struct _MData MData;
MData pub_mData;

struct _Bounds {
	int32_t		X;				// Top left X coordinate
	int32_t		Y;				// Top left Y coordinate
	int32_t		W;				// bounds width
	int32_t		H;				// bounds height
};
typedef struct _Bounds Bounds;

#if defined(IS_MACOSX)
	static Boolean(*gAXIsProcessTrustedWithOptions) (CFDictionaryRef);
	static CFStringRef* gkAXTrustedCheckOptionPrompt;

	AXError _AXUIElementGetWindow(AXUIElementRef, CGWindowID* out);
	static AXUIElementRef GetUIElement(CGWindowID win){
		intptr pid = 0;
		// double_t pid = 0;
		// Create array storing window
		CGWindowID window[1] = { win };
		CFArrayRef wlist = CFArrayCreate(NULL, (const void**)window, 1, NULL);

		// Get window info
		CFArrayRef info = CGWindowListCreateDescriptionFromArray(wlist);
		CFRelease(wlist);

		// Check whether the resulting array is populated
		if (info != NULL && CFArrayGetCount(info) > 0) {
			// Retrieve description from info array
			CFDictionaryRef desc = (CFDictionaryRef)CFArrayGetValueAtIndex(info, 0);
			// Get window PID
			CFNumberRef data = (CFNumberRef) CFDictionaryGetValue(desc, kCGWindowOwnerPID);
			if (data != NULL) {
				CFNumberGetValue(data, kCFNumberIntType, &pid);
			}

			// Return result
			CFRelease(info);
		}

		// Check if PID was retrieved
		if (pid <= 0) { return NULL; }

		// Create an accessibility object using retrieved PID
		AXUIElementRef application = AXUIElementCreateApplication(pid);
		if (application == 0) {return NULL;}

		CFArrayRef windows = NULL;
		// Get all windows associated with the app
		AXUIElementCopyAttributeValues(application, kAXWindowsAttribute, 0, 1024, &windows);

		// Reference to resulting value
		AXUIElementRef result = NULL;

		if (windows != NULL) {
			int count = CFArrayGetCount(windows);
			// Loop all windows in the process
			for (CFIndex i = 0; i < count; ++i){
				// Get the element at the index
				AXUIElementRef element = (AXUIElementRef) CFArrayGetValueAtIndex(windows, i);
				CGWindowID temp = 0;
				// Use undocumented API to get WindowID
				_AXUIElementGetWindow(element, &temp);

				if (temp == win) {
					// Retain element
					CFRetain(element);
					result = element;
					break;
				}
			}

			CFRelease(windows);
		}

		CFRelease(application);
		return result;
	}
#elif defined(IS_LINUX)
	// Definitions
	struct Hints{
		unsigned long Flags;
		unsigned long Funcs;
		unsigned long Decorations;
		signed   long Mode;
		unsigned long Stat;
	};

	static Atom WM_STATE	= None;
	static Atom WM_ABOVE	= None;
	static Atom WM_HIDDEN	= None;
	static Atom WM_HMAX		= None;
	static Atom WM_VMAX		= None;

	static Atom WM_DESKTOP	= None;
	static Atom WM_CURDESK	= None;

	static Atom WM_NAME		= None;
	static Atom WM_UTF8		= None;
	static Atom WM_PID		= None;
	static Atom WM_ACTIVE	= None;
	static Atom WM_HINTS	= None;
	static Atom WM_EXTENTS	= None;
	static Display *WM_ATOM_DISPLAY = NULL;
	static unsigned long WM_ATOM_GENERATION = 0;
	static const char ROBOTGO_ATOM_ACTIVE_NAME[] = "_NET_ACTIVE_WINDOW";
	static const char ROBOTGO_ATOM_CURRENT_DESKTOP_NAME[] = "_NET_CURRENT_DESKTOP";

	////////////////////////////////////////////////////////////////////////////////

	static void ResetAtoms(void) {
		WM_STATE = WM_ABOVE = WM_HIDDEN = WM_HMAX = WM_VMAX = None;
		WM_DESKTOP = WM_CURDESK = None;
		WM_NAME = WM_UTF8 = WM_PID = WM_ACTIVE = WM_HINTS = WM_EXTENTS = None;
		WM_ATOM_DISPLAY = NULL;
		WM_ATOM_GENERATION = 0;
	}

	static bool LoadAtoms(Display *display) {
		if (display == NULL) { return false; }
		unsigned long generation = XGetMainDisplayGeneration();
		if (WM_ATOM_DISPLAY == display && WM_ATOM_GENERATION == generation) {
			return true;
		}

		ResetAtoms();
		RobotGoXErrorTrap trap;
		if (!robotgo_xerror_trap_begin(display, &trap)) { return false; }
		WM_STATE   = XInternAtom(display, "_NET_WM_STATE",                True);
		WM_ABOVE   = XInternAtom(display, "_NET_WM_STATE_ABOVE",          True);
		WM_HIDDEN  = XInternAtom(display, "_NET_WM_STATE_HIDDEN",         True);
		WM_HMAX    = XInternAtom(display, "_NET_WM_STATE_MAXIMIZED_HORZ", True);
		WM_VMAX    = XInternAtom(display, "_NET_WM_STATE_MAXIMIZED_VERT", True);

		WM_DESKTOP = XInternAtom(display, "_NET_WM_DESKTOP",              False);
		WM_CURDESK = XInternAtom(display, ROBOTGO_ATOM_CURRENT_DESKTOP_NAME, True);

		WM_NAME    = XInternAtom(display, "_NET_WM_NAME",                 False);
		WM_UTF8    = XInternAtom(display, "UTF8_STRING",                  False);
		WM_PID     = XInternAtom(display, "_NET_WM_PID",                  False);
		WM_ACTIVE  = XInternAtom(display, ROBOTGO_ATOM_ACTIVE_NAME,        True);
		WM_HINTS   = XInternAtom(display, "_MOTIF_WM_HINTS",              False);
		WM_EXTENTS = XInternAtom(display, "_NET_FRAME_EXTENTS",           False);
		if (!robotgo_xerror_trap_end(&trap)) {
			ResetAtoms();
			return false;
		}
		WM_ATOM_DISPLAY = display;
		WM_ATOM_GENERATION = generation;
		return true;
	}

	static bool RefreshOptionalAtom(Display *display, Atom *atom,
									const char *name) {
		if (display == NULL || atom == NULL || name == NULL) { return false; }
		if (*atom != None) { return true; }

		RobotGoXErrorTrap trap;
		if (!robotgo_xerror_trap_begin(display, &trap)) { return false; }
		Atom refreshed = XInternAtom(display, name, True);
		if (!robotgo_xerror_trap_end(&trap)) { return false; }
		*atom = refreshed;
		return true;
	}

	// Functions
	static void* GetWindowPropertyOnDisplay(Display *display, MData win,
						 Atom atom, Atom expected_type,
						 int expected_format,
						 unsigned long minimum_items,
						 unsigned long maximum_items,
						 uint32_t* items) {
		// Property variables
		Atom type = None; int format = 0;
		unsigned long nItems = 0;
		unsigned long bAfter = 0;
		unsigned char* result = NULL;
		if (items != NULL) { *items = 0; }
		if (display == NULL || win.XWin == 0 || atom == None) { return NULL; }

		RobotGoXErrorTrap trap;
		if (!robotgo_xerror_trap_begin(display, &trap)) { return NULL; }
		int status = XGetWindowProperty(display, win.XWin, atom, 0,
			BUFSIZ, False, AnyPropertyType, &type, &format, &nItems, &bAfter,
			&result);
		bool ok = robotgo_xerror_trap_end(&trap) && status == Success &&
			result != NULL && nItems >= minimum_items &&
			(maximum_items == 0 || nItems <= maximum_items) &&
			(expected_type == AnyPropertyType || type == expected_type) &&
			(expected_format == 0 || format == expected_format);
		if (ok) {
			if (items != NULL) { *items = (uint32_t)nItems; }
			return result;
		}
		if (result != NULL) {
			XFree(result);
		}
		return NULL;
	}

	static void* GetWindowProperty(MData win, Atom atom, Atom expected_type,
								 int expected_format,
								 unsigned long minimum_items,
								 unsigned long maximum_items,
								 uint32_t* items) {
		return GetWindowPropertyOnDisplay(
			XGetMainDisplay(), win, atom, expected_type, expected_format,
			minimum_items, maximum_items, items);
	}

	//////
	#define STATE_TOPMOST  0
	#define STATE_MINIMIZE 1
	#define STATE_MAXIMIZE 2

	//////
	static void SetDesktopForWindow(MData win){
		Display *rDisplay = XGetMainDisplay();
		if (rDisplay == NULL) { return; }
		if (!LoadAtoms(rDisplay)) { return; }
		if (!RefreshOptionalAtom(
				rDisplay, &WM_CURDESK,
				ROBOTGO_ATOM_CURRENT_DESKTOP_NAME)) { return; }
		// Validate every atom that we want to use
		if (WM_DESKTOP != None && WM_CURDESK != None) {
			// Get desktop property
			long* desktop = (long*)GetWindowPropertyOnDisplay(
				rDisplay, win, WM_DESKTOP, XA_CARDINAL, 32, 1, 0, NULL);
			// Check result value
			if (desktop != NULL) {
				RobotGoXErrorTrap trap;
				if (!robotgo_xerror_trap_begin(rDisplay, &trap)) {
					XFree(desktop);
					return;
				}
				// Retrieve the screen number
				XWindowAttributes attr = { 0 };
				if (XGetWindowAttributes(rDisplay, win.XWin, &attr) == 0 || attr.screen == NULL) {
					(void)robotgo_xerror_trap_end(&trap);
					XFree(desktop);
					return;
				}
				int s = XScreenNumberOfScreen(attr.screen);
				Window root = XRootWindow(rDisplay, s);

				// Prepare an event
				XClientMessageEvent e = { 0 };
				e.window = root; e.format = 32;
				e.message_type = WM_CURDESK;
				e.display = rDisplay;
				e.type = ClientMessage;
				e.data.l[0] = *desktop;
				e.data.l[1] = CurrentTime;

				// Send the message
				XSendEvent(rDisplay, root, False, SubstructureNotifyMask | SubstructureRedirectMask, 
					(XEvent*) &e);
				(void)robotgo_xerror_trap_end(&trap);
				XFree(desktop);
			}
		}
	}

	static Bounds GetFrame(MData win){
		Bounds frame = {0};
		// Retrieve frame bounds
		if (WM_EXTENTS != None) {
			long* result; uint32_t nItems = 0;
			// Get the window extents property
			result = (long*) GetWindowProperty(
				win, WM_EXTENTS, XA_CARDINAL, 32, 4, 4, &nItems);
			if (result != NULL) {
				if (nItems == 4) {
					frame.X = (int32_t) result[0];
					frame.Y = (int32_t) result[2];
					frame.W = (int32_t) result[0] + (int32_t) result[1];
					frame.H = (int32_t) result[2] + (int32_t) result[3];
				}

				XFree(result);
			}
		}
		return frame;
	}


#elif defined(IS_WINDOWS)
	HWND getHwnd(uintptr pid, int8_t isPid);
	
	void win_min(HWND hwnd, bool state){
        if (state) {
            ShowWindow(hwnd, SW_MINIMIZE);
        } else {
            ShowWindow(hwnd, SW_RESTORE);
        }
    }

    void win_max(HWND hwnd, bool state){
        if (state) {
            ShowWindow(hwnd, SW_MAXIMIZE);
        } else {
            ShowWindow(hwnd, SW_RESTORE);
        }
    }
#endif

#endif /* ROBOTGO_WINDOW_PUB_H_ */
