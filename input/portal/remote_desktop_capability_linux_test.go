//go:build linux

package portal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestCapabilitySupports(t *testing.T) {
	capability := Capability{
		Version: 2, AvailableDevices: DeviceKeyboard | DevicePointer,
		AvailableSources:     SourceMonitor | SourceWindow,
		AvailableCursorModes: CursorHidden | CursorMetadata,
	}
	if !capability.Supports(DeviceKeyboard | DevicePointer) {
		t.Fatal("expected keyboard and pointer to be supported")
	}
	if !capability.SupportsSources(SourceMonitor|SourceWindow) || capability.SupportsSources(SourceVirtual) {
		t.Fatalf("unexpected source capability: %+v", capability)
	}
	if !capability.SupportsCursorMode(CursorMetadata) || capability.SupportsCursorMode(CursorEmbedded) {
		t.Fatalf("unexpected cursor capability: %+v", capability)
	}
}

func TestProbeRemoteDesktopCapabilityHandlesOptionalScreenCastErrors(t *testing.T) {
	baseProperty := func(_ context.Context, iface, property string) (uint32, error) {
		if iface == remoteDesktopInterface {
			switch property {
			case remoteDesktopVersionKey:
				return 2, nil
			case availableDevicesKey:
				return uint32(DeviceKeyboard | DevicePointer), nil
			}
		}
		return 0, dbus.NewError(dbusUnknownInterface, nil)
	}
	screenCastCalls := 0
	capability, err := probeRemoteDesktopCapability(context.Background(), func(ctx context.Context, iface, property string) (uint32, error) {
		if iface == screenCastInterface {
			screenCastCalls++
		}
		return baseProperty(ctx, iface, property)
	})
	if err != nil {
		t.Fatalf("optional ScreenCast interface error was not ignored: %v", err)
	}
	if capability.Version != 2 || capability.AvailableDevices != DeviceKeyboard|DevicePointer || capability.ScreenCastVersion != 0 || capability.ScreenCastIssue == "" {
		t.Fatalf("capability = %+v", capability)
	}
	if screenCastCalls != 1 {
		t.Fatalf("ScreenCast property calls = %d, want one definitive interface probe", screenCastCalls)
	}

	deadlineCtx, cancel := context.WithCancel(context.Background())
	cancel()
	capability, err = probeRemoteDesktopCapability(deadlineCtx, func(ctx context.Context, iface, property string) (uint32, error) {
		if iface == remoteDesktopInterface {
			return baseProperty(ctx, iface, property)
		}
		return 0, ctx.Err()
	})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrUnavailable) || capability.AvailableDevices != DeviceKeyboard|DevicePointer || !strings.Contains(capability.ScreenCastIssue, context.Canceled.Error()) {
		t.Fatalf("cancelled ScreenCast probe = (%+v, %v), want usable RemoteDesktop with ScreenCast issue", capability, err)
	}

	decodeErr := errors.New("invalid property type")
	capability, err = probeRemoteDesktopCapability(context.Background(), func(ctx context.Context, iface, property string) (uint32, error) {
		if iface == remoteDesktopInterface {
			return baseProperty(ctx, iface, property)
		}
		return 0, decodeErr
	})
	if !errors.Is(err, decodeErr) || !errors.Is(err, ErrUnavailable) || capability.AvailableDevices != DeviceKeyboard|DevicePointer || !strings.Contains(capability.ScreenCastIssue, decodeErr.Error()) {
		t.Fatalf("malformed ScreenCast probe = (%+v, %v), want usable RemoteDesktop with ScreenCast issue", capability, err)
	}
}

func TestProbeRemoteDesktopCapabilityPopulatesScreenCastProperties(t *testing.T) {
	capability, err := probeRemoteDesktopCapability(context.Background(), func(_ context.Context, iface, property string) (uint32, error) {
		values := map[string]uint32{
			remoteDesktopInterface + "." + remoteDesktopVersionKey: 2,
			remoteDesktopInterface + "." + availableDevicesKey:     uint32(allDeviceTypes) | 8,
			screenCastInterface + "." + remoteDesktopVersionKey:    6,
			screenCastInterface + "." + availableSourcesKey:        uint32(allSourceTypes) | 8,
			screenCastInterface + "." + availableCursorModesKey:    uint32(allCursorModes) | 8,
		}
		return values[iface+"."+property], nil
	})
	if err != nil {
		t.Fatalf("probeRemoteDesktopCapability error: %v", err)
	}
	want := Capability{
		Version: 2, AvailableDevices: allDeviceTypes,
		ScreenCastVersion: 6, AvailableSources: allSourceTypes,
		AvailableCursorModes: allCursorModes,
	}
	if capability != want {
		t.Fatalf("capability = %+v, want %+v", capability, want)
	}
}

func TestProbeRemoteDesktopCapabilityContinuesAfterUnknownProperty(t *testing.T) {
	capability, err := probeRemoteDesktopCapability(context.Background(), func(_ context.Context, iface, property string) (uint32, error) {
		if iface == remoteDesktopInterface {
			if property == remoteDesktopVersionKey {
				return 2, nil
			}
			return uint32(DevicePointer), nil
		}
		if property == remoteDesktopVersionKey {
			return 0, dbus.NewError(dbusUnknownProperty, nil)
		}
		if property == availableSourcesKey {
			return uint32(SourceMonitor), nil
		}
		return uint32(CursorHidden), nil
	})
	if err != nil {
		t.Fatalf("probeRemoteDesktopCapability error: %v", err)
	}
	if capability.ScreenCastVersion != 0 || capability.AvailableSources != SourceMonitor || capability.AvailableCursorModes != CursorHidden {
		t.Fatalf("capability = %+v", capability)
	}
}

func TestValidateDevicesRejectsUnmappedScreenCastDevices(t *testing.T) {
	if err := validateOptions(OpenOptions{Devices: DeviceTouchscreen}); !errors.Is(err, ErrScreenCastRequired) {
		t.Fatalf("touchscreen without ScreenCast error = %v, want ErrScreenCastRequired", err)
	}
}

func TestValidateRemoteDesktopScreenCastOptions(t *testing.T) {
	tests := []OpenOptions{
		{Devices: DevicePointer, Sources: SourceType(8)},
		{Devices: DevicePointer, Sources: SourceMonitor, CursorMode: CursorHidden | CursorEmbedded},
		{Devices: DevicePointer, PersistMode: PersistMode(3)},
		{Devices: DevicePointer, Multiple: true},
		{Devices: DevicePointer, CursorMode: CursorHidden},
	}
	for _, options := range tests {
		if err := validateOptions(options); err == nil {
			t.Fatalf("options %+v unexpectedly accepted", options)
		}
	}
}
