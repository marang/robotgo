//go:build !cgo && !linux && !darwin

package robotgo

import (
	"runtime"
	"testing"
)

func TestPureGoPlatformCapabilitiesMatchCompiledCaptureBackend(t *testing.T) {
	capabilities := GetRuntimeCapabilities()
	wantAvailable := pureGoScreenshotSupported(runtime.GOOS, runtime.GOARCH)
	if capabilities.Capture.Available != wantAvailable || capabilities.Bounds.Available != wantAvailable {
		t.Fatalf("runtime capabilities = %+v, want capture/bounds available=%v", capabilities, wantAvailable)
	}
	if capabilities.Capture.Backend == "" || capabilities.Bounds.Backend == "" {
		t.Fatalf("runtime capabilities = %+v, want named capture/bounds backends", capabilities)
	}
}
