//go:build windows

package accessibility

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/marang/robotgo/internal/windowswindow"
)

func probe(ctx context.Context) Capability {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := boundedWindowsUIAContext(ctx, 3*time.Second)
	defer cancel()
	err := runOnWindowsUIAThread(probeCtx, func() error {
		client, err := newUIAClient(probeCtx)
		if err != nil {
			return err
		}
		client.close()
		return nil
	})
	if err != nil {
		return windowsUIACapabilityError(err)
	}
	return Capability{
		Available: true,
		Backend:   BackendWindowsAutomation,
		Reason:    "Windows UI Automation is available",
		Notes:     "semantic inspection is read-only, process-scoped, bounded, and does not open a consent dialog",
	}
}

func boundedWindowsUIAContext(ctx context.Context, maximum time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= maximum {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, maximum)
}

func inspect(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if target.ExpectedTitle == "" || !validUIALimits(limits) ||
		(target.ProcessID > 0) == (target.NativeWindowHandle > 0) {
		return Snapshot{}, fmt.Errorf("uia inspect input: %w", ErrInvalidTree)
	}
	return runOnWindowsUIAThreadValue(ctx, func() (Snapshot, error) {
		return inspectWindowsUIA(ctx, target, limits)
	})
}

func act(ctx context.Context, request ActionRequest) (result ActionResult, resultErr error) {
	defer func() { result.CleanupComplete = true }()
	if ctx == nil {
		ctx = context.Background()
	}
	if request.Target.ExpectedTitle == "" || request.Expected.Sensitive ||
		(request.Target.ProcessID > 0) == (request.Target.NativeWindowHandle > 0) {
		return ActionResult{}, ErrStaleTarget
	}
	return runOnWindowsUIAThreadValue(ctx, func() (ActionResult, error) {
		return actWindowsUIA(ctx, request)
	})
}

func check(ctx context.Context, request ActionRequest) (result ConditionResult, resultErr error) {
	defer func() { result.CleanupComplete = true }()
	if ctx == nil {
		ctx = context.Background()
	}
	if request.Target.ExpectedTitle == "" || request.Expected.Sensitive ||
		(request.Target.ProcessID > 0) == (request.Target.NativeWindowHandle > 0) {
		return ConditionResult{}, ErrStaleTarget
	}
	if err := validateElementCondition(request); err != nil {
		return ConditionResult{}, err
	}
	return runOnWindowsUIAThreadValue(ctx, func() (ConditionResult, error) {
		return checkWindowsUIA(ctx, request)
	})
}

