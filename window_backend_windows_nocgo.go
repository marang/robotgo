//go:build windows && !cgo

package robotgo

import (
	"github.com/marang/robotgo/internal/windowbackend"
	"github.com/marang/robotgo/internal/windowswindow"
)

var pureGoWindowsWindowBackend = windowswindow.NewNative()

func platformPureGoWindowBackend() windowbackend.Backend {
	return pureGoWindowsWindowBackend
}

func platformPureGoWindowCapability() FeatureCapability {
	return FeatureCapability{
		Available: true,
		Backend:   featureBackendPureGoWindows,
		Reason:    "Pure-Go Win32 window introspection and control are available",
		Notes:     "PID targets prefer visible unowned top-level windows; Windows foreground-activation policy still applies",
	}
}
