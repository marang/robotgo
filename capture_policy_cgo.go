//go:build cgo

package robotgo

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"runtime"
	"strings"
	"time"

	portalpkg "github.com/marang/robotgo/screen/portal"
)

func captureViaPortalScreenshot(x, y, w, h int32) (CBitmap, error) {
	if os.Getenv(envDisablePortal) != "" {
		return nil, ErrPortalFailed
	}
	img, err := portalpkg.CaptureRegionImage(context.Background(), int(x), int(y), int(w), int(h))
	if err != nil {
		captureDebugf("portal screenshot failed: %v", err)
		return nil, err
	}
	if img == nil {
		return nil, errors.New("portal screenshot returned nil image")
	}
	cb := ImgToCBitmap(img)
	if cb == nil {
		return nil, errors.New("portal screenshot conversion failed")
	}
	setLastBackend(BackendPortal)
	return cb, nil
}

func captureViaPersistentScreenCast(x, y, w, h int32) (CBitmap, error) {
	if os.Getenv(envDisablePortal) != "" {
		return nil, fmt.Errorf("%w: persistent ScreenCast disabled by %s", ErrPortalFailed, envDisablePortal)
	}
	img, err := captureViaScreenCast(context.Background(), int(x), int(y), int(w), int(h))
	if err != nil {
		captureDebugf("persistent ScreenCast failed: %v", err)
		return nil, err
	}
	bitmap := ImgToCBitmap(img)
	if bitmap == nil {
		return nil, errors.New("ScreenCast frame conversion failed")
	}
	setLastBackend(BackendScreenCast)
	return bitmap, nil
}

func captureScreen(allowPortal bool, args ...int) (CBitmap, error) {
	argX, argY, argW, argH, argErr := captureRegionFromArgs(args)
	if argErr != nil {
		return nil, argErr
	}
	var x, y, w, h int32
	displayId := -1
	if configured := currentDisplayID(); configured != -1 {
		displayId = configured
	}

	if len(args) > 4 {
		displayId = args[4]
	}

	ds := selectedDisplayServer()

	if len(args) > 3 {
		x = int32(argX)
		y = int32(argY)
		w = int32(argW)
		h = int32(argH)
	} else if runtime.GOOS != "linux" {
		// Get the main screen rect on non-Linux platforms. Linux resolves X11
		// bounds while holding the native display lease in the X11 branch below;
		// Wayland and portal backends accept an empty rectangle as full-screen.
		rect := getScreenRectLocked(displayId, false)
		if err := validateCaptureRegionRequest(rect.X, rect.Y, rect.W, rect.H); err != nil {
			return nil, err
		}
		if runtime.GOOS == "windows" {
			x = int32(rect.X)
			y = int32(rect.Y)
		}

		w = int32(rect.W)
		h = int32(rect.H)
	}

	isPid := 0
	if currentTreatAsHandle() || len(args) > 5 {
		isPid = 1
	}

	if runtime.GOOS == "linux" {
		// Allow tests or environments to force the portal backend regardless
		// of the detected display server.
		backendOverride := strings.ToLower(strings.TrimSpace(os.Getenv(envWaylandBackend)))
		forcePortal := allowPortal && (os.Getenv(envForcePortal) != "" || backendOverride == waylandBackendPortalName)
		if len(args) <= 3 && ds == DisplayServerX11 &&
			(forcePortal || allowPortal && backendOverride == waylandBackendScreenCast) {
			// Preserve argumentless X11 crop semantics without holding the native
			// display lease during portal or PipeWire I/O.
			unlockDisplay := lockNativeX11Display()
			if x11MainDisplayAvailableLocked() {
				rect := getScreenRectLocked(displayId, false)
				if validateCaptureRegionRequest(rect.X, rect.Y, rect.W, rect.H) == nil {
					x = int32(rect.X)
					y = int32(rect.Y)
					w = int32(rect.W)
					h = int32(rect.H)
				}
			}
			unlockDisplay()
		}
		if allowPortal && backendOverride == waylandBackendScreenCast {
			bitmap, err := captureViaPersistentScreenCast(x, y, w, h)
			if err != nil {
				return nil, errors.Join(ErrPortalFailed, err)
			}
			captureDebugf("forced persistent ScreenCast backend (rect=%d,%d %dx%d)", int(x), int(y), int(w), int(h))
			return bitmap, nil
		}
		if forcePortal {
			if cb, pErr := captureViaPortalScreenshot(x, y, w, h); pErr == nil {
				captureDebugf("forced portal screenshot backend (display=%d, rect=%d,%d %dx%d)", displayId, int(x), int(y), int(w), int(h))
				return cb, nil
			} else if portalStubEnabled() {
				cb, sErr := captureViaPortalStub(x, y, w, h, displayId, isPid)
				if sErr != nil {
					return nil, fmt.Errorf("%w: %v", ErrPortalFailed, sErr)
				}
				captureDebugf("forced portal stub backend (display=%d, rect=%d,%d %dx%d)", displayId, int(x), int(y), int(w), int(h))
				return cb, nil
			}
			return nil, ErrPortalFailed
		}
		switch ds {
		case DisplayServerWayland:
			backend := selectedWaylandBackend()
			if envBackend, ok := waylandBackendFromEnv(); ok {
				backend = envBackend
			}
			bit, cerr := nativeCaptureWayland(x, y, w, h, displayId, isPid, backend)
			if bit == nil {
				err := waylandErr(cerr)
				captureDebugf("wayland screencopy failed: %v", err)
				var sErr error
				if allowPortal {
					if cb, streamErr := captureViaPersistentScreenCast(x, y, w, h); streamErr == nil {
						captureDebugf("fallback to persistent ScreenCast backend")
						return cb, nil
					}
					// Try portal screenshot (real pixels) when available.
					if cb, pErr := captureViaPortalScreenshot(x, y, w, h); pErr == nil {
						captureDebugf("fallback to portal screenshot backend")
						return cb, nil
					}
					// Optional fallback to C portal stub for tests only.
					if portalStubEnabled() {
						var cb CBitmap
						cb, sErr = captureViaPortalStub(x, y, w, h, displayId, isPid)
						if sErr == nil {
							captureDebugf("fallback to C portal stub backend")
							return cb, nil
						}
					}
				}
				if errors.Is(err, ErrNoScreencopy) {
					if !allowPortal {
						return nil, err
					}
					if sErr != nil {
						return nil, fmt.Errorf("%w; %v", err, sErr)
					}
					return nil, fmt.Errorf("%w; %w", err, ErrPortalFailed)
				}
				return nil, err
			}
			setLastBackend(BackendScreencopy)
			return bit, nil
		case DisplayServerX11:
			// The shared X11 connection is borrowed by bounds and capture. Keep
			// the lease scoped to those native operations; portal, ScreenCast,
			// and Wayland I/O above never run while this mutex is held.
			unlockDisplay := lockNativeX11Display()
			defer unlockDisplay()
			if !x11MainDisplayAvailableLocked() {
				return nil, errors.New("no display server found")
			}
			if len(args) <= 3 {
				rect := getScreenRectLocked(displayId, false)
				if err := validateCaptureRegionRequest(rect.X, rect.Y, rect.W, rect.H); err != nil {
					return nil, err
				}
				x = int32(rect.X)
				y = int32(rect.Y)
				w = int32(rect.W)
				h = int32(rect.H)
			}
			bit := nativeCaptureScreen(x, y, w, h, displayId, isPid)
			if bit == nil {
				return nil, errors.New("screen capture failed")
			}
			setLastBackend(BackendX11)
			return bit, nil
		default:
			return nil, errors.New("no display server found")
		}
	}

	bit := nativeCaptureScreen(x, y, w, h, displayId, isPid)
	if bit == nil {
		return nil, errors.New("screen capture failed")
	}
	setLastBackend(BackendNone)
	return bit, nil
}