func actWindowsUIA(ctx context.Context, request ActionRequest) (ActionResult, error) {
	referencePID, referenceHandle, runtimeID, err := decodeUIAReference(request.Reference)
	if err != nil {
		return ActionResult{}, err
	}
	windowBackend := windowswindow.NewNative()
	isHandle := request.Target.NativeWindowHandle > 0
	windowTarget := request.Target.ProcessID
	if isHandle {
		windowTarget = request.Target.NativeWindowHandle
	}
	handle, err := windowBackend.Resolve(windowTarget, isHandle)
	if err != nil {
		return ActionResult{}, ErrStaleTarget
	}
	if uint64(uintptr(handle)) != referenceHandle {
		return ActionResult{}, ErrStaleTarget
	}
	processID, err := windowBackend.PID(handle)
	if err != nil || processID <= 0 || int32(processID) != referencePID ||
		request.Target.ProcessID > 0 && processID != request.Target.ProcessID {
		return ActionResult{}, ErrStaleTarget
	}
	title, err := windowBackend.Title(handle)
	if err != nil || title != request.Target.ExpectedTitle {
		return ActionResult{}, ErrStaleTarget
	}
	client, err := newUIAClient(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	defer client.close()
	root, err := client.elementFromHandle(ctx, uintptr(handle))
	if err != nil {
		return ActionResult{}, err
	}
	element, err := findUIAElement(ctx, client.query, root, referencePID, runtimeID)
	if err != nil {
		return ActionResult{}, err
	}
	defer element.Release()
	var role string
	validateElement := func() error {
		var err error
		role, err = validateUIAElement(ctx, client.query, element, request)
		return err
	}
	validateWindow := func() error {
		liveRoot, err := client.elementFromHandle(ctx, uintptr(handle))
		if err != nil {
			return err
		}
		liveElement, err := findUIAElement(ctx, client.query, liveRoot, referencePID, runtimeID)
		if err != nil {
			return err
		}
		liveElement.Release()
		livePID, pidErr := windowBackend.PID(handle)
		liveTitle, titleErr := windowBackend.Title(handle)
		if pidErr != nil || titleErr != nil || livePID != processID || liveTitle != request.Target.ExpectedTitle {
			return ErrStaleTarget
		}
		return nil
	}
	checkCondition := func() (bool, error) {
		return checkUIAElementCondition(ctx, client.query, element, request)
	}
	alreadySatisfied, err := finalActionGate(
		request.Postcondition,
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
		return ActionResult{}, err
	}
	if alreadySatisfied {
		return ActionResult{AlreadySatisfied: true}, nil
	}
	return dispatchUIAAction(ctx, element, role, request, validateElement, checkCondition, validateWindow)
}

func checkWindowsUIA(ctx context.Context, request ActionRequest) (ConditionResult, error) {
	referencePID, referenceHandle, runtimeID, err := decodeUIAReference(request.Reference)
	if err != nil {
		return ConditionResult{}, err
	}
	windowBackend := windowswindow.NewNative()
	isHandle := request.Target.NativeWindowHandle > 0
	windowTarget := request.Target.ProcessID
	if isHandle {
		windowTarget = request.Target.NativeWindowHandle
	}
	handle, err := windowBackend.Resolve(windowTarget, isHandle)
	if err != nil || uint64(uintptr(handle)) != referenceHandle {
		return ConditionResult{}, ErrStaleTarget
	}
	processID, err := windowBackend.PID(handle)
	if err != nil || processID <= 0 || int32(processID) != referencePID ||
		request.Target.ProcessID > 0 && processID != request.Target.ProcessID {
		return ConditionResult{}, ErrStaleTarget
	}
	title, err := windowBackend.Title(handle)
	if err != nil || title != request.Target.ExpectedTitle {
		return ConditionResult{}, ErrStaleTarget
	}
	client, err := newUIAClient(ctx)
	if err != nil {
		return ConditionResult{}, err
	}
	defer client.close()
	root, err := client.elementFromHandle(ctx, uintptr(handle))
	if err != nil {
		return ConditionResult{}, err
	}
	element, err := findUIAElement(ctx, client.query, root, referencePID, runtimeID)
	if err != nil {
		return ConditionResult{}, err
	}
	defer element.Release()
	satisfied, err := checkUIAElementCondition(ctx, client.query, element, request)
	if err != nil {
		return ConditionResult{}, err
	}
	liveRoot, err := client.elementFromHandle(ctx, uintptr(handle))
	if err != nil {
		return ConditionResult{}, err
	}
	liveElement, err := findUIAElement(ctx, client.query, liveRoot, referencePID, runtimeID)
	if err != nil {
		return ConditionResult{}, err
	}
	liveElement.Release()
	livePID, pidErr := windowBackend.PID(handle)
	liveTitle, titleErr := windowBackend.Title(handle)
	if pidErr != nil || titleErr != nil || livePID != processID || liveTitle != request.Target.ExpectedTitle {
		return ConditionResult{}, ErrStaleTarget
	}
	return ConditionResult{Satisfied: satisfied}, nil
}

func validateUIAElement(ctx context.Context, query *uiaCOMQuery, element *ole.IUnknown, request ActionRequest) (string, error) {
	structure, err := query.structure(ctx, element)
	if err != nil {
		return "", err
	}
	role := mapUIAControlType(structure.ControlType, structure.Password, structure.ToggleAvailable)
	if structure.Password || structure.Offscreen || role != request.Expected.Role {
		return "", ErrStaleTarget
	}
	details, err := query.details(ctx, element, role, Limits{
		MaxStringBytes: 1 << 20, ReadName: true, ReadStates: true,
		ReadBounds: true, ReadActions: true,
	})
	if err != nil {
		return "", err
	}
	if details.Name != request.Expected.Name || !slices.Equal(details.States, request.Expected.States) ||
		!equalAccessibilityBounds(details.Bounds, request.Expected.Bounds) ||
		!slices.Equal(details.Actions, request.Expected.Actions) ||
		!slices.Contains(details.Actions, request.Action) || slices.Contains(details.States, "disabled") {
		return "", ErrStaleTarget
	}
	return role, nil
}

func checkUIAElementCondition(
	ctx context.Context,
	query *uiaCOMQuery,
	element *ole.IUnknown,
	request ActionRequest,
) (bool, error) {
	if err := validateElementCondition(request); err != nil {
		return false, err
	}
	structure, err := query.structure(ctx, element)
	if err != nil {
		return false, err
	}
	role := mapUIAControlType(structure.ControlType, structure.Password, structure.ToggleAvailable)
	if structure.Password || structure.Offscreen || role != request.Expected.Role {
		return false, ErrStaleTarget
	}
	condition := request.Postcondition
	details, err := query.detailsWithDisabledActions(ctx, element, role, Limits{
		MaxStringBytes: 1 << 20, ReadName: true, ReadStates: true,
		ReadBounds: true, ReadActions: true,
		ReadFocus: condition.Kind == ElementConditionFocused || condition.Kind == ElementConditionNotFocused,
		ReadValue: condition.Kind == ElementConditionValueEqualsActionValue,
	}, true)
	if err != nil {
		return false, err
	}
	if !elementConditionObservable(
		condition, details.ObservableStates, details.FocusObservable, details.ValueObservable,
	) {
		return false, ErrUnsupported
	}
	if details.Name != request.Expected.Name ||
		!equalAccessibilityBounds(details.Bounds, request.Expected.Bounds) ||
		!conditionIdentityMatches(
			condition, request.Action, request.Expected.States, details.States,
			request.Expected.Actions, details.Actions,
		) {
		return false, ErrStaleTarget
	}
	return elementConditionSatisfied(
		condition, details.States, details.Focused, role, details.Value, request.Value,
	)
}

func findUIAElement(ctx context.Context, query *uiaCOMQuery, root *ole.IUnknown, processID int32, runtimeID []int32) (*ole.IUnknown, error) {
	const maxElements = 10_000
	const maxDepth = 64
	visited := 0
	var walk func(*ole.IUnknown, int) (*ole.IUnknown, error)
	walk = func(element *ole.IUnknown, depth int) (*ole.IUnknown, error) {
		if element == nil {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			element.Release()
			return nil, err
		}
		visited++
		if visited > maxElements || depth > maxDepth {
			element.Release()
			return nil, ErrStaleTarget
		}
		pid, err := query.processID(ctx, element)
		if err != nil {
			element.Release()
			return nil, err
		}
		if pid != processID {
			element.Release()
			return nil, nil
		}
		structure, err := query.structure(ctx, element)
		if err != nil {
			element.Release()
			return nil, err
		}
		if slices.Equal(structure.RuntimeID, runtimeID) {
			return element, nil
		}
		if depth == maxDepth || structure.Password || structure.Offscreen {
			element.Release()
			return nil, nil
		}
		child, err := query.firstChild(ctx, element)
		if err != nil {
			element.Release()
			return nil, err
		}
		for child != nil {
			next, nextErr := query.nextSibling(ctx, child)
			if nextErr != nil {
				child.Release()
				element.Release()
				return nil, nextErr
			}
			found, walkErr := walk(child, depth+1)
			if walkErr != nil || found != nil {
				if next != nil {
					next.Release()
				}
				element.Release()
				return found, walkErr
			}
			child = next
		}
		element.Release()
		return nil, nil
	}
	result, err := walk(root, 0)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrStaleTarget
	}
	return result, nil
}

