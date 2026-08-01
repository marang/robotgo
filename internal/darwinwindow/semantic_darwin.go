//go:build darwin

package darwinwindow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"unsafe"

	"github.com/marang/robotgo/internal/windowbackend"
)

const (
	axSemanticTimeoutSeconds = float32(1)
	maximumAXChildren        = int64(4096)
	maximumAXActions         = int64(128)
	maximumSemanticWindows   = int64(256)
)

var errAXBoundsUnavailable = errors.New("AX bounds are unavailable")

type nativeAXSemanticQuery struct {
	api         *nativeAPI
	rootBounds  AccessibilityBounds
	maxChildren int64
	bounds      map[uintptr]*AccessibilityBounds
}

var _ axSemanticQuery[uintptr] = (*nativeAXSemanticQuery)(nil)

func inspectAccessibility(
	ctx context.Context,
	target AccessibilityTarget,
	limits AccessibilityLimits,
) (AccessibilitySnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if target.ExpectedTitle == "" || !validAccessibilityLimits(limits) ||
		(target.ProcessID > 0) == (target.CGWindowID > 0) {
		return AccessibilitySnapshot{}, ErrAccessibilityInvalidTree
	}
	return runOnAXThread(ctx, func() (AccessibilitySnapshot, error) {
		return inspectAccessibilityOnThread(ctx, target, limits)
	})
}

func inspectAccessibilityOnThread(
	ctx context.Context,
	target AccessibilityTarget,
	limits AccessibilityLimits,
) (AccessibilitySnapshot, error) {
	api, err := openNativeAPI()
	if err != nil {
		return AccessibilitySnapshot{}, err
	}
	defer func() { _ = api.close() }()
	if !api.axIsProcessTrusted() {
		return AccessibilitySnapshot{}, ErrPermission
	}
	pid, err := semanticTargetPID(api, target)
	if err != nil {
		return AccessibilitySnapshot{}, err
	}
	application := api.axUIElementCreateApplication(pid)
	if application == 0 {
		return AccessibilitySnapshot{}, ErrAccessibilityStaleTarget
	}
	defer api.cfRelease(application)
	if err := semanticAXResult(
		"set AX messaging timeout",
		api.axUIElementSetMessagingTimeout(application, axSemanticTimeoutSeconds),
	); err != nil {
		return AccessibilitySnapshot{}, err
	}
	root, windowID, err := semanticTargetWindow(ctx, api, application, target, pid)
	if err != nil {
		return AccessibilitySnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		api.cfRelease(root)
		return AccessibilitySnapshot{}, err
	}
	rootBounds, err := semanticElementRect(api, root)
	if err != nil {
		api.cfRelease(root)
		return AccessibilitySnapshot{}, err
	}
	query := &nativeAXSemanticQuery{
		api: api, rootBounds: accessibilityWindowRect(rootBounds),
		maxChildren: min(maximumAXChildren, int64(limits.MaxElements)+1),
		bounds:      make(map[uintptr]*AccessibilityBounds),
	}
	snapshot, err := buildAXSemanticTree(ctx, query, root, pid, windowID, limits)
	if err != nil {
		return AccessibilitySnapshot{}, err
	}

	if err := ctx.Err(); err != nil {
		clearAccessibilitySnapshot(&snapshot)
		return AccessibilitySnapshot{}, err
	}
	livePID, pidErr := windowPIDLocked(api, windowbackend.Handle(windowID))
	liveRoot, liveRootErr := applicationWindowByID(ctx, api, application, windowID)
	var liveTitle string
	var titleErr error
	if liveRootErr != nil {
		titleErr = liveRootErr
	} else {
		liveTitle, titleErr = semanticStringAttribute(api, liveRoot, api.axTitleAttribute)
		api.cfRelease(liveRoot)
	}
	if pidErr != nil || titleErr != nil {
		clearAccessibilitySnapshot(&snapshot)
		return AccessibilitySnapshot{}, normalizeSemanticError(errors.Join(pidErr, titleErr))
	}
	if livePID != pid || liveTitle != target.ExpectedTitle {
		clearAccessibilitySnapshot(&snapshot)
		return AccessibilitySnapshot{}, ErrAccessibilityStaleTarget
	}
	return snapshot, nil
}

