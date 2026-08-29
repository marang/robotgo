package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingTraceSink struct {
	mu     sync.Mutex
	traces []RobotGoTrace
	err    error
}

func (sink *recordingTraceSink) ExportTrace(_ context.Context, trace RobotGoTrace) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.traces = append(sink.traces, trace)
	return sink.err
}

func tracePolicy(export bool, tiers ...TracePrivacyTier) Policy {
	policy := semanticVerificationPolicy()
	policy.AllowedTraceTiers = append([]TracePrivacyTier(nil), tiers...)
	policy.MaxTraceEvents = 16
	policy.MaxTraceBytes = 16 << 10
	policy.TraceLifetimeMillis = 5_000
	policy.AllowTraceExport = export
	return policy
}

func verifiedTraceFixture(
	t *testing.T,
	policy Policy,
	tier TracePrivacyTier,
) (*Session, *semanticFakeDriver, ElementActionRequest) {
	t.Helper()
	snapshot := semanticSnapshot()
	snapshot.Nodes[1].Role = UIRoleTextBox
	snapshot.Nodes[1].Name = "trace-private-name-sentinel"
	snapshot.Nodes[1].StableID = []byte("trace-private-native-sentinel")
	snapshot.Nodes[1].Actions = []UIAction{UIActionSetValue}
	policy.AllowedUIRoles = append(policy.AllowedUIRoles, UIRoleTextBox)
	session, driver := newSemanticSession(t, policy, snapshot)
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	element := observation.Elements[1]
	driver.checkResults = []uiBackendElementConditionResult{
		{CleanupComplete: true},
		{Satisfied: true, CleanupComplete: true},
	}
	return session, driver, ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: element.ElementID,
		Action: UIActionSetValue, Expected: expectationFromUIElement(&element),
		Value: "trace-private-value-sentinel", Confirmed: true,
		Postcondition: &UIElementCondition{Kind: UIElementConditionValueEqualsActionValue},
		Trace:         &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: tier},
	}
}

func traceEventByKind(t *testing.T, trace *RobotGoTrace, kind TraceEventKind) TraceEvent {
	t.Helper()
	if trace != nil {
		for _, event := range trace.Events {
			if event.Kind == kind {
				return event
			}
		}
	}
	t.Fatalf("trace event %q missing: %+v", kind, trace)
	return TraceEvent{}
}

