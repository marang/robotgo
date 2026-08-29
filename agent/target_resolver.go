package agent

import (
	"context"
	"errors"
	"slices"
	"unicode/utf8"
)

const (
	// TargetSpecSchemaVersion identifies the deterministic semantic plus
	// reviewed analysis-evidence locator contract. Semantic-only v1 inputs
	// remain accepted with unchanged behavior.
	TargetSpecSchemaVersion       = "2"
	TargetSpecLegacySchemaVersion = "1"
	// TargetResolutionSchemaVersion identifies the privacy-reduced resolver
	// result contract.
	TargetResolutionSchemaVersion = "2"
	maxTargetSpecAncestors        = maxAgentUITreeDepth
)

// TargetResolutionStrategy identifies the exact semantic or reviewed,
// policy-gated analysis-evidence resolver path.
type TargetResolutionStrategy string

const (
	TargetResolutionExactSemantic      TargetResolutionStrategy = "exact-semantic"
	TargetResolutionStructuralSemantic TargetResolutionStrategy = "structural-semantic"
	TargetResolutionAdaptiveSemantic   TargetResolutionStrategy = "adaptive-semantic"
	TargetResolutionOCREvidence        TargetResolutionStrategy = "ocr-evidence"
	TargetResolutionVisualEvidence     TargetResolutionStrategy = "visual-evidence"
	TargetResolutionCombinedEvidence   TargetResolutionStrategy = "combined-evidence"
)

var allTargetResolutionStrategies = []TargetResolutionStrategy{
	TargetResolutionExactSemantic,
	TargetResolutionStructuralSemantic,
	TargetResolutionAdaptiveSemantic,
	TargetResolutionOCREvidence,
	TargetResolutionVisualEvidence,
	TargetResolutionCombinedEvidence,
}

// TargetEvidence is a fixed, payload-free explanation token. The order in a
// result is stable and follows the order of these constants.
type TargetEvidence string

const (
	TargetEvidenceWindowIdentity     TargetEvidence = "window-identity"
	TargetEvidenceRole               TargetEvidence = "role"
	TargetEvidenceName               TargetEvidence = "name"
	TargetEvidenceStates             TargetEvidence = "required-states"
	TargetEvidenceActions            TargetEvidence = "required-actions"
	TargetEvidenceAncestors          TargetEvidence = "ancestor-chain"
	TargetEvidenceImageObservation   TargetEvidence = "image-observation"
	TargetEvidenceAnalysisProvenance TargetEvidence = "analysis-provenance"
	TargetEvidenceOCRItem            TargetEvidence = "ocr-item"
	TargetEvidenceVisualItem         TargetEvidence = "visual-item"
)

// TargetWindowSpec binds a TargetSpec to the same explicit, policy-owned
// window identity used by its source semantic observation. ExpectedTitle is
// compared privately and is never returned in resolver evidence or errors.
type TargetWindowSpec struct {
	Target        int              `json:"target"`
	Kind          WindowTargetKind `json:"kind"`
	ExpectedTitle string           `json:"expected_title"`
}

// TargetAncestor is one exact structural anchor. Ancestors are ordered from
// the immediate parent outward and resolution never skips hierarchy levels.
type TargetAncestor struct {
	Role           UIRole    `json:"role"`
	Name           string    `json:"name"`
	RequiredStates []UIState `json:"required_states,omitempty"`
}

// TargetSpec describes one exact semantic target in one observation-scoped
// window. Required state/action clauses use set containment; all identity and
// ancestor fields use exact equality.
type TargetSpec struct {
	SchemaVersion   string                 `json:"schema_version"`
	Window          TargetWindowSpec       `json:"window"`
	Role            UIRole                 `json:"role"`
	Name            string                 `json:"name"`
	RequiredStates  []UIState              `json:"required_states,omitempty"`
	RequiredActions []UIAction             `json:"required_actions,omitempty"`
	Ancestors       []TargetAncestor       `json:"ancestors,omitempty"`
	Evidence        []TargetEvidenceClause `json:"evidence,omitempty"`
}

