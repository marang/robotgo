//go:build linux && !cgo

package robotgo

import (
	"github.com/marang/robotgo/internal/windowbackend"
	"github.com/marang/robotgo/internal/x11window"
)

var pureGoX11WindowBackend = x11window.NewNative(x11window.Config{
	ResolveDisplay: resolvePureGoX11Display,
})

func platformPureGoWindowBackend() windowbackend.Backend {
	if DetectDisplayServer() != DisplayServerX11 || pureGoX11EnvironmentConflict() {
		return nil
	}
	return pureGoX11WindowBackend
}

func platformPureGoWindowCapability() FeatureCapability {
	if DetectDisplayServer() != DisplayServerX11 {
		return FeatureCapability{
			Reason: ErrNotSupported.Error(),
			Notes:  "Pure-Go X11 window control requires an X11-primary session",
		}
	}
	if pureGoX11EnvironmentConflict() {
		return FeatureCapability{
			Backend: featureBackendPureGoX11,
			Reason:  envXDGSessionType + " selects Wayland while DISPLAY selects X11",
			Notes:   "Pure-Go X11 window control refuses implicit Xwayland fallback",
		}
	}
	return FeatureCapability{
		Available: true,
		Backend:   featureBackendPureGoX11,
		Reason:    "Pure-Go X11 window introspection and EWMH control are selected",
		Notes:     "runtime X server access is validated per operation; mutations require a consistent EWMH manager that advertises the operation",
	}
}
