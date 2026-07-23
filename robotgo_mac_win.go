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

//go:build darwin || windows
// +build darwin windows

package robotgo

/*
#include "screen/goScreen.h"
#include "window/alert_c.h"
*/
import "C"

import "unsafe"

// GetBounds get the window bounds
func GetBounds(pid int, args ...int) (int, int, int, int) {
	x, y, width, height, _ := GetBoundsE(pid, args...)
	return x, y, width, height
}

// GetBoundsE returns the target window bounds or an explicit backend error.
func GetBoundsE(pid int, args ...int) (int, int, int, int, error) {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
	}

	x, y, width, height := internalGetBounds(pid, isPid)
	return checkedPlatformWindowGeometry(x, y, width, height)
}

// GetClient get the window client bounds
func GetClient(pid int, args ...int) (int, int, int, int) {
	x, y, width, height, _ := GetClientE(pid, args...)
	return x, y, width, height
}

// GetClientE returns the target window client bounds or an explicit backend error.
func GetClientE(pid int, args ...int) (int, int, int, int, error) {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
	}

	x, y, width, height := internalGetClient(pid, isPid)
	return checkedPlatformWindowGeometry(x, y, width, height)
}

func checkedPlatformWindowGeometry(x, y, width, height int) (int, int, int, int, error) {
	rect, err := validateWindowRect("native window geometry", Rect{
		Point: Point{X: x, Y: y},
		Size:  Size{W: width, H: height},
	})
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return rect.X, rect.Y, rect.W, rect.H, nil
}

// internalGetTitle get the window title
func internalGetTitle(pid int, args ...int) string {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
	}
	gtitle := cgetTitle(pid, isPid)

	return gtitle
}

// ActivePid active the window by PID,
//
// If args[0] > 0 on the Windows platform via a window handle to active
//
// Examples:
//
//	ids, _ := robotgo.FindIds()
//	robotgo.ActivePid(ids[0])
func ActivePid(pid int, args ...int) error {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
	}

	internalActive(pid, isPid)
	return nil
}

// DisplaysNum get the count of displays
func DisplaysNum() int {
	return int(C.get_num_displays())
}

// Alert shows an alert window and preserves the legacy bool-only API.
func Alert(title, msg string, args ...string) bool {
	accepted, _ := AlertE(title, msg, args...)
	return accepted
}

// AlertE shows an alert window and distinguishes a backend failure from the
// user rejecting the dialog.
func AlertE(title, msg string, args ...string) (bool, error) {
	defaultLabel, cancelLabel := alertArgs(args...)
	ct := C.CString(title)
	cm := C.CString(msg)
	defBtn := C.CString(defaultLabel)
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cm))
	defer C.free(unsafe.Pointer(defBtn))
	var cancelBtn *C.char
	if cancelLabel != "" {
		cancelBtn = C.CString(cancelLabel)
		defer C.free(unsafe.Pointer(cancelBtn))
	}
	return nativeAlertResult(int(C.showAlert(ct, cm, defBtn, cancelBtn)))
}
