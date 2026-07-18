//go:build !windows && !linux && !cgo

package robotgo

import "github.com/marang/robotgo/internal/windowbackend"

func platformPureGoWindowBackend() windowbackend.Backend {
	return nil
}

func platformPureGoWindowCapability() FeatureCapability {
	return FeatureCapability{
		Reason: ErrNotSupported.Error(),
		Notes:  "no matching Pure-Go window backend is active in this build",
	}
}
