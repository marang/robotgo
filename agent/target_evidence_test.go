package agent

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"
)

var targetEvidenceViewRegion = CaptureRegion{Width: 8, Height: 6, DisplayID: 0}
var targetEvidenceAnalysisRegion = CaptureRegion{X: 1, Y: 1, Width: 6, Height: 4, DisplayID: 0}

func targetEvidencePolicy(sources ...TargetEvidenceSource) Policy {
	policy := capabilityLeasePolicy()
	policy.AllowedOperations = append(policy.AllowedOperations, OperationView)
	policy.AllowedDisplayIDs = []int{0}
	policy.AllowedViewRegions = []CaptureRegion{targetEvidenceViewRegion}
	policy.MaxObservations = 8
	policy.MaxViewSourcePixels = 48
	policy.MaxViewEncodedBytes = 16 << 10
	policy.MaxViewWidth = 8
	policy.MaxViewHeight = 6
	policy.MaxViews = 4
	policy.MaxConcurrentViews = 1
	policy.MinViewIntervalMillis = 1
	policy.ViewTimeoutMillis = 1000
	policy.MaxAnalysisPixels = 48
	policy.MaxAnalyses = 4
	policy.MaxConcurrentAnalyses = 1
	policy.MinAnalysisIntervalMillis = 1
	policy.AnalysisTimeoutMillis = 1000
	policy.AllowedTargetEvidenceSources = append([]TargetEvidenceSource(nil), sources...)
	policy.AllowedTargetEvidenceRegions = []CaptureRegion{targetEvidenceAnalysisRegion}
	policy.MaxTargetEvidenceAgeMillis = 1000
	policy.AdaptiveTargetThreshold = 90
	for _, source := range sources {
		switch source {
		case TargetEvidenceSourceOCR:
			policy.AllowedOperations = append(policy.AllowedOperations, OperationOCR)
			policy.AllowedOCRLanguages = []string{"eng"}
			policy.MaxOCRBoxes = 4
			policy.MaxOCRTextBytes = 32
			policy.MinTargetOCRConfidence = 0.8
			policy.AllowedTargetEvidenceProviders = append(policy.AllowedTargetEvidenceProviders,
				TargetEvidenceProvider{Source: source, Backend: "fake-memory-ocr", Model: "fixture-v1"})
		case TargetEvidenceSourceVisual:
			policy.AllowedOperations = append(policy.AllowedOperations, OperationDetectElements)
			policy.MaxVisualElements = 4
			policy.MinTargetVisualConfidence = 0.8
			policy.AllowedTargetEvidenceProviders = append(policy.AllowedTargetEvidenceProviders,
				TargetEvidenceProvider{Source: source, Backend: VisualAnalysisBackend, Model: VisualAnalysisModel})
		}
	}
	return policy
}

func targetEvidenceSnapshot() uiBackendSnapshot {
	snapshot := resolverSnapshot()
	snapshot.Nodes[0].Bounds = &UIBounds{Width: 8, Height: 6}
	snapshot.Nodes[1].Name = "Store"
	snapshot.Nodes[1].Bounds = &UIBounds{X: 2, Y: 2, Width: 4, Height: 2}
	return snapshot
}

func targetEvidenceSession(t *testing.T, policy Policy) (*Session, *semanticFakeDriver) {
	t.Helper()
	session, driver := newSemanticSession(t, policy, targetEvidenceSnapshot())
	driver.captureImages = []image.Image{syntheticCapture(8, 6, 0)}
	return session, driver
}

func createTargetEvidenceView(t *testing.T, session *Session) ViewMetadata {
	t.Helper()
	view, err := session.View(t.Context(), ViewRequest{Region: &targetEvidenceViewRegion})
	if err != nil {
		t.Fatal(err)
	}
	metadata := view.Metadata
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	return metadata
}

