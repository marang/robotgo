//go:build linux && !wayland
// +build linux,!wayland

package robotgo

/*
#include <X11/Xlib.h>
Display *XGetMainDisplay(void);
*/
import "C"

func x11MainDisplayAvailable() bool {
	return C.XGetMainDisplay() != nil
}
