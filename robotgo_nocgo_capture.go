//go:build !cgo

package robotgo

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"

	portalpkg "github.com/marang/robotgo/screen/portal"
	"github.com/vcaesar/screenshot"
)

const (
	envCaptureDebug            = "ROBOTGO_CAPTURE_DEBUG"
	envWaylandBackend          = "ROBOTGO_WAYLAND_BACKEND"
	envForcePortal             = "ROBOTGO_FORCE_PORTAL"
	envDisablePortal           = "ROBOTGO_DISABLE_PORTAL"
	envXDGSessionType          = "XDG_SESSION_TYPE"
	sessionTypeWayland         = "wayland"
	waylandBackendPortalName   = "portal"
	waylandBackendScreenCast   = "screencast"
	capabilityBackendPureGoX11 = "pure-go-x11"
)

var (
	pureGoCaptureImage = func(args ...int) (image.Image, error) {
		return Capture(args...)
	}
	pureGoPortalCaptureImage = portalpkg.CaptureRegionImage
	pureGoPortalAvailable    = portalpkg.Available

	pureGoCaptureBackendMu sync.RWMutex
	pureGoCaptureBackend   CaptureBackend
)

// LastBackend reports the backend used by the latest successful capture.
func LastBackend() CaptureBackend {
	pureGoCaptureBackendMu.RLock()
	defer pureGoCaptureBackendMu.RUnlock()
	return pureGoCaptureBackend
}

func setPureGoCaptureBackend(backend CaptureBackend) {
	pureGoCaptureBackendMu.Lock()
	pureGoCaptureBackend = backend
	pureGoCaptureBackendMu.Unlock()
}

func captureDebugf(format string, args ...interface{}) {
	if os.Getenv(envCaptureDebug) != "" {
		log.Printf("robotgo capture: "+format, args...)
	}
}

// CaptureImg captures an image through a backend that does not require CGO.
// Linux Wayland sessions use the screenshot portal; Linux X11 and Windows use
// the Pure-Go screenshot backend. Unsupported targets return ErrNotSupported.
func CaptureImg(args ...int) (image.Image, error) {
	if err := validateCaptureArguments(args); err != nil {
		return nil, err
	}
	if runtime.GOOS == "linux" {
		backendOverride := pureGoWaylandBackendOverride()
		if backendOverride == waylandBackendScreenCast {
			return nil, fmt.Errorf("%w: persistent ScreenCast capture requires a CGO PipeWire backend", ErrNotSupported)
		}
		if pureGoPortalForced(backendOverride) {
			return capturePureGoPortal(args, "forced")
		}
		switch DetectDisplayServer() {
		case DisplayServerWayland:
			if backendOverride != "" && backendOverride != "auto" {
				captureDebugf("Pure-Go build cannot use requested %s backend; falling back to screenshot portal", backendOverride)
			}
			return capturePureGoPortal(args, "Wayland")
		case DisplayServerX11:
			// Continue with the Pure-Go X11 backend below.
		default:
			return nil, fmt.Errorf("%w: no supported Linux display server detected", ErrNotSupported)
		}
	}
	if !pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH) {
		return nil, fmt.Errorf("%w: Pure-Go capture is unavailable on %s/%s", ErrNotSupported, runtime.GOOS, runtime.GOARCH)
	}
	backend := pureGoScreenshotBackend(runtime.GOOS)
	if backend == BackendX11 && pureGoX11EnvironmentConflict() {
		return nil, fmt.Errorf(
			"%w: the X11 backend is selected but %s selects Wayland; refusing the screenshot dependency's implicit portal fallback",
			ErrNotSupported, envXDGSessionType,
		)
	}

	img, err := pureGoCaptureImage(args...)
	if err != nil {
		if errors.Is(err, screenshot.ErrUnsupported) {
			return nil, fmt.Errorf("%w: Pure-Go capture on %s via %s: %w", ErrNotSupported, runtime.GOOS, backend, err)
		}
		return nil, fmt.Errorf("Pure-Go capture on %s via %s: %w", runtime.GOOS, backend, err)
	}
	if img == nil || img.Bounds().Empty() {
		return nil, errors.New("Pure-Go capture returned an empty image")
	}
	setPureGoCaptureBackend(backend)
	captureDebugf("Pure-Go capture used %s backend", backend)
	return img, nil
}

func pureGoWaylandBackendOverride() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv(envWaylandBackend)))
}

func pureGoPortalForced(backendOverride string) bool {
	return os.Getenv(envForcePortal) != "" || backendOverride == waylandBackendPortalName
}

func capturePureGoPortal(args []int, selection string) (image.Image, error) {
	if os.Getenv(envDisablePortal) != "" {
		return nil, fmt.Errorf("%w: screenshot portal disabled by %s", ErrNotSupported, envDisablePortal)
	}
	x, y, width, height, err := captureRegionFromArgs(args)
	if err != nil {
		return nil, errors.Join(ErrPortalFailed, err)
	}
	img, err := pureGoPortalCaptureImage(context.Background(), x, y, width, height)
	if err != nil {
		return nil, errors.Join(ErrPortalFailed, err)
	}
	img, err = zeroOriginCaptureImage(img)
	if err != nil {
		return nil, errors.Join(ErrPortalFailed, err)
	}
	setPureGoCaptureBackend(BackendPortal)
	captureDebugf("%s screenshot portal capture used (rect=%d,%d %dx%d)", selection, x, y, width, height)
	return img, nil
}

func pureGoX11EnvironmentConflict() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envXDGSessionType)), sessionTypeWayland)
}

func pureGoScreenshotSupported(goos, goarch string) bool {
	if goarch == "s390x" || goarch == "ppc64le" {
		return false
	}
	switch goos {
	case "windows", "linux", "freebsd", "openbsd", "netbsd":
		return true
	default:
		return false
	}
}

func pureGoScreenshotBackend(goos string) CaptureBackend {
	switch goos {
	case "linux", "freebsd", "openbsd", "netbsd":
		return BackendX11
	default:
		return BackendPureGo
	}
}

// CaptureScreen captures a Pure-Go image and converts it to the compatibility
// bitmap representation owned by Go memory.
func CaptureScreen(args ...int) (CBitmap, error) {
	img, err := CaptureImg(args...)
	if err != nil {
		return nil, err
	}
	bitmap, err := ImgToCBitmapE(img)
	if err != nil {
		return nil, fmt.Errorf("convert Pure-Go capture to bitmap: %w", err)
	}
	return bitmap, nil
}

// CaptureGo captures the screen into a Go-owned bitmap without CGO.
func CaptureGo(args ...int) (Bitmap, error) {
	bitmap, err := CaptureScreen(args...)
	if err != nil {
		return Bitmap{}, err
	}
	return ToBitmap(bitmap), nil
}

func zeroOriginCaptureImage(img image.Image) (image.Image, error) {
	if img == nil || img.Bounds().Empty() {
		return nil, errors.New("Pure-Go capture returned an empty image")
	}
	bounds := img.Bounds()
	if bounds.Min == (image.Point{}) {
		return img, nil
	}
	result := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(result, result.Bounds(), img, bounds.Min, draw.Src)
	return result, nil
}