func inspectTargetEvidenceUI(t *testing.T, session *Session) UIObservation {
	t.Helper()
	observation, err := session.InspectUI(t.Context(), InspectUIRequest{Target: 42, Kind: WindowTargetProcess})
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func targetEvidenceSpec(source TargetEvidenceSource, observationID, evidenceID string) TargetSpec {
	spec := targetSpec("Save")
	spec.RequiredStates = []UIState{UIStateEnabled}
	spec.RequiredActions = []UIAction{UIActionPress}
	spec.Evidence = []TargetEvidenceClause{{
		SchemaVersion: TargetEvidenceClauseSchemaVersion,
		ObservationID: observationID,
		EvidenceID:    evidenceID,
		Source:        source,
		ItemIndex:     0,
	}}
	return spec
}

func TestOCRTargetEvidenceResolvesWithoutRetainingTextAndDispatchesLease(t *testing.T) {
	session, driver := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
	sink := &recordingAuditSink{}
	session.auditSink = sink
	secret := "Save private account"
	analyzer := &fakeOCRAnalyzer{boxes: []rawOCRBox{{
		text: []byte(secret), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
	}}}
	installFakeOCR(session, analyzer)
	view := createTargetEvidenceView(t, session)
	ocr, err := session.OCR(t.Context(), OCRRequest{
		ObservationID: view.ObservationID, Region: targetEvidenceAnalysisRegion,
		Languages: []string{"eng"}, MinConfidence: 0.8,
	})
	if err != nil || ocr.Metadata.EvidenceID == "" || len(ocr.Boxes) != 1 {
		t.Fatalf("OCR evidence=%+v err=%v", ocr, err)
	}
	ui := inspectTargetEvidenceUI(t, session)
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: ui.ObservationID,
		Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
		Mode:          TargetResolutionModeAdaptive,
		Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
	})
	if err != nil || result.Strategy != TargetResolutionOCREvidence || result.Lease == nil ||
		result.AdaptiveScore != 100 || result.CandidateCount != 1 ||
		len(result.EvidenceSources) != 1 || result.EvidenceSources[0] != TargetEvidenceSourceOCR {
		t.Fatalf("OCR resolution=%+v err=%v", result, err)
	}
	serialized, err := json.Marshal(struct {
		Result TargetResolutionResult
		Audit  []AuditEvent
	}{Result: result, Audit: sink.events})
	if err != nil {
		t.Fatal(err)
	}
	if containsString(serialized, secret) || containsString([]byte(fmt.Sprintf("%#v", session.observations)), secret) {
		t.Fatalf("resolver retained or serialized OCR text: %s", serialized)
	}
	action, err := session.ActUIElement(t.Context(), ElementActionRequest{
		CapabilityLeaseID: result.Lease.ID, Action: UIActionPress,
	})
	if err != nil || action.Proof.Lease.Status != CapabilityLeaseConsumed || driver.actCalls != 1 {
		t.Fatalf("OCR leased action=%+v err=%v calls=%d", action, err, driver.actCalls)
	}
}

func TestCombinedTargetEvidenceUsesOneObservationAndFixedScores(t *testing.T) {
	policy := targetEvidencePolicy(TargetEvidenceSourceOCR, TargetEvidenceSourceVisual)
	session, driver := targetEvidenceSession(t, policy)
	source := syntheticCapture(8, 6, 240)
	for y := 2; y < 4; y++ {
		for x := 2; x < 4; x++ {
			source.SetRGBA(x, y, color.RGBA{A: 255})
		}
	}
	driver.captureImages = []image.Image{source}
	installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
		text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
	}}})
	view := createTargetEvidenceView(t, session)
	ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
		Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
	if err != nil {
		t.Fatal(err)
	}
	session.lastAnalysis = session.now().Add(-time.Second)
	visual, err := session.DetectVisualElements(t.Context(), VisualElementsRequest{
		ObservationID: view.ObservationID, Region: targetEvidenceAnalysisRegion, MinConfidence: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	ui := inspectTargetEvidenceUI(t, session)
	spec := targetSpec("Save")
	spec.RequiredStates = []UIState{UIStateEnabled}
	spec.RequiredActions = []UIAction{UIActionPress}
	spec.Evidence = []TargetEvidenceClause{
		{SchemaVersion: TargetEvidenceClauseSchemaVersion, ObservationID: view.ObservationID,
			EvidenceID: visual.Metadata.EvidenceID, Source: TargetEvidenceSourceVisual},
		{SchemaVersion: TargetEvidenceClauseSchemaVersion, ObservationID: view.ObservationID,
			EvidenceID: ocr.Metadata.EvidenceID, Source: TargetEvidenceSourceOCR},
	}
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: ui.ObservationID, Target: spec, Mode: TargetResolutionModeReview,
	})
	if err != nil || result.Strategy != TargetResolutionCombinedEvidence || result.Patch == nil ||
		result.AdaptiveScore != 100 || len(result.EvidenceSources) != 2 || len(result.EvidenceProviders) != 2 ||
		result.EvidenceSources[0] != TargetEvidenceSourceOCR || result.EvidenceSources[1] != TargetEvidenceSourceVisual {
		t.Fatalf("combined evidence=%+v err=%v", result, err)
	}
}

