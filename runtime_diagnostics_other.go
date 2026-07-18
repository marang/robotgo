//go:build !linux

package robotgo

import (
	"context"
	"runtime"
	"strings"
)

func platformRuntimeDiagnosticDetails(
	_ context.Context,
	capabilities RuntimeCapabilities,
) ([]RuntimeProtocolDiagnostic, []RuntimePermissionDiagnostic) {
	switch runtime.GOOS {
	case "darwin":
		return nil, darwinRuntimePermissions(capabilities)
	case "windows":
		return nil, []RuntimePermissionDiagnostic{
			nativeAPIPermission(runtimeFeatureCapture, "Windows desktop access", capabilities.Capture),
			nativeAPIPermission(runtimeFeatureKeyboard, "Windows input policy", capabilities.Keyboard),
			nativeAPIPermission(runtimeFeatureMouse, "Windows input policy", capabilities.Mouse),
			nativeAPIPermission(runtimeFeatureWindow, "Windows process integrity policy", capabilities.Window),
		}
	default:
		return nil, []RuntimePermissionDiagnostic{
			{
				Feature: runtimeFeatureDesktop,
				Name:    "platform support",
				State:   RuntimePermissionUnavailable,
				Reason:  ErrNotSupported.Error(),
			},
		}
	}
}

func darwinRuntimePermissions(
	capabilities RuntimeCapabilities,
) []RuntimePermissionDiagnostic {
	return []RuntimePermissionDiagnostic{
		{
			Feature: runtimeFeatureCapture,
			Name:    "Screen Recording",
			State: permissionFromCapability(
				capabilities.Capture,
				"permission is granted",
			),
			Reason: capabilities.Capture.Reason,
		},
		{
			Feature: runtimeFeatureKeyboard,
			Name:    "Accessibility",
			State: permissionFromCapability(
				capabilities.Keyboard,
				"backend is ready",
			),
			Reason: capabilities.Keyboard.Reason,
		},
		{
			Feature: runtimeFeatureMouse,
			Name:    "Accessibility",
			State: permissionFromCapability(
				capabilities.Mouse,
				"backend is ready",
			),
			Reason: capabilities.Mouse.Reason,
		},
		{
			Feature: runtimeFeatureWindow,
			Name:    "Accessibility",
			State: permissionFromCapability(
				capabilities.Window,
				"backend is ready",
			),
			Reason: capabilities.Window.Reason,
		},
	}
}

func permissionFromCapability(
	capability FeatureCapability,
	grantedMarker string,
) RuntimePermissionState {
	reason := strings.ToLower(capability.Reason)
	notes := strings.ToLower(capability.Notes)
	if strings.Contains(reason, strings.ToLower(ErrPermissionDenied.Error())) {
		return RuntimePermissionDenied
	}
	if strings.Contains(notes, "cannot be preflighted") ||
		strings.Contains(notes, "could not be inspected") ||
		strings.Contains(notes, "validates access") {
		return RuntimePermissionUnknown
	}
	if capability.Available &&
		(strings.Contains(notes, strings.ToLower(grantedMarker)) ||
			strings.Contains(reason, strings.ToLower(grantedMarker))) {
		return RuntimePermissionGranted
	}
	if capability.Available {
		return RuntimePermissionUnknown
	}
	return RuntimePermissionUnavailable
}

func nativeAPIPermission(
	feature, name string,
	capability FeatureCapability,
) RuntimePermissionDiagnostic {
	state := RuntimePermissionUnavailable
	if capability.Available {
		state = RuntimePermissionNotRequired
	}
	return RuntimePermissionDiagnostic{
		Feature: feature,
		Name:    name,
		State:   state,
		Reason:  capability.Reason,
	}
}
