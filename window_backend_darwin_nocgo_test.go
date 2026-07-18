//go:build darwin && !cgo

package robotgo

import (
	"errors"
	"strings"
	"testing"

	"github.com/marang/robotgo/internal/darwinwindow"
	"github.com/marang/robotgo/internal/windowbackend"
)

func TestPureGoDarwinWindowCapabilityUsesNonPromptingPreflight(t *testing.T) {
	capability := GetRuntimeCapabilities().Window
	if capability.Backend != featureBackendPureGoMacOSWindow {
		t.Fatalf("window capability = %+v", capability)
	}
	if capability.Available {
		if !strings.Contains(capability.Reason, "backend is ready") {
			t.Fatalf("available window capability = %+v", capability)
		}
	} else if capability.Reason != ErrPermissionDenied.Error() &&
		!strings.Contains(capability.Reason, ErrNotSupported.Error()) {
		t.Fatalf("unavailable window capability = %+v", capability)
	}

	permissions := darwinRuntimePermissions(GetRuntimeCapabilities())
	var found bool
	for _, permission := range permissions {
		if permission.Feature != runtimeFeatureWindow {
			continue
		}
		found = true
		if permission.Name != "Accessibility" {
			t.Fatalf("window permission = %+v", permission)
		}
	}
	if !found {
		t.Fatal("runtime diagnostics omitted macOS window Accessibility permission")
	}
}

func TestTranslatePureGoDarwinWindowErrors(t *testing.T) {
	permission := errors.Join(windowbackend.ErrPermission, darwinwindow.ErrPermission)
	if err := translatePureGoWindowError(permission); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("permission translation = %v", err)
	}
	unsupported := errors.Join(windowbackend.ErrUnsupported, darwinwindow.ErrUnsupported)
	if err := translatePureGoWindowError(unsupported); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("unsupported translation = %v", err)
	}
}

func TestPureGoDarwinWindowCleanupIsReusable(t *testing.T) {
	before := GetRuntimeCapabilities().Window
	if err := closePureGoPlatformWindow(); err != nil {
		t.Fatalf("close Pure-Go macOS window backend: %v", err)
	}
	after := GetRuntimeCapabilities().Window
	if before.Available != after.Available || before.Backend != after.Backend {
		t.Fatalf("window capability changed after cleanup: before=%+v after=%+v", before, after)
	}
	if err := closePureGoPlatformWindow(); err != nil {
		t.Fatalf("second close Pure-Go macOS window backend: %v", err)
	}
}
