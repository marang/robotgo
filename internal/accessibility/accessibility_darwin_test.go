//go:build darwin

package accessibility

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDarwinAccessibilityProbeReportsExplicitPermissionState(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	capability := Probe(ctx)
	if capability.Available {
		if capability.Backend != BackendMacOSAccessibility || capability.PermissionDenied || capability.Reason == "" {
			t.Fatalf("available capability = %+v", capability)
		}
		return
	}
	if capability.Backend != "" || capability.Reason == "" || capability.Notes == "" {
		t.Fatalf("unavailable capability is ambiguous: %+v", capability)
	}
	if capability.PermissionDenied && capability.Reason != "macOS Accessibility permission is not granted" {
		t.Fatalf("permission capability = %+v", capability)
	}
}

func TestDarwinAccessibilityRejectsInvalidTargetBeforeNativeCalls(t *testing.T) {
	limits := Limits{
		MaxElements: 1, MaxDepth: 1, MaxStringBytes: 1,
		MaxReferenceBytes: 16, MaxTotalReferenceBytes: 16,
		AllowedRoles: map[string]bool{"window": true},
	}
	for _, target := range []Target{
		{},
		{ProcessID: 1, NativeWindowHandle: 1, ExpectedTitle: "Fixture"},
		{ProcessID: 1},
	} {
		if _, err := Inspect(t.Context(), target, limits); !errors.Is(err, ErrInvalidTree) {
			t.Fatalf("target %+v error = %v", target, err)
		}
	}
}