// ResolveUIRequest resolves one TargetSpec only within one live retained UI
// observation. Confirmed is used only when immutable policy requires it.
type ResolveUIRequest struct {
	ObservationID string                  `json:"observation_id"`
	Target        TargetSpec              `json:"target"`
	Mode          TargetResolutionMode    `json:"mode,omitempty"`
	Lease         *CapabilityLeaseRequest `json:"lease,omitempty"`
	Confirmed     bool                    `json:"confirmed,omitempty"`
}

// TargetPatchProposal is deliberately non-executable and payload-free. It
// identifies which locator clauses drifted without returning replacement text.
type TargetPatchProposal struct {
	SchemaVersion string           `json:"schema_version"`
	Changed       []TargetEvidence `json:"changed"`
	Score         uint32           `json:"score"`
	Threshold     uint32           `json:"threshold"`
	Executable    bool             `json:"executable"`
}

// TargetResolutionResult returns legacy exact selection, an opaque single-use
// lease, or a non-executable review proposal only when one candidate qualifies.
type TargetResolutionResult struct {
	SchemaVersion          string                   `json:"schema_version"`
	ObservationID          string                   `json:"observation_id,omitempty"`
	Strategy               TargetResolutionStrategy `json:"strategy,omitempty"`
	Mode                   TargetResolutionMode     `json:"mode"`
	MatchedBy              []TargetEvidence         `json:"matched_by,omitempty"`
	Changed                []TargetEvidence         `json:"changed,omitempty"`
	CandidateCount         uint32                   `json:"candidate_count"`
	RejectedCandidateCount uint32                   `json:"rejected_candidate_count"`
	Ambiguous              bool                     `json:"ambiguous"`
	AdaptiveScore          uint32                   `json:"adaptive_score,omitempty"`
	AdaptiveThreshold      uint32                   `json:"adaptive_threshold,omitempty"`
	ElementID              string                   `json:"element_id,omitempty"`
	Expected               *UIElementExpectation    `json:"expected,omitempty"`
	Lease                  *CapabilityLease         `json:"lease,omitempty"`
	Patch                  *TargetPatchProposal     `json:"patch,omitempty"`
	EvidenceSources        []TargetEvidenceSource   `json:"evidence_sources,omitempty"`
	EvidenceProviders      []TargetEvidenceProvider `json:"evidence_providers,omitempty"`
	EvidenceAgeMillis      int64                    `json:"evidence_age_ms,omitempty"`
}

type retainedUITarget struct {
	elementID        string
	parentID         string
	parentIncomplete bool
	expected         UIElementExpectation
}

type retainedUITargetGraph struct {
	target     uiBackendTarget
	elements   []retainedUITarget
	incomplete bool
	actionable bool
}

