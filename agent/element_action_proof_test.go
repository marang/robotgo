package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
)

type cancelOnElementActionIntentSink struct {
	events []AuditEvent
	cancel context.CancelFunc
}

func (sink *cancelOnElementActionIntentSink) Record(_ context.Context, event AuditEvent) error {
	sink.events = append(sink.events, event)
	if event.Kind == AuditActionStarted && event.Operation == OperationElementAct && sink.cancel != nil {
		sink.cancel()
	}
	return nil
}

func semanticVerificationPolicy() Policy {
	policy := semanticActionPolicy()
	policy.MaxQueries = 8
	policy.MaxObservations = 8
	policy.UIVerificationAttempts = 3
	policy.UIVerificationIntervalMillis = 0
	policy.UIVerificationTimeoutMillis = 250
	return policy
}

func semanticConditionRequest(observation UIObservation) ElementActionRequest {
	button := observation.Elements[1]
	return ElementActionRequest{
		ObservationID: observation.ObservationID,
		ElementID:     button.ElementID,
		Action:        UIActionPress,
		Expected:      expectationFromUIElement(&button),
		Postcondition: &UIElementCondition{
			Kind: UIElementConditionStatePresent, State: UIStateChecked,
		},
		Confirmed: true,
	}
}

func inspectSemanticConditionFixture(t *testing.T, policy Policy) (*Session, *semanticFakeDriver, ElementActionRequest) {
	t.Helper()
	session, driver := newSemanticSession(t, policy, semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{
		Target: 42, Kind: WindowTargetProcess,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session, driver, semanticConditionRequest(observation)
}

func TestElementActionProofOutcomeMatrix(t *testing.T) {
	tests := []struct {
		name             string
		configure        func(*semanticFakeDriver)
		wantStatus       ActionStatus
		wantProof        ActionProofStatus
		wantExecution    ActionExecutionStatus
		wantVerification ActionVerificationStatus
		wantCode         ErrorCode
		wantActCalls     int
		wantCheckCalls   int
		wantPostAttempts uint32
		wantFinalGate    bool
	}{
		{
			name: "already satisfied by precheck",
			configure: func(driver *semanticFakeDriver) {
				driver.checkResults = []uiBackendElementConditionResult{{Satisfied: true, CleanupComplete: true}}
			},
			wantStatus: ActionSucceeded, wantProof: ActionProofVerified,
			wantExecution:    ActionExecutionSkippedAlreadySatisfied,
			wantVerification: ActionVerificationMatched, wantCheckCalls: 1,
		},
		{
			name: "newly satisfied in final gate",
			configure: func(driver *semanticFakeDriver) {
				driver.checkResults = []uiBackendElementConditionResult{{CleanupComplete: true}}
				driver.dispatch = false
				driver.alreadySatisfied = true
			},
			wantStatus: ActionSucceeded, wantProof: ActionProofVerified,
			wantExecution:    ActionExecutionSkippedAlreadySatisfied,
			wantVerification: ActionVerificationMatched, wantActCalls: 1, wantCheckCalls: 1,
			wantFinalGate: true,
		},
		{
			name: "dispatch and first poll matches",
			configure: func(driver *semanticFakeDriver) {
				driver.checkResults = []uiBackendElementConditionResult{
					{CleanupComplete: true},
					{Satisfied: true, CleanupComplete: true},
				}
			},
			wantStatus: ActionSucceeded, wantProof: ActionProofVerified,
			wantExecution:    ActionExecutionDispatched,
			wantVerification: ActionVerificationMatched, wantActCalls: 1, wantCheckCalls: 2,
			wantPostAttempts: 1, wantFinalGate: true,
		},
		{
			name: "dispatch exhausts exact poll bound",
			configure: func(driver *semanticFakeDriver) {
				driver.checkResults = []uiBackendElementConditionResult{
					{CleanupComplete: true}, {CleanupComplete: true},
					{CleanupComplete: true}, {CleanupComplete: true},
				}
			},
			wantStatus: ActionUnverified, wantProof: ActionProofUnverifiedAfterDispatch,
			wantExecution:    ActionExecutionDispatched,
			wantVerification: ActionVerificationNotMatched, wantCode: ErrorVerification,
			wantActCalls: 1, wantCheckCalls: 4, wantPostAttempts: 3,
			wantFinalGate: true,
		},
		{
			name: "precheck backend failure",
			configure: func(driver *semanticFakeDriver) {
				driver.checkResults = []uiBackendElementConditionResult{{CleanupComplete: true}}
				driver.checkErrors = []error{errors.New("private precheck detail")}
			},
			wantStatus: ActionFailed, wantProof: ActionProofFailedBeforeDispatch,
			wantExecution:    ActionExecutionNotDispatched,
			wantVerification: ActionVerificationFailed, wantCode: ErrorBackendFailure,
			wantCheckCalls: 1,
		},
		{
			name: "native final gate fails before dispatch",
			configure: func(driver *semanticFakeDriver) {
				driver.checkResults = []uiBackendElementConditionResult{{CleanupComplete: true}}
				driver.dispatch = false
				driver.actErr = errors.New("private final gate detail")
			},
			wantStatus: ActionFailed, wantProof: ActionProofFailedBeforeDispatch,
			wantExecution:    ActionExecutionNotDispatched,
			wantVerification: ActionVerificationNotMatched, wantCode: ErrorBackendFailure,
			wantActCalls: 1, wantCheckCalls: 1,
		},
		{
			name: "post-dispatch error cannot be upgraded by matching poll",
			configure: func(driver *semanticFakeDriver) {
				driver.checkResults = []uiBackendElementConditionResult{
					{CleanupComplete: true},
					{Satisfied: true, CleanupComplete: true},
				}
				driver.actErr = errors.New("private execution detail")
			},
			wantStatus: ActionUnverified, wantProof: ActionProofUnverifiedAfterDispatch,
			wantExecution:    ActionExecutionDispatched,
			wantVerification: ActionVerificationMatched, wantCode: ErrorBackendFailure,
			wantActCalls: 1, wantCheckCalls: 2, wantPostAttempts: 1, wantFinalGate: true,
		},
		{
			name: "backend cleanup uncertainty dominates",
			configure: func(driver *semanticFakeDriver) {
				driver.checkResults = []uiBackendElementConditionResult{{CleanupComplete: true}}
				driver.actErr = errors.New("private backend detail before cleanup failure")
				driver.actCleanup = false
			},
			wantStatus: ActionUnverified, wantProof: ActionProofCleanupPending,
			wantExecution:    ActionExecutionDispatched,
			wantVerification: ActionVerificationFailed, wantCode: ErrorCleanupFailed,
			wantActCalls: 1, wantCheckCalls: 1, wantFinalGate: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, driver, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
			test.configure(driver)
			usedBefore := session.used
			queriesBefore, observationsBefore := session.usedQueries, session.usedObservations
			result, err := session.ActUIElement(t.Context(), request)
			if result.Status != test.wantStatus || result.Proof == nil || result.Proof.Status != test.wantProof ||
				result.Proof.Execution.Status != test.wantExecution || result.Proof.Verification == nil ||
				result.Proof.Verification.Status != test.wantVerification ||
				result.Proof.Verification.PostconditionAttempts != test.wantPostAttempts ||
				result.Proof.Verification.FinalGateChecked != test.wantFinalGate ||
				driver.actCalls != test.wantActCalls || driver.checkCalls != test.wantCheckCalls {
				t.Fatalf("result = %+v, err=%v, act=%d check=%d", result, err, driver.actCalls, driver.checkCalls)
			}
			if test.wantCode == "" {
				if err != nil || result.Error != nil {
					t.Fatalf("unexpected error = %+v, %v", result.Error, err)
				}
			} else if !hasErrorCode(err, test.wantCode) || result.Error == nil || result.Proof.ErrorCode != test.wantCode {
				t.Fatalf("error = %+v, %v", result.Error, err)
			}
			wantReleased := test.wantProof != ActionProofCleanupPending
			if result.Proof.Cleanup.TransientResourcesReleased != wantReleased {
				t.Fatalf("cleanup proof = %+v", result.Proof.Cleanup)
			}
			wantUsed := usedBefore
			if test.wantExecution == ActionExecutionDispatched {
				wantUsed++
			}
			if session.used != wantUsed {
				t.Fatalf("used actions = %d, want %d", session.used, wantUsed)
			}
			if session.usedQueries != queriesBefore+uint64(test.wantCheckCalls) ||
				session.usedObservations != observationsBefore+uint64(test.wantCheckCalls) {
				t.Fatalf("semantic reads = queries %d observations %d, want +%d",
					session.usedQueries, session.usedObservations, test.wantCheckCalls)
			}
		})
	}
}

func TestElementActionWithoutPostconditionPreservesSucceededStatusWithUnverifiedProof(t *testing.T) {
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
	if err != nil || result.Status != ActionSucceeded || result.Proof == nil ||
		result.Proof.Status != ActionProofUnverifiedAfterDispatch ||
		result.Proof.Execution.Status != ActionExecutionDispatched ||
		result.Proof.Verification == nil || result.Proof.Verification.Status != ActionVerificationNotRequested ||
		driver.actCalls != 1 || driver.checkCalls != 0 || session.used != 1 {
		t.Fatalf("condition-free action = %+v, %v calls=%d/%d used=%d", result, err, driver.actCalls, driver.checkCalls, session.used)
	}
}

func TestElementActionCleanupPendingTaintsSessionAgainstRetries(t *testing.T) {
	session, driver, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
	driver.checkResults = []uiBackendElementConditionResult{{CleanupComplete: true}}
	driver.actCleanup = false
	result, err := session.ActUIElement(t.Context(), request)
	if !hasErrorCode(err, ErrorCleanupFailed) || result.Proof.Status != ActionProofCleanupPending ||
		result.Proof.Cleanup.TransientResourcesReleased {
		t.Fatalf("cleanup-pending action = %+v, %v", result, err)
	}
	actCalls, checkCalls := driver.actCalls, driver.checkCalls
	retry, retryErr := session.ActUIElement(t.Context(), request)
	if !hasErrorCode(retryErr, ErrorSessionClosed) || retry.Proof == nil ||
		retry.Proof.Status != ActionProofRejectedBeforeDispatch ||
		driver.actCalls != actCalls || driver.checkCalls != checkCalls {
		t.Fatalf("tainted retry = %+v, %v calls=%d/%d", retry, retryErr, driver.actCalls, driver.checkCalls)
	}
}

func TestElementActionProofExistsWhenSessionIsAlreadyClosed(t *testing.T) {
	session, _, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := session.ActUIElement(t.Context(), request)
	if !hasErrorCode(err, ErrorSessionClosed) || result.Proof == nil ||
		result.Proof.SchemaVersion != ActionProofSchemaVersion ||
		result.Proof.TransactionID != result.ActionID ||
		result.Proof.Status != ActionProofRejectedBeforeDispatch ||
		!result.Proof.Cleanup.TransientResourcesReleased {
		t.Fatalf("closed-session proof = %+v, %v", result, err)
	}
}

func TestElementActionAlreadySatisfiedIgnoresMutationBudgetButNotAuthorization(t *testing.T) {
	policy := semanticVerificationPolicy()
	policy.ConfirmOperations = []Operation{OperationElementAct}
	session, driver, request := inspectSemanticConditionFixture(t, policy)
	driver.checkResults = []uiBackendElementConditionResult{{Satisfied: true, CleanupComplete: true}}
	session.used = session.policy.MaxActions
	session.lastAction = session.now()

	request.Confirmed = false
	result, err := session.ActUIElement(t.Context(), request)
	if !hasErrorCode(err, ErrorPolicyDenied) || result.Proof.Authorization == nil ||
		!result.Proof.Authorization.PolicyAllowed || driver.checkCalls != 0 || driver.actCalls != 0 {
		t.Fatalf("unconfirmed retry = %+v, %v calls=%d/%d", result, err, driver.checkCalls, driver.actCalls)
	}
	request.Confirmed = true
	result, err = session.ActUIElement(t.Context(), request)
	if err != nil || result.Status != ActionSucceeded || result.Proof.Status != ActionProofVerified ||
		result.Proof.Execution.Status != ActionExecutionSkippedAlreadySatisfied ||
		session.used != session.policy.MaxActions || driver.checkCalls != 1 || driver.actCalls != 0 {
		t.Fatalf("already-satisfied exhausted retry = %+v, %v used=%d calls=%d/%d", result, err, session.used, driver.checkCalls, driver.actCalls)
	}
}

func TestElementActionPostconditionRejectsInsufficientWorstCaseReadCapacity(t *testing.T) {
	session, driver, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
	required := uint64(session.policy.UIVerificationAttempts) + 1
	session.usedQueries = session.policy.MaxQueries - required + 1
	session.usedObservations = session.policy.MaxObservations - required + 1
	result, err := session.ActUIElement(t.Context(), request)
	if !hasErrorCode(err, ErrorPolicyDenied) || result.Proof.Status != ActionProofRejectedBeforeDispatch ||
		driver.checkCalls != 0 || driver.actCalls != 0 {
		t.Fatalf("read-capacity rejection = %+v, %v calls=%d/%d", result, err, driver.checkCalls, driver.actCalls)
	}
}

func TestElementActionPostconditionEnforcesSemanticReadRate(t *testing.T) {
	session, driver, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
	session.lastUIQuery = session.now().Add(time.Second)
	result, err := session.ActUIElement(t.Context(), request)
	if !hasErrorCode(err, ErrorPolicyDenied) || result.Proof.Status != ActionProofFailedBeforeDispatch ||
		result.Proof.Verification.PrecheckAttempts != 0 || driver.checkCalls != 0 || driver.actCalls != 0 {
		t.Fatalf("read-rate rejection = %+v, %v calls=%d/%d", result, err, driver.checkCalls, driver.actCalls)
	}
}

func TestElementActionConditionValidationAndPropertyPolicyRejectBeforeDesktopIO(t *testing.T) {
	tests := []struct {
		name      string
		action    UIAction
		condition *UIElementCondition
	}{
		{"state missing operand", UIActionPress, &UIElementCondition{Kind: UIElementConditionStatePresent}},
		{"focus with state operand", UIActionPress, &UIElementCondition{Kind: UIElementConditionFocused, State: UIStateEnabled}},
		{"value with wrong action", UIActionPress, &UIElementCondition{Kind: UIElementConditionValueEqualsActionValue}},
		{"unknown kind", UIActionPress, &UIElementCondition{Kind: "private-invalid-kind"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, driver, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
			request.Action = test.action
			request.Postcondition = test.condition
			result, err := session.ActUIElement(t.Context(), request)
			if !hasErrorCode(err, ErrorInvalidInput) || result.Proof.Status != ActionProofRejectedBeforeDispatch ||
				driver.checkCalls != 0 || driver.actCalls != 0 {
				t.Fatalf("condition rejection = %+v, %v calls=%d/%d", result, err, driver.checkCalls, driver.actCalls)
			}
		})
	}

	policy := semanticVerificationPolicy()
	filtered := policy.AllowedUIProperties[:0]
	for _, property := range policy.AllowedUIProperties {
		if property != UIPropertyFocus {
			filtered = append(filtered, property)
		}
	}
	policy.AllowedUIProperties = filtered
	session, driver, request := inspectSemanticConditionFixture(t, policy)
	request.Postcondition = &UIElementCondition{Kind: UIElementConditionFocused}
	result, err := session.ActUIElement(t.Context(), request)
	if !hasErrorCode(err, ErrorPolicyDenied) || result.Proof.Authorization == nil ||
		result.Proof.Authorization.PolicyAllowed || driver.checkCalls != 0 || driver.actCalls != 0 {
		t.Fatalf("property rejection = %+v, %v calls=%d/%d", result, err, driver.checkCalls, driver.actCalls)
	}
}

func TestElementActionUIVerificationPolicyBoundsAreIndependent(t *testing.T) {
	for name, mutate := range map[string]func(*Policy){
		"attempt hard bound": func(policy *Policy) {
			policy.UIVerificationAttempts = maxAgentUIVerificationAttempts + 1
			policy.UIVerificationTimeoutMillis = 1
		},
		"timeout pair": func(policy *Policy) { policy.UIVerificationAttempts = 1 },
		"timeout hard bound": func(policy *Policy) {
			policy.UIVerificationAttempts = 1
			policy.UIVerificationTimeoutMillis = maxAgentUIVerificationTimeoutMS + 1
		},
		"read capacity": func(policy *Policy) {
			policy.UIVerificationAttempts = 3
			policy.UIVerificationTimeoutMillis = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := semanticActionPolicy()
			mutate(&policy)
			if _, err := preparePolicy(policy); err == nil {
				t.Fatalf("invalid UI verification policy succeeded: %+v", policy)
			}
		})
	}

	policy := semanticVerificationPolicy()
	policy.VerificationAttempts = 0
	policy.VerificationTimeoutMillis = 0
	if _, err := preparePolicy(policy); err != nil {
		t.Fatalf("semantic verification incorrectly depends on capture verification: %v", err)
	}
}

