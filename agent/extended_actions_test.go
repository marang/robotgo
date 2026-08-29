package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
)

type actionOutcome struct {
	result ActionResult
	err    error
}

func extendedActionPolicy(operations ...Operation) Policy {
	return Policy{
		AllowedOperations:       append([]Operation(nil), operations...),
		AllowedDisplayIDs:       []int{0},
		AllowedMouseButtons:     []MouseButton{MouseButtonLeft},
		AllowedKeys:             []string{"c", "enter"},
		AllowedModifiers:        []KeyModifier{KeyModifierControl, KeyModifierShift},
		AllowedWindows:          []WindowTarget{{Target: 42, Kind: WindowTargetProcess, ExpectedTitle: "fixture"}},
		MaxActions:              10,
		MaxScrollEvents:         4,
		MaxScrollDistance:       100,
		MaxDragDistance:         100,
		MaxDragDurationMillis:   2_000,
		MaxChordKeys:            3,
		MinActionIntervalMillis: 1,
		SessionTimeoutMillis:    10_000,
	}
}

func TestExtendedOperationsAreDeniedByDefault(t *testing.T) {
	session := newTestSession(t, Policy{
		AllowedOperations: []Operation{OperationObserve},
		MaxObservations:   1,
	}, &fakeDriver{})
	for _, operation := range []Operation{
		OperationScroll, OperationDrag, OperationKeyChord, OperationActivate,
	} {
		capability, ok := session.capability(operation)
		if !ok {
			t.Fatalf("catalog omitted %s", operation)
		}
		if capability.PolicyAllowed {
			t.Fatalf("%s unexpectedly allowed by diagnostics-only policy", operation)
		}
	}
}

func TestWaylandActivationCapabilityDoesNotInheritBroadWindowSupport(t *testing.T) {
	policy, err := preparePolicy(extendedActionPolicy(OperationActivate))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := availableCapabilities()
	capabilities.Runtime.GOOS = "linux"
	capabilities.Runtime.DisplayServer = robotgo.DisplayServerWayland
	capabilities.Window.Available = true
	capabilities.Window.Backend = "wayland-compositor"
	catalog := buildCatalog(policy, capabilities)
	var activation OperationCapability
	for _, candidate := range catalog.Operations {
		if candidate.Operation == OperationActivate {
			activation = candidate
			break
		}
	}
	if activation.Available || activation.Fallback ||
		activation.UnavailableCode != ErrorUnsupported ||
		activation.Reason == "" || activation.Remediation == "" {
		t.Fatalf("Wayland activation capability = %+v", activation)
	}
}

func TestNativeMacOSActivationCapabilityRejectsHandleOnlyPolicy(t *testing.T) {
	input := extendedActionPolicy(OperationActivate)
	input.AllowedWindows = []WindowTarget{{
		Target: 42, Kind: WindowTargetHandle, ExpectedTitle: "fixture",
	}}
	policy, err := preparePolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := availableCapabilities()
	capabilities.Runtime.GOOS = "darwin"
	capabilities.Runtime.CGOEnabled = true
	capabilities.Window.Available = true
	capability := activationFeature(policy, capabilities)
	if capability.Available || capability.Reason == "" || capability.Notes == "" {
		t.Fatalf("native macOS handle activation = %+v", capability)
	}

	capabilities.Runtime.CGOEnabled = false
	if capability := activationFeature(policy, capabilities); !capability.Available {
		t.Fatalf("Pure-Go macOS handle activation = %+v", capability)
	}
}

func TestWaylandKeyChordCapabilityRequiresProcessTargetedInjection(t *testing.T) {
	for _, test := range []struct {
		backend string
	}{
		{backend: agentWindowBackendSway},
		{backend: agentWindowBackendHyprland},
		{backend: "wlroots-generic"},
	} {
		t.Run(test.backend, func(t *testing.T) {
			capabilities := availableCapabilities()
			capabilities.Runtime.GOOS = goOSLinux
			capabilities.Runtime.DisplayServer = robotgo.DisplayServerWayland
			capabilities.Window.Backend = test.backend
			capability := keyChordFeature(capabilities)
			if capability.Available ||
				featureUnavailableCode(capability) != ErrorUnsupported {
				t.Fatalf("keyboard.chord capability = %+v", capability)
			}
		})
	}
}

func TestWindowsKeyChordCapabilityRequiresAtomicTargetDispatch(t *testing.T) {
	for _, cgoEnabled := range []bool{false, true} {
		capabilities := availableCapabilities()
		capabilities.Runtime.GOOS = "windows"
		capabilities.Runtime.CGOEnabled = cgoEnabled
		capability := keyChordFeature(capabilities)
		if capability.Available ||
			featureUnavailableCode(capability) != ErrorUnsupported ||
			capability.Notes == "" {
			t.Fatalf(
				"Windows keyboard.chord capability with CGO=%t = %+v",
				cgoEnabled,
				capability,
			)
		}
	}
}

