package agent

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
)

type driverCall struct {
	operation  Operation
	text       string
	down       bool
	x          int
	y          int
	displayID  int
	deltaX     int
	deltaY     int
	button     MouseButton
	key        string
	modifiers  []KeyModifier
	target     int
	targetKind WindowTargetKind
	immediate  bool
}

type fakeDriver struct {
	mu               sync.Mutex
	calls            []driverCall
	err              error
	bounds           map[int]displayBounds
	boundsErr        error
	boundsHit        chan struct{}
	boundsGo         chan struct{}
	started          chan struct{}
	release          chan struct{}
	captureImages    []image.Image
	captureErr       error
	captureCalls     int
	captureHit       chan struct{}
	captureGo        chan struct{}
	capabilities     *robotgo.RuntimeCapabilities
	capabilityHit    chan struct{}
	capabilityGo     chan struct{}
	windowTitle      string
	windowTitles     []string
	windowTitleCalls int
	resolvedHandle   int
	callHook         func(driverCall)
	callError        func(driverCall) error
}

func (d *fakeDriver) DisplayBounds(displayID int) (displayBounds, error) {
	if d.boundsHit != nil {
		select {
		case d.boundsHit <- struct{}{}:
		default:
		}
	}
	if d.boundsGo != nil {
		<-d.boundsGo
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.boundsErr != nil {
		return displayBounds{}, d.boundsErr
	}
	if bounds, ok := d.bounds[displayID]; ok {
		return bounds, nil
	}
	switch displayID {
	case 0:
		return displayBounds{x: 0, y: 0, width: 100, height: 100}, nil
	case 2:
		return displayBounds{x: -100, y: 0, width: 100, height: 100}, nil
	default:
		return displayBounds{}, errors.New("display bounds unavailable")
	}
}

func (d *fakeDriver) record(call driverCall) error {
	if d.callHook != nil {
		d.callHook(call)
	}
	if d.started != nil {
		select {
		case d.started <- struct{}{}:
		default:
		}
	}
	if d.release != nil {
		<-d.release
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	call.modifiers = append([]KeyModifier(nil), call.modifiers...)
	d.calls = append(d.calls, call)
	if d.callError != nil {
		return d.callError(call)
	}
	return d.err
}

func (d *fakeDriver) MoveImmediate(x, y, displayID int) error {
	return d.record(driverCall{
		operation: OperationMove, text: "immediate", immediate: true,
		x: x, y: y, displayID: displayID,
	})
}

func (d *fakeDriver) ClickImmediate(button MouseButton, _ bool) error {
	return d.record(driverCall{
		operation: OperationClick, button: button, immediate: true,
	})
}

func (d *fakeDriver) ScrollImmediate(deltaX, deltaY int) error {
	return d.record(driverCall{
		operation: OperationScroll, deltaX: deltaX, deltaY: deltaY,
		immediate: true,
	})
}

func (d *fakeDriver) ToggleMouse(button MouseButton, down bool) error {
	return d.record(driverCall{operation: OperationDrag, button: button, down: down})
}

func (d *fakeDriver) TypeTextImmediate(text string) error {
	return d.record(driverCall{
		operation: OperationTypeText, text: text, immediate: true,
	})
}

func (d *fakeDriver) ToggleKeyImmediate(
	key string,
	modifiers []KeyModifier,
	targetPID int,
	down bool,
) error {
	return d.record(driverCall{
		operation: OperationKeyChord, key: key, modifiers: modifiers,
		target: targetPID, down: down, immediate: true,
	})
}

func (d *fakeDriver) ResolveWindow(target int, kind WindowTargetKind) (int, error) {
	call := driverCall{
		operation: OperationActivate, text: "resolve",
		target: target, targetKind: kind,
	}
	if err := d.record(call); err != nil {
		return 0, err
	}
	if d.resolvedHandle != 0 {
		return d.resolvedHandle, nil
	}
	return target, nil
}

func (d *fakeDriver) WindowTitle(target int, kind WindowTargetKind) (string, error) {
	call := driverCall{
		operation: OperationActivate, text: "title",
		target: target, targetKind: kind,
	}
	if err := d.record(call); err != nil {
		return "", err
	}
	return d.nextWindowTitle(), nil
}

func (d *fakeDriver) nextWindowTitle() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.windowTitles) > 0 {
		index := d.windowTitleCalls
		d.windowTitleCalls++
		if index >= len(d.windowTitles) {
			index = len(d.windowTitles) - 1
		}
		return d.windowTitles[index]
	}
	if d.windowTitle != "" {
		return d.windowTitle
	}
	return "fixture"
}

