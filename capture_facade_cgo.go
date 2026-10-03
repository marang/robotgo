//go:build cgo

package robotgo

import (
	"context"
	"image"
	"runtime"
)

// CaptureScreen capture the screen and return a bitmap (C struct).
// Use `defer robotgo.FreeBitmap(bitmap)` to free the bitmap.
//
// robotgo.CaptureScreen(x, y, w, h int)
func CaptureScreen(args ...int) (CBitmap, error) {
	return captureScreen(true, args...)
}

// CaptureGo capture the screen and return a Go bitmap.
func CaptureGo(args ...int) (Bitmap, error) {
	bit, err := CaptureScreen(args...)
	if err != nil {
		return Bitmap{}, err
	}
	defer FreeBitmap(bit)

	return ownedBitmapFromC(bit)
}

// CaptureImg capture the screen and return image.Image, error
func CaptureImg(args ...int) (image.Image, error) {
	if img, handled, err := platformCaptureImgFallback(args...); handled {
		return img, err
	}
	bit, err := CaptureScreen(args...)
	if err != nil {
		return nil, err
	}
	defer FreeBitmap(bit)

	return ToImage(bit), nil
}

// CaptureImgNative captures through the session's native backend without
// opening or reusing a desktop portal. On Wayland this attempts compositor
// screencopy only; callers may choose an already-authorized portal fallback
// explicitly after inspecting the returned error.
func CaptureImgNative(args ...int) (image.Image, error) {
	if img, handled, err := platformCaptureImgNativeFallback(args...); handled {
		return img, err
	}
	bit, err := captureScreen(false, args...)
	if err != nil {
		return nil, err
	}
	defer FreeBitmap(bit)

	return ToImage(bit), nil
}

// CaptureImgNativeContext captures through the native backend while honoring
// a context deadline for native Wayland screencopy dispatch. It never opens or
// reuses a desktop portal.
func CaptureImgNativeContext(ctx context.Context, args ...int) (image.Image, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runtime.GOOS != "linux" || selectedDisplayServer() != DisplayServerWayland {
		return CaptureImgNative(args...)
	}

	bit, err := captureScreenWaylandNativeContext(ctx, args...)
	if err != nil {
		return nil, err
	}
	defer FreeBitmap(bit)
	img := ToImage(bit)
	return finishNativeContextCapture(ctx, img)
}