func TestExtendedCapabilityReportsPermissionAndUnavailableStates(t *testing.T) {
	policy, err := preparePolicy(extendedActionPolicy(OperationScroll))
	if err != nil {
		t.Fatal(err)
	}
	request := ActionRequest{
		Operation: OperationScroll,
		Scroll: &ScrollAction{
			TargetX: 1, TargetY: 1, DeltaY: 1, Events: 1, DisplayID: 0,
		},
	}
	for _, test := range []struct {
		name     string
		reason   string
		fallback bool
		code     ErrorCode
		cause    error
	}{
		{
			name: "permission", reason: robotgo.ErrPermissionDenied.Error(),
			code: ErrorPermissionDenied, cause: robotgo.ErrPermissionDenied,
		},
		{
			name: "unsupported", reason: robotgo.ErrNotSupported.Error(),
			fallback: true, code: ErrorUnsupported, cause: robotgo.ErrNotSupported,
		},
		{name: "unavailable", reason: "backend service is unavailable", code: ErrorUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			capabilities := availableCapabilities()
			capabilities.Mouse.Available = false
			capabilities.Mouse.Reason = test.reason
			capabilities.Mouse.Fallback = test.fallback
			session, sessionErr := newSession(policy, &fakeDriver{}, capabilities)
			if sessionErr != nil {
				t.Fatal(sessionErr)
			}
			t.Cleanup(func() { _ = session.Close() })
			capability, ok := session.capability(OperationScroll)
			if !ok || capability.Available || capability.Fallback != test.fallback ||
				capability.UnavailableCode != test.code {
				t.Fatalf("capability = %+v", capability)
			}
			result, executeErr := session.DryRun(t.Context(), request)
			var actionErr *ActionError
			if !errors.As(executeErr, &actionErr) || actionErr.Code != test.code {
				t.Fatalf("unavailable result = %+v, %v", result, executeErr)
			}
			if test.cause != nil && !errors.Is(executeErr, test.cause) {
				t.Fatalf("unavailable cause = %v", executeErr)
			}
		})
	}
}

func TestPureGoX11ScrollReportsAndEnforcesVerticalAxisOnly(t *testing.T) {
	driver := &fakeDriver{}
	policy, err := preparePolicy(extendedActionPolicy(OperationScroll))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := availableCapabilities()
	capabilities.Mouse.Backend = agentInputPureGoX11
	session, err := newSession(policy, driver, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	capability, ok := session.capability(OperationScroll)
	if !ok || !capability.Available ||
		len(capability.ScrollAxes) != 1 ||
		capability.ScrollAxes[0] != ScrollAxisVertical {
		t.Fatalf("Pure-Go X11 scroll capability = %+v", capability)
	}
	catalog := session.Catalog()
	for index := range catalog.Operations {
		if catalog.Operations[index].Operation == OperationScroll {
			catalog.Operations[index].ScrollAxes[0] = ScrollAxisHorizontal
		}
	}
	unchanged, _ := session.capability(OperationScroll)
	if unchanged.ScrollAxes[0] != ScrollAxisVertical {
		t.Fatal("catalog clone exposed mutable scroll axes")
	}

	result, executeErr := session.Execute(t.Context(), ActionRequest{
		Operation: OperationScroll,
		Scroll: &ScrollAction{
			TargetX: 1, TargetY: 1, DeltaX: 1, Events: 1, DisplayID: 0,
		},
	})
	var actionErr *ActionError
	if !errors.As(executeErr, &actionErr) ||
		actionErr.Code != ErrorUnsupported ||
		result.Status != ActionFailed {
		t.Fatalf("horizontal scroll result = %+v, %v", result, executeErr)
	}
	if calls := driver.recordedCalls(); len(calls) != 0 {
		t.Fatalf("unsupported horizontal scroll reached driver: %+v", calls)
	}
}

func TestScrollMovesToBoundedTargetAndEmitsExactEvents(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, extendedActionPolicy(OperationScroll), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationScroll,
		Scroll: &ScrollAction{
			TargetX: 12, TargetY: 23, DeltaX: -2, DeltaY: 3,
			Events: 3, DisplayID: 0,
		},
	})
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("scroll result = %+v, %v", result, err)
	}
	calls := driver.recordedCalls()
	if len(calls) != 4 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].operation != OperationMove || calls[0].x != 12 ||
		calls[0].y != 23 || calls[0].displayID != 0 || !calls[0].immediate {
		t.Fatalf("target move = %+v", calls[0])
	}
	for _, call := range calls[1:] {
		if call.operation != OperationScroll || call.deltaX != -2 ||
			call.deltaY != 3 || !call.immediate {
			t.Fatalf("scroll call = %+v", call)
		}
	}
}

