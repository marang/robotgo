//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

package key

/*
#cgo pkg-config: wayland-server
#include "testdata/mock_keyboard_server.c"
*/
import "C"
import "unsafe"

func startMockKeyboardServer(socket string, expectedKeys, timeoutMs uint32, done chan struct{}) {
	csock := C.CString(socket)
	go func() {
		C.run_mock_keyboard_server(csock, C.uint32_t(expectedKeys), C.uint32_t(timeoutMs))
		C.free(unsafe.Pointer(csock))
		close(done)
	}()
}

func stopMockKeyboardServer() {
	C.stop_mock_keyboard_server()
}

func mockKeyboardKeyEvents() uint32 {
	return uint32(C.mock_keyboard_key_events())
}

func mockKeyboardModEvents() uint32 {
	return uint32(C.mock_keyboard_mod_events())
}
