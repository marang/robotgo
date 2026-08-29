package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

type blockingResolutionAuditSink struct {
	mu      sync.Mutex
	events  []AuditEvent
	started chan struct{}
	release chan struct{}
}

func (sink *blockingResolutionAuditSink) Record(_ context.Context, event AuditEvent) error {
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	sink.mu.Unlock()
	if event.Kind == AuditResolutionStarted {
		close(sink.started)
		<-sink.release
	}
	return nil
}

func semanticResolverPolicy() Policy {
	policy := semanticPolicy()
	policy.AllowedOperations = append(policy.AllowedOperations, OperationResolveUI)
	policy.AllowedUIRoles = append(policy.AllowedUIRoles, UIRoleGroup)
	return policy
}

func targetSpec(name string) TargetSpec {
	return TargetSpec{
		SchemaVersion: TargetSpecSchemaVersion,
		Window: TargetWindowSpec{
			Target: 42, Kind: WindowTargetProcess, ExpectedTitle: "fixture",
		},
		Role: UIRoleButton, Name: name,
	}
}

func resolverSnapshot() uiBackendSnapshot {
	snapshot := semanticSnapshot()
	snapshot.Nodes = append([]uiBackendNode(nil), snapshot.Nodes[:3]...)
	return snapshot
}

func inspectResolverFixture(
	t *testing.T,
	policy Policy,
	snapshot uiBackendSnapshot,
) (*Session, *semanticFakeDriver, UIObservation) {
	t.Helper()
	session, driver := newSemanticSession(t, policy, snapshot)
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{
		Target: 42, Kind: WindowTargetProcess,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session, driver, observation
}

func TestResolveUITargetExactIsDeterministicAndQuotaFree(t *testing.T) {
	session, driver, observation := inspectResolverFixture(t, semanticResolverPolicy(), semanticSnapshot())
	usedQueries, usedObservations, usedActions := session.usedQueries, session.usedObservations, session.used
	spec := targetSpec("Save")
	spec.RequiredStates = []UIState{UIStateEnabled}
	spec.RequiredActions = []UIAction{UIActionPress}

	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID,
		Target:        spec,
	})
	if err != nil {
		t.Fatalf("resolve: %v observation=%+v record=%+v", err, observation, session.observations[observation.ObservationID])
	}
	wantEvidence := []TargetEvidence{
		TargetEvidenceWindowIdentity, TargetEvidenceRole, TargetEvidenceName,
		TargetEvidenceStates, TargetEvidenceActions,
	}
	if !observation.Truncated || result.SchemaVersion != TargetResolutionSchemaVersion ||
		result.ObservationID != observation.ObservationID ||
		result.Strategy != TargetResolutionExactSemantic ||
		!slices.Equal(result.MatchedBy, wantEvidence) || result.CandidateCount != 1 ||
		result.RejectedCandidateCount != 2 || result.Ambiguous ||
		result.ElementID != observation.Elements[1].ElementID || result.Expected == nil ||
		result.Expected.Name != "Save" || result.Expected.Role != UIRoleButton ||
		!slices.Equal(result.Expected.Actions, []UIAction{UIActionPress}) {
		t.Fatalf("resolution = %+v", result)
	}
	if driver.calls != 1 || driver.actCalls != 0 || driver.checkCalls != 0 ||
		session.usedQueries != usedQueries || session.usedObservations != usedObservations || session.used != usedActions {
		t.Fatalf("resolver touched runtime or quota: calls=%d/%d/%d quota=%d/%d/%d",
			driver.calls, driver.actCalls, driver.checkCalls,
			session.usedQueries, session.usedObservations, session.used)
	}

	result.Expected.Actions[0] = UIActionToggle
	result.Expected.States[0] = UIStateDisabled
	result.MatchedBy[0] = "mutated"
	second, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID,
		Target:        spec,
	})
	if err != nil || second.Expected == nil || second.Expected.Actions[0] != UIActionPress ||
		second.Expected.States[0] != UIStateEnabled || second.MatchedBy[0] != TargetEvidenceWindowIdentity {
		t.Fatalf("result mutation leaked into retained observation: %+v, %v", second, err)
	}
}

