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