func dispatchUIAAction(
	ctx context.Context,
	element *ole.IUnknown,
	role string,
	request ActionRequest,
	validateElement func() error,
	checkCondition func() (bool, error),
	validateWindow func() error,
) (ActionResult, error) {
	validateDispatch := func() (bool, error) {
		return finalActionGate(
			request.Postcondition,
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
	}
	gate := func() (*ActionResult, error) {
		alreadySatisfied, err := validateDispatch()
		if err != nil {
			return nil, err
		}
		if alreadySatisfied {
			result := ActionResult{AlreadySatisfied: true}
			return &result, nil
		}
		return nil, nil
	}
	if request.Action == "focus" {
		if result, err := gate(); err != nil {
			return ActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return ActionResult{Dispatched: true}, setUIAFocus(ctx, element)
	}
	patternID, method := int32(0), uiaPatternMethodPrimary
	switch request.Action {
	case "press":
		properties := newUIAPropertyReader(ctx, element, 1)
		invoke := false
		if uiaRoleUsesInvoke(role) {
			var err error
			invoke, err = properties.bool(uiaPropertyInvokeAvailable)
			if err != nil {
				return ActionResult{}, err
			}
		}
		if invoke {
			patternID = uiaPatternInvoke
		} else {
			selection, err := properties.bool(uiaPropertySelectionItemAvailable)
			if err != nil {
				return ActionResult{}, err
			}
			if uiaPatternAction(role, false, selection) != "press" {
				return ActionResult{}, ErrStaleTarget
			}
			patternID = uiaPatternSelectionItem
		}
	case "toggle":
		if !uiaRoleUsesToggle(role) {
			return ActionResult{}, ErrStaleTarget
		}
		properties := newUIAPropertyReader(ctx, element, 1)
		toggle, err := properties.bool(uiaPropertyToggleAvailable)
		if err != nil {
			return ActionResult{}, err
		}
		if !toggle {
			return ActionResult{}, ErrStaleTarget
		}
		patternID = uiaPatternToggle
	case "expand":
		patternID = uiaPatternExpandCollapse
	case "collapse":
		patternID, method = uiaPatternExpandCollapse, uiaPatternMethodSecondary
	case "set-value":
		if role == "slider" {
			value, err := strconv.ParseFloat(string(request.Value), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return ActionResult{}, ErrInvalidTree
			}
			pattern, err := currentUIAPattern(ctx, element, uiaPatternRangeValue)
			if err != nil {
				return ActionResult{}, ErrStaleTarget
			}
			defer pattern.Release()
			minimum, err := uiaPatternNumber(ctx, pattern, uiaRangeMethodMinimum)
			if err != nil {
				return ActionResult{}, err
			}
			maximum, err := uiaPatternNumber(ctx, pattern, uiaRangeMethodMaximum)
			if err != nil {
				return ActionResult{}, err
			}
			if err := validateExplicitRangeValue(value, minimum, maximum); err != nil {
				return ActionResult{}, err
			}
			if result, err := gate(); err != nil {
				return ActionResult{}, err
			} else if result != nil {
				return *result, nil
			}
			minimum, err = uiaPatternNumber(ctx, pattern, uiaRangeMethodMinimum)
			if err != nil {
				return ActionResult{}, err
			}
			maximum, err = uiaPatternNumber(ctx, pattern, uiaRangeMethodMaximum)
			if err != nil {
				return ActionResult{}, err
			}
			if err := validateExplicitRangeValue(value, minimum, maximum); err != nil {
				return ActionResult{}, err
			}
			if result, err := gate(); err != nil {
				return ActionResult{}, err
			} else if result != nil {
				return *result, nil
			}
			return ActionResult{Dispatched: true}, setUIARangeValue(ctx, pattern, value)
		}
		pattern, err := currentUIAPattern(ctx, element, uiaPatternValue)
		if err != nil {
			return ActionResult{}, ErrStaleTarget
		}
		defer pattern.Release()
		if result, err := gate(); err != nil {
			return ActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return ActionResult{Dispatched: true}, setUIAStringValue(ctx, pattern, string(request.Value))
	case "increment", "decrement":
		pattern, err := currentUIAPattern(ctx, element, uiaPatternRangeValue)
		if err != nil {
			return ActionResult{}, ErrStaleTarget
		}
		defer pattern.Release()
		current, err := uiaPatternNumber(ctx, pattern, uiaRangeMethodCurrent)
		if err != nil {
			return ActionResult{}, err
		}
		step, err := uiaPatternNumber(ctx, pattern, uiaRangeMethodSmallChange)
		if err != nil {
			return ActionResult{}, err
		}
		minimum, err := uiaPatternNumber(ctx, pattern, uiaRangeMethodMinimum)
		if err != nil {
			return ActionResult{}, err
		}
		maximum, err := uiaPatternNumber(ctx, pattern, uiaRangeMethodMaximum)
		if err != nil {
			return ActionResult{}, err
		}
		if _, err := nextBoundedStepValue(current, step, minimum, maximum, request.Action == "decrement"); err != nil {
			return ActionResult{}, ErrInvalidTree
		}
		if result, err := gate(); err != nil {
			return ActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		current, err = uiaPatternNumber(ctx, pattern, uiaRangeMethodCurrent)
		if err != nil {
			return ActionResult{}, err
		}
		step, err = uiaPatternNumber(ctx, pattern, uiaRangeMethodSmallChange)
		if err != nil {
			return ActionResult{}, err
		}
		minimum, err = uiaPatternNumber(ctx, pattern, uiaRangeMethodMinimum)
		if err != nil {
			return ActionResult{}, err
		}
		maximum, err = uiaPatternNumber(ctx, pattern, uiaRangeMethodMaximum)
		if err != nil {
			return ActionResult{}, err
		}
		next, err := nextBoundedStepValue(current, step, minimum, maximum, request.Action == "decrement")
		if err != nil {
			return ActionResult{}, ErrInvalidTree
		}
		if result, err := gate(); err != nil {
			return ActionResult{}, err
		} else if result != nil {
			return *result, nil
		}
		return ActionResult{Dispatched: true}, setUIARangeValue(ctx, pattern, next)
	default:
		return ActionResult{}, ErrUnsupported
	}
	pattern, err := currentUIAPattern(ctx, element, patternID)
	if err != nil {
		return ActionResult{}, ErrStaleTarget
	}
	defer pattern.Release()
	if result, err := gate(); err != nil {
		return ActionResult{}, err
	} else if result != nil {
		return *result, nil
	}
	return ActionResult{Dispatched: true}, callUIAPattern(ctx, pattern, method)
}

func inspectWindowsUIA(ctx context.Context, target Target, limits Limits) (Snapshot, error) {
	windowBackend := windowswindow.NewNative()
	isHandle := target.NativeWindowHandle > 0
	windowTarget := target.ProcessID
	if isHandle {
		windowTarget = target.NativeWindowHandle
	}
	handle, err := windowBackend.Resolve(windowTarget, isHandle)
	if err != nil {
		return Snapshot{}, ErrStaleTarget
	}
	processID, err := windowBackend.PID(handle)
	if err != nil || processID <= 0 {
		return Snapshot{}, ErrStaleTarget
	}
	if target.ProcessID > 0 && processID != target.ProcessID {
		return Snapshot{}, ErrStaleTarget
	}
	title, err := windowBackend.Title(handle)
	if err != nil || title != target.ExpectedTitle {
		return Snapshot{}, ErrStaleTarget
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	client, err := newUIAClient(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer client.close()
	root, err := client.elementFromHandle(ctx, uintptr(handle))
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := buildUIATree(ctx, client.query, root, int32(processID), uint64(uintptr(handle)), limits)
	if err != nil {
		return Snapshot{}, err
	}

	// Re-resolve both immutable policy fields after traversal. UIA runtime IDs
	// are opaque and recyclable, so HWND/PID/title validation remains the
	// authoritative observation boundary.
	liveProcessID, pidErr := windowBackend.PID(handle)
	liveTitle, titleErr := windowBackend.Title(handle)
	if pidErr != nil || titleErr != nil || liveProcessID != processID || liveTitle != target.ExpectedTitle {
		clearSnapshot(&snapshot)
		return Snapshot{}, ErrStaleTarget
	}
	return snapshot, nil
}

func windowsUIACapabilityError(err error) Capability {
	if errors.Is(err, ErrPermissionDenied) {
		return Capability{
			Reason:           "Windows denied access to UI Automation",
			Notes:            "run in the interactive user's desktop session with permission to inspect the target application",
			PermissionDenied: true,
		}
	}
	if errors.Is(err, ErrUnsupported) {
		return Capability{
			Reason: "bounded Windows UI Automation is unsupported by this Windows installation",
			Notes:  "Windows UI Automation 2 with configurable connection and transaction timeouts is required",
		}
	}
	return Capability{
		Reason: "Windows UI Automation is unavailable",
		Notes:  "run RobotGo in an interactive Windows desktop session",
	}
}

func runOnWindowsUIAThread(ctx context.Context, operation func() error) error {
	_, err := runOnWindowsUIAThreadValue(ctx, func() (struct{}, error) {
		return struct{}{}, operation()
	})
	return err
}

func runOnWindowsUIAThreadValue[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	completed := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := initializeUIAThread(); err != nil {
			var zero T
			completed <- result{value: zero, err: err}
			return
		}
		defer uninitializeUIAThread()
		value, err := operation()
		completed <- result{value: value, err: err}
	}()
	// Do not abandon the worker on cancellation: UIA2 applies a strict native
	// transaction timeout, and waiting here guarantees COM and BSTR/SAFEARRAY
	// cleanup before the API returns.
	outcome := <-completed
	return outcome.value, outcome.err
}
