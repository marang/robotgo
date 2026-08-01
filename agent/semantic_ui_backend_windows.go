//go:build windows

package agent

import (
	"context"

	"github.com/marang/robotgo/internal/accessibility"
)

func inspectPlatformUI(ctx context.Context, target uiBackendTarget, limits uiBackendLimits) (uiBackendSnapshot, error) {
	nativeTarget := accessibility.Target{ExpectedTitle: target.ExpectedTitle}
	if target.Kind == WindowTargetHandle {
		nativeTarget.NativeWindowHandle = target.Target
	} else {
		nativeTarget.ProcessID = target.Target
	}
	return inspectAccessibilityUI(ctx, nativeTarget, limits)
}
