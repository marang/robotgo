//go:build linux

package portal

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

func (p *dbusRemoteDesktopPortal) capability(ctx context.Context) (Capability, error) {
	return probeRemoteDesktopCapability(ctx, p.propertyUint32)
}

func probeRemoteDesktopCapability(ctx context.Context, property propertyUint32Func) (Capability, error) {
	version, err := property(ctx, remoteDesktopInterface, remoteDesktopVersionKey)
	if err != nil {
		return Capability{}, fmt.Errorf("%w: query interface version: %w", ErrUnavailable, err)
	}
	devices, err := property(ctx, remoteDesktopInterface, availableDevicesKey)
	if err != nil {
		return Capability{}, fmt.Errorf("%w: query available devices: %w", ErrUnavailable, err)
	}
	capability := Capability{Version: version, AvailableDevices: DeviceType(devices) & allDeviceTypes}
	optional := []struct {
		property string
		apply    func(uint32)
	}{
		{property: remoteDesktopVersionKey, apply: func(value uint32) { capability.ScreenCastVersion = value }},
		{property: availableSourcesKey, apply: func(value uint32) { capability.AvailableSources = SourceType(value) & allSourceTypes }},
		{property: availableCursorModesKey, apply: func(value uint32) { capability.AvailableCursorModes = CursorMode(value) & allCursorModes }},
	}
	for _, item := range optional {
		value, queryErr := property(ctx, screenCastInterface, item.property)
		if queryErr == nil {
			item.apply(value)
			continue
		}
		switch optionalPortalPropertyError(queryErr) {
		case dbusUnknownInterface:
			capability.ScreenCastIssue = "ScreenCast interface unavailable"
			return capability, nil
		case dbusUnknownProperty:
			continue
		}
		// ScreenCast is optional for relative pointer and keyboard input. Preserve
		// the successfully probed RemoteDesktop capability while making the
		// ScreenCast degradation observable to diagnostics.
		capability.ScreenCastIssue = fmt.Sprintf("query ScreenCast property %s: %v", item.property, queryErr)
		return capability, fmt.Errorf("%w: query ScreenCast property %s: %w", ErrUnavailable, item.property, queryErr)
	}
	return capability, nil
}

func optionalPortalPropertyError(err error) string {
	var pointerError *dbus.Error
	if errors.As(err, &pointerError) {
		return pointerError.Name
	}
	var valueError dbus.Error
	if errors.As(err, &valueError) {
		return valueError.Name
	}
	return ""
}

func (p *dbusRemoteDesktopPortal) propertyUint32(ctx context.Context, iface, property string) (uint32, error) {
	call := p.obj.CallWithContext(ctx, propertiesGetMethod, 0, iface, property)
	if call.Err != nil {
		return 0, call.Err
	}
	var value dbus.Variant
	if err := call.Store(&value); err != nil {
		return 0, err
	}
	n, ok := value.Value().(uint32)
	if !ok {
		return 0, fmt.Errorf("property %s has type %T", property, value.Value())
	}
	return n, nil
}
