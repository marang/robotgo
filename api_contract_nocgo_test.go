//go:build !cgo

package robotgo

import (
	"errors"
	"runtime"
	"testing"
)

func TestNonCGOPortableAPIContract(t *testing.T) {
	capabilities := GetLinuxCapabilities()
	_ = capabilities.Hook
	_ = capabilities.Events

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		invalidTargets := []error{
			SetActiveE(0),
			MinWindowE(0),
			MaxWindowE(0),
			CloseWindowE(0),
			CloseWindowKill(0),
		}
		for _, err := range invalidTargets {
			if err == nil || errors.Is(err, ErrNotSupported) {
				t.Fatalf(
					"Pure-Go %s invalid-target error = %v, want explicit backend error",
					runtime.GOOS,
					err,
				)
			}
		}
	} else {
		unsupported := []error{
			CloseWindowE(),
			MinWindowE(0),
			MaxWindowE(0),
			SetTopMostE(true),
		}
		for _, err := range unsupported {
			if !errors.Is(err, ErrNotSupported) {
				t.Fatalf("non-CGO operation error = %v, want ErrNotSupported", err)
			}
		}
	}
	if err := MinWindowE(0, "invalid"); err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("MinWindowE invalid argument error = %v, want validation error", err)
	}
}

func TestNonCGOWindowGeometryErrorContract(t *testing.T) {
	operations := []struct {
		name string
		call func(int, ...int) (int, int, int, int, error)
	}{
		{name: "GetBoundsE", call: GetBoundsE},
		{name: "GetClientE", call: GetClientE},
	}
	for _, operation := range operations {
		x, y, width, height, err := operation.call(0)
		if err == nil {
			t.Fatalf("%s accepted zero window target", operation.name)
		}
		if x != 0 || y != 0 || width != 0 || height != 0 {
			t.Fatalf(
				"%s failure returned geometry %d,%d %dx%d",
				operation.name,
				x,
				y,
				width,
				height,
			)
		}
	}
	if x, y, width, height := GetBounds(0); x != 0 || y != 0 || width != 0 || height != 0 {
		t.Fatalf("legacy GetBounds failure = %d,%d %dx%d, want zero geometry", x, y, width, height)
	}
	if x, y, width, height := GetClient(0); x != 0 || y != 0 || width != 0 || height != 0 {
		t.Fatalf("legacy GetClient failure = %d,%d %dx%d, want zero geometry", x, y, width, height)
	}
}
