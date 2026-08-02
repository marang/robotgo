package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	robotgo "github.com/marang/robotgo"
)

type semanticFakeDriver struct {
	*fakeDriver
	snapshot uiBackendSnapshot
	err      error
	calls    int
	handle   int
	limits   uiBackendLimits
	act      uiBackendElementAction
	actRef   string
	actCalls int
	dispatch bool
	actErr   error
	actBlock bool
	actStart func()
}

func (driver *semanticFakeDriver) ActUIElement(ctx context.Context, request uiBackendElementAction) (bool, error) {
	driver.actCalls++
	driver.actRef = string(request.Reference)
	request.Reference = nil
	driver.act = request
	if driver.actStart != nil {
		driver.actStart()
	}
	if driver.actBlock {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return driver.dispatch, driver.actErr
}

type blockingSemanticDriver struct {
	*fakeDriver
	called chan struct{}
	expire func()
}

func (driver *blockingSemanticDriver) InspectUI(ctx context.Context, _ uiBackendTarget, _ uiBackendLimits) (uiBackendSnapshot, error) {
	close(driver.called)
	driver.expire()
	<-ctx.Done()
	return uiBackendSnapshot{}, ctx.Err()
}

type controlledDeadlineContext struct {
	done chan struct{}
}

func (ctx *controlledDeadlineContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *controlledDeadlineContext) Done() <-chan struct{}       { return ctx.done }
func (ctx *controlledDeadlineContext) Value(any) any               { return nil }
func (ctx *controlledDeadlineContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (driver *semanticFakeDriver) InspectUI(_ context.Context, target uiBackendTarget, limits uiBackendLimits) (uiBackendSnapshot, error) {
	handle, err := driver.ResolveWindow(target.Target, target.Kind)
	if err != nil {
		return uiBackendSnapshot{}, err
	}
	title, err := driver.WindowTitle(handle, WindowTargetHandle)
	if err != nil {
		return uiBackendSnapshot{}, err
	}
	if title != target.ExpectedTitle {
		return uiBackendSnapshot{}, ErrStaleTarget
	}
	driver.calls++
	driver.handle = handle
	driver.limits = limits
	return driver.snapshot, driver.err
}

func semanticPolicy() Policy {
	return Policy{
		AllowedOperations: []Operation{OperationInspectUI},
		AllowedWindows: []WindowTarget{{
			Target: 42, Kind: WindowTargetProcess, ExpectedTitle: "fixture",
		}},
		AllowedUIRoles: []UIRole{UIRoleWindow, UIRoleButton, UIRolePassword},
		AllowedUIProperties: []UIProperty{
			UIPropertyRole, UIPropertyName, UIPropertyDescription, UIPropertyValue,
			UIPropertyState, UIPropertyBounds, UIPropertyFocus, UIPropertyActions,
			UIPropertyHierarchy,
		},
		MaxObservations:          2,
		MaxQueries:               2,
		MaxUIElements:            8,
		MaxUITreeDepth:           4,
		MaxUIStringBytes:         256,
		MinUIQueryIntervalMillis: 10,
		SessionTimeoutMillis:     10_000,
	}
}

func semanticActionPolicy() Policy {
	policy := semanticPolicy()
	policy.AllowedOperations = append(policy.AllowedOperations, OperationElementAct)
	policy.AllowedUIActions = []UIAction{UIActionPress, UIActionSetValue}
	policy.MaxUIActionValueBytes = 32
	policy.UIActionTimeoutMillis = 1000
	policy.MaxActions = 4
	policy.MinActionIntervalMillis = 1
	return policy
}

func semanticSnapshot() uiBackendSnapshot {
	return uiBackendSnapshot{
		Backend: "fake-accessibility",
		Nodes: []uiBackendNode{
			{
				StableID: []byte("native-window-991"), Parent: -1, Depth: 0,
				Role: UIRoleWindow, Name: "Fixture", States: []UIState{UIStateEnabled},
				Bounds: &UIBounds{X: 10, Y: 20, Width: 300, Height: 200}, Focused: true,
			},
			{
				StableID: []byte("native-button-992"), Parent: 0, Depth: 1,
				Role: UIRoleButton, Name: "Save", Description: "Store changes",
				States: []UIState{UIStateEnabled}, Actions: []UIAction{UIActionPress},
				Bounds: &UIBounds{X: 20, Y: 40, Width: 80, Height: 30},
			},
			{
				StableID: []byte("native-password-993"), Parent: 0, Depth: 1,
				Role: UIRolePassword, Name: "Password", Description: "secret-description",
				Value: "correct horse battery staple", Sensitive: true,
			},
			{
				StableID: []byte("native-hidden-994"), Parent: 0, Depth: 1,
				Role: UIRoleButton, Name: "hidden-secret", Hidden: true,
			},
			{
				StableID: []byte("native-label-995"), Parent: 0, Depth: 1,
				Role: UIRoleLabel, Name: "out-of-policy-label",
			},
		},
	}
}

func newSemanticSession(t *testing.T, input Policy, snapshot uiBackendSnapshot) (*Session, *semanticFakeDriver) {
	t.Helper()
	policy, err := preparePolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	base := &fakeDriver{resolvedHandle: 9001, windowTitle: "fixture"}
	driver := &semanticFakeDriver{fakeDriver: base, snapshot: snapshot}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{
		Available: true, Backend: "fake-accessibility", Reason: "self-owned fixture",
	}
	session, err := newSession(policy, driver, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, driver
}

func TestInspectUISanitizesAndScopesSemanticTree(t *testing.T) {
	session, driver := newSemanticSession(t, semanticPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{
		Target: 42, Kind: WindowTargetProcess,
	})
	if err != nil {
		t.Fatal(err)
	}
	if observation.SchemaVersion != UISchemaVersion || observation.ObservationID == "" ||
		observation.Backend != "fake-accessibility" || !observation.Truncated {
		t.Fatalf("observation metadata = %+v", observation)
	}
	if driver.calls != 1 || driver.handle != 9001 || driver.limits.MaxElements != 8 ||
		driver.limits.MaxDepth != 4 || driver.limits.MaxStringBytes != 256 ||
		driver.limits.MaxReferenceBytes != maxUIBackendReferenceBytes ||
		driver.limits.MaxTotalReferenceBytes != maxUIBackendReferenceTotalBytes ||
		len(driver.limits.AllowedRoles) != 3 {
		t.Fatalf("backend request = calls=%d handle=%d limits=%+v", driver.calls, driver.handle, driver.limits)
	}
	for _, role := range []UIRole{UIRoleWindow, UIRoleButton, UIRolePassword} {
		if _, ok := driver.limits.AllowedRoles[role]; !ok {
			t.Fatalf("backend request omitted allowed role %q: %+v", role, driver.limits)
		}
	}
	if !driver.limits.ReadName || !driver.limits.ReadDescription || !driver.limits.ReadValue ||
		!driver.limits.ReadStates || !driver.limits.ReadBounds || !driver.limits.ReadFocus ||
		!driver.limits.ReadActions {
		t.Fatalf("backend request omitted allowed properties: %+v", driver.limits)
	}
	for _, node := range driver.snapshot.Nodes {
		if len(node.StableID) != 0 || node.Role != "" || node.Name != "" ||
			node.Description != "" || node.Value != "" || node.Bounds != nil ||
			len(node.States) != 0 || len(node.Actions) != 0 {
			t.Fatalf("temporary backend node was not cleared: %+v", node)
		}
	}
	if len(observation.Elements) != 3 {
		t.Fatalf("elements = %+v", observation.Elements)
	}
	root, button, password := observation.Elements[0], observation.Elements[1], observation.Elements[2]
	if root.Name != "Fixture" || !root.Focused || root.Bounds == nil ||
		len(root.ChildIDs) != 2 || button.ParentID != root.ElementID ||
		password.ParentID != root.ElementID {
		t.Fatalf("semantic hierarchy = %+v", observation.Elements)
	}
	if password.Name != "" || password.Description != "" || password.Value != "" ||
		!password.Sensitive || !password.ValueRedacted {
		t.Fatalf("password projection = %+v", password)
	}
	serialized, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"correct horse", "secret-description", "hidden-secret",
		"out-of-policy-label", "native-window", "native-button", "native-password",
	} {
		if strings.Contains(string(serialized), secret) {
			t.Fatalf("semantic observation leaked %q: %s", secret, serialized)
		}
	}
}

func TestElementActionUsesObservationBoundReferenceAndExpectation(t *testing.T) {
	session, driver := newSemanticSession(t, semanticActionPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	button := observation.Elements[1]
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionPress, Expected: expectationFromUIElement(&button), Confirmed: true,
	})
	if err != nil || result.Status != ActionSucceeded || result.Backend != "fake-accessibility" {
		t.Fatalf("element action = %+v, %v", result, err)
	}
	if driver.actCalls != 1 || driver.actRef != "native-button-992" ||
		driver.act.Target.ExpectedTitle != "fixture" || driver.act.Expected.Role != UIRoleButton {
		t.Fatalf("backend action = %+v calls=%d", driver.act, driver.actCalls)
	}
}

func TestElementActionRejectsChangedExpectationBeforeDispatch(t *testing.T) {
	session, driver := newSemanticSession(t, semanticActionPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	button := observation.Elements[1]
	expected := expectationFromUIElement(&button)
	expected.Name = "Delete"
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionPress, Expected: expected, Confirmed: true,
	})
	if !hasErrorCode(err, ErrorStaleTarget) || result.Status != ActionFailed || driver.actCalls != 0 {
		t.Fatalf("changed expectation = %+v, %v calls=%d", result, err, driver.actCalls)
	}
}

