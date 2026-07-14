package robotgo

import "testing"

func TestValidateCaptureArguments(t *testing.T) {
	valid := [][]int{
		nil,
		{0, 0, 0, 0},
		{-10, 20, 30, 40},
		{0, 0, 1, 1, 2},
	}
	for _, args := range valid {
		if err := validateCaptureArguments(args); err != nil {
			t.Fatalf("validateCaptureArguments(%v): %v", args, err)
		}
	}

	maxInt := int(^uint(0) >> 1)
	invalid := [][]int{
		{1},
		{1, 2},
		{1, 2, 3},
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 0, 1},
		{0, 0, 1, 0},
		{0, 0, -1, 1},
		{0, 0, 1, -1},
		{maxInt, 0, 1, 1},
		{maxCaptureCoordinate, 0, 1, 1},
		{0, 0, maxBitmapBufferSize/captureBytesPerPixel + 1, 1},
		{0, 0, 16384, maxBitmapBufferSize/(16384*captureBytesPerPixel) + 1},
	}
	for _, args := range invalid {
		if err := validateCaptureArguments(args); err == nil {
			t.Fatalf("validateCaptureArguments(%v) unexpectedly succeeded", args)
		}
	}
}

func TestCaptureEntryPointsRejectInvalidArgumentsBeforeBackend(t *testing.T) {
	invalid := [][]int{
		{1},
		{1, 0, 0, 0},
		{0, 0, maxBitmapBufferSize/captureBytesPerPixel + 1, 1},
	}
	for _, args := range invalid {
		if _, err := Capture(args...); err == nil {
			t.Fatalf("Capture(%v) unexpectedly succeeded", args)
		}
		if _, err := CaptureScreen(args...); err == nil {
			t.Fatalf("CaptureScreen(%v) unexpectedly succeeded", args)
		}
		if _, err := CaptureImg(args...); err == nil {
			t.Fatalf("CaptureImg(%v) unexpectedly succeeded", args)
		}
	}
}
