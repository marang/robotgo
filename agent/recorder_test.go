package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recorderPolicy() Policy {
	policy := tracePolicy(false, TracePrivacyMetadataOnly)
	policy.AllowedOperations = append(policy.AllowedOperations, OperationResolveUI)
	policy.AllowRecorder = true
	policy.MaxRecorderEvents = 32
	policy.MaxRecorderBytes = 64 << 10
	policy.RecorderLifetimeMillis = 5_000
	return policy
}

func TestRecorderPolicyIsDenyByDefaultAndRequiresAllBounds(t *testing.T) {
	denied, _ := newSemanticSession(t, tracePolicy(false, TracePrivacyMetadataOnly), semanticSnapshot())
	if recorder, err := denied.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion}); recorder != nil || !hasErrorCode(err, ErrorPolicyDenied) {
		t.Fatalf("default recorder = %+v, %v", recorder, err)
	}

	for name, mutate := range map[string]func(*Policy){
		"missing-events":        func(policy *Policy) { policy.MaxRecorderEvents = 0 },
		"events-above-maximum":  func(policy *Policy) { policy.MaxRecorderEvents = maxAgentRecorderEvents + 1 },
		"missing-bytes":         func(policy *Policy) { policy.MaxRecorderBytes = 0 },
		"bytes-below-minimum":   func(policy *Policy) { policy.MaxRecorderBytes = minAgentRecorderBytes - 1 },
		"bytes-above-maximum":   func(policy *Policy) { policy.MaxRecorderBytes = maxAgentRecorderBytes + 1 },
		"missing-lifetime":      func(policy *Policy) { policy.RecorderLifetimeMillis = 0 },
		"lifetime-over-session": func(policy *Policy) { policy.RecorderLifetimeMillis = policy.SessionTimeoutMillis + 1 },
		"missing-resolver": func(policy *Policy) {
			policy.AllowedOperations = slicesWithoutOperation(policy.AllowedOperations, OperationResolveUI)
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := recorderPolicy()
			mutate(&policy)
			if _, err := preparePolicy(policy); err == nil {
				t.Fatalf("invalid recorder policy accepted: %+v", policy)
			}
		})
	}

	policy := recorderPolicy()
	policy.AllowRecorder = false
	if _, err := preparePolicy(policy); err == nil {
		t.Fatal("recorder bounds without permission were accepted")
	}
}

func TestRecorderCatalogExposesConfiguredBounds(t *testing.T) {
	policy := recorderPolicy()
	session, _ := newSemanticSession(t, policy, semanticSnapshot())
	for _, capability := range session.Catalog().Operations {
		if capability.Operation != OperationElementAct {
			continue
		}
		if capability.RecorderSchemaVersion != SemanticRecorderSchemaVersion ||
			!capability.RecorderAllowed || capability.MaxRecorderEvents != policy.MaxRecorderEvents ||
			capability.MaxRecorderBytes != policy.MaxRecorderBytes ||
			capability.RecorderLifetimeMillis != policy.RecorderLifetimeMillis {
			t.Fatalf("recorder catalog capability = %+v", capability)
		}
		return
	}
	t.Fatal("element action capability missing")
}

func slicesWithoutOperation(source []Operation, remove Operation) []Operation {
	result := make([]Operation, 0, len(source))
	for _, operation := range source {
		if operation != remove {
			result = append(result, operation)
		}
	}
	return result
}

