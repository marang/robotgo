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
	capabilities := GetRuntimeCapabilities()
	if capabilities.Mouse.Backend != featureBackendPureGoQuartzInput {
		t.Fatalf("mouse capability = %+v, want Quartz backend", capabilities.Mouse)
	}
	if capabilities.Keyboard.Backend != featureBackendPureGoQuartzInput {
		t.Fatalf("keyboard capability = %+v, want Quartz backend", capabilities.Keyboard)
	}

	keyboardErr := KeyboardReady()
	mouseErr := MouseReady()
	switch {
	case keyboardErr == nil && mouseErr == nil:
		if !capabilities.Keyboard.Available || !capabilities.Mouse.Available {
			t.Fatalf("input readiness succeeded but capabilities = %+v", capabilities)
		}
	case errors.Is(keyboardErr, ErrPermissionDenied) &&
		errors.Is(mouseErr, ErrPermissionDenied):
		if capabilities.Keyboard.Available || capabilities.Mouse.Available {
			t.Fatalf("input readiness denied but capabilities = %+v", capabilities)
		}
		if !strings.Contains(keyboardErr.Error(), "Accessibility") ||
			!strings.Contains(mouseErr.Error(), "Accessibility") {
			t.Fatalf(
				"permission errors lack actionable Accessibility hint: keyboard=%v mouse=%v",
				keyboardErr, mouseErr,
			)
		}
	case errors.Is(keyboardErr, ErrNotSupported) || errors.Is(mouseErr, ErrNotSupported):
		t.Fatalf(
			"required hosted-runner Quartz symbols are unavailable: keyboard=%v mouse=%v",
			keyboardErr, mouseErr,
		)
	default:
		t.Fatalf("inconsistent input readiness: keyboard=%v mouse=%v", keyboardErr, mouseErr)
	}

	if err := CloseMainDisplayE(); err != nil {
		t.Fatalf("CloseMainDisplayE: %v", err)
	}
}