func TestResolveUITargetMatchesImmediateAndOrderedAncestorChain(t *testing.T) {
	snapshot := uiBackendSnapshot{
		Backend: "fake-accessibility",
		Nodes: []uiBackendNode{
			{
				StableID: []byte("window"), Parent: -1, Depth: 0,
				Role: UIRoleWindow, Name: "Fixture", States: []UIState{UIStateEnabled},
				Bounds: &UIBounds{Width: 600, Height: 400},
			},
			{
				StableID: []byte("toolbar"), Parent: 0, Depth: 1,
				Role: UIRoleGroup, Name: "Toolbar", States: []UIState{UIStateEnabled},
				Bounds: &UIBounds{Width: 600, Height: 80},
			},
			{
				StableID: []byte("file-group"), Parent: 1, Depth: 2,
				Role: UIRoleGroup, Name: "File", States: []UIState{UIStateEnabled, UIStateExpanded},
				Bounds: &UIBounds{Width: 200, Height: 80},
			},
			{
				StableID: []byte("save"), Parent: 2, Depth: 3,
				Role: UIRoleButton, Name: "Save", States: []UIState{UIStateEnabled},
				Actions: []UIAction{UIActionFocus, UIActionPress},
				Bounds:  &UIBounds{X: 10, Y: 10, Width: 80, Height: 30},
			},
		},
	}
	session, _, observation := inspectResolverFixture(t, semanticResolverPolicy(), snapshot)
	spec := targetSpec("Save")
	spec.RequiredActions = []UIAction{UIActionPress}
	spec.Ancestors = []TargetAncestor{
		{Role: UIRoleGroup, Name: "File", RequiredStates: []UIState{UIStateExpanded}},
		{Role: UIRoleGroup, Name: "Toolbar", RequiredStates: []UIState{UIStateEnabled}},
		{Role: UIRoleWindow, Name: "Fixture"},
	}
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: spec,
	})
	if err != nil || result.Strategy != TargetResolutionStructuralSemantic ||
		result.ElementID != observation.Elements[3].ElementID ||
		!slices.Equal(result.MatchedBy, []TargetEvidence{
			TargetEvidenceWindowIdentity, TargetEvidenceRole, TargetEvidenceName,
			TargetEvidenceActions, TargetEvidenceAncestors,
		}) {
		t.Fatalf("structural resolution = %+v, %v", result, err)
	}

	spec.Ancestors[0], spec.Ancestors[1] = spec.Ancestors[1], spec.Ancestors[0]
	missing, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: spec,
	})
	if !hasErrorCode(err, ErrorTargetNotFound) || missing.ElementID != "" ||
		missing.Expected != nil || missing.CandidateCount != 0 || missing.RejectedCandidateCount != 4 {
		t.Fatalf("skipped/reordered ancestor chain = %+v, %v", missing, err)
	}
}

