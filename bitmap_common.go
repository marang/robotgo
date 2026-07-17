package robotgo

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unsafe"
)

const (
	bitmapStringFormat      = "robotgo.bitmap.v1"
	maxBitmapBufferSize     = 512 * 1024 * 1024
	defaultColorTolerance   = 0.01
	minimumRGBBytesPerPixel = 3
	maximumRGBBytesPerPixel = 4
)

type bitmapStringPayload struct {
	Format        string `json:"format"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	Bytewidth     int    `json:"bytewidth"`
	BitsPixel     uint8  `json:"bitsPixel"`
	BytesPerPixel uint8  `json:"bytesPerPixel"`
	Data          string `json:"data"`
}

// NewBitmap validates and copies raw pixel data into a RobotGo-owned Bitmap.
// Use this constructor instead of assigning ImgBuf to arbitrary memory.
func NewBitmap(data []byte, width, height, bytewidth int, bitsPixel, bytesPerPixel uint8) (Bitmap, error) {
	bitmap := Bitmap{
		Width: width, Height: height, Bytewidth: bytewidth,
		BitsPixel: bitsPixel, BytesPerPixel: bytesPerPixel,
	}
	total, err := bitmapBufferLen(bitmap)
	if err != nil {
		return Bitmap{}, err
	}
	if len(data) != total {
		return Bitmap{}, fmt.Errorf("bitmap data length %d does not match layout size %d", len(data), total)
	}
	bitmap.buf = append([]byte(nil), data...)
	bitmap.ImgBuf = &bitmap.buf[0]
	return bitmap, nil
}

func bitmapBufferLen(bit Bitmap) (int, error) {
	if bit.Width <= 0 || bit.Height <= 0 {
		return 0, fmt.Errorf("invalid bitmap dimensions %dx%d", bit.Width, bit.Height)
	}
	bytesPerPixel := int(bit.BytesPerPixel)
	if bit.Bytewidth <= 0 || bytesPerPixel == 0 {
		return 0, fmt.Errorf("invalid bitmap layout bytewidth=%d bytesPerPixel=%d", bit.Bytewidth, bit.BytesPerPixel)
	}
	if bit.BitsPixel == 0 || int(bit.BitsPixel) != bytesPerPixel*8 {
		return 0, fmt.Errorf("invalid bitmap pixel layout bitsPixel=%d bytesPerPixel=%d", bit.BitsPixel, bit.BytesPerPixel)
	}
	maxInt := int(^uint(0) >> 1)
	if bit.Width > maxInt/bytesPerPixel {
		return 0, errors.New("bitmap row size overflows int")
	}
	minimumStride := bit.Width * bytesPerPixel
	if bit.Bytewidth < minimumStride {
		return 0, fmt.Errorf("invalid bitmap stride %d for width=%d bytesPerPixel=%d", bit.Bytewidth, bit.Width, bit.BytesPerPixel)
	}
	if bit.Height > maxInt/bit.Bytewidth {
		return 0, errors.New("bitmap buffer size overflows int")
	}
	total := bit.Bytewidth * bit.Height
	if total <= 0 || total > maxBitmapBufferSize {
		return 0, fmt.Errorf("invalid bitmap buffer size %d", total)
	}
	return total, nil
}

func bitmapBytes(bit Bitmap) ([]byte, error) {
	src, err := bitmapReadableBuffer(bit)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), src...), nil
}

func bitmapReadableBuffer(bit Bitmap) ([]byte, error) {
	total, err := bitmapBufferLen(bit)
	if err != nil {
		return nil, err
	}
	if len(bit.buf) != 0 {
		if len(bit.buf) < total {
			return nil, fmt.Errorf("bitmap backing buffer length %d is smaller than layout size %d", len(bit.buf), total)
		}
		return bit.buf[:total], nil
	}
	if bit.ImgBuf == nil {
		return nil, errors.New("bitmap image buffer is nil")
	}
	if !bit.trusted {
		return nil, errors.New("bitmap image buffer is not owned by RobotGo")
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(bit.ImgBuf)), total), nil
}

// ToStrBitmap serializes a Bitmap into a stable JSON/base64 string.
func ToStrBitmap(bit Bitmap) (string, error) {
	data, err := bitmapBytes(bit)
	if err != nil {
		return "", err
	}
	payload := bitmapStringPayload{
		Format: bitmapStringFormat, Width: bit.Width, Height: bit.Height,
		Bytewidth: bit.Bytewidth, BitsPixel: bit.BitsPixel,
		BytesPerPixel: bit.BytesPerPixel, Data: base64.StdEncoding.EncodeToString(data),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// BitmapFromStr decodes a bitmap string produced by ToStrBitmap.
func BitmapFromStr(str string) (Bitmap, error) {
	var payload bitmapStringPayload
	if err := json.Unmarshal([]byte(str), &payload); err != nil {
		return Bitmap{}, err
	}
	if payload.Format != bitmapStringFormat {
		return Bitmap{}, fmt.Errorf("unsupported bitmap string format %q", payload.Format)
	}
	bit := Bitmap{
		Width: payload.Width, Height: payload.Height, Bytewidth: payload.Bytewidth,
		BitsPixel: payload.BitsPixel, BytesPerPixel: payload.BytesPerPixel,
	}
	total, err := bitmapBufferLen(bit)
	if err != nil {
		return Bitmap{}, err
	}
	if len(payload.Data) != base64.StdEncoding.EncodedLen(total) {
		return Bitmap{}, fmt.Errorf("bitmap payload encoding cannot match layout size %d", total)
	}
	data, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return Bitmap{}, err
	}
	if len(data) != total {
		return Bitmap{}, fmt.Errorf("bitmap payload length %d does not match layout size %d", len(data), total)
	}
	return NewBitmap(data, bit.Width, bit.Height, bit.Bytewidth, bit.BitsPixel, bit.BytesPerPixel)
}

// CaptureBitmapStr captures the screen and returns the serialized bitmap.
func CaptureBitmapStr(args ...int) (string, error) {
	bit, err := CaptureGo(args...)
	if err != nil {
		return "", err
	}
	return ToStrBitmap(bit)
}

// FindBitmapStr searches for needleStr inside haystackStr.
func FindBitmapStr(needleStr string, haystackStr ...string) (int, int, error) {
	if len(haystackStr) > 1 {
		return -1, -1, fmt.Errorf("find bitmap string accepts at most one haystack, got %d", len(haystackStr))
	}
	needle, err := BitmapFromStr(needleStr)
	if err != nil {
		return -1, -1, err
	}
	var haystack Bitmap
	if len(haystackStr) > 0 {
		haystack, err = BitmapFromStr(haystackStr[0])
	} else {
		haystack, err = CaptureGo()
	}
	if err != nil {
		return -1, -1, err
	}
	return findBitmap(haystack, needle)
}

// FindColorCS searches a captured region for a color and returns absolute
// screen coordinates. Tolerance is optional, defaults to 0.01, and must be a
// finite value in the inclusive range 0 through 1.
func FindColorCS(x, y, width, height int, color CHex, tolerance ...float64) (int, int, error) {
	if width <= 0 || height <= 0 {
		return -1, -1, fmt.Errorf("invalid search region %dx%d", width, height)
	}
	tol, err := parseColorTolerance(tolerance)
	if err != nil {
		return -1, -1, err
	}
	bmp, err := CaptureGo(x, y, width, height)
	if err != nil {
		return -1, -1, err
	}
	buf, err := bitmapRGBBuffer(bmp)
	if err != nil {
		return -1, -1, err
	}
	r, g, b := splitHex(uint32(color))
	for yy := 0; yy < bmp.Height; yy++ {
		for xx := 0; xx < bmp.Width; xx++ {
			pr, pg, pb := bitmapRGBAtBuffer(bmp, buf, xx, yy)
			if rgbSimilar(pr, pg, pb, r, g, b, tol) {
				return x + xx, y + yy, nil
			}
		}
	}
	return -1, -1, nil
}

// FindcolorCS preserves the historical RobotGo-Pro spelling.
func FindcolorCS(x, y, width, height int, color CHex, tolerance ...float64) (int, int, error) {
	return FindColorCS(x, y, width, height, color, tolerance...)
}

func parseColorTolerance(tolerance []float64) (float64, error) {
	if len(tolerance) > 1 {
		return 0, fmt.Errorf("color search accepts at most one tolerance, got %d", len(tolerance))
	}
	value := defaultColorTolerance
	if len(tolerance) == 1 {
		value = tolerance[0]
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return 0, fmt.Errorf("invalid color tolerance %v: expected a finite value from 0 through 1", value)
	}
	return value, nil
}

func splitHex(hex uint32) (uint8, uint8, uint8) {
	return uint8((hex >> 16) & 0xff), uint8((hex >> 8) & 0xff), uint8(hex & 0xff)
}

func rgbSimilar(r1, g1, b1, r2, g2, b2 uint8, tolerance float64) bool {
	if tolerance <= 0 {
		return r1 == r2 && g1 == g2 && b1 == b2
	}
	dr, dg, db := float64(int(r1)-int(r2)), float64(int(g1)-int(g2)), float64(int(b1)-int(b2))
	return dr*dr+dg*dg+db*db <= (tolerance*442.0)*(tolerance*442.0)
}

func bitmapRGBAt(bit Bitmap, x, y int) (uint8, uint8, uint8, bool) {
	if x < 0 || y < 0 || x >= bit.Width || y >= bit.Height {
		return 0, 0, 0, false
	}
	buf, err := bitmapRGBBuffer(bit)
	if err != nil {
		return 0, 0, 0, false
	}
	r, g, b := bitmapRGBAtBuffer(bit, buf, x, y)
	return r, g, b, true
}

func bitmapRGBBuffer(bit Bitmap) ([]byte, error) {
	if bit.BytesPerPixel < minimumRGBBytesPerPixel ||
		bit.BytesPerPixel > maximumRGBBytesPerPixel {
		return nil, fmt.Errorf(
			"unsupported bitmap bytesPerPixel=%d; RGB operations require 3 or 4",
			bit.BytesPerPixel,
		)
	}
	return bitmapReadableBuffer(bit)
}

func bitmapRGBAtBuffer(bit Bitmap, buf []byte, x, y int) (uint8, uint8, uint8) {
	offset := y*bit.Bytewidth + x*int(bit.BytesPerPixel)
	return buf[offset+2], buf[offset+1], buf[offset]
}

func findBitmap(haystack, needle Bitmap) (int, int, error) {
	haystackBuf, err := bitmapRGBBuffer(haystack)
	if err != nil {
		return -1, -1, err
	}
	needleBuf, err := bitmapRGBBuffer(needle)
	if err != nil {
		return -1, -1, err
	}
	if needle.Width > haystack.Width || needle.Height > haystack.Height {
		return -1, -1, nil
	}
	for y := 0; y <= haystack.Height-needle.Height; y++ {
		for x := 0; x <= haystack.Width-needle.Width; x++ {
			if bitmapMatchesAt(haystack, haystackBuf, needle, needleBuf, x, y) {
				return x, y, nil
			}
		}
	}
	return -1, -1, nil
}

func bitmapMatchesAt(
	haystack Bitmap,
	haystackBuf []byte,
	needle Bitmap,
	needleBuf []byte,
	startX, startY int,
) bool {
	for y := 0; y < needle.Height; y++ {
		for x := 0; x < needle.Width; x++ {
			hr, hg, hb := bitmapRGBAtBuffer(haystack, haystackBuf, startX+x, startY+y)
			nr, ng, nb := bitmapRGBAtBuffer(needle, needleBuf, x, y)
			if hr != nr || hg != ng || hb != nb {
				return false
			}
		}
	}
	return true
}
