package agent

import (
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"time"
)

const (
	// VerifiedFlowPlanSchemaVersion identifies the advisory verified-flow
	// capability-planning contract. Plans never grant execution authority.
	VerifiedFlowPlanSchemaVersion = "1"
	maxVerifiedFlowPlanSteps      = 128
)

// FlowFeasibilityStatus is the closed outcome vocabulary for one planned
// transaction or the complete flow.
type FlowFeasibilityStatus string

const (
	FlowFeasible              FlowFeasibilityStatus = "feasible"
	FlowConditionallyFeasible FlowFeasibilityStatus = "conditionally-feasible"
	FlowUnsupported           FlowFeasibilityStatus = "unsupported"
	FlowPolicyDenied          FlowFeasibilityStatus = "policy-denied"
	FlowAmbiguous             FlowFeasibilityStatus = "ambiguous"
	FlowStale                 FlowFeasibilityStatus = "stale"
	FlowEvidenceRequired      FlowFeasibilityStatus = "evidence-required"
)

// FlowPlanBlocker is a fixed, privacy-safe reason why a step is not presently
// feasible. It never contains target text, identifiers, tokens, or raw errors.
type FlowPlanBlocker string

const (
	FlowBlockerOperationUnavailable    FlowPlanBlocker = "operation-unavailable"
	FlowBlockerPermissionRequired      FlowPlanBlocker = "permission-required"
	FlowBlockerOperationDenied         FlowPlanBlocker = "operation-policy-denied"
	FlowBlockerWindowDenied            FlowPlanBlocker = "window-policy-denied"
	FlowBlockerRoleDenied              FlowPlanBlocker = "role-policy-denied"
	FlowBlockerTargetBoundsDenied      FlowPlanBlocker = "target-bounds-policy-denied"
	FlowBlockerPropertyDenied          FlowPlanBlocker = "property-policy-denied"
	FlowBlockerActionDenied            FlowPlanBlocker = "action-policy-denied"
	FlowBlockerModeDenied              FlowPlanBlocker = "mode-policy-denied"
	FlowBlockerConfirmationRequired    FlowPlanBlocker = "confirmation-required"
	FlowBlockerQuotaInsufficient       FlowPlanBlocker = "quota-insufficient"
	FlowBlockerRateWaitRequired        FlowPlanBlocker = "policy-rate-wait-required"
	FlowBlockerSessionTimeInsufficient FlowPlanBlocker = "session-lifetime-insufficient"
	FlowBlockerClockSkew               FlowPlanBlocker = "policy-clock-skew"
	FlowBlockerLeaseRequired           FlowPlanBlocker = "capability-lease-required"
	FlowBlockerLeaseDurationDenied     FlowPlanBlocker = "capability-lease-duration-denied"
	FlowBlockerObservationRequired     FlowPlanBlocker = "observation-required"
	FlowBlockerObservationStale        FlowPlanBlocker = "observation-stale"
	FlowBlockerObservationIncomplete   FlowPlanBlocker = "observation-incomplete"
	FlowBlockerTargetNotFound          FlowPlanBlocker = "target-not-found"
	FlowBlockerTargetAmbiguous         FlowPlanBlocker = "target-ambiguous"
	FlowBlockerEvidenceStale           FlowPlanBlocker = "target-evidence-stale"
	FlowBlockerEvidenceIncomplete      FlowPlanBlocker = "target-evidence-incomplete"
	FlowBlockerEvidenceDenied          FlowPlanBlocker = "target-evidence-policy-denied"
	FlowBlockerTraceDenied             FlowPlanBlocker = "trace-policy-denied"
	FlowBlockerTraceExportUnavailable  FlowPlanBlocker = "trace-export-unavailable"
	FlowBlockerVerificationRequired    FlowPlanBlocker = "verification-required"
	FlowBlockerReviewRequired          FlowPlanBlocker = "operator-review-required"
)

// FlowPlanRemediation is a fixed advisory hint. Applying one never changes the
// immutable session policy and must happen outside the planner.
type FlowPlanRemediation string

const (
	FlowRemediationEnableBackend        FlowPlanRemediation = "enable-supported-backend"
	FlowRemediationGrantPermission      FlowPlanRemediation = "grant-permission-outside-planner"
	FlowRemediationAllowOperation       FlowPlanRemediation = "allow-operation-in-policy"
	FlowRemediationAllowWindow          FlowPlanRemediation = "allow-window-in-policy"
	FlowRemediationAllowRole            FlowPlanRemediation = "allow-role-in-policy"
	FlowRemediationReduceTargetBounds   FlowPlanRemediation = "reduce-target-to-policy-bounds"
	FlowRemediationAllowProperty        FlowPlanRemediation = "allow-property-in-policy"
	FlowRemediationAllowAction          FlowPlanRemediation = "allow-action-in-policy"
	FlowRemediationAllowMode            FlowPlanRemediation = "allow-resolution-mode-in-policy"
	FlowRemediationObtainConfirmation   FlowPlanRemediation = "obtain-operator-confirmation"
	FlowRemediationIncreaseQuota        FlowPlanRemediation = "start-session-with-sufficient-quota"
	FlowRemediationWaitForRateLimit     FlowPlanRemediation = "wait-for-policy-rate-limit"
	FlowRemediationStartLongerSession   FlowPlanRemediation = "start-session-with-sufficient-lifetime"
	FlowRemediationCorrectClock         FlowPlanRemediation = "restore-monotonic-session-clock"
	FlowRemediationRequestLease         FlowPlanRemediation = "request-fresh-capability-lease"
	FlowRemediationReduceLeaseLifetime  FlowPlanRemediation = "reduce-capability-lease-lifetime"
	FlowRemediationProvideObservation   FlowPlanRemediation = "provide-live-semantic-observation"
	FlowRemediationRefreshObservation   FlowPlanRemediation = "refresh-semantic-observation"
	FlowRemediationRefineTarget         FlowPlanRemediation = "refine-target-specification"
	FlowRemediationProvideEvidence      FlowPlanRemediation = "provide-reviewed-target-evidence"
	FlowRemediationAllowEvidence        FlowPlanRemediation = "allow-target-evidence-in-policy"
	FlowRemediationAllowTrace           FlowPlanRemediation = "allow-trace-tier-in-policy"
	FlowRemediationConfigureTraceExport FlowPlanRemediation = "configure-trace-export-sink"
	FlowRemediationAddPostcondition     FlowPlanRemediation = "add-semantic-postcondition"
	FlowRemediationReviewTarget         FlowPlanRemediation = "review-target-change-before-execution"
)