func TestScrollPolicyRejectsTotalDistanceBeforeInput(t *testing.T) {
	driver := &fakeDriver{}
	policy := extendedActionPolicy(OperationScroll)
	policy.MaxScrollDistance = 5
	session := newTestSession(t, policy, driver)
	_, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationScroll,
		Scroll: &ScrollAction{
			TargetX: 1, TargetY: 1, DeltaY: 2, Events: 3, DisplayID: 0,
		},
	})
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("scroll error = %v", err)
	}
	if driver.callCount() != 0 {
		t.Fatalf("denied scroll reached driver: %+v", driver.recordedCalls())
	}
}

func TestScrollFailureAfterTargetMoveIsUnverified(t *testing.T) {
	driver := &fakeDriver{callError: func(call driverCall) error {
		if call.operation == OperationScroll {
			return errors.New("scroll backend failed")
		}
		return nil
	}}
	session := newTestSession(t, extendedActionPolicy(OperationScroll), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationScroll,
		Scroll: &ScrollAction{
			TargetX: 1, TargetY: 1, DeltaY: 1, Events: 2, DisplayID: 0,
		},
	})
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorBackendFailure ||
		result.Status != ActionUnverified {
		t.Fatalf("partial scroll result = %+v, %v", result, err)
	}
	calls := driver.recordedCalls()
	if len(calls) != 2 || calls[0].operation != OperationMove ||
		calls[1].operation != OperationScroll {
		t.Fatalf("partial scroll calls = %+v", calls)
	}
}

func TestScrollTargetMoveFailureIsUnverified(t *testing.T) {
	driver := &fakeDriver{callError: func(call driverCall) error {
		if call.operation == OperationMove {
			return errors.New("ambiguous target move failure")
		}
		return nil
	}}
	session := newTestSession(t, extendedActionPolicy(OperationScroll), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationScroll,
		Scroll: &ScrollAction{
			TargetX: 1, TargetY: 1, DeltaY: 1, Events: 1, DisplayID: 0,
		},
	})
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorBackendFailure ||
		result.Status != ActionUnverified {
		t.Fatalf("ambiguous target move result = %+v, %v", result, err)
	}
	calls := driver.recordedCalls()
	if len(calls) != 1 || calls[0].operation != OperationMove ||
		!calls[0].immediate {
		t.Fatalf("ambiguous target move calls = %+v", calls)
	}
}

func TestDragUsesBoundedPathAndAlwaysReleasesButton(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, extendedActionPolicy(OperationDrag), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationDrag,
		Confirmed: true,
		Drag: &DragAction{
			StartX: 10, StartY: 20, EndX: 13, EndY: 24,
			DisplayID: 0, Button: MouseButtonLeft, DurationMillis: 5,
		},
	})
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("drag result = %+v, %v", result, err)
	}
	calls := driver.recordedCalls()
	if len(calls) < 4 {
		t.Fatalf("drag calls = %+v", calls)
	}
	if calls[0].operation != OperationMove || calls[0].x != 10 || calls[0].y != 20 {
		t.Fatalf("drag start = %+v", calls[0])
	}
	if calls[1].operation != OperationDrag || !calls[1].down {
		t.Fatalf("drag press = %+v", calls[1])
	}
	lastMove := calls[len(calls)-2]
	if lastMove.operation != OperationMove || lastMove.x != 13 || lastMove.y != 24 {
		t.Fatalf("drag end = %+v", lastMove)
	}
	if release := calls[len(calls)-1]; release.operation != OperationDrag || release.down {
		t.Fatalf("drag release = %+v", release)
	}
	if len(session.pressedInputs) != 0 {
		t.Fatalf("pressed ledger = %+v", session.pressedInputs)
	}
}

func TestDragStartMoveFailureIsUnverified(t *testing.T) {
	driver := &fakeDriver{callError: func(call driverCall) error {
		if call.operation == OperationMove {
			return errors.New("ambiguous start move failure")
		}
		return nil
	}}
	session := newTestSession(t, extendedActionPolicy(OperationDrag), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationDrag,
		Confirmed: true,
		Drag: &DragAction{
			StartX: 1, StartY: 1, EndX: 2, EndY: 1,
			DisplayID: 0, Button: MouseButtonLeft, DurationMillis: 1,
		},
	})
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorBackendFailure ||
		result.Status != ActionUnverified {
		t.Fatalf("ambiguous start move result = %+v, %v", result, err)
	}
	calls := driver.recordedCalls()
	if len(calls) != 1 || calls[0].operation != OperationMove ||
		!calls[0].immediate {
		t.Fatalf("ambiguous start move calls = %+v", calls)
	}
}

