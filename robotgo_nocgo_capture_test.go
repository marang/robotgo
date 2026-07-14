//go:build !cgo && linux

package robotgo

import (
	"context"
	"errors"
	"image"
	"runtime"
	"strings"
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
	"github.com/vcaesar/screenshot"
)

func preservePureGoCaptureFakes(t *testing.T) {
	t.Helper()
	nativeCapture := pureGoCaptureImage
	portalCapture := pureGoPortalCaptureImage
	portalAvailable := pureGoPortalAvailable
	remoteDesktopProbe := remoteDesktopStatusProbe
	backend := LastBackend()
	t.Setenv(envForcePortal, "")
	t.Setenv(envWaylandBackend, "")
	t.Setenv(envCaptureDebug, "")
	remoteDesktopStatusProbe = func(context.Context) (inputportal.Capability, error) {
		return inputportal.Capability{}, inputportal.ErrUnavailable
	}
	t.Cleanup(func() {
		pureGoCaptureImage = nativeCapture
		pureGoPortalCaptureImage = portalCapture
		pureGoPortalAvailable = portalAvailable
		remoteDesktopStatusProbe = remoteDesktopProbe
		setPureGoCaptureBackend(backend)
	})
}

func TestPureGoScreenshotPlatformSupport(t *testing.T) {
	tests := []struct {
		goos      string
		goarch    string
		supported bool
		backend   CaptureBackend
	}{
		{goos: "windows", goarch: "amd64", supported: true, backend: BackendPureGo},
		{goos: "linux", goarch: "amd64", supported: true, backend: BackendX11},
		{goos: "freebsd", goarch: "amd64", supported: true, backend: BackendX11},
		{goos: "linux", goarch: "s390x", supported: false, backend: BackendX11},
		{goos: "linux", goarch: "ppc64le", supported: false, backend: BackendX11},
		{goos: "darwin", goarch: "amd64", supported: true, backend: BackendPureGo},
		{goos: "darwin", goarch: "arm64", supported: true, backend: BackendPureGo},
	}
	for _, test := range tests {
		t.Run(test.goos+"/"+test.goarch, func(t *testing.T) {
			if got := pureGoScreenshotSupported(test.goos, test.goarch); got != test.supported {
				t.Fatalf("pureGoScreenshotSupported = %v, want %v", got, test.supported)
			}
			if got := pureGoScreenshotBackend(test.goos); got != test.backend {
				t.Fatalf("pureGoScreenshotBackend = %q, want %q", got, test.backend)
			}
		})
	}
}

func TestPureGoX11RejectsImplicitDependencyPortalFallback(t *testing.T) {
	if !pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH) {
		t.Skip("Pure-Go X11 screenshot dependency is unsupported on this architecture")
	}
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":99")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	t.Setenv(envDisablePortal, "1")
	calls := 0
	pureGoCaptureImage = func(...int) (image.Image, error) {
		calls++
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}
	if _, err := CaptureImg(0, 0, 1, 1); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("CaptureImg error = %v, want ErrNotSupported", err)
	}
	if calls != 0 {
		t.Fatalf("screenshot dependency calls = %d, want 0", calls)
	}
	capability := GetLinuxCapabilities().Capture
	if capability.Available || !strings.Contains(capability.Reason, envXDGSessionType) {
		t.Fatalf("conflicting X11 capability = %+v", capability)
	}
}

func TestPureGoLinuxCaptureCapabilities(t *testing.T) {
	preservePureGoCaptureFakes(t)
	t.Run("X11 ready", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", ":99")
		t.Setenv(envXDGSessionType, "x11")
		capture := GetLinuxCapabilities().Capture
		wantAvailable := pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH)
		if capture.Available != wantAvailable || capture.Backend != featureBackendPureGoX11 {
			t.Fatalf("capture capability = %+v", capture)
		}
		bounds := GetLinuxCapabilities().Bounds
		if bounds.Available != wantAvailable || bounds.Backend != featureBackendPureGoX11 {
			t.Fatalf("bounds capability = %+v", bounds)
		}
	})
	t.Run("Wayland portal ready", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "wayland-test")
		t.Setenv("DISPLAY", "")
		t.Setenv(envDisablePortal, "")
		pureGoPortalAvailable = func(context.Context) (bool, error) { return true, nil }
		capture := GetLinuxCapabilities().Capture
		if !capture.Available || capture.Backend != string(BackendPortal) {
			t.Fatalf("capture capability = %+v", capture)
		}
	})
	t.Run("Wayland portal unavailable", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "wayland-test")
		t.Setenv("DISPLAY", "")
		t.Setenv(envDisablePortal, "")
		pureGoPortalAvailable = func(context.Context) (bool, error) { return false, nil }
		capture := GetLinuxCapabilities().Capture
		if capture.Available || capture.Reason != "screenshot portal service is not available" {
			t.Fatalf("capture capability = %+v", capture)
		}
	})
}