func semanticTargetPID(api *nativeAPI, target AccessibilityTarget) (int32, error) {
	if target.ProcessID > 0 {
		if int64(target.ProcessID) > math.MaxInt32 {
			return 0, ErrAccessibilityStaleTarget
		}
		return int32(target.ProcessID), nil
	}
	if uint64(target.CGWindowID) > math.MaxUint32 {
		return 0, ErrAccessibilityStaleTarget
	}
	pid, err := windowPIDLocked(api, windowbackend.Handle(uintptr(target.CGWindowID)))
	if err != nil {
		return 0, normalizeSemanticError(err)
	}
	return pid, nil
}

func semanticTargetWindow(
	ctx context.Context,
	api *nativeAPI,
	application uintptr,
	target AccessibilityTarget,
	pid int32,
) (uintptr, uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	count, err := semanticAttributeCount(api, application, api.axWindowsAttribute)
	if err != nil {
		return 0, 0, err
	}
	if count <= 0 || count > maximumSemanticWindows {
		return 0, 0, ErrAccessibilityStaleTarget
	}
	var windows uintptr
	if err := semanticAXResult(
		"copy AX windows",
		api.axUIElementCopyAttributeValues(application, api.axWindowsAttribute, 0, count, &windows),
	); err != nil {
		if windows != 0 {
			api.cfRelease(windows)
		}
		return 0, 0, err
	}
	if windows == 0 {
		return 0, 0, ErrAccessibilityStaleTarget
	}
	defer api.cfRelease(windows)
	if requireCFType(api, windows, api.cfArrayGetTypeID(), "AXWindows") != nil {
		return 0, 0, ErrAccessibilityInvalidTree
	}
	arrayCount := api.cfArrayGetCount(windows)
	if arrayCount <= 0 || arrayCount > count {
		return 0, 0, ErrAccessibilityInvalidTree
	}

	var matched uintptr
	var matchedID uint32
	for index := int64(0); index < arrayCount; index++ {
		if err := ctx.Err(); err != nil {
			if matched != 0 {
				api.cfRelease(matched)
			}
			return 0, 0, err
		}
		element := api.cfArrayGetValueAtIndex(windows, index)
		if element == 0 || requireCFType(api, element, api.axUIElementGetTypeID(), "AX window") != nil {
			if matched != 0 {
				api.cfRelease(matched)
			}
			return 0, 0, ErrAccessibilityInvalidTree
		}
		var owner int32
		if err := semanticAXResult("get AX window pid", api.axUIElementGetPID(element, &owner)); err != nil {
			if matched != 0 {
				api.cfRelease(matched)
			}
			return 0, 0, err
		}
		if owner != pid {
			continue
		}
		var candidate uint32
		if err := semanticAXResult("map AX window", api.axUIElementGetWindow(element, &candidate)); err != nil {
			if matched != 0 {
				api.cfRelease(matched)
			}
			return 0, 0, err
		}
		if candidate == 0 {
			if matched != 0 {
				api.cfRelease(matched)
			}
			return 0, 0, ErrAccessibilityInvalidTree
		}
		if target.CGWindowID > 0 && candidate != uint32(target.CGWindowID) {
			continue
		}
		title, titleErr := semanticStringAttribute(api, element, api.axTitleAttribute)
		if errors.Is(titleErr, ErrAccessibilityStaleTarget) {
			continue
		}
		if titleErr != nil {
			if matched != 0 {
				api.cfRelease(matched)
			}
			return 0, 0, titleErr
		}
		if title != target.ExpectedTitle {
			continue
		}
		if matched != 0 {
			api.cfRelease(matched)
			return 0, 0, ErrAccessibilityStaleTarget
		}
		matched = api.cfRetain(element)
		matchedID = candidate
	}
	if matched == 0 {
		return 0, 0, ErrAccessibilityStaleTarget
	}
	return matched, matchedID, nil
}