func TestResolveUITargetRejectsUniqueMatchWhenAnotherCandidateHierarchyIsFiltered(t *testing.T) {
	snapshot := uiBackendSnapshot{
		Backend: "fake-accessibility",
		Nodes: []uiBackendNode{
			{
				StableID: []byte("window"), Parent: -1, Depth: 0,
				Role: UIRoleWindow, Name: "Fixture", States: []UIState{UIStateEnabled},
				Bounds: &UIBounds{Width: 600, Height: 400},
			},
			{
				StableID: []byte("visible-toolbar"), Parent: 0, Depth: 1,
				Role: UIRoleGroup, Name: "Toolbar", States: []UIState{UIStateEnabled},
				Bounds: &UIBounds{Width: 300, Height: 80},
			},
			{
				StableID: []byte("visible-save"), Parent: 1, Depth: 2,
				Role: UIRoleButton, Name: "Save", States: []UIState{UIStateEnabled},
				Actions: []UIAction{UIActionPress},
				Bounds:  &UIBounds{X: 10, Y: 10, Width: 80, Height: 30},
			},
			{
				StableID: []byte("hidden-toolbar"), Parent: 0, Depth: 1, Hidden: true,
				Role: UIRoleGroup, Name: "Toolbar", States: []UIState{UIStateEnabled},
				Bounds: &UIBounds{X: 300, Width: 300, Height: 80},
			},
			{
				StableID: []byte("unproven-save"), Parent: 3, Depth: 2,
				Role: UIRoleButton, Name: "Save", States: []UIState{UIStateEnabled},
				Actions: []UIAction{UIActionPress},
				Bounds:  &UIBounds{X: 310, Y: 10, Width: 80, Height: 30},
			},
		},
	}
	session, _, observation := inspectResolverFixture(t, semanticResolverPolicy(), snapshot)
	spec := targetSpec("Save")
	spec.Ancestors = []TargetAncestor{{Role: UIRoleGroup, Name: "Toolbar"}}
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: spec,
	})
	if !hasErrorCode(err, ErrorIncompleteObservation) || !errors.Is(err, ErrIncompleteObservation) ||
		result.CandidateCount != 1 || result.RejectedCandidateCount != 3 ||
		result.ElementID != "" || result.Expected != nil || result.Ambiguous {
		t.Fatalf("filtered candidate hierarchy = %+v, %v", result, err)
	}
}

func TestResolveUITargetFailsClosedOnAmbiguityAndMissingTarget(t *testing.T) {
	snapshot := semanticSnapshot()
	duplicate := snapshot.Nodes[1]
	duplicate.StableID = []byte("native-button-duplicate")
	duplicate.Bounds = &UIBounds{X: 120, Y: 40, Width: 80, Height: 30}
	snapshot.Nodes = append(snapshot.Nodes, duplicate)
	session, _, observation := inspectResolverFixture(t, semanticResolverPolicy(), snapshot)

	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: targetSpec("Save"),
	})
	if !hasErrorCode(err, ErrorAmbiguousTarget) || !errors.Is(err, ErrAmbiguousTarget) ||
		!result.Ambiguous || result.CandidateCount != 2 || result.RejectedCandidateCount != 2 ||
		result.ElementID != "" || result.Expected != nil || len(result.MatchedBy) != 3 {
		t.Fatalf("ambiguous resolution = %+v, %v", result, err)
	}

	result, err = session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: targetSpec("Missing"),
	})
	if !hasErrorCode(err, ErrorTargetNotFound) || !errors.Is(err, ErrTargetNotFound) ||
		result.Ambiguous || result.CandidateCount != 0 || result.RejectedCandidateCount != 4 ||
		result.ElementID != "" || result.Expected != nil || len(result.MatchedBy) != 0 {
		t.Fatalf("missing resolution = %+v, %v", result, err)
	}
}

func TestResolveUITargetRejectsWrongWindowAndIncompleteObservation(t *testing.T) {
	for name, mutate := range map[string]func(*uiBackendSnapshot){
		"truncated":          func(snapshot *uiBackendSnapshot) { snapshot.Truncated = true },
		"identity-truncated": func(snapshot *uiBackendSnapshot) { snapshot.IdentityTruncated = true },
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := resolverSnapshot()
			mutate(&snapshot)
			session, _, observation := inspectResolverFixture(t, semanticResolverPolicy(), snapshot)
			result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
				ObservationID: observation.ObservationID, Target: targetSpec("Save"),
			})
			if !hasErrorCode(err, ErrorIncompleteObservation) || !errors.Is(err, ErrIncompleteObservation) ||
				result.ElementID != "" || result.Expected != nil || result.RejectedCandidateCount != 3 {
				t.Fatalf("incomplete resolution = %+v, %v", result, err)
			}
		})
	}

	session, _, observation := inspectResolverFixture(t, semanticResolverPolicy(), resolverSnapshot())
	spec := targetSpec("Save")
	spec.Window.ExpectedTitle = "another-private-title"
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: spec,
	})
	if !hasErrorCode(err, ErrorTargetNotFound) || result.ElementID != "" || result.RejectedCandidateCount != 3 ||
		strings.Contains(err.Error(), spec.Window.ExpectedTitle) {
		t.Fatalf("wrong-window resolution = %+v, %v", result, err)
	}
}

