package robotgo

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// Test that image conversions round-trip correctly.
func TestBitmapRoundTrip(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 4})

	bmp := RGBAToBitmap(src)
	got := ToRGBAGo(bmp)
	if !bytes.Equal(src.Pix, got.Pix) {
		t.Fatalf("RGBAToBitmap/ToRGBAGo mismatch: %v vs %v", src.Pix, got.Pix)
	}

	bmp2 := ImgToBitmap(src)
	got2 := ToRGBAGo(bmp2)
	if !bytes.Equal(src.Pix, got2.Pix) {
		t.Fatalf("ImgToBitmap/ToRGBAGo mismatch: %v vs %v", src.Pix, got2.Pix)
	}
}
