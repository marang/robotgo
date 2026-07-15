//go:build cgo && darwin

package robotgo

import "fmt"

func nativePlatformCaptureCapabilities() (FeatureCapability, FeatureCapability) {
	const backend = featureBackendNativeCGO
	capture := FeatureCapability{Backend: backend}
	bounds := FeatureCapability{Backend: backend}
	displays := DisplaysNum()
	if displays <= 0 {
		capture.Reason = "CoreGraphics reports no active displays"
		capture.Notes = "an active macOS GUI session is required"
		bounds.Reason = capture.Reason
		bounds.Notes = capture.Notes
		return capture, bounds
	}
	bounds.Available = true
	bounds.Reason = "native CoreGraphics display enumeration is available"
	bounds.Notes = fmt.Sprintf("active displays=%d", displays)
	granted, supported, err := darwinScreenCapturePreflight()
	if err != nil {
		capture.Reason = err.Error()
		capture.Notes = "Screen Recording permission could not be inspected"
		return capture, bounds
	}
	if supported && !granted {
		capture.Reason = ErrPermissionDenied.Error()
		capture.Notes = "grant Screen Recording access to this application in System Settings"
		return capture, bounds
	}
	capture.Available = true
	capture.Reason = "native CoreGraphics capture is available"
	if supported {
		capture.Notes = "Screen Recording permission is granted"
	} else {
		capture.Notes = "this macOS version does not expose a permission preflight; capture validates access"
	}
	return capture, bounds
}
