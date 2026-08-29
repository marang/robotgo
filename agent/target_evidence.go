package agent

import (
	"fmt"
	"math"
	"slices"
	"sync/atomic"
	"time"
)

const (
	// TargetEvidenceClauseSchemaVersion identifies one reviewed analysis-item
	// reference used by TargetSpec v2.
	TargetEvidenceClauseSchemaVersion = "1"
	maxTargetEvidenceClauses          = 2
	maxRetainedTargetEvidenceRecords  = 16
	maxTargetEvidenceIdentifierBytes  = 128
	targetEvidenceIDPrefix            = "target-evidence-"
	targetEvidenceOCRScore            = 25
	targetEvidenceVisualScore         = 15
)

var targetEvidenceSerial atomic.Uint64

// TargetEvidenceSource is one fixed, policy-gated weaker resolver source.
type TargetEvidenceSource string

const (
	TargetEvidenceSourceOCR    TargetEvidenceSource = "ocr"
	TargetEvidenceSourceVisual TargetEvidenceSource = "visual"
)

var allTargetEvidenceSources = []TargetEvidenceSource{
	TargetEvidenceSourceOCR,
	TargetEvidenceSourceVisual,
}

// TargetEvidenceProvider is one exact backend/model identity explicitly
// trusted by immutable policy for weaker target-resolution evidence.
type TargetEvidenceProvider struct {
	Source  TargetEvidenceSource `json:"source"`
	Backend string               `json:"backend"`
	Model   string               `json:"model"`
}

// TargetEvidenceClause references exactly one item from an explicit prior OCR
// or visual-analysis result. It contains no pixels or OCR text.
type TargetEvidenceClause struct {
	SchemaVersion string               `json:"schema_version"`
	ObservationID string               `json:"observation_id"`
	EvidenceID    string               `json:"evidence_id"`
	Source        TargetEvidenceSource `json:"source"`
	ItemIndex     uint32               `json:"item_index"`
}

type retainedTargetEvidenceItem struct {
	bounds     CaptureRegion
	confidence float64
	kind       string
}

type retainedTargetEvidence struct {
	id        string
	serial    uint64
	source    TargetEvidenceSource
	region    CaptureRegion
	backend   string
	model     string
	languages []string
	createdAt time.Time
	truncated bool
	sanitized bool
	items     []retainedTargetEvidenceItem
}

type retainedTargetEvidenceBundle struct {
	observationID  string
	viewRegion     CaptureRegion
	viewCreatedAt  time.Time
	viewDownscaled bool
	viewRedacted   bool
	evidence       []retainedTargetEvidence
	ageMillis      int64
	expiresAt      time.Time
}

func validTargetEvidenceSource(source TargetEvidenceSource) bool {
	return source == TargetEvidenceSourceOCR || source == TargetEvidenceSourceVisual
}

func validTargetEvidenceIdentifier(value string) bool {
	return value != "" && len(value) <= maxTargetEvidenceIdentifierBytes && sanitizeUIText(value) == value
}

func validTargetEvidenceProvider(provider TargetEvidenceProvider) bool {
	return validTargetEvidenceSource(provider.Source) &&
		validTargetEvidenceIdentifier(provider.Backend) && validTargetEvidenceIdentifier(provider.Model)
}

func validTargetEvidenceConfidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value <= 1
}

func newTargetEvidenceIdentity() (string, uint64) {
	serial := targetEvidenceSerial.Add(1)
	return fmt.Sprintf("%s%d", targetEvidenceIDPrefix, serial), serial
}