func finishNativeContextCapture(ctx context.Context, img image.Image) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		wipeCaptureImage(img)
		return nil, err
	}
	return img, nil
}

func captureScreenWaylandNativeContext(ctx context.Context, args ...int) (CBitmap, error) {
	argX, argY, argW, argH, argErr := captureRegionFromArgs(args)
	if argErr != nil {
		return nil, argErr
	}
	var x, y, w, h int32
	displayID := currentDisplayID()
	if len(args) > 4 {
		displayID = args[4]
	}
	if len(args) > 3 {
		x, y = int32(argX), int32(argY)
		w, h = int32(argW), int32(argH)
	}
	isPID := 0
	if currentTreatAsHandle() || len(args) > 5 {
		isPID = 1
	}
	backend := selectedWaylandBackend()
	if envBackend, ok := waylandBackendFromEnv(); ok {
		backend = envBackend
	}
	timeout := 2 * time.Second
	contextControlsTimeout := false
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, context.DeadlineExceeded
		}
		if remaining < timeout {
			timeout = remaining
			contextControlsTimeout = true
		}
	}
	timeoutMillis := max(int64(1), int64((timeout+time.Millisecond-1)/time.Millisecond))
	bit, captureErr := nativeCaptureWaylandTimeout(x, y, w, h, displayID, isPID, backend, timeoutMillis)
	if bit == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if nativeCaptureTimedOut(captureErr) {
			if contextControlsTimeout {
				return nil, context.DeadlineExceeded
			}
			return nil, captureTimeoutError{backend: BackendScreencopy}
		}
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return nil, context.DeadlineExceeded
		}
		return nil, waylandErr(captureErr)
	}
	setLastBackend(BackendScreencopy)
	return bit, nil
}