func applicationWindowByID(
	ctx context.Context,
	api *nativeAPI,
	application uintptr,
	windowID uint32,
) (uintptr, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	count, err := semanticAttributeCount(api, application, api.axWindowsAttribute)
	if err != nil {
		return 0, err
	}
	if count <= 0 || count > maximumSemanticWindows {
		return 0, ErrAccessibilityInvalidTree
	}
	var windows uintptr
	result := api.axUIElementCopyAttributeValues(application, api.axWindowsAttribute, 0, count, &windows)
	if result != axErrorSuccess {
		if windows != 0 {
			api.cfRelease(windows)
		}
		return 0, semanticAXResult("revalidate AX windows", result)
	}
	if windows == 0 {
		return 0, ErrAccessibilityStaleTarget
	}
	defer api.cfRelease(windows)
	if requireCFType(api, windows, api.cfArrayGetTypeID(), "AXWindows") != nil {
		return 0, ErrAccessibilityInvalidTree
	}
	arrayCount := api.cfArrayGetCount(windows)
	if arrayCount <= 0 || arrayCount > count {
		return 0, ErrAccessibilityInvalidTree
	}
	for index := int64(0); index < arrayCount; index++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		element := api.cfArrayGetValueAtIndex(windows, index)
		if element == 0 || requireCFType(api, element, api.axUIElementGetTypeID(), "AX window") != nil {
			return 0, ErrAccessibilityInvalidTree
		}
		var candidate uint32
		result := api.axUIElementGetWindow(element, &candidate)
		if err := semanticAXResult("revalidate AX window ID", result); err != nil {
			return 0, err
		}
		if candidate == windowID {
			return api.cfRetain(element), nil
		}
	}
	return 0, ErrAccessibilityStaleTarget
}

func (query *nativeAXSemanticQuery) processID(ctx context.Context, element uintptr) (int32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var pid int32
	if err := semanticAXResult("get AX element pid", query.api.axUIElementGetPID(element, &pid)); err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, ErrAccessibilityInvalidTree
	}
	return pid, nil
}

func (query *nativeAXSemanticQuery) structure(
	ctx context.Context,
	element uintptr,
) (axSemanticStructure, error) {
	if err := ctx.Err(); err != nil {
		return axSemanticStructure{}, err
	}
	roleValue, err := semanticStringAttribute(query.api, element, query.api.axRoleAttribute)
	if err != nil {
		return axSemanticStructure{}, err
	}
	if err := ctx.Err(); err != nil {
		return axSemanticStructure{}, err
	}
	subrole, _, err := semanticOptionalStringAttribute(query.api, element, query.api.axSubroleAttribute)
	if err != nil {
		return axSemanticStructure{}, err
	}
	if err := ctx.Err(); err != nil {
		return axSemanticStructure{}, err
	}
	hidden, _, err := semanticOptionalBoolAttribute(query.api, element, query.api.axHiddenAttribute)
	if err != nil {
		return axSemanticStructure{}, err
	}
	if err := ctx.Err(); err != nil {
		return axSemanticStructure{}, err
	}
	bounds, hasBounds, err := semanticOptionalElementBounds(query.api, element)
	if err != nil {
		return axSemanticStructure{}, err
	}
	if hasBounds {
		query.bounds[element] = bounds
	}
	sensitive := subrole == "AXSecureTextField"
	role := mapAXRole(roleValue, subrole, sensitive)
	return axSemanticStructure{
		Role:      role,
		Sensitive: sensitive,
		Hidden:    hidden,
		Offscreen: (hasBounds && !accessibilityRectsIntersect(query.rootBounds, *bounds)) ||
			(!hasBounds && !structuralAXRole(role)),
	}, nil
}

