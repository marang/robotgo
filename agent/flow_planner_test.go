package agent

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
)

func flowPlannerPolicy() Policy {
	policy := tracePolicy(false, TracePrivacyMetadataOnly)
	policy.AllowedOperations = append(policy.AllowedOperations, OperationResolveUI)
	policy.AllowedTargetModes = []TargetResolutionMode{
		TargetResolutionModeStrict, TargetResolutionModeAdaptive, TargetResolutionModeReview,
	}
	policy.RequireCapabilityLease = true
	policy.MaxCapabilityLeases = 16
	policy.MaxCapabilityLeaseMillis = 5_000
	policy.AdaptiveTargetThreshold = 80
	policy.MaxActions = 16
	policy.MaxQueries = 64
	policy.MaxObservations = 64
	return policy
}

func flowPlannerRequest(observationID, name string) VerifiedFlowPlanRequest {
	target := targetSpec(name)
	target.RequiredStates = []UIState{UIStateEnabled}
	target.RequiredActions = []UIAction{UIActionPress}
	return VerifiedFlowPlanRequest{
		SchemaVersion: VerifiedFlowPlanSchemaVersion, CatalogVersion: CatalogSchemaVersion,
		TargetSpecVersion: TargetSpecSchemaVersion, CapabilityLeaseVersion: CapabilityLeaseSchemaVersion,
		ActionProofVersion: ActionProofSchemaVersion, TraceVersion: RobotGoTraceSchemaVersion,
		Steps: []VerifiedFlowPlanStep{{
			ObservationID: observationID, Target: target, Mode: TargetResolutionModeStrict,
			Action: UIActionPress, CapabilityLeaseDurationMillis: 1_000,
			Postcondition: &UIElementCondition{Kind: UIElementConditionStatePresent, State: UIStateChecked},
			Trace:         &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyMetadataOnly},
		}},
	}
}

func inspectFlowPlannerFixture(t *testing.T, policy Policy, snapshot uiBackendSnapshot) (*Session, *semanticFakeDriver, VerifiedFlowPlanRequest) {
	t.Helper()
	targetName := snapshot.Nodes[1].Name
	session, driver := newSemanticSession(t, policy, snapshot)
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	return session, driver, flowPlannerRequest(observation.ObservationID, targetName)
}

func TestVerifiedFlowPlanIsDeterministicAdvisoryAndSideEffectFree(t *testing.T) {
	const privateName = "private-flow-name-sentinel"
	snapshot := semanticSnapshot()
	snapshot.Nodes[1].Name = privateName
	session, driver, request := inspectFlowPlannerFixture(t, flowPlannerPolicy(), snapshot)
	planNow := session.now()
	session.now = func() time.Time { return planNow }
	before := flowPlannerSessionState(session)
	driverCalls, actionCalls, checkCalls := driver.calls, driver.actCalls, driver.checkCalls

	first, err := session.PlanVerifiedFlow(t.Context(), request)
	if err != nil {
		t.Fatalf("%v: %v", err, errors.Unwrap(err))
	}
	second, err := session.PlanVerifiedFlow(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic plan:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.SchemaVersion != VerifiedFlowPlanSchemaVersion || first.CatalogVersion != CatalogSchemaVersion ||
		!first.AdvisoryOnly || first.Status != FlowFeasible || len(first.Steps) != 1 ||
		first.Steps[0].Status != FlowFeasible || first.Steps[0].CandidateCount != 1 {
		t.Fatalf("plan = %+v", first)
	}
	wantQuota := FlowQuotaRequirement{Actions: 1, Queries: 7, Observations: 7, Leases: 1}
	if first.Requirements.Quota != wantQuota || first.Steps[0].Requirements.Quota != wantQuota ||
		!first.Requirements.CapabilityLeaseRequired || !first.Requirements.RuntimeRevalidationRequired ||
		!slices.Contains(first.Requirements.Cleanup, FlowCleanupCapabilityLease) ||
		!slices.Contains(first.Requirements.Assumptions, FlowAssumptionSessionRemainsOpen) ||
		!slices.Contains(first.Requirements.Assumptions, FlowAssumptionRateScheduleHonored) {
		t.Fatalf("requirements = %+v", first.Requirements)
	}
	if driver.calls != driverCalls || driver.actCalls != actionCalls || driver.checkCalls != checkCalls ||
		flowPlannerSessionState(session) != before {
		t.Fatalf("planner changed runtime: calls=%d/%d/%d before=%+v after=%+v",
			driver.calls, driver.actCalls, driver.checkCalls, before, flowPlannerSessionState(session))
	}
	payload, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{privateName, request.Steps[0].ObservationID, "fixture", "native-button"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("plan leaked %q: %s", forbidden, payload)
		}
	}
}