func TestElementActionRejectsElementIDFromAnotherObservation(t *testing.T) {
	session, driver := newSemanticSession(t, semanticActionPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	button := observation.Elements[1]
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: "observation-999-element-2",
		Action: UIActionPress, Expected: expectationFromUIElement(&button),
	})
	if !hasErrorCode(err, ErrorInvalidInput) || result.Status != ActionFailed || driver.actCalls != 0 {
		t.Fatalf("foreign element ID = %+v, %v calls=%d", result, err, driver.actCalls)
	}
}

func TestElementActionReportsPostDispatchFailureAsUnverified(t *testing.T) {
	session, driver := newSemanticSession(t, semanticActionPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	driver.dispatch = true
	driver.actErr = errors.New("private backend detail")
	button := observation.Elements[1]
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionPress, Expected: expectationFromUIElement(&button), Confirmed: true,
	})
	if !hasErrorCode(err, ErrorBackendFailure) || result.Status != ActionUnverified {
		t.Fatalf("post-dispatch failure = %+v, %v", result, err)
	}
}

func TestElementActionRejectsUnofferedAndSensitiveActionsBeforeDispatch(t *testing.T) {
	session, driver := newSemanticSession(t, semanticActionPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	button := observation.Elements[1]
	expected := expectationFromUIElement(&button)
	expected.Actions = []UIAction{UIActionSetValue}
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionSetValue, Expected: expected,
	})
	if !hasErrorCode(err, ErrorStaleTarget) || result.Status != ActionFailed || driver.actCalls != 0 {
		t.Fatalf("unoffered action = %+v, %v calls=%d", result, err, driver.actCalls)
	}
	password := observation.Elements[2]
	password.Actions = []UIAction{UIActionSetValue}
	result, err = session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: password.ElementID,
		Action: UIActionSetValue, Expected: expectationFromUIElement(&password),
	})
	if !hasErrorCode(err, ErrorInvalidInput) || result.Status != ActionFailed || driver.actCalls != 0 {
		t.Fatalf("sensitive action = %+v, %v calls=%d", result, err, driver.actCalls)
	}
}

