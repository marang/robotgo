//go:build darwin && !cgo

package robotgo

import (
	"errors"
	"image"
	"math"
	"testing"
	"unsafe"
)

type darwinGraphicsCounters struct {
	closeCalls        int
	modeReleaseCalls  int
	imageReleaseCalls int
	colorReleaseCalls int
	contextCalls      int
	drawCalls         int
}

func fakeDarwinGraphics(bounds cgRect, counters *darwinGraphicsCounters) *darwinGraphicsAPI {
	var bitmapData []byte
	return &darwinGraphicsAPI{
		close:                  func() error { counters.closeCalls++; return nil },
		preflightCaptureAccess: func() bool { return true },
		getActiveDisplayList: func(max uint32, displays *uint32, count *uint32) int32 {
			*count = 1
			if max > 0 && displays != nil {
				*displays = 42
			}
			return cgErrorSuccess
		},
		displayBounds:          func(uint32) cgRect { return bounds },
		mainDisplayID:          func() uint32 { return 42 },
		displayCopyDisplayMode: func(uint32) uintptr { return 21 },
		displayModeGetPixelWidth: func(uintptr) uintptr {
			return 200
		},
		displayModeGetWidth: func(uintptr) uintptr {
			return 100
		},
		displayModeRelease:    func(uintptr) { counters.modeReleaseCalls++ },
		windowListCreateImage: func(cgRect, uint32, uint32, uint32) uintptr { return 11 },
		imageRelease:          func(uintptr) { counters.imageReleaseCalls++ },
		colorSpaceCreateDeviceRGB: func() uintptr {
			return 12
		},
		colorSpaceRelease: func(uintptr) { counters.colorReleaseCalls++ },
		bitmapContextCreate: func(data unsafe.Pointer, width, height, _ uintptr, bytesPerRow uintptr, _ uintptr, _ uint32) uintptr {
			if data != nil {
				return 0
			}
			bitmapData = make([]byte, int(height*bytesPerRow))
			for offset := 0; offset < len(bitmapData); offset += 4 {
				bitmapData[offset], bitmapData[offset+1], bitmapData[offset+2], bitmapData[offset+3] = 1, 2, 3, 255
			}
			if width == 0 || height == 0 {
				return 0
			}
			return 13
		},
		bitmapContextGetData: func(uintptr) unsafe.Pointer {
			if len(bitmapData) == 0 {
				return nil
			}
			return unsafe.Pointer(&bitmapData[0])
		},
		contextRelease:      func(uintptr) { counters.contextCalls++ },
		contextTranslateCTM: func(uintptr, float64, float64) {},
		contextScaleCTM:     func(uintptr, float64, float64) {},
		contextDrawImage:    func(uintptr, cgRect, uintptr) { counters.drawCalls++ },
	}
}

func TestCaptureDarwinWithAPIProducesOwnedRGBA(t *testing.T) {
	counters := &darwinGraphicsCounters{}
	api := fakeDarwinGraphics(cgRect{}, counters)
	img, err := captureDarwinWithAPI(api, image.Rect(-2, 4, 1, 6))
	if err != nil {
		t.Fatalf("captureDarwinWithAPI error: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatalf("bounds = %v, want 3x2 zero-origin image", img.Bounds())
	}
	if got := img.RGBAAt(0, 0); got.R != 1 || got.G != 2 || got.B != 3 || got.A != 255 {
		t.Fatalf("pixel = %#v, want RGBA(1,2,3,255)", got)
	}
	if counters.imageReleaseCalls != 1 || counters.colorReleaseCalls != 1 || counters.contextCalls != 1 || counters.drawCalls != 1 {
		t.Fatalf("resource counters = %+v, want every CoreGraphics object released once", counters)
	}
}

func TestCaptureDarwinWithAPIReportsPermissionDenial(t *testing.T) {
	counters := &darwinGraphicsCounters{}
	api := fakeDarwinGraphics(cgRect{}, counters)
	api.preflightCaptureAccess = func() bool { return false }
	imageCalls := 0
	api.windowListCreateImage = func(cgRect, uint32, uint32, uint32) uintptr {
		imageCalls++
		return 0
	}
	_, err := captureDarwinWithAPI(api, image.Rect(0, 0, 1, 1))
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("error = %v, want ErrPermissionDenied", err)
	}
	if imageCalls != 0 {
		t.Fatalf("capture calls = %d, want permission denial before capture", imageCalls)
	}
}

