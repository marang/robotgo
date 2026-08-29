//go:build darwin

package darwinwindow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
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

func validateAccessibilityElementCondition(request AccessibilityActionRequest) error {
	condition := request.Postcondition
	if condition == nil {
		return ErrAccessibilityInvalidTree
	}
	switch condition.Kind {
	case AccessibilityElementConditionStatePresent, AccessibilityElementConditionStateAbsent:
		if !validAccessibilityConditionState(condition.State) {
			return ErrAccessibilityInvalidTree
		}
	case AccessibilityElementConditionFocused, AccessibilityElementConditionNotFocused:
		if condition.State != "" {
			return ErrAccessibilityInvalidTree
		}
	case AccessibilityElementConditionValueEqualsActionValue:
		if condition.State != "" || request.Action != "set-value" ||
			len(request.Value) > maximumAXStringBytes {
			return ErrAccessibilityInvalidTree
		}
	default:
		return ErrAccessibilityInvalidTree
	}
	return nil
}

func validAccessibilityConditionState(state string) bool {
	switch state {
	case accessibilityStateEnabled, accessibilityStateDisabled, accessibilityStateChecked,
		accessibilityStateSelected, accessibilityStateExpanded, accessibilityStateCollapsed,
		accessibilityStateReadOnly, accessibilityStateRequired, accessibilityStateInvalid:
		return true
	default:
		return false
	}
}

func accessibilityElementConditionSatisfied(
	condition *AccessibilityElementCondition,
	liveStates []string,
	focused bool,
	role, liveValue string,
	valueTruncated bool,
	actionValue []byte,
) (bool, error) {
	if condition == nil {
		return false, ErrAccessibilityInvalidTree
	}
	switch condition.Kind {
	case AccessibilityElementConditionStatePresent:
		return slices.Contains(liveStates, condition.State), nil
	case AccessibilityElementConditionStateAbsent:
		return !slices.Contains(liveStates, condition.State), nil
	case AccessibilityElementConditionFocused:
		return focused, nil
	case AccessibilityElementConditionNotFocused:
		return !focused, nil
	case AccessibilityElementConditionValueEqualsActionValue:
		if valueTruncated {
			return false, nil
		}
		if role == "slider" {
			live, liveErr := strconv.ParseFloat(liveValue, 64)
			want, wantErr := strconv.ParseFloat(string(actionValue), 64)
			if liveErr != nil || wantErr != nil || math.IsNaN(live) || math.IsNaN(want) ||
				math.IsInf(live, 0) || math.IsInf(want, 0) {
				return false, ErrAccessibilityInvalidTree
			}
			return live == want, nil
		}
		return liveValue == string(actionValue), nil
	default:
		return false, ErrAccessibilityInvalidTree
	}
}

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

func actAccessibility(ctx context.Context, request AccessibilityActionRequest) (AccessibilityActionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if request.Target.ExpectedTitle == "" || request.Expected.Sensitive ||
		(request.Target.ProcessID > 0) == (request.Target.CGWindowID > 0) {
		return AccessibilityActionResult{CleanupComplete: true}, ErrAccessibilityStaleTarget
	}
	called := false
	result, err := runOnAXThread(ctx, func() (AccessibilityActionResult, error) {
		called = true
		return actAccessibilityOnThread(ctx, request)
	})
	if !called {
		result.CleanupComplete = true
	}
	return result, err
}

func checkAccessibility(ctx context.Context, request AccessibilityActionRequest) (AccessibilityConditionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if request.Target.ExpectedTitle == "" || request.Expected.Sensitive ||
		(request.Target.ProcessID > 0) == (request.Target.CGWindowID > 0) {
		return AccessibilityConditionResult{CleanupComplete: true}, ErrAccessibilityStaleTarget
	}
	if err := validateAccessibilityElementCondition(request); err != nil {
		return AccessibilityConditionResult{CleanupComplete: true}, err
	}
	called := false
	result, err := runOnAXThread(ctx, func() (AccessibilityConditionResult, error) {
		called = true
		return checkAccessibilityOnThread(ctx, request)
	})
	if !called {
		result.CleanupComplete = true
	}
	return result, err
}

