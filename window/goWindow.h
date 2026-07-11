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

#include "alert_c.h"
#include "window.h"
#include "win_sys.h"

bool min_window(uintptr pid, bool state, int8_t isPid){
	#if defined(IS_MACOSX)
		AXUIElementRef axID = AXUIElementCreateApplication(pid);
		if (axID == NULL) { return false; }
		AXError result = AXUIElementSetAttributeValue(axID, kAXMinimizedAttribute,
										state ? kCFBooleanTrue : kCFBooleanFalse);
		CFRelease(axID);
		return result == kAXErrorSuccess;
	#elif defined(IS_LINUX)
		(void)pid; (void)state; (void)isPid;
		return false;
	#elif defined(IS_WINDOWS)
        HWND hwnd = getHwnd(pid, isPid);
		if (hwnd == NULL || !IsWindow(hwnd)) { return false; }
		win_min(hwnd, state);
		return true;
	#endif
}

bool max_window(uintptr pid, bool state, int8_t isPid){
	#if defined(IS_MACOSX)
		(void)pid; (void)state; (void)isPid;
		return false;
	#elif defined(IS_LINUX)
		(void)pid; (void)state; (void)isPid;
		return false;
	#elif defined(IS_WINDOWS)
        HWND hwnd = getHwnd(pid, isPid);
		if (hwnd == NULL || !IsWindow(hwnd)) { return false; }
		win_max(hwnd, state);
		return true;
	#endif
}

uintptr get_handle(){
	MData mData = get_active();

	#if defined(IS_MACOSX)
		return (uintptr)mData.CgID;
	#elif defined(IS_LINUX)
		return (uintptr)mData.XWin;
	#elif defined(IS_WINDOWS)
		return (uintptr)mData.HWnd;
	#endif
}

uintptr b_get_handle() {
	#if defined(IS_MACOSX)
		return (uintptr)pub_mData.CgID;
	#elif defined(IS_LINUX)
		return (uintptr)pub_mData.XWin;
	#elif defined(IS_WINDOWS)
		return (uintptr)pub_mData.HWnd;
	#endif
}

void active_PID(uintptr pid, int8_t isPid){
	MData win = set_handle_pid(pid, isPid);
	set_active(win);
}
