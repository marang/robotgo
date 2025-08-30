//go:build wayland
// +build wayland

package robotgo

/*
#cgo pkg-config: wayland-client
#cgo CFLAGS: -DUSE_WAYLAND
#include "window/get_bounds_wayland_c.h"
*/
import "C"

import "log"

func GetBounds(pid int, args ...int) (int, int, int, int) {
	var w, h C.int
	if C.get_bounds_wayland(&w, &h) != 0 {
		log.Println("get_bounds_wayland failed")
		return 0, 0, 0, 0
	}
	return 0, 0, int(w), int(h)
}