// FlowCleanupObligation is one resource-lifecycle duty of later execution.
type FlowCleanupObligation string

const (
	FlowCleanupTransientResources FlowCleanupObligation = "release-transient-resources"
	FlowCleanupCapabilityLease    FlowCleanupObligation = "invalidate-capability-lease"
	FlowCleanupObservation        FlowCleanupObligation = "release-observation"
)

// FlowPlatformLimitation is a fixed capability limitation; catalog reason and
// remediation strings are deliberately never copied into planner output.
type FlowPlatformLimitation string

const (
	FlowPlatformSemanticInspectionUnavailable FlowPlatformLimitation = "semantic-inspection-unavailable"
	FlowPlatformSemanticActionUnavailable     FlowPlatformLimitation = "semantic-action-unavailable"
	FlowPlatformSemanticCheckUnavailable      FlowPlatformLimitation = "semantic-verification-unavailable"
	FlowPlatformTraceExportUnavailable        FlowPlatformLimitation = "trace-export-unavailable"
)

// VerifiedFlowPlanStep describes one future resolve -> act -> verify
// transaction. It contains no element identity, native reference, action
// value, capability token, or confirmation grant.
type VerifiedFlowPlanStep struct {
	ObservationID                 string               `json:"observation_id,omitempty"`
	Target                        TargetSpec           `json:"target"`
	Mode                          TargetResolutionMode `json:"mode,omitempty"`
	Action                        UIAction             `json:"action"`
	ActionValueBytes              uint32               `json:"action_value_bytes,omitempty"`
	Postcondition                 *UIElementCondition  `json:"postcondition,omitempty"`
	CapabilityLeaseDurationMillis int                  `json:"capability_lease_duration_ms,omitempty"`
	Trace                         *TraceRequest        `json:"trace,omitempty"`
}

// VerifiedFlowPlanRequest pins every version consumed by a bounded advisory
// plan. Catalog drift fails before session state is inspected.
type VerifiedFlowPlanRequest struct {
	SchemaVersion          string                 `json:"schema_version"`
	CatalogVersion         string                 `json:"catalog_version"`
	TargetSpecVersion      string                 `json:"target_spec_version"`
	CapabilityLeaseVersion string                 `json:"capability_lease_version"`
	ActionProofVersion     string                 `json:"action_proof_version"`
	TraceVersion           string                 `json:"trace_version"`
	Steps                  []VerifiedFlowPlanStep `json:"steps"`
}

// FlowQuotaRequirement is a conservative upper bound for one later execution.
// Planning never reserves or consumes any of these counters.
type FlowQuotaRequirement struct {
	Actions      uint64 `json:"actions"`
	Queries      uint64 `json:"queries"`
	Observations uint64 `json:"observations"`
	Leases       uint64 `json:"leases"`
}

// FlowTimingRequirement is a lower-bound schedule derived from current rate
// gates and the remaining immutable session lifetime. It is advisory only.
type FlowTimingRequirement struct {
	ActionReadyDelayMillis    int64 `json:"action_ready_delay_ms"`
	QueryReadyDelayMillis     int64 `json:"query_ready_delay_ms"`
	MinimumVerifiedFlowMillis int64 `json:"minimum_verified_flow_ms"`
	SessionRemainingMillis    int64 `json:"session_remaining_ms"`
}

// FlowPlanAssumption is a fixed, privacy-safe condition that execution must
// revalidate because planning cannot reserve time, evidence, or runtime state.
type FlowPlanAssumption string

const (
	FlowAssumptionSessionRemainsOpen     FlowPlanAssumption = "session-remains-open"
	FlowAssumptionRuntimeRevalidated     FlowPlanAssumption = "runtime-state-revalidated"
	FlowAssumptionObservationRemainsLive FlowPlanAssumption = "observation-remains-live"
	FlowAssumptionEvidenceRemainsFresh   FlowPlanAssumption = "target-evidence-remains-fresh"
	FlowAssumptionRateScheduleHonored    FlowPlanAssumption = "policy-rate-schedule-honored"
)

// FlowBackendRequirement is one catalog-pinned backend dependency.
type FlowBackendRequirement struct {
	Operation Operation `json:"operation"`
	Backend   string    `json:"backend"`
	Fallback  bool      `json:"fallback"`
}

// FlowPlanRequirements is the deterministic union of authority, runtime, and
// cleanup prerequisites for a step or complete flow.
type FlowPlanRequirements struct {
	Operations                    []Operation              `json:"operations"`
	UIRoles                       []UIRole                 `json:"ui_roles"`
	UIProperties                  []UIProperty             `json:"ui_properties"`
	UIActions                     []UIAction               `json:"ui_actions"`
	TargetModes                   []TargetResolutionMode   `json:"target_modes"`
	EvidenceSources               []TargetEvidenceSource   `json:"evidence_sources,omitempty"`
	TraceTiers                    []TracePrivacyTier       `json:"trace_tiers,omitempty"`
	Confirmations                 []Operation              `json:"confirmations,omitempty"`
	Backends                      []FlowBackendRequirement `json:"backends"`
	Cleanup                       []FlowCleanupObligation  `json:"cleanup"`
	PlatformLimitations           []FlowPlatformLimitation `json:"platform_limitations,omitempty"`
	Assumptions                   []FlowPlanAssumption     `json:"assumptions"`
	Quota                         FlowQuotaRequirement     `json:"quota"`
	Timing                        FlowTimingRequirement    `json:"timing"`
	CapabilityLeaseRequired       bool                     `json:"capability_lease_required"`
	CapabilityLeaseDurationMillis int                      `json:"capability_lease_duration_ms,omitempty"`
	FreshObservationRequired      bool                     `json:"fresh_observation_required"`
	RuntimeRevalidationRequired   bool                     `json:"runtime_revalidation_required"`
}

