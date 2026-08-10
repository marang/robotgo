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
	return AccessibilityActionResult{CleanupComplete: true}, ErrUnsupported
}

func checkAccessibility(context.Context, AccessibilityActionRequest) (AccessibilityConditionResult, error) {
	return AccessibilityConditionResult{CleanupComplete: true}, ErrUnsupported
}
