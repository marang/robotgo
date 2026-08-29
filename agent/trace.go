package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	// RobotGoTraceSchemaVersion identifies the bounded verified-transaction
	// timeline contract.
	RobotGoTraceSchemaVersion = "1"
	// TraceRequestSchemaVersion identifies an explicit per-transaction capture
	// request. A request never grants an action or changes its execution.
	TraceRequestSchemaVersion = "1"
)

// TracePrivacyTier selects the fixed projection applied before a trace leaves
// the transaction. Trace v1 never includes pixels, text values, native
// handles, raw backend errors, credentials, or unrestricted UI trees.
type TracePrivacyTier string

const (
	TracePrivacyMetadataOnly     TracePrivacyTier = "metadata-only"
	TracePrivacySemanticRedacted TracePrivacyTier = "semantic-redacted"
	TracePrivacyVisualRedacted   TracePrivacyTier = "visual-redacted"
	TracePrivacyFullExplicit     TracePrivacyTier = "full-explicit"
)

var allTracePrivacyTiers = []TracePrivacyTier{
	TracePrivacyMetadataOnly,
	TracePrivacySemanticRedacted,
	TracePrivacyVisualRedacted,
	TracePrivacyFullExplicit,
}

// TraceRequest explicitly opts one semantic action into policy-approved trace
// capture. Export requests synchronous delivery to Config.TraceSink.
type TraceRequest struct {
	SchemaVersion string           `json:"schema_version"`
	Tier          TracePrivacyTier `json:"tier"`
	Export        bool             `json:"export,omitempty"`
}

// TraceExportStatus reports the atomic export outcome without changing the
// underlying desktop transaction result.
type TraceExportStatus string

const (
	TraceExportNotRequested TraceExportStatus = "not-requested"
	TraceExportSucceeded    TraceExportStatus = "succeeded"
	TraceExportFailed       TraceExportStatus = "failed"
)

// TraceEventKind is the fixed, replay-neutral transaction timeline vocabulary.
type TraceEventKind string

const (
	TraceTransactionStarted    TraceEventKind = "transaction.started"
	TraceResolutionFinished    TraceEventKind = "resolution.finished"
	TraceAuthorizationFinished TraceEventKind = "authorization.finished"
	TraceBackendSelected       TraceEventKind = "backend.selected"
	TraceDispatchFinished      TraceEventKind = "dispatch.finished"
	TraceVerificationFinished  TraceEventKind = "verification.finished"
	TraceCancellationObserved  TraceEventKind = "cancellation.observed"
	TraceCleanupFinished       TraceEventKind = "cleanup.finished"
	TraceTransactionFinished   TraceEventKind = "transaction.finished"
)

// TraceEventCode is a stable explanation code, never a free-form log message.
type TraceEventCode string

const (
	TraceCodeStarted           TraceEventCode = "started"
	TraceCodeResolved          TraceEventCode = "resolved"
	TraceCodeMissingEvidence   TraceEventCode = "missing-evidence"
	TraceCodeAllowed           TraceEventCode = "allowed"
	TraceCodeDenied            TraceEventCode = "denied"
	TraceCodeSelected          TraceEventCode = "selected"
	TraceCodeDispatched        TraceEventCode = "dispatched"
	TraceCodeSkipped           TraceEventCode = "skipped"
	TraceCodeNotDispatched     TraceEventCode = "not-dispatched"
	TraceCodeMatched           TraceEventCode = "matched"
	TraceCodeNotMatched        TraceEventCode = "not-matched"
	TraceCodeNotRequested      TraceEventCode = "not-requested"
	TraceCodeFailed            TraceEventCode = "failed"
	TraceCodeCanceled          TraceEventCode = "canceled"
	TraceCodeTimedOut          TraceEventCode = "timed-out"
	TraceCodeCleanupComplete   TraceEventCode = "cleanup-complete"
	TraceCodeCleanupIncomplete TraceEventCode = "cleanup-incomplete"
	TraceCodeVerified          TraceEventCode = "verified"
	TraceCodeRejected          TraceEventCode = "rejected"
	TraceCodeUnverified        TraceEventCode = "unverified"
	TraceCodeCleanupPending    TraceEventCode = "cleanup-pending"
)

