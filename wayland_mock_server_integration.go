//go:build cgo && linux && wayland && waylandint

package robotgo

/*
#cgo pkg-config: wayland-server
#include <stdlib.h>
#include "key/testdata/mock_keyboard_server.c"
*/
import "C"

import "unsafe"

func startPublicWaylandMockServer(socket string, timeoutMS uint32, done chan struct{}) {
	csock := C.CString(socket)
	go func() {
		C.run_mock_keyboard_server(csock, 0, C.uint32_t(timeoutMS))
		C.free(unsafe.Pointer(csock))
		close(done)
	}()
}

func stopPublicWaylandMockServer() {
	C.stop_mock_keyboard_server()
}

func publicWaylandMockReady() bool {
	return C.mock_keyboard_server_ready() != 0
}

func publicWaylandMockKeyEvents() uint32 {
	return uint32(C.mock_keyboard_key_events())
}

func publicWaylandMockModEvents() uint32 {
	return uint32(C.mock_keyboard_mod_events())
}

func publicWaylandMockLastMods() uint32 {
	return uint32(C.mock_keyboard_last_mods())
}
