package robotgo

import (
	"fmt"
	"image"
)

func invalidDisplayIndexError(displayIndex int) error {
	return fmt.Errorf("robotgo: display index must be non-negative, got %d", displayIndex)
}

func validateDisplayRectangle(bounds image.Rectangle) error {
	if bounds.Empty() {
		return fmt.Errorf("robotgo: display bounds are unavailable or empty")
	}
	return nil
}

// GetScreenSizeE returns the selected screen size and reports unavailable or
// empty backend results explicitly.
func GetScreenSizeE() (width, height int, err error) {
	if err := pureGoWaylandBoundsError(); err != nil {
		return 0, 0, err
	}
	if width, height, err, handled := pureGoWaylandScreenSizeE(); handled {
		return width, height, err
	}
	width, height = GetScreenSize()
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("robotgo: screen size is unavailable or empty")
	}
	return width, height, nil
}

// GetScreenRectE returns a screen rectangle and reports unavailable or empty
// backend results explicitly.
func GetScreenRectE(displayID ...int) (Rect, error) {
	if len(displayID) > 1 {
		return Rect{}, fmt.Errorf(
			"robotgo: screen rectangle accepts at most one display index, got %d",
			len(displayID),
		)
	}
	if len(displayID) == 1 && displayID[0] < -1 {
		return Rect{}, fmt.Errorf(
			"robotgo: screen display identifier must be -1 or non-negative, got %d",
			displayID[0],
		)
	}
	if err := pureGoWaylandBoundsError(); err != nil {
		return Rect{}, err
	}
	if rect, err, handled := pureGoWaylandScreenRectE(displayID...); handled {
		return rect, err
	}
	rect := GetScreenRect(displayID...)
	if rect.W <= 0 || rect.H <= 0 {
		return Rect{}, fmt.Errorf("robotgo: screen rectangle is unavailable or empty")
	}
	return rect, nil
}

// DisplaysNumE returns the active display count and reports an unavailable
// display-enumeration backend explicitly.
func DisplaysNumE() (int, error) {
	if err := pureGoWaylandBoundsError(); err != nil {
		return 0, err
	}
	if count, err, handled := pureGoWaylandDisplaysNumE(); handled {
		return count, err
	}
	count := DisplaysNum()
	if count <= 0 {
		return 0, fmt.Errorf("robotgo: active display count is unavailable")
	}
	return count, nil
}
