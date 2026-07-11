//go:build cgo

package robotgo

import (
	"image"
	"testing"
)

func TestCaptureScreenUsesForcedPersistentScreenCast(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	t.Setenv(envWaylandBackend, waylandBackendScreenCast)
	setLastBackend(BackendNone)
	t.Cleanup(func() { setLastBackend(BackendNone) })

	frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
	frame.Set(0, 0, image.White)
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = &fakeScreenCastCapture{frame: frame}
	screenCastCaptureState.Unlock()

	bitmap, err := CaptureScreen(0, 0, 2, 2)
	if err != nil {
		t.Fatalf("CaptureScreen error: %v", err)
	}
	if bitmap == nil {
		t.Fatal("CaptureScreen returned a nil bitmap")
	}
	FreeBitmap(bitmap)
	if got := LastBackend(); got != BackendScreenCast {
		t.Fatalf("LastBackend = %q, want %q", got, BackendScreenCast)
	}
}

func TestCaptureScreenForcedScreenCastRequiresActiveSession(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	t.Setenv(envWaylandBackend, waylandBackendScreenCast)
	if bitmap, err := CaptureScreen(0, 0, 2, 2); bitmap != nil || err == nil {
		t.Fatalf("CaptureScreen = (%v, %v), want explicit unavailable error", bitmap, err)
	}
}
