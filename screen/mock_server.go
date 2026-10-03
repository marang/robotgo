//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package screen

/*
#cgo pkg-config: wayland-server
#include "testdata/mock_server.c"
*/
import "C"
import "unsafe"

func startMockServer(socket string, maj, min uint32, modifier uint64, done chan struct{}) {
	startMockServerMode(socket, maj, min, modifier, 0, done)
}

func startMockServerMode(socket string, maj, min uint32, modifier uint64, mode uint32, done chan struct{}) {
	csock := C.CString(socket)
	go func() {
		C.run_mock_server_mode(csock, C.uint32_t(maj), C.uint32_t(min), C.uint64_t(modifier), C.uint32_t(mode))
		C.free(unsafe.Pointer(csock))
		close(done)
	}()
}

func stopMockServer() {
	C.stop_mock_server()
}

func mockPointerFrameCount() uint32 { return uint32(C.mock_pointer_frame_count()) }

func mockPointerScroll(axis uint32) (sourceCount, source uint32, value, discreteValue, discrete int32) {
	return uint32(C.mock_pointer_axis_source_count()), uint32(C.mock_pointer_axis_source()),
		int32(C.mock_pointer_axis_value(C.uint32_t(axis))),
		int32(C.mock_pointer_axis_discrete_value(C.uint32_t(axis))),
		int32(C.mock_pointer_axis_discrete_count(C.uint32_t(axis)))
}