// TraceEvent contains only bounded fixed-vocabulary evidence. Fields are
// projected by tier before publication.
type TraceEvent struct {
	Sequence                 uint32                   `json:"sequence"`
	Kind                     TraceEventKind           `json:"kind"`
	Code                     TraceEventCode           `json:"code"`
	TransactionID            string                   `json:"transaction_id"`
	Operation                Operation                `json:"operation,omitempty"`
	ObservationID            string                   `json:"observation_id,omitempty"`
	EvidenceObservationID    string                   `json:"evidence_observation_id,omitempty"`
	Backend                  string                   `json:"backend,omitempty"`
	Fallback                 bool                     `json:"fallback,omitempty"`
	Action                   UIAction                 `json:"action,omitempty"`
	ActionStatus             ActionStatus             `json:"action_status,omitempty"`
	ActionProofStatus        ActionProofStatus        `json:"action_proof_status,omitempty"`
	ActionResolutionStrategy ActionResolutionStrategy `json:"action_resolution_strategy,omitempty"`
	TargetResolutionStrategy TargetResolutionStrategy `json:"target_resolution_strategy,omitempty"`
	TargetResolutionMode     TargetResolutionMode     `json:"target_resolution_mode,omitempty"`
	CandidateCount           uint32                   `json:"candidate_count,omitempty"`
	RejectedCandidateCount   uint32                   `json:"rejected_candidate_count,omitempty"`
	Ambiguous                bool                     `json:"ambiguous,omitempty"`
	AdaptiveScore            uint32                   `json:"adaptive_score,omitempty"`
	AdaptiveThreshold        uint32                   `json:"adaptive_threshold,omitempty"`
	MatchedBy                []TargetEvidence         `json:"matched_by,omitempty"`
	Changed                  []TargetEvidence         `json:"changed,omitempty"`
	EvidenceSources          []TargetEvidenceSource   `json:"evidence_sources,omitempty"`
	EvidenceProviders        []TargetEvidenceProvider `json:"evidence_providers,omitempty"`
	EvidenceAgeMillis        int64                    `json:"evidence_age_ms,omitempty"`
	PolicyAllowed            bool                     `json:"policy_allowed,omitempty"`
	ConfirmationRequired     bool                     `json:"confirmation_required,omitempty"`
	Confirmed                bool                     `json:"confirmed,omitempty"`
	CapabilityLeasePresented bool                     `json:"capability_lease_presented,omitempty"`
	CapabilityLeaseStatus    CapabilityLeaseStatus    `json:"capability_lease_status,omitempty"`
	ExecutionStatus          ActionExecutionStatus    `json:"execution_status,omitempty"`
	ConditionKind            UIElementConditionKind   `json:"condition_kind,omitempty"`
	VerificationStatus       ActionVerificationStatus `json:"verification_status,omitempty"`
	PrecheckAttempts         uint32                   `json:"precheck_attempts,omitempty"`
	FinalGateChecked         bool                     `json:"final_gate_checked,omitempty"`
	PostconditionAttempts    uint32                   `json:"postcondition_attempts,omitempty"`
	AlreadySatisfied         bool                     `json:"already_satisfied,omitempty"`
	CleanupComplete          bool                     `json:"cleanup_complete,omitempty"`
	ErrorCode                ErrorCode                `json:"error_code,omitempty"`
}

// RobotGoTrace is one complete, bounded and replay-neutral transaction
// timeline. It is never accepted as action input.
type RobotGoTrace struct {
	SchemaVersion        string            `json:"schema_version"`
	TransactionID        string            `json:"transaction_id"`
	Tier                 TracePrivacyTier  `json:"tier"`
	StartedAt            time.Time         `json:"started_at"`
	FinishedAt           time.Time         `json:"finished_at"`
	DurationMillis       int64             `json:"duration_ms"`
	Events               []TraceEvent      `json:"events"`
	Truncated            bool              `json:"truncated"`
	Redacted             bool              `json:"redacted"`
	MissingEvidence      bool              `json:"missing_evidence"`
	Expired              bool              `json:"expired"`
	CleanupComplete      bool              `json:"cleanup_complete"`
	ExportStatus         TraceExportStatus `json:"export_status"`
	TransactionErrorCode ErrorCode         `json:"transaction_error_code,omitempty"`
	ExportErrorCode      ErrorCode         `json:"export_error_code,omitempty"`
}