func TestDarwinDisplayBoundsUseEnclosingEdges(t *testing.T) {
	counters := &darwinGraphicsCounters{}
	api := fakeDarwinGraphics(cgRect{
		Origin: cgPoint{X: -10.25, Y: 20.5},
		Size:   cgSize{Width: 30.5, Height: 40.25},
	}, counters)
	bounds, err := darwinDisplayBounds(api, 0)
	if err != nil {
		t.Fatalf("darwinDisplayBounds error: %v", err)
	}
	if want := image.Rect(-11, 20, 21, 61); bounds != want {
		t.Fatalf("bounds = %v, want %v", bounds, want)
	}
	if _, err := darwinDisplayBounds(api, 1); err == nil {
		t.Fatal("out-of-range display index unexpectedly succeeded")
	}
}

func TestDarwinDisplayScaleUsesRetinaWidthsAndReleasesMode(t *testing.T) {
	counters := &darwinGraphicsCounters{}
	api := fakeDarwinGraphics(cgRect{}, counters)
	var requestedDisplay uint32
	api.displayCopyDisplayMode = func(displayID uint32) uintptr {
		requestedDisplay = displayID
		return 55
	}
	api.displayModeGetPixelWidth = func(mode uintptr) uintptr {
		if mode != 55 {
			t.Fatalf("pixel-width mode = %d, want 55", mode)
		}
		return 3024
	}
	api.displayModeGetWidth = func(mode uintptr) uintptr {
		if mode != 55 {
			t.Fatalf("logical-width mode = %d, want 55", mode)
		}
		return 1512
	}

	scale, err := darwinDisplayScale(api, 77)
	if err != nil {
		t.Fatalf("darwinDisplayScale: %v", err)
	}
	if scale != 2 {
		t.Fatalf("scale = %v, want 2", scale)
	}
	if requestedDisplay != 77 {
		t.Fatalf("display ID = %d, want 77", requestedDisplay)
	}
	if counters.modeReleaseCalls != 1 {
		t.Fatalf("display-mode releases = %d, want 1", counters.modeReleaseCalls)
	}
}

func TestDarwinDisplayScaleUsesMainDisplay(t *testing.T) {
	for _, displayID := range [][]int{nil, {-1}} {
		counters := &darwinGraphicsCounters{}
		api := fakeDarwinGraphics(cgRect{}, counters)
		requestedDisplay := uint32(0)
		api.mainDisplayID = func() uint32 { return 91 }
		api.displayCopyDisplayMode = func(displayID uint32) uintptr {
			requestedDisplay = displayID
			return 55
		}
		if _, err := darwinDisplayScale(api, displayID...); err != nil {
			t.Fatalf("darwinDisplayScale(%v): %v", displayID, err)
		}
		if requestedDisplay != 91 {
			t.Fatalf("darwinDisplayScale(%v) display = %d, want main display 91", displayID, requestedDisplay)
		}
	}
}

func TestDarwinDisplayScaleReportsUnavailableAndInvalidModes(t *testing.T) {
	counters := &darwinGraphicsCounters{}
	api := fakeDarwinGraphics(cgRect{}, counters)
	api.displayModeGetWidth = nil
	if _, err := darwinDisplayScale(api); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("missing-symbol error = %v, want ErrNotSupported", err)
	}

	api = fakeDarwinGraphics(cgRect{}, counters)
	api.displayCopyDisplayMode = func(uint32) uintptr { return 0 }
	if _, err := darwinDisplayScale(api); err == nil {
		t.Fatal("nil display mode unexpectedly succeeded")
	}

	api = fakeDarwinGraphics(cgRect{}, counters)
	api.displayModeGetPixelWidth = func(uintptr) uintptr { return 0 }
	if _, err := darwinDisplayScale(api); err == nil {
		t.Fatal("zero pixel width unexpectedly succeeded")
	}
	if counters.modeReleaseCalls != 1 {
		t.Fatalf("display-mode releases = %d, want release after invalid width", counters.modeReleaseCalls)
	}

	if _, err := darwinDisplayScale(api, -2); err == nil {
		t.Fatal("invalid negative display ID unexpectedly succeeded")
	}
}

func TestPureGoDarwinSysScaleEntryPoint(t *testing.T) {
	previous := openDarwinGraphics
	counters := &darwinGraphicsCounters{}
	openDarwinGraphics = func() (*darwinGraphicsAPI, error) {
		return fakeDarwinGraphics(cgRect{}, counters), nil
	}
	t.Cleanup(func() { openDarwinGraphics = previous })

	if scale := SysScale(); scale != 2 {
		t.Fatalf("SysScale() = %v, want 2", scale)
	}
	if counters.modeReleaseCalls != 1 || counters.closeCalls != 1 {
		t.Fatalf("resource counters = %+v, want one mode release and framework close", counters)
	}
}