func TestTracePolicyIsDenyByDefaultAndRequiresAllBounds(t *testing.T) {
	t.Run("explicit-request-required", func(t *testing.T) {
		session, driver, request := inspectSemanticConditionFixture(t, tracePolicy(false, TracePrivacyMetadataOnly))
		driver.checkResults = []uiBackendElementConditionResult{{Satisfied: true, CleanupComplete: true}}
		result, err := session.ActUIElement(t.Context(), request)
		if err != nil || result.Trace != nil || driver.actCalls != 0 || driver.checkCalls != 1 {
			t.Fatalf("implicit trace result=%+v err=%v calls=%d/%d", result, err, driver.actCalls, driver.checkCalls)
		}
	})

	t.Run("request-denied", func(t *testing.T) {
		session, driver, request := inspectSemanticConditionFixture(t, semanticVerificationPolicy())
		request.Trace = &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyMetadataOnly}
		result, err := session.ActUIElement(t.Context(), request)
		if !hasErrorCode(err, ErrorPolicyDenied) || result.Trace != nil || driver.actCalls != 0 || driver.checkCalls != 0 {
			t.Fatalf("default-denied trace result=%+v err=%v calls=%d/%d", result, err, driver.actCalls, driver.checkCalls)
		}
	})

	for name, mutate := range map[string]func(*Policy){
		"missing-events":        func(policy *Policy) { policy.MaxTraceEvents = 0 },
		"events-below-minimum":  func(policy *Policy) { policy.MaxTraceEvents = minAgentTraceEvents - 1 },
		"events-above-maximum":  func(policy *Policy) { policy.MaxTraceEvents = maxAgentTraceEvents + 1 },
		"missing-bytes":         func(policy *Policy) { policy.MaxTraceBytes = 0 },
		"bytes-below-minimum":   func(policy *Policy) { policy.MaxTraceBytes = minAgentTraceBytes - 1 },
		"bytes-above-maximum":   func(policy *Policy) { policy.MaxTraceBytes = maxAgentTraceBytes + 1 },
		"missing-lifetime":      func(policy *Policy) { policy.TraceLifetimeMillis = 0 },
		"lifetime-over-session": func(policy *Policy) { policy.TraceLifetimeMillis = policy.SessionTimeoutMillis + 1 },
		"duplicate-tier": func(policy *Policy) {
			policy.AllowedTraceTiers = append(policy.AllowedTraceTiers, TracePrivacyMetadataOnly)
		},
		"unknown-tier": func(policy *Policy) { policy.AllowedTraceTiers[0] = "private-everything" },
	} {
		t.Run(name, func(t *testing.T) {
			policy := tracePolicy(false, TracePrivacyMetadataOnly)
			mutate(&policy)
			if _, err := preparePolicy(policy); err == nil {
				t.Fatalf("invalid trace policy accepted: %+v", policy)
			}
		})
	}
	policy := semanticVerificationPolicy()
	policy.MaxTraceEvents = 3
	if _, err := preparePolicy(policy); err == nil {
		t.Fatal("trace bound without an allowed tier was accepted")
	}
	policy = tracePolicy(false, TracePrivacyMetadataOnly)
	policy.AllowedOperations = []Operation{OperationInspectUI}
	if _, err := preparePolicy(policy); err == nil {
		t.Fatal("trace policy without desktop.element-act was accepted")
	}

	t.Run("invalid-request", func(t *testing.T) {
		session, driver, request := inspectSemanticConditionFixture(t, tracePolicy(false, TracePrivacyMetadataOnly))
		request.Trace = &TraceRequest{SchemaVersion: "2", Tier: TracePrivacyMetadataOnly}
		result, err := session.ActUIElement(t.Context(), request)
		if !hasErrorCode(err, ErrorInvalidInput) || result.Trace != nil || driver.actCalls != 0 || driver.checkCalls != 0 {
			t.Fatalf("invalid trace request result=%+v err=%v calls=%d/%d", result, err, driver.actCalls, driver.checkCalls)
		}
	})

	t.Run("lease-store", func(t *testing.T) {
		session, _, observation := inspectResolverFixture(t, capabilityLeasePolicy(), semanticSnapshot())
		issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
		key := sha256.Sum256([]byte(issued.Lease.ID))
		record := session.leases[key]
		if record.traceResolution.present || len(record.traceResolution.matchedBy) != 0 ||
			len(record.traceResolution.evidenceSources) != 0 {
			t.Fatalf("default-denied lease retained trace provenance: %+v", record.traceResolution)
		}
	})
}

func TestTraceTiersSerializeWithoutForbiddenPayloads(t *testing.T) {
	for _, tier := range allTracePrivacyTiers {
		t.Run(string(tier), func(t *testing.T) {
			policy := tracePolicy(false, tier)
			session, driver, request := verifiedTraceFixture(t, policy, tier)
			result, err := session.ActUIElement(t.Context(), request)
			if err != nil || result.Status != ActionSucceeded || result.Proof.Status != ActionProofVerified ||
				result.Trace == nil || result.Trace.SchemaVersion != RobotGoTraceSchemaVersion ||
				result.Trace.TransactionID != result.ActionID || result.Trace.Tier != tier ||
				result.Trace.Truncated || !result.Trace.CleanupComplete || driver.actCalls != 1 {
				t.Fatalf("tier %s result=%+v err=%v calls=%d", tier, result, err, driver.actCalls)
			}
			for index, event := range result.Trace.Events {
				if event.Sequence != uint32(index+1) || event.TransactionID != result.ActionID {
					t.Fatalf("tier %s ordering/lineage event=%+v at %d", tier, event, index)
				}
			}
			serialized, err := json.Marshal(result.Trace)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{
				"trace-private-name-sentinel", "trace-private-native-sentinel",
				"trace-private-value-sentinel", "native-window-991", "fixture",
			} {
				if strings.Contains(string(serialized), forbidden) {
					t.Fatalf("tier %s leaked %q: %s", tier, forbidden, serialized)
				}
			}
			resolution := traceEventByKind(t, result.Trace, TraceResolutionFinished)
			dispatch := traceEventByKind(t, result.Trace, TraceDispatchFinished)
			verification := traceEventByKind(t, result.Trace, TraceVerificationFinished)
			if dispatch.Code != TraceCodeDispatched || verification.Code != TraceCodeMatched ||
				verification.PostconditionAttempts != 1 {
				t.Fatalf("tier %s dispatch/verification=%+v/%+v", tier, dispatch, verification)
			}
			if tier == TracePrivacyMetadataOnly && resolution.ObservationID != "" {
				t.Fatalf("metadata-only trace retained semantic fields: %+v %+v", resolution, dispatch)
			}
			if tier != TracePrivacyMetadataOnly && resolution.ObservationID == "" {
				t.Fatalf("tier %s omitted redacted observation lineage: %+v", tier, resolution)
			}
			if result.Trace.Redacted != (tier != TracePrivacyFullExplicit) {
				t.Fatalf("tier %s redacted=%v", tier, result.Trace.Redacted)
			}
			if len(result.Trace.Events) > int(policy.MaxTraceEvents) || traceJSONSize(*result.Trace) > policy.MaxTraceBytes {
				t.Fatalf("tier %s exceeded policy bounds: events=%d bytes=%d", tier,
					len(result.Trace.Events), traceJSONSize(*result.Trace))
			}
		})
	}
}

