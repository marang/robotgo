//go:build linux

package accessibility

import (
	"bytes"
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	atspiBusDestination           = "org.a11y.Bus"
	atspiBusPath                  = dbus.ObjectPath("/org/a11y/bus")
	atspiBusInterface             = "org.a11y.Bus"
	atspiRegistryDestination      = "org.a11y.atspi.Registry"
	atspiRootPath                 = dbus.ObjectPath("/org/a11y/atspi/accessible/root")
	atspiAccessibleInterface      = "org.a11y.atspi.Accessible"
	atspiActionInterface          = "org.a11y.atspi.Action"
	atspiComponentInterface       = "org.a11y.atspi.Component"
	atspiEditableTextInterface    = "org.a11y.atspi.EditableText"
	atspiTextInterface            = "org.a11y.atspi.Text"
	atspiValueInterface           = "org.a11y.atspi.Value"
	atspiPropertyChildCount       = "ChildCount"
	atspiPropertyName             = "Name"
	atspiPropertyDescription      = "Description"
	atspiPropertyCurrentValue     = "CurrentValue"
	atspiPropertyMinimumIncrement = "MinimumIncrement"
	atspiPropertyActionCount      = "NActions"
	atspiPropertyParent           = "Parent"
	atspiShortAction              = "Action"
	atspiShortComponent           = "Component"
	atspiShortEditableText        = "EditableText"
	atspiShortText                = "Text"
	atspiShortValue               = "Value"
	dbusPropertiesInterface       = "org.freedesktop.DBus.Properties"
	dbusDestination               = "org.freedesktop.DBus"
	dbusPath                      = dbus.ObjectPath("/org/freedesktop/DBus")
	dbusInterface                 = "org.freedesktop.DBus"
	atspiNullPath                 = dbus.ObjectPath("/org/a11y/atspi/null")
	atspiProbeTimeout             = 750 * time.Millisecond
	maxATSPIApplications          = 4096
	maxATSPITopLevelWindows       = 256
	maxATSPIInterfaces            = 64
	maxATSPIInterfaceBytes        = 128
	maxATSPIActions               = 64
	maxATSPIAncestors             = 64
	atspiCoordinateTypeScreen     = uint32(0)
)

type atspiReference struct {
	Bus  string
	Path dbus.ObjectPath
}

type atspiRect struct {
	X      int32
	Y      int32
	Width  int32
	Height int32
}

type atspiQuery interface {
	applications(context.Context) ([]atspiReference, error)
	processID(context.Context, string) (uint32, error)
	childCount(context.Context, atspiReference) (int32, error)
	child(context.Context, atspiReference, int32) (atspiReference, error)
	role(context.Context, atspiReference) (uint32, error)
	states(context.Context, atspiReference) ([]uint32, error)
	stringProperty(context.Context, atspiReference, string) (string, error)
	interfaces(context.Context, atspiReference) ([]string, error)
	extents(context.Context, atspiReference) (atspiRect, error)
	text(context.Context, atspiReference, int32) (string, error)
	currentValue(context.Context, atspiReference) (float64, error)
	actionCount(context.Context, atspiReference) (int32, error)
	actionName(context.Context, atspiReference, int32) (string, error)
	parent(context.Context, atspiReference) (atspiReference, error)
	doAction(context.Context, atspiReference, int32) (bool, error)
	grabFocus(context.Context, atspiReference) (bool, error)
	setTextContents(context.Context, atspiReference, string) (bool, error)
	minimumIncrement(context.Context, atspiReference) (float64, error)
	setCurrentValue(context.Context, atspiReference, float64) error
}

type dbusATSPIQuery struct {
	conn *dbus.Conn
}