func TestCanceledDragReleasesButton(t *testing.T) {
	pressed := make(chan struct{})
	driver := &fakeDriver{callHook: func(call driverCall) {
		if call.operation == OperationDrag && call.down {
			select {
			case <-pressed:
			default:
				close(pressed)
			}
		}
	}}
	session := newTestSession(t, extendedActionPolicy(OperationDrag), driver)
	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan actionOutcome, 1)
	go func() {
		result, err := session.Execute(ctx, ActionRequest{
			Operation: OperationDrag,
			Confirmed: true,
			Drag: &DragAction{
				StartX: 1, StartY: 1, EndX: 11, EndY: 1,
				DisplayID: 0, Button: MouseButtonLeft, DurationMillis: 1_000,
			},
		})
		resultChannel <- actionOutcome{result: result, err: err}
	}()
	select {
	case <-pressed:
	case <-time.After(time.Second):
		t.Fatal("drag did not press button")
	}
	cancel()
	select {
	case outcome := <-resultChannel:
		var actionErr *ActionError
		if !errors.As(outcome.err, &actionErr) || actionErr.Code != ErrorCanceled ||
			outcome.result.Status != ActionUnverified {
			t.Fatalf("drag cancellation outcome = %+v, %v", outcome.result, outcome.err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled drag did not return")
	}
	calls := driver.recordedCalls()
	if release := calls[len(calls)-1]; release.operation != OperationDrag || release.down {
		t.Fatalf("final call did not release drag: %+v", calls)
	}
}

func TestSessionCloseCancelsDragAndReleasesButton(t *testing.T) {
	pressed := make(chan struct{})
	driver := &fakeDriver{callHook: func(call driverCall) {
		if call.operation == OperationDrag && call.down {
			select {
			case <-pressed:
			default:
				close(pressed)
			}
		}
	}}
	policy, err := preparePolicy(extendedActionPolicy(OperationDrag))
	if err != nil {
		t.Fatal(err)
	}
	session, err := newSession(policy, driver, availableCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	resultChannel := make(chan actionOutcome, 1)
	go func() {
		result, executeErr := session.Execute(context.Background(), ActionRequest{
			Operation: OperationDrag,
			Confirmed: true,
			Drag: &DragAction{
				StartX: 1, StartY: 1, EndX: 11, EndY: 1,
				DisplayID: 0, Button: MouseButtonLeft, DurationMillis: 1_000,
			},
		})
		resultChannel <- actionOutcome{result: result, err: executeErr}
	}()
	select {
	case <-pressed:
	case <-time.After(time.Second):
		t.Fatal("drag did not press button")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close = %v", err)
	}
	select {
	case outcome := <-resultChannel:
		var actionErr *ActionError
		if !errors.As(outcome.err, &actionErr) || actionErr.Code != ErrorSessionClosed ||
			outcome.result.Status != ActionUnverified {
			t.Fatalf("execute outcome = %+v, %v", outcome.result, outcome.err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not stop active drag")
	}
	calls := driver.recordedCalls()
	if release := calls[len(calls)-1]; release.operation != OperationDrag || release.down {
		t.Fatalf("close left button pressed: %+v", calls)
	}
}

func TestCleanupFailureRetainsExclusiveOwnerUntilCloseRetrySucceeds(t *testing.T) {
	releaseFailures := 2
	driver := &fakeDriver{callError: func(call driverCall) error {
		if call.operation == OperationKeyChord && call.text == "" &&
			!call.down && releaseFailures > 0 {
			releaseFailures--
			return errors.New("release failed")
		}
		return nil
	}}
	policy, err := preparePolicy(extendedActionPolicy(OperationKeyChord))
	if err != nil {
		t.Fatal(err)
	}
	session, err := newSession(policy, driver, availableCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	_, _ = session.Execute(t.Context(), ActionRequest{
		Operation: OperationKeyChord,
		Confirmed: true,
		KeyChord:  &KeyChordAction{Key: "c", TargetPID: 42},
	})
	if err := session.Close(); !errors.Is(err, ErrInputCleanup) {
		t.Fatalf("first close = %v", err)
	}
	if replacement, replaceErr := newSession(policy, &fakeDriver{}, availableCapabilities()); replacement != nil ||
		!errors.Is(replaceErr, ErrSessionBusy) {
		t.Fatalf("replacement during retained ownership = %v, %v", replacement, replaceErr)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("cleanup retry = %v", err)
	}
	replacement, err := newSession(policy, &fakeDriver{}, availableCapabilities())
	if err != nil {
		t.Fatalf("replacement after cleanup = %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatalf("replacement close = %v", err)
	}
}

func TestAmbiguousInputDownFailureIsReleasedImmediately(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation Operation
		request   ActionRequest
	}{
		{
			name: "mouse", operation: OperationDrag,
			request: ActionRequest{
				Operation: OperationDrag, Confirmed: true,
				Drag: &DragAction{
					StartX: 1, StartY: 1, EndX: 2, EndY: 1,
					DisplayID: 0, Button: MouseButtonLeft, DurationMillis: 1,
				},
			},
		},
		{
			name: "keyboard", operation: OperationKeyChord,
			request: ActionRequest{
				Operation: OperationKeyChord, Confirmed: true,
				KeyChord: &KeyChordAction{Key: "c", TargetPID: 42},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := &fakeDriver{callError: func(call driverCall) error {
				if call.operation == test.operation && call.down {
					return errors.New("ambiguous down failure")
				}
				return nil
			}}
			session := newTestSession(
				t,
				extendedActionPolicy(test.operation),
				driver,
			)
			result, executeErr := session.Execute(t.Context(), test.request)
			if executeErr == nil || result.Status != ActionUnverified {
				t.Fatalf("ambiguous down result = %+v, %v", result, executeErr)
			}
			var mutationCalls []driverCall
			for _, call := range driver.recordedCalls() {
				if call.operation == test.operation && call.text == "" {
					mutationCalls = append(mutationCalls, call)
				}
			}
			if len(mutationCalls) != 2 ||
				!mutationCalls[0].down || mutationCalls[1].down ||
				len(session.pressedInputs) != 0 || session.inputTainted {
				t.Fatalf(
					"ambiguous down cleanup calls = %+v, ledger = %+v",
					mutationCalls,
					session.pressedInputs,
				)
			}
		})
	}
}

func TestAmbiguousKeyDownAndCleanupFailureRemainOwned(t *testing.T) {
	releaseFailures := 1
	driver := &fakeDriver{callError: func(call driverCall) error {
		if call.operation != OperationKeyChord || call.text != "" {
			return nil
		}
		if call.down {
			return errors.New("ambiguous down failure")
		}
		if releaseFailures > 0 {
			releaseFailures--
			return errors.New("release failed")
		}
		return nil
	}}
	policy, err := preparePolicy(extendedActionPolicy(OperationKeyChord))
	if err != nil {
		t.Fatal(err)
	}
	session, err := newSession(policy, driver, availableCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	result, executeErr := session.Execute(t.Context(), ActionRequest{
		Operation: OperationKeyChord, Confirmed: true,
		KeyChord: &KeyChordAction{Key: "c", TargetPID: 42},
	})
	var actionErr *ActionError
	if !errors.As(executeErr, &actionErr) ||
		actionErr.Code != ErrorCleanupFailed ||
		result.Status != ActionUnverified ||
		len(session.pressedInputs) != 1 || !session.inputTainted {
		t.Fatalf(
			"ambiguous cleanup failure = %+v, %v, ledger = %+v",
			result,
			executeErr,
			session.pressedInputs,
		)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close cleanup retry = %v", err)
	}
}

func TestKnownUnappliedMouseDownDoesNotReleaseForeignInput(t *testing.T) {
	for _, backendErr := range []error{
		robotgo.ErrInputNotApplied,
		robotgo.ErrInputOwnership,
	} {
		t.Run(backendErr.Error(), func(t *testing.T) {
			driver := &fakeDriver{callError: func(call driverCall) error {
				if call.operation == OperationDrag && call.down {
					return backendErr
				}
				return nil
			}}
			session := newTestSession(
				t,
				extendedActionPolicy(OperationDrag),
				driver,
			)
			result, err := session.Execute(t.Context(), ActionRequest{
				Operation: OperationDrag,
				Confirmed: true,
				Drag: &DragAction{
					StartX: 1, StartY: 1, EndX: 2, EndY: 1,
					DisplayID: 0, Button: MouseButtonLeft, DurationMillis: 1,
				},
			})
			if !errors.Is(err, backendErr) || result.Status != ActionUnverified {
				t.Fatalf("known-unapplied mouse down = %+v, %v", result, err)
			}
			var mutations []driverCall
			for _, call := range driver.recordedCalls() {
				if call.operation == OperationDrag && call.text == "" {
					mutations = append(mutations, call)
				}
			}
			if len(mutations) != 1 || !mutations[0].down ||
				len(session.pressedInputs) != 0 || session.inputTainted {
				t.Fatalf(
					"known-unapplied mouse cleanup = %+v, ledger = %+v",
					mutations,
					session.pressedInputs,
				)
			}
		})
	}
}

func TestKnownUnappliedKeyDownDoesNotReleaseForeignInput(t *testing.T) {
	for _, backendErr := range []error{
		robotgo.ErrInputNotApplied,
		robotgo.ErrInputOwnership,
	} {
		t.Run(backendErr.Error(), func(t *testing.T) {
			driver := &fakeDriver{callError: func(call driverCall) error {
				if call.operation == OperationKeyChord && call.down {
					return backendErr
				}
				return nil
			}}
			session := newTestSession(
				t,
				extendedActionPolicy(OperationKeyChord),
				driver,
			)
			result, err := session.Execute(t.Context(), ActionRequest{
				Operation: OperationKeyChord, Confirmed: true,
				KeyChord: &KeyChordAction{Key: "c", TargetPID: 42},
			})
			var actionErr *ActionError
			if !errors.Is(err, backendErr) ||
				!errors.As(err, &actionErr) ||
				actionErr.Code != ErrorBackendFailure ||
				result.Status != ActionFailed {
				t.Fatalf("known-unapplied key down = %+v, %v", result, err)
			}
			var mutations []driverCall
			for _, call := range driver.recordedCalls() {
				if call.operation == OperationKeyChord && call.text == "" {
					mutations = append(mutations, call)
				}
			}
			if len(mutations) != 1 || !mutations[0].down ||
				len(session.pressedInputs) != 0 || session.inputTainted {
				t.Fatalf(
					"known-unapplied key cleanup = %+v, ledger = %+v",
					mutations,
					session.pressedInputs,
				)
			}
		})
	}
}

func TestKeyChordUsesCanonicalOrderAndRelease(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, extendedActionPolicy(OperationKeyChord), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationKeyChord,
		Confirmed: true,
		KeyChord: &KeyChordAction{
			Key: "c", Modifiers: []KeyModifier{KeyModifierControl, KeyModifierShift},
			TargetPID: 42,
		},
	})
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("chord result = %+v, %v", result, err)
	}
	calls := driver.recordedCalls()
	var keyCalls []driverCall
	for _, call := range calls {
		if call.operation == OperationKeyChord && call.text == "" {
			keyCalls = append(keyCalls, call)
		}
	}
	if len(keyCalls) != 2 || !keyCalls[0].down || keyCalls[1].down ||
		keyCalls[0].key != "c" || keyCalls[0].target != 42 ||
		keyCalls[1].target != 42 ||
		!keyCalls[0].immediate || !keyCalls[1].immediate ||
		!sameModifiers(keyCalls[0].modifiers, keyCalls[1].modifiers) {
		t.Fatalf("chord calls = %+v", calls)
	}
}

func TestPureGoX11KeyChordIsUnavailableWithoutProcessTargeting(t *testing.T) {
	driver := &fakeDriver{}
	policy, err := preparePolicy(extendedActionPolicy(OperationKeyChord))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := availableCapabilities()
	capabilities.Runtime.GOOS = goOSLinux
	capabilities.Runtime.DisplayServer = robotgo.DisplayServerX11
	capabilities.Keyboard.Backend = agentInputPureGoX11
	session, err := newSession(policy, driver, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	capability, ok := session.capability(OperationKeyChord)
	if !ok || capability.Available ||
		capability.UnavailableCode != ErrorUnsupported {
		t.Fatalf("Pure-Go X11 chord capability = %+v", capability)
	}
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationKeyChord,
		Confirmed: true,
		KeyChord: &KeyChordAction{
			Key: "c", Modifiers: []KeyModifier{KeyModifierControl}, TargetPID: 42,
		},
	})
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorUnsupported ||
		result.Status != ActionFailed {
		t.Fatalf("Pure-Go X11 chord result = %+v, %v", result, err)
	}
	if calls := driver.recordedCalls(); len(calls) != 0 {
		t.Fatalf("unavailable Pure-Go X11 chord reached driver: %+v", calls)
	}
}

func TestNativeX11KeyChordIsUnavailableWithoutProcessTargeting(t *testing.T) {
	capabilities := availableCapabilities()
	capabilities.Runtime.GOOS = goOSLinux
	capabilities.Runtime.CGOEnabled = true
	capabilities.Runtime.DisplayServer = robotgo.DisplayServerX11
	capabilities.Keyboard.Backend = "x11"
	capability := keyChordFeature(capabilities)
	if capability.Available ||
		featureUnavailableCode(capability) != ErrorUnsupported {
		t.Fatalf("native X11 chord capability = %+v", capability)
	}
}

func TestKeyChordRejectsChangedTargetTitleBeforeInput(t *testing.T) {
	driver := &fakeDriver{windowTitles: []string{"fixture", "different fixture"}}
	session := newTestSession(t, extendedActionPolicy(OperationKeyChord), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationKeyChord,
		Confirmed: true,
		KeyChord: &KeyChordAction{
			Key: "c", Modifiers: []KeyModifier{KeyModifierControl}, TargetPID: 42,
		},
	})
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorStaleTarget ||
		result.Status != ActionFailed {
		t.Fatalf("stale chord result = %+v, %v", result, err)
	}
	for _, call := range driver.recordedCalls() {
		if call.operation == OperationKeyChord && call.text == "" {
			t.Fatalf("stale chord injected key input: %+v", driver.recordedCalls())
		}
	}
}

func TestKeyChordCancellationDuringFinalIdentityCheckPreventsInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	titleChecks := 0
	driver := &fakeDriver{callHook: func(call driverCall) {
		if call.text == "title" {
			titleChecks++
			if titleChecks == 2 {
				cancel()
			}
		}
	}}
	session := newTestSession(t, extendedActionPolicy(OperationKeyChord), driver)
	result, err := session.Execute(ctx, ActionRequest{
		Operation: OperationKeyChord,
		Confirmed: true,
		KeyChord: &KeyChordAction{
			Key: "c", Modifiers: []KeyModifier{KeyModifierControl}, TargetPID: 42,
		},
	})
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorCanceled ||
		result.Status != ActionFailed {
		t.Fatalf("canceled chord result = %+v, %v", result, err)
	}
	for _, call := range driver.recordedCalls() {
		if call.operation == OperationKeyChord && call.text == "" {
			t.Fatalf("canceled chord injected key input: %+v", driver.recordedCalls())
		}
	}
}