// TraceSink atomically accepts one complete defensive trace copy. Calls may
// overlap after their desktop transaction gates are released; implementations
// must be concurrency-safe, honor ctx, and must not call back into the Session.
// On failure RobotGo clears its transient copy and reports ErrorTraceExport
// without changing the action outcome or proof.
type TraceSink interface {
	ExportTrace(ctx context.Context, trace RobotGoTrace) error
}

type retainedTraceResolution struct {
	present               bool
	observationID         string
	evidenceObservationID string
	strategy              TargetResolutionStrategy
	mode                  TargetResolutionMode
	candidateCount        uint32
	rejectedCount         uint32
	ambiguous             bool
	adaptiveScore         uint32
	adaptiveThreshold     uint32
	matchedBy             []TargetEvidence
	changed               []TargetEvidence
	evidenceSources       []TargetEvidenceSource
	evidenceProviders     []TargetEvidenceProvider
	evidenceAgeMillis     int64
}

func retainedTraceResolutionFromResult(result TargetResolutionResult, evidenceObservationID string) retainedTraceResolution {
	return retainedTraceResolution{
		present: true, observationID: result.ObservationID, evidenceObservationID: evidenceObservationID,
		strategy: result.Strategy, mode: result.Mode, candidateCount: result.CandidateCount,
		rejectedCount: result.RejectedCandidateCount, ambiguous: result.Ambiguous,
		adaptiveScore: result.AdaptiveScore, adaptiveThreshold: result.AdaptiveThreshold,
		matchedBy:         append([]TargetEvidence(nil), result.MatchedBy...),
		changed:           append([]TargetEvidence(nil), result.Changed...),
		evidenceSources:   append([]TargetEvidenceSource(nil), result.EvidenceSources...),
		evidenceProviders: append([]TargetEvidenceProvider(nil), result.EvidenceProviders...),
		evidenceAgeMillis: result.EvidenceAgeMillis,
	}
}

func cloneRetainedTraceResolution(source retainedTraceResolution) retainedTraceResolution {
	result := source
	result.matchedBy = append([]TargetEvidence(nil), source.matchedBy...)
	result.changed = append([]TargetEvidence(nil), source.changed...)
	result.evidenceSources = append([]TargetEvidenceSource(nil), source.evidenceSources...)
	result.evidenceProviders = append([]TargetEvidenceProvider(nil), source.evidenceProviders...)
	return result
}

func clearRetainedTraceResolution(source *retainedTraceResolution) {
	if source == nil {
		return
	}
	clear(source.matchedBy)
	clear(source.changed)
	clear(source.evidenceSources)
	clear(source.evidenceProviders)
	*source = retainedTraceResolution{}
}

type actionTraceRecorder struct {
	session       *Session
	request       TraceRequest
	transactionID string
	startedAt     time.Time
	deadline      time.Time
	resolution    retainedTraceResolution
}

func validTracePrivacyTier(tier TracePrivacyTier) bool {
	for _, candidate := range allTracePrivacyTiers {
		if tier == candidate {
			return true
		}
	}
	return false
}

func (s *Session) prepareActionTrace(transactionID string, request *TraceRequest) (*actionTraceRecorder, *ActionError) {
	if request == nil {
		return nil, nil
	}
	if request.SchemaVersion != TraceRequestSchemaVersion || !validTracePrivacyTier(request.Tier) {
		return nil, newActionError(ErrorInvalidInput, OperationElementAct, "invalid transaction trace request", nil)
	}
	if _, allowed := s.policy.allowTraceTier[request.Tier]; !allowed {
		return nil, newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy denied transaction trace capture", ErrPolicyDenied)
	}
	if request.Export && (!s.policy.AllowTraceExport || s.traceSink == nil) {
		return nil, newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy or configuration denied transaction trace export", ErrPolicyDenied)
	}
	startedAt := s.now().UTC()
	return &actionTraceRecorder{
		session: s, request: *request, transactionID: transactionID, startedAt: startedAt,
		deadline: startedAt.Add(time.Duration(s.policy.TraceLifetimeMillis) * time.Millisecond),
	}, nil
}

func (r *actionTraceRecorder) setResolution(source retainedTraceResolution) {
	if r == nil {
		return
	}
	clearRetainedTraceResolution(&r.resolution)
	r.resolution = cloneRetainedTraceResolution(source)
}