func (d *fakeDriver) ActivateWindow(target int, kind WindowTargetKind) error {
	return d.record(driverCall{
		operation: OperationActivate, text: "activate",
		target: target, targetKind: kind,
	})
}

func (d *fakeDriver) RuntimeCapabilities() robotgo.RuntimeCapabilities {
	if d.capabilityHit != nil {
		select {
		case d.capabilityHit <- struct{}{}:
		default:
		}
	}
	if d.capabilityGo != nil {
		<-d.capabilityGo
	}
	if d.capabilities != nil {
		return *d.capabilities
	}
	return availableCapabilities()
}

func (d *fakeDriver) Capture(_ context.Context, region CaptureRegion) (image.Image, error) {
	if d.captureHit != nil {
		select {
		case d.captureHit <- struct{}{}:
		default:
		}
	}
	if d.captureGo != nil {
		<-d.captureGo
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	call := d.captureCalls
	d.captureCalls++
	if d.captureErr != nil {
		if call < len(d.captureImages) {
			return d.captureImages[call], d.captureErr
		}
		return nil, d.captureErr
	}
	if len(d.captureImages) != 0 {
		if call >= len(d.captureImages) {
			call = len(d.captureImages) - 1
		}
		return d.captureImages[call], nil
	}
	if d.err != nil {
		return nil, d.err
	}
	img := image.NewRGBA(image.Rect(0, 0, region.Width, region.Height))
	img.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	return img, nil
}

func (d *fakeDriver) captureCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.captureCalls
}

func (d *fakeDriver) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func (d *fakeDriver) recordedCalls() []driverCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := append([]driverCall(nil), d.calls...)
	for index := range result {
		result[index].modifiers = append([]KeyModifier(nil), result[index].modifiers...)
	}
	return result
}

func availableCapabilities() robotgo.RuntimeCapabilities {
	return robotgo.RuntimeCapabilities{
		Capture:  robotgo.FeatureCapability{Available: true, Backend: "fake-capture"},
		Bounds:   robotgo.FeatureCapability{Available: true, Backend: "fake-bounds"},
		Mouse:    robotgo.FeatureCapability{Available: true, Backend: "fake-mouse"},
		Keyboard: robotgo.FeatureCapability{Available: true, Backend: "fake-keyboard"},
		Window:   robotgo.FeatureCapability{Available: true, Backend: "fake-window"},
	}
}

func testPolicy() Policy {
	return Policy{
		AllowedOperations: []Operation{OperationMove, OperationClick, OperationTypeText},
		AllowedDisplayIDs: []int{0, 2}, MaxActions: 3, MaxTextRunes: 8,
		AllowedMouseButtons: []MouseButton{MouseButtonLeft, MouseButtonMiddle, MouseButtonRight},
		AllowDoubleClick:    true,
	}
}

