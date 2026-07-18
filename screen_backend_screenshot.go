//go:build (cgo && !linux) || (!cgo && !darwin && !linux)

package robotgo

import (
	"fmt"
	"image"

	"github.com/vcaesar/screenshot"
)

func platformDisplayBoundsE(displayIndex int) (image.Rectangle, error) {
	bounds := screenshot.GetDisplayBounds(displayIndex)
	if bounds.Empty() {
		return image.Rectangle{}, fmt.Errorf(
			"robotgo: display %d bounds are unavailable",
			displayIndex,
		)
	}
	return bounds, nil
}

func platformCapture(x, y, width, height int) (*image.RGBA, error) {
	return screenshot.Capture(x, y, width, height)
}
