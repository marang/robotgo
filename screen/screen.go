package screen

import robotgo "github.com/marang/robotgo"

// CaptureScreen proxies to robotgo.CaptureScreen, exposing screen capture
// functionality to subpackages without importing cgo details.
func CaptureScreen(args ...int) (robotgo.CBitmap, error) {
	return robotgo.CaptureScreen(args...)
}

// GetPixelColor proxies to robotgo.GetPixelColor.
func GetPixelColor(x, y int, displayId ...int) (string, error) {
	return robotgo.GetPixelColor(x, y, displayId...)
}
