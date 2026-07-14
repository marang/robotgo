package robotgo

import "fmt"

const (
	minCaptureCoordinate = -1 << 31
	maxCaptureCoordinate = 1<<31 - 1
	captureBytesPerPixel = 4
)

func validateCaptureArguments(args []int) error {
	if len(args) > 0 && len(args) < 4 {
		return fmt.Errorf("capture requires either zero arguments or at least x, y, width, height; got %d", len(args))
	}
	if len(args) >= 4 {
		return validateCaptureRegionRequest(args[0], args[1], args[2], args[3])
	}
	return nil
}

func captureRegionFromArgs(args []int) (x, y, width, height int, err error) {
	if err := validateCaptureArguments(args); err != nil {
		return 0, 0, 0, 0, err
	}
	if len(args) >= 4 {
		return args[0], args[1], args[2], args[3], nil
	}
	return 0, 0, 0, 0, nil
}

func validateCaptureRegionRequest(x, y, width, height int) error {
	if width == 0 && height == 0 {
		if x != 0 || y != 0 {
			return fmt.Errorf("full-screen capture requires zero origin, got %d,%d", x, y)
		}
		return nil
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid capture region size %dx%d", width, height)
	}
	if x+width < x || y+height < y {
		return fmt.Errorf("capture region overflows integer coordinates: %d,%d %dx%d", x, y, width, height)
	}
	if x < minCaptureCoordinate || y < minCaptureCoordinate ||
		x > maxCaptureCoordinate || y > maxCaptureCoordinate ||
		width > maxCaptureCoordinate || height > maxCaptureCoordinate ||
		x+width > maxCaptureCoordinate || y+height > maxCaptureCoordinate {
		return fmt.Errorf("capture region exceeds backend coordinate range: %d,%d %dx%d", x, y, width, height)
	}
	if width > maxBitmapBufferSize/captureBytesPerPixel ||
		height > maxBitmapBufferSize/(width*captureBytesPerPixel) {
		return fmt.Errorf("capture region buffer exceeds %d bytes: %dx%d", maxBitmapBufferSize, width, height)
	}
	return nil
}