// FlowPlanStepReport identifies a step only by its stable one-based position.
// Caller-controlled target and observation identifiers are never echoed.
type FlowPlanStepReport struct {
	Sequence               uint32                `json:"sequence"`
	Status                 FlowFeasibilityStatus `json:"status"`
	CandidateCount         uint32                `json:"candidate_count"`
	RejectedCandidateCount uint32                `json:"rejected_candidate_count"`
	Requirements           FlowPlanRequirements  `json:"requirements"`
	Blockers               []FlowPlanBlocker     `json:"blockers,omitempty"`
	Remediations           []FlowPlanRemediation `json:"remediations,omitempty"`
}

// VerifiedFlowPlanReport is deterministic for the same immutable session
// snapshot and clock. AdvisoryOnly is always true and the report can never be
// supplied as action authority.
type VerifiedFlowPlanReport struct {
	SchemaVersion  string                `json:"schema_version"`
	CatalogVersion string                `json:"catalog_version"`
	Status         FlowFeasibilityStatus `json:"status"`
	AdvisoryOnly   bool                  `json:"advisory_only"`
	Requirements   FlowPlanRequirements  `json:"requirements"`
	Steps          []FlowPlanStepReport  `json:"steps"`
}

type flowPlannerSnapshot struct {
	at             time.Time
	graphs         map[string]retainedUITargetGraph
	evidence       []retainedTargetEvidenceBundle
	evidenceErrors []error
}

