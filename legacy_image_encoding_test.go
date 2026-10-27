package robotgo

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func TestLegacyImageEncodingRepresentation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	img.Set(1, 0, color.RGBA{B: 150, A: 255})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	// The legacy serializer defaults to JPEG and returns standard base64 in an oversized
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

func TestLegacyImageEncodingFormats(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 200, A: 255})
	img.SetRGBA(1, 0, color.RGBA{B: 150, A: 255})
	img.SetRGBA(2, 1, color.RGBA{G: 100, A: 128})

	for _, tc := range []struct {
		format string
		encode func(io.Writer, image.Image) error
	}{
		{"jpeg", func(w io.Writer, img image.Image) error {
			return jpeg.Encode(w, img, &jpeg.Options{Quality: 70})
		}},
		{"png", png.Encode},
		{"gif", func(w io.Writer, img image.Image) error {
			return gif.Encode(w, img, &gif.Options{})
		}},
		{"bmp", bmp.Encode},
		{"tiff", func(w io.Writer, img image.Image) error {
			return tiff.Encode(w, img, &tiff.Options{})
		}},
	} {
		t.Run(tc.format, func(t *testing.T) {
			var encoded bytes.Buffer
			if err := tc.encode(&encoded, img); err != nil {
				t.Fatal(err)
			}
			want := make([]byte, base64.RawStdEncoding.EncodedLen(encoded.Len()+1024))
			base64.StdEncoding.Encode(want, encoded.Bytes())
			for _, args := range [][]string{{tc.format}, {tc.format, "ignored-extra-format"}} {
				if got := ToByteImg(img, args...); !bytes.Equal(got, want) {
					t.Fatalf("ToByteImg(%v) changed payload/padding: got %d bytes, want %d", args, len(got), len(want))
				}
				if got := ToStringImg(img, args...); got != string(want) {
					t.Fatalf("ToStringImg(%v) changed payload/padding", args)
				}
			}
		})
	}
}

func TestLegacyImageEncodingRejectsUnsupportedFormats(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	// These spellings were not accepted by imgo 0.42. New upstream aliases must
	// not silently broaden the legacy serializer's format contract.
	for _, format := range []string{"", "jpg", "tif", "JPEG", "PNG", " jpeg", "unsupported-fixture-format"} {
		t.Run(format, func(t *testing.T) {
			if got := ToByteImg(img, format); got != nil {
				t.Fatalf("ToByteImg(%q) = %d bytes, want nil", format, len(got))
			}
			if got := ToStringImg(img, format); got != "" {
				t.Fatalf("ToStringImg(%q) = %d bytes, want empty", format, len(got))
			}
		})
	}
}

func TestLegacyImageEncodingFailure(t *testing.T) {
	t.Parallel()
	// JPEG dimensions must fit in 16 bits; this fixture fails before encoding.
	img := image.NewRGBA(image.Rect(0, 0, 1<<16, 1))
	if got := ToByteImg(img); got != nil {
		t.Fatalf("failed JPEG encoding returned %d bytes, want nil", len(got))
	}
	if got := ToStringImg(img); got != "" {
		t.Fatalf("failed JPEG encoding returned a %d-byte string, want empty", len(got))
	}
}

func TestLegacyImageFileFormats(t *testing.T) {
	// Bare relative format names were accepted by the legacy path parser. Keep
	// these synthetic files in a disposable working directory, never the repo.
	t.Chdir(t.TempDir())
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 200, A: 255})
	for _, format := range []string{"jpeg", "png", "gif", "bmp", "tiff"} {
		for _, path := range []string{format, "fixture." + format} {
			t.Run(path, func(t *testing.T) {
				if err := Save(img, path); err != nil {
					t.Fatalf("Save(%q): %v", path, err)
				}
				got, err := Read(path)
				if err != nil {
					t.Fatalf("Read(%q): %v", path, err)
				}
				if got.Bounds().Size() != img.Bounds().Size() {
					t.Fatalf("Read(%q) bounds = %v, want %v", path, got.Bounds(), img.Bounds())
				}
			})
		}
	}

	for _, tc := range []struct{ suffix, format string }{
		{"jpg", "jpeg"}, {"tif", "tiff"}, {"JPEG", "jpeg"},
		{"PNG", "png"}, {"unsupported-fixture-format", "png"},
	} {
		path := "fixture." + tc.suffix
		t.Run(path, func(t *testing.T) {
			if err := Save(img, path); err == nil {
				t.Errorf("Save(%q) accepted a previously unsupported format", path)
			}
			// Valid encoded content proves rejection is based on the filename,
			// rather than failing only because Save left an empty file.
			var encoded bytes.Buffer
			var err error
			switch tc.format {
			case "jpeg":
				err = jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 70})
			case "tiff":
				err = tiff.Encode(&encoded, img, &tiff.Options{})
			default:
				err = png.Encode(&encoded, img)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Read(path); err == nil {
				t.Fatalf("Read(%q) accepted a previously unsupported format", path)
			}
		})
	}
	const qualityPath = "quality.jpeg"
	if err := Save(img, qualityPath, 95, 10); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(qualityPath); err != nil || !bytes.Equal(got, encoded.Bytes()) {
		t.Fatalf("Save changed first-quality-argument behavior: %v", err)
	}

	if _, err := Read("missing.png"); !os.IsNotExist(err) {
		t.Fatalf("Read missing file: %v, want os.ErrNotExist", err)
	}
	if err := Save(img, filepath.Join("missing-directory", "fixture.png")); !os.IsNotExist(err) {
		t.Fatalf("Save in missing directory: %v, want os.ErrNotExist", err)
	}
}