func checkAccessibilityOnThread(
	ctx context.Context,
	request AccessibilityActionRequest,
) (result AccessibilityConditionResult, resultErr error) {
	cleanupComplete := true
	defer func() { result.CleanupComplete = cleanupComplete }()
	referencePID, referenceWindow, path, err := decodeAccessibilityReference(request.Reference)
	if err != nil || len(path) > 64 {
		return AccessibilityConditionResult{}, ErrAccessibilityStaleTarget
	}
	api, setupCleanupComplete, err := openNativeAPI()
	cleanupComplete = cleanupComplete && setupCleanupComplete
	if err != nil {
		return AccessibilityConditionResult{}, err
	}
	defer func() {
		if err := api.close(); err != nil {
			cleanupComplete = false
		}
	}()
	if !api.axIsProcessTrusted() {
		return AccessibilityConditionResult{}, ErrPermission
	}
	pid, err := semanticTargetPID(api, request.Target)
	if err != nil || pid != referencePID {
		return AccessibilityConditionResult{}, ErrAccessibilityStaleTarget
	}
	application := api.axUIElementCreateApplication(pid)
	if application == 0 {
		return AccessibilityConditionResult{}, ErrAccessibilityStaleTarget
	}
	defer api.cfRelease(application)
	if err := semanticAXResult(
		"set AX messaging timeout",
		api.axUIElementSetMessagingTimeout(application, axSemanticTimeoutSeconds),
	); err != nil {
		return AccessibilityConditionResult{}, err
	}
	root, windowID, err := semanticTargetWindow(ctx, api, application, request.Target, pid)
	if err != nil {
		return AccessibilityConditionResult{}, err
	}
	if windowID != referenceWindow {
		api.cfRelease(root)
		return AccessibilityConditionResult{}, ErrAccessibilityStaleTarget
	}
	rootBounds, err := semanticElementRect(api, root)
	if err != nil {
		api.cfRelease(root)
		return AccessibilityConditionResult{}, err
	}
	query := &nativeAXSemanticQuery{
		api: api, rootBounds: accessibilityWindowRect(rootBounds),
		maxChildren: maximumAXChildren, bounds: make(map[uintptr]*AccessibilityBounds),
	}
	element, err := resolveAXPath(ctx, query, root, path, pid)
	if err != nil {
		return AccessibilityConditionResult{}, err
	}
	defer func() { query.release(element) }()
	satisfied, err := checkAXElementCondition(ctx, query, element, request)
	if err != nil {
		return AccessibilityConditionResult{}, err
	}
	// The observation reference is a structural child-index path, not a
	// retained native pointer. Resolve it again so a sibling replacement or
	// reorder cannot make this probe attest the former path occupant.
	freshElement, err := resolveLiveAXPath(ctx, api, query, application, windowID, path, pid)
	if err != nil {
		return AccessibilityConditionResult{}, err
	}
	query.release(element)
	element = freshElement
	satisfied, err = checkAXElementCondition(ctx, query, element, request)
	if err != nil {
		return AccessibilityConditionResult{}, err
	}
	if err := validateAXActionWindow(ctx, api, query, application, element, pid, windowID, request.Target.ExpectedTitle); err != nil {
		return AccessibilityConditionResult{}, err
	}
	return AccessibilityConditionResult{Satisfied: satisfied}, nil
}