// PlanVerifiedFlow evaluates a bounded verified-flow description against this
// session's immutable catalog and policy plus already-retained observations.
// It never calls a desktop backend, emits audit/trace/recorder events, issues a
// lease, reserves authority, consumes quota, or prompts for permission.
func (s *Session) PlanVerifiedFlow(ctx context.Context, request VerifiedFlowPlanRequest) (VerifiedFlowPlanReport, error) {
	report := VerifiedFlowPlanReport{
		SchemaVersion: VerifiedFlowPlanSchemaVersion, CatalogVersion: CatalogSchemaVersion,
		Status: FlowFeasible, AdvisoryOnly: true,
	}
	if s == nil {
		return report, flowPlannerError(ErrorSessionClosed, "verified-flow planner session is unavailable", ErrSessionClosed)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateVerifiedFlowPlanRequest(request); err != nil {
		return report, flowPlannerError(ErrorInvalidInput, "invalid verified-flow plan", err)
	}
	if err := ctx.Err(); err != nil {
		return report, flowPlannerContextError(ctx)
	}
	if err := s.acquire(ctx); err != nil {
		return report, flowPlannerOperationError(err)
	}
	defer s.release()
	if err := s.ensureOpen(); err != nil {
		return report, flowPlannerOperationError(err)
	}
	snapshot := s.flowPlannerSnapshot(request)
	defer clearFlowPlannerSnapshot(&snapshot)
	if err := ctx.Err(); err != nil {
		return report, flowPlannerContextError(ctx)
	}
	if err := s.ensureOpen(); err != nil {
		return report, flowPlannerOperationError(err)
	}

	report.Steps = make([]FlowPlanStepReport, 0, len(request.Steps))
	var cumulative FlowQuotaRequirement
	var cumulativeMinimumQueries uint64
	for index := range request.Steps {
		if err := ctx.Err(); err != nil {
			return VerifiedFlowPlanReport{
				SchemaVersion: VerifiedFlowPlanSchemaVersion, CatalogVersion: CatalogSchemaVersion,
				Status: FlowFeasible, AdvisoryOnly: true,
			}, flowPlannerContextError(ctx)
		}
		if err := s.ensureOpen(); err != nil {
			return VerifiedFlowPlanReport{
				SchemaVersion: VerifiedFlowPlanSchemaVersion, CatalogVersion: CatalogSchemaVersion,
				Status: FlowFeasible, AdvisoryOnly: true,
			}, flowPlannerOperationError(err)
		}
		stepReport := s.planVerifiedFlowStep(uint32(index+1), request.Steps[index], &snapshot, index)
		if err := ctx.Err(); err != nil {
			return VerifiedFlowPlanReport{
				SchemaVersion: VerifiedFlowPlanSchemaVersion, CatalogVersion: CatalogSchemaVersion,
				Status: FlowFeasible, AdvisoryOnly: true,
			}, flowPlannerContextError(ctx)
		}
		if err := s.ensureOpen(); err != nil {
			return VerifiedFlowPlanReport{
				SchemaVersion: VerifiedFlowPlanSchemaVersion, CatalogVersion: CatalogSchemaVersion,
				Status: FlowFeasible, AdvisoryOnly: true,
			}, flowPlannerOperationError(err)
		}
		cumulative = addFlowQuota(cumulative, stepReport.Requirements.Quota)
		cumulativeMinimumQueries = saturatingFlowAdd(
			cumulativeMinimumQueries, minimumVerifiedFlowQueries(request.Steps[index]))
		s.applyFlowQuotaCapacity(&stepReport, cumulative)
		s.applyFlowTimingCapacity(&stepReport, cumulative, cumulativeMinimumQueries, snapshot.at)
		report.Steps = append(report.Steps, stepReport)
		report.Status = worseFlowStatus(report.Status, stepReport.Status)
		mergeFlowRequirements(&report.Requirements, stepReport.Requirements)
	}
	report.Requirements.Quota = cumulative
	report.Requirements.Timing, _ = s.flowTimingRequirement(cumulativeMinimumQueries, snapshot.at)
	normalizeFlowRequirements(&report.Requirements)
	return report, nil
}

func (s *Session) flowPlannerSnapshot(request VerifiedFlowPlanRequest) flowPlannerSnapshot {
	snapshot := flowPlannerSnapshot{
		at:             s.now(),
		graphs:         make(map[string]retainedUITargetGraph),
		evidence:       make([]retainedTargetEvidenceBundle, len(request.Steps)),
		evidenceErrors: make([]error, len(request.Steps)),
	}
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	for index, step := range request.Steps {
		if step.ObservationID != "" {
			if _, seen := snapshot.graphs[step.ObservationID]; !seen {
				if graph, ok := s.retainUITargetGraphLocked(step.ObservationID); ok {
					snapshot.graphs[step.ObservationID] = graph
				}
			}
		}
		if len(step.Target.Evidence) > 0 {
			snapshot.evidence[index], snapshot.evidenceErrors[index] =
				s.retainTargetEvidenceBundleLocked(step.Target.Evidence)
		}
	}
	return snapshot
}

func clearFlowPlannerSnapshot(snapshot *flowPlannerSnapshot) {
	if snapshot == nil {
		return
	}
	for observationID, graph := range snapshot.graphs {
		clearRetainedUITargets(graph.elements)
		delete(snapshot.graphs, observationID)
	}
	for index := range snapshot.evidence {
		clearRetainedTargetEvidenceBundle(&snapshot.evidence[index])
	}
	clear(snapshot.evidenceErrors)
	snapshot.evidence = nil
	snapshot.evidenceErrors = nil
	snapshot.at = time.Time{}
}

func validateVerifiedFlowPlanRequest(request VerifiedFlowPlanRequest) error {
	if request.SchemaVersion != VerifiedFlowPlanSchemaVersion || request.CatalogVersion != CatalogSchemaVersion ||
		request.TargetSpecVersion != TargetSpecSchemaVersion || request.CapabilityLeaseVersion != CapabilityLeaseSchemaVersion ||
		request.ActionProofVersion != ActionProofSchemaVersion || request.TraceVersion != RobotGoTraceSchemaVersion ||
		len(request.Steps) == 0 || len(request.Steps) > maxVerifiedFlowPlanSteps {
		return errors.New("unsupported schema version or step count")
	}
	for _, step := range request.Steps {
		mode := normalizeTargetResolutionMode(step.Mode)
		if step.ObservationID != "" && !validObservationID(step.ObservationID) {
			return errors.New("invalid semantic observation ID")
		}
		if err := validateTargetSpecForMode(step.Target, mode); err != nil {
			return err
		}
		if !validUIAction(step.Action) || !slices.Contains(step.Target.RequiredActions, step.Action) ||
			validateUIElementCondition(step.Action, step.Postcondition) != nil {
			return errors.New("invalid semantic action or postcondition")
		}
		if step.Action != UIActionSetValue && step.ActionValueBytes != 0 {
			return errors.New("only set-value accepts an action value size")
		}
		if step.ActionValueBytes > maxAgentUIActionValueBytes || step.CapabilityLeaseDurationMillis < 0 ||
			step.CapabilityLeaseDurationMillis > maxAgentCapabilityLeaseMillis {
			return errors.New("planned action value or capability lease exceeds hard bounds")
		}
		if step.Trace != nil && (step.Trace.SchemaVersion != TraceRequestSchemaVersion || !validTracePrivacyTier(step.Trace.Tier)) {
			return errors.New("invalid transaction trace request")
		}
	}
	return nil
}

func (s *Session) planVerifiedFlowStep(
	sequence uint32,
	step VerifiedFlowPlanStep,
	snapshot *flowPlannerSnapshot,
	stepIndex int,
) FlowPlanStepReport {
	mode := normalizeTargetResolutionMode(step.Mode)
	report := FlowPlanStepReport{Sequence: sequence, Status: FlowFeasible}
	report.Requirements = s.flowStepRequirements(step, mode)

	for _, operation := range report.Requirements.Operations {
		capability, ok := s.capability(operation)
		if !ok || !capability.Available || capability.Backend == "" {
			limitation := FlowPlatformSemanticInspectionUnavailable
			if operation == OperationElementAct {
				limitation = FlowPlatformSemanticActionUnavailable
			}
			report.Requirements.PlatformLimitations = appendUniqueFlowPlatformLimitation(report.Requirements.PlatformLimitations, limitation)
			if ok && capability.UnavailableCode == ErrorPermissionDenied {
				addFlowIssue(&report, FlowEvidenceRequired, FlowBlockerPermissionRequired, FlowRemediationGrantPermission)
			} else {
				addFlowIssue(&report, FlowUnsupported, FlowBlockerOperationUnavailable, FlowRemediationEnableBackend)
			}
		}
		if _, allowed := s.policy.allowOperation[operation]; !allowed {
			addFlowIssue(&report, FlowPolicyDenied, FlowBlockerOperationDenied, FlowRemediationAllowOperation)
		}
		if _, required := s.policy.requireConfirmation[operation]; required {
			addFlowIssue(&report, FlowConditionallyFeasible, FlowBlockerConfirmationRequired, FlowRemediationObtainConfirmation)
		}
	}

	policyWindow, windowAllowed := s.policy.allowWindow[windowTargetIdentity{
		target: step.Target.Window.Target, kind: step.Target.Window.Kind,
	}]
	if !windowAllowed || policyWindow.ExpectedTitle != step.Target.Window.ExpectedTitle {
		addFlowIssue(&report, FlowPolicyDenied, FlowBlockerWindowDenied, FlowRemediationAllowWindow)
	}
	if targetSpecExceedsPolicyBounds(step.Target, s.policy) {
		addFlowIssue(&report, FlowPolicyDenied, FlowBlockerTargetBoundsDenied, FlowRemediationReduceTargetBounds)
	}
	for _, role := range report.Requirements.UIRoles {
		if _, allowed := s.policy.allowUIRole[role]; !allowed {
			addFlowIssue(&report, FlowPolicyDenied, FlowBlockerRoleDenied, FlowRemediationAllowRole)
		}
	}
	for _, property := range report.Requirements.UIProperties {
		if _, allowed := s.policy.allowUIProperty[property]; !allowed {
			addFlowIssue(&report, FlowPolicyDenied, FlowBlockerPropertyDenied, FlowRemediationAllowProperty)
		}
	}
	if _, allowed := s.policy.allowUIAction[step.Action]; !allowed ||
		step.ActionValueBytes > s.policy.MaxUIActionValueBytes {
		addFlowIssue(&report, FlowPolicyDenied, FlowBlockerActionDenied, FlowRemediationAllowAction)
	}
	if _, allowed := s.policy.allowTargetMode[mode]; !allowed {
		addFlowIssue(&report, FlowPolicyDenied, FlowBlockerModeDenied, FlowRemediationAllowMode)
	}
	for _, source := range report.Requirements.EvidenceSources {
		if _, allowed := s.policy.allowTargetEvidenceSource[source]; !allowed {
			addFlowIssue(&report, FlowPolicyDenied, FlowBlockerEvidenceDenied, FlowRemediationAllowEvidence)
		}
	}

	leaseRequired := report.Requirements.CapabilityLeaseRequired
	if leaseRequired && step.CapabilityLeaseDurationMillis == 0 {
		addFlowIssue(&report, FlowConditionallyFeasible, FlowBlockerLeaseRequired, FlowRemediationRequestLease)
	}
	if step.CapabilityLeaseDurationMillis > s.policy.MaxCapabilityLeaseMillis {
		addFlowIssue(&report, FlowPolicyDenied, FlowBlockerLeaseDurationDenied, FlowRemediationReduceLeaseLifetime)
	}
	if mode == TargetResolutionModeReview {
		addFlowIssue(&report, FlowEvidenceRequired, FlowBlockerReviewRequired, FlowRemediationReviewTarget)
	}

	if step.Postcondition == nil {
		addFlowIssue(&report, FlowEvidenceRequired, FlowBlockerVerificationRequired, FlowRemediationAddPostcondition)
	} else {
		if s.policy.UIVerificationAttempts == 0 || s.policy.UIVerificationTimeoutMillis == 0 {
			addFlowIssue(&report, FlowPolicyDenied, FlowBlockerPropertyDenied, FlowRemediationAllowProperty)
		}
		if _, ok := s.driver.(uiElementCheckDriver); !ok {
			report.Requirements.PlatformLimitations = appendUniqueFlowPlatformLimitation(
				report.Requirements.PlatformLimitations, FlowPlatformSemanticCheckUnavailable)
			addFlowIssue(&report, FlowUnsupported, FlowBlockerOperationUnavailable, FlowRemediationEnableBackend)
		}
	}
	if _, ok := s.driver.(uiElementActDriver); !ok {
		report.Requirements.PlatformLimitations = appendUniqueFlowPlatformLimitation(
			report.Requirements.PlatformLimitations, FlowPlatformSemanticActionUnavailable)
		addFlowIssue(&report, FlowUnsupported, FlowBlockerOperationUnavailable, FlowRemediationEnableBackend)
	}

	if step.Trace != nil {
		if _, allowed := s.policy.allowTraceTier[step.Trace.Tier]; !allowed {
			addFlowIssue(&report, FlowPolicyDenied, FlowBlockerTraceDenied, FlowRemediationAllowTrace)
		}
		if step.Trace.Export && (!s.policy.AllowTraceExport || s.traceSink == nil) {
			report.Requirements.PlatformLimitations = appendUniqueFlowPlatformLimitation(
				report.Requirements.PlatformLimitations, FlowPlatformTraceExportUnavailable)
			addFlowIssue(&report, FlowPolicyDenied, FlowBlockerTraceExportUnavailable, FlowRemediationConfigureTraceExport)
		}
	}

	if step.ObservationID == "" {
		addFlowIssue(&report, FlowEvidenceRequired, FlowBlockerObservationRequired, FlowRemediationProvideObservation)
	} else {
		s.planFlowTarget(&report, step, mode, snapshot, stepIndex)
	}
	normalizeFlowRequirements(&report.Requirements)
	return report
}

func (s *Session) flowStepRequirements(step VerifiedFlowPlanStep, mode TargetResolutionMode) FlowPlanRequirements {
	result := FlowPlanRequirements{
		Operations: []Operation{OperationInspectUI, OperationResolveUI, OperationElementAct},
		UIRoles:    []UIRole{step.Target.Role}, UIActions: []UIAction{step.Action},
		TargetModes:  []TargetResolutionMode{mode},
		UIProperties: []UIProperty{UIPropertyRole, UIPropertyName, UIPropertyState, UIPropertyBounds, UIPropertyActions},
		Cleanup:      []FlowCleanupObligation{FlowCleanupTransientResources, FlowCleanupObservation},
		Quota:        FlowQuotaRequirement{Actions: 1}, RuntimeRevalidationRequired: true,
		Assumptions: []FlowPlanAssumption{
			FlowAssumptionSessionRemainsOpen, FlowAssumptionRuntimeRevalidated,
			FlowAssumptionObservationRemainsLive,
		},
		CapabilityLeaseDurationMillis: step.CapabilityLeaseDurationMillis,
	}
	for _, ancestor := range step.Target.Ancestors {
		result.UIRoles = appendUniqueUIRole(result.UIRoles, ancestor.Role)
	}
	if len(step.Target.Ancestors) > 0 {
		result.UIProperties = appendUniqueUIProperty(result.UIProperties, UIPropertyHierarchy)
	}
	if step.Postcondition != nil {
		result.Quota.Queries = maximumUIElementActionReads(s.policy.UIVerificationAttempts)
		result.Quota.Observations = result.Quota.Queries
		switch step.Postcondition.Kind {
		case UIElementConditionFocused, UIElementConditionNotFocused:
			result.UIProperties = appendUniqueUIProperty(result.UIProperties, UIPropertyFocus)
		case UIElementConditionValueEqualsActionValue:
			result.UIProperties = appendUniqueUIProperty(result.UIProperties, UIPropertyValue)
		}
	}
	if step.ObservationID == "" {
		result.FreshObservationRequired = true
		result.Quota.Queries++
		result.Quota.Observations++
	}
	result.CapabilityLeaseRequired = mode != TargetResolutionModeReview &&
		(s.policy.RequireCapabilityLease || mode == TargetResolutionModeAdaptive ||
			step.CapabilityLeaseDurationMillis > 0)
	if result.CapabilityLeaseRequired && mode != TargetResolutionModeReview {
		result.Quota.Leases = 1
		result.Cleanup = appendUniqueFlowCleanup(result.Cleanup, FlowCleanupCapabilityLease)
	}
	for _, clause := range step.Target.Evidence {
		if !slices.Contains(result.EvidenceSources, clause.Source) {
			result.EvidenceSources = append(result.EvidenceSources, clause.Source)
		}
	}
	if len(step.Target.Evidence) > 0 {
		result.Assumptions = append(result.Assumptions, FlowAssumptionEvidenceRemainsFresh)
	}
	if s.policy.MinActionIntervalMillis > 0 || s.policy.MinUIQueryIntervalMillis > 0 {
		result.Assumptions = append(result.Assumptions, FlowAssumptionRateScheduleHonored)
	}
	if step.Trace != nil {
		result.TraceTiers = []TracePrivacyTier{step.Trace.Tier}
	}
	for _, operation := range result.Operations {
		if _, required := s.policy.requireConfirmation[operation]; required {
			result.Confirmations = append(result.Confirmations, operation)
		}
		if capability, ok := s.capability(operation); ok {
			result.Backends = append(result.Backends, FlowBackendRequirement{
				Operation: operation, Backend: capability.Backend, Fallback: capability.Fallback,
			})
		}
	}
	return result
}

func (s *Session) planFlowTarget(
	report *FlowPlanStepReport,
	step VerifiedFlowPlanStep,
	mode TargetResolutionMode,
	snapshot *flowPlannerSnapshot,
	stepIndex int,
) {
	graph, ok := snapshot.graphs[step.ObservationID]
	if !ok {
		addFlowIssue(report, FlowStale, FlowBlockerObservationStale, FlowRemediationRefreshObservation)
		return
	}
	if graph.target.Target != step.Target.Window.Target || graph.target.Kind != step.Target.Window.Kind ||
		graph.target.ExpectedTitle != step.Target.Window.ExpectedTitle {
		report.RejectedCandidateCount = uint32(len(graph.elements))
		addFlowIssue(report, FlowStale, FlowBlockerObservationStale, FlowRemediationRefreshObservation)
		return
	}
	if graph.incomplete || !graph.actionable {
		report.RejectedCandidateCount = uint32(len(graph.elements))
		addFlowIssue(report, FlowStale, FlowBlockerObservationIncomplete, FlowRemediationRefreshObservation)
		return
	}

	byID := make(map[string]int, len(graph.elements))
	for index := range graph.elements {
		byID[graph.elements[index].elementID] = index
	}
	incompleteCandidate := false
	for index := range graph.elements {
		candidate := &graph.elements[index]
		matches, incomplete := matchesTargetSpec(candidate, step.Target, graph.elements, byID)
		score, _, adaptiveIncomplete := adaptiveTargetScore(candidate, step.Target, graph.elements, byID)
		if mode != TargetResolutionModeStrict {
			matches = !adaptiveIncomplete && score >= s.policy.AdaptiveTargetThreshold
			incomplete = adaptiveIncomplete
		}
		if matches {
			report.CandidateCount++
		} else {
			report.RejectedCandidateCount++
			incompleteCandidate = incompleteCandidate || incomplete
		}
	}
	if report.CandidateCount <= 1 && incompleteCandidate {
		addFlowIssue(report, FlowStale, FlowBlockerObservationIncomplete, FlowRemediationRefreshObservation)
		return
	}

	if report.CandidateCount == 0 && len(step.Target.Evidence) > 0 {
		if snapshot.evidenceErrors[stepIndex] != nil {
			s.addFlowEvidenceError(report, snapshot.evidenceErrors[stepIndex])
			return
		}
		bundle := &snapshot.evidence[stepIndex]
		if err := s.authorizeTargetEvidenceAt(bundle, snapshot.at); err != nil {
			s.addFlowEvidenceError(report, err)
			return
		}
		report.CandidateCount = 0
		report.RejectedCandidateCount = 0
		incompleteCandidate = false
		for index := range graph.elements {
			candidate := &graph.elements[index]
			score, _, incomplete := adaptiveTargetScore(candidate, step.Target, graph.elements, byID)
			bonus, evidenceMatches := targetEvidenceCandidateScore(candidate, *bundle)
			if !incomplete && evidenceMatches {
				score = min(uint32(100), score+bonus)
			}
			if incomplete || !evidenceMatches || score < s.policy.AdaptiveTargetThreshold {
				report.RejectedCandidateCount++
				incompleteCandidate = incompleteCandidate || incomplete
				continue
			}
			report.CandidateCount++
		}
		if report.CandidateCount <= 1 && incompleteCandidate {
			addFlowIssue(report, FlowEvidenceRequired, FlowBlockerEvidenceIncomplete, FlowRemediationProvideEvidence)
			return
		}
	}

	switch {
	case report.CandidateCount == 0:
		addFlowIssue(report, FlowEvidenceRequired, FlowBlockerTargetNotFound, FlowRemediationRefineTarget)
	case report.CandidateCount > 1:
		addFlowIssue(report, FlowAmbiguous, FlowBlockerTargetAmbiguous, FlowRemediationRefineTarget)
	}
}

func (s *Session) addFlowEvidenceError(report *FlowPlanStepReport, err error) {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		switch actionErr.Code {
		case ErrorPolicyDenied:
			addFlowIssue(report, FlowPolicyDenied, FlowBlockerEvidenceDenied, FlowRemediationAllowEvidence)
		case ErrorIncompleteObservation:
			addFlowIssue(report, FlowEvidenceRequired, FlowBlockerEvidenceIncomplete, FlowRemediationProvideEvidence)
		case ErrorStaleTarget:
			addFlowIssue(report, FlowStale, FlowBlockerEvidenceStale, FlowRemediationProvideEvidence)
		default:
			addFlowIssue(report, FlowEvidenceRequired, FlowBlockerTargetNotFound, FlowRemediationProvideEvidence)
		}
		return
	}
	addFlowIssue(report, FlowEvidenceRequired, FlowBlockerEvidenceStale, FlowRemediationProvideEvidence)
}

