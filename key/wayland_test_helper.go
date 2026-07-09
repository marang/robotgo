//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

package key

/*
#cgo CFLAGS: -DROBOTGO_USE_WAYLAND -DDISPLAY_SERVER_WAYLAND
#cgo pkg-config: wayland-client xkbcommon
#include <stdlib.h>
#include "wayland_test_helper.h"
*/
import "C"
import "unsafe"

func sendUTFForTest(s string) int {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return int(C.robotgo_wayland_test_input_utf(cs))
}
