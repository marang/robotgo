//go:build !cgo && linux

package robotgo

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/vcaesar/screenshot"
)

func platformDisplayCount() int {
	if selectedDisplayServer() != DisplayServerX11 || pureGoX11EnvironmentConflict() {
		return 0
	}
	return screenshot.NumActiveDisplays()
}

func platformDisplayBoundsE(displayIndex int) (image.Rectangle, error) {
	if err := pureGoWaylandBoundsError(); err != nil {
		return image.Rectangle{}, err
	}
	return dispatchLinuxDisplayBounds(
		selectedDisplayServer(),
		displayIndex,
		nil,
		func(index int) (image.Rectangle, error) {
			return screenshot.GetDisplayBounds(index), nil
		},
	)
}

func platformCapture(x, y, width, height int) (*image.RGBA, error) {
	server := selectedDisplayServer()
	backendOverride := pureGoWaylandBackendOverride()
	if backendOverride == waylandBackendScreenCast {
		return nil, fmt.Errorf(
			"%w: persistent ScreenCast capture requires a CGO PipeWire backend",
			ErrNotSupported,
		)
	}
	if pureGoPortalForced(backendOverride) || server == DisplayServerWayland {
		if server == DisplayServerWayland &&
			backendOverride != "" &&
			backendOverride != "auto" &&
			backendOverride != waylandBackendPortalName {
			captureDebugf(
				"Pure-Go build cannot use requested %s backend; falling back to screenshot portal",
				backendOverride,
			)
		}
		selection := "Wayland compatibility"
		if pureGoPortalForced(backendOverride) {
			selection = "forced compatibility"
		}
		img, err := capturePureGoPortal([]int{x, y, width, height}, selection)
		if err != nil {
			return nil, err
		}
		return zeroOriginRGBA(img)
	}
	if server == DisplayServerX11 && pureGoX11EnvironmentConflict() {
		return nil, fmt.Errorf(
			"%w: the X11 backend is selected but %s selects Wayland; refusing implicit Xwayland capture",
			ErrNotSupported,
			envXDGSessionType,
		)
	}
	img, err := dispatchLinuxCapture(
		server,
		x,
		y,
		width,
		height,
		nil,
		screenshot.Capture,
	)
	if err != nil {
		return nil, err
	}
	setPureGoCaptureBackend(BackendX11)
	captureDebugf("Pure-Go capture compatibility helper used %s backend", BackendX11)
	return img, nil
}

func zeroOriginRGBA(img image.Image) (*image.RGBA, error) {
	normalized, err := zeroOriginCaptureImage(img)
	if err != nil {
		return nil, err
	}
	if rgba, ok := normalized.(*image.RGBA); ok {
		return rgba, nil
	}
	bounds := normalized.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), normalized, bounds.Min, draw.Src)
	return rgba, nil
}