func TestKeyChordReleaseFailureIsRetriedOnClose(t *testing.T) {
	releaseFailures := 1
	driver := &fakeDriver{callError: func(call driverCall) error {
		if call.operation == OperationKeyChord && call.text == "" &&
			!call.down && releaseFailures > 0 {
			releaseFailures--
			return errors.New("release failed")
		}
		return nil
	}}
	policy, err := preparePolicy(extendedActionPolicy(OperationKeyChord))
	if err != nil {
		t.Fatal(err)
	}
	session, err := newSession(policy, driver, availableCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, executeErr := session.Execute(t.Context(), ActionRequest{
		Operation: OperationKeyChord,
		Confirmed: true,
		KeyChord: &KeyChordAction{
			Key: "c", Modifiers: []KeyModifier{KeyModifierControl}, TargetPID: 42,
		},
	})
	var actionErr *ActionError
	if !errors.As(executeErr, &actionErr) || actionErr.Code != ErrorCleanupFailed ||
		result.Status != ActionUnverified || len(session.pressedInputs) != 1 {
		t.Fatalf("release failure result = %+v, %v, ledger = %+v", result, executeErr, session.pressedInputs)
	}
	callsBeforeDeniedRetry := driver.callCount()
	_, retryErr := session.DryRun(t.Context(), ActionRequest{
		Operation: OperationKeyChord,
		Confirmed: true,
		KeyChord: &KeyChordAction{
			Key: "c", Modifiers: []KeyModifier{KeyModifierControl}, TargetPID: 42,
		},
	})
	if !errors.As(retryErr, &actionErr) || actionErr.Code != ErrorCleanupFailed {
		t.Fatalf("tainted session retry = %v", retryErr)
	}
	if driver.callCount() != callsBeforeDeniedRetry {
		t.Fatalf("tainted session reached driver: %+v", driver.recordedCalls())
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close retry = %v", err)
	}
	calls := driver.recordedCalls()
	var keyCalls []driverCall
	for _, call := range calls {
		if call.operation == OperationKeyChord && call.text == "" {
			keyCalls = append(keyCalls, call)
		}
	}
	if len(keyCalls) != 3 || keyCalls[2].down {
		t.Fatalf("release retry calls = %+v", calls)
	}
}

func TestDragDistanceAndInterpolationRemainSafeAtIntegerEdges(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	minimum := -maximum - 1
	for _, action := range []DragAction{
		{StartX: maximum - 100, EndX: maximum},
		{StartX: minimum, EndX: minimum + 100},
	} {
		if distance := dragDistance(action); distance != 100 {
			t.Fatalf("edge drag distance = %d for %+v", distance, action)
		}
		midpoint := interpolateCoordinate(action.StartX, action.EndX, 1, 2)
		if midpoint < min(action.StartX, action.EndX) ||
			midpoint > max(action.StartX, action.EndX) {
			t.Fatalf("edge midpoint escaped path: %d for %+v", midpoint, action)
		}
		if end := interpolateCoordinate(action.StartX, action.EndX, 2, 2); end != action.EndX {
			t.Fatalf("edge endpoint = %d, want %d", end, action.EndX)
		}
	}
	if distance := dragDistance(DragAction{StartX: minimum, EndX: maximum}); distance <= maxAgentDragDistance {
		t.Fatalf("cross-range drag distance = %d", distance)
	}
}

func TestDragUsesOnlyDelayFreeMovesWhileButtonIsOwned(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, extendedActionPolicy(OperationDrag), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationDrag, Confirmed: true,
		Drag: &DragAction{
			StartX: 1, StartY: 1, EndX: 4, EndY: 1,
			DisplayID: 0, Button: MouseButtonLeft, DurationMillis: 3,
		},
	})
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("drag result = %+v, %v", result, err)
	}
	moveCalls := 0
	for _, call := range driver.recordedCalls() {
		if call.operation != OperationMove {
			continue
		}
		moveCalls++
		if call.text != "immediate" {
			t.Fatalf("drag used delayed move: %+v", driver.recordedCalls())
		}
	}
	if moveCalls < 2 {
		t.Fatalf("drag move calls = %+v", driver.recordedCalls())
	}
}

