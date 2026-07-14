//go:build !cgo && linux

package screen

import (
	"errors"
	"testing"

	robotgo "github.com/marang/robotgo"
)

func TestCaptureReportsUnsupportedWithoutBackend(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	_, err := robotgo.CaptureImg()
	if !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("CaptureImg error = %v, want ErrNotSupported", err)
	}
}