func (s *Session) applyFlowQuotaCapacity(report *FlowPlanStepReport, cumulative FlowQuotaRequirement) {
	insufficient := cumulative.Actions > remainingFlowQuota(s.policy.MaxActions, s.used) ||
		cumulative.Queries > remainingFlowQuota(s.policy.MaxQueries, s.usedQueries) ||
		cumulative.Observations > remainingFlowQuota(s.policy.MaxObservations, s.usedObservations) ||
		cumulative.Leases > remainingFlowQuota(s.policy.MaxCapabilityLeases, s.usedLeases)
	if insufficient {
		addFlowIssue(report, FlowPolicyDenied, FlowBlockerQuotaInsufficient, FlowRemediationIncreaseQuota)
	}
}

func (s *Session) applyFlowTimingCapacity(
	report *FlowPlanStepReport,
	cumulative FlowQuotaRequirement,
	minimumQueries uint64,
	now time.Time,
) {
	timing, clockSkew := s.flowTimingRequirement(minimumQueries, now)
	report.Requirements.Timing = timing
	if clockSkew {
		addFlowIssue(report, FlowConditionallyFeasible, FlowBlockerClockSkew, FlowRemediationCorrectClock)
	}
	if _, hasDeadline := s.ctx.Deadline(); hasDeadline &&
		timing.MinimumVerifiedFlowMillis > timing.SessionRemainingMillis {
		addFlowIssue(report, FlowPolicyDenied, FlowBlockerSessionTimeInsufficient, FlowRemediationStartLongerSession)
		return
	}
	actionWaitRequired := timing.ActionReadyDelayMillis > 0
	freshQueryWaitRequired := report.Requirements.FreshObservationRequired &&
		timing.QueryReadyDelayMillis > 0
	interActionWaitRequired := cumulative.Actions > 1 && s.policy.MinActionIntervalMillis > 0
	if actionWaitRequired || freshQueryWaitRequired || interActionWaitRequired {
		addFlowIssue(report, FlowConditionallyFeasible, FlowBlockerRateWaitRequired, FlowRemediationWaitForRateLimit)
	}
}