func TestWindowActivationRevalidatesImmutableIdentity(t *testing.T) {
	driver := &fakeDriver{windowTitle: "fixture", resolvedHandle: 77}
	session := newTestSession(t, extendedActionPolicy(OperationActivate), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationActivate,
		Confirmed: true,
		Activate:  &ActivateWindowAction{Target: 42, Kind: WindowTargetProcess},
	})
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("activation result = %+v, %v", result, err)
	}
	calls := driver.recordedCalls()
	if len(calls) != 5 ||
		calls[0].text != "resolve" || calls[1].text != "title" ||
		calls[2].text != "resolve" || calls[3].text != "title" ||
		calls[4].text != "activate" ||
		calls[1].target != 77 || calls[3].target != 77 ||
		calls[4].target != 77 || calls[4].targetKind != WindowTargetHandle {
		t.Fatalf("activation calls = %+v", calls)
	}
}

func TestWindowActivationRejectsChangedIdentityBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		driver *fakeDriver
		calls  int
	}{
		{
			name: "stale at preflight", driver: &fakeDriver{windowTitle: "different fixture"},
			calls: 2,
		},
		{
			name:   "stale immediately before dispatch",
			driver: &fakeDriver{windowTitles: []string{"fixture", "different fixture"}},
			calls:  4,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := newTestSession(t, extendedActionPolicy(OperationActivate), test.driver)
			_, err := session.Execute(t.Context(), ActionRequest{
				Operation: OperationActivate,
				Confirmed: true,
				Activate:  &ActivateWindowAction{Target: 42, Kind: WindowTargetProcess},
			})
			var actionErr *ActionError
			if !errors.As(err, &actionErr) || actionErr.Code != ErrorStaleTarget {
				t.Fatalf("activation error = %v", err)
			}
			calls := test.driver.recordedCalls()
			if len(calls) != test.calls {
				t.Fatalf("stale activation calls = %+v", calls)
			}
			for _, call := range calls {
				if call.text == "activate" {
					t.Fatalf("stale activation reached mutation: %+v", calls)
				}
			}
		})
	}
}

