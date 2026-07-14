//go:build !cgo && !linux

package robotgo

import (
	"errors"
	"image"
	"runtime"
	"testing"
)

func TestPureGoCaptureImgUsesPortableNativeBackend(t *testing.T) {
	if !pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH) {
		if _, err := CaptureImg(); !errors.Is(err, ErrNotSupported) {
			t.Fatalf("CaptureImg error = %v, want ErrNotSupported", err)
		}
		return
	}
	t.Setenv(envXDGSessionType, "x11")
	nativeCapture := pureGoCaptureImage
	backend := LastBackend()
	t.Cleanup(func() {
		pureGoCaptureImage = nativeCapture
		setPureGoCaptureBackend(backend)
	})
	pureGoCaptureImage = func(args ...int) (image.Image, error) {
		return image.NewRGBA(image.Rect(0, 0, 3, 2)), nil
	}
	img, err := CaptureImg(10, 20, 3, 2)
	if err != nil {
		t.Fatalf("CaptureImg error: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatalf("capture bounds = %v", img.Bounds())
	}
	wantBackend := pureGoScreenshotBackend(runtime.GOOS)
	if LastBackend() != wantBackend {
		t.Fatalf("LastBackend = %q, want %q", LastBackend(), wantBackend)
	}
}