func (s *Session) flowTimingRequirement(minimumQueries uint64, now time.Time) (FlowTimingRequirement, bool) {
	actionDelay, actionClockSkew := flowRateDelay(now, s.lastAction, s.policy.MinActionIntervalMillis)
	queryDelay, queryClockSkew := flowRateDelay(now, s.lastUIQuery, s.policy.MinUIQueryIntervalMillis)
	result := FlowTimingRequirement{
		ActionReadyDelayMillis:    actionDelay.Milliseconds(),
		QueryReadyDelayMillis:     queryDelay.Milliseconds(),
		MinimumVerifiedFlowMillis: flowRateScheduleMillis(queryDelay, minimumQueries, s.policy.MinUIQueryIntervalMillis),
	}
	if deadline, ok := s.ctx.Deadline(); ok {
		result.SessionRemainingMillis = max(int64(0), deadline.Sub(now).Milliseconds())
	}
	return result, actionClockSkew || queryClockSkew
}

func flowRateDelay(now, previous time.Time, intervalMillis int) (time.Duration, bool) {
	if previous.IsZero() || intervalMillis <= 0 {
		return 0, false
	}
	delay := previous.Add(time.Duration(intervalMillis) * time.Millisecond).Sub(now)
	return max(time.Duration(0), delay), now.Before(previous)
}

