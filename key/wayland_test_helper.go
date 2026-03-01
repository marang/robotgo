//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

package key

/*
#cgo CFLAGS: -DROBOTGO_USE_WAYLAND -DDISPLAY_SERVER_WAYLAND
#cgo pkg-config: x11 xtst wayland-client xkbcommon
#include "keypress_c.h"
*/
import "C"
import "unsafe"

func sendUTFForTest(s string) int {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return int(C.input_utf(cs))
}
