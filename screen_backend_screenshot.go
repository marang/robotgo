//go:build cgo || !darwin

package robotgo

import (
	"image"

	"github.com/vcaesar/screenshot"
)

func platformDisplayBounds(displayIndex int) image.Rectangle {
	return screenshot.GetDisplayBounds(displayIndex)
}

func platformCapture(x, y, width, height int) (*image.RGBA, error) {
	return screenshot.Capture(x, y, width, height)
}
