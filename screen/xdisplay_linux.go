//go:build linux
// +build linux

package screen

/*
#cgo linux LDFLAGS: -lX11
#include <stdlib.h>
#include <X11/Xlib.h>
*/
import "C"
import "unsafe"

func openXDisplay(name string) bool {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	d := C.XOpenDisplay(cName)
	if d == nil {
		return false
	}
	C.XCloseDisplay(d)
	return true
}