func (query *nativeAXSemanticQuery) details(
	ctx context.Context,
	element uintptr,
	role string,
	limits AccessibilityLimits,
) (axSemanticDetails, error) {
	if err := ctx.Err(); err != nil {
		return axSemanticDetails{}, err
	}
	var details axSemanticDetails
	var err error
	if limits.ReadName {
		details.Name, _, err = semanticOptionalStringAttribute(query.api, element, query.api.axTitleAttribute)
		if err != nil {
			return axSemanticDetails{}, err
		}
		if err := ctx.Err(); err != nil {
			return axSemanticDetails{}, err
		}
	}
	if limits.ReadDescription {
		details.Description, _, err = semanticOptionalStringAttribute(query.api, element, query.api.axDescriptionAttribute)
		if err != nil {
			return axSemanticDetails{}, err
		}
		if details.Description == "" {
			details.Description, _, err = semanticOptionalStringAttribute(query.api, element, query.api.axHelpAttribute)
			if err != nil {
				return axSemanticDetails{}, err
			}
		}
		if err := ctx.Err(); err != nil {
			return axSemanticDetails{}, err
		}
	}
	if limits.ReadValue {
		details.Value, err = semanticValueAttribute(query.api, element, role)
		if err != nil {
			return axSemanticDetails{}, err
		}
		if err := ctx.Err(); err != nil {
			return axSemanticDetails{}, err
		}
	}
	if limits.ReadStates {
		details.States, err = semanticStates(query.api, element)
		if err != nil {
			return axSemanticDetails{}, err
		}
		if err := ctx.Err(); err != nil {
			return axSemanticDetails{}, err
		}
	}
	if limits.ReadBounds {
		details.Bounds = query.bounds[element]
		if details.Bounds == nil {
			details.Bounds, _, err = semanticOptionalElementBounds(query.api, element)
			if err != nil {
				return axSemanticDetails{}, err
			}
		}
		if err := ctx.Err(); err != nil {
			return axSemanticDetails{}, err
		}
	}
	if limits.ReadFocus {
		details.Focused, _, err = semanticOptionalBoolAttribute(query.api, element, query.api.axFocusedAttribute)
		if err != nil {
			return axSemanticDetails{}, err
		}
		if err := ctx.Err(); err != nil {
			return axSemanticDetails{}, err
		}
	}
	if limits.ReadActions {
		details.Actions, err = semanticActions(query.api, element, role)
		if err != nil {
			return axSemanticDetails{}, err
		}
	}
	return details, nil
}

func (query *nativeAXSemanticQuery) children(
	ctx context.Context,
	element uintptr,
) ([]uintptr, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	count, err := semanticAttributeCount(query.api, element, query.api.axChildrenAttribute)
	if errors.Is(err, ErrUnsupported) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if count <= 0 {
		return nil, false, nil
	}
	copyCount := min(count, query.maxChildren, maximumAXChildren)
	var array uintptr
	if err := semanticAXResult(
		"copy AX children",
		query.api.axUIElementCopyAttributeValues(element, query.api.axChildrenAttribute, 0, copyCount, &array),
	); err != nil {
		if array != 0 {
			query.api.cfRelease(array)
		}
		return nil, false, err
	}
	if array == 0 {
		return nil, false, ErrAccessibilityInvalidTree
	}
	defer query.api.cfRelease(array)
	if requireCFType(query.api, array, query.api.cfArrayGetTypeID(), "AXChildren") != nil {
		return nil, false, ErrAccessibilityInvalidTree
	}
	arrayCount := query.api.cfArrayGetCount(array)
	if arrayCount < 0 || arrayCount > copyCount {
		return nil, false, ErrAccessibilityInvalidTree
	}
	children := make([]uintptr, 0, arrayCount)
	for index := int64(0); index < arrayCount; index++ {
		child := query.api.cfArrayGetValueAtIndex(array, index)
		if child == 0 || requireCFType(query.api, child, query.api.axUIElementGetTypeID(), "AX child") != nil {
			for _, retained := range children {
				query.api.cfRelease(retained)
			}
			return nil, false, ErrAccessibilityInvalidTree
		}
		children = append(children, query.api.cfRetain(child))
	}
	return children, count > copyCount, nil
}

