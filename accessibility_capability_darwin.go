//go:build darwin

package robotgo

import (
	"context"

	"github.com/marang/robotgo/internal/accessibility"
)

func accessibilityCapability() FeatureCapability {
	capability := accessibility.Probe(context.Background())
	reason := capability.Reason
	if capability.PermissionDenied {
		reason = ErrPermissionDenied.Error() + ": " + reason
	}
	return FeatureCapability{
		Available: capability.Available,
		Backend:   capability.Backend,
		Reason:    reason,
		Notes:     capability.Notes,
	}
}
