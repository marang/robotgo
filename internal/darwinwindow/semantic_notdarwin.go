//go:build !darwin

package darwinwindow

import "context"

func inspectAccessibility(
	context.Context,
	AccessibilityTarget,
	AccessibilityLimits,
) (AccessibilitySnapshot, error) {
	return AccessibilitySnapshot{}, ErrUnsupported
}

func actAccessibility(context.Context, AccessibilityActionRequest) (AccessibilityActionResult, error) {
	return AccessibilityActionResult{}, ErrUnsupported
}
