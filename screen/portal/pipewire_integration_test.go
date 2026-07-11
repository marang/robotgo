//go:build linux && cgo && pipewire && integration

package portal

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPipeWireCapturePersistentSessionIntegration(t *testing.T) {
	if os.Getenv("ROBOTGO_SCREENCAST_E2E") == "" {
		t.Skip("set ROBOTGO_SCREENCAST_E2E=1 in a graphical Wayland session")
	}
	openCtx, cancelOpen := context.WithTimeout(context.Background(), 2*time.Minute)
	capture, err := OpenPipeWireCapture(openCtx, ScreenCastOptions{
		Sources: ScreenCastSourceMonitor,
		Cursor:  ScreenCastCursorEmbedded,
		Persist: ScreenCastPersistApplication,
	}, 0)
	cancelOpen()
	if err != nil {
		t.Fatalf("OpenPipeWireCapture error: %v", err)
	}
	defer func() {
		if err := capture.Close(); err != nil {
			t.Errorf("Close error: %v", err)
		}
	}()

	for frameNumber := 1; frameNumber <= 2; frameNumber++ {
		frameCtx, cancelFrame := context.WithTimeout(context.Background(), 10*time.Second)
		frame, err := capture.Capture(frameCtx, 0, 0, 0, 0)
		cancelFrame()
		if err != nil {
			t.Fatalf("capture frame %d: %v", frameNumber, err)
		}
		if frame.Bounds().Empty() {
			t.Fatalf("frame %d is empty", frameNumber)
		}
	}
}
