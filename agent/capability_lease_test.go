package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func capabilityLeasePolicy() Policy {
	policy := semanticActionPolicy()
	policy.AllowedOperations = append(policy.AllowedOperations, OperationResolveUI)
	policy.AllowedTargetModes = []TargetResolutionMode{
		TargetResolutionModeStrict, TargetResolutionModeAdaptive, TargetResolutionModeReview,
	}
	policy.RequireCapabilityLease = true
	policy.MaxCapabilityLeases = 32
	policy.MaxCapabilityLeaseMillis = 10_000
	policy.AdaptiveTargetThreshold = 75
	return policy
}

func TestReviewPolicyDoesNotRequireExecutableLeaseCapacity(t *testing.T) {
	policy := semanticResolverPolicy()
	policy.AllowedTargetModes = []TargetResolutionMode{TargetResolutionModeStrict, TargetResolutionModeReview}
	policy.AdaptiveTargetThreshold = 75
	if _, err := preparePolicy(policy); err != nil {
		t.Fatalf("review-only policy: %v", err)
	}
}

func TestCapabilityLeasePolicyAndCatalogExposeBoundedContract(t *testing.T) {
	for name, mutate := range map[string]func(*Policy){
		"missing-count":     func(policy *Policy) { policy.MaxCapabilityLeases = 0 },
		"missing-lifetime":  func(policy *Policy) { policy.MaxCapabilityLeaseMillis = 0 },
		"invalid-threshold": func(policy *Policy) { policy.AdaptiveTargetThreshold = 101 },
		"duplicate-mode": func(policy *Policy) {
			policy.AllowedTargetModes = append(policy.AllowedTargetModes, TargetResolutionModeStrict)
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := capabilityLeasePolicy()
			mutate(&policy)
			if _, err := preparePolicy(policy); err == nil {
				t.Fatalf("invalid policy accepted: %+v", policy)
			}
		})
	}
	t.Run("lease-action-without-resolver", func(t *testing.T) {
		policy := semanticActionPolicy()
		policy.RequireCapabilityLease = true
		policy.MaxCapabilityLeases = 1
		policy.MaxCapabilityLeaseMillis = 1000
		if _, err := preparePolicy(policy); err == nil {
			t.Fatalf("unusable lease action policy accepted: %+v", policy)
		}
	})
	session, _ := newSemanticSession(t, capabilityLeasePolicy(), resolverSnapshot())
	for _, capability := range session.Catalog().Operations {
		if capability.Operation != OperationResolveUI {
			continue
		}
		if capability.CapabilityLeaseVersion != CapabilityLeaseSchemaVersion || !capability.CapabilityLeaseRequired ||
			capability.MaxCapabilityLeases != 32 || capability.MaxCapabilityLeaseMillis != 10_000 ||
			capability.AdaptiveTargetThreshold != 75 || !slices.Equal(capability.TargetResolutionModes, allTargetResolutionModes) {
			t.Fatalf("lease catalog = %+v", capability)
		}
		return
	}
	t.Fatal("resolver capability missing")
}

func issuePressLease(t *testing.T, session *Session, observation UIObservation, mode TargetResolutionMode, name string) TargetResolutionResult {
	t.Helper()
	spec := targetSpec(name)
	spec.RequiredStates = []UIState{UIStateEnabled}
	spec.RequiredActions = []UIAction{UIActionPress}
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: spec, Mode: mode,
		Lease: &CapabilityLeaseRequest{
			SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 1000,
		},
	})
	if err != nil {
		t.Fatalf("issue lease: result=%+v err=%v", result, err)
	}
	if result.Lease == nil || result.Lease.ID == "" || result.ElementID != "" || result.Expected != nil {
		t.Fatalf("unsafe lease result = %+v", result)
	}
	return result
}

func TestCapabilityLeaseConsumesExactlyOnceAtDispatch(t *testing.T) {
	session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
	issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
	request := ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress, Confirmed: true}
	result, err := session.ActUIElement(t.Context(), request)
	if err != nil || result.Status != ActionSucceeded || result.Proof.Lease == nil ||
		result.Proof.Lease.Status != CapabilityLeaseConsumed || driver.actCalls != 1 {
		t.Fatalf("leased action = %+v err=%v calls=%d", result, err, driver.actCalls)
	}
	replay, replayErr := session.ActUIElement(t.Context(), request)
	if !hasErrorCode(replayErr, ErrorLeaseConsumed) || replay.Proof.Lease.Status != CapabilityLeaseConsumed || driver.actCalls != 1 {
		t.Fatalf("replay = %+v err=%v calls=%d", replay, replayErr, driver.actCalls)
	}
	session.now = func() time.Time { return issued.Lease.ExpiresAt.Add(time.Second) }
	_, replayErr = session.ActUIElement(t.Context(), request)
	if !hasErrorCode(replayErr, ErrorLeaseConsumed) {
		t.Fatalf("expired replay changed terminal state: %v", replayErr)
	}
}

