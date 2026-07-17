//go:build windows && !cgo

package robotgo

import (
	"errors"
	"strconv"
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
	runtimeCapabilities := GetRuntimeCapabilities()
	for name, capability := range map[string]FeatureCapability{
		"keyboard": runtimeCapabilities.Keyboard,
		"mouse":    runtimeCapabilities.Mouse,
	} {
		if !capability.Available || capability.Backend != featureBackendPureGoWindows {
			t.Fatalf("runtime %s capability = %+v", name, capability)
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

func TestPureGoWindowsTextDelayRejectsDurationOverflow(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("int cannot exceed time.Duration milliseconds on 32-bit Windows")
	}
	overflow := maxWindowsTextDelayMilliseconds + 1
	backend := &windowsInputBackend{core: windowsinput.New()}
	err := backend.Text(pureGoTextEvent{Text: "a", Delay: int(overflow)})
	if err == nil {
		t.Fatal("Text accepted a delay that overflows time.Duration")
	}
}