// ResolveUITarget deterministically resolves one exact semantic or structural
// TargetSpec against a retained sanitized observation. It never calls a
// desktop backend and consumes no query, observation, or action quota.
func (s *Session) ResolveUITarget(ctx context.Context, request ResolveUIRequest) (TargetResolutionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mode := normalizeTargetResolutionMode(request.Mode)
	result := TargetResolutionResult{SchemaVersion: TargetResolutionSchemaVersion, Mode: mode}
	if err := validateResolveUIRequest(request); err != nil {
		return result, targetResolutionError(ErrorInvalidInput, "invalid semantic target specification", err)
	}
	result.ObservationID = request.ObservationID
	result.Strategy = targetResolutionStrategy(request.Target)
	if mode != TargetResolutionModeStrict {
		result.Strategy = TargetResolutionAdaptiveSemantic
	}

	if err := s.acquire(ctx); err != nil {
		return clearTargetSelection(result), targetResolutionOperationError(err)
	}
	defer s.release()
	if err := s.ensureOpen(); err != nil {
		return clearTargetSelection(result), targetResolutionOperationError(err)
	}
	if err := s.authorizeTargetResolution(request); err != nil {
		return clearTargetSelection(result), err
	}
	if err := s.emitAudit(ctx, targetResolutionAuditEvent(AuditResolutionStarted, result, "")); err != nil {
		return clearTargetSelection(result), targetResolutionError(
			ErrorAuditDelivery, "audit sink rejected semantic target resolution intent", err)
	}
	if err := s.targetResolutionExecutionError(ctx); err != nil {
		return s.finishTargetResolution(ctx, clearTargetSelection(result), err)
	}

	graph, ok := s.retainUITargetGraph(request.ObservationID)
	if !ok {
		operationErr := targetResolutionError(ErrorTargetNotFound, "semantic target observation is unavailable", ErrTargetNotFound)
		return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
	}
	defer clearRetainedUITargets(graph.elements)
	if operationErr := s.targetResolutionExecutionError(ctx); operationErr != nil {
		return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
	}
	if graph.target.Target != request.Target.Window.Target || graph.target.Kind != request.Target.Window.Kind ||
		graph.target.ExpectedTitle != request.Target.Window.ExpectedTitle {
		result.RejectedCandidateCount = uint32(len(graph.elements))
		operationErr := targetResolutionError(ErrorTargetNotFound, "semantic target was not found", ErrTargetNotFound)
		return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
	}
	if graph.incomplete || !graph.actionable {
		result.RejectedCandidateCount = uint32(len(graph.elements))
		operationErr := targetResolutionError(
			ErrorIncompleteObservation,
			"semantic target cannot be proven from an incomplete observation",
			ErrIncompleteObservation,
		)
		return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
	}

	byID := make(map[string]int, len(graph.elements))
	for index := range graph.elements {
		byID[graph.elements[index].elementID] = index
	}
	var selected *retainedUITarget
	var selectedScore uint32
	var selectedChanged []TargetEvidence
	var incompleteCandidate bool
	for index := range graph.elements {
		if operationErr := s.targetResolutionExecutionError(ctx); operationErr != nil {
			return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
		}
		candidate := &graph.elements[index]
		matches, incomplete := matchesTargetSpec(candidate, request.Target, graph.elements, byID)
		score, changed, adaptiveIncomplete := adaptiveTargetScore(candidate, request.Target, graph.elements, byID)
		if mode != TargetResolutionModeStrict {
			matches = !adaptiveIncomplete && score >= s.policy.AdaptiveTargetThreshold
			incomplete = adaptiveIncomplete
		}
		if !matches {
			result.RejectedCandidateCount++
			incompleteCandidate = incompleteCandidate || incomplete
			continue
		}
		result.CandidateCount++
		if selected == nil {
			selected = candidate
			selectedScore = score
			selectedChanged = changed
		}
	}
	if operationErr := s.targetResolutionExecutionError(ctx); operationErr != nil {
		return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
	}
	if result.CandidateCount <= 1 && incompleteCandidate {
		operationErr := targetResolutionError(
			ErrorIncompleteObservation,
			"semantic target cannot be proven from an incomplete observation",
			ErrIncompleteObservation,
		)
		return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
	}
	var evidenceBundle retainedTargetEvidenceBundle
	if result.CandidateCount == 0 && len(request.Target.Evidence) > 0 {
		var evidenceErr error
		evidenceBundle, evidenceErr = s.retainTargetEvidenceBundle(request.Target.Evidence)
		if evidenceErr != nil {
			return s.finishTargetResolution(ctx, clearTargetSelection(result), evidenceErr)
		}
		defer clearRetainedTargetEvidenceBundle(&evidenceBundle)
		if evidenceErr = s.authorizeTargetEvidence(&evidenceBundle); evidenceErr != nil {
			return s.finishTargetResolution(ctx, clearTargetSelection(result), evidenceErr)
		}
		result.Strategy = targetEvidenceStrategy(evidenceBundle)
		result.EvidenceSources, result.EvidenceProviders = targetEvidenceResultMetadata(evidenceBundle)
		result.EvidenceAgeMillis = evidenceBundle.ageMillis
		result.RejectedCandidateCount = 0
		incompleteCandidate = false
		for index := range graph.elements {
			if operationErr := s.targetResolutionExecutionError(ctx); operationErr != nil {
				return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
			}
			candidate := &graph.elements[index]
			score, changed, incomplete := adaptiveTargetScore(candidate, request.Target, graph.elements, byID)
			bonus, evidenceMatches := targetEvidenceCandidateScore(candidate, evidenceBundle)
			if !incomplete && evidenceMatches {
				score = min(uint32(100), score+bonus)
			}
			if incomplete || !evidenceMatches || score < s.policy.AdaptiveTargetThreshold {
				result.RejectedCandidateCount++
				incompleteCandidate = incompleteCandidate || incomplete
				continue
			}
			result.CandidateCount++
			if selected == nil {
				selected = candidate
				selectedScore = score
				selectedChanged = changed
			}
		}
		if result.CandidateCount <= 1 && incompleteCandidate {
			operationErr := targetResolutionError(
				ErrorIncompleteObservation,
				"semantic target cannot be proven from incomplete visual evidence",
				ErrIncompleteObservation,
			)
			return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
		}
	}

	switch result.CandidateCount {
	case 0:
		operationErr := targetResolutionError(ErrorTargetNotFound, "semantic target was not found", ErrTargetNotFound)
		return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
	case 1:
		result.MatchedBy = targetResolutionEvidence(request.Target)
		if mode != TargetResolutionModeStrict {
			result.AdaptiveScore = selectedScore
			result.AdaptiveThreshold = s.policy.AdaptiveTargetThreshold
			result.Changed = append([]TargetEvidence(nil), selectedChanged...)
			for _, changed := range selectedChanged {
				result.MatchedBy = slices.DeleteFunc(result.MatchedBy, func(value TargetEvidence) bool { return value == changed })
			}
			if len(evidenceBundle.evidence) > 0 {
				result.MatchedBy = append(result.MatchedBy, targetEvidenceTokens(evidenceBundle)...)
			}
		}
		if mode == TargetResolutionModeReview {
			result.Patch = &TargetPatchProposal{SchemaVersion: TargetPatchProposalSchemaVersion, Changed: append([]TargetEvidence{}, selectedChanged...),
				Score: selectedScore, Threshold: s.policy.AdaptiveTargetThreshold, Executable: false}
			return s.finishTargetResolution(ctx, result, nil)
		}
		if request.Lease != nil {
			lease, err := s.issueCapabilityLease(request, selected, evidenceBundle.expiresAt)
			if err != nil {
				code := ErrorBackendFailure
				if errors.Is(err, ErrStaleTarget) {
					code = ErrorStaleTarget
				} else if errors.Is(err, ErrPolicyDenied) {
					code = ErrorPolicyDenied
				}
				operationErr := targetResolutionError(code, "semantic capability lease could not be issued", err)
				return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
			}
			result.Lease = lease
			return s.finishTargetResolution(ctx, result, nil)
		}
		expected := cloneUIElementExpectation(selected.expected)
		result.ElementID = selected.elementID
		result.Expected = &expected
		return s.finishTargetResolution(ctx, result, nil)
	default:
		result.MatchedBy = targetResolutionEvidence(request.Target)
		result.Ambiguous = true
		operationErr := targetResolutionError(ErrorAmbiguousTarget, "semantic target is ambiguous", ErrAmbiguousTarget)
		return s.finishTargetResolution(ctx, clearTargetSelection(result), operationErr)
	}
}

