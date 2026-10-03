//go:build cgo

package robotgo

import (
	"errors"
	"image"
)

// GetPixelColor returns the pixel color as a hex string.
func GetPixelColor(x, y int, displayId ...int) (string, error) {
	c, err := GetPxColor(x, y, displayId...)
	if err != nil {
		return "", err
	}
	return PadHex(c), nil
}

// GetLocationColor gets the color of the current mouse location.
func GetLocationColor(displayId ...int) (string, error) {
	x, y, err := LocationE()
	if err != nil {
		return "", err
	}
	return GetPixelColor(x, y, displayId...)
}

// FreeBitmapArr free and dealloc the C bitmap array
func FreeBitmapArr(bit ...CBitmap) {
	for i := 0; i < len(bit); i++ {
		FreeBitmap(bit[i])
	}
}

func ownedBitmapFromC(bit CBitmap) (Bitmap, error) {
	bitmap := ToBitmap(bit)
	data, err := bitmapBytes(bitmap)
	if err != nil {
		return Bitmap{}, err
	}
	bitmap.buf = data
	if len(bitmap.buf) > 0 {
		bitmap.ImgBuf = &bitmap.buf[0]
	}
	return bitmap, nil
}

// ToCBitmap trans Bitmap to C.MMBitmapRef. Invalid input returns nil; callers
// that need the validation error should use ToCBitmapE.
func ToCBitmap(bit Bitmap) CBitmap {
	bitmap, _ := ToCBitmapE(bit)
	return bitmap
}

// ToImage convert C.MMBitmapRef to standard image.Image
func ToImage(bit CBitmap) image.Image {
	img, err := ToRGBAE(bit)
	if err != nil {
		return nil
	}
	return img
}

// ToRGBA convert C.MMBitmapRef to standard image.RGBA
func ToRGBA(bit CBitmap) *image.RGBA {
	img, _ := ToRGBAE(bit)
	return img
}

// ToRGBAE validates a C bitmap before converting it to image.RGBA.
func ToRGBAE(bit CBitmap) (*image.RGBA, error) {
	if bit == nil {
		return nil, errors.New("bitmap is nil")
	}
	return ToRGBAGoE(ToBitmap(bit))
}

// ImgToCBitmap trans image.Image to CBitmap
func ImgToCBitmap(img image.Image) CBitmap {
	bitmap, _ := ImgToCBitmapE(img)
	return bitmap
}

// ImgToCBitmapE converts an image to a validated C bitmap.
func ImgToCBitmapE(img image.Image) (CBitmap, error) {
	bitmap, err := ImgToBitmapE(img)
	if err != nil {
		return nil, err
	}
	return ToCBitmapE(bitmap)
}

// ByteToCBitmap trans []byte to CBitmap
func ByteToCBitmap(by []byte) CBitmap {
	bitmap, _ := ByteToCBitmapE(by)
	return bitmap
}

// ByteToCBitmapE decodes image bytes and returns any decode or bitmap error.
func ByteToCBitmapE(by []byte) (CBitmap, error) {
	img, err := ByteToImg(by)
	if err != nil {
		return nil, err
	}
	return ImgToCBitmapE(img)
}