func TestTargetEvidenceLeaseIsInvalidatedWithImageObservation(t *testing.T) {
	session, driver := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
	installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
		text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
	}}})
	view := createTargetEvidenceView(t, session)
	ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
		Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
	if err != nil {
		t.Fatal(err)
	}
	ui := inspectTargetEvidenceUI(t, session)
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: ui.ObservationID,
		Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
		Mode:          TargetResolutionModeAdaptive,
		Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ReleaseObservation(view.ObservationID); err != nil {
		t.Fatal(err)
	}
	_, err = session.ActUIElement(t.Context(), ElementActionRequest{CapabilityLeaseID: result.Lease.ID, Action: UIActionPress})
	if !hasErrorCode(err, ErrorLeaseInvalid) || driver.actCalls != 0 {
		t.Fatalf("released image lease err=%v calls=%d", err, driver.actCalls)
	}
}

func TestTargetEvidenceCapsLeaseAtEvidenceExpiry(t *testing.T) {
	session, driver := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
	clock := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	session.now = func() time.Time { return clock }
	installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
		text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
	}}})
	view := createTargetEvidenceView(t, session)
	ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
		Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
	if err != nil {
		t.Fatal(err)
	}
	ui := inspectTargetEvidenceUI(t, session)
	clock = clock.Add(900 * time.Millisecond)
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: ui.ObservationID,
		Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
		Mode:          TargetResolutionModeAdaptive,
		Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
	})
	if err != nil || result.Lease == nil || result.Lease.ExpiresAt.Sub(clock) != 100*time.Millisecond {
		t.Fatalf("evidence-capped lease=%+v err=%v", result.Lease, err)
	}
	clock = clock.Add(101 * time.Millisecond)
	_, err = session.ActUIElement(t.Context(), ElementActionRequest{
		CapabilityLeaseID: result.Lease.ID, Action: UIActionPress,
	})
	if !hasErrorCode(err, ErrorLeaseExpired) || driver.actCalls != 0 {
		t.Fatalf("expired evidence lease err=%v calls=%d", err, driver.actCalls)
	}
}

func TestTargetEvidenceDoesNotPublishExpiredLease(t *testing.T) {
	session, _ := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
	clock := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	session.now = func() time.Time { return clock }
	installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
		text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
	}}})
	view := createTargetEvidenceView(t, session)
	ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
		Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
	if err != nil {
		t.Fatal(err)
	}
	ui := inspectTargetEvidenceUI(t, session)
	clock = clock.Add(time.Second)
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: ui.ObservationID,
		Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
		Mode:          TargetResolutionModeAdaptive,
		Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
	})
	if !hasErrorCode(err, ErrorStaleTarget) || result.Lease != nil {
		t.Fatalf("expired evidence lease=%+v err=%v", result.Lease, err)
	}
}

func TestVisualTargetEvidenceProducesReviewOnlyProposal(t *testing.T) {
	session, driver := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceVisual))
	source := syntheticCapture(8, 6, 240)
	for y := 2; y < 4; y++ {
		for x := 2; x < 4; x++ {
			source.SetRGBA(x, y, color.RGBA{A: 255})
		}
	}
	driver.captureImages = []image.Image{source}
	view := createTargetEvidenceView(t, session)
	visual, err := session.DetectVisualElements(t.Context(), VisualElementsRequest{
		ObservationID: view.ObservationID, Region: targetEvidenceAnalysisRegion, MinConfidence: 0.8,
	})
	if err != nil || visual.Metadata.EvidenceID == "" || len(visual.Elements) != 1 {
		t.Fatalf("visual evidence=%+v err=%v", visual, err)
	}
	ui := inspectTargetEvidenceUI(t, session)
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: ui.ObservationID,
		Target:        targetEvidenceSpec(TargetEvidenceSourceVisual, view.ObservationID, visual.Metadata.EvidenceID),
		Mode:          TargetResolutionModeReview,
	})
	if err != nil || result.Strategy != TargetResolutionVisualEvidence || result.Patch == nil ||
		result.Patch.Executable || result.Lease != nil || result.AdaptiveScore != 90 || driver.actCalls != 0 {
		t.Fatalf("visual review=%+v err=%v calls=%d", result, err, driver.actCalls)
	}
}

