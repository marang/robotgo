// Copyright 2016 The go-vgo Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

package robotgo_test

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"testing"

	"github.com/marang/robotgo"
	"github.com/vcaesar/tt"
)

func requireDisplay(t *testing.T) {
	t.Helper()
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display available")
	}
}

func TestGetVer(t *testing.T) {
	fmt.Println("go version: ", runtime.Version())
	ver := robotgo.GetVersion()

	tt.Expect(t, robotgo.Version, ver)
}

func TestGetScreenSize(t *testing.T) {
    requireDisplay(t)
    if runtime.GOOS == "linux" && robotgo.DetectDisplayServer() == robotgo.DisplayServerWayland {
        t.Skip("skip size/location on Wayland in non-wayland build")
    }
	x, y := robotgo.GetScreenSize()
	log.Println("Get screen size: ", x, y)

	rect := robotgo.GetScreenRect()
	fmt.Println("Get screen rect: ", rect)

	x, y = robotgo.Location()
	fmt.Println("Get location: ", x, y)
}

func TestGetSysScale(t *testing.T) {
    requireDisplay(t)
    if runtime.GOOS == "linux" && robotgo.DetectDisplayServer() == robotgo.DisplayServerWayland {
        t.Skip("skip SysScale on Wayland in non-wayland build")
    }
	s := robotgo.SysScale()
	log.Println("SysScale: ", s)

	f := robotgo.ScaleF()
	log.Println("scale: ", f)
}

func TestGetTitle(t *testing.T) {
    requireDisplay(t)
    if runtime.GOOS == "linux" && robotgo.DetectDisplayServer() == robotgo.DisplayServerWayland {
        t.Skip("skip window title on Wayland in non-wayland build")
    }
	// just exercise the function, it used to crash with a segfault + "Maximum
	// number of clients reached"
	for i := 0; i < 128; i++ {
		robotgo.GetTitle()
	}
}
