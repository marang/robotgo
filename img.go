// Copyright (c) 2016-2025 AtomAI, All rights reserved.
//
// See the COPYRIGHT file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

package robotgo

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"unsafe"

	"github.com/vcaesar/imgo"
)

// DecodeImg decode the image to image.Image and return
func DecodeImg(path string) (image.Image, string, error) {
	return imgo.DecodeFile(path)
}

// OpenImg open the image return []byte
func OpenImg(path string) ([]byte, error) {
	return imgo.ImgToBytes(path)
}

// Read read the file return image.Image
func Read(path string) (image.Image, error) {
	return imgo.Read(path)
}

// Save create a image file with the image.Image
func Save(img image.Image, path string, quality ...int) error {
	return imgo.Save(path, img, quality...)
}

// SaveImg save the image by []byte
func SaveImg(b []byte, path string) error {
	return imgo.SaveByte(path, b)
}

// SavePng save the image by image.Image
func SavePng(img image.Image, path string) error {
	return imgo.SaveToPNG(path, img)
}

// SaveJpeg save the image by image.Image
func SaveJpeg(img image.Image, path string, quality ...int) error {
	return imgo.SaveToJpeg(path, img, quality...)
}

// ToByteImg convert image.Image to []byte
func ToByteImg(img image.Image, fm ...string) []byte {
	return imgo.ToByte(img, fm...)
}

// ToStringImg convert image.Image to string
func ToStringImg(img image.Image, fm ...string) string {
	return string(ToByteImg(img, fm...))
}

// StrToImg convert base64 string to image.Image
func StrToImg(data string) (image.Image, error) {
	return imgo.StrToImg(data)
}

// ByteToImg convert []byte to image.Image
func ByteToImg(b []byte) (image.Image, error) {
	return imgo.ByteToImg(b)
}

// ImgSize get the file image size
func ImgSize(path string) (int, int, error) {
	return imgo.GetSize(path)
}

// Width return the image.Image width
func Width(img image.Image) int {
	return img.Bounds().Max.X
}

// Height return the image.Image height
func Height(img image.Image) int {
	return img.Bounds().Max.Y
}

// RGBAToBitmap convert the standard image.RGBA to Bitmap
func RGBAToBitmap(r1 *image.RGBA) (bit Bitmap) {
	bit, _ = RGBAToBitmapE(r1)
	return bit
}

// RGBAToBitmapE validates and converts an image.RGBA to an owned Bitmap.
func RGBAToBitmapE(r1 *image.RGBA) (bit Bitmap, err error) {
	if r1 == nil {
		return Bitmap{}, fmt.Errorf("image is nil")
	}
	bounds := r1.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return Bitmap{}, fmt.Errorf("invalid image dimensions %dx%d", width, height)
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/4 {
		return Bitmap{}, fmt.Errorf("image row size overflows int")
	}
	bytewidth := width * 4
	if r1.Stride < bytewidth || height-1 > (maxInt-bytewidth)/r1.Stride {
		return Bitmap{}, fmt.Errorf("invalid RGBA stride %d for %dx%d image", r1.Stride, width, height)
	}
	required := (height-1)*r1.Stride + bytewidth
	if len(r1.Pix) < required {
		return Bitmap{}, fmt.Errorf("RGBA pixel buffer length %d is smaller than required size %d", len(r1.Pix), required)
	}
	metadata := Bitmap{Width: width, Height: height, Bytewidth: bytewidth, BitsPixel: 32, BytesPerPixel: 4}
	total, err := bitmapBufferLen(metadata)
	if err != nil {
		return Bitmap{}, err
	}
	pixels := make([]byte, total)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sourceOffset := y*r1.Stride + x*4
			targetOffset := y*bytewidth + x*4
			pixels[targetOffset] = r1.Pix[sourceOffset+2]
			pixels[targetOffset+1] = r1.Pix[sourceOffset+1]
			pixels[targetOffset+2] = r1.Pix[sourceOffset]
			pixels[targetOffset+3] = r1.Pix[sourceOffset+3]
		}
	}
	return NewBitmap(pixels, width, height, bytewidth, 32, 4)
}