func TestTargetEvidenceThresholdsAndAmbiguityAreDeterministic(t *testing.T) {
	for _, test := range []struct {
		name       string
		confidence float64
		wantCode   ErrorCode
	}{
		{name: "exact-threshold", confidence: 0.8},
		{name: "below-threshold", confidence: 0.799, wantCode: ErrorTargetNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			session, _ := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
			installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
				text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: test.confidence,
			}}})
			view := createTargetEvidenceView(t, session)
			ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
				Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.7})
			if err != nil {
				t.Fatal(err)
			}
			ui := inspectTargetEvidenceUI(t, session)
			result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
				ObservationID: ui.ObservationID,
				Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
				Mode:          TargetResolutionModeAdaptive,
				Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
			})
			if test.wantCode == "" && (err != nil || result.Lease == nil || result.AdaptiveScore != 100) {
				t.Fatalf("threshold result=%+v err=%v", result, err)
			}
			if test.wantCode != "" && !hasErrorCode(err, test.wantCode) {
				t.Fatalf("threshold code=%s result=%+v err=%v", test.wantCode, result, err)
			}
		})
	}

	t.Run("ambiguous", func(t *testing.T) {
		policy := targetEvidencePolicy(TargetEvidenceSourceOCR)
		snapshot := targetEvidenceSnapshot()
		snapshot.Nodes = append(snapshot.Nodes, uiBackendNode{
			StableID: []byte("second-button"), Parent: 0, Depth: 1,
			Role: UIRoleButton, Name: "Submit", States: []UIState{UIStateEnabled},
			Actions: []UIAction{UIActionPress}, Bounds: &UIBounds{X: 2, Y: 2, Width: 4, Height: 2},
		})
		session, driver := newSemanticSession(t, policy, snapshot)
		driver.captureImages = []image.Image{syntheticCapture(8, 6, 0)}
		installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
			text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
		}}})
		view := createTargetEvidenceView(t, session)
		ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
			Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
		if err != nil {
			t.Fatal(err)
		}
		ui := inspectTargetEvidenceUI(t, session)
		result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
			ObservationID: ui.ObservationID,
			Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
			Mode:          TargetResolutionModeAdaptive,
			Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
		})
		if !hasErrorCode(err, ErrorAmbiguousTarget) || !result.Ambiguous ||
			result.CandidateCount != 2 || result.Lease != nil {
			t.Fatalf("evidence ambiguity=%+v err=%v", result, err)
		}
	})
}

func TestTargetEvidenceSemanticPrecedenceSkipsWeakerSource(t *testing.T) {
	policy := targetEvidencePolicy(TargetEvidenceSourceOCR)
	snapshot := targetEvidenceSnapshot()
	snapshot.Nodes[1].Name = "Save"
	session, _ := newSemanticSession(t, policy, snapshot)
	ui := inspectTargetEvidenceUI(t, session)
	spec := targetEvidenceSpec(TargetEvidenceSourceOCR, "observation-999", "target-evidence-999")
	result, err := session.ResolveUITarget(t.Context(), ResolveUIRequest{
		ObservationID: ui.ObservationID, Target: spec, Mode: TargetResolutionModeAdaptive,
		Lease: &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
	})
	if err != nil || result.Strategy != TargetResolutionAdaptiveSemantic || result.Lease == nil || len(result.EvidenceSources) != 0 {
		t.Fatalf("semantic precedence result=%+v err=%v", result, err)
	}
}

