// Copyright 2016 The go-vgo Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

package robotgo

import (
	"os"

	"github.com/vcaesar/keycode"
)

type uMap map[string]uint16

// MouseMap robotgo hook mouse's code map
var MouseMap = keycode.MouseMap

const (
	// Mleft mouse left button
	Mleft      = "left"
	Mright     = "right"
	Center     = "center"
	WheelDown  = "wheelDown"
	WheelUp    = "wheelUp"
	WheelLeft  = "wheelLeft"
	WheelRight = "wheelRight"
)

// Keycode robotgo hook key's code map
var Keycode = keycode.Keycode

// Special is the special key map
var Special = keycode.Special

// specialWayland holds the special key map for Wayland displays.
var specialWayland = keycode.Special

// CurrentSpecialTable returns the special key map for the active
// display server. If the environment indicates a Wayland session
// (WAYLAND_DISPLAY is set and DISPLAY is empty) the Wayland table is
// returned, otherwise the X11 table is used.
func CurrentSpecialTable() map[string]string {
	if os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("DISPLAY") == "" {
		return specialWayland
	}
	return Special
}