func TestTraceCarriesLeaseBoundResolverEvidenceWithoutAuthority(t *testing.T) {
	policy := capabilityLeasePolicy()
	policy.AllowedTraceTiers = []TracePrivacyTier{TracePrivacyFullExplicit}
	policy.MaxTraceEvents = 16
	policy.MaxTraceBytes = 16 << 10
	policy.TraceLifetimeMillis = 5_000
	session, _, observation := inspectResolverFixture(t, policy, semanticSnapshot())
	issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		CapabilityLeaseID: issued.Lease.ID, Action: UIActionPress, Confirmed: true,
		Trace: &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyFullExplicit},
	})
	if err != nil || result.Trace == nil {
		t.Fatalf("leased trace result=%+v err=%v", result, err)
	}
	resolution := traceEventByKind(t, result.Trace, TraceResolutionFinished)
	if resolution.Code != TraceCodeResolved || resolution.ObservationID != observation.ObservationID ||
		resolution.TargetResolutionStrategy != TargetResolutionExactSemantic ||
		resolution.TargetResolutionMode != TargetResolutionModeStrict || resolution.CandidateCount != 1 ||
		len(resolution.MatchedBy) == 0 {
		t.Fatalf("lease-bound resolver trace=%+v", resolution)
	}
	serialized, err := json.Marshal(result.Trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), issued.Lease.ID) || strings.Contains(string(serialized), "native-button") ||
		strings.Contains(string(serialized), "Save") || strings.Contains(string(serialized), "fixture") {
		t.Fatalf("trace leaked lease authority or desktop identity: %s", serialized)
	}
	key := sha256.Sum256([]byte(issued.Lease.ID))
	stored := session.leases[key]
	if stored.status != CapabilityLeaseConsumed || stored.traceResolution.present ||
		len(stored.traceResolution.matchedBy) != 0 || len(stored.traceResolution.evidenceSources) != 0 {
		t.Fatalf("terminal lease retained trace provenance: %+v", stored)
	}
}