func actAccessibilityOnThread(ctx context.Context, request AccessibilityActionRequest) (result AccessibilityActionResult, resultErr error) {
	cleanupComplete := true
	defer func() { result.CleanupComplete = cleanupComplete }()
	referencePID, referenceWindow, path, err := decodeAccessibilityReference(request.Reference)
	if err != nil || len(path) > 64 {
		return AccessibilityActionResult{}, ErrAccessibilityStaleTarget
	}
	api, setupCleanupComplete, err := openNativeAPI()
	cleanupComplete = cleanupComplete && setupCleanupComplete
	if err != nil {
		return AccessibilityActionResult{}, err
	}
	defer func() {
		if err := api.close(); err != nil {
			cleanupComplete = false
		}
	}()
	if !api.axIsProcessTrusted() {
		return AccessibilityActionResult{}, ErrPermission
	}
	pid, err := semanticTargetPID(api, request.Target)
	if err != nil || pid != referencePID {
		return AccessibilityActionResult{}, ErrAccessibilityStaleTarget
	}
	application := api.axUIElementCreateApplication(pid)
	if application == 0 {
		return AccessibilityActionResult{}, ErrAccessibilityStaleTarget
	}
	defer api.cfRelease(application)
	if err := semanticAXResult("set AX messaging timeout", api.axUIElementSetMessagingTimeout(application, axSemanticTimeoutSeconds)); err != nil {
		return AccessibilityActionResult{}, err
	}
	root, windowID, err := semanticTargetWindow(ctx, api, application, request.Target, pid)
	if err != nil {
		return AccessibilityActionResult{}, err
	}
	if windowID != referenceWindow {
		api.cfRelease(root)
		return AccessibilityActionResult{}, ErrAccessibilityStaleTarget
	}
	rootBounds, err := semanticElementRect(api, root)
	if err != nil {
		api.cfRelease(root)
		return AccessibilityActionResult{}, err
	}
	query := &nativeAXSemanticQuery{
		api: api, rootBounds: accessibilityWindowRect(rootBounds),
		maxChildren: maximumAXChildren, bounds: make(map[uintptr]*AccessibilityBounds),
	}
	element, err := resolveAXPath(ctx, query, root, path, pid)
	if err != nil {
		return AccessibilityActionResult{}, err
	}
	defer func() { query.release(element) }()
	refreshElement := func() error {
		freshElement, err := resolveLiveAXPath(ctx, api, query, application, windowID, path, pid)
		if err != nil {
			return err
		}
		query.release(element)
		element = freshElement
		return nil
	}
	var role string
	validateElement := func() error {
		if err := refreshElement(); err != nil {
			return err
		}
		var err error
		role, err = validateAXActionElement(ctx, query, element, request)
		return err
	}
	checkCondition := func() (bool, error) {
		if err := refreshElement(); err != nil {
			return false, err
		}
		return checkAXElementCondition(ctx, query, element, request)
	}
	validateWindow := func() error {
		return validateAXActionWindow(
			ctx, api, query, application, element, pid, windowID,
			request.Target.ExpectedTitle,
		)
	}
	alreadySatisfied, err := finalAccessibilityActionGate(
		request.Postcondition,
		func() error {
			if request.BeforeFinalGate == nil {
				return nil
			}
			return request.BeforeFinalGate(ctx)
		},
		func() (bool, error) {
			satisfied, err := checkCondition()
			if err != nil {
				return false, err
			}
			if err := validateWindow(); err != nil {
				return false, err
			}
			return satisfied, nil
		},
		func() error {
			if err := validateElement(); err != nil {
				return err
			}
			return validateWindow()
		},
	)
	if err != nil {
		return AccessibilityActionResult{}, err
	}
	if alreadySatisfied {
		return AccessibilityActionResult{AlreadySatisfied: true}, nil
	}
	return dispatchAXAction(
		ctx, api, func() uintptr { return element }, role, request,
		validateElement, checkCondition, validateWindow,
	)
}

func validateAXActionWindow(
	ctx context.Context,
	api *nativeAPI,
	query *nativeAXSemanticQuery,
	application, element uintptr,
	processID int32,
	windowID uint32,
	expectedTitle string,
) error {
	owner, err := query.processID(ctx, element)
	if err != nil {
		return err
	}
	if owner != processID {
		return ErrAccessibilityStaleTarget
	}
	if err := validateAXElementWindow(api, element, windowID); err != nil {
		return err
	}
	liveRoot, err := applicationWindowByID(ctx, api, application, windowID)
	if err != nil {
		return err
	}
	defer api.cfRelease(liveRoot)
	liveOwner, ownerErr := query.processID(ctx, liveRoot)
	liveTitle, titleErr := semanticStringAttribute(api, liveRoot, api.axTitleAttribute)
	if ownerErr != nil {
		return ownerErr
	}
	if titleErr != nil {
		return titleErr
	}
	if liveOwner != processID || liveTitle != expectedTitle {
		return ErrAccessibilityStaleTarget
	}
	return nil
}