func probe(ctx context.Context) Capability {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := boundedATSPIContext(ctx, atspiProbeTimeout)
	defer cancel()
	session, err := connectSessionBusWithoutAutostart(probeCtx)
	if err != nil {
		return unavailableATSPICapability("the desktop session bus is unavailable")
	}
	defer func() { _ = session.Close() }()
	owned, err := nameHasOwner(probeCtx, session, atspiBusDestination)
	if err != nil {
		return capabilityFromATSPIError(err)
	}
	if !owned {
		return Capability{
			Reason: "the AT-SPI accessibility bus is not active",
			Notes:  "enable the desktop accessibility service before starting RobotGo; capability probes never activate it",
		}
	}
	address, err := accessibilityBusAddress(probeCtx, session)
	if err != nil {
		return capabilityFromATSPIError(err)
	}
	accessibilityBus, err := dbus.Connect(address, dbus.WithContext(probeCtx))
	if err != nil {
		return unavailableATSPICapability("the AT-SPI accessibility bus could not be reached")
	}
	defer func() { _ = accessibilityBus.Close() }()
	owned, err = nameHasOwner(probeCtx, accessibilityBus, atspiRegistryDestination)
	if err != nil {
		return capabilityFromATSPIError(err)
	}
	if !owned {
		return unavailableATSPICapability("the AT-SPI registry is not active")
	}
	return Capability{
		Available: true,
		Backend:   BackendATSPI2,
		Reason:    "the AT-SPI2 accessibility bus and registry are active",
		Notes:     "process-targeted semantic inspection is read-only and never opens a desktop consent dialog",
	}
}

func inspect(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if target.ProcessID <= 0 || target.NativeWindowHandle != 0 ||
		target.ExpectedTitle == "" || !validATSPILimits(limits) {
		return Snapshot{}, ErrInvalidTree
	}
	session, err := connectSessionBusWithoutAutostart(ctx)
	if err != nil {
		return Snapshot{}, normalizeATSPIError(err)
	}
	defer func() { _ = session.Close() }()
	owned, err := nameHasOwner(ctx, session, atspiBusDestination)
	if err != nil {
		return Snapshot{}, normalizeATSPIError(err)
	}
	if !owned {
		return Snapshot{}, ErrUnavailable
	}
	address, err := accessibilityBusAddress(ctx, session)
	if err != nil {
		return Snapshot{}, normalizeATSPIError(err)
	}
	accessibilityBus, err := dbus.Connect(address, dbus.WithContext(ctx))
	if err != nil {
		return Snapshot{}, normalizeATSPIError(err)
	}
	defer func() { _ = accessibilityBus.Close() }()
	owned, err = nameHasOwner(ctx, accessibilityBus, atspiRegistryDestination)
	if err != nil {
		return Snapshot{}, normalizeATSPIError(err)
	}
	if !owned {
		return Snapshot{}, ErrUnavailable
	}
	query := &dbusATSPIQuery{conn: accessibilityBus}
	root, err := findATSPITarget(ctx, query, target)
	if err != nil {
		return Snapshot{}, err
	}
	liveTitle, err := query.stringProperty(ctx, root, atspiPropertyName)
	if err != nil {
		return Snapshot{}, normalizeATSPIError(err)
	}
	if liveTitle != target.ExpectedTitle {
		return Snapshot{}, ErrStaleTarget
	}
	snapshot, err := buildATSPITree(ctx, query, root, limits)
	if err != nil {
		return Snapshot{}, err
	}
	liveTitle, err = query.stringProperty(ctx, root, atspiPropertyName)
	if err != nil {
		clearSnapshot(&snapshot)
		return Snapshot{}, normalizeATSPIError(err)
	}
	if liveTitle != target.ExpectedTitle {
		clearSnapshot(&snapshot)
		return Snapshot{}, ErrStaleTarget
	}
	return snapshot, nil
}