func newTestSession(t *testing.T, input Policy, driver inputDriver) *Session {
	t.Helper()
	policy, err := preparePolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	session, err := newSession(policy, driver, availableCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestCatalogIsStableAndDefensive(t *testing.T) {
	session := newTestSession(t, testPolicy(), &fakeDriver{})
	catalog := session.Catalog()
	if catalog.SchemaVersion != CatalogSchemaVersion {
		t.Fatalf("schema = %q", catalog.SchemaVersion)
	}
	want := []Operation{
		OperationObserve, OperationFindColor, OperationWaitColor,
		OperationMove, OperationClick, OperationScroll, OperationDrag,
		OperationTypeText, OperationKeyChord, OperationActivate,
	}
	for index, operation := range want {
		got := catalog.Operations[index]
		if got.Operation != operation || !got.Available || !got.ExclusiveAgentSession {
			t.Fatalf("operation[%d] = %+v", index, got)
		}
		wantCancellation := CancellationPreflightOnly
		if operation == OperationFindColor || operation == OperationWaitColor ||
			operation == OperationScroll || operation == OperationDrag ||
			operation == OperationKeyChord {
			wantCancellation = CancellationCooperative
		}
		if got.Cancellation != wantCancellation {
			t.Fatalf("cancellation = %q", got.Cancellation)
		}
		if got.ProcessGlobalBackend != (operation != OperationFindColor) {
			t.Fatalf("process-global operation[%d] = %+v", index, got)
		}
	}
	catalog.Operations[3].Backend = "mutated"
	if got := session.Catalog().Operations[3].Backend; got != "fake-mouse" {
		t.Fatalf("catalog mutation leaked into session: %q", got)
	}
}

func TestDryRunDoesNotInjectOrConsumeQuota(t *testing.T) {
	driver := &fakeDriver{}
	policy := testPolicy()
	policy.MaxActions = 1
	session := newTestSession(t, policy, driver)
	request := ActionRequest{
		Operation: OperationMove,
		Move:      &MoveAction{X: -10, Y: 20, DisplayID: 2},
	}
	result, err := session.DryRun(context.Background(), request)
	if err != nil || result.Status != ActionPlanned {
		t.Fatalf("DryRun = %+v, %v", result, err)
	}
	if driver.callCount() != 0 {
		t.Fatal("dry-run injected input")
	}
	if result, err = session.Execute(context.Background(), request); err != nil || result.Status != ActionSucceeded {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if driver.callCount() != 1 {
		t.Fatalf("driver calls = %d", driver.callCount())
	}
	result, err = session.Execute(context.Background(), request)
	if err == nil || result.Error == nil || result.Error.Code != ErrorPolicyDenied {
		t.Fatalf("quota result = %+v, %v", result, err)
	}
	result, err = session.DryRun(context.Background(), request)
	if err == nil || result.Error == nil || result.Error.Code != ErrorPolicyDenied {
		t.Fatalf("dry-run quota result = %+v, %v", result, err)
	}
}

func TestAgentMutationsUseOnlyDelayFreeDriverMethods(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, testPolicy(), driver)
	requests := []ActionRequest{
		{
			Operation: OperationMove,
			Move:      &MoveAction{X: 1, Y: 2, DisplayID: 0},
		},
		{
			Operation: OperationClick,
			Click:     &ClickAction{Button: MouseButtonLeft},
		},
		{
			Operation: OperationTypeText,
			TypeText:  &TypeTextAction{Text: "bounded"},
		},
	}
	for _, request := range requests {
		result, err := session.Execute(t.Context(), request)
		if err != nil || result.Status != ActionSucceeded {
			t.Fatalf("%s result = %+v, %v", request.Operation, result, err)
		}
	}
	calls := driver.recordedCalls()
	if len(calls) != len(requests) {
		t.Fatalf("delay-free driver calls = %+v", calls)
	}
	for _, call := range calls {
		if !call.immediate {
			t.Fatalf("agent used a legacy delayed driver method: %+v", calls)
		}
	}
}

func TestValidationAndPolicyRunBeforeDriver(t *testing.T) {
	driver := &fakeDriver{}
	policy := testPolicy()
	policy.ConfirmOperations = []Operation{OperationClick}
	policy.AllowDoubleClick = false
	session := newTestSession(t, policy, driver)
	tests := []ActionRequest{
		{Operation: OperationMove, Move: &MoveAction{DisplayID: 9}},
		{Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft}},
		{Operation: OperationClick, Confirmed: true, Click: &ClickAction{Button: MouseButtonLeft, Double: true}},
		{Operation: OperationTypeText, TypeText: &TypeTextAction{Text: "too-long!"}},
		{Operation: OperationTypeText, Move: &MoveAction{DisplayID: 0}, TypeText: &TypeTextAction{Text: "x"}},
	}
	for _, request := range tests {
		if _, err := session.Execute(context.Background(), request); err == nil {
			t.Fatalf("request %+v unexpectedly succeeded", request)
		}
	}
	if driver.callCount() != 0 {
		t.Fatalf("rejected requests reached driver %d times", driver.callCount())
	}
}

func TestMoveCoordinatesMustBeWithinAllowedDisplay(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, testPolicy(), driver)

	for _, request := range []ActionRequest{
		{
			Operation: OperationMove,
			Move:      &MoveAction{X: -10, Y: 20, DisplayID: 0},
		},
		{
			Operation: OperationMove,
			Move:      &MoveAction{X: 100, Y: 20, DisplayID: 0},
		},
	} {
		result, err := session.DryRun(context.Background(), request)
		if !errors.Is(err, ErrPolicyDenied) || result.Error == nil || result.Error.Code != ErrorPolicyDenied {
			t.Fatalf("out-of-display move = %+v, %v", result, err)
		}
	}
	if driver.callCount() != 0 {
		t.Fatalf("out-of-display moves reached driver %d times", driver.callCount())
	}

	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationMove,
		Move:      &MoveAction{X: -10, Y: 20, DisplayID: 2},
	})
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("in-display move = %+v, %v", result, err)
	}
	if driver.callCount() != 1 {
		t.Fatalf("driver calls = %d", driver.callCount())
	}
}