func TestPureGoCaptureImgRejectsInvalidPortalRegionBeforeRequest(t *testing.T) {
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")
	t.Setenv(envDisablePortal, "")
	calls := 0
	pureGoPortalCaptureImage = func(context.Context, int, int, int, int) (image.Image, error) {
		calls++
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}
	for _, size := range [][2]int{{0, 2}, {2, 0}, {-1, 2}, {2, -1}} {
		if _, err := CaptureImg(0, 0, size[0], size[1]); err == nil {
			t.Fatalf("CaptureImg size %dx%d unexpectedly succeeded", size[0], size[1])
		}
	}
	maxInt := int(^uint(0) >> 1)
	if _, err := CaptureImg(maxInt, 0, 1, 1); err == nil {
		t.Fatal("overflowing CaptureImg unexpectedly succeeded")
	}
	if _, err := CaptureImg(1, 0, 0, 0); err == nil {
		t.Fatal("non-zero-origin full capture unexpectedly succeeded")
	}
	for argCount := 1; argCount <= 3; argCount++ {
		args := make([]int, argCount)
		if _, err := CaptureImg(args...); err == nil {
			t.Fatalf("CaptureImg with %d arguments unexpectedly succeeded", argCount)
		}
	}
	if calls != 0 {
		t.Fatalf("portal capture calls = %d, want 0", calls)
	}
}

func TestPureGoCaptureHonorsPortalOverrides(t *testing.T) {
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":99")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	t.Setenv(envDisablePortal, "")
	nativeCalls := 0
	portalCalls := 0
	pureGoCaptureImage = func(...int) (image.Image, error) {
		nativeCalls++
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil
	}
	pureGoPortalCaptureImage = func(context.Context, int, int, int, int) (image.Image, error) {
		portalCalls++
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil
	}
	pureGoPortalAvailable = func(context.Context) (bool, error) { return true, nil }

	t.Run("force portal", func(t *testing.T) {
		t.Setenv(envForcePortal, "1")
		if _, err := CaptureImg(0, 0, 2, 2); err != nil {
			t.Fatalf("CaptureImg error: %v", err)
		}
		capability := GetLinuxCapabilities().Capture
		if !capability.Available || capability.Backend != string(BackendPortal) {
			t.Fatalf("forced portal capability = %+v", capability)
		}
	})
	t.Run("portal backend", func(t *testing.T) {
		t.Setenv(envWaylandBackend, waylandBackendPortalName)
		if _, err := CaptureImg(0, 0, 2, 2); err != nil {
			t.Fatalf("CaptureImg error: %v", err)
		}
		capability := GetLinuxCapabilities().Capture
		if !capability.Available || capability.Backend != string(BackendPortal) {
			t.Fatalf("portal override capability = %+v", capability)
		}
	})
	if nativeCalls != 0 || portalCalls != 2 {
		t.Fatalf("capture calls: native=%d portal=%d, want native=0 portal=2", nativeCalls, portalCalls)
	}
	if LastBackend() != BackendPortal {
		t.Fatalf("LastBackend = %q, want %q", LastBackend(), BackendPortal)
	}

	t.Run("screencast unavailable", func(t *testing.T) {
		t.Setenv(envWaylandBackend, waylandBackendScreenCast)
		if _, err := CaptureImg(0, 0, 2, 2); !errors.Is(err, ErrNotSupported) {
			t.Fatalf("CaptureImg error = %v, want ErrNotSupported", err)
		}
		capability := GetLinuxCapabilities().Capture
		if capability.Available || capability.Backend != string(BackendScreenCast) {
			t.Fatalf("ScreenCast override capability = %+v", capability)
		}
	})
	t.Run("disabled forced portal", func(t *testing.T) {
		t.Setenv(envForcePortal, "1")
		t.Setenv(envDisablePortal, "1")
		if _, err := CaptureImg(0, 0, 2, 2); !errors.Is(err, ErrNotSupported) {
			t.Fatalf("CaptureImg error = %v, want ErrNotSupported", err)
		}
		capability := GetLinuxCapabilities().Capture
		if capability.Available || capability.Backend != string(BackendPortal) {
			t.Fatalf("disabled forced portal capability = %+v", capability)
		}
	})
	if nativeCalls != 0 || portalCalls != 2 {
		t.Fatalf("failed override changed capture calls: native=%d portal=%d", nativeCalls, portalCalls)
	}
}

func TestZeroOriginCaptureImageReusesNormalizedImage(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 3, 2))
	got, err := zeroOriginCaptureImage(source)
	if err != nil {
		t.Fatalf("zeroOriginCaptureImage error: %v", err)
	}
	if got != source {
		t.Fatal("zero-origin image was unnecessarily copied")
	}
}