func TestPureGoDarwinGetScaleSizeUsesRetinaFactor(t *testing.T) {
	previous := openDarwinGraphics
	counters := &darwinGraphicsCounters{}
	openDarwinGraphics = func() (*darwinGraphicsAPI, error) {
		return fakeDarwinGraphics(cgRect{Size: cgSize{Width: 3, Height: 2}}, counters), nil
	}
	t.Cleanup(func() { openDarwinGraphics = previous })

	width, height := GetScaleSize()
	if width != 6 || height != 4 {
		t.Fatalf("GetScaleSize() = %dx%d, want 6x4 Retina pixels", width, height)
	}
	if counters.modeReleaseCalls != 1 || counters.closeCalls != 2 {
		t.Fatalf("resource counters = %+v, want one mode release and two framework closes", counters)
	}
}

func TestPureGoDarwinDisplayScaleRuntime(t *testing.T) {
	api, err := openDarwinCoreGraphics()
	if err != nil {
		t.Fatalf("open CoreGraphics: %v", err)
	}
	defer func() {
		if err := api.close(); err != nil {
			t.Errorf("close CoreGraphics: %v", err)
		}
	}()

	scale, err := darwinDisplayScale(api)
	if err != nil {
		t.Fatalf("query main-display scale: %v", err)
	}
	if !(scale > 0) {
		t.Fatalf("main-display scale = %v, want positive factor", scale)
	}
	if publicScale := SysScale(); math.Abs(publicScale-scale) > 1e-9 {
		t.Fatalf("SysScale() = %v, direct CoreGraphics scale = %v", publicScale, scale)
	}
	logicalWidth, logicalHeight := GetScreenSize()
	scaledWidth, scaledHeight := GetScaleSize()
	if logicalWidth <= 0 || logicalHeight <= 0 || scaledWidth <= 0 || scaledHeight <= 0 {
		t.Fatalf(
			"display sizes must be positive: logical=%dx%d scaled=%dx%d",
			logicalWidth,
			logicalHeight,
			scaledWidth,
			scaledHeight,
		)
	}
	if scaledWidth != int(float64(logicalWidth)*scale) ||
		scaledHeight != int(float64(logicalHeight)*scale) {
		t.Fatalf(
			"GetScaleSize() = %dx%d, logical=%dx%d scale=%v",
			scaledWidth,
			scaledHeight,
			logicalWidth,
			logicalHeight,
			scale,
		)
	}
}

func TestPureGoDarwinCaptureEntryPoint(t *testing.T) {
	previous := openDarwinGraphics
	counters := &darwinGraphicsCounters{}
	openDarwinGraphics = func() (*darwinGraphicsAPI, error) {
		return fakeDarwinGraphics(cgRect{Size: cgSize{Width: 3, Height: 2}}, counters), nil
	}
	t.Cleanup(func() { openDarwinGraphics = previous })

	img, err := Capture()
	if err != nil {
		t.Fatalf("Capture error: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatalf("Capture bounds = %v, want 3x2", img.Bounds())
	}
	if counters.closeCalls != 2 {
		t.Fatalf("CoreGraphics close calls = %d, want bounds and capture handles closed", counters.closeCalls)
	}
}

func TestPureGoDarwinCapabilitiesSeparateBoundsFromPermission(t *testing.T) {
	previous := openDarwinGraphics
	counters := &darwinGraphicsCounters{}
	openDarwinGraphics = func() (*darwinGraphicsAPI, error) {
		api := fakeDarwinGraphics(cgRect{Size: cgSize{Width: 3, Height: 2}}, counters)
		api.preflightCaptureAccess = func() bool { return false }
		return api, nil
	}
	t.Cleanup(func() { openDarwinGraphics = previous })

	capture, bounds := pureGoPlatformCaptureCapabilities()
	if capture.Available || capture.Backend != featureBackendPureGoCoreGraphics || capture.Reason != ErrPermissionDenied.Error() {
		t.Fatalf("capture capability = %+v, want explicit permission denial", capture)
	}
	if !bounds.Available || bounds.Backend != featureBackendPureGoCoreGraphics {
		t.Fatalf("bounds capability = %+v, want available CoreGraphics bounds", bounds)
	}
	if counters.closeCalls != 1 {
		t.Fatalf("CoreGraphics close calls = %d, want 1", counters.closeCalls)
	}
}

func BenchmarkDarwinCapturePipeline(b *testing.B) {
	counters := &darwinGraphicsCounters{}
	api := fakeDarwinGraphics(cgRect{}, counters)
	region := image.Rect(0, 0, 640, 480)
	b.ReportAllocs()
	for range b.N {
		if _, err := captureDarwinWithAPI(api, region); err != nil {
			b.Fatal(err)
		}
	}
}
