package robotgo

import (
	"image"
	"testing"
)

var (
	_ func(int) (int, int, int, int, error) = GetDisplayBoundsE
	_ func() (int, int, error)              = GetScreenSizeE
	_ func(...int) (Rect, error)            = GetScreenRectE
	_ func() (int, error)                   = DisplaysNumE
)

func TestDisplayBoundsErrorAPIsValidateArgumentsBeforeBackend(t *testing.T) {
	t.Parallel()

	if _, _, _, _, err := GetDisplayBoundsE(-1); err == nil {
		t.Fatal("GetDisplayBoundsE(-1) unexpectedly succeeded")
	}
	if _, err := GetScreenRectE(-2); err == nil {
		t.Fatal("GetScreenRectE(-2) unexpectedly succeeded")
	}
	if _, err := GetScreenRectE(0, 1); err == nil {
		t.Fatal("GetScreenRectE(0, 1) unexpectedly succeeded")
	}
}

func TestValidateDisplayRectangle(t *testing.T) {
	t.Parallel()

	if err := validateDisplayRectangle(image.Rectangle{}); err == nil {
		t.Fatal("validateDisplayRectangle() accepted empty bounds")
	}
	if err := validateDisplayRectangle(image.Rect(-10, 20, 90, 220)); err != nil {
		t.Fatalf("validateDisplayRectangle() error = %v", err)
	}
}