func TestPureGoCaptureImgUsesX11Backend(t *testing.T) {
	if !pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH) {
		t.Skip("Pure-Go X11 screenshot dependency is unsupported on this architecture")
	}
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":99")
	t.Setenv(envXDGSessionType, "x11")
	pureGoCaptureImage = func(args ...int) (image.Image, error) {
		if len(args) != 4 || args[0] != 10 || args[1] != 20 || args[2] != 3 || args[3] != 2 {
			t.Fatalf("capture args = %v", args)
		}
		return image.NewRGBA(image.Rect(0, 0, 3, 2)), nil
	}

	img, err := CaptureImg(10, 20, 3, 2)
	if err != nil {
		t.Fatalf("CaptureImg error: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatalf("capture bounds = %v", img.Bounds())
	}
	if LastBackend() != BackendX11 {
		t.Fatalf("LastBackend = %q, want %q", LastBackend(), BackendX11)
	}
	bitmap, err := CaptureScreen(10, 20, 3, 2)
	if err != nil {
		t.Fatalf("CaptureScreen error: %v", err)
	}
	if bitmap == nil || bitmap.Width != 3 || bitmap.Height != 2 {
		t.Fatalf("bitmap = %+v, want 3x2", bitmap)
	}
	owned, err := CaptureGo(10, 20, 3, 2)
	if err != nil {
		t.Fatalf("CaptureGo error: %v", err)
	}
	if owned.Width != 3 || owned.Height != 2 {
		t.Fatalf("owned bitmap = %+v, want 3x2", owned)
	}
	serialized, err := CaptureBitmapStr(10, 20, 3, 2)
	if err != nil {
		t.Fatalf("CaptureBitmapStr error: %v", err)
	}
	decoded, err := BitmapFromStr(serialized)
	if err != nil {
		t.Fatalf("BitmapFromStr error: %v", err)
	}
	if decoded.Width != 3 || decoded.Height != 2 {
		t.Fatalf("decoded bitmap = %+v, want 3x2", decoded)
	}
}

func TestPureGoCaptureImgUsesHardenedWaylandPortal(t *testing.T) {
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")
	t.Setenv(envDisablePortal, "")
	pureGoCaptureImage = func(...int) (image.Image, error) {
		t.Fatal("legacy screenshot backend called for Wayland")
		return nil, nil
	}
	pureGoPortalCaptureImage = func(_ context.Context, x, y, width, height int) (image.Image, error) {
		if x != -2 || y != 4 || width != 3 || height != 2 {
			t.Fatalf("portal region = %d,%d %dx%d", x, y, width, height)
		}
		return image.NewRGBA(image.Rect(7, 9, 10, 11)), nil
	}

	img, err := CaptureImg(-2, 4, 3, 2)
	if err != nil {
		t.Fatalf("CaptureImg error: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatalf("normalized capture bounds = %v", img.Bounds())
	}
	if LastBackend() != BackendPortal {
		t.Fatalf("LastBackend = %q, want %q", LastBackend(), BackendPortal)
	}
	owned, err := CaptureGo(-2, 4, 3, 2)
	if err != nil {
		t.Fatalf("CaptureGo error: %v", err)
	}
	if owned.Width != 3 || owned.Height != 2 {
		t.Fatalf("owned portal bitmap = %+v, want 3x2", owned)
	}
}

func TestPureGoCaptureImgReportsExplicitUnsupported(t *testing.T) {
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	if _, err := CaptureImg(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("CaptureImg error = %v, want ErrNotSupported", err)
	}

	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv(envDisablePortal, "1")
	if _, err := CaptureImg(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("disabled portal error = %v, want ErrNotSupported", err)
	}
}

func TestPureGoCaptureImgWrapsPortalFailure(t *testing.T) {
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")
	t.Setenv(envDisablePortal, "")
	wantErr := errors.New("portal denied")
	pureGoPortalCaptureImage = func(context.Context, int, int, int, int) (image.Image, error) {
		return nil, wantErr
	}
	if _, err := CaptureImg(); !errors.Is(err, ErrPortalFailed) || !errors.Is(err, wantErr) {
		t.Fatalf("CaptureImg error = %v, want joined portal failure", err)
	}
}

func TestPureGoCaptureImgRejectsEmptyBackendImage(t *testing.T) {
	if !pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH) {
		t.Skip("Pure-Go X11 screenshot dependency is unsupported on this architecture")
	}
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":99")
	t.Setenv(envXDGSessionType, "x11")
	pureGoCaptureImage = func(...int) (image.Image, error) { return nil, nil }
	if _, err := CaptureImg(); err == nil {
		t.Fatal("CaptureImg unexpectedly accepted a nil backend image")
	}
}

func TestPureGoCaptureImgPreservesPortableBackendErrors(t *testing.T) {
	if !pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH) {
		t.Skip("Pure-Go X11 screenshot dependency is unsupported on this architecture")
	}
	preservePureGoCaptureFakes(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":99")
	t.Setenv(envXDGSessionType, "x11")
	pureGoCaptureImage = func(...int) (image.Image, error) {
		return nil, screenshot.ErrUnsupported
	}
	_, err := CaptureImg(0, 0, 1, 1)
	if !errors.Is(err, ErrNotSupported) || !errors.Is(err, screenshot.ErrUnsupported) {
		t.Fatalf("unsupported error = %v, want both sentinels", err)
	}

	wantErr := errors.New("backend failed")
	pureGoCaptureImage = func(...int) (image.Image, error) { return nil, wantErr }
	_, err = CaptureImg(0, 0, 1, 1)
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), string(BackendX11)) {
		t.Fatalf("backend error = %v, want wrapped error with backend context", err)
	}
}
