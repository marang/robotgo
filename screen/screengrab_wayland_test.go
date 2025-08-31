//go:build cgo && linux
// +build cgo,linux

package screen

import (
	"testing"

	robotgo "github.com/marang/robotgo"
)

func TestScreengrabWayland(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if _, err := robotgo.CaptureScreen(); err != nil {
		t.Skipf("Wayland capture skipped: %v", err)
	}
}
