//go:build cgo

package robotgo

import (
	"context"
	"errors"
	"image"
	"testing"
)

func TestFinishNativeContextCaptureClearsCanceledImage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	canceledImage := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for index := range canceledImage.Pix {
		canceledImage.Pix[index] = byte(index + 1)
	}
	cancel()

	img, err := finishNativeContextCapture(ctx, canceledImage)
	if img != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("finishNativeContextCapture = (%v, %v), want nil canceled result", img, err)
	}
	for index, value := range canceledImage.Pix {
		if value != 0 {
			t.Fatalf("canceled capture byte %d = %d, want zero", index, value)
		}
	}
}
