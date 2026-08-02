//go:build linux

package agent

import (
	"context"
	"fmt"

	robotgo "github.com/marang/robotgo"
	"github.com/marang/robotgo/internal/accessibility"
)

func inspectPlatformUI(ctx context.Context, target uiBackendTarget, limits uiBackendLimits) (uiBackendSnapshot, error) {
	if target.Kind != WindowTargetProcess {
		return uiBackendSnapshot{}, fmt.Errorf("%w: AT-SPI inspection requires a process target", robotgo.ErrNotSupported)
	}
	return inspectAccessibilityUI(ctx, accessibility.Target{
		ProcessID: target.Target, ExpectedTitle: target.ExpectedTitle,
	}, limits)
}

func actPlatformUIElement(ctx context.Context, request uiBackendElementAction) (bool, error) {
	if request.Target.Kind != WindowTargetProcess {
		return false, fmt.Errorf("%w: AT-SPI actions require a process target", robotgo.ErrNotSupported)
	}
	return actAccessibilityUI(ctx, request, accessibility.Target{
		ProcessID: request.Target.Target, ExpectedTitle: request.Target.ExpectedTitle,
	})
}
