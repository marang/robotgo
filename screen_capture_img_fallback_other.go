//go:build cgo && !linux

package robotgo

import "image"

func platformCaptureImgFallback(...int) (image.Image, bool, error) {
	return nil, false, nil
}