func TestDisplayBoundsFailureDoesNotReachInput(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code ErrorCode
	}{
		{name: "unsupported", err: robotgo.ErrNotSupported, code: ErrorUnsupported},
		{name: "backend", err: errors.New("bounds failed"), code: ErrorBackendFailure},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			driver := &fakeDriver{boundsErr: tc.err}
			session := newTestSession(t, testPolicy(), driver)
			result, err := session.Execute(context.Background(), ActionRequest{
				Operation: OperationMove,
				Move:      &MoveAction{X: 10, Y: 20, DisplayID: 0},
			})
			if !errors.Is(err, tc.err) || result.Error == nil || result.Error.Code != tc.code {
				t.Fatalf("bounds failure = %+v, %v", result, err)
			}
			if driver.callCount() != 0 {
				t.Fatalf("failed bounds lookup reached driver %d times", driver.callCount())
			}
		})
	}
}

func TestMoveCancellationDuringBoundsPreflightDoesNotReachInput(t *testing.T) {
	driver := &fakeDriver{boundsHit: make(chan struct{}, 1), boundsGo: make(chan struct{})}
	session := newTestSession(t, testPolicy(), driver)
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := session.Execute(ctx, ActionRequest{
			Operation: OperationMove,
			Move:      &MoveAction{X: 10, Y: 20, DisplayID: 0},
		})
		resultCh <- result
		errCh <- err
	}()
	<-driver.boundsHit
	cancel()
	close(driver.boundsGo)
	result, err := <-resultCh, <-errCh
	if !errors.Is(err, context.Canceled) || result.Error == nil || result.Error.Code != ErrorCanceled {
		t.Fatalf("bounds preflight cancellation = %+v, %v", result, err)
	}
	if driver.callCount() != 0 {
		t.Fatalf("canceled bounds preflight reached input %d times", driver.callCount())
	}
}

func TestTypedTextIsAbsentFromResultAndSerializedError(t *testing.T) {
	const secret = "s3cr3t"
	driver := &fakeDriver{err: errors.New("backend detail")}
	session := newTestSession(t, testPolicy(), driver)
	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationTypeText,
		TypeText:  &TypeTextAction{Text: secret},
	})
	if err == nil || result.Status != ActionUnverified ||
		result.Error == nil || result.Error.Code != ErrorBackendFailure {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "backend detail") {
		t.Fatalf("serialized result leaked payload or backend detail: %s", data)
	}
	if !errors.Is(err, driver.err) {
		t.Fatalf("returned error lost backend cause: %v", err)
	}
}