func TestTraceResolverEvidenceIsClearedOnTerminalLeaseTransitions(t *testing.T) {
	for _, test := range []struct {
		name       string
		transition func(*Session, string, [sha256.Size]byte)
		wantStatus CapabilityLeaseStatus
	}{
		{
			name: "invalidated", wantStatus: CapabilityLeaseInvalidated,
			transition: func(session *Session, id string, _ [sha256.Size]byte) {
				session.invalidateIssuedCapabilityLease(id)
			},
		},
		{
			name: "expired", wantStatus: CapabilityLeaseExpired,
			transition: func(session *Session, id string, key [sha256.Size]byte) {
				record := session.leases[key]
				record.expiresAt = session.now()
				session.leases[key] = record
				_, actionErr := session.reserveCapabilityLease(ElementActionRequest{
					CapabilityLeaseID: id, Action: record.action, Postcondition: record.postcondition,
				})
				if actionErr == nil || actionErr.Code != ErrorLeaseExpired {
					t.Fatalf("expired lease error=%v", actionErr)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := capabilityLeasePolicy()
			policy.AllowedTraceTiers = []TracePrivacyTier{TracePrivacyFullExplicit}
			policy.MaxTraceEvents = 16
			policy.MaxTraceBytes = 16 << 10
			policy.TraceLifetimeMillis = 5_000
			session, _, observation := inspectResolverFixture(t, policy, semanticSnapshot())
			issued := issuePressLease(t, session, observation, TargetResolutionModeStrict, "Save")
			key := sha256.Sum256([]byte(issued.Lease.ID))
			if !session.leases[key].traceResolution.present {
				t.Fatal("trace resolver provenance was not retained for the active lease")
			}
			test.transition(session, issued.Lease.ID, key)
			stored := session.leases[key]
			if stored.status != test.wantStatus || stored.traceResolution.present ||
				len(stored.traceResolution.matchedBy) != 0 || len(stored.traceResolution.evidenceSources) != 0 {
				t.Fatalf("terminal lease retained trace provenance: %+v", stored)
			}
		})
	}
}

func TestTraceCorrelatesWithActionProofAndAuditWithoutVersionChanges(t *testing.T) {
	policy := tracePolicy(false, TracePrivacyMetadataOnly)
	session, _, request := verifiedTraceFixture(t, policy, TracePrivacyMetadataOnly)
	audit := &recordingAuditSink{}
	session.auditSink = audit
	result, err := session.ActUIElement(t.Context(), request)
	if err != nil || result.Trace == nil || result.Proof == nil ||
		result.Trace.TransactionID != result.Proof.TransactionID || result.Proof.TransactionID != result.ActionID {
		t.Fatalf("trace/proof correlation result=%+v err=%v", result, err)
	}
	if len(audit.events) != 3 {
		t.Fatalf("audit events=%+v", audit.events)
	}
	for _, event := range audit.events {
		if event.SchemaVersion != AuditSchemaVersion || event.ActionID != result.ActionID {
			t.Fatalf("trace/audit correlation event=%+v result=%+v", event, result)
		}
	}
}

func TestTraceVisualTierProjectsImageEvidenceProvenance(t *testing.T) {
	for _, tier := range []TracePrivacyTier{TracePrivacyVisualRedacted, TracePrivacyFullExplicit} {
		t.Run(string(tier), func(t *testing.T) {
			policy := targetEvidencePolicy(TargetEvidenceSourceOCR)
			policy.AllowedTraceTiers = []TracePrivacyTier{tier}
			policy.MaxTraceEvents = 16
			policy.MaxTraceBytes = 16 << 10
			policy.TraceLifetimeMillis = 5_000
			session, _ := targetEvidenceSession(t, policy)
			installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
				text: []byte("trace-private-ocr-sentinel"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
			}}})
			view := createTargetEvidenceView(t, session)
			ocr, err := session.OCR(t.Context(), OCRRequest{
				ObservationID: view.ObservationID, Region: targetEvidenceAnalysisRegion,
				Languages: []string{"eng"}, MinConfidence: 0.8,
			})
			if err != nil {
				t.Fatal(err)
			}
			ui := inspectTargetEvidenceUI(t, session)
			resolved, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
				ObservationID: ui.ObservationID,
				Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
				Mode:          TargetResolutionModeAdaptive,
				Lease: &CapabilityLeaseRequest{
					SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.ActUIElement(t.Context(), ElementActionRequest{
				CapabilityLeaseID: resolved.Lease.ID, Action: UIActionPress, Confirmed: true,
				Trace: &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: tier},
			})
			if err != nil || result.Trace == nil {
				t.Fatalf("evidence trace result=%+v err=%v", result, err)
			}
			resolution := traceEventByKind(t, result.Trace, TraceResolutionFinished)
			if resolution.EvidenceObservationID != view.ObservationID ||
				len(resolution.EvidenceSources) != 1 || resolution.EvidenceSources[0] != TargetEvidenceSourceOCR ||
				resolution.TargetResolutionStrategy != TargetResolutionOCREvidence {
				t.Fatalf("evidence provenance=%+v", resolution)
			}
			if tier == TracePrivacyVisualRedacted && len(resolution.EvidenceProviders) != 0 {
				t.Fatalf("visual-redacted trace exposed provider: %+v", resolution.EvidenceProviders)
			}
			if tier == TracePrivacyFullExplicit && len(resolution.EvidenceProviders) != 1 {
				t.Fatalf("full-explicit trace omitted provider: %+v", resolution.EvidenceProviders)
			}
			serialized, err := json.Marshal(result.Trace)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(serialized), "trace-private-ocr-sentinel") ||
				strings.Contains(string(serialized), resolved.Lease.ID) {
				t.Fatalf("evidence trace leaked OCR text or authority: %s", serialized)
			}
		})
	}
}