func validateResolveUIRequest(request ResolveUIRequest) error {
	mode := normalizeTargetResolutionMode(request.Mode)
	if !validObservationID(request.ObservationID) ||
		(request.Target.SchemaVersion != TargetSpecSchemaVersion && request.Target.SchemaVersion != TargetSpecLegacySchemaVersion) {
		return errors.New("invalid observation ID or target schema version")
	}
	if request.Target.SchemaVersion == TargetSpecLegacySchemaVersion && len(request.Target.Evidence) != 0 {
		return errors.New("TargetSpec v1 cannot contain analysis evidence")
	}
	if !validTargetResolutionMode(mode) {
		return errors.New("invalid semantic target mode")
	}
	if mode == TargetResolutionModeAdaptive && request.Lease == nil {
		return errors.New("adaptive resolution requires a single-use capability lease")
	}
	if mode == TargetResolutionModeReview && request.Lease != nil {
		return errors.New("review resolution cannot issue executable authority")
	}
	if len(request.Target.Evidence) > maxTargetEvidenceClauses ||
		(mode == TargetResolutionModeStrict && len(request.Target.Evidence) != 0) {
		return errors.New("target analysis evidence requires bounded adaptive or review mode")
	}
	seenEvidenceSources := make(map[TargetEvidenceSource]struct{}, len(request.Target.Evidence))
	evidenceObservationID := ""
	for _, clause := range request.Target.Evidence {
		if clause.SchemaVersion != TargetEvidenceClauseSchemaVersion ||
			!validObservationID(clause.ObservationID) || !validTargetEvidenceID(clause.EvidenceID) ||
			!validTargetEvidenceSource(clause.Source) || clause.ItemIndex >= uint32(maxAgentAnalysisBoxes) {
			return errors.New("invalid target analysis evidence clause")
		}
		if evidenceObservationID == "" {
			evidenceObservationID = clause.ObservationID
		} else if evidenceObservationID != clause.ObservationID {
			return errors.New("target analysis evidence must share one image observation")
		}
		if _, duplicate := seenEvidenceSources[clause.Source]; duplicate {
			return errors.New("target analysis evidence source is duplicated")
		}
		seenEvidenceSources[clause.Source] = struct{}{}
	}
	if request.Lease != nil {
		if request.Lease.SchemaVersion != CapabilityLeaseSchemaVersion || !validUIAction(request.Lease.Action) ||
			request.Lease.DurationMillis <= 0 || request.Lease.DurationMillis > maxAgentCapabilityLeaseMillis ||
			validateUIElementCondition(request.Lease.Action, request.Lease.Postcondition) != nil {
			return errors.New("invalid semantic capability lease request")
		}
		if request.Lease.Action == UIActionSetValue {
			if _, ok := parseLeaseValueDigest(request.Lease.ActionValueSHA256); !ok {
				return errors.New("set-value lease requires a SHA-256 value binding")
			}
		} else if request.Lease.ActionValueSHA256 != "" {
			return errors.New("only set-value accepts a value binding")
		}
	}
	window := request.Target.Window
	if window.Target <= 0 || !validWindowTargetKind(window.Kind) || window.ExpectedTitle == "" ||
		!utf8.ValidString(window.ExpectedTitle) || utf8.RuneCountInString(window.ExpectedTitle) > maxAgentWindowTitleRunes {
		return errors.New("invalid target window identity")
	}
	totalNameBytes := len(request.Target.Name)
	if !validTargetIdentity(request.Target.Role, request.Target.Name, request.Target.RequiredStates) ||
		len(request.Target.RequiredActions) > maxUIActionsPerNode ||
		!validUniqueUIActions(request.Target.RequiredActions) {
		return errors.New("invalid target semantic identity")
	}
	if len(request.Target.Ancestors) > maxTargetSpecAncestors {
		return errors.New("target ancestor chain exceeds the hard limit")
	}
	for _, ancestor := range request.Target.Ancestors {
		if len(ancestor.Name) > maxAgentUIStringBytes-totalNameBytes {
			return errors.New("target identity text exceeds the hard aggregate limit")
		}
		totalNameBytes += len(ancestor.Name)
		if !validTargetIdentity(ancestor.Role, ancestor.Name, ancestor.RequiredStates) {
			return errors.New("invalid target ancestor identity")
		}
	}
	return nil
}

