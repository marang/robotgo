//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package screen

import (
	"errors"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
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

	if _, err := CaptureScreen(); err != nil {
		if errors.Is(err, robotgo.ErrDmabufImport) || errors.Is(err, robotgo.ErrDmabufMap) {
			t.Skip("dmabuf allocation not available")
		}
		t.Fatalf("capture failed: %v", err)
	}

	<-done
}