func validateAXActionElement(
	ctx context.Context,
	query *nativeAXSemanticQuery,
	element uintptr,
	request AccessibilityActionRequest,
) (string, error) {
	structure, err := query.structure(ctx, element)
	if err != nil {
		return "", err
	}
	if structure.Sensitive || structure.Hidden || structure.Offscreen || structure.Role != request.Expected.Role {
		return "", ErrAccessibilityStaleTarget
	}
	details, err := query.details(ctx, element, structure.Role, AccessibilityLimits{
		MaxStringBytes: 1 << 20, ReadName: true, ReadStates: true,
		ReadBounds: true, ReadActions: true,
	})
	if err != nil {
		return "", err
	}
	if details.Name != request.Expected.Name || !slices.Equal(details.States, request.Expected.States) ||
		!equalAccessibilityActionBounds(details.Bounds, request.Expected.Bounds) ||
		!slices.Equal(details.Actions, request.Expected.Actions) ||
		!slices.Contains(details.Actions, request.Action) || slices.Contains(details.States, accessibilityStateDisabled) {
		return "", ErrAccessibilityStaleTarget
	}
	return structure.Role, nil
}

func checkAXElementCondition(
	ctx context.Context,
	query *nativeAXSemanticQuery,
	element uintptr,
	request AccessibilityActionRequest,
) (bool, error) {
	if err := validateAccessibilityElementCondition(request); err != nil {
		return false, err
	}
	structure, err := query.structure(ctx, element)
	if err != nil {
		return false, err
	}
	if structure.Sensitive || structure.Hidden || structure.Offscreen || structure.Role != request.Expected.Role {
		return false, ErrAccessibilityStaleTarget
	}
	condition := request.Postcondition
	details, err := query.details(ctx, element, structure.Role, AccessibilityLimits{
		MaxStringBytes: 1 << 20, ReadName: true, ReadStates: true,
		ReadBounds: true, ReadActions: true,
		ReadFocus: condition.Kind == AccessibilityElementConditionFocused ||
			condition.Kind == AccessibilityElementConditionNotFocused,
		ReadValue: condition.Kind == AccessibilityElementConditionValueEqualsActionValue,
	})
	if err != nil {
		return false, err
	}
	if !accessibilityElementConditionObservable(
		condition, details.ObservableStates, details.FocusObservable,
		details.ValueObservable || details.ValueTruncated,
	) {
		return false, ErrUnsupported
	}
	if details.Name != request.Expected.Name ||
		!equalAccessibilityActionBounds(details.Bounds, request.Expected.Bounds) ||
		!accessibilityConditionIdentityMatches(
			condition, request.Action, request.Expected.States, details.States,
			request.Expected.Actions, details.Actions,
		) {
		return false, ErrAccessibilityStaleTarget
	}
	return accessibilityElementConditionSatisfied(
		condition, details.States, details.Focused,
		structure.Role, details.Value, details.ValueTruncated, request.Value,
	)
}

func resolveLiveAXPath(
	ctx context.Context,
	api *nativeAPI,
	query *nativeAXSemanticQuery,
	application uintptr,
	windowID uint32,
	path []uint32,
	processID int32,
) (uintptr, error) {
	root, err := applicationWindowByID(ctx, api, application, windowID)
	if err != nil {
		return 0, err
	}
	return resolveAXPath(ctx, query, root, path, processID)
}

func equalAccessibilityActionBounds(left, right *AccessibilityBounds) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func validateAXElementWindow(api *nativeAPI, element uintptr, windowID uint32) error {
	var liveWindowID uint32
	if err := semanticAXResult("revalidate AX element window", api.axUIElementGetWindow(element, &liveWindowID)); err != nil {
		return err
	}
	if liveWindowID != windowID {
		return ErrAccessibilityStaleTarget
	}
	return nil
}

