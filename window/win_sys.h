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

#include "../base/os.h"
#if defined(IS_LINUX)
#include <X11/Xresource.h>
#if defined(ROBOTGO_USE_WAYLAND)
#include "wayland_bounds.h"
#include "wayland_bounds.c"
#endif
#endif

Bounds get_client(uintptr pid, int8_t isPid);

Bounds get_bounds(uintptr pid, int8_t isPid) {
  // Check if the window is valid
  Bounds bounds = {0};
  if (!is_valid()) {
    return bounds;
  }

#if defined(IS_MACOSX)
  // Bounds bounds;
  AXValueRef axp = NULL;
  AXValueRef axs = NULL;
  AXUIElementRef AxID = AXUIElementCreateApplication(pid);
  AXUIElementRef AxWin = NULL;

  // Get the window from the application
  if (AXUIElementCopyAttributeValue(AxID, kAXFocusedWindowAttribute,
                                    (CFTypeRef *)&AxWin) != kAXErrorSuccess ||
      AxWin == NULL) {
    // If no focused window, try to get the main window
    if (AXUIElementCopyAttributeValue(AxID, kAXMainWindowAttribute,
                                      (CFTypeRef *)&AxWin) != kAXErrorSuccess ||
        AxWin == NULL) {
      goto exit;
    }
  }

  // Determine the current point of the window
  if (AXUIElementCopyAttributeValue(AxWin, kAXPositionAttribute,
                                    (CFTypeRef *)&axp) != kAXErrorSuccess ||
      axp == NULL) {
    goto exit;
  }

  // Determine the current size of the window
  if (AXUIElementCopyAttributeValue(AxWin, kAXSizeAttribute,
                                    (CFTypeRef *)&axs) != kAXErrorSuccess ||
      axs == NULL) {
    goto exit;
  }

  CGPoint p;
  CGSize s;
  // Attempt to convert both values into atomic types
  if (AXValueGetValue(axp, kAXValueCGPointType, &p) &&
      AXValueGetValue(axs, kAXValueCGSizeType, &s)) {
    bounds.X = p.x;
    bounds.Y = p.y;
    bounds.W = s.width;
    bounds.H = s.height;
  }

// return bounds;
exit:
  if (axp != NULL) {
    CFRelease(axp);
  }
  if (axs != NULL) {
    CFRelease(axs);
  }
  if (AxWin != NULL) {
    CFRelease(AxWin);
  }
  if (AxID != NULL) {
    CFRelease(AxID);
  }

  return bounds;
#elif defined(IS_LINUX)
#if defined(ROBOTGO_USE_WAYLAND)
  if (detectDisplayServer() == Wayland) {
    return wayland_get_bounds();
  }
#endif
  MData win;
  win.XWin = (Window)pid;

  Bounds client = get_client(pid, isPid);
  Bounds frame = GetFrame(win);

  bounds.X = client.X - frame.X;
  bounds.Y = client.Y - frame.Y;
  bounds.W = client.W + frame.W;
  bounds.H = client.H + frame.H;

  return bounds;
#elif defined(IS_WINDOWS)
  HWND hwnd = getHwnd(pid, isPid);

  RECT rect = {0};
  GetWindowRect(hwnd, &rect);

  bounds.X = rect.left;
  bounds.Y = rect.top;
  bounds.W = rect.right - rect.left;
  bounds.H = rect.bottom - rect.top;

  return bounds;
#endif
}

Bounds get_client(uintptr pid, int8_t isPid) {
  // Check if the window is valid
  Bounds bounds = {0};
  if (!is_valid()) {
    return bounds;
  }

#if defined(IS_MACOSX)
  return get_bounds(pid, isPid);
#elif defined(IS_LINUX)
#if defined(ROBOTGO_USE_WAYLAND)
  if (detectDisplayServer() == Wayland) {
    return wayland_get_bounds();
  }
#endif
  Display *rDisplay = XGetMainDisplay();
  if (rDisplay == NULL) {
    return bounds;
  }

  MData win;
  win.XWin = (Window)pid;

  // Property variables
  Window root = None;
  Window parent = None;
  Window child = None;
  Window *children = NULL;
  unsigned int count = 0;
  int32_t x = 0, y = 0;

  RobotGoXErrorTrap trap;
  if (!robotgo_xerror_trap_begin(rDisplay, &trap)) {
    return bounds;
  }

  // Check if the window is the root
  Status tree_status =
      XQueryTree(rDisplay, win.XWin, &root, &parent, &children, &count);
  if (children) {
    XFree(children);
    children = NULL;
  }
  if (tree_status == 0) {
    (void)robotgo_xerror_trap_end(&trap);
    return bounds;
  }

  // Retrieve window attributes
  XWindowAttributes attr = {0};
  if (XGetWindowAttributes(rDisplay, win.XWin, &attr) == 0 ||
      attr.root == None) {
    (void)robotgo_xerror_trap_end(&trap);
    return bounds;
  }

  // Coordinates must be translated
  if (parent != attr.root) {
	/* Translate the client origin; attr.x/attr.y are already parent-relative. */
    if (XTranslateCoordinates(rDisplay, win.XWin, attr.root, 0, 0,
                              &x, &y, &child) == 0) {
      (void)robotgo_xerror_trap_end(&trap);
      return bounds;
    }
  } else {
    x = attr.x;
    y = attr.y;
  }

  if (!robotgo_xerror_trap_end(&trap)) {
    return bounds;
  }

  // Return resulting window bounds
  bounds.X = x;
  bounds.Y = y;
  bounds.W = attr.width;
  bounds.H = attr.height;

  return bounds;
#elif defined(IS_WINDOWS)
  HWND hwnd = getHwnd(pid, isPid);

  RECT rect = {0};
  GetClientRect(hwnd, &rect);

  POINT point;
  point.x = rect.left;
  point.y = rect.top;

  // Convert the client point to screen
  ClientToScreen(hwnd, &point);

  bounds.X = point.x;
  bounds.Y = point.y;
  bounds.W = rect.right - rect.left;
  bounds.H = rect.bottom - rect.top;

  return bounds;
#endif
}
