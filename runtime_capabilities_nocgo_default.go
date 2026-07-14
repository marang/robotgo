//go:build !cgo && !darwin

package robotgo

import "runtime"

func pureGoPlatformCaptureCapabilities() (FeatureCapability, FeatureCapability) {
	backend := featureBackendPureGoPrefix + runtime.GOOS
	if runtime.GOOS == "windows" {
		backend = featureBackendPureGoWindows
	}
	if pureGoScreenshotBackend(runtime.GOOS) == BackendX11 {
		backend = featureBackendPureGoX11
	}
	compiled := pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH)
	capture := FeatureCapability{
		Available: compiled,
		Backend:   backend,
		Reason:    "Pure-Go capture backend is compiled; runtime access is checked when capture starts",
	}
	bounds := FeatureCapability{
		Available: compiled,
		Backend:   backend,
		Reason:    "Pure-Go display enumeration is compiled; runtime access is checked when queried",
	}
	if !compiled {
		reason := "Pure-Go capture and display enumeration are unavailable on " + runtime.GOOS + "/" + runtime.GOARCH
		capture.Reason = reason
		capture.Notes = ErrNotSupported.Error()
		bounds.Reason = reason
		bounds.Notes = ErrNotSupported.Error()
	}
	return capture, bounds
}