func TestWindowActivationBackendErrorIsUnverified(t *testing.T) {
	backendErr := errors.New("ambiguous activation failure")
	driver := &fakeDriver{callError: func(call driverCall) error {
		if call.operation == OperationActivate && call.text == "activate" {
			return backendErr
		}
		return nil
	}}
	session := newTestSession(t, extendedActionPolicy(OperationActivate), driver)
	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationActivate,
		Confirmed: true,
		Activate:  &ActivateWindowAction{Target: 42, Kind: WindowTargetProcess},
	})
	var actionErr *ActionError
	if !errors.Is(err, backendErr) ||
		!errors.As(err, &actionErr) ||
		actionErr.Code != ErrorBackendFailure ||
		result.Status != ActionUnverified {
		t.Fatalf("ambiguous activation result = %+v, %v", result, err)
	}
	calls := driver.recordedCalls()
	if len(calls) != 5 || calls[4].text != "activate" {
		t.Fatalf("ambiguous activation calls = %+v", calls)
	}
}

func TestWindowActivationCancellationDuringFinalIdentityCheckPreventsMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	titleChecks := 0
	driver := &fakeDriver{callHook: func(call driverCall) {
		if call.operation == OperationActivate && call.text == "title" {
			titleChecks++
			if titleChecks == 2 {
				cancel()
			}
		}
	}}
	session := newTestSession(t, extendedActionPolicy(OperationActivate), driver)
	result, err := session.Execute(ctx, ActionRequest{
		Operation: OperationActivate,
		Confirmed: true,
		Activate:  &ActivateWindowAction{Target: 42, Kind: WindowTargetProcess},
	})
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorCanceled ||
		result.Status != ActionFailed {
		t.Fatalf("canceled activation result = %+v, %v", result, err)
	}
	for _, call := range driver.recordedCalls() {
		if call.operation == OperationActivate && call.text == "activate" {
			t.Fatalf("canceled activation reached mutation: %+v", driver.recordedCalls())
		}
	}
}