func act(ctx context.Context, request ActionRequest) (ActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if request.Target.ProcessID <= 0 || request.Target.NativeWindowHandle != 0 ||
		request.Target.ExpectedTitle == "" || request.Expected.Sensitive {
		return ActionResult{}, ErrStaleTarget
	}
	reference, err := decodeATSPIReference(request.Reference)
	if err != nil {
		return ActionResult{}, err
	}
	session, err := connectSessionBusWithoutAutostart(ctx)
	if err != nil {
		return ActionResult{}, normalizeATSPIError(err)
	}
	defer func() { _ = session.Close() }()
	owned, err := nameHasOwner(ctx, session, atspiBusDestination)
	if err != nil || !owned {
		if err == nil {
			err = ErrUnavailable
		}
		return ActionResult{}, normalizeATSPIError(err)
	}
	address, err := accessibilityBusAddress(ctx, session)
	if err != nil {
		return ActionResult{}, normalizeATSPIError(err)
	}
	accessibilityBus, err := dbus.Connect(address, dbus.WithContext(ctx))
	if err != nil {
		return ActionResult{}, normalizeATSPIError(err)
	}
	defer func() { _ = accessibilityBus.Close() }()
	owned, err = nameHasOwner(ctx, accessibilityBus, atspiRegistryDestination)
	if err != nil || !owned {
		if err == nil {
			err = ErrUnavailable
		}
		return ActionResult{}, normalizeATSPIError(err)
	}
	return actATSPI(ctx, &dbusATSPIQuery{conn: accessibilityBus}, request, reference)
}

func actATSPI(ctx context.Context, query atspiQuery, request ActionRequest, reference atspiReference) (ActionResult, error) {
	root, err := findATSPITarget(ctx, query, request.Target)
	if err != nil {
		return ActionResult{}, err
	}
	if err := validateATSPIMembership(ctx, query, root, reference, uint32(request.Target.ProcessID)); err != nil {
		return ActionResult{}, err
	}
	liveTitle, err := query.stringProperty(ctx, root, atspiPropertyName)
	if err != nil {
		return ActionResult{}, normalizeATSPIError(err)
	}
	if liveTitle != request.Target.ExpectedTitle {
		return ActionResult{}, ErrStaleTarget
	}
	roleID, err := query.role(ctx, reference)
	if err != nil {
		return ActionResult{}, normalizeATSPIError(err)
	}
	states, err := query.states(ctx, reference)
	if err != nil {
		return ActionResult{}, normalizeATSPIError(err)
	}
	if len(states) != 2 || roleID == atspiRolePasswordText {
		return ActionResult{}, ErrStaleTarget
	}
	name, err := query.stringProperty(ctx, reference, atspiPropertyName)
	if err != nil {
		return ActionResult{}, normalizeATSPIError(err)
	}
	interfaces, err := readATSPIInterfaces(ctx, query, reference)
	if err != nil {
		return ActionResult{}, err
	}
	liveBounds, err := liveATSPIBounds(ctx, query, reference, interfaces, request.Expected.Bounds != nil)
	if err != nil {
		return ActionResult{}, err
	}
	actionNames, err := readATSPIActionNames(ctx, query, reference, interfaces)
	if err != nil {
		return ActionResult{}, err
	}
	liveActions := inferATSPIActions(mapATSPIRole(roleID), states, interfaces, actionNames)
	if mapATSPIRole(roleID) != request.Expected.Role || name != request.Expected.Name ||
		!slices.Equal(mapATSPIStates(mapATSPIRole(roleID), states), request.Expected.States) ||
		!equalAccessibilityBounds(liveBounds, request.Expected.Bounds) ||
		!slices.Equal(liveActions, request.Expected.Actions) || !slices.Contains(liveActions, request.Action) ||
		slices.Contains(request.Expected.States, "disabled") {
		return ActionResult{}, ErrStaleTarget
	}
	liveTitle, err = query.stringProperty(ctx, root, atspiPropertyName)
	if err != nil {
		return ActionResult{}, normalizeATSPIError(err)
	}
	if liveTitle != request.Target.ExpectedTitle {
		return ActionResult{}, ErrStaleTarget
	}
	return dispatchATSPIAction(ctx, query, reference, roleID, interfaces, actionNames, request)
}