func validTargetIdentity(role UIRole, name string, states []UIState) bool {
	return validUIRole(role) && name != "" && utf8.ValidString(name) &&
		len(name) <= maxAgentUIStringBytes && sanitizeUIText(name) == name &&
		len(states) <= maxUIStatesPerNode && validUniqueUIStates(states)
}

func (s *Session) authorizeTargetResolution(request ResolveUIRequest) *ActionError {
	mode := normalizeTargetResolutionMode(request.Mode)
	if s.policy.RequireCapabilityLease && mode != TargetResolutionModeReview && request.Lease == nil {
		return targetResolutionError(ErrorLeaseRequired, "semantic capability lease is required", ErrLeaseRequired)
	}
	if _, allowed := s.policy.allowOperation[OperationResolveUI]; !allowed {
		return targetResolutionError(ErrorPolicyDenied, "agent policy denied semantic target resolution", ErrPolicyDenied)
	}
	if _, required := s.policy.requireConfirmation[OperationResolveUI]; required && !request.Confirmed {
		return targetResolutionError(ErrorPolicyDenied, "agent policy requires semantic target resolution confirmation", ErrPolicyDenied)
	}
	if _, allowed := s.policy.allowWindow[windowTargetIdentity{
		target: request.Target.Window.Target,
		kind:   request.Target.Window.Kind,
	}]; !allowed {
		return targetResolutionError(ErrorPolicyDenied, "agent policy denied the semantic target window", ErrPolicyDenied)
	}
	if _, allowed := s.policy.allowTargetMode[mode]; !allowed {
		return targetResolutionError(ErrorPolicyDenied, "agent policy denied the semantic target mode", ErrPolicyDenied)
	}
	for _, clause := range request.Target.Evidence {
		if _, allowed := s.policy.allowTargetEvidenceSource[clause.Source]; !allowed {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied the target evidence source", ErrPolicyDenied)
		}
	}
	if request.Lease != nil {
		if request.Lease.DurationMillis > s.policy.MaxCapabilityLeaseMillis ||
			!slices.Contains(request.Target.RequiredActions, request.Lease.Action) {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied the semantic capability lease binding", ErrPolicyDenied)
		}
		if _, allowed := s.policy.allowUIAction[request.Lease.Action]; !allowed {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied the semantic capability lease action", ErrPolicyDenied)
		}
	}
	totalNameBytes := len(request.Target.Name)
	for _, ancestor := range request.Target.Ancestors {
		totalNameBytes += len(ancestor.Name)
	}
	if totalNameBytes > int(s.policy.MaxUIStringBytes) || len(request.Target.Ancestors) > int(s.policy.MaxUITreeDepth) {
		return targetResolutionError(ErrorPolicyDenied, "semantic target specification exceeds UI policy bounds", ErrPolicyDenied)
	}
	for _, property := range []UIProperty{
		UIPropertyRole, UIPropertyName, UIPropertyState, UIPropertyBounds, UIPropertyActions,
	} {
		if _, allowed := s.policy.allowUIProperty[property]; !allowed {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied a required semantic target property", ErrPolicyDenied)
		}
	}
	if len(request.Target.Ancestors) > 0 {
		if _, allowed := s.policy.allowUIProperty[UIPropertyHierarchy]; !allowed {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied semantic target hierarchy", ErrPolicyDenied)
		}
	}
	if _, allowed := s.policy.allowUIRole[request.Target.Role]; !allowed {
		return targetResolutionError(ErrorPolicyDenied, "agent policy denied the semantic target role", ErrPolicyDenied)
	}
	for _, ancestor := range request.Target.Ancestors {
		if _, allowed := s.policy.allowUIRole[ancestor.Role]; !allowed {
			return targetResolutionError(ErrorPolicyDenied, "agent policy denied a semantic target ancestor role", ErrPolicyDenied)
		}
	}
	return nil
}

