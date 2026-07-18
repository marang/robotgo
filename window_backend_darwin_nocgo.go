//go:build darwin && !cgo

package robotgo

import (
	"errors"

	"github.com/marang/robotgo/internal/darwinwindow"
	"github.com/marang/robotgo/internal/windowbackend"
)

var pureGoDarwinWindowBackend = darwinwindow.NewNative()

func platformPureGoWindowBackend() windowbackend.Backend {
	return pureGoDarwinWindowBackend
}

func closePureGoPlatformWindow() error {
	return translatePureGoWindowError(pureGoDarwinWindowBackend.CloseSystem())
}

func platformPureGoWindowCapability() FeatureCapability {
	capability := FeatureCapability{
		Backend: featureBackendPureGoMacOSWindow,
		Notes: "active/PID/CGWindowID resolution, title, AX frame bounds, " +
			"activation, minimize/restore, minimized state, and close are supported; " +
			"CloseWindowKill can request graceful close but has no safe force-kill fallback; " +
			"client bounds equal the AX frame; maximize and topmost are explicitly unsupported; " +
			"CGWindowID-to-Accessibility mapping requires the runtime-resolved macOS bridge",
	}
	err := pureGoDarwinWindowBackend.Ready()
	switch {
	case err == nil:
		capability.Available = true
		capability.Reason = "Pure-Go macOS Accessibility window backend is ready"
	case errors.Is(err, darwinwindow.ErrPermission):
		capability.Reason = ErrPermissionDenied.Error()
		capability.Notes += "; grant Accessibility access in System Settings > Privacy & Security"
	case errors.Is(err, darwinwindow.ErrUnsupported):
		capability.Reason = translatePureGoWindowError(
			errors.Join(windowbackend.ErrUnsupported, err),
		).Error()
	default:
		capability.Reason = err.Error()
	}
	return capability
}