func TestExtendedActionRateAndSessionLifetimeAreBounded(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, extendedActionPolicy(OperationScroll), driver)
	now := time.Unix(1, 0)
	session.now = func() time.Time { return now }
	request := ActionRequest{
		Operation: OperationScroll,
		Scroll: &ScrollAction{
			TargetX: 1, TargetY: 1, DeltaY: 1, Events: 1, DisplayID: 0,
		},
	}
	if _, err := session.Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Execute(t.Context(), request); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("rate-limited action = %v", err)
	}

	timeoutPolicy := extendedActionPolicy(OperationScroll)
	timeoutPolicy.SessionTimeoutMillis = 1
	_ = session.Close()
	timeoutSession := newTestSession(t, timeoutPolicy, &fakeDriver{})
	select {
	case <-timeoutSession.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("session deadline did not expire")
	}
	_, err := timeoutSession.Execute(t.Context(), request)
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorTimedOut {
		t.Fatalf("expired session action = %v", err)
	}
}

func TestExtendedPolicyRejectsAliasesAndUnboundedOperations(t *testing.T) {
	tests := []Policy{
		extendedActionPolicy(OperationScroll),
		extendedActionPolicy(OperationDrag),
		extendedActionPolicy(OperationKeyChord),
		extendedActionPolicy(OperationActivate),
	}
	tests[0].MaxScrollEvents = 0
	tests[1].MaxDragDurationMillis = 0
	tests[2].AllowedModifiers = []KeyModifier{"ctrl"}
	tests[3].AllowedWindows[0].ExpectedTitle = ""
	for _, policy := range tests {
		if _, err := preparePolicy(policy); err == nil {
			t.Fatalf("invalid extended policy accepted: %+v", policy)
		}
	}
}
