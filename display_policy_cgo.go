//go:build cgo

package robotgo

import (
	"runtime"
)

// IsMain is main display
func IsMain(displayId int) bool {
	return displayId == GetMainId()
}

func displayIdx(id ...int) int {
	display := -1
	if configured := currentDisplayID(); configured != -1 {
		display = configured
	}
	if len(id) > 0 {
		display = id[0]
	}

	return display
}

// SysScale get the sys scale
func SysScale(displayId ...int) float64 {
	display := displayIdx(displayId...)
	unlock := lockNativeX11Display()
	defer unlock()
	return sysScaleLocked(display)
}

func scaleFLocked(displayId ...int) float64 {
	f := sysScaleLocked(displayIdx(displayId...))
	if f == 0.0 {
		return 1.0
	}
	return f
}

// Scaled get the screen scaled return scale size
func Scaled(x int, displayId ...int) int {
	f := ScaleF(displayId...)
	return Scaled0(x, f)
}

// Scaled0 return int(x * f)
func Scaled0(x int, f float64) int {
	return int(float64(x) * f)
}

// Scaled1 return int(x / f)
func Scaled1(x int, f float64) int {
	return int(float64(x) / f)
}

// GetScreenSize get the screen size
func GetScreenSize() (int, int) {
	unlock := lockNativeX11Display()
	size := nativeMainDisplaySize()
	unlock()
	w := size.W
	h := size.H
	if w > 0 && h > 0 {
		return w, h
	}
	if rect, ok := waylandPrimaryBoundsFallback(); ok {
		return rect.W, rect.H
	}
	return w, h
}

// GetScreenRect get the screen rect (x, y, w, h)
func GetScreenRect(displayId ...int) Rect {
	display := -1
	if len(displayId) > 0 {
		display = displayId[0]
	}

	unlock := lockNativeX11Display()
	rect := getScreenRectLocked(display, false)
	unlock()
	if display < 0 && (rect.W <= 0 || rect.H <= 0) && isWaylandSession() {
		if wlRect, ok := waylandScreenBoundsFallback(); ok {
			return wlRect
		}
	}
	return rect
}

// getScreenRectLocked returns the native screen rectangle while the caller
// holds the native X11 display lease. The lease is a no-op on platforms that
// do not compile the native X11 backend.
func getScreenRectLocked(display int, waylandSession bool) Rect {
	rect := nativeScreenRect(display)
	x, y, w, h := rect.X, rect.Y, rect.W, rect.H
	if (w <= 0 || h <= 0) && waylandSession {
		if wlRect, ok := waylandScreenBoundsFallback(); ok {
			x, y, w, h = wlRect.X, wlRect.Y, wlRect.W, wlRect.H
		}
	}

	if runtime.GOOS == "windows" {
		// f := ScaleF(displayId...)
		f := ScaleF()
		x, y, w, h = Scaled0(x, f), Scaled0(y, f), Scaled0(w, f), Scaled0(h, f)
	}
	return Rect{
		Point{X: x, Y: y},
		Size{W: w, H: h},
	}
}

// GetScaleSize get the screen scale size
func GetScaleSize(displayId ...int) (int, int) {
	x, y := GetScreenSize()
	f := ScaleF(displayId...)
	return int(float64(x) * f), int(float64(y) * f)
}

// MoveScale calculate the os scale factor x, y
func MoveScale(x, y int, displayId ...int) (int, int) {
	if currentScale() || runtime.GOOS == "windows" {
		f := ScaleF()
		x, y = Scaled1(x, f), Scaled1(y, f)
	}

	return x, y
}

func moveScaleLocked(x, y int, displayId ...int) (int, int) {
	if currentScale() || runtime.GOOS == "windows" {
		f := scaleFLocked(displayId...)
		x, y = Scaled1(x, f), Scaled1(y, f)
	}
	return x, y
}