func (r *actionTraceRecorder) finish(result ActionResult, operationErr error) (RobotGoTrace, error) {
	if r == nil {
		return RobotGoTrace{}, nil
	}
	defer clearRetainedTraceResolution(&r.resolution)
	finishedAt := r.session.now().UTC()
	duration := finishedAt.Sub(r.startedAt)
	expired := duration < 0 || !finishedAt.Before(r.deadline)
	trace := RobotGoTrace{
		SchemaVersion: RobotGoTraceSchemaVersion, TransactionID: r.transactionID,
		Tier: r.request.Tier, StartedAt: r.startedAt, FinishedAt: finishedAt,
		DurationMillis: max(int64(0), duration.Milliseconds()), Redacted: r.request.Tier != TracePrivacyFullExplicit,
		Expired: expired, ExportStatus: TraceExportNotRequested,
	}
	trace.TransactionErrorCode = traceTransactionErrorCode(result, operationErr)
	trace.Events, trace.MissingEvidence = r.events(result, trace.TransactionErrorCode)
	for index := range trace.Events {
		trace.Events[index].TransactionID = r.transactionID
	}
	trace.CleanupComplete = result.Proof != nil && result.Proof.Cleanup.TransientResourcesReleased
	if expired {
		trace.Truncated = true
		trace.MissingEvidence = true
	}
	r.projectAndBound(&trace)
	if !r.request.Export {
		return trace, nil
	}
	if expired {
		trace.ExportStatus = TraceExportFailed
		trace.ExportErrorCode = ErrorTraceExport
		r.projectAndBound(&trace)
		return trace, newActionError(ErrorTraceExport, OperationElementAct, "transaction trace expired before export", ErrTraceExport)
	}
	trace.ExportStatus = TraceExportSucceeded
	r.projectAndBound(&trace)
	exported := cloneRobotGoTrace(trace)
	exportCtx, cancel := context.WithDeadline(context.Background(), r.deadline)
	err := r.session.traceSink.ExportTrace(exportCtx, exported)
	cancel()
	if err == nil {
		return trace, nil
	}
	clearRobotGoTrace(&exported)
	trace.ExportStatus = TraceExportFailed
	trace.ExportErrorCode = ErrorTraceExport
	r.projectAndBound(&trace)
	return trace, newActionError(ErrorTraceExport, OperationElementAct, "transaction trace export failed", errors.Join(ErrTraceExport, err))
}

