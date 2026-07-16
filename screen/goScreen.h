// Copyright 2016 The go-vgo Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0> 
//
// This file may not be copied, modified, or distributed
// except according to those terms.

#include "../base/types.h"
#include "../base/pubs.h"
#include "../base/rgb.h"
#include "screengrab_c.h"
#include <stdio.h>
#include <string.h>

static inline char* robotgo_copy_string(const char* value) {
	size_t length = strlen(value) + 1;
	char* copy = (char*)malloc(length);
	if (copy != NULL) {
		memcpy(copy, value, length);
	}
	return copy;
}

static inline void padHex(MMRGBHex color, char* hex) {
	// Length needs to be 7 because snprintf includes a terminating null.
	snprintf(hex, 7, "%06x", color);
}

static inline char* pad_hex(MMRGBHex color) {
	char hex[7];
	padHex(color, hex);
	// destroyMMBitmap(bitmap);

	char* str = (char*)calloc(100, sizeof(char*));
    if (str) { strcpy(str, hex); }
	return str;
}

static uint8_t rgb[3];

static inline uint8_t* color_hex_to_rgb(uint32_t h) {
	rgb[0] = RED_FROM_HEX(h);
	rgb[1] = GREEN_FROM_HEX(h);
	rgb[2] = BLUE_FROM_HEX(h);
	return rgb;
}

static inline uint32_t color_rgb_to_hex(uint8_t r, uint8_t g, uint8_t b) {
	return RGB_TO_HEX(r, g, b);
}

static inline MMRGBHex get_px_color(int32_t x, int32_t y, int32_t display_id) {
	MMBitmapRef bitmap;
	MMRGBHex color = 0;

	if (!pointVisibleOnMainDisplay(MMPointInt32Make(x, y))) {
		return color;
	}

	bitmap = copyMMBitmapFromDisplayInRect(MMRectInt32Make(x, y, 1, 1), display_id, 0);
	if (bitmap == NULL) {
		return color;
	}
	color = MMRGBHexAtPoint(bitmap, 0, 0);
	destroyMMBitmap(bitmap);

	return color;
}

static inline char* set_XDisplay_name(char* name) {
	#if defined(IS_LINUX) && !defined(DISPLAY_SERVER_WAYLAND)
		if (setXDisplay(name) != 0) {
			return "failed to allocate X11 display name";
		}
		return "";
	#else
		return "SetXDisplayName is only supported on Linux";
	#endif
}

static inline char* get_XDisplay_name() {
	#if defined(IS_LINUX) && !defined(DISPLAY_SERVER_WAYLAND)
		const char* display = getXDisplay();
		return robotgo_copy_string(display == NULL ? "" : display);
	#else
		return robotgo_copy_string("GetXDisplayName is only supported on Linux");
	#endif
}

static inline const char* get_XDisplay_name_borrowed() {
	#if defined(IS_LINUX) && !defined(DISPLAY_SERVER_WAYLAND)
		return getXDisplay();
	#else
		return NULL;
	#endif
}

static inline void close_main_display() {
	#if defined(IS_LINUX) && !defined(DISPLAY_SERVER_WAYLAND)
		XCloseMainDisplay();
	#else
		// 
	#endif
}

static inline uint32_t get_num_displays() {
	#if defined(IS_MACOSX)
		uint32_t count = 0;
		if (CGGetActiveDisplayList(0, nil, &count) == kCGErrorSuccess) {
			return count;
		}
		return 0;
	#elif defined(IS_LINUX)
		return 0;
	#elif defined(IS_WINDOWS)
		uint32_t count = 0;
		if (EnumDisplayMonitors(NULL, NULL, MonitorEnumProc, (LPARAM)&count)) {
			return count;
		}
		return 0;
	#endif
}

static inline uintptr get_hwnd_by_pid(uintptr pid) {
	#if defined(IS_WINDOWS)
		HWND hwnd = GetHwndByPid(pid);
		return (uintptr)hwnd;
	#else
		return 0;
	#endif
}

static inline void bitmap_dealloc(MMBitmapRef bitmap) {
	if (bitmap != NULL) {
		destroyMMBitmap(bitmap);
		bitmap = NULL;
	}
}

// capture_screen capture screen
static inline MMBitmapRef capture_screen(int32_t x, int32_t y, int32_t w, int32_t h, int32_t display_id, int8_t isPid) {
	MMBitmapRef bitmap = copyMMBitmapFromDisplayInRect(MMRectInt32Make(x, y, w, h), display_id, isPid);
	return bitmap;
}
