//go:build linux && portal
// +build linux,portal

package portal

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"os"
	"unsafe"
)

/*
#include "../../base/bitmap_free_c.h"
#include <stdlib.h>
#include <string.h>
*/
import "C"

// CBitmap mirrors robotgo.CBitmap without importing the root package.
type CBitmap = C.MMBitmapRef

func freeCBitmap(bitmap CBitmap) {
	if bitmap != nil {
		C.destroyMMBitmap(bitmap)
	}
}

func imageToCBitmap(img image.Image) (CBitmap, error) {
	if img == nil {
		return nil, errors.New("nil image")
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, errors.New("invalid image bounds")
	}

	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)

	total := len(rgba.Pix)
	if total <= 0 {
		return nil, errors.New("empty image pixels")
	}
	cbuf := C.malloc(C.size_t(total))
	if cbuf == nil {
		return nil, errors.New("malloc failed")
	}
	C.memcpy(cbuf, unsafe.Pointer(&rgba.Pix[0]), C.size_t(total))

	bit := C.createMMBitmap_c(
		(*C.uint8_t)(cbuf),
		C.int32_t(rgba.Bounds().Dx()),
		C.int32_t(rgba.Bounds().Dy()),
		C.int32_t(rgba.Stride),
		C.uint8_t(32),
		C.uint8_t(4),
	)
	if bit == nil {
		C.free(cbuf)
		return nil, errors.New("createMMBitmap_c failed")
	}

	return bit, nil
}

// Capture captures a screen region using the freedesktop portal screenshot
// API and returns it as MMBitmapRef.
func Capture(ctx context.Context, x, y, w, h int) (CBitmap, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if os.Getenv("ROBOTGO_PORTAL_FAIL") != "" {
		return nil, errors.New("portal capture forced failure")
	}

	img, err := CaptureRegionImage(ctx, x, y, w, h)
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, errors.New("portal screenshot returned nil image")
	}
	return imageToCBitmap(img)
}

// CaptureRegionImage is implemented in screenshot_portal.go.