func TestElementActionCatalogV10PublishesDefensiveProofAndConditionBounds(t *testing.T) {
	policy := semanticVerificationPolicy()
	session, _ := newSemanticSession(t, policy, semanticSnapshot())
	catalog := session.Catalog()
	var capability OperationCapability
	for _, candidate := range catalog.Operations {
		if candidate.Operation == OperationElementAct {
			capability = candidate
			break
		}
	}
	if catalog.SchemaVersion != "10" || capability.ActionProofVersion != ActionProofSchemaVersion ||
		capability.UIVerificationAttempts != policy.UIVerificationAttempts ||
		capability.UIVerificationIntervalMillis != policy.UIVerificationIntervalMillis ||
		capability.UIVerificationTimeoutMillis != policy.UIVerificationTimeoutMillis ||
		len(capability.UIConditionKinds) != len(allUIElementConditionKinds) {
		t.Fatalf("semantic action catalog capability = %+v", capability)
	}
	capability.UIConditionKinds[0] = "mutated"
	for _, candidate := range session.Catalog().Operations {
		if candidate.Operation == OperationElementAct && candidate.UIConditionKinds[0] != UIElementConditionStatePresent {
			t.Fatalf("catalog condition-kind mutation leaked: %+v", candidate.UIConditionKinds)
		}
	}
}