func minimumVerifiedFlowQueries(step VerifiedFlowPlanStep) uint64 {
	var result uint64
	if step.ObservationID == "" {
		result++
	}
	if step.Postcondition != nil {
		result++
	}
	return result
}

func flowRateScheduleMillis(delay time.Duration, count uint64, intervalMillis int) int64 {
	result := delay.Milliseconds()
	if count > 1 && intervalMillis > 0 {
		additional := saturatingFlowMultiply(count-1, uint64(intervalMillis))
		if additional > math.MaxInt64 || result > math.MaxInt64-int64(additional) {
			return math.MaxInt64
		}
		result += int64(additional)
	}
	return result
}

func saturatingFlowMultiply(left, right uint64) uint64 {
	if left != 0 && right > math.MaxUint64/left {
		return math.MaxUint64
	}
	return left * right
}

func remainingFlowQuota(limit, used uint64) uint64 {
	if used >= limit {
		return 0
	}
	return limit - used
}

func addFlowQuota(left, right FlowQuotaRequirement) FlowQuotaRequirement {
	return FlowQuotaRequirement{
		Actions:      saturatingFlowAdd(left.Actions, right.Actions),
		Queries:      saturatingFlowAdd(left.Queries, right.Queries),
		Observations: saturatingFlowAdd(left.Observations, right.Observations),
		Leases:       saturatingFlowAdd(left.Leases, right.Leases),
	}
}

func saturatingFlowAdd(left, right uint64) uint64 {
	if right > math.MaxUint64-left {
		return math.MaxUint64
	}
	return left + right
}

func addFlowIssue(report *FlowPlanStepReport, status FlowFeasibilityStatus, blocker FlowPlanBlocker, remediation FlowPlanRemediation) {
	report.Status = worseFlowStatus(report.Status, status)
	if !slices.Contains(report.Blockers, blocker) {
		report.Blockers = append(report.Blockers, blocker)
	}
	if !slices.Contains(report.Remediations, remediation) {
		report.Remediations = append(report.Remediations, remediation)
	}
}

