//go:build darwin && !cgo

package robotgo

import (
	"errors"
	"strings"
	"testing"

	"github.com/marang/robotgo/internal/darwininput"
)

func TestPureGoDarwinInputErrorTranslation(t *testing.T) {
	for _, test := range []struct {
		name string
		in   error
		want error
	}{
		{name: "unsupported", in: darwininput.ErrUnsupported, want: ErrNotSupported},
		{name: "permission", in: darwininput.ErrPermission, want: ErrPermissionDenied},
		{name: "ownership", in: darwininput.ErrOwnership, want: ErrInputOwnership},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := translatePureGoDarwinInputError(test.in); !errors.Is(err, test.want) {
				t.Fatalf("translated error = %v, want %v", err, test.want)
			}
		})
	}
}

// TestPureGoDarwinInputRuntime is deliberately preflight-only: it resolves the
// real framework symbols and checks Accessibility status without posting input
// or opening a macOS consent dialog.
func TestPureGoDarwinInputRuntime(t *testing.T) {
	if err := KeyboardReady(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("KeyboardReady = %v, want ErrNotSupported", err)
	}

	capabilities := GetRuntimeCapabilities()
	if capabilities.Mouse.Backend != featureBackendPureGoQuartzInput {
		t.Fatalf("mouse capability = %+v, want Quartz backend", capabilities.Mouse)
	}
	if capabilities.Keyboard.Available ||
		capabilities.Keyboard.Backend != featureBackendPureGoQuartzInput {
		t.Fatalf("keyboard capability = %+v, want named unsupported Quartz keyboard", capabilities.Keyboard)
	}

	readyErr := MouseReady()
	switch {
	case readyErr == nil:
		if !capabilities.Mouse.Available {
			t.Fatalf("MouseReady succeeded but capability = %+v", capabilities.Mouse)
		}
	case errors.Is(readyErr, ErrPermissionDenied):
		if capabilities.Mouse.Available {
			t.Fatalf("MouseReady denied but capability = %+v", capabilities.Mouse)
		}
		if !strings.Contains(readyErr.Error(), "Accessibility") {
			t.Fatalf("permission error lacks actionable Accessibility hint: %v", readyErr)
		}
	case errors.Is(readyErr, ErrNotSupported):
		t.Fatalf("required hosted-runner Quartz symbols are unavailable: %v", readyErr)
	default:
		t.Fatalf("unexpected MouseReady error: %v", readyErr)
	}

	if err := CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE: %v", err)
	}
}
