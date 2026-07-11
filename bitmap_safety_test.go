package robotgo

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestBitmapSafeConversionsRejectInvalidInput(t *testing.T) {
	tests := []struct {
		name           string
		bitmap         Bitmap
		invalidCBitmap bool
	}{
		{name: "zero value", bitmap: Bitmap{}, invalidCBitmap: true},
		{name: "nil buffer", bitmap: Bitmap{Width: 1, Height: 1, Bytewidth: 4, BitsPixel: 32, BytesPerPixel: 4}, invalidCBitmap: true},
		{name: "short stride", bitmap: Bitmap{Width: 2, Height: 1, Bytewidth: 4, BitsPixel: 32, BytesPerPixel: 4}, invalidCBitmap: true},
		{name: "unowned pointer", bitmap: Bitmap{ImgBuf: new(uint8), Width: 1, Height: 1, Bytewidth: 3, BitsPixel: 24, BytesPerPixel: 3}, invalidCBitmap: true},
		{name: "overflow", bitmap: Bitmap{ImgBuf: new(uint8), Width: int(^uint(0) >> 1), Height: 2, Bytewidth: int(^uint(0) >> 1), BitsPixel: 32, BytesPerPixel: 4}, invalidCBitmap: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ToRGBAGoE(test.bitmap); err == nil {
				t.Fatal("ToRGBAGoE accepted invalid bitmap")
			}
			cBitmap, err := ToCBitmapE(test.bitmap)
			if test.invalidCBitmap && err == nil {
				t.Fatal("ToCBitmapE accepted invalid bitmap")
			}
			if err == nil {
				FreeBitmap(cBitmap)
			}
		})
	}
	if _, err := ByteToCBitmapE([]byte("not an image")); err == nil {
		t.Fatal("ByteToCBitmapE ignored decode failure")
	}
	if bitmap := ByteToCBitmap([]byte("not an image")); bitmap != nil {
		t.Fatal("legacy ByteToCBitmap returned a bitmap for invalid data")
	}
}

func TestBitmapSafeConversionsRoundTrip(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2, 1))
	source.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 4})
	source.SetRGBA(1, 0, color.RGBA{R: 5, G: 6, B: 7, A: 8})

	cBitmap, err := ImgToCBitmapE(source)
	if err != nil {
		t.Fatalf("ImgToCBitmapE: %v", err)
	}
	defer FreeBitmap(cBitmap)
	result, err := ToRGBAE(cBitmap)
	if err != nil {
		t.Fatalf("ToRGBAE: %v", err)
	}
	if !bytes.Equal(result.Pix, source.Pix) {
		t.Fatalf("round trip pixels = %v, want %v", result.Pix, source.Pix)
	}
}

func TestNewBitmapCopiesCallerData(t *testing.T) {
	data := []byte{3, 2, 1, 4}
	bitmap, err := NewBitmap(data, 1, 1, 4, 32, 4)
	if err != nil {
		t.Fatalf("NewBitmap: %v", err)
	}
	data[0] = 99
	result, err := ToRGBAGoE(bitmap)
	if err != nil {
		t.Fatalf("ToRGBAGoE: %v", err)
	}
	if got := result.RGBAAt(0, 0); got != (color.RGBA{R: 1, G: 2, B: 3, A: 4}) {
		t.Fatalf("converted color = %#v", got)
	}
}

func TestBitmapSearchRejectsUnownedPointers(t *testing.T) {
	unowned := Bitmap{
		ImgBuf: new(uint8), Width: 1, Height: 1, Bytewidth: 4,
		BitsPixel: 32, BytesPerPixel: 4,
	}
	if _, _, err := findBitmap(unowned, unowned); err == nil {
		t.Fatal("findBitmap accepted unowned bitmap pointers")
	}
}

func TestRGBAToBitmapHandlesSubimageStride(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 8, 2))
	source.SetRGBA(7, 1, color.RGBA{R: 9, G: 8, B: 7, A: 6})
	subimage := source.SubImage(image.Rect(7, 1, 8, 2)).(*image.RGBA)

	bitmap, err := RGBAToBitmapE(subimage)
	if err != nil {
		t.Fatalf("RGBAToBitmapE: %v", err)
	}
	if bitmap.Bytewidth != 4 {
		t.Fatalf("bitmap stride = %d, want tightly packed stride 4", bitmap.Bytewidth)
	}
	converted, err := ToRGBAGoE(bitmap)
	if err != nil {
		t.Fatalf("ToRGBAGoE: %v", err)
	}
	if got := converted.RGBAAt(0, 0); got != (color.RGBA{R: 9, G: 8, B: 7, A: 6}) {
		t.Fatalf("converted color = %#v", got)
	}
}