func (query *nativeAXSemanticQuery) release(element uintptr) {
	delete(query.bounds, element)
	if element != 0 {
		query.api.cfRelease(element)
	}
}

func semanticAttributeCount(api *nativeAPI, element, attribute uintptr) (int64, error) {
	var count int64
	result := api.axUIElementGetAttributeValueCount(element, attribute, &count)
	if result == axErrorAttributeUnsupported || result == axErrorNoValue {
		return 0, ErrUnsupported
	}
	if err := semanticAXResult("count AX attribute values", result); err != nil {
		return 0, err
	}
	if count < 0 {
		return 0, ErrAccessibilityInvalidTree
	}
	return count, nil
}

func semanticOptionalAttribute(api *nativeAPI, element, attribute uintptr) (uintptr, bool, error) {
	var value uintptr
	result := api.axUIElementCopyAttributeValue(element, attribute, &value)
	if result == axErrorAttributeUnsupported || result == axErrorNoValue {
		return 0, false, nil
	}
	if err := semanticAXResult("copy AX attribute", result); err != nil {
		if value != 0 {
			api.cfRelease(value)
		}
		return 0, false, err
	}
	if value == 0 {
		return 0, false, ErrAccessibilityInvalidTree
	}
	return value, true, nil
}

func semanticStringAttribute(api *nativeAPI, element, attribute uintptr) (string, error) {
	value, ok, err := semanticOptionalStringAttribute(api, element, attribute)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrAccessibilityStaleTarget
	}
	return value, nil
}

func semanticOptionalStringAttribute(api *nativeAPI, element, attribute uintptr) (string, bool, error) {
	value, ok, err := semanticOptionalAttribute(api, element, attribute)
	if err != nil || !ok {
		return "", ok, err
	}
	defer api.cfRelease(value)
	text, err := cfStringLocked(api, value)
	if err != nil {
		return "", false, ErrAccessibilityInvalidTree
	}
	return text, true, nil
}

func semanticOptionalBoolAttribute(api *nativeAPI, element, attribute uintptr) (bool, bool, error) {
	value, ok, err := semanticOptionalAttribute(api, element, attribute)
	if err != nil || !ok {
		return false, ok, err
	}
	defer api.cfRelease(value)
	if requireCFType(api, value, api.cfBooleanGetTypeID(), "AX boolean") != nil {
		return false, false, ErrAccessibilityInvalidTree
	}
	return api.cfBooleanGetValue(value), true, nil
}

func semanticElementRect(api *nativeAPI, element uintptr) (windowbackend.Rect, error) {
	position, dimensions, err := semanticElementGeometry(api, element)
	if err != nil {
		return windowbackend.Rect{}, err
	}
	rect, err := enclosingRect(position, dimensions)
	if err != nil {
		return windowbackend.Rect{}, ErrAccessibilityInvalidTree
	}
	return rect, nil
}