func TestResolveUITargetValidatesBeforeObservationReadAndEnforcesProperties(t *testing.T) {
	valid := ResolveUIRequest{ObservationID: "observation-1", Target: targetSpec("Save")}
	tests := map[string]func(*ResolveUIRequest){
		"schema":      func(request *ResolveUIRequest) { request.Target.SchemaVersion = "2" },
		"observation": func(request *ResolveUIRequest) { request.ObservationID = "private-caller-value" },
		"window":      func(request *ResolveUIRequest) { request.Target.Window.Target = 0 },
		"role":        func(request *ResolveUIRequest) { request.Target.Role = "unknown" },
		"name":        func(request *ResolveUIRequest) { request.Target.Name = "" },
		"control":     func(request *ResolveUIRequest) { request.Target.Name = "bad\x00name" },
		"duplicate-state": func(request *ResolveUIRequest) {
			request.Target.RequiredStates = []UIState{UIStateEnabled, UIStateEnabled}
		},
		"duplicate-action": func(request *ResolveUIRequest) {
			request.Target.RequiredActions = []UIAction{UIActionPress, UIActionPress}
		},
		"ancestor": func(request *ResolveUIRequest) {
			request.Target.Ancestors = []TargetAncestor{{Role: UIRoleGroup}}
		},
	}
	policy, err := preparePolicy(semanticResolverPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{fakeDriver: &fakeDriver{}, snapshot: resolverSnapshot()}
	session, err := newSession(policy, driver, availableCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			result, resolveErr := session.ResolveUITarget(t.Context(), request)
			if !hasErrorCode(resolveErr, ErrorInvalidInput) || result.ObservationID != "" ||
				driver.calls != 0 || len(session.observations) != 0 {
				t.Fatalf("invalid resolution = %+v, %v calls=%d", result, resolveErr, driver.calls)
			}
		})
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	policyWithoutHierarchy := semanticResolverPolicy()
	policyWithoutHierarchy.AllowedUIProperties = slices.DeleteFunc(
		policyWithoutHierarchy.AllowedUIProperties,
		func(property UIProperty) bool { return property == UIPropertyHierarchy },
	)
	session, _, observation := inspectResolverFixture(t, policyWithoutHierarchy, resolverSnapshot())
	request := ResolveUIRequest{ObservationID: observation.ObservationID, Target: targetSpec("Save")}
	request.Target.Ancestors = []TargetAncestor{{Role: UIRoleWindow, Name: "Fixture"}}
	result, err := session.ResolveUITarget(t.Context(), request)
	if !hasErrorCode(err, ErrorPolicyDenied) || result.ElementID != "" {
		t.Fatalf("hierarchy policy resolution = %+v, %v", result, err)
	}
}

