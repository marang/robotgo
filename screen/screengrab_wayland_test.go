//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package screen

import (
	"testing"
	"time"
)

// TestScreencopyDmabuf ensures CaptureScreen handles linux_dmabuf/buffer_done events.
func TestScreencopyDmabuf(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)

	done := make(chan struct{})
	startMockServer(sock, done)

	// Allow the server to start
	time.Sleep(100 * time.Millisecond)

	if _, err := CaptureScreen(); err == nil {
		t.Fatalf("expected error due to unsupported dmabuf, got nil")
	}

	<-done
}