func semanticElementGeometry(api *nativeAPI, element uintptr) (point, size, error) {
	positionValue, ok, err := semanticOptionalAttribute(api, element, api.axPositionAttribute)
	if err != nil {
		return point{}, size{}, err
	}
	if !ok {
		return point{}, size{}, errAXBoundsUnavailable
	}
	defer api.cfRelease(positionValue)
	sizeValue, ok, err := semanticOptionalAttribute(api, element, api.axSizeAttribute)
	if err != nil {
		return point{}, size{}, err
	}
	if !ok {
		return point{}, size{}, errAXBoundsUnavailable
	}
	defer api.cfRelease(sizeValue)
	if requireCFType(api, positionValue, api.axValueGetTypeID(), "AXPosition") != nil ||
		requireCFType(api, sizeValue, api.axValueGetTypeID(), "AXSize") != nil ||
		api.axValueGetType(positionValue) != axValueCGPointType ||
		api.axValueGetType(sizeValue) != axValueCGSizeType {
		return point{}, size{}, ErrAccessibilityInvalidTree
	}
	var position point
	var dimensions size
	if !api.axValueGetValue(positionValue, axValueCGPointType, unsafe.Pointer(&position)) ||
		!api.axValueGetValue(sizeValue, axValueCGSizeType, unsafe.Pointer(&dimensions)) {
		return point{}, size{}, ErrAccessibilityInvalidTree
	}
	return position, dimensions, nil
}

func semanticOptionalElementBounds(api *nativeAPI, element uintptr) (*AccessibilityBounds, bool, error) {
	position, dimensions, err := semanticElementGeometry(api, element)
	if errors.Is(err, errAXBoundsUnavailable) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if dimensions.Width == 0 || dimensions.Height == 0 {
		return nil, false, nil
	}
	rect, err := enclosingRect(position, dimensions)
	if err != nil {
		return nil, false, ErrAccessibilityInvalidTree
	}
	bounds := accessibilityWindowRect(rect)
	return &bounds, true, nil
}