func TestSimpleMutationBackendErrorsAreUnverified(t *testing.T) {
	tests := []struct {
		name    string
		request ActionRequest
	}{
		{
			name: "move",
			request: ActionRequest{
				Operation: OperationMove,
				Move:      &MoveAction{X: 1, Y: 2, DisplayID: 0},
			},
		},
		{
			name: "click",
			request: ActionRequest{
				Operation: OperationClick,
				Click:     &ClickAction{Button: MouseButtonLeft},
			},
		},
		{
			name: "type text",
			request: ActionRequest{
				Operation: OperationTypeText,
				TypeText:  &TypeTextAction{Text: "bounded"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backendErr := errors.New("ambiguous mutation failure")
			driver := &fakeDriver{err: backendErr}
			session := newTestSession(t, testPolicy(), driver)
			result, err := session.Execute(t.Context(), test.request)
			var actionErr *ActionError
			if !errors.Is(err, backendErr) ||
				!errors.As(err, &actionErr) ||
				actionErr.Code != ErrorBackendFailure ||
				result.Status != ActionUnverified {
				t.Fatalf("ambiguous mutation result = %+v, %v", result, err)
			}
			if driver.callCount() != 1 {
				t.Fatalf("ambiguous mutation calls = %+v", driver.recordedCalls())
			}
		})
	}
}

func TestRetainedClickReleaseTaintsSessionUntilCloseRetry(t *testing.T) {
	releaseErr := errors.New("release unconfirmed")
	driver := &fakeDriver{}
	driver.callError = func(call driverCall) error {
		if call.operation == OperationClick {
			return errors.Join(robotgo.ErrInputReleasePending, releaseErr)
		}
		return nil
	}
	session := newTestSession(t, testPolicy(), driver)

	result, err := session.Execute(t.Context(), ActionRequest{
		Operation: OperationClick,
		Click:     &ClickAction{Button: MouseButtonLeft},
	})
	if !errors.Is(err, releaseErr) || !errors.Is(err, ErrInputCleanup) ||
		result.Status != ActionUnverified || result.Error == nil ||
		result.Error.Code != ErrorCleanupFailed {
		t.Fatalf("retained click result = %+v, %v", result, err)
	}
	if !session.inputTainted || len(session.pressedInputs) != 1 {
		t.Fatalf("retained click state: tainted=%v ledger=%+v", session.inputTainted, session.pressedInputs)
	}

	blocked, blockedErr := session.Execute(t.Context(), ActionRequest{
		Operation: OperationMove,
		Move:      &MoveAction{X: 1, Y: 2, DisplayID: 0},
	})
	if !errors.Is(blockedErr, ErrInputCleanup) || blocked.Error == nil ||
		blocked.Error.Code != ErrorCleanupFailed || driver.callCount() != 1 {
		t.Fatalf("action after retained click = %+v, %v, calls=%+v", blocked, blockedErr, driver.recordedCalls())
	}

	if err := session.Close(); err != nil {
		t.Fatalf("close retry: %v", err)
	}
	calls := driver.recordedCalls()
	if len(calls) != 2 || calls[1].down || calls[1].button != MouseButtonLeft {
		t.Fatalf("close retry calls = %+v", calls)
	}
	if session.inputTainted || len(session.pressedInputs) != 0 {
		t.Fatalf("close retry state: tainted=%v ledger=%+v", session.inputTainted, session.pressedInputs)
	}
}

func TestContextIsPreflightOnlyOnceDriverStarts(t *testing.T) {
	driver := &fakeDriver{started: make(chan struct{}, 1), release: make(chan struct{})}
	session := newTestSession(t, testPolicy(), driver)
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := session.Execute(ctx, ActionRequest{
			Operation: OperationClick,
			Click:     &ClickAction{Button: MouseButtonLeft},
		})
		resultCh <- result
		errCh <- err
	}()
	<-driver.started
	cancel()
	close(driver.release)
	if result, err := <-resultCh, <-errCh; err != nil || result.Status != ActionSucceeded {
		t.Fatalf("completed backend action mislabeled: %+v, %v", result, err)
	}
}

func TestQueuedActionCancellationDoesNotWaitForDriver(t *testing.T) {
	driver := &fakeDriver{started: make(chan struct{}, 1), release: make(chan struct{})}
	session := newTestSession(t, testPolicy(), driver)
	firstDone := make(chan struct{})
	go func() {
		_, _ = session.Execute(context.Background(), ActionRequest{
			Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		})
		close(firstDone)
	}()
	<-driver.started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	result, err := session.Execute(ctx, ActionRequest{
		Operation: OperationMove, Move: &MoveAction{DisplayID: 0},
	})
	if !errors.Is(err, context.Canceled) || result.Error == nil || result.Error.Code != ErrorCanceled {
		t.Fatalf("queued cancellation = %+v, %v", result, err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("queued cancellation took %v", elapsed)
	}
	close(driver.release)
	<-firstDone
}

func TestExclusiveSessionAndConcurrentClose(t *testing.T) {
	driver := &fakeDriver{started: make(chan struct{}, 1), release: make(chan struct{})}
	session := newTestSession(t, testPolicy(), driver)
	policy, err := preparePolicy(testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newSession(policy, &fakeDriver{}, availableCapabilities()); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("second session error = %v", err)
	}
	executeDone := make(chan struct{})
	go func() {
		_, _ = session.Execute(context.Background(), ActionRequest{
			Operation: OperationMove,
			Move:      &MoveAction{DisplayID: 0},
		})
		close(executeDone)
	}()
	<-driver.started
	closeDone := make(chan struct{})
	go func() {
		_ = session.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("Close returned before the active driver call completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(driver.release)
	<-executeDone
	<-closeDone
	if _, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationMove, Move: &MoveAction{DisplayID: 0},
	}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("post-close action error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("idempotent Close = %v", err)
	}
	replacement, err := newSession(policy, &fakeDriver{}, availableCapabilities())
	if err != nil {
		t.Fatalf("replacement session = %v", err)
	}
	_ = replacement.Close()
}
