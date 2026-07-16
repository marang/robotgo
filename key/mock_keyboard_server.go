//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

package key

/*
#cgo pkg-config: wayland-server
#include "testdata/mock_keyboard_server.c"
#include "../virtual-keyboard-unstable-v1-client-protocol.c"
*/
import "C"
import "unsafe"

func startMockKeyboardServer(socket string, expectedKeys, timeoutMs uint32, done chan struct{}) {
	startMockKeyboardServerWithSeats(socket, expectedKeys, timeoutMs, 1, 1, done)
}

func startMockKeyboardServerWithSeats(
	socket string, expectedKeys, timeoutMs, seatCount, keyboardSeatMask uint32, done chan struct{},
) {
	csock := C.CString(socket)
	go func() {
		C.run_mock_keyboard_server_with_seats(
			csock,
			C.uint32_t(expectedKeys),
			C.uint32_t(timeoutMs),
			C.uint32_t(seatCount),
			C.uint32_t(keyboardSeatMask),
		)
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

func mockKeyboardLastMods() uint32 {
	return uint32(C.mock_keyboard_last_mods())
}

func mockKeyboardServerReady() bool {
	return C.mock_keyboard_server_ready() != 0
}

func mockKeyboardSeatResourcesDestroyed() uint32 {
	return uint32(C.mock_keyboard_seat_resources_destroyed())
}

func mockKeyboardKeyboardResourcesDestroyed() uint32 {
	return uint32(C.mock_keyboard_keyboard_resources_destroyed())
}

func mockKeyboardSelectedSeat() uint32 {
	return uint32(C.mock_keyboard_selected_seat())
}

func setMockKeyboardSeatCapabilities(seat, capabilities uint32) uint32 {
	return uint32(C.mock_keyboard_set_seat_capabilities(
		C.uint32_t(seat), C.uint32_t(capabilities),
	))
}

func removeMockKeyboardSeatGlobal(seat uint32) uint32 {
	return uint32(C.mock_keyboard_remove_seat_global(C.uint32_t(seat)))
}

func mockKeyboardControlAppliedGeneration() uint32 {
	return uint32(C.mock_keyboard_control_applied_generation())
}
