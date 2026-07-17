//go:build linux

package robotgo

import (
	"context"
	"fmt"

	portalpkg "github.com/marang/robotgo/screen/portal"
)

const (
	protocolWaylandScreencopy      = "zwlr_screencopy_manager_v1"
	protocolWaylandVirtualKeyboard = "zwp_virtual_keyboard_manager_v1"
	protocolWaylandVirtualPointer  = "zwlr_virtual_pointer_manager_v1"
	protocolPortalRemoteDesktop    = "org.freedesktop.portal.RemoteDesktop"
	protocolPortalScreenCast       = "org.freedesktop.portal.ScreenCast"
	protocolXTest                  = "XTEST"
	backendWaylandScreencopy       = "wayland+screencopy"
	backendWaylandVirtualKeyboard  = "wayland-virtual-keyboard"
	backendWaylandVirtualPointer   = "wayland-virtual-pointer"
	backendPortalRemoteDesktop     = "portal-remote-desktop"
	backendPortalScreenCast        = "portal-screencast+pipewire"
)

var (
	runtimeNativeWaylandProtocolProbe = nativeWaylandProtocolVersions
	runtimeX11ProtocolProbe           = nativeX11ProtocolVersion
	runtimeRemoteDesktopProbe         = GetRemoteDesktopInputStatus
	runtimeScreenCastProbe            = portalpkg.ProbeScreenCast
)

func platformRuntimeDiagnosticDetails(
	ctx context.Context,
	capabilities RuntimeCapabilities,
) ([]RuntimeProtocolDiagnostic, []RuntimePermissionDiagnostic) {
	if capabilities.Runtime.DisplayServer == DisplayServerX11 {
		return x11RuntimeDiagnosticDetails(capabilities)
	}
	if capabilities.Runtime.DisplayServer != DisplayServerWayland {
		return nil, unavailableDisplayPermissions(capabilities)
	}
	return waylandRuntimeDiagnosticDetails(ctx, capabilities)
}

func x11RuntimeDiagnosticDetails(
	capabilities RuntimeCapabilities,
) ([]RuntimeProtocolDiagnostic, []RuntimePermissionDiagnostic) {
	major, minor, negotiated := runtimeX11ProtocolProbe()
	version := ""
	if negotiated {
		version = fmt.Sprintf("%d.%d", major, minor)
	}
	protocols := []RuntimeProtocolDiagnostic{
		{
			Feature:    runtimeFeatureKeyboard,
			Name:       protocolXTest,
			Version:    version,
			Negotiated: negotiated,
			Reason:     capabilities.Keyboard.Reason,
		},
		{
			Feature:    runtimeFeatureMouse,
			Name:       protocolXTest,
			Version:    version,
			Negotiated: negotiated,
			Reason:     capabilities.Mouse.Reason,
		},
	}
	permissions := []RuntimePermissionDiagnostic{
		notRequiredPermission(runtimeFeatureCapture, "X11 server access", capabilities.Capture.Reason),
		notRequiredPermission(runtimeFeatureKeyboard, "X11 XTEST access", capabilities.Keyboard.Reason),
		notRequiredPermission(runtimeFeatureMouse, "X11 XTEST access", capabilities.Mouse.Reason),
		notRequiredPermission(runtimeFeatureWindow, "X11 window access", capabilities.Window.Reason),
	}
	return protocols, permissions
}

func waylandRuntimeDiagnosticDetails(
	ctx context.Context,
	capabilities RuntimeCapabilities,
) ([]RuntimeProtocolDiagnostic, []RuntimePermissionDiagnostic) {
	native := runtimeNativeWaylandProtocolProbe()
	protocols := make([]RuntimeProtocolDiagnostic, 0, 5)
	protocols = appendNativeWaylandProtocol(
		protocols, runtimeFeatureCapture, protocolWaylandScreencopy, native.Screencopy,
		capabilities.Capture.Backend == backendWaylandScreencopy,
		capabilities.Capture.Reason,
	)
	protocols = appendNativeWaylandProtocol(
		protocols, runtimeFeatureKeyboard, protocolWaylandVirtualKeyboard,
		native.VirtualKeyboard,
		capabilities.Keyboard.Backend == backendWaylandVirtualKeyboard,
		capabilities.Keyboard.Reason,
	)
	protocols = appendNativeWaylandProtocol(
		protocols, runtimeFeatureMouse, protocolWaylandVirtualPointer,
		native.VirtualPointer,
		capabilities.Mouse.Backend == backendWaylandVirtualPointer,
		capabilities.Mouse.Reason,
	)

	remoteStatus, remoteErr := runtimeRemoteDesktopProbe(ctx)
	remoteReason := remoteStatus.Reason
	if remoteReason == "" && remoteErr != nil {
		remoteReason = remoteErr.Error()
	}
	protocols = append(protocols, RuntimeProtocolDiagnostic{
		Feature:    runtimeFeatureRemoteDesktop,
		Name:       protocolPortalRemoteDesktop,
		Version:    uintVersion(remoteStatus.PortalVersion),
		Negotiated: remoteStatus.PortalAvailable && remoteStatus.PortalVersion != 0,
		Reason:     remoteReason,
	})

	screenCast, screenCastErr := runtimeScreenCastProbe(ctx)
	screenCastReason := ""
	if screenCastErr != nil {
		screenCastReason = screenCastErr.Error()
	} else if !screenCast.PipeWireReady {
		screenCastReason = "portal detected, but PipeWire file-descriptor transfer is unavailable"
	}
	protocols = append(protocols, RuntimeProtocolDiagnostic{
		Feature:    runtimeFeatureCapture,
		Name:       protocolPortalScreenCast,
		Version:    uintVersion(screenCast.Version),
		Negotiated: screenCastErr == nil && screenCast.Version != 0,
		Reason:     screenCastReason,
	})

	remotePermission := RuntimePermissionDiagnostic{
		Feature: runtimeFeatureRemoteDesktop,
		Name:    "desktop portal consent",
		State:   runtimeRemoteDesktopPermission(remoteStatus.Permission),
		Reason:  remoteReason,
	}
	if remoteErr != nil && !remoteStatus.PortalAvailable &&
		remotePermission.State == RuntimePermissionNotRequested {
		remotePermission.State = RuntimePermissionUnavailable
	}
	permissions := []RuntimePermissionDiagnostic{
		capturePermission(capabilities.Capture, screenCastErr),
		inputPermission(runtimeFeatureKeyboard, capabilities.Keyboard, remotePermission),
		inputPermission(runtimeFeatureMouse, capabilities.Mouse, remotePermission),
		remotePermission,
	}
	return protocols, permissions
}