func TestRecorderCapturesVerifiedSemanticFlowAndReusesTarget(t *testing.T) {
	session, driver := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
	recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	spec := targetSpec("Save")
	spec.RequiredStates = []UIState{UIStateEnabled}
	spec.RequiredActions = []UIAction{UIActionPress}
	var resolved TargetResolutionResult
	for range 2 {
		resolved, err = session.ResolveUITarget(t.Context(), ResolveUIRequest{
			ObservationID: observation.ObservationID, Target: spec, Mode: TargetResolutionModeStrict,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	driver.checkResults = []uiBackendElementConditionResult{
		{CleanupComplete: true}, {Satisfied: true, CleanupComplete: true},
	}
	condition := &UIElementCondition{Kind: UIElementConditionStatePresent, State: UIStateChecked}
	result, err := session.ActUIElement(t.Context(), ElementActionRequest{
		ObservationID: resolved.ObservationID, ElementID: resolved.ElementID,
		Action: UIActionPress, Expected: *resolved.Expected, Postcondition: condition,
		Confirmed: true, Hint: &RecorderActionHint{Impact: RecorderActionReversible},
		Trace: &TraceRequest{SchemaVersion: TraceRequestSchemaVersion, Tier: TracePrivacyMetadataOnly},
	})
	if err != nil || result.Status != ActionSucceeded || result.Proof == nil ||
		result.Proof.Verification == nil || result.Proof.Verification.Status != ActionVerificationMatched {
		t.Fatalf("verified action = %+v, %v", result, err)
	}
	flow, err := recorder.Stop(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !flow.CleanupComplete || flow.Truncated || len(flow.Targets) != 1 || len(flow.Events) != 4 {
		t.Fatalf("recorded flow = %+v", flow)
	}
	if flow.Events[0].Kind != RecorderEventObservation || flow.Events[0].Observation == nil ||
		flow.Events[0].Observation.ElementCount != uint32(len(observation.Elements)) {
		t.Fatalf("observation event = %+v", flow.Events[0])
	}
	if flow.Events[0].ObservationKey != "source-1" || flow.Events[1].ObservationKey != "source-1" ||
		flow.Events[2].ObservationKey != "source-1" || flow.Events[3].ObservationKey != "source-1" ||
		flow.Events[1].TargetID != "target-1" || flow.Events[2].TargetID != "target-1" {
		t.Fatalf("target reuse missing: %+v", flow.Events)
	}
	action := flow.Events[3]
	if action.Kind != RecorderEventAction || !action.Executable || action.ReviewRequired ||
		action.TargetID != "target-1" || action.Trace == nil || action.Trace.SchemaVersion != RobotGoTraceSchemaVersion {
		t.Fatalf("recorded action = %+v", action)
	}

	first, err := flow.Generate(FlowGenerationRequest{PackageName: "recordedflow", FunctionName: "Run"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := flow.Generate(FlowGenerationRequest{PackageName: "recordedflow", FunctionName: "Run"})
	if err != nil || first != second {
		t.Fatalf("generation is not deterministic: %v", err)
	}
	for _, forbidden := range []string{"native-button-992", "native-window-991", "\"target\": 42", "capability_lease_id\": \""} {
		if strings.Contains(first.GoSource, forbidden) || strings.Contains(first.MCPFixture, forbidden) {
			t.Fatalf("generated artifacts contain forbidden native payload %q", forbidden)
		}
	}
	if !strings.Contains(first.GoSource, "observations map[string]string") ||
		!strings.Contains(first.GoSource, "windows map[string]agent.TargetWindowSpec") ||
		!strings.Contains(first.MCPFixture, "operator_observations.source-1") ||
		!strings.Contains(first.MCPFixture, "operator_windows.window-1") ||
		!strings.Contains(first.MCPFixture, "generated_policy_broadening\": false") {
		t.Fatalf("generated prerequisites/window placeholder missing:\n%s\n%s", first.GoSource, first.MCPFixture)
	}
}

func TestRecorderNeverRetainsActionValuesCoordinatesOrNativeReferences(t *testing.T) {
	session, _ := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
	recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	session.recordNonSemanticAction(ActionRequest{
		Operation: OperationMove, Move: &MoveAction{X: 8123, Y: 9456, DisplayID: 0},
	}, ActionResult{Operation: OperationMove, Status: ActionSucceeded}, nil)
	session.recordElementAction(ElementActionRequest{
		Action: UIActionSetValue, Value: "recorder-secret-value-sentinel",
		Hint: &RecorderActionHint{Impact: RecorderActionDestructive},
	}, "private-capability-sentinel", ActionResult{
		Operation: OperationElementAct, Status: ActionSucceeded,
		Trace: &RobotGoTrace{SchemaVersion: RobotGoTraceSchemaVersion, Tier: TracePrivacyMetadataOnly, TransactionID: "action-7"},
	}, nil)
	flow, err := recorder.Stop(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{
		"recorder-secret-value-sentinel", "private-capability-sentinel", "8123", "9456",
		"native-button-992", "native-window-991",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("recorded flow leaked %q: %s", forbidden, text)
		}
	}
	if len(flow.Events) != 2 || !flow.Events[0].ReviewRequired || flow.Events[0].Executable ||
		!containsRecorderReview(flow.Events[0].ReviewReasons, RecorderReviewCoordinateInput) ||
		!containsRecorderReview(flow.Events[1].ReviewReasons, RecorderReviewSecretInputOmitted) ||
		!containsRecorderReview(flow.Events[1].ReviewReasons, RecorderReviewDestructiveAction) {
		t.Fatalf("review-only redaction events = %+v", flow.Events)
	}
}

func containsRecorderReview(reasons []RecorderReviewReason, reason RecorderReviewReason) bool {
	for _, candidate := range reasons {
		if candidate == reason {
			return true
		}
	}
	return false
}

func TestRecorderBoundsLifecycleAndCleanup(t *testing.T) {
	t.Run("exclusive-and-bounded", func(t *testing.T) {
		policy := recorderPolicy()
		policy.MaxRecorderEvents = 1
		session, _ := newSemanticSession(t, policy, semanticSnapshot())
		recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
		if err != nil {
			t.Fatal(err)
		}
		if second, secondErr := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion}); second != nil || !hasErrorCode(secondErr, ErrorRecorderActive) {
			t.Fatalf("second recorder = %+v, %v", second, secondErr)
		}
		for range 2 {
			session.recordNonSemanticAction(ActionRequest{Operation: OperationClick}, ActionResult{Status: ActionSucceeded}, nil)
		}
		flow, stopErr := recorder.Stop(t.Context())
		if stopErr != nil || len(flow.Events) != 1 || !flow.Truncated || !flow.CleanupComplete {
			t.Fatalf("bounded flow = %+v, %v", flow, stopErr)
		}
		if len(recorder.events) != 0 || len(recorder.targets) != 0 || recorder.bindingByLease != nil {
			t.Fatalf("recorder temporary state retained: %+v", recorder)
		}
		if _, stopErr = recorder.Stop(t.Context()); !hasErrorCode(stopErr, ErrorRecorderStopped) {
			t.Fatalf("second stop = %v", stopErr)
		}
	})

	t.Run("canceled-stop-clears", func(t *testing.T) {
		session, _ := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
		recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
		if err != nil {
			t.Fatal(err)
		}
		session.recordNonSemanticAction(ActionRequest{Operation: OperationClick}, ActionResult{Status: ActionSucceeded}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, stopErr := recorder.Stop(ctx); !hasErrorCode(stopErr, ErrorCanceled) || len(recorder.events) != 0 {
			t.Fatalf("canceled stop = %v recorder=%+v", stopErr, recorder)
		}
	})

	t.Run("timed-out-stop-clears", func(t *testing.T) {
		session, _ := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
		recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
		if err != nil {
			t.Fatal(err)
		}
		session.recordNonSemanticAction(ActionRequest{Operation: OperationClick}, ActionResult{Status: ActionSucceeded}, nil)
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
		defer cancel()
		if _, stopErr := recorder.Stop(ctx); !hasErrorCode(stopErr, ErrorTimedOut) ||
			len(recorder.events) != 0 || session.activeRecorder() != nil {
			t.Fatalf("timed-out stop = %v recorder=%+v", stopErr, recorder)
		}
	})

	t.Run("recorder-close-clears", func(t *testing.T) {
		session, _ := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
		recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
		if err != nil {
			t.Fatal(err)
		}
		session.recordNonSemanticAction(ActionRequest{Operation: OperationClick}, ActionResult{Status: ActionSucceeded}, nil)
		if closeErr := recorder.Close(); closeErr != nil || len(recorder.events) != 0 || session.activeRecorder() != nil {
			t.Fatalf("recorder close = %v recorder=%+v", closeErr, recorder)
		}
	})

	t.Run("wall-clock-regression-is-clamped", func(t *testing.T) {
		session, _ := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
		recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
		if err != nil {
			t.Fatal(err)
		}
		started := recorder.startedAt
		session.now = func() time.Time { return started.Add(-time.Second) }
		flow, stopErr := recorder.Stop(t.Context())
		if stopErr != nil || !flow.FinishedAt.Equal(started) || flow.DurationMillis != 0 {
			t.Fatalf("clock-regressed flow = %+v, %v", flow, stopErr)
		}
	})

	t.Run("serialized-byte-bound", func(t *testing.T) {
		policy := recorderPolicy()
		policy.MaxRecorderBytes = minAgentRecorderBytes
		session, _ := newSemanticSession(t, policy, semanticSnapshot())
		recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
		if err != nil {
			t.Fatal(err)
		}
		for range policy.MaxRecorderEvents {
			session.recordNonSemanticAction(ActionRequest{Operation: OperationMove}, ActionResult{Status: ActionSucceeded}, nil)
		}
		flow, stopErr := recorder.Stop(t.Context())
		if stopErr != nil {
			t.Fatal(stopErr)
		}
		payload, marshalErr := json.Marshal(flow)
		if marshalErr != nil || uint64(len(payload)) > policy.MaxRecorderBytes || !flow.Truncated {
			t.Fatalf("serialized bound bytes=%d max=%d truncated=%t err=%v", len(payload), policy.MaxRecorderBytes, flow.Truncated, marshalErr)
		}
	})

	t.Run("lifetime-expiry-clears", func(t *testing.T) {
		policy := recorderPolicy()
		policy.RecorderLifetimeMillis = 10
		session, _ := newSemanticSession(t, policy, semanticSnapshot())
		recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
		if err != nil {
			t.Fatal(err)
		}
		session.recordNonSemanticAction(ActionRequest{Operation: OperationClick}, ActionResult{Status: ActionSucceeded}, nil)
		deadline := time.Now().Add(time.Second)
		for !recorderExpired(recorder) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if _, stopErr := recorder.Stop(t.Context()); !hasErrorCode(stopErr, ErrorRecorderExpired) ||
			len(recorder.events) != 0 || session.activeRecorder() != nil {
			t.Fatalf("expired stop = %v recorder=%+v", stopErr, recorder)
		}
	})

	t.Run("session-close-clears", func(t *testing.T) {
		session, _ := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
		recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
		if err != nil {
			t.Fatal(err)
		}
		session.recordNonSemanticAction(ActionRequest{Operation: OperationClick}, ActionResult{Status: ActionSucceeded}, nil)
		if closeErr := session.Close(); closeErr != nil || len(recorder.events) != 0 || session.activeRecorder() != nil {
			t.Fatalf("session close = %v recorder=%+v", closeErr, recorder)
		}
	})
}

func recorderExpired(recorder *SemanticRecorder) bool {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.expired
}

func TestRecordedFlowGoldenArtifactsAndCompileWithoutExecution(t *testing.T) {
	flow := goldenRecordedFlow()
	artifacts, err := flow.Generate(FlowGenerationRequest{PackageName: "recordedflow", FunctionName: "RunVerifiedFlow"})
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenFile(t, "testdata/recorder_flow.go.golden", artifacts.GoSource)
	assertGoldenFile(t, "testdata/recorder_flow.mcp.json.golden", artifacts.MCPFixture)

	directory := t.TempDir()
	moduleRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	goMod := "module robotgo-recorder-fixture\n\ngo 1.26\n\nrequire github.com/marang/robotgo v0.0.0\n\nreplace github.com/marang/robotgo => " + moduleRoot + "\n"
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "flow.go"), []byte(artifacts.GoSource), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "-mod=mod", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=go1.26.0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated flow did not compile: %v\n%s", err, output)
	}
}

func goldenRecordedFlow() RecordedFlow {
	started := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	condition := &UIElementCondition{Kind: UIElementConditionStatePresent, State: UIStateChecked}
	return RecordedFlow{
		SchemaVersion: RecordedFlowSchemaVersion, RecorderVersion: SemanticRecorderSchemaVersion,
		TargetSpecVersion: TargetSpecSchemaVersion, ActionProofVersion: ActionProofSchemaVersion,
		TraceVersion: RobotGoTraceSchemaVersion, StartedAt: started, FinishedAt: started.Add(time.Second),
		DurationMillis: 1000, CleanupComplete: true,
		Targets: []RecordedTarget{{
			ID: "target-1", WindowID: "window-1", SchemaVersion: TargetSpecSchemaVersion, Role: UIRoleButton, Name: "Save",
			RequiredStates: []UIState{UIStateEnabled}, RequiredActions: []UIAction{UIActionPress},
			Ancestors: []TargetAncestor{{Role: UIRoleWindow, Name: "Editor", RequiredStates: []UIState{UIStateEnabled}}},
		}},
		Events: []RecorderEvent{
			{Sequence: 1, Kind: RecorderEventObservation, Operation: OperationInspectUI, ObservationKey: "source-1",
				Observation: &RecorderObservationEvidence{SchemaVersion: UISchemaVersion, ElementCount: 2}},
			{Sequence: 2, Kind: RecorderEventResolution, Operation: OperationResolveUI, ObservationKey: "source-1", TargetID: "target-1",
				Resolution: &RecorderResolutionEvidence{SchemaVersion: TargetResolutionSchemaVersion,
					Strategy: TargetResolutionExactSemantic, Mode: TargetResolutionModeStrict, CandidateCount: 1}},
			{Sequence: 3, Kind: RecorderEventAction, Operation: OperationElementAct, ObservationKey: "source-1", TargetID: "target-1",
				Action: UIActionPress, Impact: RecorderActionReversible, Postcondition: condition, Executable: true,
				Outcome: &RecorderActionOutcome{Status: ActionSucceeded, ProofStatus: ActionProofVerified,
					ExecutionStatus: ActionExecutionDispatched, VerificationStatus: ActionVerificationMatched,
					CleanupComplete: true},
				Trace: &RecorderTraceLineage{SchemaVersion: RobotGoTraceSchemaVersion,
					Tier: TracePrivacyMetadataOnly, TransactionID: "action-1", Redacted: true, CleanupComplete: true}},
			{Sequence: 4, Kind: RecorderEventReview, Operation: OperationMove,
				ReviewRequired: true, ReviewReasons: []RecorderReviewReason{
					RecorderReviewUnsupportedAction, RecorderReviewCoordinateInput,
				}},
		},
	}
}

func assertGoldenFile(t *testing.T, path, got string) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\n--- generated ---\n%s", path, err, got)
	}
	wantText := strings.ReplaceAll(string(want), "\r\n", "\n")
	if got != wantText {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestRecordedFlowRejectsUnsupportedSchemasAndInvalidIdentifiers(t *testing.T) {
	flow := goldenRecordedFlow()
	for name, mutate := range map[string]func(*RecordedFlow){
		"flow-schema":        func(flow *RecordedFlow) { flow.SchemaVersion = "99" },
		"target-schema":      func(flow *RecordedFlow) { flow.TargetSpecVersion = "99" },
		"cleanup-incomplete": func(flow *RecordedFlow) { flow.CleanupComplete = false },
		"duration-mismatch":  func(flow *RecordedFlow) { flow.DurationMillis++ },
		"nonmonotonic":       func(flow *RecordedFlow) { flow.Events[0].Sequence = 9 },
		"unknown-target":     func(flow *RecordedFlow) { flow.Events[2].TargetID = "target-native-secret" },
		"source-injection":   func(flow *RecordedFlow) { flow.Events[2].ObservationKey = "source-1\nmalicious" },
		"review-injection": func(flow *RecordedFlow) {
			flow.Events[3].ReviewReasons[0] = "coordinate-input\n}\nfunc injected() {}"
		},
		"role-injection": func(flow *RecordedFlow) { flow.Targets[0].Role = "button\nmalicious" },
		"unproven-executable": func(flow *RecordedFlow) {
			flow.Events[2].Outcome.CleanupComplete = false
		},
		"incomplete-trace": func(flow *RecordedFlow) { flow.Events[2].Trace.Truncated = true },
		"missing-impact-review": func(flow *RecordedFlow) {
			flow.Events[2].Executable = false
			flow.Events[2].Impact = ""
			flow.Events[2].ReviewRequired = true
			flow.Events[2].ReviewReasons = []RecorderReviewReason{RecorderReviewUnverifiedOutcome}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := flow
			candidate.Targets = cloneRecordedTargets(flow.Targets)
			candidate.Events = cloneRecorderEvents(flow.Events)
			mutate(&candidate)
			if _, err := candidate.Generate(FlowGenerationRequest{PackageName: "flow", FunctionName: "Run"}); err == nil {
				t.Fatal("invalid flow was generated")
			}
		})
	}
	for name, request := range map[string]FlowGenerationRequest{
		"keyword package": {PackageName: "package", FunctionName: "Run"},
		"blank package":   {PackageName: "_", FunctionName: "Run"},
		"blank function":  {PackageName: "flow", FunctionName: "_"},
		"init function":   {PackageName: "flow", FunctionName: "init"},
		"main entrypoint": {PackageName: "main", FunctionName: "main"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := flow.Generate(request); err == nil || !hasErrorCode(err, ErrorInvalidInput) {
				t.Fatalf("invalid identifier = %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	policy := recorderPolicy()
	prepared, err := preparePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	session, err := newSession(prepared, &fakeDriver{}, availableCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if recorder, err := session.StartRecorder(ctx, RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion}); recorder != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start = %+v, %v", recorder, err)
	}
}

func TestGeneratedFlowReusesSemanticTargetsAndFormatsReviewOnlyFlows(t *testing.T) {
	flow := goldenRecordedFlow()
	secondAction := cloneRecorderEvent(flow.Events[2])
	secondAction.Sequence = 4
	review := cloneRecorderEvent(flow.Events[3])
	review.Sequence = 5
	flow.Events = append(flow.Events[:3], secondAction, review)
	artifacts, err := flow.Generate(FlowGenerationRequest{PackageName: "flow", FunctionName: "Run"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(artifacts.GoSource, "target1 := agent.TargetSpec") != 1 ||
		strings.Count(artifacts.GoSource, "Target:        target1") != 2 ||
		strings.Count(artifacts.MCPFixture, "\"target_id\": \"target-1\"") != 2 {
		t.Fatalf("semantic target was not reused:\n%s\n%s", artifacts.GoSource, artifacts.MCPFixture)
	}

	reviewOnly := goldenRecordedFlow()
	reviewOnly.Targets = nil
	reviewOnly.Events = []RecorderEvent{{
		Sequence: 1, Kind: RecorderEventReview, Operation: OperationMove,
		ReviewRequired: true, ReviewReasons: []RecorderReviewReason{RecorderReviewCoordinateInput},
	}}
	artifacts, err = reviewOnly.Generate(FlowGenerationRequest{PackageName: "flow", FunctionName: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(artifacts.GoSource, "\"errors\"") ||
		!strings.Contains(artifacts.GoSource, "REVIEW REQUIRED step 1: coordinate-input") ||
		strings.Contains(artifacts.MCPFixture, "robotgo_element_act") {
		t.Fatalf("review-only generation is not inert:\n%s\n%s", artifacts.GoSource, artifacts.MCPFixture)
	}
}

func TestRecorderSeparatesWindowsAndUpgradesLegacySemanticTargets(t *testing.T) {
	session, _ := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
	recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	for index, windowTarget := range []TargetWindowSpec{
		{Target: 42, Kind: WindowTargetProcess, ExpectedTitle: "first-private-title"},
		{Target: 43, Kind: WindowTargetProcess, ExpectedTitle: "second-private-title"},
	} {
		observationID := "observation-" + string(rune('1'+index))
		request := ResolveUIRequest{
			ObservationID: observationID,
			Target: TargetSpec{
				SchemaVersion: TargetSpecLegacySchemaVersion, Window: windowTarget,
				Role: UIRoleButton, Name: "Save",
			},
		}
		result := TargetResolutionResult{
			SchemaVersion: TargetResolutionSchemaVersion, ObservationID: observationID,
			Mode: TargetResolutionModeStrict, Strategy: TargetResolutionExactSemantic,
			CandidateCount: 1, ElementID: observationID + "-element-1",
		}
		session.recordTargetResolution(request, result, nil)
	}
	flow, err := recorder.Stop(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Targets) != 2 || flow.Targets[0].WindowID != "window-1" ||
		flow.Targets[1].WindowID != "window-2" ||
		flow.Targets[0].SchemaVersion != TargetSpecSchemaVersion ||
		flow.Targets[1].SchemaVersion != TargetSpecSchemaVersion {
		t.Fatalf("window-scoped target upgrade = %+v", flow.Targets)
	}
	payload, _ := json.Marshal(flow)
	if strings.Contains(string(payload), "first-private-title") ||
		strings.Contains(string(payload), "second-private-title") || strings.Contains(string(payload), "\"target\":42") {
		t.Fatalf("native window identity leaked: %s", payload)
	}
	for index, target := range flow.Targets {
		flow.Events = append(flow.Events, RecorderEvent{
			Sequence: uint32(len(flow.Events) + 1), Kind: RecorderEventAction,
			Operation: OperationElementAct, ObservationKey: "source-" + string(rune('1'+index)),
			TargetID: target.ID, Action: UIActionPress, Impact: RecorderActionReversible,
			Postcondition: &UIElementCondition{Kind: UIElementConditionFocused}, Executable: true,
			Outcome: &RecorderActionOutcome{Status: ActionSucceeded, ProofStatus: ActionProofVerified,
				ExecutionStatus: ActionExecutionDispatched, VerificationStatus: ActionVerificationMatched,
				CleanupComplete: true},
			Trace: &RecorderTraceLineage{SchemaVersion: RobotGoTraceSchemaVersion,
				Tier: TracePrivacyMetadataOnly, TransactionID: "action-" + string(rune('1'+index)), CleanupComplete: true},
		})
	}
	artifacts, err := flow.Generate(FlowGenerationRequest{PackageName: "flow", FunctionName: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(artifacts.MCPFixture, "window-1") || !strings.Contains(artifacts.MCPFixture, "window-2") {
		t.Fatalf("window placeholders missing: %s", artifacts.MCPFixture)
	}
}

func TestTruncatedRecordedFlowGeneratesReviewOnlyArtifacts(t *testing.T) {
	flow := goldenRecordedFlow()
	flow.Truncated = true
	artifacts, err := flow.Generate(FlowGenerationRequest{PackageName: "flow", FunctionName: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(artifacts.GoSource, "session.ActUIElement(ctx") ||
		strings.Contains(artifacts.MCPFixture, "robotgo_element_act") ||
		!strings.Contains(artifacts.GoSource, string(RecorderReviewTruncatedFlow)) ||
		!strings.Contains(artifacts.MCPFixture, string(RecorderReviewTruncatedFlow)) {
		t.Fatalf("truncated flow remained executable:\n%s\n%s", artifacts.GoSource, artifacts.MCPFixture)
	}
}

func TestRecorderMarksLocatorPatchesVisualEvidenceAndMissingProofForReview(t *testing.T) {
	request := ResolveUIRequest{
		Target: TargetSpec{Evidence: []TargetEvidenceClause{{Source: TargetEvidenceSourceOCR}}},
		Mode:   TargetResolutionModeReview,
	}
	result := TargetResolutionResult{
		CandidateCount:  1,
		Changed:         []TargetEvidence{TargetEvidenceName},
		EvidenceSources: []TargetEvidenceSource{TargetEvidenceSourceOCR},
		Patch:           &TargetPatchProposal{},
	}
	reasons := resolutionReviewReasons(request, result, nil)
	if !containsRecorderReview(reasons, RecorderReviewLocatorPatch) ||
		!containsRecorderReview(reasons, RecorderReviewVisualEvidence) {
		t.Fatalf("review evidence reasons = %v", reasons)
	}

	session, _ := newSemanticSession(t, recorderPolicy(), semanticSnapshot())
	recorder, err := session.StartRecorder(t.Context(), RecorderRequest{SchemaVersion: SemanticRecorderSchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	session.recordElementAction(ElementActionRequest{Action: UIActionPress}, "", ActionResult{
		Operation: OperationElementAct, Status: ActionUnverified,
	}, errors.New("private backend detail"))
	flow, err := recorder.Stop(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Events) != 1 || !containsRecorderReview(flow.Events[0].ReviewReasons, RecorderReviewMissingPostcondition) ||
		!containsRecorderReview(flow.Events[0].ReviewReasons, RecorderReviewUnverifiedOutcome) ||
		!containsRecorderReview(flow.Events[0].ReviewReasons, RecorderReviewMissingTrace) ||
		!containsRecorderReview(flow.Events[0].ReviewReasons, RecorderReviewUnknownImpact) {
		t.Fatalf("missing proof review = %+v", flow.Events)
	}
	payload, _ := json.Marshal(flow)
	if strings.Contains(string(payload), "private backend detail") {
		t.Fatalf("raw backend error leaked: %s", payload)
	}
}