func TestResolveUITargetReleaseCloseAndAuditAreFailClosedAndPrivate(t *testing.T) {
	policy, err := preparePolicy(semanticResolverPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{fakeDriver: &fakeDriver{}, snapshot: resolverSnapshot()}
	sink := &recordingAuditSink{}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	sink.events = nil
	request := ResolveUIRequest{ObservationID: observation.ObservationID, Target: targetSpec("Save")}
	result, err := session.ResolveUITarget(t.Context(), request)
	if err != nil || result.Expected == nil || len(sink.events) != 2 ||
		sink.events[0].Kind != AuditResolutionStarted || sink.events[1].Kind != AuditResolutionFinished ||
		sink.events[1].TargetCandidateCount != 1 || sink.events[1].TargetRejectedCount != 2 ||
		len(sink.events[0].TargetMatchedBy) != 0 || len(sink.events[1].TargetMatchedBy) != 3 {
		t.Fatalf("resolver audit/result = %+v, %v events=%+v", result, err, sink.events)
	}
	serialized, err := json.Marshal(sink.events)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Save", "fixture", "correct horse", "native-button"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("resolver audit leaked %q: %s", forbidden, serialized)
		}
	}

	if err := session.ReleaseObservation(observation.ObservationID); err != nil {
		t.Fatal(err)
	}
	result, err = session.ResolveUITarget(t.Context(), request)
	if !hasErrorCode(err, ErrorTargetNotFound) || result.ElementID != "" || result.Expected != nil {
		t.Fatalf("released resolution = %+v, %v", result, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	result, err = session.ResolveUITarget(context.Background(), request)
	if !hasErrorCode(err, ErrorSessionClosed) || result.ElementID != "" || result.Expected != nil {
		t.Fatalf("closed resolution = %+v, %v", result, err)
	}
}

func TestResolveUITargetIntentAuditFailurePreventsObservationRead(t *testing.T) {
	policy, err := preparePolicy(semanticResolverPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{fakeDriver: &fakeDriver{}, snapshot: resolverSnapshot()}
	sink := &recordingAuditSink{failAt: 3}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	record := session.observations[observation.ObservationID]
	clearRetainedUITargets(record.uiTree)
	record.uiTree = nil
	session.observations[observation.ObservationID] = record
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: targetSpec("Save"),
	})
	if !hasErrorCode(err, ErrorAuditDelivery) || result.ElementID != "" || result.Expected != nil || len(sink.events) != 3 {
		t.Fatalf("intent-audit resolution = %+v, %v events=%+v", result, err, sink.events)
	}
}

func TestResolveUITargetCompletionAuditFailureClearsSelection(t *testing.T) {
	policy, err := preparePolicy(semanticResolverPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &semanticFakeDriver{fakeDriver: &fakeDriver{}, snapshot: resolverSnapshot()}
	sink := &recordingAuditSink{failAt: 4}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: observation.ObservationID, Target: targetSpec("Save"),
	})
	if !hasErrorCode(err, ErrorAuditDelivery) || result.ElementID != "" || result.Expected != nil ||
		result.CandidateCount != 1 || len(sink.events) != 4 {
		t.Fatalf("completion-audit resolution = %+v, %v events=%+v", result, err, sink.events)
	}
}