func TestElementActionUsesIndependentPolicyTimeout(t *testing.T) {
	policy := semanticActionPolicy()
	policy.UIActionTimeoutMillis = 5
	session, driver := newSemanticSession(t, policy, semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	driver.actBlock = true
	button := observation.Elements[1]
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionPress, Expected: expectationFromUIElement(&button),
	})
	if !hasErrorCode(err, ErrorTimedOut) || result.Status != ActionFailed || driver.actCalls != 1 {
		t.Fatalf("timed semantic action = %+v, %v calls=%d", result, err, driver.actCalls)
	}
}

func TestElementActionSessionLifetimeCancelsBackend(t *testing.T) {
	session, driver := newSemanticSession(t, semanticActionPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	deadlineCtx := &controlledDeadlineContext{done: make(chan struct{})}
	session.cancel()
	session.ctx = deadlineCtx
	session.cancel = func() {}
	driver.actBlock = true
	driver.actStart = func() { close(deadlineCtx.done) }
	button := observation.Elements[1]
	result, err := session.ActUIElement(context.Background(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionPress, Expected: expectationFromUIElement(&button),
	})
	if !hasErrorCode(err, ErrorTimedOut) || result.Status != ActionFailed || driver.actCalls != 1 {
		t.Fatalf("session-lifetime semantic action = %+v, %v calls=%d", result, err, driver.actCalls)
	}
}

func TestRetainedElementActionOwnsReferenceAndExpectationCopies(t *testing.T) {
	session, _ := newSemanticSession(t, semanticActionPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	button := observation.Elements[1]
	retained, ok := session.retainUIElementAction(observation.ObservationID, button.ElementID)
	if !ok {
		t.Fatal("failed to retain semantic action target")
	}
	defer clear(retained.reference)
	if err := session.ReleaseObservation(observation.ObservationID); err != nil {
		t.Fatal(err)
	}
	if string(retained.reference) != "native-button-992" || retained.expected.Name != "Save" ||
		!slices.Equal(retained.expected.States, []UIState{UIStateEnabled}) ||
		!slices.Equal(retained.expected.Actions, []UIAction{UIActionPress}) || retained.expected.Bounds == nil {
		t.Fatalf("retained semantic action was changed by observation release: %+v", retained)
	}
}

func TestElementActionRejectsTruncatedObservationBeforeDispatch(t *testing.T) {
	snapshot := semanticSnapshot()
	snapshot.Truncated = true
	snapshot.IdentityTruncated = true
	session, driver := newSemanticSession(t, semanticActionPolicy(), snapshot)
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil || !observation.Truncated {
		t.Fatalf("truncated observation = %+v, %v", observation, err)
	}
	button := observation.Elements[1]
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionPress, Expected: expectationFromUIElement(&button),
	})
	if !hasErrorCode(err, ErrorStaleTarget) || result.Status != ActionFailed || driver.actCalls != 0 {
		t.Fatalf("truncated action = %+v, %v calls=%d", result, err, driver.actCalls)
	}
}