func TestTraceTruncationPreservesLifecycleAndByteBound(t *testing.T) {
	policy := tracePolicy(false, TracePrivacyFullExplicit)
	policy.MaxTraceEvents = minAgentTraceEvents
	policy.MaxTraceBytes = minAgentTraceBytes
	session, _, request := verifiedTraceFixture(t, policy, TracePrivacyFullExplicit)
	result, err := session.ActUIElement(t.Context(), request)
	if err != nil || result.Trace == nil || !result.Trace.Truncated || !result.Trace.MissingEvidence ||
		len(result.Trace.Events) != minAgentTraceEvents {
		t.Fatalf("truncated trace=%+v err=%v", result.Trace, err)
	}
	if result.Trace.Events[0].Kind != TraceTransactionStarted ||
		result.Trace.Events[len(result.Trace.Events)-2].Kind != TraceCleanupFinished ||
		result.Trace.Events[len(result.Trace.Events)-1].Kind != TraceTransactionFinished {
		t.Fatalf("truncation lost terminal lifecycle: %+v", result.Trace.Events)
	}
	if size := traceJSONSize(*result.Trace); size > policy.MaxTraceBytes {
		t.Fatalf("trace size=%d exceeds %d", size, policy.MaxTraceBytes)
	}

	session.policy.MaxTraceEvents = 16
	transactionID := "action-byte-bound"
	recorder, traceErr := session.prepareActionTrace(transactionID, &TraceRequest{
		SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyFullExplicit,
	})
	if traceErr != nil {
		t.Fatal(traceErr)
	}
	providerText := strings.Repeat("p", maxTargetEvidenceIdentifierBytes)
	recorder.setResolution(retainedTraceResolution{
		present: true, observationID: "observation-123", evidenceObservationID: "observation-124",
		strategy: TargetResolutionCombinedEvidence, mode: TargetResolutionModeAdaptive,
		candidateCount: 1, rejectedCount: 99, adaptiveScore: 100, adaptiveThreshold: 90,
		matchedBy: append([]TargetEvidence(nil), targetEvidenceTokens(retainedTargetEvidenceBundle{
			evidence: []retainedTargetEvidence{{source: TargetEvidenceSourceOCR}, {source: TargetEvidenceSourceVisual}},
		})...),
		evidenceSources: []TargetEvidenceSource{TargetEvidenceSourceOCR, TargetEvidenceSourceVisual},
		evidenceProviders: []TargetEvidenceProvider{
			{Source: TargetEvidenceSourceOCR, Backend: providerText, Model: providerText},
			{Source: TargetEvidenceSourceVisual, Backend: providerText, Model: providerText},
		},
	})
	fullResult := ActionResult{
		ActionID: transactionID, Operation: OperationElementAct, Status: ActionSucceeded,
		PreconditionObservationID: "observation-123",
		Proof: &ActionProof{
			SchemaVersion: ActionProofSchemaVersion, TransactionID: transactionID, Status: ActionProofVerified,
			Resolution:    &ActionResolutionProof{Strategy: ActionResolutionAdaptiveLease, CandidateCount: 1, Healing: true},
			Authorization: &ActionAuthorizationProof{PolicyAllowed: true, ConfirmationRequired: true, Confirmed: true},
			Execution:     ActionExecutionProof{Backend: providerText, Action: UIActionPress, Status: ActionExecutionDispatched},
			Verification: &ActionVerificationProof{ConditionKind: UIElementConditionStatePresent,
				Status: ActionVerificationMatched, PrecheckAttempts: 1, FinalGateChecked: true, PostconditionAttempts: 3},
			Cleanup: ActionCleanupProof{TransientResourcesReleased: true},
		},
	}
	byteBoundTrace, finishErr := recorder.finish(fullResult, nil)
	if finishErr != nil || !byteBoundTrace.Truncated || !byteBoundTrace.MissingEvidence ||
		traceJSONSize(byteBoundTrace) > session.policy.MaxTraceBytes {
		t.Fatalf("byte-bounded trace=%+v size=%d err=%v", byteBoundTrace, traceJSONSize(byteBoundTrace), finishErr)
	}
}