func validTargetEvidenceID(value string) bool {
	if len(value) <= len(targetEvidenceIDPrefix) || len(value) > len(targetEvidenceIDPrefix)+20 ||
		value[:len(targetEvidenceIDPrefix)] != targetEvidenceIDPrefix {
		return false
	}
	for _, digit := range value[len(targetEvidenceIDPrefix):] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func cloneRetainedTargetEvidence(source retainedTargetEvidence) retainedTargetEvidence {
	result := source
	result.languages = append([]string(nil), source.languages...)
	result.items = append([]retainedTargetEvidenceItem(nil), source.items...)
	return result
}

func clearRetainedTargetEvidence(evidence *retainedTargetEvidence) {
	if evidence == nil {
		return
	}
	clear(evidence.languages)
	evidence.languages = nil
	for index := range evidence.items {
		evidence.items[index] = retainedTargetEvidenceItem{}
	}
	clear(evidence.items)
	evidence.items = nil
	evidence.id = ""
	evidence.serial = 0
	evidence.backend = ""
	evidence.model = ""
}

func (s *Session) storeTargetEvidence(observationID string, evidence retainedTargetEvidence) (string, error) {
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	record, ok := s.observations[observationID]
	if !ok || record.source != OperationView || !record.hasCapture || record.capture == nil ||
		!record.capture.usable() || !captureRegionContains(record.region, evidence.region) {
		clearRetainedTargetEvidence(&evidence)
		return "", ErrObservationClosed
	}
	if record.targetEvidence == nil {
		record.targetEvidence = make(map[string]retainedTargetEvidence)
	}
	pruneRetainedTargetEvidence(
		record.targetEvidence, s.now(), time.Duration(s.policy.MaxTargetEvidenceAgeMillis)*time.Millisecond,
	)
	evidence.id, evidence.serial = newTargetEvidenceIdentity()
	record.targetEvidence[evidence.id] = evidence
	s.observations[observationID] = record
	return evidence.id, nil
}

func pruneRetainedTargetEvidence(records map[string]retainedTargetEvidence, now time.Time, maximumAge time.Duration) {
	for id, evidence := range records {
		age := now.Sub(evidence.createdAt)
		if evidence.createdAt.IsZero() || age < 0 || age > maximumAge {
			clearRetainedTargetEvidence(&evidence)
			delete(records, id)
		}
	}
	for len(records) >= maxRetainedTargetEvidenceRecords {
		oldestID := ""
		var oldestCreatedAt time.Time
		var oldestSerial uint64
		for id, evidence := range records {
			if oldestID == "" || evidence.createdAt.Before(oldestCreatedAt) ||
				(evidence.createdAt.Equal(oldestCreatedAt) && evidence.serial < oldestSerial) {
				oldestID = id
				oldestCreatedAt = evidence.createdAt
				oldestSerial = evidence.serial
			}
		}
		evidence := records[oldestID]
		clearRetainedTargetEvidence(&evidence)
		delete(records, oldestID)
	}
}

func (s *Session) publishTargetEvidence(observationID string, evidence retainedTargetEvidence) (string, error) {
	provider := TargetEvidenceProvider{Source: evidence.source, Backend: evidence.backend, Model: evidence.model}
	_, sourceAllowed := s.policy.allowTargetEvidenceSource[evidence.source]
	_, providerAllowed := s.policy.allowTargetEvidenceProvider[provider]
	regionAllowed := false
	for _, region := range s.policy.AllowedTargetEvidenceRegions {
		regionAllowed = regionAllowed || captureRegionContains(region, evidence.region)
	}
	if !sourceAllowed || !providerAllowed || !regionAllowed || len(evidence.items) == 0 {
		clearRetainedTargetEvidence(&evidence)
		return "", nil
	}
	return s.storeTargetEvidence(observationID, evidence)
}

func (s *Session) removeTargetEvidence(observationID, evidenceID string) {
	if evidenceID == "" {
		return
	}
	s.observationMu.Lock()
	record, ok := s.observations[observationID]
	if ok {
		if evidence, exists := record.targetEvidence[evidenceID]; exists {
			clearRetainedTargetEvidence(&evidence)
			delete(record.targetEvidence, evidenceID)
			s.observations[observationID] = record
		}
	}
	s.observationMu.Unlock()
}

func (s *Session) retainTargetEvidenceBundle(clauses []TargetEvidenceClause) (retainedTargetEvidenceBundle, error) {
	var bundle retainedTargetEvidenceBundle
	if len(clauses) == 0 {
		return bundle, nil
	}
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	record, ok := s.observations[clauses[0].ObservationID]
	if !ok || record.source != OperationView || !record.hasCapture || record.capture == nil || !record.capture.usable() {
		return bundle, targetResolutionError(ErrorStaleTarget, "target image observation is no longer live", ErrObservationClosed)
	}
	bundle = retainedTargetEvidenceBundle{
		observationID: clauses[0].ObservationID, viewRegion: record.region,
		viewCreatedAt: record.createdAt, viewDownscaled: record.viewDownscaled,
		viewRedacted: record.redacted, evidence: make([]retainedTargetEvidence, 0, len(clauses)),
	}
	for _, clause := range clauses {
		evidence, exists := record.targetEvidence[clause.EvidenceID]
		if !exists || evidence.source != clause.Source || clause.ItemIndex >= uint32(len(evidence.items)) {
			clearRetainedTargetEvidenceBundle(&bundle)
			return retainedTargetEvidenceBundle{}, targetResolutionError(
				ErrorStaleTarget, "target analysis evidence is unavailable", ErrStaleTarget)
		}
		selected := cloneRetainedTargetEvidence(evidence)
		item := selected.items[clause.ItemIndex]
		clear(selected.items)
		selected.items = []retainedTargetEvidenceItem{item}
		bundle.evidence = append(bundle.evidence, selected)
	}
	slices.SortFunc(bundle.evidence, func(left, right retainedTargetEvidence) int {
		return targetEvidenceSourceRank(left.source) - targetEvidenceSourceRank(right.source)
	})
	return bundle, nil
}

func targetEvidenceSourceRank(source TargetEvidenceSource) int {
	for index, candidate := range allTargetEvidenceSources {
		if candidate == source {
			return index
		}
	}
	return len(allTargetEvidenceSources)
}

func (s *Session) authorizeTargetEvidence(bundle *retainedTargetEvidenceBundle) error {
	if bundle == nil || len(bundle.evidence) == 0 {
		return nil
	}
	if bundle.viewDownscaled || bundle.viewRedacted || bundle.viewCreatedAt.IsZero() {
		return targetResolutionError(ErrorIncompleteObservation,
			"target image evidence has redacted or transformed lineage", ErrIncompleteObservation)
	}
	now := s.now()
	maximumAge := time.Duration(s.policy.MaxTargetEvidenceAgeMillis) * time.Millisecond
	viewAge := now.Sub(bundle.viewCreatedAt)
	if viewAge < 0 || viewAge > maximumAge {
		return targetResolutionError(ErrorStaleTarget, "target image observation exceeded its policy age", ErrStaleTarget)
	}
	bundle.ageMillis = max(int64(0), viewAge.Milliseconds())
	bundle.expiresAt = bundle.viewCreatedAt.Add(maximumAge)
	for index := range bundle.evidence {
		evidence := &bundle.evidence[index]
		if evidence.truncated || evidence.sanitized || evidence.createdAt.IsZero() ||
			!captureRegionContains(bundle.viewRegion, evidence.region) || len(evidence.items) != 1 {
			return targetResolutionError(ErrorIncompleteObservation,
				"target analysis evidence is clipped, sanitized, or incomplete", ErrIncompleteObservation)
		}
		age := now.Sub(evidence.createdAt)
		if age < 0 || age > maximumAge {
			return targetResolutionError(ErrorStaleTarget, "target analysis evidence exceeded its policy age", ErrStaleTarget)
		}
		bundle.ageMillis = max(bundle.ageMillis, age.Milliseconds())
		evidenceExpiry := evidence.createdAt.Add(maximumAge)
		if evidenceExpiry.Before(bundle.expiresAt) {
			bundle.expiresAt = evidenceExpiry
		}
		if _, allowed := s.policy.allowTargetEvidenceSource[evidence.source]; !allowed {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied the target evidence source", ErrPolicyDenied)
		}
		provider := TargetEvidenceProvider{Source: evidence.source, Backend: evidence.backend, Model: evidence.model}
		if _, allowed := s.policy.allowTargetEvidenceProvider[provider]; !allowed {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied the target evidence provider", ErrPolicyDenied)
		}
		regionAllowed := false
		for _, region := range s.policy.AllowedTargetEvidenceRegions {
			regionAllowed = regionAllowed || captureRegionContains(region, evidence.region)
		}
		if !regionAllowed {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied the target evidence region", ErrPolicyDenied)
		}
		item := evidence.items[0]
		if !strictlyContainsCaptureRegion(evidence.region, item.bounds) {
			return targetResolutionError(ErrorIncompleteObservation,
				"target analysis item touches a clipped region boundary", ErrIncompleteObservation)
		}
		switch evidence.source {
		case TargetEvidenceSourceOCR:
			if item.confidence < s.policy.MinTargetOCRConfidence {
				return targetResolutionError(ErrorTargetNotFound, "OCR target evidence is below the policy threshold", ErrTargetNotFound)
			}
			for _, language := range evidence.languages {
				if _, allowed := s.policy.allowOCRLanguage[language]; !allowed {
					return targetResolutionError(ErrorPolicyDenied, "agent policy denied the target OCR language", ErrPolicyDenied)
				}
			}
		case TargetEvidenceSourceVisual:
			if item.kind != "visual-region" || item.confidence < s.policy.MinTargetVisualConfidence {
				return targetResolutionError(ErrorTargetNotFound, "visual target evidence is below the policy threshold", ErrTargetNotFound)
			}
		default:
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied the target evidence source", ErrPolicyDenied)
		}
	}
	return nil
}

func targetEvidenceCandidateScore(candidate *retainedUITarget, bundle retainedTargetEvidenceBundle) (uint32, bool) {
	if candidate == nil || !validUIBounds(candidate.expected.Bounds) || len(bundle.evidence) == 0 {
		return 0, false
	}
	score := uint32(0)
	for _, evidence := range bundle.evidence {
		if len(evidence.items) != 1 || !uiBoundsContainCaptureCenter(candidate.expected.Bounds, evidence.items[0].bounds) {
			return 0, false
		}
		switch evidence.source {
		case TargetEvidenceSourceOCR:
			score += targetEvidenceOCRScore
		case TargetEvidenceSourceVisual:
			score += targetEvidenceVisualScore
		default:
			return 0, false
		}
	}
	return score, true
}

func targetEvidenceStrategy(bundle retainedTargetEvidenceBundle) TargetResolutionStrategy {
	if len(bundle.evidence) == 2 {
		return TargetResolutionCombinedEvidence
	}
	if len(bundle.evidence) == 1 && bundle.evidence[0].source == TargetEvidenceSourceOCR {
		return TargetResolutionOCREvidence
	}
	return TargetResolutionVisualEvidence
}

func targetEvidenceResultMetadata(bundle retainedTargetEvidenceBundle) ([]TargetEvidenceSource, []TargetEvidenceProvider) {
	sources := make([]TargetEvidenceSource, 0, len(bundle.evidence))
	providers := make([]TargetEvidenceProvider, 0, len(bundle.evidence))
	for _, evidence := range bundle.evidence {
		sources = append(sources, evidence.source)
		providers = append(providers, TargetEvidenceProvider{
			Source: evidence.source, Backend: evidence.backend, Model: evidence.model,
		})
	}
	return sources, providers
}

func targetEvidenceTokens(bundle retainedTargetEvidenceBundle) []TargetEvidence {
	tokens := []TargetEvidence{TargetEvidenceImageObservation, TargetEvidenceAnalysisProvenance}
	for _, evidence := range bundle.evidence {
		if evidence.source == TargetEvidenceSourceOCR {
			tokens = append(tokens, TargetEvidenceOCRItem)
		} else {
			tokens = append(tokens, TargetEvidenceVisualItem)
		}
	}
	return tokens
}

func strictlyContainsCaptureRegion(outer, inner CaptureRegion) bool {
	return outer.DisplayID == inner.DisplayID && inner.X > outer.X && inner.Y > outer.Y &&
		inner.X+inner.Width < outer.X+outer.Width && inner.Y+inner.Height < outer.Y+outer.Height
}

func uiBoundsContainCaptureCenter(bounds *UIBounds, region CaptureRegion) bool {
	if !validUIBounds(bounds) || region.Width <= 0 || region.Height <= 0 {
		return false
	}
	x := region.X + region.Width/2
	y := region.Y + region.Height/2
	return x >= bounds.X && x < bounds.X+bounds.Width && y >= bounds.Y && y < bounds.Y+bounds.Height
}

func clearRetainedTargetEvidenceBundle(bundle *retainedTargetEvidenceBundle) {
	if bundle == nil {
		return
	}
	for index := range bundle.evidence {
		clearRetainedTargetEvidence(&bundle.evidence[index])
	}
	clear(bundle.evidence)
	bundle.evidence = nil
	bundle.observationID = ""
}