func decodeATSPIReference(data []byte) (atspiReference, error) {
	separator := bytes.IndexByte(data, 0)
	if separator <= 0 || separator == len(data)-1 || bytes.IndexByte(data[separator+1:], 0) >= 0 {
		return atspiReference{}, ErrStaleTarget
	}
	reference := atspiReference{Bus: string(data[:separator]), Path: dbus.ObjectPath(string(data[separator+1:]))}
	if !validATSPIReference(reference) {
		return atspiReference{}, ErrStaleTarget
	}
	return reference, nil
}

func validateATSPIMembership(ctx context.Context, query atspiQuery, root, reference atspiReference, processID uint32) error {
	livePID, err := query.processID(ctx, reference.Bus)
	if err != nil {
		return normalizeATSPIError(err)
	}
	if livePID != processID {
		return ErrStaleTarget
	}
	seen := make(map[string]struct{}, maxATSPIAncestors)
	current := reference
	for depth := 0; depth <= maxATSPIAncestors; depth++ {
		if current == root {
			return nil
		}
		key := current.Bus + "\x00" + string(current.Path)
		if _, duplicate := seen[key]; duplicate {
			return ErrStaleTarget
		}
		seen[key] = struct{}{}
		parent, err := query.parent(ctx, current)
		if err != nil {
			return normalizeATSPIError(err)
		}
		if !validATSPIReference(parent) {
			return ErrStaleTarget
		}
		current = parent
	}
	return ErrStaleTarget
}

func readATSPIInterfaces(ctx context.Context, query atspiQuery, reference atspiReference) (map[string]bool, error) {
	values, err := query.interfaces(ctx, reference)
	if err != nil {
		return nil, normalizeATSPIError(err)
	}
	if len(values) > maxATSPIInterfaces {
		return nil, ErrInvalidTree
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || len(value) > maxATSPIInterfaceBytes || strings.IndexByte(value, 0) >= 0 {
			return nil, ErrInvalidTree
		}
		result[value] = true
	}
	return result, nil
}