func TestCapabilityLeaseConcurrentReplayDispatchesAtMostOnce(t *testing.T) {
	session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
	issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
	const callers = 24
	var successes atomic.Uint32
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			result, err := session.ActUIElement(context.Background(), ElementActionRequest{
				CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress, Confirmed: true,
			})
			if err == nil && result.Status == ActionSucceeded {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || driver.actCalls != 1 {
		t.Fatalf("successes=%d native calls=%d", successes.Load(), driver.actCalls)
	}
}

func TestCapabilityLeaseExpiryMismatchObservationReleaseAndCloseFailClosed(t *testing.T) {
	t.Run("required", func(t *testing.T) {
		session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		button := observation.Elements[1]
		_, err := session.ActUIElement(t.Context(), ElementActionRequest{
			ObservationID: observation.ObservationID, ElementID: button.ElementID,
			Expected: expectationFromUIElement(&button), Action: UIActionPress,
		})
		if !hasErrorCode(err, ErrorLeaseRequired) || driver.actCalls != 0 {
			t.Fatalf("required err=%v calls=%d", err, driver.actCalls)
		}
	})
	t.Run("resolver-required", func(t *testing.T) {
		session, _, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		_, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{ObservationID: observation.ObservationID, Target: targetSpec("Save")})
		if !hasErrorCode(err, ErrorLeaseRequired) {
			t.Fatalf("resolver required err=%v", err)
		}
	})
	t.Run("expiry", func(t *testing.T) {
		session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		now := time.Unix(1_700_000_000, 0)
		session.now = func() time.Time { return now }
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		now = now.Add(2 * time.Second)
		_, err := session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorLeaseExpired) || driver.actCalls != 0 {
			t.Fatalf("expiry err=%v calls=%d", err, driver.actCalls)
		}
	})
	t.Run("wrong-action-invalidates", func(t *testing.T) {
		session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		_, err := session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionToggle})
		if !hasErrorCode(err, ErrorLeaseMismatch) {
			t.Fatalf("mismatch err=%v", err)
		}
		_, err = session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorLeaseInvalid) || driver.actCalls != 0 {
			t.Fatalf("retry err=%v calls=%d", err, driver.actCalls)
		}
	})
	t.Run("observation-release", func(t *testing.T) {
		session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		if err := session.ReleaseObservation(observation.ObservationID); err != nil {
			t.Fatal(err)
		}
		_, err := session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorLeaseInvalid) || driver.actCalls != 0 {
			t.Fatalf("released err=%v calls=%d", err, driver.actCalls)
		}
	})
	t.Run("wrong-session-and-close", func(t *testing.T) {
		session, _, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
		second, driver := newSemanticSession(t, capabilityLeasePolicy(), semanticSnapshot())
		_, err := second.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorLeaseInvalid) || driver.actCalls != 0 {
			t.Fatalf("wrong session err=%v calls=%d", err, driver.actCalls)
		}
	})
}

