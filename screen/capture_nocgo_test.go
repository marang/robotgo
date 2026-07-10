//go:build !cgo

package screen

import (
	"errors"
	"testing"

	robotgo "github.com/marang/robotgo"
)

func TestCaptureReportsUnsupportedWithoutBackend(t *testing.T) {
	_, err := robotgo.CaptureImg()
	if !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("CaptureImg error = %v, want ErrNotSupported", err)
	}
}