func dispatchAXAction(
	ctx context.Context,
	api *nativeAPI,
	element func() uintptr,
	role string,
	request AccessibilityActionRequest,
	validateElement func() error,
	checkCondition func() (bool, error),
	validateWindow func() error,
) (AccessibilityActionResult, error) {
	if err := ctx.Err(); err != nil {
		return AccessibilityActionResult{}, err
	}
	gate := func() (*AccessibilityActionResult, error) {
		alreadySatisfied, err := finalAccessibilityActionGate(
			request.Postcondition,
			func() error {
				if request.BeforeFinalGate == nil {
					return nil
				}
				return request.BeforeFinalGate(ctx)
			},
			func() (bool, error) {
				satisfied, err := checkCondition()
				if err != nil {
					return false, err
				}
				if err := validateWindow(); err != nil {
					return false, err
				}
				return satisfied, nil
			},
			func() error {
				if err := validateElement(); err != nil {
					return err
				}
				return validateWindow()
			},
		)
		if err != nil {
			return nil, err
		}
		if alreadySatisfied {
			result := AccessibilityActionResult{AlreadySatisfied: true}
			return &result, nil
		}
		return nil, nil
	}
	switch request.Action {
	case "press":
		action := api.axPressAction
		if role == "textbox" {
			action = api.axConfirmAction
		}
		if result, err := gate(); err != nil {
			return AccessibilityActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
			return semanticAXResult("perform semantic AX action", api.axUIElementPerformAction(element(), action))
		})
	case "toggle":
		if result, err := gate(); err != nil {
			return AccessibilityActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
			return semanticAXResult("perform semantic AX action", api.axUIElementPerformAction(element(), api.axPressAction))
		})
	case "increment":
		if result, err := gate(); err != nil {
			return AccessibilityActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
			return semanticAXResult("perform semantic AX action", api.axUIElementPerformAction(element(), api.axIncrementAction))
		})
	case "decrement":
		if result, err := gate(); err != nil {
			return AccessibilityActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
			return semanticAXResult("perform semantic AX action", api.axUIElementPerformAction(element(), api.axDecrementAction))
		})
	case "focus":
		if result, err := gate(); err != nil {
			return AccessibilityActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
			return semanticAXResult("focus semantic AX element", api.axUIElementSetAttributeValue(element(), api.axFocusedAttribute, api.cfBooleanTrue))
		})
	case "expand", "collapse":
		settable, err := semanticAttributeSettable(api, element(), api.axExpandedAttribute)
		if err != nil {
			return AccessibilityActionResult{}, err
		}
		if settable {
			value := api.cfBooleanTrue
			if request.Action == "collapse" {
				value = api.cfBooleanFalse
			}
			if result, err := gate(); err != nil {
				return AccessibilityActionResult{}, err
			} else if result != nil {
				return *result, nil
			}
			return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
				return semanticAXResult("change semantic AX expansion", api.axUIElementSetAttributeValue(element(), api.axExpandedAttribute, value))
			})
		}
		if request.Action == "expand" {
			if result, err := gate(); err != nil {
				return AccessibilityActionResult{}, err
			} else if result != nil {
				return *result, nil
			}
			return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
				return semanticAXResult("show semantic AX menu", api.axUIElementPerformAction(element(), api.axShowMenuAction))
			})
		}
		return AccessibilityActionResult{}, ErrAccessibilityStaleTarget
	case "set-value":
		if role == "slider" {
			value, err := strconv.ParseFloat(string(request.Value), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return AccessibilityActionResult{}, ErrAccessibilityInvalidTree
			}
			number := api.cfNumberCreate(0, cfNumberFloat64Type, unsafe.Pointer(&value))
			runtime.KeepAlive(value)
			if number == 0 {
				return AccessibilityActionResult{}, ErrAccessibilityUnavailable
			}
			defer api.cfRelease(number)
			if result, err := gate(); err != nil {
				return AccessibilityActionResult{}, err
			} else if result != nil {
				return *result, nil
			}
			return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
				return semanticAXResult("set semantic AX numeric value", api.axUIElementSetAttributeValue(element(), api.axValueAttribute, number))
			})
		}
		value, err := api.createTransientString(request.Value)
		if err != nil {
			return AccessibilityActionResult{}, err
		}
		defer api.cfRelease(value)
		if result, err := gate(); err != nil {
			return AccessibilityActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return dispatchAccessibilityMutation(ctx, request.BeforeDispatch, func() error {
			return semanticAXResult("set semantic AX text value", api.axUIElementSetAttributeValue(element(), api.axValueAttribute, value))
		})
	default:
		return AccessibilityActionResult{}, ErrUnsupported
	}
}