func TestCapabilityLeasePreDispatchFailuresInvalidateAndPostDispatchFailureConsumes(t *testing.T) {
	t.Run("resolver-completion-audit", func(t *testing.T) {
		session, _, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		sink := &recordingAuditSink{failAt: 2}
		session.auditSink = sink
		spec := targetSpec("Save")
		spec.RequiredActions = []UIAction{UIActionPress}
		result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
			ObservationID: observation.ObservationID, Target: spec,
			Lease: &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 1000},
		})
		if !hasErrorCode(err, ErrorAuditDelivery) || result.Lease != nil {
			t.Fatalf("resolver audit result=%+v err=%v", result, err)
		}
		for _, record := range session.leases {
			if record.status == CapabilityLeaseIssued {
				t.Fatalf("resolver audit left issued authority: %+v", record)
			}
		}
	})
	t.Run("audit-intent", func(t *testing.T) {
		session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		sink := &recordingAuditSink{failAt: 1}
		session.auditSink = sink
		_, err := session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorAuditDelivery) || driver.actCalls != 0 {
			t.Fatalf("audit err=%v calls=%d", err, driver.actCalls)
		}
		session.auditSink = nil
		_, err = session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorLeaseInvalid) {
			t.Fatalf("audit retry err=%v", err)
		}
	})
	t.Run("already-satisfied", func(t *testing.T) {
		policy := capabilityLeasePolicy()
		policy.UIVerificationAttempts = 1
		policy.UIVerificationIntervalMillis = 1
		policy.UIVerificationTimeoutMillis = 1000
		policy.MaxQueries = 8
		policy.MaxObservations = 8
		session, driver, observation := inspectResolverFixture(t, policy, semanticSnapshot())
		condition := &UIElementCondition{Kind: UIElementConditionStatePresent, State: UIStateEnabled}
		spec := targetSpec("Save")
		spec.RequiredActions = []UIAction{UIActionPress}
		issued, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
			ObservationID: observation.ObservationID, Target: spec,
			Lease: &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress,
				Postcondition: condition, DurationMillis: 1000},
		})
		if err != nil {
			t.Fatal(err)
		}
		driver.checkResults = []uiBackendElementConditionResult{{Satisfied: true, CleanupComplete: true}}
		result, err := session.ActUIElement(t.Context(), ElementActionRequest{
			CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress, Postcondition: condition,
		})
		if err != nil || result.Proof.Execution.Status != ActionExecutionSkippedAlreadySatisfied ||
			result.Proof.Lease.Status != CapabilityLeaseInvalidated || driver.actCalls != 0 {
			t.Fatalf("already satisfied=%+v err=%v calls=%d", result, err, driver.actCalls)
		}
		_, err = session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress, Postcondition: condition})
		if !hasErrorCode(err, ErrorLeaseInvalid) {
			t.Fatalf("skip replay err=%v", err)
		}
	})
	t.Run("native-uncertainty", func(t *testing.T) {
		session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		driver.actErr = errors.New("native result uncertain")
		result, err := session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorBackendFailure) || result.Proof.Lease.Status != CapabilityLeaseConsumed || driver.actCalls != 1 {
			t.Fatalf("uncertain=%+v err=%v calls=%d", result, err, driver.actCalls)
		}
	})
	t.Run("cancellation-before-dispatch", func(t *testing.T) {
		session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		driver.actBlock = true
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, err := session.ActUIElement(ctx, ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorTimedOut) || driver.actCalls != 1 {
			t.Fatalf("cancel err=%v calls=%d", err, driver.actCalls)
		}
		driver.actBlock = false
		_, err = session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorLeaseInvalid) {
			t.Fatalf("cancel retry err=%v", err)
		}
	})
	t.Run("timeout-waiting-for-session-gate", func(t *testing.T) {
		session, _, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		<-session.actionGate
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, err := session.ActUIElement(ctx, ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		cancel()
		session.actionGate <- struct{}{}
		if !hasErrorCode(err, ErrorTimedOut) {
			t.Fatalf("gate timeout err=%v", err)
		}
		_, err = session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress})
		if !hasErrorCode(err, ErrorLeaseInvalid) {
			t.Fatalf("gate timeout retry err=%v", err)
		}
	})
}

func TestCapabilityLeaseBindsSetValueWithoutRetainingValue(t *testing.T) {
	policy := capabilityLeasePolicy()
	policy.AllowedUIRoles = append(policy.AllowedUIRoles, UIRoleTextBox)
	snapshot := semanticSnapshot()
	snapshot.Nodes = append(snapshot.Nodes, uiBackendNode{
		StableID: []byte("native-textbox"), Parent: 0, Depth: 1, Role: UIRoleTextBox, Name: "Alias",
		States: []UIState{UIStateEnabled}, Actions: []UIAction{UIActionSetValue}, Bounds: &UIBounds{Width: 100, Height: 20},
	})
	session, driver, observation := inspectResolverFixture(t, policy, snapshot)
	spec := targetSpec("Alias")
	spec.Role = UIRoleTextBox
	spec.RequiredActions = []UIAction{UIActionSetValue}
	secret := "not-retained-secret"
	issued, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: spec,
		Lease: &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionSetValue,
			ActionValueSHA256: CapabilityLeaseActionValueDigest(secret), DurationMillis: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: issued.Lease.ID, Action: UIActionSetValue, Value: "wrong"})
	if !hasErrorCode(err, ErrorLeaseMismatch) || driver.actCalls != 0 {
		t.Fatalf("value mismatch err=%v calls=%d", err, driver.actCalls)
	}
	store := fmt.Sprintf("%#v", session.leases)
	if containsString([]byte(store), secret) {
		t.Fatalf("lease store retained raw value: %s", store)
	}
}

