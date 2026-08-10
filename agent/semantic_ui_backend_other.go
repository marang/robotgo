//go:build !darwin && !linux && !windows

package agent

import (
	"context"
	"fmt"

	robotgo "github.com/marang/robotgo"
)

func inspectPlatformUI(context.Context, uiBackendTarget, uiBackendLimits) (uiBackendSnapshot, error) {
	return uiBackendSnapshot{}, fmt.Errorf("%w: no native accessibility adapter is active", robotgo.ErrNotSupported)
}

func actPlatformUIElement(context.Context, uiBackendElementAction) (uiBackendElementActionResult, error) {
	return uiBackendElementActionResult{CleanupComplete: true},
		fmt.Errorf("%w: no native accessibility action adapter is active", robotgo.ErrNotSupported)
}

func checkPlatformUIElement(context.Context, uiBackendElementAction) (uiBackendElementConditionResult, error) {
	return uiBackendElementConditionResult{CleanupComplete: true},
		fmt.Errorf("%w: no native accessibility check adapter is active", robotgo.ErrNotSupported)
}