func appendNativeWaylandProtocol(
	protocols []RuntimeProtocolDiagnostic,
	feature, name string,
	version uint32,
	selected bool,
	reason string,
) []RuntimeProtocolDiagnostic {
	if version == 0 && !selected {
		return protocols
	}
	return append(protocols, RuntimeProtocolDiagnostic{
		Feature:    feature,
		Name:       name,
		Version:    uintVersion(version),
		Negotiated: version != 0,
		Reason:     reason,
	})
}

func capturePermission(
	capability FeatureCapability,
	screenCastErr error,
) RuntimePermissionDiagnostic {
	permission := RuntimePermissionDiagnostic{
		Feature: runtimeFeatureCapture,
		Name:    "desktop capture consent",
		State:   RuntimePermissionUnavailable,
		Reason:  capability.Reason,
	}
	switch capability.Backend {
	case backendWaylandScreencopy:
		permission.State = RuntimePermissionNotRequired
	case backendPortalScreenCast:
		permission.State = RuntimePermissionGranted
	case string(BackendPortal):
		permission.State = RuntimePermissionNotRequested
	default:
		if screenCastErr == nil {
			permission.State = RuntimePermissionNotRequested
		}
	}
	return permission
}

func inputPermission(
	feature string,
	capability FeatureCapability,
	remote RuntimePermissionDiagnostic,
) RuntimePermissionDiagnostic {
	if capability.Backend == backendPortalRemoteDesktop || capability.Fallback {
		remote.Feature = feature
		return remote
	}
	state := RuntimePermissionUnavailable
	if capability.Available {
		state = RuntimePermissionNotRequired
	}
	return RuntimePermissionDiagnostic{
		Feature: feature,
		Name:    "Wayland compositor input policy",
		State:   state,
		Reason:  capability.Reason,
	}
}

func unavailableDisplayPermissions(
	capabilities RuntimeCapabilities,
) []RuntimePermissionDiagnostic {
	return []RuntimePermissionDiagnostic{
		{
			Feature: runtimeFeatureCapture,
			Name:    "desktop session",
			State:   RuntimePermissionUnavailable,
			Reason:  capabilities.Capture.Reason,
		},
		{
			Feature: runtimeFeatureKeyboard,
			Name:    "desktop session",
			State:   RuntimePermissionUnavailable,
			Reason:  capabilities.Keyboard.Reason,
		},
		{
			Feature: runtimeFeatureMouse,
			Name:    "desktop session",
			State:   RuntimePermissionUnavailable,
			Reason:  capabilities.Mouse.Reason,
		},
	}
}

func notRequiredPermission(
	feature, name, reason string,
) RuntimePermissionDiagnostic {
	return RuntimePermissionDiagnostic{
		Feature: feature,
		Name:    name,
		State:   RuntimePermissionNotRequired,
		Reason:  reason,
	}
}

func runtimeRemoteDesktopPermission(
	status RemoteDesktopPermissionStatus,
) RuntimePermissionState {
	switch status {
	case RemoteDesktopPermissionNotRequested:
		return RuntimePermissionNotRequested
	case RemoteDesktopPermissionGranted:
		return RuntimePermissionGranted
	case RemoteDesktopPermissionClosed:
		return RuntimePermissionClosed
	case RemoteDesktopPermissionCancelled:
		return RuntimePermissionCancelled
	case RemoteDesktopPermissionTimedOut:
		return RuntimePermissionTimedOut
	case RemoteDesktopPermissionDenied:
		return RuntimePermissionDenied
	case RemoteDesktopPermissionFailed:
		return RuntimePermissionFailed
	case RemoteDesktopPermissionUnavailable:
		return RuntimePermissionUnavailable
	default:
		return RuntimePermissionUnknown
	}
}

func uintVersion(version uint32) string {
	if version == 0 {
		return ""
	}
	return fmt.Sprint(version)
}
