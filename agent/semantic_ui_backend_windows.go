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

func actPlatformUIElement(ctx context.Context, request uiBackendElementAction) (uiBackendElementActionResult, error) {
	target := accessibility.Target{ExpectedTitle: request.Target.ExpectedTitle}
	if request.Target.Kind == WindowTargetHandle {
		target.NativeWindowHandle = request.Target.Target
	} else {
		target.ProcessID = request.Target.Target
	}
	return actAccessibilityUI(ctx, request, target)
}

func checkPlatformUIElement(ctx context.Context, request uiBackendElementAction) (uiBackendElementConditionResult, error) {
	target := accessibility.Target{ExpectedTitle: request.Target.ExpectedTitle}
	if request.Target.Kind == WindowTargetHandle {
		target.NativeWindowHandle = request.Target.Target
	} else {
		target.ProcessID = request.Target.Target
	}
	return checkAccessibilityUI(ctx, request, target)
}
