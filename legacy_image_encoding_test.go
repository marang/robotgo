package robotgo

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestLegacyImageEncodingRepresentation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	img.Set(1, 0, color.RGBA{B: 150, A: 255})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	// Pinned imgo defaults to JPEG and returns standard base64 in an oversized
	// zero-padded buffer, not the raw encoded image. A later strict alternative
	// must decide representation explicitly rather than accidentally changing it.
	want := make([]byte, base64.RawStdEncoding.EncodedLen(encoded.Len()+1024))
	base64.StdEncoding.Encode(want, encoded.Bytes())
	got := ToByteImg(img)
	if !bytes.Equal(got, want) || !bytes.Equal(ToByteImg(img, "jpeg"), want) {
		t.Fatal("legacy default JPEG/base64/padding representation changed")
	}
	if ToStringImg(img) != string(want) {
		t.Fatal("string serializer no longer preserves the byte representation")
	}
	if got := ToByteImg(img, "unsupported-fixture-format"); got != nil {
		t.Fatalf("legacy encoder failure = %v, want nil", got)
	}
	if got := ToStringImg(img, "unsupported-fixture-format"); got != "" {
		t.Fatalf("legacy string encoder failure = %q, want empty", got)
	}
}
