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

func sendExactTextForTest(s string) int {
	runes := []rune(s)
	values := make([]C.uint32_t, len(runes))
	for index, value := range runes {
		values[index] = C.uint32_t(value)
	}
	var valuesPtr *C.uint32_t
	if len(values) > 0 {
		valuesPtr = &values[0]
	}
	return int(C.robotgo_wayland_test_type_codepoints(
		valuesPtr, C.size_t(len(values)), 0,
	))
}

const (
	waylandTestKeysymA       = 0x61
	waylandTestKeysymShiftL  = 0xffe1
	waylandTestKeysymControl = 0xffe3
	waylandTestModControl    = 1 << 2
	waylandTestModShift      = 1 << 3
)

func toggleWaylandKeyForTest(keysym uint32, down bool, flags uint32) int {
	return int(C.robotgo_wayland_test_toggle_key(
		C.uint32_t(keysym), C.int(boolToInt(down)), C.uint32_t(flags),
	))
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func waylandKeyboardReadyForTest() int {
	return int(C.robotgo_wayland_test_keyboard_ready())
}

func waylandKeyboardLastErrorForTest() int {
	return int(C.robotgo_wayland_test_keyboard_last_error())
}

func syncWaylandKeyboardForTest() int {
	return int(C.robotgo_wayland_test_roundtrip())
}

func disconnectWaylandKeyboardTransportForTest() int {
	return int(C.robotgo_wayland_test_disconnect_transport())
}

func closeWaylandKeyboardForTest() {
	C.robotgo_wayland_test_keyboard_close()
}