func TestTraceProjectionAndTruncationClearRemovedProvenance(t *testing.T) {
	providerBacking := []TargetEvidenceProvider{{
		Source: TargetEvidenceSourceOCR, Backend: "private-provider", Model: "private-model",
	}}
	sourceBacking := []TargetEvidenceSource{TargetEvidenceSourceOCR}
	matchedBacking := []TargetEvidence{TargetEvidenceName}
	changedBacking := []TargetEvidence{TargetEvidenceAncestors}
	event := TraceEvent{
		EvidenceProviders: providerBacking, EvidenceSources: sourceBacking,
		MatchedBy: matchedBacking, Changed: changedBacking,
	}
	projectTraceEvent(&event, TracePrivacyMetadataOnly)
	if providerBacking[0] != (TargetEvidenceProvider{}) || sourceBacking[0] != "" ||
		matchedBacking[0] != "" || changedBacking[0] != "" {
		t.Fatalf("projection retained removed provenance: %+v %+v %+v %+v",
			providerBacking, sourceBacking, matchedBacking, changedBacking)
	}

	removedBacking := []TargetEvidenceProvider{{
		Source: TargetEvidenceSourceVisual, Backend: "private-provider", Model: "private-model",
	}}
	trace := RobotGoTrace{Events: []TraceEvent{
		{Kind: TraceTransactionStarted},
		{Kind: TraceResolutionFinished, EvidenceProviders: removedBacking},
		{Kind: TraceCleanupFinished},
		{Kind: TraceTransactionFinished},
	}}
	removeTraceDetailEvent(&trace)
	if removedBacking[0] != (TargetEvidenceProvider{}) || len(trace.Events) != 3 {
		t.Fatalf("truncation retained removed provenance: %+v events=%+v", removedBacking, trace.Events)
	}
}

func TestTraceCancellationAndLifetimeAreExplicit(t *testing.T) {
	t.Run("canceled", func(t *testing.T) {
		policy := tracePolicy(false, TracePrivacyMetadataOnly)
		policy.MaxTraceEvents = 4
		session, driver, request := inspectSemanticConditionFixture(t, policy)
		request.Trace = &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyMetadataOnly}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := session.ActUIElement(ctx, request)
		if !hasErrorCode(err, ErrorCanceled) || result.Trace == nil || driver.actCalls != 0 {
			t.Fatalf("canceled trace=%+v err=%v calls=%d", result.Trace, err, driver.actCalls)
		}
		cancellation := traceEventByKind(t, result.Trace, TraceCancellationObserved)
		if cancellation.Code != TraceCodeCanceled || cancellation.ErrorCode != ErrorCanceled {
			t.Fatalf("cancellation event=%+v", cancellation)
		}
		if !result.Trace.Truncated || len(result.Trace.Events) != 4 {
			t.Fatalf("bounded cancellation trace=%+v", result.Trace)
		}
	})

	t.Run("expired", func(t *testing.T) {
		policy := tracePolicy(false, TracePrivacyMetadataOnly)
		policy.TraceLifetimeMillis = 100
		session, driver, request := inspectSemanticConditionFixture(t, policy)
		clock := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
		session.now = func() time.Time { return clock }
		driver.checkResults = []uiBackendElementConditionResult{{Satisfied: true, CleanupComplete: true}}
		driver.checkFinish = func(int) { clock = clock.Add(100 * time.Millisecond) }
		request.Trace = &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyMetadataOnly}
		result, err := session.ActUIElement(t.Context(), request)
		if err != nil || result.Trace == nil || !result.Trace.Expired || !result.Trace.Truncated ||
			!result.Trace.MissingEvidence {
			t.Fatalf("expired trace=%+v err=%v", result.Trace, err)
		}
	})
}

func TestTraceExportIsAtomicAndFailureDoesNotRewriteActionOutcome(t *testing.T) {
	for _, test := range []struct {
		name       string
		exportErr  error
		wantStatus TraceExportStatus
		wantErr    bool
	}{
		{name: "success", wantStatus: TraceExportSucceeded},
		{name: "failure", exportErr: errors.New("private sink diagnostic"), wantStatus: TraceExportFailed, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := tracePolicy(true, TracePrivacySemanticRedacted)
			session, _, request := verifiedTraceFixture(t, policy, TracePrivacySemanticRedacted)
			sink := &recordingTraceSink{err: test.exportErr}
			session.traceSink = sink
			request.Trace.Export = true
			result, err := session.ActUIElement(t.Context(), request)
			if result.Status != ActionSucceeded || result.Proof.Status != ActionProofVerified ||
				result.Trace == nil || result.Trace.ExportStatus != test.wantStatus || len(sink.traces) != 1 {
				t.Fatalf("export result=%+v err=%v traces=%d", result, err, len(sink.traces))
			}
			if test.wantErr {
				if !errors.Is(err, ErrTraceExport) || result.Error != nil || result.Proof.ErrorCode != "" ||
					result.Trace.ExportErrorCode != ErrorTraceExport {
					t.Fatalf("export failure rewrote transaction: result=%+v err=%v", result, err)
				}
				for _, event := range sink.traces[0].Events {
					if event.Kind != "" {
						t.Fatalf("failed export retained transient event: %+v", event)
					}
				}
				serialized, marshalErr := json.Marshal(result)
				if marshalErr != nil || strings.Contains(string(serialized), "private sink diagnostic") {
					t.Fatalf("export failure leaked sink diagnostics: %s err=%v", serialized, marshalErr)
				}
			} else if err != nil || sink.traces[0].ExportStatus != TraceExportSucceeded {
				t.Fatalf("successful export err=%v trace=%+v", err, sink.traces[0])
			}
		})
	}
}

