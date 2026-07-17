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

//go:build linux && wayland
// +build linux,wayland

package robotgo

/*
#cgo pkg-config: wayland-client wayland-cursor wayland-egl xkbcommon gbm libdrm
#cgo LDFLAGS: -lwayland-client -lwayland-cursor -lwayland-egl -lxkbcommon -lgbm -ldrm
#cgo CFLAGS: -DROBOTGO_USE_WAYLAND -DDISPLAY_SERVER_WAYLAND
#include "window/get_bounds_wayland.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"log"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xinerama"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/marang/robotgo/base"
)

// GetBounds returns the window bounds and dispatches to the appropriate
// Wayland or X11 implementation based on DetectDisplayServer.
func GetBounds(pid int, args ...int) (int, int, int, int) {
	if base.DetectDisplayServer() == base.Wayland {
		rect := GetScreenRect(-1)
		return rect.X, rect.Y, rect.W, rect.H
	}
	if !nativeX11BackendCompiled() {
		return 0, 0, 0, 0
	}

	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
		return internalGetBounds(pid, isPid)
	}

	xid, err := GetXid(nil, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return 0, 0, 0, 0
	}

	return internalGetBounds(int(xid), isPid)
}

// GetClient returns the client bounds of the window and dispatches to the
// Wayland or X11 implementation based on DetectDisplayServer.
func GetClient(pid int, args ...int) (int, int, int, int) {
	if base.DetectDisplayServer() == base.Wayland {
		rect := GetScreenRect(-1)
		return rect.X, rect.Y, rect.W, rect.H
	}
	if !nativeX11BackendCompiled() {
		return 0, 0, 0, 0
	}

	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
		return internalGetClient(pid, isPid)
	}

	xid, err := GetXid(nil, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return 0, 0, 0, 0
	}

	return internalGetClient(int(xid), isPid)
}

// internalGetTitle get the window title
func internalGetTitle(pid int, args ...int) string {
	if !nativeX11BackendCompiled() {
		return ""
	}
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
		return cgetTitle(pid, isPid)
	}

	xid, err := GetXid(nil, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return ""
	}

	return cgetTitle(int(xid), isPid)
}

// ActivePidC active the window by Pid,
// If args[0] > 0 on the unix platform via a xid to active
func ActivePidC(pid int, args ...int) error {
	if base.DetectDisplayServer() == base.Wayland {
		return waylandWindowNotSupported("activate window")
	}
	if err := nativeX11WindowReady(); err != nil {
		return err
	}

	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
		if !internalActive(pid, isPid) {
			return fmt.Errorf("%w: native X11 backend could not activate target window", errWindowOperationFailed)
		}
		return nil
	}

	xid, err := GetXid(nil, pid)
	if err != nil {
		log.Println("Get Xid from Pid errors is: ", err)
		return err
	}

	if !internalActive(int(xid), isPid) {
		return fmt.Errorf("%w: native X11 backend could not activate target window", errWindowOperationFailed)
	}
	return nil
}

// ActivePid active the window by Pid,
//
// If args[0] > 0 on the Windows platform via a window handle to active,
// If args[0] > 0 on the unix platform via a xid to active
func ActivePid(pid int, args ...int) error {
	if base.DetectDisplayServer() == base.Wayland {
		return waylandWindowNotSupported("activate window")
	}
	if err := nativeX11WindowReady(); err != nil {
		return err
	}

	xu, err := xgbutil.NewConn()
	if err != nil {
		return err
	}
	defer xu.Conn().Close()

	if len(args) > 0 {
		err := ewmh.ActiveWindowReq(xu, xproto.Window(pid))
		if err != nil {
			return err
		}

		return nil
	}

	// get the xid from pid
	xid, err := GetXidByPid(xu, pid)
	if err != nil {
		return err
	}

	err = ewmh.ActiveWindowReq(xu, xid)
	if err != nil {
		return err
	}

	return nil
}

// GetXid get the xid return window and error
func GetXid(xu *xgbutil.XUtil, pid int) (xproto.Window, error) {
	owned := false
	if xu == nil {
		var err error
		xu, err = xgbutil.NewConn()
		if err != nil {
			// log.Println("xgbutil.NewConn errors is: ", err)
			return 0, err
		}
		owned = true
	}
	if owned {
		defer xu.Conn().Close()
	}

	xid, err := GetXidByPid(xu, pid)
	return xid, err
}

// Deprecated: use the GetXidByPid(),
//
// GetXidFromPid get the xid from pid
func GetXidFromPid(xu *xgbutil.XUtil, pid int) (xproto.Window, error) {
	return GetXidByPid(xu, pid)
}

// GetXidByPid get the xid from pid
func GetXidByPid(xu *xgbutil.XUtil, pid int) (xproto.Window, error) {
	windows, err := ewmh.ClientListGet(xu)
	if err != nil {
		return 0, err
	}

	for _, window := range windows {
		wmPid, err := x11WindowPID(xu, window)
		if err != nil {
			// A client can legitimately omit _NET_WM_PID. Keep scanning the
			// remaining client list instead of making its ordering observable.
			continue
		}

		if uint(pid) == wmPid {
			return window, nil
		}
	}

	return 0, errors.New("failed to find a window with a matching pid")
}

// DisplaysNum returns the number of outputs exposed by the active display
// server without routing Wayland sessions through X11.
func DisplaysNum() int {
	if base.DetectDisplayServer() == base.Wayland {
		display := C.wl_display_connect(nil)
		if display == nil {
			return 0
		}
		defer C.wl_display_disconnect(display)
		return int(C.get_num_displays_wayland(display))
	}

	c, err := xgb.NewConn()
	if err != nil {
		return 0
	}
	defer c.Close()

	err = xinerama.Init(c)
	if err != nil {
		return 0
	}

	reply, err := xinerama.QueryScreens(c).Reply()
	if err != nil {
		return 0
	}

	return int(reply.Number)
}

// GetMainId returns the primary display index for the active display server.
// Wayland output indices are deterministic: the output containing logical
// coordinate (0,0) is first, followed by geometry order.
func GetMainId() int {
	if base.DetectDisplayServer() == base.Wayland {
		display := C.wl_display_connect(nil)
		if display == nil {
			return -1
		}
		defer C.wl_display_disconnect(display)
		return int(C.get_main_display_wayland(display))
	}

	conn, err := xgb.NewConn()
	if err != nil {
		return -1
	}
	defer conn.Close()

	setup := xproto.Setup(conn)
	defaultScreen := setup.DefaultScreen(conn)
	id := -1
	for i, screen := range setup.Roots {
		if defaultScreen.Root == screen.Root {
			id = i
			break
		}
	}
	return id
}

// Alert show a alert window
// Displays alert with the attributes.
// If cancel button is not given, only the default button is displayed
//
// Examples:
//
//	robotgo.Alert("hi", "window", "ok", "cancel")
