//go:build !cgo

package robotgo

import (
	"errors"
	"testing"
)

func TestNonCGOPortableAPIContract(t *testing.T) {
	capabilities := GetLinuxCapabilities()
	_ = capabilities.Hook
	_ = capabilities.Events

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
	if err := MinWindowE(0, "invalid"); err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("MinWindowE invalid argument error = %v, want validation error", err)
	}
}