func TestTraceConcurrentTransactionsRemainIsolated(t *testing.T) {
	policy := tracePolicy(false, TracePrivacyMetadataOnly)
	session, driver := newSemanticSession(t, policy, semanticSnapshot())
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	button := observation.Elements[1]
	request := ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: button.ElementID,
		Action: UIActionPress, Expected: expectationFromUIElement(&button), Confirmed: true,
		Trace: &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyMetadataOnly},
	}
	base := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	var ticks atomic.Int64
	session.now = func() time.Time { return base.Add(time.Duration(ticks.Add(2)) * time.Millisecond) }

	type outcome struct {
		result ActionResult
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			result, err := session.ActUIElement(context.Background(), request)
			results <- outcome{result: result, err: err}
		}()
	}
	first, second := <-results, <-results
	for index, outcome := range []outcome{first, second} {
		if outcome.err != nil || outcome.result.Trace == nil || outcome.result.Trace.TransactionID != outcome.result.ActionID {
			t.Fatalf("outcome %d=%+v err=%v", index, outcome.result, outcome.err)
		}
	}
	if first.result.ActionID == second.result.ActionID || first.result.Trace == second.result.Trace || driver.actCalls != 2 {
		t.Fatalf("concurrent traces crossed: first=%s second=%s calls=%d", first.result.ActionID, second.result.ActionID, driver.actCalls)
	}
}

func TestTraceCloseRaceReturnsOneBoundedTerminalTrace(t *testing.T) {
	session, driver, request := inspectSemanticConditionFixture(t, tracePolicy(false, TracePrivacyMetadataOnly))
	request.Trace = &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyMetadataOnly}
	<-session.actionGate
	started := make(chan struct{})
	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		close(started)
		result, err := session.ActUIElement(context.Background(), request)
		resultCh <- result
		errCh <- err
	}()
	<-started
	session.cancel()
	session.actionGate <- struct{}{}
	result, err := <-resultCh, <-errCh
	if !hasErrorCode(err, ErrorSessionClosed) || result.Trace == nil ||
		result.Trace.TransactionID != result.ActionID || len(result.Trace.Events) > int(session.policy.MaxTraceEvents) ||
		traceEventByKind(t, result.Trace, TraceCleanupFinished).Code != TraceCodeCleanupComplete ||
		driver.actCalls != 0 || driver.checkCalls != 0 {
		t.Fatalf("close-race trace=%+v err=%v calls=%d/%d", result.Trace, err, driver.actCalls, driver.checkCalls)
	}
}

func TestTraceCatalogExposesConfiguredBounds(t *testing.T) {
	policy := tracePolicy(true, TracePrivacyMetadataOnly, TracePrivacySemanticRedacted)
	session, _ := newSemanticSession(t, policy, semanticSnapshot())
	for _, capability := range session.Catalog().Operations {
		if capability.Operation != OperationElementAct {
			continue
		}
		if capability.TraceSchemaVersion != RobotGoTraceSchemaVersion ||
			fmt.Sprint(capability.TracePrivacyTiers) != fmt.Sprint(policy.AllowedTraceTiers) ||
			capability.MaxTraceEvents != policy.MaxTraceEvents || capability.MaxTraceBytes != policy.MaxTraceBytes ||
			capability.TraceLifetimeMillis != policy.TraceLifetimeMillis || !capability.TraceExportAllowed {
			t.Fatalf("trace catalog=%+v", capability)
		}
		return
	}
	t.Fatal("element action capability missing")
}
