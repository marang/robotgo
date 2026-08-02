package robotgo

import "image"

func wipeCaptureImage(img image.Image) {
	switch typed := img.(type) {
	case *image.RGBA:
		clear(typed.Pix)
	case *image.NRGBA:
		clear(typed.Pix)
	case *image.RGBA64:
		clear(typed.Pix)
	case *image.NRGBA64:
		clear(typed.Pix)
	case *image.Gray:
		clear(typed.Pix)
	case *image.Gray16:
		clear(typed.Pix)
	case *image.Alpha:
		clear(typed.Pix)
	case *image.Alpha16:
		clear(typed.Pix)
	case *image.CMYK:
		clear(typed.Pix)
	case *image.Paletted:
		clear(typed.Pix)
	case *image.YCbCr:
		clear(typed.Y)
		clear(typed.Cb)
		clear(typed.Cr)
	case *image.NYCbCrA:
		clear(typed.Y)
		clear(typed.Cb)
		clear(typed.Cr)
		clear(typed.A)
	}
}