func TestElementActionIntentAuditFailurePreventsSemanticReadAndMutation(t *testing.T) {
	policy, err := preparePolicy(semanticVerificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{
		fakeDriver: &fakeDriver{resolvedHandle: 9001, windowTitle: "fixture"},
		snapshot:   semanticSnapshot(), dispatch: true, actCleanup: true,
		checkResults: []uiBackendElementConditionResult{{Satisfied: true, CleanupComplete: true}},
	}
	sink := &recordingAuditSink{failAt: 3}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{Available: true, Backend: "fake-accessibility"}
	session, err := newSessionWithAudit(policy, driver, capabilities, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.ActUIElement(t.Context(), semanticConditionRequest(observation))
	if !hasErrorCode(err, ErrorAuditDelivery) || result.Proof.Status != ActionProofRejectedBeforeDispatch ||
		driver.checkCalls != 0 || driver.actCalls != 0 || len(sink.events) != 3 {
		t.Fatalf("intent audit rejection = %+v, %v events=%+v calls=%d/%d", result, err, sink.events, driver.checkCalls, driver.actCalls)
	}
}

func TestElementActionCancellationStillEmitsBoundedTerminalAudit(t *testing.T) {
	policy, err := preparePolicy(semanticVerificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{
		fakeDriver: &fakeDriver{resolvedHandle: 9001, windowTitle: "fixture"},
		snapshot:   semanticSnapshot(), dispatch: true, actCleanup: true,
		checkResults: []uiBackendElementConditionResult{{CleanupComplete: true}},
		checkBlockAt: 2,
	}
	sink := &recordingAuditSink{}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{Available: true, Backend: "fake-accessibility"}
	session, err := newSessionWithAudit(policy, driver, capabilities, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	driver.checkStart = cancel
	result, err := session.ActUIElement(ctx, semanticConditionRequest(observation))
	if !hasErrorCode(err, ErrorCanceled) || result.Status != ActionUnverified ||
		result.Proof.Status != ActionProofUnverifiedAfterDispatch || driver.actCalls != 1 || driver.checkCalls != 2 {
		t.Fatalf("canceled verification = %+v, %v calls=%d/%d", result, err, driver.actCalls, driver.checkCalls)
	}
	if len(sink.events) != 5 || sink.events[3].Kind != AuditVerificationFinished ||
		sink.events[4].Kind != AuditActionFinished || sink.events[4].ErrorCode != ErrorCanceled {
		t.Fatalf("terminal audit = %+v", sink.events)
	}
	for _, event := range sink.events[3:] {
		if event.SchemaVersion != "3" || event.ActionProofStatus != ActionProofUnverifiedAfterDispatch ||
			event.ActionExecutionStatus != ActionExecutionDispatched ||
			event.UIConditionKind != UIElementConditionStatePresent ||
			event.UIConditionPhase != UIConditionPhasePostDispatch || event.UIPrecheckAttempts != 1 ||
			!event.UIFinalGateChecked || event.UIPostconditionAttempts != 1 {
			t.Fatalf("semantic terminal audit evidence = %+v", event)
		}
	}
}

func TestElementActionCancellationAfterIntentPreventsConditionFreeNativeCall(t *testing.T) {
	policy, err := preparePolicy(semanticActionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{
		fakeDriver: &fakeDriver{resolvedHandle: 9001, windowTitle: "fixture"},
		snapshot:   semanticSnapshot(), dispatch: true, actCleanup: true,
	}
	sink := &cancelOnElementActionIntentSink{}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{Available: true, Backend: "fake-accessibility"}
	session, err := newSessionWithAudit(policy, driver, capabilities, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	button := observation.Elements[1]
	ctx, cancel := context.WithCancel(context.Background())
	sink.cancel = cancel
	result, err := session.ActUIElement(ctx, ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionPress, Expected: expectationFromUIElement(&button), Confirmed: true,
	})
	if !hasErrorCode(err, ErrorCanceled) || result.Proof.Status != ActionProofFailedBeforeDispatch ||
		driver.actCalls != 0 || driver.checkCalls != 0 || len(sink.events) != 5 {
		t.Fatalf("post-intent cancellation = %+v, %v calls=%d/%d events=%+v", result, err, driver.actCalls, driver.checkCalls, sink.events)
	}
}

func TestElementActionCancellationBetweenZeroIntervalPollsPreventsExtraProbe(t *testing.T) {
	session, driver, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
	ctx, cancel := context.WithCancel(context.Background())
	driver.checkResults = []uiBackendElementConditionResult{
		{CleanupComplete: true}, {CleanupComplete: true}, {Satisfied: true, CleanupComplete: true},
	}
	driver.checkFinish = func(call int) {
		if call == 2 {
			cancel()
		}
	}
	clock := time.Now()
	session.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	queriesBefore, observationsBefore := session.usedQueries, session.usedObservations
	result, err := session.ActUIElement(ctx, request)
	if !hasErrorCode(err, ErrorCanceled) || result.Status != ActionUnverified ||
		result.Proof.Status != ActionProofUnverifiedAfterDispatch ||
		result.Proof.Verification.PostconditionAttempts != 1 ||
		driver.actCalls != 1 || driver.checkCalls != 2 ||
		session.usedQueries != queriesBefore+2 || session.usedObservations != observationsBefore+2 {
		t.Fatalf("between-poll cancellation = %+v, %v calls=%d/%d reads=%d/%d", result, err, driver.actCalls, driver.checkCalls, session.usedQueries-queriesBefore, session.usedObservations-observationsBefore)
	}
}

func TestElementActionProofAuditAndCatalogSerializationArePayloadFree(t *testing.T) {
	const (
		privateValue  = "private-action-value-sentinel"
		privateName   = "private-element-name-sentinel"
		privateTitle  = "private-window-title-sentinel"
		privateNative = "private-native-reference-sentinel"
	)
	policy := semanticVerificationPolicy()
	policy.AllowedUIRoles = append(policy.AllowedUIRoles, UIRoleTextBox)
	policy.AllowedWindows[0].ExpectedTitle = privateTitle
	prepared, err := preparePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := semanticSnapshot()
	snapshot.Nodes[1].Role = UIRoleTextBox
	snapshot.Nodes[1].Name = privateName
	snapshot.Nodes[1].StableID = []byte(privateNative)
	snapshot.Nodes[1].Actions = []UIAction{UIActionSetValue}
	driver := &semanticFakeDriver{
		fakeDriver: &fakeDriver{resolvedHandle: 9001, windowTitle: privateTitle},
		snapshot:   snapshot, dispatch: true, actCleanup: true,
		checkResults: []uiBackendElementConditionResult{
			{CleanupComplete: true}, {Satisfied: true, CleanupComplete: true},
		},
	}
	sink := &recordingAuditSink{}
	capabilities := availableCapabilities()
	capabilities.Accessibility = robotgo.FeatureCapability{Available: true, Backend: "fake-accessibility"}
	session, err := newSessionWithAudit(prepared, driver, capabilities, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	element := observation.Elements[1]
	result, actionErr := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: element.ElementID,
		Action: UIActionSetValue, Expected: expectationFromUIElement(&element),
		Value: privateValue, Confirmed: true,
		Postcondition: &UIElementCondition{Kind: UIElementConditionValueEqualsActionValue},
	})
	if actionErr != nil || result.Status != ActionSucceeded {
		t.Fatalf("set-value action = %+v, %v", result, actionErr)
	}
	serialized, err := json.Marshal(struct {
		Result  ActionResult     `json:"result"`
		Events  []AuditEvent     `json:"events"`
		Catalog OperationCatalog `json:"catalog"`
		Error   error            `json:"error,omitempty"`
	}{result, sink.events, session.Catalog(), actionErr})
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{privateValue, privateName, privateTitle, privateNative} {
		if strings.Contains(string(serialized), private) {
			t.Fatalf("serialized semantic proof leaked %q: %s", private, serialized)
		}
	}
	if len(driver.actValue) == 0 {
		t.Fatal("test did not observe the borrowed action-value buffer")
	}
	for _, value := range driver.actValue {
		if value != 0 {
			t.Fatalf("action-value copy was not cleared: %v", driver.actValue)
		}
	}
	for _, valueCopy := range driver.checkValues {
		for _, value := range valueCopy {
			if value != 0 {
				t.Fatalf("verification value copy was not cleared: %v", valueCopy)
			}
		}
	}
	record, ok := session.observation(observation.ObservationID)
	if !ok || len(record.uiElements[element.ElementID]) == 0 {
		t.Fatal("semantic action destroyed the still-live source observation reference")
	}
}

func TestElementActionReleaseRaceUsesOwnedTransientReference(t *testing.T) {
	session, driver, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
	started := make(chan struct{})
	resume := make(chan struct{})
	driver.checkResults = []uiBackendElementConditionResult{{Satisfied: true, CleanupComplete: true}}
	driver.checkBlockAt = 1
	driver.checkStart = func() { close(started) }
	driver.checkWait = resume
	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := session.ActUIElement(context.Background(), request)
		resultCh <- result
		errCh <- err
	}()
	<-started
	if err := session.ReleaseObservation(request.ObservationID); err != nil {
		t.Fatal(err)
	}
	close(resume)
	result, err := <-resultCh, <-errCh
	if err != nil || result.Status != ActionSucceeded || result.Proof.Status != ActionProofVerified ||
		driver.actCalls != 0 || len(driver.checkRefs) != 1 || driver.checkRefs[0] != "native-button-992" {
		t.Fatalf("release race = %+v, %v refs=%v calls=%d", result, err, driver.checkRefs, driver.actCalls)
	}
}