func TestResolveUITargetLinearizesReleaseAndCloseRacesWithoutResurrection(t *testing.T) {
	t.Run("release-before-read", func(t *testing.T) {
		policy, err := preparePolicy(semanticResolverPolicy())
		if err != nil {
			t.Fatal(err)
		}
		sink := &blockingResolutionAuditSink{started: make(chan struct{}), release: make(chan struct{})}
		driver := &semanticFakeDriver{fakeDriver: &fakeDriver{}, snapshot: resolverSnapshot()}
		session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
		if err != nil {
			t.Fatal(err)
		}
		type outcome struct {
			result TargetResolutionResult
			err    error
		}
		resolved := make(chan outcome, 1)
		go func() {
			result, resolveErr := session.ResolveUITarget(context.Background(), ResolveUIRequest{
				ObservationID: observation.ObservationID, Target: targetSpec("Save"),
			})
			resolved <- outcome{result: result, err: resolveErr}
		}()
		<-sink.started
		if err := session.ReleaseObservation(observation.ObservationID); err != nil {
			t.Fatal(err)
		}
		close(sink.release)
		got := <-resolved
		if !hasErrorCode(got.err, ErrorTargetNotFound) || got.result.ElementID != "" || got.result.Expected != nil {
			t.Fatalf("release race = %+v, %v", got.result, got.err)
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("close-before-read", func(t *testing.T) {
		policy, err := preparePolicy(semanticResolverPolicy())
		if err != nil {
			t.Fatal(err)
		}
		sink := &blockingResolutionAuditSink{started: make(chan struct{}), release: make(chan struct{})}
		driver := &semanticFakeDriver{fakeDriver: &fakeDriver{}, snapshot: resolverSnapshot()}
		session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
		if err != nil {
			t.Fatal(err)
		}
		type outcome struct {
			result TargetResolutionResult
			err    error
		}
		resolved := make(chan outcome, 1)
		go func() {
			result, resolveErr := session.ResolveUITarget(context.Background(), ResolveUIRequest{
				ObservationID: observation.ObservationID, Target: targetSpec("Save"),
			})
			resolved <- outcome{result: result, err: resolveErr}
		}()
		<-sink.started
		closed := make(chan error, 1)
		go func() { closed <- session.Close() }()
		<-session.ctx.Done()
		close(sink.release)
		got := <-resolved
		if !hasErrorCode(got.err, ErrorSessionClosed) || got.result.ElementID != "" || got.result.Expected != nil {
			t.Fatalf("close race = %+v, %v", got.result, got.err)
		}
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
	})
}

func TestResolveUIPolicyRequiresInspectAndMutationReadyProperties(t *testing.T) {
	tests := map[string]func(*Policy){
		"inspect": func(policy *Policy) {
			policy.AllowedOperations = []Operation{OperationResolveUI}
		},
		"role": func(policy *Policy) {
			policy.AllowedUIProperties = slices.DeleteFunc(policy.AllowedUIProperties, func(value UIProperty) bool {
				return value == UIPropertyRole
			})
		},
		"name": func(policy *Policy) {
			policy.AllowedUIProperties = slices.DeleteFunc(policy.AllowedUIProperties, func(value UIProperty) bool {
				return value == UIPropertyName
			})
		},
		"state": func(policy *Policy) {
			policy.AllowedUIProperties = slices.DeleteFunc(policy.AllowedUIProperties, func(value UIProperty) bool {
				return value == UIPropertyState
			})
		},
		"bounds": func(policy *Policy) {
			policy.AllowedUIProperties = slices.DeleteFunc(policy.AllowedUIProperties, func(value UIProperty) bool {
				return value == UIPropertyBounds
			})
		},
		"actions": func(policy *Policy) {
			policy.AllowedUIProperties = slices.DeleteFunc(policy.AllowedUIProperties, func(value UIProperty) bool {
				return value == UIPropertyActions
			})
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			policy := semanticResolverPolicy()
			mutate(&policy)
			if _, err := preparePolicy(policy); err == nil {
				t.Fatalf("invalid resolver policy accepted: %+v", policy)
			}
		})
	}
}

func TestResolveUICatalogPublishesDefensiveTargetSpecContract(t *testing.T) {
	session, _ := newSemanticSession(t, semanticResolverPolicy(), resolverSnapshot())
	var capability OperationCapability
	for _, candidate := range session.Catalog().Operations {
		if candidate.Operation == OperationResolveUI {
			capability = candidate
			break
		}
	}
	if capability.Operation != OperationResolveUI || !capability.Available || !capability.PolicyAllowed ||
		capability.ProcessGlobalBackend || capability.TargetSpecVersion != TargetSpecSchemaVersion ||
		capability.CapabilityLeaseVersion != CapabilityLeaseSchemaVersion ||
		!slices.Equal(capability.TargetResolutionModes, []TargetResolutionMode{TargetResolutionModeStrict}) ||
		capability.MaxTargetAncestors != semanticResolverPolicy().MaxUITreeDepth ||
		!slices.Equal(capability.TargetResolutionStrategies, allTargetResolutionStrategies) {
		t.Fatalf("resolver catalog capability = %+v", capability)
	}
	capability.TargetResolutionStrategies[0] = "mutated"
	capability.TargetResolutionModes[0] = "mutated"
	for _, candidate := range session.Catalog().Operations {
		if candidate.Operation == OperationResolveUI &&
			(candidate.TargetResolutionStrategies[0] != TargetResolutionExactSemantic ||
				candidate.TargetResolutionModes[0] != TargetResolutionModeStrict) {
			t.Fatalf("resolver strategy mutation leaked: %+v", candidate)
		}
	}
}