func accessibilityWindowRect(rect windowbackend.Rect) AccessibilityBounds {
	return AccessibilityBounds{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
}

func accessibilityRectsIntersect(left, right AccessibilityBounds) bool {
	if left.Width <= 0 || left.Height <= 0 || right.Width <= 0 || right.Height <= 0 {
		return false
	}
	return left.X < right.X+right.Width && right.X < left.X+left.Width &&
		left.Y < right.Y+right.Height && right.Y < left.Y+left.Height
}

func semanticValueAttribute(api *nativeAPI, element uintptr, role string) (string, error) {
	value, ok, err := semanticOptionalAttribute(api, element, api.axValueAttribute)
	if err != nil || !ok {
		return "", err
	}
	defer api.cfRelease(value)
	switch api.cfGetTypeID(value) {
	case api.cfStringGetTypeID():
		return cfStringLocked(api, value)
	case api.cfNumberGetTypeID():
		if role != "slider" && role != "progress" {
			return "", nil
		}
		var number float64
		if !api.cfNumberGetValue(value, cfNumberFloat64Type, unsafe.Pointer(&number)) ||
			math.IsNaN(number) || math.IsInf(number, 0) {
			return "", ErrAccessibilityInvalidTree
		}
		return strconv.FormatFloat(number, 'g', -1, 64), nil
	default:
		return "", nil
	}
}

func semanticStates(api *nativeAPI, element uintptr) ([]string, error) {
	states := make([]string, 0, 5)
	enabled, hasEnabled, err := semanticOptionalBoolAttribute(api, element, api.axEnabledAttribute)
	if err != nil {
		return nil, err
	}
	if hasEnabled {
		if enabled {
			states = append(states, "enabled")
		} else {
			states = append(states, "disabled")
		}
	}
	for _, candidate := range []struct {
		attribute uintptr
		trueState string
	}{
		{api.axSelectedAttribute, "selected"},
		{api.axExpandedAttribute, "expanded"},
	} {
		value, present, stateErr := semanticOptionalBoolAttribute(api, element, candidate.attribute)
		if stateErr != nil {
			return nil, stateErr
		}
		if present && value {
			states = append(states, candidate.trueState)
		}
		if present && !value && candidate.trueState == "expanded" {
			states = append(states, "collapsed")
		}
	}
	return states, nil
}

func semanticActions(api *nativeAPI, element uintptr, role string) ([]string, error) {
	var actionArray uintptr
	result := api.axUIElementCopyActionNames(element, &actionArray)
	if result == axErrorActionUnsupported || result == axErrorNoValue || result == axErrorNotImplemented {
		return nil, nil
	}
	if err := semanticAXResult("copy AX action names", result); err != nil {
		if actionArray != 0 {
			api.cfRelease(actionArray)
		}
		return nil, err
	}
	if actionArray == 0 {
		return nil, ErrAccessibilityInvalidTree
	}
	defer api.cfRelease(actionArray)
	if requireCFType(api, actionArray, api.cfArrayGetTypeID(), "AX action names") != nil {
		return nil, ErrAccessibilityInvalidTree
	}
	count := api.cfArrayGetCount(actionArray)
	if count < 0 || count > maximumAXActions {
		return nil, ErrAccessibilityInvalidTree
	}
	actions := make(map[string]struct{}, 4)
	for index := int64(0); index < count; index++ {
		value := api.cfArrayGetValueAtIndex(actionArray, index)
		if value == 0 {
			continue
		}
		name, err := cfStringLocked(api, value)
		if err != nil {
			return nil, ErrAccessibilityInvalidTree
		}
		switch name {
		case "AXPress":
			switch role {
			case "checkbox", "radio", "switch":
				actions["toggle"] = struct{}{}
			case "button", "link", "menu-item", "tab":
				actions["press"] = struct{}{}
			}
		case "AXConfirm":
			if role == "textbox" {
				actions["press"] = struct{}{}
			}
		case "AXIncrement":
			actions["increment"] = struct{}{}
		case "AXDecrement":
			actions["decrement"] = struct{}{}
		case "AXShowMenu":
			actions["expand"] = struct{}{}
		}
	}
	resultActions := make([]string, 0, len(actions))
	for action := range actions {
		resultActions = append(resultActions, action)
	}
	sort.Strings(resultActions)
	return resultActions, nil
}

func semanticAXResult(operation string, result int32) error {
	if result == axErrorSuccess {
		return nil
	}
	switch result {
	case axErrorAPIDisabled:
		return fmt.Errorf("%w: %s", ErrPermission, operation)
	case axErrorInvalidUIElement:
		return fmt.Errorf("%w: %s", ErrAccessibilityStaleTarget, operation)
	case axErrorCannotComplete:
		return fmt.Errorf("%w: %s", ErrAccessibilityUnavailable, operation)
	case axErrorAttributeUnsupported, axErrorActionUnsupported,
		axErrorNotImplemented, axErrorParameterizedAttributeUnsupported:
		return fmt.Errorf("%w: %s", ErrUnsupported, operation)
	case axErrorFailure, axErrorIllegalArgument:
		return fmt.Errorf("%w: %s", ErrAccessibilityInvalidTree, operation)
	default:
		return fmt.Errorf("%w: %s returned AXError %d", ErrAccessibilityUnavailable, operation, result)
	}
}

func normalizeSemanticError(err error) error {
	switch {
	case errors.Is(err, ErrPermission), errors.Is(err, ErrUnsupported),
		errors.Is(err, ErrAccessibilityUnavailable), errors.Is(err, ErrAccessibilityStaleTarget),
		errors.Is(err, ErrAccessibilityInvalidTree):
		return err
	case errors.Is(err, errWindowUnavailable):
		return ErrAccessibilityStaleTarget
	default:
		return ErrAccessibilityUnavailable
	}
}

func runOnAXThread[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	completed := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		value, err := operation()
		completed <- result{value: value, err: err}
	}()
	// AXUIElementSetMessagingTimeout bounds native IPC. Waiting for the worker
	// guarantees all retained CoreFoundation objects are released before return.
	outcome := <-completed
	return outcome.value, outcome.err
}
