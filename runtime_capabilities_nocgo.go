//go:build !cgo

package robotgo

import "runtime"

func runtimeCapabilities() RuntimeCapabilities {
	unsupported := FeatureCapability{
		Reason: ErrNotSupported.Error(),
		Notes:  "no matching Pure-Go backend is active in this build",
	}
	result := RuntimeCapabilities{
		Runtime:       GetRuntimeBackendInfo(),
		Capture:       unsupported,
		Bounds:        unsupported,
		Keyboard:      unsupported,
		Mouse:         unsupported,
		RemoteDesktop: unsupported,
		Window:        unsupported,
		Hook:          unsupported,
		Events:        unsupported,
		Process: FeatureCapability{
			Available: true,
			Backend:   featureBackendPureGoProcess,
			Reason:    "process helpers do not require CGO",
		},
		Clipboard: FeatureCapability{
			Available: true,
			Backend:   featureBackendPureGoClipboard,
			Reason:    "clipboard helpers do not require CGO",
		},
	}
	if runtime.GOOS == "linux" {
		linux := GetLinuxCapabilities()
		result.Capture = linux.Capture
		result.Bounds = linux.Bounds
		result.Keyboard = linux.Keyboard
		result.Mouse = linux.Mouse
		result.RemoteDesktop = linux.RemoteDesktop
		result.Window = linux.Window
		result.Hook = linux.Hook
		result.Events = linux.Events
		return result
	}
	result.Capture, result.Bounds = pureGoPlatformCaptureCapabilities()
	keyboard, mouse := pureGoInputCapabilities()
	if keyboard.Backend != "" {
		result.Keyboard = keyboard
	}
	if mouse.Backend != "" {
		result.Mouse = mouse
	}
	result.Window = pureGoWindowCapability()
	return result
}
