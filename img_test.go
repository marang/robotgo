package robotgo

import (
	"bytes"
	"image"
	"image/color"
	"math"
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

func TestBitmapStringRoundTripAndFind(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	src.SetRGBA(1, 0, color.RGBA{R: 4, G: 5, B: 6, A: 255})
	src.SetRGBA(2, 0, color.RGBA{R: 7, G: 8, B: 9, A: 255})
	src.SetRGBA(0, 1, color.RGBA{R: 10, G: 11, B: 12, A: 255})
	src.SetRGBA(1, 1, color.RGBA{R: 13, G: 14, B: 15, A: 255})
	src.SetRGBA(2, 1, color.RGBA{R: 16, G: 17, B: 18, A: 255})

	str, err := ToStrBitmap(RGBAToBitmap(src))
	if err != nil {
		t.Fatalf("ToStrBitmap error: %v", err)
	}
	bmp, err := BitmapFromStr(str)
	if err != nil {
		t.Fatalf("BitmapFromStr error: %v", err)
	}
	got := ToRGBAGo(bmp)
	if !bytes.Equal(src.Pix, got.Pix) {
		t.Fatalf("BitmapFromStr/ToRGBAGo mismatch: %v vs %v", src.Pix, got.Pix)
	}

	needle := image.NewRGBA(image.Rect(0, 0, 2, 1))
	needle.SetRGBA(0, 0, color.RGBA{R: 13, G: 14, B: 15, A: 255})
	needle.SetRGBA(1, 0, color.RGBA{R: 16, G: 17, B: 18, A: 255})
	needleStr, err := ToStrBitmap(RGBAToBitmap(needle))
	if err != nil {
		t.Fatalf("ToStrBitmap needle error: %v", err)
	}
	x, y, err := FindBitmapStr(needleStr, str)
	if err != nil {
		t.Fatalf("FindBitmapStr error: %v", err)
	}
	if x != 1 || y != 1 {
		t.Fatalf("FindBitmapStr got (%d,%d), want (1,1)", x, y)
	}
}

func TestFindColorCSSignatureOrder(t *testing.T) {
	t.Parallel()

	// Compile-time compatibility check for the documented
	// FindColorCS(x, y, w, h int, color CHex) argument order.
	acceptFindColorCS := func(func(int, int, int, int, CHex, ...float64) (int, int, error)) {}
	acceptFindColorCS(FindColorCS)
	acceptFindColorCS(FindcolorCS)
}

func TestBitmapStringInvalid(t *testing.T) {
	t.Parallel()

	if _, err := BitmapFromStr("not-json"); err == nil {
		t.Fatalf("expected invalid bitmap string error")
	}
	if _, err := ToStrBitmap(Bitmap{}); err == nil {
		t.Fatalf("expected invalid bitmap error")
	}
}

func TestBitmapStringRejectsUnsupportedRGBSearchLayout(t *testing.T) {
	t.Parallel()

	twoByteBitmap, err := NewBitmap([]byte{1, 2, 3, 4}, 2, 1, 4, 16, 2)
	if err != nil {
		t.Fatalf("NewBitmap error: %v", err)
	}
	serialized, err := ToStrBitmap(twoByteBitmap)
	if err != nil {
		t.Fatalf("ToStrBitmap error: %v", err)
	}
	if _, _, err := FindBitmapStr(serialized, serialized); err == nil {
		t.Fatal("FindBitmapStr unexpectedly accepted a two-byte pixel layout")
	}
}

func TestFindBitmapStrRejectsMultipleHaystacks(t *testing.T) {
	t.Parallel()

	bitmap, err := NewBitmap([]byte{1, 2, 3}, 1, 1, 3, 24, 3)
	if err != nil {
		t.Fatalf("NewBitmap error: %v", err)
	}
	serialized, err := ToStrBitmap(bitmap)
	if err != nil {
		t.Fatalf("ToStrBitmap error: %v", err)
	}
	if _, _, err := FindBitmapStr(serialized, serialized, serialized); err == nil {
		t.Fatal("FindBitmapStr unexpectedly accepted multiple haystacks")
	}
}

func TestFindBitmapStrSupportsThreeBytePixelsAndPaddedStride(t *testing.T) {
	t.Parallel()

	haystack, err := NewBitmap(
		[]byte{
			3, 2, 1, 6, 5, 4, 99, 99,
			9, 8, 7, 12, 11, 10, 88, 88,
		},
		2,
		2,
		8,
		24,
		3,
	)
	if err != nil {
		t.Fatalf("NewBitmap haystack error: %v", err)
	}
	needle, err := NewBitmap([]byte{12, 11, 10}, 1, 1, 3, 24, 3)
	if err != nil {
		t.Fatalf("NewBitmap needle error: %v", err)
	}
	haystackStr, err := ToStrBitmap(haystack)
	if err != nil {
		t.Fatalf("ToStrBitmap haystack error: %v", err)
	}
	needleStr, err := ToStrBitmap(needle)
	if err != nil {
		t.Fatalf("ToStrBitmap needle error: %v", err)
	}
	x, y, err := FindBitmapStr(needleStr, haystackStr)
	if err != nil {
		t.Fatalf("FindBitmapStr error: %v", err)
	}
	if x != 1 || y != 1 {
		t.Fatalf("FindBitmapStr = (%d,%d), want (1,1)", x, y)
	}
}

func TestParseColorTolerance(t *testing.T) {
	t.Parallel()

	got, err := parseColorTolerance(nil)
	if err != nil || got != defaultColorTolerance {
		t.Fatalf("parseColorTolerance(nil) = %v, %v; want %v, nil", got, err, defaultColorTolerance)
	}
	for _, value := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := parseColorTolerance([]float64{value}); err == nil {
			t.Fatalf("parseColorTolerance(%v) unexpectedly succeeded", value)
		}
	}
	if _, err := parseColorTolerance([]float64{0, 0}); err == nil {
		t.Fatal("parseColorTolerance unexpectedly accepted multiple values")
	}
}

func TestBitmapColorHelpers(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	bmp := RGBAToBitmap(src)

	r, g, b, ok := bitmapRGBAt(bmp, 0, 0)
	if !ok {
		t.Fatalf("bitmapRGBAt failed")
	}
	if r != 10 || g != 20 || b != 30 {
		t.Fatalf("bitmapRGBAt got %d,%d,%d; want 10,20,30", r, g, b)
	}
	if !rgbSimilar(10, 20, 31, 10, 20, 30, 0.01) {
		t.Fatalf("expected color similarity within tolerance")
	}
}