func (s *Session) retainUITargetGraph(observationID string) (retainedUITargetGraph, bool) {
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	record, ok := s.observations[observationID]
	if !ok || record.uiTarget == nil || record.uiBackend == "" || len(record.uiTree) == 0 {
		return retainedUITargetGraph{}, false
	}
	return retainedUITargetGraph{
		target:     *record.uiTarget,
		elements:   cloneRetainedUITargets(record.uiTree),
		incomplete: record.uiResolutionIncomplete,
		actionable: record.uiActionable,
	}, true
}

func matchesTargetSpec(
	candidate *retainedUITarget,
	spec TargetSpec,
	elements []retainedUITarget,
	byID map[string]int,
) (bool, bool) {
	if candidate == nil || candidate.expected.Sensitive || candidate.expected.Role != spec.Role ||
		candidate.expected.Name != spec.Name || !validUIBounds(candidate.expected.Bounds) ||
		!slices.Contains(candidate.expected.States, UIStateEnabled) || len(candidate.expected.Actions) == 0 ||
		!containsAllUIStates(candidate.expected.States, spec.RequiredStates) ||
		!containsAllUIActions(candidate.expected.Actions, spec.RequiredActions) {
		return false, false
	}
	parentID := candidate.parentID
	for _, ancestor := range spec.Ancestors {
		parentIndex, ok := byID[parentID]
		if !ok {
			return false, candidate.parentIncomplete
		}
		parent := &elements[parentIndex]
		if parent.expected.Sensitive {
			return false, true
		}
		if parent.expected.Role != ancestor.Role || parent.expected.Name != ancestor.Name ||
			!containsAllUIStates(parent.expected.States, ancestor.RequiredStates) {
			return false, false
		}
		candidate = parent
		parentID = parent.parentID
	}
	return true, false
}

