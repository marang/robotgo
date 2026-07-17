//go:build linux

package robotgo

import (
	"context"
	"errors"
	"reflect"
	"testing"

	portalpkg "github.com/marang/robotgo/screen/portal"
)

func stubRuntimeDiagnosticProbes(t *testing.T) {
	t.Helper()
	oldNative := runtimeNativeWaylandProtocolProbe
	oldX11 := runtimeX11ProtocolProbe
	oldRemote := runtimeRemoteDesktopProbe
	oldScreenCast := runtimeScreenCastProbe
	t.Cleanup(func() {
		runtimeNativeWaylandProtocolProbe = oldNative
		runtimeX11ProtocolProbe = oldX11
		runtimeRemoteDesktopProbe = oldRemote
		runtimeScreenCastProbe = oldScreenCast
	})
}

func TestWaylandRuntimeDiagnosticsReportProtocolsAndPermissions(t *testing.T) {
	stubRuntimeDiagnosticProbes(t)
	runtimeNativeWaylandProtocolProbe = func() nativeWaylandProtocolInfo {
		return nativeWaylandProtocolInfo{
			Screencopy:      3,
			VirtualKeyboard: 1,
			VirtualPointer:  2,
		}
	}
	runtimeRemoteDesktopProbe = func(ctx context.Context) (RemoteDesktopInputStatus, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("RemoteDesktop diagnostic probe received no deadline")
		}
		return RemoteDesktopInputStatus{
			PortalAvailable:   true,
			PortalVersion:     2,
			ScreenCastVersion: 4,
			Permission:        RemoteDesktopPermissionGranted,
			SessionActive:     true,
			Reason:            "portal consent session is active",
		}, nil
	}
	runtimeScreenCastProbe = func(ctx context.Context) (portalpkg.ScreenCastCapability, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("ScreenCast diagnostic probe received no deadline")
		}
		return portalpkg.ScreenCastCapability{
			Version:       4,
			PipeWireReady: true,
		}, nil
	}
	capabilities := RuntimeCapabilities{
		Runtime: RuntimeBackendInfo{
			GOOS:          "linux",
			DisplayServer: DisplayServerWayland,
		},
		Capture: FeatureCapability{
			Available: true,
			Backend:   backendWaylandScreencopy,
			Reason:    "native capture ready",
		},
		Keyboard: FeatureCapability{
			Available: true,
			Fallback:  true,
			Backend:   backendPortalRemoteDesktop,
			Reason:    "portal keyboard session ready",
		},
		Mouse: FeatureCapability{
			Available: true,
			Backend:   backendWaylandVirtualPointer,
			Reason:    "native pointer ready",
		},
	}

	ctx, cancel := boundedDiagnosticContext(context.Background())
	defer cancel()
	protocols, permissions := waylandRuntimeDiagnosticDetails(ctx, capabilities)
	wantProtocols := []RuntimeProtocolDiagnostic{
		{
			Feature:    "capture",
			Name:       protocolWaylandScreencopy,
			Version:    "3",
			Negotiated: true,
			Reason:     "native capture ready",
		},
		{
			Feature:    "keyboard",
			Name:       protocolWaylandVirtualKeyboard,
			Version:    "1",
			Negotiated: true,
			Reason:     "portal keyboard session ready",
		},
		{
			Feature:    "mouse",
			Name:       protocolWaylandVirtualPointer,
			Version:    "2",
			Negotiated: true,
			Reason:     "native pointer ready",
		},
		{
			Feature:    "remote-desktop",
			Name:       protocolPortalRemoteDesktop,
			Version:    "2",
			Negotiated: true,
			Reason:     "portal consent session is active",
		},
		{
			Feature:    "capture",
			Name:       protocolPortalScreenCast,
			Version:    "4",
			Negotiated: true,
		},
	}
	if !reflect.DeepEqual(protocols, wantProtocols) {
		t.Fatalf("protocol diagnostics = %#v, want %#v", protocols, wantProtocols)
	}
	wantPermissions := []RuntimePermissionDiagnostic{
		{
			Feature: "capture",
			Name:    "desktop capture consent",
			State:   RuntimePermissionNotRequired,
			Reason:  "native capture ready",
		},
		{
			Feature: "keyboard",
			Name:    "desktop portal consent",
			State:   RuntimePermissionGranted,
			Reason:  "portal consent session is active",
		},
		{
			Feature: "mouse",
			Name:    "Wayland compositor input policy",
			State:   RuntimePermissionNotRequired,
			Reason:  "native pointer ready",
		},
		{
			Feature: "remote-desktop",
			Name:    "desktop portal consent",
			State:   RuntimePermissionGranted,
			Reason:  "portal consent session is active",
		},
	}
	if !reflect.DeepEqual(permissions, wantPermissions) {
		t.Fatalf("permission diagnostics = %#v, want %#v", permissions, wantPermissions)
	}
}

