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

func actPlatformUIElement(context.Context, uiBackendElementAction) (bool, error) {
	return false, fmt.Errorf("%w: no native accessibility action adapter is active", robotgo.ErrNotSupported)
}