func TestTargetEvidencePolicyAndLineageFailuresAreFailClosed(t *testing.T) {
	for name, mutate := range map[string]func(*Policy){
		"missing-age":      func(policy *Policy) { policy.MaxTargetEvidenceAgeMillis = 0 },
		"missing-provider": func(policy *Policy) { policy.AllowedTargetEvidenceProviders = nil },
		"missing-region":   func(policy *Policy) { policy.AllowedTargetEvidenceRegions = nil },
		"missing-operation": func(policy *Policy) {
			policy.AllowedOperations = []Operation{OperationInspectUI, OperationResolveUI, OperationElementAct, OperationView}
		},
		"zero-confidence": func(policy *Policy) { policy.MinTargetOCRConfidence = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			policy := targetEvidencePolicy(TargetEvidenceSourceOCR)
			mutate(&policy)
			if _, err := preparePolicy(policy); err == nil {
				t.Fatalf("invalid target evidence policy accepted: %+v", policy)
			}
		})
	}

	t.Run("stale", func(t *testing.T) {
		session, _ := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
		now := time.Unix(1_700_000_000, 0)
		session.now = func() time.Time { return now }
		installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
			text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
		}}})
		view := createTargetEvidenceView(t, session)
		ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
			Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
		if err != nil {
			t.Fatal(err)
		}
		ui := inspectTargetEvidenceUI(t, session)
		now = now.Add(2 * time.Second)
		_, err = session.ResolveUITarget(t.Context(), ResolveUIRequest{
			ObservationID: ui.ObservationID,
			Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
			Mode:          TargetResolutionModeAdaptive,
			Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
		})
		if !hasErrorCode(err, ErrorStaleTarget) {
			t.Fatalf("stale evidence err=%v", err)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		session, _ := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
		installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
			text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9, truncated: true,
		}}})
		view := createTargetEvidenceView(t, session)
		ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
			Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
		if err != nil {
			t.Fatal(err)
		}
		ui := inspectTargetEvidenceUI(t, session)
		_, err = session.ResolveUITarget(t.Context(), ResolveUIRequest{
			ObservationID: ui.ObservationID,
			Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
			Mode:          TargetResolutionModeAdaptive,
			Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
		})
		if !hasErrorCode(err, ErrorIncompleteObservation) {
			t.Fatalf("truncated evidence err=%v", err)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(*Policy)
		box    image.Rectangle
	}{
		{name: "redacted", mutate: func(policy *Policy) {
			policy.ViewRedactionMasks = []CaptureRegion{{X: 2, Y: 2, Width: 1, Height: 1, DisplayID: 0}}
		}, box: image.Rect(1, 1, 3, 2)},
		{name: "downscaled", mutate: func(policy *Policy) {
			policy.MaxViewWidth = 4
			policy.MaxViewHeight = 3
		}, box: image.Rect(1, 1, 3, 2)},
		{name: "clipped", mutate: func(*Policy) {}, box: image.Rect(0, 1, 2, 2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := targetEvidencePolicy(TargetEvidenceSourceOCR)
			test.mutate(&policy)
			session, _ := targetEvidenceSession(t, policy)
			installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
				text: []byte("Save"), bounds: test.box, confidence: 0.9,
			}}})
			view := createTargetEvidenceView(t, session)
			ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
				Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
			if err != nil {
				t.Fatal(err)
			}
			ui := inspectTargetEvidenceUI(t, session)
			_, err = session.ResolveUITarget(t.Context(), ResolveUIRequest{
				ObservationID: ui.ObservationID,
				Target:        targetEvidenceSpec(TargetEvidenceSourceOCR, view.ObservationID, ocr.Metadata.EvidenceID),
				Mode:          TargetResolutionModeAdaptive,
				Lease:         &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
			})
			if !hasErrorCode(err, ErrorIncompleteObservation) {
				t.Fatalf("%s evidence err=%v", test.name, err)
			}
		})
	}

	t.Run("provider-denied", func(t *testing.T) {
		policy := targetEvidencePolicy(TargetEvidenceSourceOCR)
		policy.AllowedTargetEvidenceProviders[0].Model = "other-model"
		session, _ := targetEvidenceSession(t, policy)
		installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
			text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
		}}})
		view := createTargetEvidenceView(t, session)
		ocr, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
			Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
		if err != nil {
			t.Fatal(err)
		}
		record, ok := session.observation(view.ObservationID)
		if ocr.Metadata.EvidenceID != "" || !ok || len(record.targetEvidence) != 0 {
			t.Fatalf("denied provider published evidence: metadata=%+v retained=%+v", ocr.Metadata, record.targetEvidence)
		}
	})
}

func TestTargetEvidenceClauseValidationIsBoundedAndVersioned(t *testing.T) {
	base := ResolveUIRequest{
		ObservationID: "observation-1", Target: targetEvidenceSpec(TargetEvidenceSourceOCR, "observation-2", "target-evidence-1"),
		Mode:  TargetResolutionModeAdaptive,
		Lease: &CapabilityLeaseRequest{SchemaVersion: CapabilityLeaseSchemaVersion, Action: UIActionPress, DurationMillis: 500},
	}
	for name, mutate := range map[string]func(*ResolveUIRequest){
		"legacy-with-evidence": func(request *ResolveUIRequest) { request.Target.SchemaVersion = TargetSpecLegacySchemaVersion },
		"strict-with-evidence": func(request *ResolveUIRequest) { request.Mode = TargetResolutionModeStrict },
		"bad-evidence-id":      func(request *ResolveUIRequest) { request.Target.Evidence[0].EvidenceID = "caller-text" },
		"bad-schema":           func(request *ResolveUIRequest) { request.Target.Evidence[0].SchemaVersion = "2" },
		"duplicate-source": func(request *ResolveUIRequest) {
			request.Target.Evidence = append(request.Target.Evidence, request.Target.Evidence[0])
		},
		"multiple-observations": func(request *ResolveUIRequest) {
			second := request.Target.Evidence[0]
			second.Source = TargetEvidenceSourceVisual
			second.ObservationID = "observation-3"
			second.EvidenceID = "target-evidence-2"
			request.Target.Evidence = append(request.Target.Evidence, second)
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := base
			request.Target.Evidence = append([]TargetEvidenceClause(nil), base.Target.Evidence...)
			mutate(&request)
			if err := validateResolveUIRequest(request); err == nil {
				t.Fatalf("invalid evidence request accepted: %+v", request)
			}
		})
	}
	legacy := base
	legacy.Target.SchemaVersion = TargetSpecLegacySchemaVersion
	legacy.Target.Evidence = nil
	if err := validateResolveUIRequest(legacy); err != nil {
		t.Fatalf("semantic-only TargetSpec v1 changed behavior: %v", err)
	}
}

