//go:build !cgo

package robotgo

import (
	"errors"
	"image"
	"image/color"
	"math"
	"runtime"
	"testing"
)

func preservePureGoCaptureHelperState(t *testing.T) {
	t.Helper()
	capture := pureGoCaptureImage
	backend := LastBackend()
	t.Cleanup(func() {
		pureGoCaptureImage = capture
		setPureGoCaptureBackend(backend)
	})
}

func TestPureGoCaptureHelperParity(t *testing.T) {
	if !pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH) {
		t.Skipf("Pure-Go capture is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	preservePureGoCaptureHelperState(t)
	if runtime.GOOS == "linux" {
		t.Setenv(envWaylandDisplay, "")
		t.Setenv(envDisplay, ":99")
		t.Setenv(envXDGSessionType, "x11")
	}

	const (
		regionX = -10
		regionY = 20
		width   = 2
		height  = 2
	)
	captureCalls := 0
	pureGoCaptureImage = func(args ...int) (image.Image, error) {
		if len(args) != 0 {
			want := []int{regionX, regionY, width, height}
			if len(args) != len(want) {
				t.Fatalf("capture args = %v, want %v", args, want)
			}
			for i := range want {
				if args[i] != want[i] {
					t.Fatalf("capture args = %v, want %v", args, want)
				}
			}
		}
		captureCalls++
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		img.SetRGBA(1, 1, color.RGBA{R: 50, G: 60, B: 70, A: 255})
		return img, nil
	}

	serialized, err := CaptureBitmapStr(regionX, regionY, width, height)
	if err != nil {
		t.Fatalf("CaptureBitmapStr error: %v", err)
	}
	decoded, err := BitmapFromStr(serialized)
	if err != nil {
		t.Fatalf("BitmapFromStr error: %v", err)
	}
	r, g, b, ok := bitmapRGBAt(decoded, 1, 1)
	if !ok || r != 50 || g != 60 || b != 70 {
		t.Fatalf("decoded target pixel = %d,%d,%d,%v; want 50,60,70,true", r, g, b, ok)
	}

	needleImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	needleImage.SetRGBA(0, 0, color.RGBA{R: 50, G: 60, B: 70, A: 255})
	needle, err := ToStrBitmap(RGBAToBitmap(needleImage))
	if err != nil {
		t.Fatalf("serialize needle: %v", err)
	}
	x, y, err := FindBitmapStr(needle)
	if err != nil {
		t.Fatalf("FindBitmapStr capture error: %v", err)
	}
	if x != 1 || y != 1 {
		t.Fatalf("FindBitmapStr capture result = (%d,%d), want (1,1)", x, y)
	}

	x, y, err = FindColorCS(regionX, regionY, width, height, CHex(0x333c46))
	if err != nil {
		t.Fatalf("FindColorCS default tolerance error: %v", err)
	}
	if x != regionX+1 || y != regionY+1 {
		t.Fatalf("FindColorCS = (%d,%d), want (%d,%d)", x, y, regionX+1, regionY+1)
	}

	x, y, err = FindcolorCS(regionX, regionY, width, height, CHex(0x333c46), 0)
	if err != nil {
		t.Fatalf("FindcolorCS exact tolerance error: %v", err)
	}
	if x != -1 || y != -1 {
		t.Fatalf("FindcolorCS exact result = (%d,%d), want (-1,-1)", x, y)
	}

	if captureCalls != 4 {
		t.Fatalf("capture calls = %d, want 4", captureCalls)
	}
	if got, want := LastBackend(), pureGoScreenshotBackend(runtime.GOOS); got != want {
		t.Fatalf("LastBackend = %q, want %q", got, want)
	}
}

func TestPureGoCaptureHelpersPreserveBackendErrors(t *testing.T) {
	if !pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH) {
		t.Skipf("Pure-Go capture is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	preservePureGoCaptureHelperState(t)
	if runtime.GOOS == "linux" {
		t.Setenv(envWaylandDisplay, "")
		t.Setenv(envDisplay, ":99")
		t.Setenv(envXDGSessionType, "x11")
	}

	wantErr := errors.New("capture failed")
	calls := 0
	pureGoCaptureImage = func(...int) (image.Image, error) {
		calls++
		return nil, wantErr
	}
	needle, err := ToStrBitmap(RGBAToBitmap(image.NewRGBA(image.Rect(0, 0, 1, 1))))
	if err != nil {
		t.Fatalf("serialize needle: %v", err)
	}

	if _, _, err := FindColorCS(0, 0, 1, 1, CHex(0), math.NaN()); err == nil {
		t.Fatal("FindColorCS unexpectedly accepted a NaN tolerance")
	}
	if calls != 0 {
		t.Fatalf("invalid tolerance reached capture backend %d times", calls)
	}
	if _, err := CaptureBitmapStr(0, 0, 1, 1); !errors.Is(err, wantErr) {
		t.Fatalf("CaptureBitmapStr error = %v, want wrapped capture error", err)
	}
	if _, _, err := FindBitmapStr(needle); !errors.Is(err, wantErr) {
		t.Fatalf("FindBitmapStr error = %v, want wrapped capture error", err)
	}
	if _, _, err := FindColorCS(0, 0, 1, 1, CHex(0)); !errors.Is(err, wantErr) {
		t.Fatalf("FindColorCS error = %v, want wrapped capture error", err)
	}
	if calls != 3 {
		t.Fatalf("capture backend calls = %d, want 3", calls)
	}
}