func TestElementActionPolicyRequiresExactSemanticAndDurationBounds(t *testing.T) {
	for name, mutate := range map[string]func(*Policy){
		"actions": func(policy *Policy) { policy.AllowedUIActions = nil },
		"timeout": func(policy *Policy) { policy.UIActionTimeoutMillis = 0 },
		"state": func(policy *Policy) {
			policy.AllowedUIProperties = slices.DeleteFunc(policy.AllowedUIProperties, func(property UIProperty) bool {
				return property == UIPropertyState
			})
		},
		"bounds": func(policy *Policy) {
			policy.AllowedUIProperties = slices.DeleteFunc(policy.AllowedUIProperties, func(property UIProperty) bool {
				return property == UIPropertyBounds
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := semanticActionPolicy()
			mutate(&policy)
			if _, err := preparePolicy(policy); err == nil {
				t.Fatalf("unbounded semantic action policy succeeded: %+v", policy)
			}
		})
	}
}

func TestInspectUIRetainedReferencesAreZeroedOnRelease(t *testing.T) {
	session, _ := newSemanticSession(t, semanticPolicy(), semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	record, ok := session.observation(observation.ObservationID)
	if !ok || len(record.uiElements) != 3 {
		t.Fatalf("stored UI references = %+v", record.uiElements)
	}
	var retained []byte
	for _, reference := range record.uiElements {
		retained = reference
		break
	}
	if err := session.ReleaseObservation(observation.ObservationID); err != nil {
		t.Fatal(err)
	}
	for _, value := range retained {
		if value != 0 {
			t.Fatalf("released backend reference was not zeroed: %v", retained)
		}
	}
	if _, ok := session.observation(observation.ObservationID); ok {
		t.Fatal("released UI observation remains in session")
	}
}

func TestInspectUIRejectsStaleTargetBeforeBackend(t *testing.T) {
	session, driver := newSemanticSession(t, semanticPolicy(), semanticSnapshot())
	driver.windowTitle = "replacement"
	_, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if !errors.Is(err, ErrStaleTarget) || driver.calls != 0 {
		t.Fatalf("stale inspection = %v, backend calls=%d", err, driver.calls)
	}
}

func TestInspectUIPolicyIsDenyByDefaultAndFullyBounded(t *testing.T) {
	base := semanticPolicy()
	tests := []Policy{base}
	tests[0].AllowedUIRoles = nil
	for _, mutate := range []func(*Policy){
		func(policy *Policy) { policy.AllowedUIProperties = nil },
		func(policy *Policy) { policy.AllowedWindows = nil },
		func(policy *Policy) { policy.MaxQueries = 0 },
		func(policy *Policy) { policy.MaxObservations = 0 },
		func(policy *Policy) { policy.MaxUIElements = 0 },
		func(policy *Policy) { policy.MaxUITreeDepth = 0 },
		func(policy *Policy) { policy.MaxUIStringBytes = 0 },
		func(policy *Policy) { policy.MinUIQueryIntervalMillis = 0 },
		func(policy *Policy) { policy.SessionTimeoutMillis = 0 },
	} {
		candidate := semanticPolicy()
		mutate(&candidate)
		tests = append(tests, candidate)
	}
	for index, policy := range tests {
		if _, err := preparePolicy(policy); err == nil {
			t.Fatalf("unbounded UI policy %d succeeded: %+v", index, policy)
		}
	}
}

func TestInspectUIEnforcesRateBeforeDesktopIOAndAuditsFailure(t *testing.T) {
	policy, err := preparePolicy(semanticPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{
		fakeDriver: &fakeDriver{resolvedHandle: 9001, windowTitle: "fixture"},
		snapshot:   semanticSnapshot(),
	}
	sink := &recordingAuditSink{}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{Available: true, Backend: "fake-accessibility"}
	session, err := newSessionWithAudit(policy, driver, capabilities, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	now := time.Unix(100, 0)
	session.now = func() time.Time { return now }

	request := InspectUIRequest{Target: 42, Kind: WindowTargetProcess}
	if _, err := session.InspectUI(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := session.InspectUI(t.Context(), request); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("rate-limited inspection error = %v", err)
	}
	if driver.calls != 1 || session.usedQueries != 1 || session.usedObservations != 1 {
		t.Fatalf("rate limit reached desktop or consumed quota: calls=%d queries=%d observations=%d",
			driver.calls, session.usedQueries, session.usedObservations)
	}
	if len(sink.events) != 4 || sink.events[2].Kind != AuditObservationStarted ||
		sink.events[3].Kind != AuditObservationFinished || sink.events[3].ErrorCode != ErrorPolicyDenied {
		t.Fatalf("rate-limit audit events = %+v", sink.events)
	}
}

func TestInspectUISessionLifetimeCancelsBackendAndAuditsTimeout(t *testing.T) {
	policy, err := preparePolicy(semanticPolicy())
	if err != nil {
		t.Fatal(err)
	}
	deadlineCtx := &controlledDeadlineContext{done: make(chan struct{})}
	driver := &blockingSemanticDriver{
		fakeDriver: &fakeDriver{resolvedHandle: 9001, windowTitle: "fixture"},
		called:     make(chan struct{}),
		expire:     func() { close(deadlineCtx.done) },
	}
	sink := &recordingAuditSink{}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{Available: true, Backend: "fake-accessibility"}
	session, err := newSessionWithAudit(policy, driver, capabilities, sink)
	if err != nil {
		t.Fatal(err)
	}
	session.cancel()
	session.ctx = deadlineCtx
	session.cancel = func() {}
	t.Cleanup(func() { _ = session.Close() })

	_, inspectErr := session.InspectUI(context.Background(), InspectUIRequest{
		Target: 42, Kind: WindowTargetProcess,
	})
	var actionErr *ActionError
	if !errors.As(inspectErr, &actionErr) || actionErr.Code != ErrorTimedOut {
		t.Fatalf("session-lifetime inspection error = %v", inspectErr)
	}
	select {
	case <-driver.called:
	default:
		t.Fatal("accessibility backend was not exercised")
	}
	if len(sink.events) != 2 || sink.events[1].Kind != AuditObservationFinished ||
		sink.events[1].ErrorCode != ErrorTimedOut {
		t.Fatalf("session-timeout audit events = %+v", sink.events)
	}
}

func TestInspectUIBackendFailureAuditContainsNoPayload(t *testing.T) {
	policy, err := preparePolicy(semanticPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{
		fakeDriver: &fakeDriver{resolvedHandle: 9001, windowTitle: "fixture"},
		snapshot:   semanticSnapshot(),
		err:        errors.New("native-secret-accessibility-path"),
	}
	sink := &recordingAuditSink{}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{Available: true, Backend: "fake-accessibility"}
	session, err := newSessionWithAudit(policy, driver, capabilities, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if _, err := session.InspectUI(t.Context(), InspectUIRequest{
		Target: 42, Kind: WindowTargetProcess,
	}); err == nil {
		t.Fatal("backend failure inspection succeeded")
	}
	serialized, err := json.Marshal(sink.events)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 || sink.events[1].ErrorCode != ErrorBackendFailure ||
		strings.Contains(string(serialized), "native-secret") || strings.Contains(string(serialized), "fixture") {
		t.Fatalf("backend-failure audit leaked payload or was incomplete: %s", serialized)
	}
	for _, node := range driver.snapshot.Nodes {
		if len(node.StableID) != 0 || node.Name != "" || node.Description != "" || node.Value != "" {
			t.Fatalf("partial failed backend snapshot was not cleared: %+v", node)
		}
	}
}

func TestInspectUIRejectsEmptyCapabilityBackendBeforeDesktopIO(t *testing.T) {
	policy, err := preparePolicy(semanticPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{
		fakeDriver: &fakeDriver{resolvedHandle: 9001, windowTitle: "fixture"},
		snapshot:   semanticSnapshot(),
	}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{Available: true}
	session, err := newSession(policy, driver, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	_, inspectErr := session.InspectUI(t.Context(), InspectUIRequest{
		Target: 42, Kind: WindowTargetProcess,
	})
	if inspectErr == nil || driver.calls != 0 || driver.callCount() != 0 {
		t.Fatalf("empty-backend capability error = %v, UI calls=%d, desktop calls=%d",
			inspectErr, driver.calls, driver.callCount())
	}
}

func TestSanitizeUISnapshotRejectsMalformedStructureAndEnums(t *testing.T) {
	policy, err := preparePolicy(semanticPolicy())
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*uiBackendSnapshot){
		"cycle-or-forward-parent": func(snapshot *uiBackendSnapshot) { snapshot.Nodes[1].Parent = 1 },
		"wrong-depth":             func(snapshot *uiBackendSnapshot) { snapshot.Nodes[1].Depth = 2 },
		"unknown-state": func(snapshot *uiBackendSnapshot) {
			snapshot.Nodes[0].States = []UIState{"backend-secret-state"}
		},
		"unknown-action": func(snapshot *uiBackendSnapshot) {
			snapshot.Nodes[1].Actions = []UIAction{"backend-secret-action"}
		},
		"duplicate-state": func(snapshot *uiBackendSnapshot) {
			snapshot.Nodes[0].States = []UIState{UIStateEnabled, UIStateEnabled}
		},
		"invalid-bounds": func(snapshot *uiBackendSnapshot) {
			snapshot.Nodes[1].Bounds = &UIBounds{Width: -1, Height: 2}
		},
		"oversized-reference": func(snapshot *uiBackendSnapshot) {
			snapshot.Nodes[1].StableID = make([]byte, maxUIBackendReferenceBytes+1)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := semanticSnapshot()
			mutate(&snapshot)
			elements, references, _, _, err := sanitizeUIBackendSnapshot("observation-1", snapshot, policy)
			closeUIReferences(references)
			if err == nil || len(elements) != 0 {
				t.Fatalf("malformed snapshot result = %+v, %v", elements, err)
			}
		})
	}
}

func TestInspectUIRejectsDuplicateBackendIdentityWithoutRetention(t *testing.T) {
	snapshot := semanticSnapshot()
	snapshot.Nodes[1].StableID = append([]byte(nil), snapshot.Nodes[0].StableID...)
	session, _ := newSemanticSession(t, semanticPolicy(), snapshot)
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	var actionErr *ActionError
	if !errors.As(err, &actionErr) || actionErr.Code != ErrorBackendFailure ||
		observation.ObservationID != "" {
		t.Fatalf("invalid backend tree = %+v, %v", observation, err)
	}
	if len(session.observations) != 0 {
		t.Fatalf("invalid backend tree retained observations: %+v", session.observations)
	}
}

func TestSanitizeUISnapshotEnforcesNodeAndUTF8ByteLimits(t *testing.T) {
	input := semanticPolicy()
	input.MaxUIElements = 2
	input.MaxUIStringBytes = 4
	policy, err := preparePolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := semanticSnapshot()
	snapshot.Nodes[0].Name = "€€"
	elements, references, truncated, _, err := sanitizeUIBackendSnapshot("observation-1", snapshot, policy)
	t.Cleanup(func() { closeUIReferences(references) })
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 2 || !truncated || elements[0].Name != "€" ||
		!utf8.ValidString(elements[0].Name) {
		t.Fatalf("bounded semantic tree = %+v, truncated=%v", elements, truncated)
	}
	if elements[1].Name != "S" || elements[1].Description != "" {
		t.Fatalf("aggregate string budget was exceeded: %+v", elements[1])
	}
}