func TestTargetEvidenceCatalogExposesOnlyConfiguredBoundedContract(t *testing.T) {
	policy := targetEvidencePolicy(TargetEvidenceSourceVisual)
	session, _ := targetEvidenceSession(t, policy)
	for _, capability := range session.Catalog().Operations {
		if capability.Operation != OperationResolveUI {
			continue
		}
		if capability.TargetSpecVersion != TargetSpecSchemaVersion ||
			capability.TargetEvidenceClauseVersion != TargetEvidenceClauseSchemaVersion ||
			capability.MaxTargetEvidenceClauses != maxTargetEvidenceClauses ||
			capability.MaxTargetEvidenceAgeMillis != policy.MaxTargetEvidenceAgeMillis ||
			capability.MinTargetVisualConfidence != policy.MinTargetVisualConfidence ||
			len(capability.TargetEvidenceSources) != 1 ||
			capability.TargetEvidenceSources[0] != TargetEvidenceSourceVisual ||
			len(capability.TargetEvidenceProviders) != 1 ||
			capability.TargetEvidenceProviders[0].Backend != VisualAnalysisBackend {
			t.Fatalf("target evidence catalog=%+v", capability)
		}
		return
	}
	t.Fatal("resolver capability missing")
}

func TestTargetEvidencePublicationIsRemovedOnAuditFailure(t *testing.T) {
	session, _ := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
	installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
		text: []byte("Save"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
	}}})
	view := createTargetEvidenceView(t, session)
	session.auditSink = &recordingAuditSink{failAt: 2}
	result, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
		Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
	if !hasErrorCode(err, ErrorAuditDelivery) || result.Metadata.EvidenceID != "" {
		t.Fatalf("audit-failed evidence=%+v err=%v", result, err)
	}
	record, ok := session.observation(view.ObservationID)
	if !ok || len(record.targetEvidence) != 0 {
		t.Fatalf("audit failure retained target evidence: %+v", record.targetEvidence)
	}
	if err := session.ReleaseObservation(view.ObservationID); err != nil {
		t.Fatal(err)
	}
}

func TestTargetEvidenceIsClearedWithObservation(t *testing.T) {
	session, _ := targetEvidenceSession(t, targetEvidencePolicy(TargetEvidenceSourceOCR))
	installFakeOCR(session, &fakeOCRAnalyzer{boxes: []rawOCRBox{{
		text: []byte("private OCR"), bounds: image.Rect(1, 1, 3, 2), confidence: 0.9,
	}}})
	view := createTargetEvidenceView(t, session)
	result, err := session.OCR(t.Context(), OCRRequest{ObservationID: view.ObservationID,
		Region: targetEvidenceAnalysisRegion, Languages: []string{"eng"}, MinConfidence: 0.8})
	if err != nil || result.Metadata.EvidenceID == "" {
		t.Fatalf("evidence=%+v err=%v", result, err)
	}
	record, ok := session.observation(view.ObservationID)
	if !ok || len(record.targetEvidence) != 1 {
		t.Fatalf("retained evidence=%+v", record.targetEvidence)
	}
	store := record.targetEvidence
	capture := record.capture
	if err := session.ReleaseObservation(view.ObservationID); err != nil {
		t.Fatal(err)
	}
	if len(store) != 0 || capture.usable() {
		t.Fatalf("release retained evidence or pixels: evidence=%d capture=%v", len(store), capture.usable())
	}
}