func TestWaylandRuntimeDiagnosticsPreserveProbeFailures(t *testing.T) {
	stubRuntimeDiagnosticProbes(t)
	runtimeNativeWaylandProtocolProbe = func() nativeWaylandProtocolInfo {
		return nativeWaylandProtocolInfo{}
	}
	remoteErr := errors.New("RemoteDesktop interface missing")
	screenCastErr := errors.New("ScreenCast interface missing")
	runtimeRemoteDesktopProbe = func(context.Context) (RemoteDesktopInputStatus, error) {
		return RemoteDesktopInputStatus{
			Permission: RemoteDesktopPermissionUnavailable,
			Reason:     "install a RemoteDesktop portal backend",
		}, remoteErr
	}
	runtimeScreenCastProbe = func(context.Context) (portalpkg.ScreenCastCapability, error) {
		return portalpkg.ScreenCastCapability{}, screenCastErr
	}
	capabilities := RuntimeCapabilities{
		Runtime: RuntimeBackendInfo{DisplayServer: DisplayServerWayland},
		Capture: FeatureCapability{
			Backend: "portal",
			Reason:  "portal capture unavailable",
		},
		Keyboard: FeatureCapability{Reason: "keyboard unavailable"},
		Mouse:    FeatureCapability{Reason: "mouse unavailable"},
	}
	protocols, permissions := waylandRuntimeDiagnosticDetails(context.Background(), capabilities)
	if protocols[0].Name != protocolPortalRemoteDesktop ||
		protocols[0].Negotiated ||
		protocols[0].Reason != "install a RemoteDesktop portal backend" {
		t.Fatalf("RemoteDesktop failure diagnostics = %+v", protocols[0])
	}
	if protocols[1].Name != protocolPortalScreenCast ||
		protocols[1].Negotiated ||
		protocols[1].Reason != screenCastErr.Error() {
		t.Fatalf("ScreenCast failure diagnostics = %+v", protocols[1])
	}
	if permissions[0].State != RuntimePermissionNotRequested {
		t.Fatalf("portal capture permission = %q, want not-requested", permissions[0].State)
	}
	if permissions[3].State != RuntimePermissionUnavailable {
		t.Fatalf("RemoteDesktop permission = %q, want unavailable", permissions[3].State)
	}
}

func TestX11RuntimeDiagnosticsReportNegotiatedXTestVersion(t *testing.T) {
	stubRuntimeDiagnosticProbes(t)
	runtimeX11ProtocolProbe = func() (major, minor int, negotiated bool) {
		return 2, 2, true
	}
	capabilities := RuntimeCapabilities{
		Keyboard: FeatureCapability{Available: true, Reason: "XTEST ready"},
		Capture:  FeatureCapability{Available: true, Reason: "X11 ready"},
		Window:   FeatureCapability{Available: true, Reason: "X11 ready"},
	}
	protocols, permissions := x11RuntimeDiagnosticDetails(capabilities)
	if len(protocols) != 2 ||
		protocols[0].Feature != "keyboard" ||
		protocols[1].Feature != "mouse" ||
		protocols[0].Version != "2.2" ||
		protocols[1].Version != "2.2" ||
		!protocols[0].Negotiated ||
		!protocols[1].Negotiated {
		t.Fatalf("XTEST protocol diagnostics = %+v", protocols)
	}
	for _, permission := range permissions {
		if permission.State != RuntimePermissionNotRequired {
			t.Fatalf("X11 permission state = %q, want not-required", permission.State)
		}
	}
}