func (r *actionTraceRecorder) events(result ActionResult, errorCode ErrorCode) ([]TraceEvent, bool) {
	events := []TraceEvent{{Kind: TraceTransactionStarted, Code: TraceCodeStarted, Operation: OperationElementAct}}
	missing := false
	proof := result.Proof
	resolution := TraceEvent{Kind: TraceResolutionFinished, Code: TraceCodeMissingEvidence, Operation: OperationElementAct}
	if r.resolution.present {
		resolution.Code = TraceCodeResolved
		resolution.ObservationID = r.resolution.observationID
		resolution.EvidenceObservationID = r.resolution.evidenceObservationID
		resolution.TargetResolutionStrategy = r.resolution.strategy
		resolution.TargetResolutionMode = r.resolution.mode
		resolution.CandidateCount = r.resolution.candidateCount
		resolution.RejectedCandidateCount = r.resolution.rejectedCount
		resolution.Ambiguous = r.resolution.ambiguous
		resolution.AdaptiveScore = r.resolution.adaptiveScore
		resolution.AdaptiveThreshold = r.resolution.adaptiveThreshold
		resolution.MatchedBy = append([]TargetEvidence(nil), r.resolution.matchedBy...)
		resolution.Changed = append([]TargetEvidence(nil), r.resolution.changed...)
		resolution.EvidenceSources = append([]TargetEvidenceSource(nil), r.resolution.evidenceSources...)
		resolution.EvidenceProviders = append([]TargetEvidenceProvider(nil), r.resolution.evidenceProviders...)
		resolution.EvidenceAgeMillis = r.resolution.evidenceAgeMillis
	} else if proof != nil && proof.Resolution != nil {
		resolution.Code = TraceCodeResolved
		resolution.ObservationID = result.PreconditionObservationID
		resolution.ActionResolutionStrategy = proof.Resolution.Strategy
		resolution.CandidateCount = proof.Resolution.CandidateCount
	} else {
		missing = true
	}
	events = append(events, resolution)

	authorization := TraceEvent{Kind: TraceAuthorizationFinished, Code: TraceCodeMissingEvidence, Operation: OperationElementAct}
	if proof != nil && proof.Authorization != nil {
		authorization.PolicyAllowed = proof.Authorization.PolicyAllowed
		authorization.ConfirmationRequired = proof.Authorization.ConfirmationRequired
		authorization.Confirmed = proof.Authorization.Confirmed
		if proof.Authorization.PolicyAllowed && (!proof.Authorization.ConfirmationRequired || proof.Authorization.Confirmed) {
			authorization.Code = TraceCodeAllowed
		} else {
			authorization.Code = TraceCodeDenied
		}
	} else {
		missing = true
	}
	events = append(events, authorization)

	backend := TraceEvent{Kind: TraceBackendSelected, Code: TraceCodeMissingEvidence, Operation: OperationElementAct}
	if proof != nil && proof.Execution.Backend != "" {
		backend.Code = TraceCodeSelected
		backend.Backend = proof.Execution.Backend
		backend.Fallback = proof.Execution.Fallback
	} else {
		missing = true
	}
	events = append(events, backend)

	dispatch := TraceEvent{Kind: TraceDispatchFinished, Code: TraceCodeNotDispatched, Operation: OperationElementAct}
	if proof != nil {
		dispatch.Action = proof.Execution.Action
		dispatch.ExecutionStatus = proof.Execution.Status
		if proof.Lease != nil {
			dispatch.CapabilityLeasePresented = proof.Lease.Presented
			dispatch.CapabilityLeaseStatus = proof.Lease.Status
		}
		switch proof.Execution.Status {
		case ActionExecutionDispatched:
			dispatch.Code = TraceCodeDispatched
		case ActionExecutionSkippedAlreadySatisfied:
			dispatch.Code = TraceCodeSkipped
		}
	}
	events = append(events, dispatch)

	verification := TraceEvent{Kind: TraceVerificationFinished, Code: TraceCodeMissingEvidence, Operation: OperationElementAct}
	if proof != nil && proof.Verification != nil {
		verification.ConditionKind = proof.Verification.ConditionKind
		verification.VerificationStatus = proof.Verification.Status
		verification.PrecheckAttempts = proof.Verification.PrecheckAttempts
		verification.FinalGateChecked = proof.Verification.FinalGateChecked
		verification.PostconditionAttempts = proof.Verification.PostconditionAttempts
		verification.AlreadySatisfied = proof.Verification.AlreadySatisfied
		switch proof.Verification.Status {
		case ActionVerificationMatched:
			verification.Code = TraceCodeMatched
		case ActionVerificationNotMatched:
			verification.Code = TraceCodeNotMatched
		case ActionVerificationNotRequested:
			verification.Code = TraceCodeNotRequested
		default:
			verification.Code = TraceCodeFailed
		}
	} else {
		missing = true
	}
	events = append(events, verification)

	if errorCode == ErrorCanceled || errorCode == ErrorTimedOut {
		code := TraceCodeCanceled
		if errorCode == ErrorTimedOut {
			code = TraceCodeTimedOut
		}
		events = append(events, TraceEvent{Kind: TraceCancellationObserved, Code: code, Operation: OperationElementAct, ErrorCode: errorCode})
	}
	cleanup := TraceEvent{Kind: TraceCleanupFinished, Code: TraceCodeCleanupIncomplete, Operation: OperationElementAct}
	if proof != nil && proof.Cleanup.TransientResourcesReleased {
		cleanup.Code = TraceCodeCleanupComplete
		cleanup.CleanupComplete = true
	}
	events = append(events, cleanup)
	finished := TraceEvent{Kind: TraceTransactionFinished, Code: traceProofCode(proof), Operation: OperationElementAct,
		ActionStatus: result.Status, ErrorCode: errorCode}
	if proof != nil {
		finished.ActionProofStatus = proof.Status
	}
	events = append(events, finished)
	return events, missing
}

func traceProofCode(proof *ActionProof) TraceEventCode {
	if proof == nil {
		return TraceCodeFailed
	}
	switch proof.Status {
	case ActionProofVerified:
		return TraceCodeVerified
	case ActionProofRejectedBeforeDispatch:
		return TraceCodeRejected
	case ActionProofUnverifiedAfterDispatch:
		return TraceCodeUnverified
	case ActionProofCleanupPending:
		return TraceCodeCleanupPending
	default:
		return TraceCodeFailed
	}
}