func inspectAccessibilityOnThread(
	ctx context.Context,
	target AccessibilityTarget,
	limits AccessibilityLimits,
) (AccessibilitySnapshot, error) {
	api, _, err := openNativeAPI()
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
		valueLimit := int64(limits.MaxStringBytes)
		if valueLimit > maximumAXStringBytes {
			valueLimit = maximumAXStringBytes
		}
		details.Value, details.ValueObservable, details.ValueTruncated, err =
			semanticValueAttribute(query.api, element, role, valueLimit)
		if err != nil {
			return axSemanticDetails{}, err
		}
		if err := ctx.Err(); err != nil {
			return axSemanticDetails{}, err
		}
	}
	if limits.ReadStates {
		details.States, details.ObservableStates, err = semanticStates(query.api, element, role)
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
		details.Focused, details.FocusObservable, err = semanticOptionalBoolAttribute(
			query.api, element, query.api.axFocusedAttribute,
		)
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

func semanticOptionalControlValueState(
	api *nativeAPI,
	element, attribute uintptr,
	role string,
) (bool, bool, error) {
	value, ok, err := semanticOptionalAttribute(api, element, attribute)
	if err != nil || !ok {
		return false, ok, err
	}
	defer api.cfRelease(value)
	actualType := api.cfGetTypeID(value)
	if booleanType := api.cfBooleanGetTypeID(); booleanType != 0 && actualType == booleanType {
		return api.cfBooleanGetValue(value), true, nil
	}
	if numberType := api.cfNumberGetTypeID(); numberType != 0 && actualType == numberType {
		var number int32
		if !api.cfNumberGetValue(value, cfNumberSInt32Type, unsafe.Pointer(&number)) {
			return false, false, ErrAccessibilityInvalidTree
		}
		active, valid := accessibilityControlValueState(role, number)
		if !valid {
			return false, false, ErrAccessibilityInvalidTree
		}
		return active, true, nil
	}
	return false, false, ErrAccessibilityInvalidTree
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

func semanticValueAttribute(
	api *nativeAPI,
	element uintptr,
	role string,
	maxBytes int64,
) (string, bool, bool, error) {
	value, ok, err := semanticOptionalAttribute(api, element, api.axValueAttribute)
	if err != nil || !ok {
		return "", false, false, err
	}
	defer api.cfRelease(value)
	switch api.cfGetTypeID(value) {
	case api.cfStringGetTypeID():
		text, truncated, err := cfStringBytesLocked(api, value, maxBytes)
		return text, err == nil && !truncated, truncated, err
	case api.cfNumberGetTypeID():
		if role != "slider" && role != "progress" {
			return "", false, false, nil
		}
		var number float64
		if !api.cfNumberGetValue(value, cfNumberFloat64Type, unsafe.Pointer(&number)) ||
			math.IsNaN(number) || math.IsInf(number, 0) {
			return "", false, false, ErrAccessibilityInvalidTree
		}
		return strconv.FormatFloat(number, 'g', -1, 64), true, false, nil
	default:
		return "", false, false, nil
	}
}

func semanticStates(api *nativeAPI, element uintptr, role string) ([]string, []string, error) {
	states := make([]string, 0, 5)
	observable := make([]string, 0, 6)
	enabled, hasEnabled, err := semanticOptionalBoolAttribute(api, element, api.axEnabledAttribute)
	if err != nil {
		return nil, nil, err
	}
	if hasEnabled {
		observable = append(observable, accessibilityStateEnabled, accessibilityStateDisabled)
		if enabled {
			states = append(states, "enabled")
		} else {
			states = append(states, "disabled")
		}
	}
	if accessibilityRoleValueState(role) != "" {
		active, present, stateErr := semanticOptionalControlValueState(
			api, element, api.axValueAttribute, role,
		)
		if stateErr != nil {
			return nil, nil, stateErr
		}
		states, stateErr = appendAccessibilityValueState(states, role, active, present)
		if stateErr != nil {
			return nil, nil, stateErr
		}
		observable = append(observable, accessibilityRoleValueState(role))
	}
	for _, candidate := range []struct {
		attribute uintptr
		trueState string
	}{
		{api.axSelectedAttribute, "selected"},
		{api.axExpandedAttribute, "expanded"},
	} {
		if role == "radio" && candidate.trueState == "selected" {
			continue
		}
		value, present, stateErr := semanticOptionalBoolAttribute(api, element, candidate.attribute)
		if stateErr != nil {
			return nil, nil, stateErr
		}
		if present {
			observable = append(observable, candidate.trueState)
			if candidate.trueState == accessibilityStateExpanded {
				observable = append(observable, accessibilityStateCollapsed)
			}
		}
		if present && value {
			states = append(states, candidate.trueState)
		}
		if present && !value && candidate.trueState == "expanded" {
			states = append(states, "collapsed")
		}
	}
	return states, observable, nil
}

func semanticActions(api *nativeAPI, element uintptr, role string) ([]string, error) {
	nativeActions, err := semanticNativeActions(api, element)
	if err != nil {
		return nil, err
	}
	actions := make(map[string]struct{}, 6)
	if _, available := nativeActions[axPressActionName]; available {
		switch role {
		case "checkbox", "switch":
			actions["toggle"] = struct{}{}
		case "button", "link", "menu-item", "tab", "radio":
			actions["press"] = struct{}{}
		}
	}
	if _, available := nativeActions[axConfirmActionName]; available && role == "textbox" {
		actions["press"] = struct{}{}
	}
	if _, available := nativeActions[axIncrementActionName]; available {
		actions["increment"] = struct{}{}
	}
	if _, available := nativeActions[axDecrementActionName]; available {
		actions["decrement"] = struct{}{}
	}
	expanded, hasExpanded, err := semanticOptionalBoolAttribute(api, element, api.axExpandedAttribute)
	if err != nil {
		return nil, err
	}
	expandedSettable, err := semanticAttributeSettable(api, element, api.axExpandedAttribute)
	if err != nil {
		return nil, err
	}
	_, showMenu := nativeActions[axShowMenuActionName]
	if action := expansionAction(expanded, hasExpanded, expandedSettable, showMenu); action != "" {
		actions[action] = struct{}{}
	}
	focusSettable, err := semanticAttributeSettable(api, element, api.axFocusedAttribute)
	if err != nil {
		return nil, err
	}
	if focusSettable {
		actions["focus"] = struct{}{}
	}
	if role == "textbox" || role == "combobox" || role == "slider" {
		valueSettable, err := semanticAttributeSettable(api, element, api.axValueAttribute)
		if err != nil {
			return nil, err
		}
		if valueSettable {
			actions["set-value"] = struct{}{}
		}
	}
	resultActions := make([]string, 0, len(actions))
	for action := range actions {
		resultActions = append(resultActions, action)
	}
	sort.Strings(resultActions)
	return resultActions, nil
}

func semanticNativeActions(api *nativeAPI, element uintptr) (map[string]struct{}, error) {
	var actionArray uintptr
	result := api.axUIElementCopyActionNames(element, &actionArray)
	if result == axErrorActionUnsupported || result == axErrorNoValue || result == axErrorNotImplemented {
		return map[string]struct{}{}, nil
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
	actions := make(map[string]struct{}, count)
	for index := int64(0); index < count; index++ {
		value := api.cfArrayGetValueAtIndex(actionArray, index)
		if value == 0 {
			continue
		}
		name, err := cfStringLocked(api, value)
		if err != nil {
			return nil, ErrAccessibilityInvalidTree
		}
		actions[name] = struct{}{}
	}
	return actions, nil
}

func semanticAttributeSettable(api *nativeAPI, element, attribute uintptr) (bool, error) {
	var settable bool
	result := api.axUIElementIsAttributeSettable(element, attribute, &settable)
	if result == axErrorAttributeUnsupported || result == axErrorNoValue || result == axErrorNotImplemented {
		return false, nil
	}
	if err := semanticAXResult("check AX attribute mutability", result); err != nil {
		return false, err
	}
	return settable, nil
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
