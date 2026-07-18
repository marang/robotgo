//go:build linux

package robotgo

import (
	"fmt"
	"image"
)

type linuxDisplayBoundsBackend func(int) (image.Rectangle, error)
type linuxCaptureBackend func(int, int, int, int) (*image.RGBA, error)

func dispatchLinuxDisplayBounds(
	server DisplayServer,
	displayIndex int,
	wayland linuxDisplayBoundsBackend,
	x11 linuxDisplayBoundsBackend,
) (image.Rectangle, error) {
	var backend linuxDisplayBoundsBackend
	switch server {
	case DisplayServerWayland:
		backend = wayland
	case DisplayServerX11:
		backend = x11
	default:
		return image.Rectangle{}, fmt.Errorf(
			"%w: no supported Linux display server selected",
			ErrNotSupported,
		)
	}
	if backend == nil {
		return image.Rectangle{}, fmt.Errorf(
			"%w: display bounds are unavailable for %s",
			ErrNotSupported,
			server,
		)
	}
	bounds, err := backend(displayIndex)
	if err != nil {
		return image.Rectangle{}, err
	}
	if err := validateDisplayRectangle(bounds); err != nil {
		return image.Rectangle{}, fmt.Errorf("%s display %d: %w", server, displayIndex, err)
	}
	return bounds, nil
}

func dispatchLinuxCapture(
	server DisplayServer,
	x, y, width, height int,
	wayland linuxCaptureBackend,
	x11 linuxCaptureBackend,
) (*image.RGBA, error) {
	var backend linuxCaptureBackend
	switch server {
	case DisplayServerWayland:
		backend = wayland
	case DisplayServerX11:
		backend = x11
	default:
		return nil, fmt.Errorf(
			"%w: no supported Linux display server selected",
			ErrNotSupported,
		)
	}
	if backend == nil {
		return nil, fmt.Errorf(
			"%w: capture is unavailable for %s",
			ErrNotSupported,
			server,
		)
	}
	img, err := backend(x, y, width, height)
	if err != nil {
		return nil, err
	}
	if img == nil || img.Bounds().Empty() {
		return nil, fmt.Errorf("%s capture returned an empty image", server)
	}
	return img, nil
}