func adaptiveTargetScore(candidate *retainedUITarget, spec TargetSpec, elements []retainedUITarget, byID map[string]int) (uint32, []TargetEvidence, bool) {
	if candidate == nil || candidate.expected.Sensitive || candidate.expected.Role != spec.Role ||
		!validUIBounds(candidate.expected.Bounds) || !slices.Contains(candidate.expected.States, UIStateEnabled) ||
		len(candidate.expected.Actions) == 0 || !containsAllUIStates(candidate.expected.States, spec.RequiredStates) ||
		!containsAllUIActions(candidate.expected.Actions, spec.RequiredActions) {
		return 0, nil, false
	}
	score := uint32(adaptiveBaseScore) // exact window, role, required states, and actions
	changed := make([]TargetEvidence, 0, 2)
	if candidate.expected.Name == spec.Name {
		score += adaptiveNameScore
	} else {
		changed = append(changed, TargetEvidenceName)
	}
	if len(spec.Ancestors) == 0 {
		return score + adaptiveAncestorScore, changed, false
	}
	matchedAncestors := uint32(0)
	parentID := candidate.parentID
	for _, ancestor := range spec.Ancestors {
		parentIndex, ok := byID[parentID]
		if !ok {
			return 0, nil, candidate.parentIncomplete
		}
		parent := &elements[parentIndex]
		if parent.expected.Sensitive {
			return 0, nil, true
		}
		if parent.expected.Role != ancestor.Role ||
			!containsAllUIStates(parent.expected.States, ancestor.RequiredStates) {
			return 0, nil, false
		}
		if parent.expected.Name == ancestor.Name {
			matchedAncestors++
		}
		candidate = parent
		parentID = parent.parentID
	}
	if matchedAncestors != uint32(len(spec.Ancestors)) {
		changed = append(changed, TargetEvidenceAncestors)
	}
	score += adaptiveAncestorScore * matchedAncestors / uint32(len(spec.Ancestors))
	return score, changed, false
}

func containsAllUIStates(actual, required []UIState) bool {
	for _, value := range required {
		if !slices.Contains(actual, value) {
			return false
		}
	}
	return true
}

func containsAllUIActions(actual, required []UIAction) bool {
	for _, value := range required {
		if !slices.Contains(actual, value) {
			return false
		}
	}
	return true
}

func targetResolutionStrategy(spec TargetSpec) TargetResolutionStrategy {
	if len(spec.Ancestors) > 0 {
		return TargetResolutionStructuralSemantic
	}
	return TargetResolutionExactSemantic
}

func targetResolutionEvidence(spec TargetSpec) []TargetEvidence {
	evidence := []TargetEvidence{
		TargetEvidenceWindowIdentity,
		TargetEvidenceRole,
		TargetEvidenceName,
	}
	if len(spec.RequiredStates) > 0 {
		evidence = append(evidence, TargetEvidenceStates)
	}
	if len(spec.RequiredActions) > 0 {
		evidence = append(evidence, TargetEvidenceActions)
	}
	if len(spec.Ancestors) > 0 {
		evidence = append(evidence, TargetEvidenceAncestors)
	}
	return evidence
}

