//go:build linux && portal
// +build linux,portal

package portal

import (
	"context"
	"runtime"
	"testing"
)

func TestCapture(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("portal capture only supported on linux")
	}
	t.Parallel()
	bm, err := Capture(context.Background(), 0, 0, 10, 10)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if bm == nil {
		t.Fatalf("expected bitmap, got nil")
	}
}

func TestCaptureError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("portal capture only supported on linux")
	}
	t.Parallel()
	t.Setenv("ROBOTGO_PORTAL_FAIL", "1")
	if _, err := Capture(context.Background(), 0, 0, 10, 10); err == nil {
		t.Fatalf("expected error")
	}
}