func worseFlowStatus(left, right FlowFeasibilityStatus) FlowFeasibilityStatus {
	if flowStatusRank(right) > flowStatusRank(left) {
		return right
	}
	return left
}

func flowStatusRank(status FlowFeasibilityStatus) int {
	switch status {
	case FlowUnsupported:
		return 7
	case FlowPolicyDenied:
		return 6
	case FlowAmbiguous:
		return 5
	case FlowStale:
		return 4
	case FlowEvidenceRequired:
		return 3
	case FlowConditionallyFeasible:
		return 2
	case FlowFeasible:
		return 1
	default:
		return 0
	}
}

func mergeFlowRequirements(target *FlowPlanRequirements, source FlowPlanRequirements) {
	for _, value := range source.Operations {
		if !slices.Contains(target.Operations, value) {
			target.Operations = append(target.Operations, value)
		}
	}
	for _, value := range source.UIRoles {
		target.UIRoles = appendUniqueUIRole(target.UIRoles, value)
	}
	for _, value := range source.UIProperties {
		target.UIProperties = appendUniqueUIProperty(target.UIProperties, value)
	}
	for _, value := range source.UIActions {
		target.UIActions = appendUniqueUIAction(target.UIActions, value)
	}
	for _, value := range source.TargetModes {
		if !slices.Contains(target.TargetModes, value) {
			target.TargetModes = append(target.TargetModes, value)
		}
	}
	for _, value := range source.EvidenceSources {
		if !slices.Contains(target.EvidenceSources, value) {
			target.EvidenceSources = append(target.EvidenceSources, value)
		}
	}
	for _, value := range source.TraceTiers {
		if !slices.Contains(target.TraceTiers, value) {
			target.TraceTiers = append(target.TraceTiers, value)
		}
	}
	for _, value := range source.Confirmations {
		if !slices.Contains(target.Confirmations, value) {
			target.Confirmations = append(target.Confirmations, value)
		}
	}
	for _, value := range source.Backends {
		if !slices.Contains(target.Backends, value) {
			target.Backends = append(target.Backends, value)
		}
	}
	for _, value := range source.Cleanup {
		target.Cleanup = appendUniqueFlowCleanup(target.Cleanup, value)
	}
	for _, value := range source.PlatformLimitations {
		target.PlatformLimitations = appendUniqueFlowPlatformLimitation(target.PlatformLimitations, value)
	}
	for _, value := range source.Assumptions {
		if !slices.Contains(target.Assumptions, value) {
			target.Assumptions = append(target.Assumptions, value)
		}
	}
	target.CapabilityLeaseRequired = target.CapabilityLeaseRequired || source.CapabilityLeaseRequired
	target.CapabilityLeaseDurationMillis = max(target.CapabilityLeaseDurationMillis, source.CapabilityLeaseDurationMillis)
	target.FreshObservationRequired = target.FreshObservationRequired || source.FreshObservationRequired
	target.RuntimeRevalidationRequired = target.RuntimeRevalidationRequired || source.RuntimeRevalidationRequired
}

func normalizeFlowRequirements(requirements *FlowPlanRequirements) {
	sort.Slice(requirements.Operations, func(i, j int) bool { return requirements.Operations[i] < requirements.Operations[j] })
	sort.Slice(requirements.UIRoles, func(i, j int) bool { return requirements.UIRoles[i] < requirements.UIRoles[j] })
	sort.Slice(requirements.UIProperties, func(i, j int) bool { return requirements.UIProperties[i] < requirements.UIProperties[j] })
	sort.Slice(requirements.UIActions, func(i, j int) bool { return requirements.UIActions[i] < requirements.UIActions[j] })
	sort.Slice(requirements.TargetModes, func(i, j int) bool { return requirements.TargetModes[i] < requirements.TargetModes[j] })
	sort.Slice(requirements.EvidenceSources, func(i, j int) bool { return requirements.EvidenceSources[i] < requirements.EvidenceSources[j] })
	sort.Slice(requirements.TraceTiers, func(i, j int) bool { return requirements.TraceTiers[i] < requirements.TraceTiers[j] })
	sort.Slice(requirements.Confirmations, func(i, j int) bool { return requirements.Confirmations[i] < requirements.Confirmations[j] })
	sort.Slice(requirements.Backends, func(i, j int) bool {
		if requirements.Backends[i].Operation != requirements.Backends[j].Operation {
			return requirements.Backends[i].Operation < requirements.Backends[j].Operation
		}
		return requirements.Backends[i].Backend < requirements.Backends[j].Backend
	})
	sort.Slice(requirements.Cleanup, func(i, j int) bool { return requirements.Cleanup[i] < requirements.Cleanup[j] })
	sort.Slice(requirements.PlatformLimitations, func(i, j int) bool {
		return requirements.PlatformLimitations[i] < requirements.PlatformLimitations[j]
	})
	sort.Slice(requirements.Assumptions, func(i, j int) bool { return requirements.Assumptions[i] < requirements.Assumptions[j] })
}

func appendUniqueFlowCleanup(values []FlowCleanupObligation, value FlowCleanupObligation) []FlowCleanupObligation {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}

func appendUniqueFlowPlatformLimitation(values []FlowPlatformLimitation, value FlowPlatformLimitation) []FlowPlatformLimitation {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}

func flowPlannerError(code ErrorCode, message string, cause error) *ActionError {
	return newActionError(code, "", message, cause)
}

func flowPlannerContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return flowPlannerError(ErrorTimedOut, "verified-flow planning deadline exceeded", ctx.Err())
	}
	return flowPlannerError(ErrorCanceled, "verified-flow planning canceled", ctx.Err())
}

func flowPlannerOperationError(err error) error {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return flowPlannerError(actionErr.Code, actionErr.Message, err)
	}
	code, message := classifyBackendError(err)
	return flowPlannerError(code, message, err)
}
