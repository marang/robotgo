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
	csock := C.CString(socket)
	go func() {
		C.run_mock_server(csock, C.uint32_t(maj), C.uint32_t(min), C.uint64_t(modifier))
		C.free(unsafe.Pointer(csock))
		close(done)
	}()
}