func TestAdaptiveAndReviewResolutionAreUniqueAndNonExecutable(t *testing.T) {
	adaptiveSnapshot := func() uiBackendSnapshot {
		snapshot := semanticSnapshot()
		snapshot.Nodes[1].Name = "Store"
		return snapshot
	}
	t.Run("adaptive-unique", func(t *testing.T) {
		session, _, observation := inspectResolverFixture(t, capabilityLeasePolicy(), adaptiveSnapshot())
		result := issuePressLease(t, session, observation, TargetResolutionModeAdaptive, "Save")
		if result.Strategy != TargetResolutionAdaptiveSemantic || result.AdaptiveScore != 75 ||
			result.AdaptiveThreshold != 75 || result.CandidateCount != 1 {
			t.Fatalf("adaptive result = %+v", result)
		}
	})
	t.Run("review", func(t *testing.T) {
		session, driver, observation := inspectResolverFixture(t, capabilityLeasePolicy(), adaptiveSnapshot())
		spec := targetSpec("Save")
		spec.RequiredActions = []UIAction{UIActionPress}
		result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
			ObservationID: observation.ObservationID, Target: spec, Mode: TargetResolutionModeReview,
		})
		if err != nil || result.Patch == nil || result.Patch.Executable || result.Lease != nil ||
			len(result.Patch.Changed) != 1 || result.Patch.Changed[0] != TargetEvidenceName || driver.actCalls != 0 {
			t.Fatalf("review result=%+v err=%v calls=%d", result, err, driver.actCalls)
		}
	})
	t.Run("ambiguous-even-with-order", func(t *testing.T) {
		ambiguous := semanticSnapshot()
		ambiguous.Nodes[1].Name = "Store"
		ambiguous.Nodes = append(ambiguous.Nodes, uiBackendNode{
			StableID: []byte("other-button"), Parent: 0, Depth: 1, Role: UIRoleButton, Name: "Submit",
			States: []UIState{UIStateEnabled}, Actions: []UIAction{UIActionPress}, Bounds: &UIBounds{Width: 40, Height: 20},
		})
		session, _, observation := inspectResolverFixture(t, capabilityLeasePolicy(), ambiguous)
		spec := targetSpec("Save")
		spec.RequiredActions = []UIAction{UIActionPress}
		result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
			ObservationID: observation.ObservationID, Target: spec, Mode: TargetResolutionModeAdaptive,
			Lease: &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 1000},
		})
		if !hasErrorCode(err, ErrorAmbiguousTarget) || !result.Ambiguous || result.CandidateCount != 2 || result.Lease != nil {
			t.Fatalf("ambiguous result=%+v err=%v", result, err)
		}
	})
}

func TestCapabilityLeaseSerializationContainsNoDesktopOrAuthorityPayload(t *testing.T) {
	session, _, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
	sink := &recordingAuditSink{}
	session.auditSink = sink
	result := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
	token := result.Lease.ID
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"fixture", "Save", "native-button", "expected_title", "policy"} {
		if containsString(payload, forbidden) {
			t.Fatalf("lease result leaked %q: %s", forbidden, payload)
		}
	}
	action, _ := session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: token, Action: UIActionPress})
	proof, err := json.Marshal(action.Proof)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(proof, token) || containsString(proof, "Save") || containsString(proof, "native-button") {
		t.Fatalf("proof leaked authority or desktop payload: %s", proof)
	}
	audit, err := json.Marshal(sink.events)
	if err != nil {
		t.Fatal(err)
	}
	if containsString(audit, token) || containsString(audit, "Save") || containsString(audit, "fixture") || containsString(audit, "native-button") {
		t.Fatalf("audit leaked authority or desktop payload: %s", audit)
	}
}

func containsString(payload []byte, value string) bool {
	for index := 0; index+len(value) <= len(payload); index++ {
		if string(payload[index:index+len(value)]) == value {
			return true
		}
	}
	return false
}