func readATSPIActionNames(ctx context.Context, query atspiQuery, reference atspiReference, interfaces map[string]bool) ([]string, error) {
	if !interfaces[atspiShortAction] {
		return nil, nil
	}
	count, err := query.actionCount(ctx, reference)
	if err != nil {
		return nil, normalizeATSPIError(err)
	}
	if count < 0 || count > maxATSPIActions {
		return nil, ErrInvalidTree
	}
	result := make([]string, 0, count)
	for index := int32(0); index < count; index++ {
		name, err := query.actionName(ctx, reference, index)
		if err != nil {
			return nil, normalizeATSPIError(err)
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || len(name) > maxATSPIInterfaceBytes || strings.IndexByte(name, 0) >= 0 {
			return nil, ErrInvalidTree
		}
		result = append(result, name)
	}
	return result, nil
}

func liveATSPIBounds(ctx context.Context, query atspiQuery, reference atspiReference, interfaces map[string]bool, required bool) (*Bounds, error) {
	if !required {
		return nil, nil
	}
	if !interfaces[atspiShortComponent] {
		return nil, ErrStaleTarget
	}
	rect, err := query.extents(ctx, reference)
	if err != nil {
		return nil, normalizeATSPIError(err)
	}
	return &Bounds{X: int(rect.X), Y: int(rect.Y), Width: int(rect.Width), Height: int(rect.Height)}, nil
}

func dispatchATSPIAction(ctx context.Context, query atspiQuery, reference atspiReference, roleID uint32, interfaces map[string]bool, names []string, request ActionRequest) (ActionResult, error) {
	dispatched := ActionResult{Dispatched: true}
	switch request.Action {
	case "focus":
		if !interfaces[atspiShortComponent] {
			return ActionResult{}, ErrStaleTarget
		}
		ok, err := query.grabFocus(ctx, reference)
		if err != nil {
			return dispatched, normalizeATSPIError(err)
		}
		if !ok {
			return dispatched, ErrUnavailable
		}
		return dispatched, nil
	case "set-value":
		if mapATSPIRole(roleID) == "textbox" && interfaces[atspiShortEditableText] {
			ok, err := query.setTextContents(ctx, reference, request.Value)
			if err != nil {
				return dispatched, normalizeATSPIError(err)
			}
			if !ok {
				return dispatched, ErrUnavailable
			}
			return dispatched, nil
		}
		if mapATSPIRole(roleID) != "slider" || !interfaces[atspiShortValue] {
			return ActionResult{}, ErrStaleTarget
		}
		value, err := strconv.ParseFloat(request.Value, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return ActionResult{}, ErrInvalidTree
		}
		if err := query.setCurrentValue(ctx, reference, value); err != nil {
			return dispatched, normalizeATSPIError(err)
		}
		return dispatched, nil
	case "increment", "decrement":
		if mapATSPIRole(roleID) != "slider" || !interfaces[atspiShortValue] {
			return ActionResult{}, ErrStaleTarget
		}
		current, err := query.currentValue(ctx, reference)
		if err != nil {
			return ActionResult{}, normalizeATSPIError(err)
		}
		step, err := query.minimumIncrement(ctx, reference)
		if err != nil {
			return ActionResult{}, normalizeATSPIError(err)
		}
		if step <= 0 || math.IsNaN(step) || math.IsInf(step, 0) || math.IsNaN(current) || math.IsInf(current, 0) {
			return ActionResult{}, ErrInvalidTree
		}
		if request.Action == "decrement" {
			step = -step
		}
		if err := query.setCurrentValue(ctx, reference, current+step); err != nil {
			return dispatched, normalizeATSPIError(err)
		}
		return dispatched, nil
	default:
		index := findATSPIActionIndex(request.Action, names)
		if index < 0 {
			return ActionResult{}, ErrStaleTarget
		}
		ok, err := query.doAction(ctx, reference, int32(index))
		if err != nil {
			return dispatched, normalizeATSPIError(err)
		}
		if !ok {
			return dispatched, ErrUnavailable
		}
		return dispatched, nil
	}
}

func findATSPIActionIndex(action string, names []string) int {
	allowed := map[string][]string{
		"press":    {"press", "click", "activate", "jump"},
		"toggle":   {"toggle", "press", "click", "activate"},
		"expand":   {"expand", "open", "show", "toggle"},
		"collapse": {"collapse", "close", "hide", "toggle"},
	}[action]
	for _, candidate := range allowed {
		if index := slices.Index(names, candidate); index >= 0 {
			return index
		}
	}
	return -1
}

func connectSessionBusWithoutAutostart(ctx context.Context) (*dbus.Conn, error) {
	conn, err := dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func validATSPILimits(limits Limits) bool {
	return limits.MaxElements > 0 && limits.MaxDepth > 0 && limits.MaxStringBytes > 0 &&
		limits.MaxReferenceBytes > 0 && limits.MaxTotalReferenceBytes > 0
}

func boundedATSPIContext(ctx context.Context, maximum time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= maximum {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, maximum)
}

func accessibilityBusAddress(ctx context.Context, session *dbus.Conn) (string, error) {
	var address string
	call := session.Object(atspiBusDestination, atspiBusPath).CallWithContext(
		ctx, atspiBusInterface+".GetAddress", 0,
	)
	if call.Err != nil {
		return "", normalizeATSPIError(call.Err)
	}
	if err := call.Store(&address); err != nil || address == "" || len(address) > 4096 || strings.IndexByte(address, 0) >= 0 {
		return "", ErrUnavailable
	}
	return address, nil
}

func nameHasOwner(ctx context.Context, conn *dbus.Conn, name string) (bool, error) {
	var owned bool
	call := conn.Object(dbusDestination, dbusPath).CallWithContext(
		ctx, dbusInterface+".NameHasOwner", 0, name,
	)
	if call.Err != nil {
		return false, normalizeATSPIError(call.Err)
	}
	if err := call.Store(&owned); err != nil {
		return false, ErrUnavailable
	}
	return owned, nil
}

func unavailableATSPICapability(reason string) Capability {
	return Capability{Reason: reason, Notes: "enable a working AT-SPI2 desktop accessibility service"}
}

func capabilityFromATSPIError(err error) Capability {
	if errors.Is(err, ErrPermissionDenied) {
		return Capability{
			Reason:           "the desktop denied access to the AT-SPI accessibility bus",
			Notes:            "grant this process access to the desktop accessibility bus",
			PermissionDenied: true,
		}
	}
	return unavailableATSPICapability("the AT-SPI accessibility service probe failed")
}

func normalizeATSPIError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	for _, known := range []error{ErrUnsupported, ErrUnavailable, ErrPermissionDenied, ErrStaleTarget, ErrInvalidTree} {
		if errors.Is(err, known) {
			return known
		}
	}
	if name, ok := atspiDBusErrorName(err); ok {
		switch name {
		case "org.freedesktop.DBus.Error.AccessDenied", "org.freedesktop.DBus.Error.AuthFailed":
			return ErrPermissionDenied
		case "org.freedesktop.DBus.Error.UnknownMethod", "org.freedesktop.DBus.Error.NotSupported":
			return ErrUnsupported
		default:
			return ErrUnavailable
		}
	}
	return ErrUnavailable
}

func atspiDBusErrorName(err error) (string, bool) {
	var value dbus.Error
	if errors.As(err, &value) {
		return value.Name, true
	}
	var pointer *dbus.Error
	if errors.As(err, &pointer) && pointer != nil {
		return pointer.Name, true
	}
	return "", false
}

func findATSPITarget(ctx context.Context, query atspiQuery, target Target) (atspiReference, error) {
	applications, err := query.applications(ctx)
	if err != nil {
		return atspiReference{}, normalizeATSPIError(err)
	}
	if len(applications) > maxATSPIApplications {
		return atspiReference{}, ErrInvalidTree
	}
	var matches []atspiReference
	for _, application := range applications {
		if !validATSPIReference(application) {
			return atspiReference{}, ErrInvalidTree
		}
		pid, err := query.processID(ctx, application.Bus)
		if err != nil {
			if atspiStaleApplicationError(err) {
				continue
			}
			return atspiReference{}, normalizeATSPIError(err)
		}
		if uint64(pid) != uint64(target.ProcessID) {
			continue
		}
		count, err := query.childCount(ctx, application)
		if err != nil {
			return atspiReference{}, normalizeATSPIError(err)
		}
		if count < 0 || count > maxATSPITopLevelWindows {
			return atspiReference{}, ErrInvalidTree
		}
		for index := int32(0); index < count; index++ {
			candidate, err := query.child(ctx, application, index)
			if err != nil {
				return atspiReference{}, normalizeATSPIError(err)
			}
			if !validATSPIReference(candidate) {
				return atspiReference{}, ErrInvalidTree
			}
			role, err := query.role(ctx, candidate)
			if err != nil {
				return atspiReference{}, normalizeATSPIError(err)
			}
			if !atspiTopLevelRole(role) {
				continue
			}
			title, err := query.stringProperty(ctx, candidate, atspiPropertyName)
			if err != nil {
				return atspiReference{}, normalizeATSPIError(err)
			}
			if title == target.ExpectedTitle {
				matches = append(matches, candidate)
			}
		}
	}
	if len(matches) != 1 {
		return atspiReference{}, ErrStaleTarget
	}
	return matches[0], nil
}

func atspiStaleApplicationError(err error) bool {
	name, ok := atspiDBusErrorName(err)
	return ok && name == "org.freedesktop.DBus.Error.NameHasNoOwner"
}
