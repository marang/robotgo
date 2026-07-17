//go:build cgo

package robotgo

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestOwnedBitmapFromCStaysValidAfterFree(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 21, G: 22, B: 23, A: 255})

	cbit := ToCBitmap(RGBAToBitmap(src))
	owned, err := ownedBitmapFromC(cbit)
	FreeBitmap(cbit)
	if err != nil {
		t.Fatalf("ownedBitmapFromC error: %v", err)
	}

	got := ToRGBAGo(owned)
	if !bytes.Equal(src.Pix, got.Pix) {
		t.Fatalf("owned bitmap mismatch after FreeBitmap: %v vs %v", src.Pix, got.Pix)
	}
	if _, err := ToStrBitmap(owned); err != nil {
		t.Fatalf("ToStrBitmap on owned bitmap after FreeBitmap error: %v", err)
	}
}
