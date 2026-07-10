//go:build linux && portal

package portal

import (
	"context"
	"image"
	"testing"
)

func TestImageToCBitmap(t *testing.T) {
	bm, err := imageToCBitmap(image.NewRGBA(image.Rect(0, 0, 10, 10)))
	if err != nil {
		t.Fatalf("imageToCBitmap failed: %v", err)
	}
	if bm == nil {
		t.Fatal("expected bitmap, got nil")
	}
	t.Cleanup(func() { freeCBitmap(bm) })
}

func TestCaptureForcedFailure(t *testing.T) {
	t.Setenv("ROBOTGO_PORTAL_FAIL", "1")
	if _, err := Capture(context.Background(), 0, 0, 10, 10); err == nil {
		t.Fatal("expected error")
	}
}
