//go:build cgo

package robotgo

import "runtime"

func runtimeCapabilities() RuntimeCapabilities {
	unsupported := FeatureCapability{
		Reason: ErrNotSupported.Error(),
		Notes:  "no matching runtime backend is active on this platform",
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
	} else {
		native := FeatureCapability{
			Available: true,
			Backend:   featureBackendNativeCGO,
			Reason:    "native CGO backend is compiled for this platform",
			Notes:     "runtime permissions are validated when an operation starts",
		}
		result.Capture, result.Bounds = nativePlatformCaptureCapabilities()
		result.Keyboard = native
		result.Mouse = native
		result.Window = native
		result.Hook = native
		result.Events = native
	}
	result.Process = FeatureCapability{
		Available: true,
		Backend:   featureBackendGoProcess,
		Reason:    "process helpers are compiled for this platform",
	}
	result.Clipboard = FeatureCapability{
		Available: true,
		Backend:   featureBackendGoClipboard,
		Reason:    "clipboard helpers are compiled for this platform",
	}
	return result
}