func targetResolutionAuditEvent(kind AuditKind, result TargetResolutionResult, code ErrorCode) AuditEvent {
	event := AuditEvent{
		Kind: kind, Operation: OperationResolveUI, ObservationID: result.ObservationID,
		TargetResolutionStrategy: result.Strategy,
		TargetResolutionMode:     result.Mode,
		TargetCandidateCount:     result.CandidateCount,
		TargetRejectedCount:      result.RejectedCandidateCount,
		TargetAmbiguous:          result.Ambiguous,
		TargetEvidenceSources:    append([]TargetEvidenceSource(nil), result.EvidenceSources...),
		TargetEvidenceAgeMillis:  result.EvidenceAgeMillis,
		ErrorCode:                code,
	}
	if result.Patch != nil {
		event.TargetAdaptiveScore = result.Patch.Score
		event.TargetAdaptiveThreshold = result.Patch.Threshold
	}
	if result.AdaptiveScore > 0 {
		event.TargetAdaptiveScore = result.AdaptiveScore
		event.TargetAdaptiveThreshold = result.AdaptiveThreshold
	}
	event.CapabilityLeaseIssued = result.Lease != nil
	if kind == AuditResolutionFinished {
		event.TargetMatchedBy = append([]TargetEvidence(nil), result.MatchedBy...)
		event.TargetChanged = append([]TargetEvidence(nil), result.Changed...)
	}
	return event
}

func (s *Session) finishTargetResolution(
	ctx context.Context,
	result TargetResolutionResult,
	operationErr error,
) (TargetResolutionResult, error) {
	auditCtx := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		auditCtx, cancel = context.WithTimeout(context.Background(), uiCompletionAuditTimeout)
	}
	defer cancel()
	if auditErr := s.emitAudit(auditCtx, targetResolutionAuditEvent(
		AuditResolutionFinished,
		result,
		classifyTargetResolutionError(operationErr),
	)); auditErr != nil {
		if result.Lease != nil {
			s.invalidateIssuedCapabilityLease(result.Lease.ID)
		}
		return clearTargetSelection(result), targetResolutionError(
			ErrorAuditDelivery,
			"semantic target resolution completed but audit delivery failed",
			errors.Join(operationErr, auditErr),
		)
	}
	return result, operationErr
}

func clearTargetSelection(result TargetResolutionResult) TargetResolutionResult {
	result.ElementID = ""
	if result.Expected != nil {
		*result.Expected = UIElementExpectation{}
		result.Expected = nil
	}
	if result.Lease != nil {
		result.Lease.ID = ""
		result.Lease = nil
	}
	result.Patch = nil
	clear(result.Changed)
	result.Changed = nil
	return result
}

func cloneRetainedUITargets(source []retainedUITarget) []retainedUITarget {
	result := make([]retainedUITarget, len(source))
	for index := range source {
		result[index] = retainedUITarget{
			elementID:        source[index].elementID,
			parentID:         source[index].parentID,
			parentIncomplete: source[index].parentIncomplete,
			expected:         cloneUIElementExpectation(source[index].expected),
		}
	}
	return result
}

func clearRetainedUITargets(targets []retainedUITarget) {
	for index := range targets {
		clear(targets[index].expected.States)
		clear(targets[index].expected.Actions)
		targets[index] = retainedUITarget{}
	}
	clear(targets)
}

func targetResolutionError(code ErrorCode, message string, cause error) *ActionError {
	return newActionError(code, OperationResolveUI, message, cause)
}

func targetResolutionOperationError(err error) error {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return &ActionError{Code: actionErr.Code, Operation: OperationResolveUI, Message: actionErr.Message, cause: err}
	}
	code, message := classifyBackendError(err)
	return targetResolutionError(code, message, err)
}

func targetResolutionContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return targetResolutionError(ErrorTimedOut, "semantic target resolution deadline exceeded", ctx.Err())
	}
	return targetResolutionError(ErrorCanceled, "semantic target resolution canceled", ctx.Err())
}

func (s *Session) targetResolutionExecutionError(ctx context.Context) error {
	if ctx.Err() != nil {
		return targetResolutionContextError(ctx)
	}
	if s.ctx.Err() != nil {
		return targetResolutionError(ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
	}
	return nil
}

func classifyTargetResolutionError(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return actionErr.Code
	}
	code, _ := classifyBackendError(err)
	return code
}