func TestVerifiedFlowPlanEmitsNoAuditTraceOrRecorderEvents(t *testing.T) {
	policy := flowPlannerPolicy()
	policy.AllowTraceExport = true
	policy.AllowRecorder = true
	policy.MaxRecorderEvents = 16
	policy.MaxRecorderBytes = 16 << 10
	policy.RecorderLifetimeMillis = 5_000
	session, _, request := inspectFlowPlannerFixture(t, policy, semanticSnapshot())
	audit := &recordingAuditSink{}
	trace := &recordingTraceSink{}
	session.auditSink = audit
	session.traceSink = trace
	request.Steps[0].Trace.Export = true
	recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorder.Close() })

	report, err := session.PlanVerifiedFlow(t.Context(), request)
	if err != nil || report.Status != FlowFeasible {
		t.Fatalf("plan = %+v, %v", report, err)
	}
	audit.mu.Lock()
	auditEvents := len(audit.events)
	audit.mu.Unlock()
	trace.mu.Lock()
	traceEvents := len(trace.traces)
	trace.mu.Unlock()
	recorder.mu.Lock()
	recorderEvents := len(recorder.events)
	recorder.mu.Unlock()
	if auditEvents != 0 || traceEvents != 0 || recorderEvents != 0 {
		t.Fatalf("planner emitted side effects: audit=%d trace=%d recorder=%d", auditEvents, traceEvents, recorderEvents)
	}
}

type plannerSessionStateSnapshot struct {
	used, queries, observations, leases, views, analyses uint64
	leaseRecords, observationRecords, viewRecords        int
	lastAction, lastQuery, lastView, lastAnalysis        time.Time
}

func flowPlannerSessionState(session *Session) plannerSessionStateSnapshot {
	return plannerSessionStateSnapshot{
		used: session.used, queries: session.usedQueries, observations: session.usedObservations,
		leases: session.usedLeases, views: session.usedViews, analyses: session.usedAnalyses,
		leaseRecords: len(session.leases), observationRecords: len(session.observations), viewRecords: len(session.views),
		lastAction: session.lastAction, lastQuery: session.lastUIQuery,
		lastView: session.lastView, lastAnalysis: session.lastAnalysis,
	}
}

