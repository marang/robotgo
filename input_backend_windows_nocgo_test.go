//go:build windows && !cgo

package robotgo

import (
	"errors"
	"testing"

	"github.com/marang/robotgo/internal/windowsinput"
)

func TestPureGoWindowsInputBackendSelection(t *testing.T) {
	backend := platformPureGoInputBackend()
	if backend == nil || backend.Name() != featureBackendPureGoWindows {
		t.Fatalf("platform backend = %#v, want %q", backend, featureBackendPureGoWindows)
	}
	keyboard, mouse := pureGoInputCapabilities()
	for name, capability := range map[string]FeatureCapability{
		"keyboard": keyboard,
		"mouse":    mouse,
	} {
		if !capability.Available || capability.Backend != featureBackendPureGoWindows {
			t.Fatalf("%s capability = %+v", name, capability)
		}
	}
}

func TestTranslatePureGoWindowsInputErrors(t *testing.T) {
	if err := translatePureGoWindowsInputError(windowsinput.ErrUnsupported); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("unsupported translation = %v", err)
	}
	if err := translatePureGoWindowsInputError(windowsinput.ErrOwnership); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("ownership translation = %v", err)
	}
}
