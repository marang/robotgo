//go:build linux

package accessibility

import (
	"context"

	"github.com/godbus/dbus/v5"
)

func validATSPIReference(reference atspiReference) bool {
	return validATSPIBusName(reference.Bus) &&
		reference.Path != atspiNullPath && reference.Path.IsValid()
}

func validATSPIBusName(name string) bool {
	if len(name) < 4 || len(name) > 255 || name[0] != ':' {
		return false
	}
	componentLength := 0
	components := 0
	for index := 1; index < len(name); index++ {
		value := name[index]
		if value == '.' {
			if componentLength == 0 {
				return false
			}
			components++
			componentLength = 0
			continue
		}
		switch {
		case value >= 'a' && value <= 'z',
			value >= 'A' && value <= 'Z',
			value >= '0' && value <= '9',
			value == '_', value == '-':
		default:
			return false
		}
		componentLength++
	}
	return components >= 1 && componentLength > 0
}

func (query *dbusATSPIQuery) applications(ctx context.Context) ([]atspiReference, error) {
	var result []atspiReference
	call := query.conn.Object(atspiRegistryDestination, atspiRootPath).CallWithContext(
		ctx, atspiAccessibleInterface+".GetChildren", 0,
	)
	if call.Err != nil {
		return nil, call.Err
	}
	return result, call.Store(&result)
}

func (query *dbusATSPIQuery) processID(ctx context.Context, bus string) (uint32, error) {
	var result uint32
	call := query.conn.Object(dbusDestination, dbusPath).CallWithContext(
		ctx, dbusInterface+".GetConnectionUnixProcessID", 0, bus,
	)
	if call.Err != nil {
		return 0, call.Err
	}
	return result, call.Store(&result)
}

func (query *dbusATSPIQuery) childCount(ctx context.Context, reference atspiReference) (int32, error) {
	var result int32
	err := query.property(ctx, reference, atspiAccessibleInterface, atspiPropertyChildCount, &result)
	return result, err
}

func (query *dbusATSPIQuery) child(ctx context.Context, reference atspiReference, index int32) (atspiReference, error) {
	var result atspiReference
	call := query.object(reference).CallWithContext(ctx, atspiAccessibleInterface+".GetChildAtIndex", 0, index)
	if call.Err != nil {
		return result, call.Err
	}
	return result, call.Store(&result)
}

func (query *dbusATSPIQuery) role(ctx context.Context, reference atspiReference) (uint32, error) {
	var result uint32
	call := query.object(reference).CallWithContext(ctx, atspiAccessibleInterface+".GetRole", 0)
	if call.Err != nil {
		return 0, call.Err
	}
	return result, call.Store(&result)
}

func (query *dbusATSPIQuery) states(ctx context.Context, reference atspiReference) ([]uint32, error) {
	var result []uint32
	call := query.object(reference).CallWithContext(ctx, atspiAccessibleInterface+".GetState", 0)
	if call.Err != nil {
		return nil, call.Err
	}
	return result, call.Store(&result)
}

func (query *dbusATSPIQuery) stringProperty(ctx context.Context, reference atspiReference, name string) (string, error) {
	var result string
	err := query.property(ctx, reference, atspiAccessibleInterface, name, &result)
	return result, err
}

func (query *dbusATSPIQuery) interfaces(ctx context.Context, reference atspiReference) ([]string, error) {
	var result []string
	call := query.object(reference).CallWithContext(ctx, atspiAccessibleInterface+".GetInterfaces", 0)
	if call.Err != nil {
		return nil, call.Err
	}
	return result, call.Store(&result)
}

func (query *dbusATSPIQuery) extents(ctx context.Context, reference atspiReference) (atspiRect, error) {
	var result atspiRect
	call := query.object(reference).CallWithContext(ctx, atspiComponentInterface+".GetExtents", 0, atspiCoordinateTypeScreen)
	if call.Err != nil {
		return result, call.Err
	}
	return result, call.Store(&result)
}

func (query *dbusATSPIQuery) text(ctx context.Context, reference atspiReference, limit int32) (string, error) {
	var result string
	call := query.object(reference).CallWithContext(ctx, atspiTextInterface+".GetText", 0, int32(0), limit)
	if call.Err != nil {
		return "", call.Err
	}
	return result, call.Store(&result)
}

func (query *dbusATSPIQuery) currentValue(ctx context.Context, reference atspiReference) (float64, error) {
	var result float64
	err := query.property(ctx, reference, atspiValueInterface, atspiPropertyCurrentValue, &result)
	return result, err
}

func (query *dbusATSPIQuery) actionCount(ctx context.Context, reference atspiReference) (int32, error) {
	var result int32
	err := query.property(ctx, reference, atspiActionInterface, atspiPropertyActionCount, &result)
	return result, err
}

func (query *dbusATSPIQuery) property(
	ctx context.Context,
	reference atspiReference,
	iface string,
	name string,
	output any,
) error {
	var value dbus.Variant
	call := query.object(reference).CallWithContext(ctx, dbusPropertiesInterface+".Get", 0, iface, name)
	if call.Err != nil {
		return call.Err
	}
	if err := call.Store(&value); err != nil {
		return err
	}
	return value.Store(output)
}

func (query *dbusATSPIQuery) object(reference atspiReference) dbus.BusObject {
	return query.conn.Object(reference.Bus, reference.Path)
}

func clearSnapshot(snapshot *Snapshot) {
	if snapshot == nil {
		return
	}
	for index := range snapshot.Nodes {
		clear(snapshot.Nodes[index].Reference)
		snapshot.Nodes[index] = Node{}
	}
	clear(snapshot.Nodes)
	snapshot.Nodes = nil
	snapshot.Backend = ""
}