// ImgToBitmap convert the standard image.Image to Bitmap
func ImgToBitmap(m image.Image) (bit Bitmap) {
	bit, _ = ImgToBitmapE(m)
	return bit
}

// ImgToBitmapE validates and converts an image.Image to an owned Bitmap.
func ImgToBitmapE(m image.Image) (bit Bitmap, err error) {
	if m == nil {
		return Bitmap{}, fmt.Errorf("image is nil")
	}
	bit.Width = m.Bounds().Size().X
	bit.Height = m.Bounds().Size().Y

	pix, stride, err := imgo.EncodeImg(m)
	if err != nil {
		return Bitmap{}, err
	}
	bit.Bytewidth = stride

	buf, src := ToUint8p(pix)
	bit.ImgBuf = src
	bit.buf = buf

	bit.BitsPixel = 32
	bit.BytesPerPixel = 32 / 8
	if _, err := bitmapBufferLen(bit); err != nil {
		return Bitmap{}, err
	}
	return bit, nil
}

// ToUint8p convert the []uint8 to a uint8 pointer and backing slice
func ToUint8p(dst []uint8) ([]uint8, *uint8) {
	if len(dst) == 0 {
		return nil, nil
	}

	src := make([]uint8, len(dst)+10)
	for i := 0; i <= len(dst)-4; i += 4 {
		src[i+3] = dst[i+3]
		src[i] = dst[i+2]
		src[i+1] = dst[i+1]
		src[i+2] = dst[i]
	}

	return src, (*uint8)(unsafe.Pointer(&src[0]))
}

// ToRGBAGo convert Bitmap to standard image.RGBA
func ToRGBAGo(bmp1 Bitmap) *image.RGBA {
	img, _ := ToRGBAGoE(bmp1)
	return img
}

// ToRGBAGoE converts a validated four-byte BGRA Bitmap to image.RGBA.
func ToRGBAGoE(bitmap Bitmap) (*image.RGBA, error) {
	if bitmap.BytesPerPixel != 4 {
		return nil, fmt.Errorf("unsupported bitmap bytesPerPixel=%d; RGBA conversion requires 4", bitmap.BytesPerPixel)
	}
	pixels, err := bitmapBytes(bitmap)
	if err != nil {
		return nil, err
	}
	for y := 0; y < bitmap.Height; y++ {
		row := y * bitmap.Bytewidth
		for x := 0; x < bitmap.Width; x++ {
			offset := row + x*4
			pixels[offset], pixels[offset+2] = pixels[offset+2], pixels[offset]
		}
	}
	return &image.RGBA{
		Pix: pixels, Stride: bitmap.Bytewidth,
		Rect: image.Rect(0, 0, bitmap.Width, bitmap.Height),
	}, nil
}

// GetTextImg get text from image.Image by writing a temporary PNG and running OCR.
func GetTextImg(img image.Image, args ...string) (string, error) {
	return GetTextImgContext(context.Background(), img, args...)
}

// GetTextImgContext writes an image to a private temporary file and runs OCR
// with caller-controlled cancellation.
func GetTextImgContext(ctx context.Context, img image.Image, args ...string) (result string, retErr error) {
	if img == nil {
		return "", fmt.Errorf("image is nil")
	}
	file, err := os.CreateTemp("", "robotgo-ocr-*.png")
	if err != nil {
		return "", err
	}
	path := file.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); retErr == nil && closeErr != nil {
				retErr = closeErr
			}
		}
		if removeErr := os.Remove(path); retErr == nil && removeErr != nil {
			retErr = removeErr
		}
	}()
	if err := png.Encode(file, img); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	closed = true
	return GetTextContext(ctx, path, args...)
}