func TestVerifiedFlowPlanStatusMatrix(t *testing.T) {
	for name, setup := range map[string]func(*testing.T) (*Session, VerifiedFlowPlanRequest, FlowFeasibilityStatus, FlowPlanBlocker){
		"missing-observation": func(t *testing.T) (*Session, VerifiedFlowPlanRequest, FlowFeasibilityStatus, FlowPlanBlocker) {
			session, _ := newSemanticSession(t, flowPlannerPolicy(), semanticSnapshot())
			return session, flowPlannerRequest("", "Save"), FlowEvidenceRequired, FlowBlockerObservationRequired
		},
		"stale-observation": func(t *testing.T) (*Session, VerifiedFlowPlanRequest, FlowFeasibilityStatus, FlowPlanBlocker) {
			session, _, request := inspectFlowPlannerFixture(t, flowPlannerPolicy(), semanticSnapshot())
			if err := session.ReleaseObservation(request.Steps[0].ObservationID); err != nil {
				t.Fatal(err)
			}
			return session, request, FlowStale, FlowBlockerObservationStale
		},
		"ambiguous": func(t *testing.T) (*Session, VerifiedFlowPlanRequest, FlowFeasibilityStatus, FlowPlanBlocker) {
			snapshot := semanticSnapshot()
			duplicate := snapshot.Nodes[1]
			duplicate.StableID = []byte("another-private-reference")
			snapshot.Nodes = append(snapshot.Nodes, duplicate)
			session, _, request := inspectFlowPlannerFixture(t, flowPlannerPolicy(), snapshot)
			return session, request, FlowAmbiguous, FlowBlockerTargetAmbiguous
		},
		"quota-denied": func(t *testing.T) (*Session, VerifiedFlowPlanRequest, FlowFeasibilityStatus, FlowPlanBlocker) {
			session, _, request := inspectFlowPlannerFixture(t, flowPlannerPolicy(), semanticSnapshot())
			session.usedQueries = session.policy.MaxQueries
			return session, request, FlowPolicyDenied, FlowBlockerQuotaInsufficient
		},
		"confirmation": func(t *testing.T) (*Session, VerifiedFlowPlanRequest, FlowFeasibilityStatus, FlowPlanBlocker) {
			policy := flowPlannerPolicy()
			policy.ConfirmOperations = []Operation{OperationElementAct}
			session, _, request := inspectFlowPlannerFixture(t, policy, semanticSnapshot())
			return session, request, FlowConditionallyFeasible, FlowBlockerConfirmationRequired
		},
	} {
		t.Run(name, func(t *testing.T) {
			session, request, wantStatus, wantBlocker := setup(t)
			report, err := session.PlanVerifiedFlow(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if report.Status != wantStatus || len(report.Steps) != 1 ||
				report.Steps[0].Status != wantStatus || !slices.Contains(report.Steps[0].Blockers, wantBlocker) {
				t.Fatalf("status report = %+v", report)
			}
		})
	}
}

func TestVerifiedFlowPlanEnforcesResolverTargetPolicyBounds(t *testing.T) {
	t.Run("aggregate-name-bytes", func(t *testing.T) {
		policy := flowPlannerPolicy()
		policy.RequireCapabilityLease = false
		policy.AdaptiveTargetThreshold = 75
		session, _, request := inspectFlowPlannerFixture(t, policy, semanticSnapshot())
		request.Steps[0].Mode = TargetResolutionModeAdaptive
		request.Steps[0].Target.Name = strings.Repeat("x", int(policy.MaxUIStringBytes)+1)

		report, err := session.PlanVerifiedFlow(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if report.Status != FlowPolicyDenied || report.Steps[0].CandidateCount != 1 ||
			!slices.Contains(report.Steps[0].Blockers, FlowBlockerTargetBoundsDenied) {
			t.Fatalf("over-policy target plan = %+v", report)
		}
		if err := session.authorizeTargetResolution(ResolveUIRequest{
			Target: request.Steps[0].Target, Mode: TargetResolutionModeAdaptive,
			Lease: &CapabilityLeaseRequest{Action: UIActionPress, DurationMillis: 1_000},
		}); !hasErrorCode(err, ErrorPolicyDenied) {
			t.Fatalf("resolver policy parity error = %v", err)
		}
	})

	t.Run("ancestor-depth-without-observation", func(t *testing.T) {
		policy := flowPlannerPolicy()
		policy.RequireCapabilityLease = false
		session, _ := newSemanticSession(t, policy, semanticSnapshot())
		request := flowPlannerRequest("", "Save")
		request.Steps[0].CapabilityLeaseDurationMillis = 0
		for range int(policy.MaxUITreeDepth) + 1 {
			request.Steps[0].Target.Ancestors = append(request.Steps[0].Target.Ancestors,
				TargetAncestor{Role: UIRoleWindow, Name: "Fixture"})
		}

		report, err := session.PlanVerifiedFlow(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if report.Status != FlowPolicyDenied ||
			!slices.Contains(report.Steps[0].Blockers, FlowBlockerTargetBoundsDenied) ||
			!slices.Contains(report.Steps[0].Blockers, FlowBlockerObservationRequired) {
			t.Fatalf("over-depth target plan = %+v", report)
		}
		if err := session.authorizeTargetResolution(ResolveUIRequest{Target: request.Steps[0].Target}); !hasErrorCode(err, ErrorPolicyDenied) {
			t.Fatalf("resolver policy parity error = %v", err)
		}
	})
}

func TestVerifiedFlowPlanQuotaArithmeticAndAggregation(t *testing.T) {
	policy := flowPlannerPolicy()
	session, driver, request := inspectFlowPlannerFixture(t, policy, semanticSnapshot())
	request.Steps = append(request.Steps, request.Steps[0])
	before := flowPlannerSessionState(session)
	report, err := session.PlanVerifiedFlow(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := FlowQuotaRequirement{Actions: 2, Queries: 14, Observations: 14, Leases: 2}
	if report.Requirements.Quota != want || len(report.Steps) != 2 ||
		report.Steps[0].Requirements.Quota != (FlowQuotaRequirement{Actions: 1, Queries: 7, Observations: 7, Leases: 1}) ||
		report.Steps[1].Requirements.Quota != (FlowQuotaRequirement{Actions: 1, Queries: 7, Observations: 7, Leases: 1}) {
		t.Fatalf("aggregate quota = %+v steps=%+v", report.Requirements.Quota, report.Steps)
	}
	if flowPlannerSessionState(session) != before || driver.actCalls != 0 || driver.checkCalls != 0 {
		t.Fatalf("quota planning consumed state: before=%+v after=%+v driver=%+v", before, flowPlannerSessionState(session), driver)
	}

	request.Steps[1].ObservationID = ""
	report, err = session.PlanVerifiedFlow(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if report.Requirements.Quota.Queries != 15 || report.Requirements.Quota.Observations != 15 ||
		!report.Steps[1].Requirements.FreshObservationRequired {
		t.Fatalf("fresh-observation quota = %+v", report)
	}
}

func TestVerifiedFlowPlanReportsRateAndSessionTiming(t *testing.T) {
	t.Run("minimum-not-quota-upper-bound", func(t *testing.T) {
		policy := flowPlannerPolicy()
		policy.MinUIQueryIntervalMillis = 100
		policy.SessionTimeoutMillis = 500
		policy.TraceLifetimeMillis = 500
		session, _, request := inspectFlowPlannerFixture(t, policy, semanticSnapshot())
		report, err := session.PlanVerifiedFlow(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		step := report.Steps[0]
		if report.Status != FlowFeasible || step.Requirements.Quota.Queries != 7 ||
			step.Requirements.Timing.MinimumVerifiedFlowMillis >= step.Requirements.Timing.SessionRemainingMillis {
			t.Fatalf("minimum timing treated conservative quota as mandatory: %+v", report)
		}
	})

	t.Run("session-lifetime-insufficient", func(t *testing.T) {
		policy := flowPlannerPolicy()
		policy.MinUIQueryIntervalMillis = 1_000
		policy.SessionTimeoutMillis = 200
		policy.TraceLifetimeMillis = 200
		session, _, request := inspectFlowPlannerFixture(t, policy, semanticSnapshot())
		report, err := session.PlanVerifiedFlow(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		step := report.Steps[0]
		if report.Status != FlowPolicyDenied ||
			!slices.Contains(step.Blockers, FlowBlockerSessionTimeInsufficient) ||
			step.Requirements.Timing.MinimumVerifiedFlowMillis <= step.Requirements.Timing.SessionRemainingMillis {
			t.Fatalf("session timing plan = %+v", report)
		}
	})

	t.Run("current-action-rate-wait", func(t *testing.T) {
		policy := flowPlannerPolicy()
		policy.MinActionIntervalMillis = 50
		policy.SessionTimeoutMillis = 10_000
		session, _, request := inspectFlowPlannerFixture(t, policy, semanticSnapshot())
		now := session.now()
		session.lastAction = now
		session.now = func() time.Time { return now }
		report, err := session.PlanVerifiedFlow(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		step := report.Steps[0]
		if report.Status != FlowConditionallyFeasible ||
			!slices.Contains(step.Blockers, FlowBlockerRateWaitRequired) ||
			step.Requirements.Timing.ActionReadyDelayMillis != 50 {
			t.Fatalf("action-rate plan = %+v", report)
		}
		if err := session.validateActionRate(); !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("execution rate parity error = %v", err)
		}
	})
}

func TestVerifiedFlowPlanRejectsSchemaDriftAndInvalidDescriptions(t *testing.T) {
	session, driver, request := inspectFlowPlannerFixture(t, flowPlannerPolicy(), semanticSnapshot())
	baseline := flowPlannerSessionState(session)
	baselineCalls := driver.calls

	for name, mutate := range map[string]func(*VerifiedFlowPlanRequest){
		"plan-schema":    func(request *VerifiedFlowPlanRequest) { request.SchemaVersion = "99" },
		"catalog":        func(request *VerifiedFlowPlanRequest) { request.CatalogVersion = "99" },
		"target-version": func(request *VerifiedFlowPlanRequest) { request.TargetSpecVersion = "99" },
		"lease-version":  func(request *VerifiedFlowPlanRequest) { request.CapabilityLeaseVersion = "99" },
		"proof-version":  func(request *VerifiedFlowPlanRequest) { request.ActionProofVersion = "99" },
		"trace-version":  func(request *VerifiedFlowPlanRequest) { request.TraceVersion = "99" },
		"empty":          func(request *VerifiedFlowPlanRequest) { request.Steps = nil },
		"too-many-steps": func(request *VerifiedFlowPlanRequest) {
			step := request.Steps[0]
			request.Steps = make([]VerifiedFlowPlanStep, maxVerifiedFlowPlanSteps+1)
			for index := range request.Steps {
				request.Steps[index] = step
			}
		},
		"action": func(request *VerifiedFlowPlanRequest) { request.Steps[0].Action = "private-action-sentinel" },
		"condition": func(request *VerifiedFlowPlanRequest) {
			request.Steps[0].Postcondition = &UIElementCondition{Kind: "private-condition-sentinel"}
		},
		"trace-tier": func(request *VerifiedFlowPlanRequest) { request.Steps[0].Trace.Tier = "private-trace-sentinel" },
		"lease-bound": func(request *VerifiedFlowPlanRequest) {
			request.Steps[0].CapabilityLeaseDurationMillis = maxAgentCapabilityLeaseMillis + 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := request
			candidate.Steps = append([]VerifiedFlowPlanStep(nil), request.Steps...)
			trace := *request.Steps[0].Trace
			candidate.Steps[0].Trace = &trace
			condition := *request.Steps[0].Postcondition
			candidate.Steps[0].Postcondition = &condition
			mutate(&candidate)
			_, err := session.PlanVerifiedFlow(t.Context(), candidate)
			if !hasErrorCode(err, ErrorInvalidInput) {
				t.Fatalf("invalid plan error = %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private-") {
				t.Fatalf("invalid input leaked through error: %v", err)
			}
		})
	}
	if flowPlannerSessionState(session) != baseline || driver.calls != baselineCalls || driver.actCalls != 0 || driver.checkCalls != 0 {
		t.Fatalf("invalid plans touched runtime: state=%+v calls=%d/%d/%d", flowPlannerSessionState(session), driver.calls, driver.actCalls, driver.checkCalls)
	}
}

func TestVerifiedFlowPlanClosedAndCanceledSessionFailWithoutPartialReport(t *testing.T) {
	session, driver, request := inspectFlowPlannerFixture(t, flowPlannerPolicy(), semanticSnapshot())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := session.PlanVerifiedFlow(ctx, request)
	if !hasErrorCode(err, ErrorCanceled) || len(report.Steps) != 0 || !report.AdvisoryOnly {
		t.Fatalf("canceled plan = %+v, %v", report, err)
	}
	if closeErr := session.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	report, err = session.PlanVerifiedFlow(t.Context(), request)
	if !hasErrorCode(err, ErrorSessionClosed) || len(report.Steps) != 0 || driver.actCalls != 0 || driver.checkCalls != 0 {
		t.Fatalf("closed plan = %+v, %v calls=%d/%d", report, err, driver.actCalls, driver.checkCalls)
	}
}

func TestVerifiedFlowPlanRuntimeCapabilityAndPermissionMatrix(t *testing.T) {
	for name, feature := range map[string]robotgo.FeatureCapability{
		"unsupported": {Available: false, Backend: "", Reason: robotgo.ErrNotSupported.Error()},
		"permission":  {Available: false, Backend: "at-spi2", Reason: robotgo.ErrPermissionDenied.Error()},
	} {
		t.Run(name, func(t *testing.T) {
			policy, err := preparePolicy(flowPlannerPolicy())
			if err != nil {
				t.Fatal(err)
			}
			driver := &semanticFakeDriver{fakeDriver: &fakeDriver{}, snapshot: semanticSnapshot()}
			capabilities := availableCapabilities()
			capabilities.Accessibility = feature
			session, err := newSession(policy, driver, capabilities)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			report, err := session.PlanVerifiedFlow(t.Context(), flowPlannerRequest("", "Save"))
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := FlowUnsupported
			wantBlocker := FlowBlockerOperationUnavailable
			if name == "permission" {
				wantStatus = FlowEvidenceRequired
				wantBlocker = FlowBlockerPermissionRequired
			}
			if report.Status != wantStatus || !slices.Contains(report.Steps[0].Blockers, wantBlocker) ||
				driver.calls != 0 || driver.actCalls != 0 || driver.checkCalls != 0 {
				t.Fatalf("capability plan = %+v calls=%d/%d/%d", report, driver.calls, driver.actCalls, driver.checkCalls)
			}
		})
	}
}

func TestVerifiedFlowPlanReportsLeaseVerificationAndTraceRequirements(t *testing.T) {
	policy := flowPlannerPolicy()
	policy.ConfirmOperations = []Operation{OperationResolveUI, OperationElementAct}
	session, _, request := inspectFlowPlannerFixture(t, policy, semanticSnapshot())
	request.Steps[0].CapabilityLeaseDurationMillis = 0
	request.Steps[0].Postcondition = nil
	report, err := session.PlanVerifiedFlow(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	step := report.Steps[0]
	for _, blocker := range []FlowPlanBlocker{
		FlowBlockerLeaseRequired, FlowBlockerVerificationRequired, FlowBlockerConfirmationRequired,
	} {
		if !slices.Contains(step.Blockers, blocker) {
			t.Fatalf("missing blocker %q: %+v", blocker, step)
		}
	}
	if step.Status != FlowEvidenceRequired || !slices.Equal(step.Requirements.Confirmations,
		[]Operation{OperationElementAct, OperationResolveUI}) {
		t.Fatalf("requirements = %+v", step)
	}

	request.Steps[0].Postcondition = &UIElementCondition{Kind: UIElementConditionStatePresent, State: UIStateChecked}
	request.Steps[0].CapabilityLeaseDurationMillis = policy.MaxCapabilityLeaseMillis + 1
	report, err = session.PlanVerifiedFlow(t.Context(), request)
	if err != nil || report.Status != FlowPolicyDenied ||
		!slices.Contains(report.Steps[0].Blockers, FlowBlockerLeaseDurationDenied) {
		t.Fatalf("policy-unbounded lease = %+v, %v", report, err)
	}
}

func TestVerifiedFlowPlanUsesRetainedAnalysisEvidenceWithoutBackendCalls(t *testing.T) {
	session, driver, analyzer, request := flowPlannerEvidenceFixture(t)
	ocrEvidenceID := request.Steps[0].Target.Evidence[0].EvidenceID
	baselineState := flowPlannerSessionState(session)
	baselineInspect, baselineCapture, baselineAnalyze := driver.calls, driver.captureCalls, analyzer.calls

	report, err := session.PlanVerifiedFlow(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != FlowFeasible || report.Steps[0].CandidateCount != 1 ||
		!slices.Equal(report.Steps[0].Requirements.EvidenceSources, []TargetEvidenceSource{TargetEvidenceSourceOCR}) {
		t.Fatalf("evidence plan = %+v", report)
	}
	if flowPlannerSessionState(session) != baselineState || driver.calls != baselineInspect ||
		driver.captureCalls != baselineCapture || analyzer.calls != baselineAnalyze ||
		driver.actCalls != 0 || driver.checkCalls != 0 {
		t.Fatalf("planner called evidence/runtime backend: state=%+v calls=%d/%d/%d/%d",
			flowPlannerSessionState(session), driver.calls, driver.captureCalls, analyzer.calls, driver.actCalls)
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "private OCR text sentinel") || strings.Contains(string(payload), ocrEvidenceID) {
		t.Fatalf("evidence plan leaked payload: %s", payload)
	}

	actualNow := session.now()
	session.now = func() time.Time { return actualNow.Add(2 * time.Second) }
	report, err = session.PlanVerifiedFlow(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != FlowStale || !slices.Contains(report.Steps[0].Blockers, FlowBlockerEvidenceStale) {
		t.Fatalf("stale evidence plan = %+v", report)
	}
}

func flowPlannerEvidenceFixture(t *testing.T) (*Session, *semanticFakeDriver, *fakeOCRAnalyzer, VerifiedFlowPlanRequest) {
	t.Helper()
	policy := targetEvidencePolicy(TargetEvidenceSourceOCR)
	policy.MaxQueries = 64
	policy.MaxObservations = 64
	policy.UIVerificationAttempts = 3
	policy.UIVerificationTimeoutMillis = 250
	session, driver := targetEvidenceSession(t, policy)
	analyzer := &fakeOCRAnalyzer{boxes: []rawOCRBox{{
		text: []byte("private OCR text sentinel"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
	}}}
	installFakeOCR(session, analyzer)
	view := createTargetEvidenceView(t, session)
	ocr, err := session.OCR(t.Context(), OCRRequest{
		ObservationID: view.ObservationID, Region: targetEvidenceAnalysisRegion,
		Languages: []string{"eng"}, MinConfidence: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	ui := inspectTargetEvidenceUI(t, session)
	request := flowPlannerRequest(ui.ObservationID, "Save")
	request.Steps[0].Mode = TargetResolutionModeAdaptive
	request.Steps[0].Target = targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID)
	request.Steps[0].CapabilityLeaseDurationMillis = 500
	request.Steps[0].Trace = nil
	return session, driver, analyzer, request
}

func TestVerifiedFlowPlanUsesOneObservationSnapshotAcrossSteps(t *testing.T) {
	session, _, _, request := flowPlannerEvidenceFixture(t)
	request.Steps = append(request.Steps, request.Steps[0])
	now := session.now()
	started := make(chan struct{})
	resume := make(chan struct{})
	session.now = func() time.Time {
		close(started)
		<-resume
		return now
	}
	var report VerifiedFlowPlanReport
	var planErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		report, planErr = session.PlanVerifiedFlow(t.Context(), request)
	}()
	<-started
	if err := session.ReleaseObservation(request.Steps[0].ObservationID); err != nil {
		t.Fatal(err)
	}
	close(resume)
	<-done
	if planErr != nil {
		t.Fatal(planErr)
	}
	if len(report.Steps) != 2 || report.Steps[0].Status != report.Steps[1].Status ||
		report.Steps[0].Status != FlowStale {
		t.Fatalf("inconsistent flow snapshot: %+v", report)
	}
}

func TestVerifiedFlowPlanRechecksCancellationAfterSnapshot(t *testing.T) {
	for _, test := range []struct {
		name     string
		close    bool
		wantCode ErrorCode
	}{
		{name: "caller", wantCode: ErrorCanceled},
		{name: "session-close", close: true, wantCode: ErrorSessionClosed},
	} {
		t.Run(test.name, func(t *testing.T) {
			session, _, _, request := flowPlannerEvidenceFixture(t)
			now := session.now()
			started := make(chan struct{})
			resume := make(chan struct{})
			session.now = func() time.Time {
				close(started)
				<-resume
				return now
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var report VerifiedFlowPlanReport
			var planErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				report, planErr = session.PlanVerifiedFlow(ctx, request)
			}()
			<-started

			var closeDone chan struct{}
			if test.close {
				closeDone = make(chan struct{})
				go func() {
					defer close(closeDone)
					_ = session.Close()
				}()
				<-session.ctx.Done()
			} else {
				cancel()
			}
			close(resume)
			<-done
			if closeDone != nil {
				<-closeDone
			}
			if !hasErrorCode(planErr, test.wantCode) || len(report.Steps) != 0 {
				t.Fatalf("canceled plan = %+v, %v", report, planErr)
			}
		})
	}
}
