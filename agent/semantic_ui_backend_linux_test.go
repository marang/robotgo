//go:build linux

package agent

import (
	"context"
	"errors"
	"testing"

	robotgo "github.com/marang/robotgo"
	"github.com/marang/robotgo/internal/accessibility"
)

func TestInspectPlatformUIRejectsNativeHandleWithoutDesktopIO(t *testing.T) {
	_, err := inspectPlatformUI(t.Context(), uiBackendTarget{
		Target: 42, Kind: WindowTargetHandle, ExpectedTitle: "Fixture",
	}, uiBackendLimits{})
	if !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("native-handle inspection error = %v", err)
	}
}

func TestAgentAccessibilityErrorPreservesStableClasses(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want ErrorCode
	}{
		{name: "unavailable", err: accessibility.ErrUnavailable, want: ErrorUnavailable},
		{name: "invalid-tree", err: accessibility.ErrInvalidTree, want: ErrorBackendFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			var actionErr *ActionError
			if got := agentAccessibilityError(test.err); !errors.As(got, &actionErr) || actionErr.Code != test.want {
				t.Fatalf("mapped error = %v, want code %q", got, test.want)
			}
		})
	}
	if !errors.Is(agentAccessibilityError(accessibility.ErrStaleTarget), ErrStaleTarget) ||
		!errors.Is(agentAccessibilityError(accessibility.ErrPermissionDenied), robotgo.ErrPermissionDenied) ||
		!errors.Is(agentAccessibilityError(accessibility.ErrUnsupported), robotgo.ErrNotSupported) ||
		!errors.Is(agentAccessibilityError(context.Canceled), context.Canceled) {
		t.Fatal("accessibility error mapping lost a stable error class")
	}
}