func traceTransactionErrorCode(result ActionResult, operationErr error) ErrorCode {
	if result.Error != nil {
		return result.Error.Code
	}
	var actionErr *ActionError
	if errors.As(operationErr, &actionErr) {
		return actionErr.Code
	}
	return ""
}

func (r *actionTraceRecorder) projectAndBound(trace *RobotGoTrace) {
	if trace == nil {
		return
	}
	for index := range trace.Events {
		projectTraceEvent(&trace.Events[index], trace.Tier)
	}
	for len(trace.Events) > int(r.session.policy.MaxTraceEvents) {
		removeTraceDetailEvent(trace)
	}
	for traceJSONSize(*trace) > r.session.policy.MaxTraceBytes && len(trace.Events) > 3 {
		removeTraceDetailEvent(trace)
	}
	for index := range trace.Events {
		trace.Events[index].Sequence = uint32(index + 1)
	}
}

func removeTraceDetailEvent(trace *RobotGoTrace) {
	if trace == nil || len(trace.Events) <= 3 {
		return
	}
	remove := -1
	for _, kind := range []TraceEventKind{
		TraceResolutionFinished, TraceAuthorizationFinished, TraceBackendSelected,
		TraceVerificationFinished, TraceDispatchFinished, TraceCancellationObserved,
	} {
		for index := 1; index < len(trace.Events)-2; index++ {
			if trace.Events[index].Kind == kind {
				remove = index
				break
			}
		}
		if remove >= 0 {
			break
		}
	}
	if remove < 0 {
		remove = len(trace.Events) - 3
	}
	clearTraceEvent(&trace.Events[remove])
	copy(trace.Events[remove:], trace.Events[remove+1:])
	clearTraceEvent(&trace.Events[len(trace.Events)-1])
	trace.Events = trace.Events[:len(trace.Events)-1]
	trace.Truncated = true
	trace.MissingEvidence = true
}

func traceJSONSize(trace RobotGoTrace) uint64 {
	payload, err := json.Marshal(trace)
	if err != nil {
		return ^uint64(0)
	}
	return uint64(len(payload))
}

func projectTraceEvent(event *TraceEvent, tier TracePrivacyTier) {
	if event == nil {
		return
	}
	if tier == TracePrivacyFullExplicit {
		return
	}
	clear(event.EvidenceProviders)
	event.EvidenceProviders = nil
	if tier == TracePrivacyVisualRedacted {
		return
	}
	event.EvidenceObservationID = ""
	clear(event.EvidenceSources)
	event.EvidenceSources = nil
	event.EvidenceAgeMillis = 0
	if tier == TracePrivacySemanticRedacted {
		return
	}
	event.ObservationID = ""
	event.ActionResolutionStrategy = ""
	event.TargetResolutionStrategy = ""
	event.TargetResolutionMode = ""
	event.CandidateCount = 0
	event.RejectedCandidateCount = 0
	event.Ambiguous = false
	event.AdaptiveScore = 0
	event.AdaptiveThreshold = 0
	clear(event.MatchedBy)
	clear(event.Changed)
	event.MatchedBy = nil
	event.Changed = nil
}

func clearTraceEvent(event *TraceEvent) {
	if event == nil {
		return
	}
	clear(event.MatchedBy)
	clear(event.Changed)
	clear(event.EvidenceSources)
	clear(event.EvidenceProviders)
	*event = TraceEvent{}
}

func cloneRobotGoTrace(source RobotGoTrace) RobotGoTrace {
	result := source
	result.Events = append([]TraceEvent(nil), source.Events...)
	for index := range result.Events {
		result.Events[index].MatchedBy = append([]TargetEvidence(nil), source.Events[index].MatchedBy...)
		result.Events[index].Changed = append([]TargetEvidence(nil), source.Events[index].Changed...)
		result.Events[index].EvidenceSources = append([]TargetEvidenceSource(nil), source.Events[index].EvidenceSources...)
		result.Events[index].EvidenceProviders = append([]TargetEvidenceProvider(nil), source.Events[index].EvidenceProviders...)
	}
	return result
}

func clearRobotGoTrace(trace *RobotGoTrace) {
	if trace == nil {
		return
	}
	for index := range trace.Events {
		clearTraceEvent(&trace.Events[index])
	}
	clear(trace.Events)
	*trace = RobotGoTrace{}
}
