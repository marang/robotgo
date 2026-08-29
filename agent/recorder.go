package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// SemanticRecorderSchemaVersion identifies the explicit bounded recorder request.
	SemanticRecorderSchemaVersion = "1"
	// RecordedFlowSchemaVersion identifies the privacy-reduced semantic event contract.
	RecordedFlowSchemaVersion = "1"
	// GeneratedFlowSchemaVersion identifies generated Go and MCP artifact metadata.
	GeneratedFlowSchemaVersion = "1"
	// MCPFlowFixtureSchemaVersion identifies the non-executing MCP request-template contract.
	MCPFlowFixtureSchemaVersion = "1"

	agentPackageImportPath       = "github.com/marang/robotgo/agent"
	recorderBindingOverheadBytes = 64
	recorderFlowReserveBytes     = 256
	maxRecorderReviewReasons     = 14
	mcpToolResolveUIName         = "robotgo_resolve_ui"
	mcpToolElementActName        = "robotgo_element_act"
	mcpTemplateReferenceKey      = "$ref"
	mcpOperatorConfirmationRef   = "operator_confirmation"
	mcpOperatorLeaseDurationRef  = "operator_lease_duration_ms"
	mcpOperatorObservationsRef   = "operator_observations."
	mcpOperatorWindowsRef        = "operator_windows."
)

// RecorderRequest explicitly starts one session-scoped semantic recorder.
// Immutable Policy bounds its lifetime, event count, and retained bytes.
type RecorderRequest struct {
	SchemaVersion string `json:"schema_version"`
}

// RecorderActionImpact is the operator-reviewed mutation-impact vocabulary.
type RecorderActionImpact string

const (
	RecorderActionReversible  RecorderActionImpact = "reversible"
	RecorderActionDestructive RecorderActionImpact = "destructive"
)

// RecorderActionHint carries fixed-vocabulary operator intent only. Omission
// is review-required while recording. It never changes authorization,
// confirmation, dispatch, or verification behavior.
type RecorderActionHint struct {
	Impact RecorderActionImpact `json:"impact"`
}

// RecorderEventKind is the fixed semantic recorder vocabulary.
type RecorderEventKind string

const (
	RecorderEventObservation RecorderEventKind = "semantic-observation"
	RecorderEventResolution  RecorderEventKind = "target-resolution"
	RecorderEventAction      RecorderEventKind = "semantic-action"
	RecorderEventReview      RecorderEventKind = "review-item"
)

// RecorderObservationEvidence captures bounded semantic observation shape,
// never the accessibility tree, text values, native references, or pixels.
type RecorderObservationEvidence struct {
	SchemaVersion string    `json:"schema_version"`
	ElementCount  uint32    `json:"element_count"`
	Truncated     bool      `json:"truncated"`
	ErrorCode     ErrorCode `json:"error_code,omitempty"`
}

// RecorderReviewReason explains why generated output must remain non-executable.
type RecorderReviewReason string

const (
	RecorderReviewCoordinateInput      RecorderReviewReason = "coordinate-input"
	RecorderReviewSecretInputOmitted   RecorderReviewReason = "secret-input-omitted"
	RecorderReviewNativeReference      RecorderReviewReason = "native-reference"
	RecorderReviewUnsupportedAction    RecorderReviewReason = "unsupported-action"
	RecorderReviewUnresolvedTarget     RecorderReviewReason = "unresolved-target"
	RecorderReviewLocatorPatch         RecorderReviewReason = "locator-patch"
	RecorderReviewVisualEvidence       RecorderReviewReason = "visual-evidence"
	RecorderReviewDestructiveAction    RecorderReviewReason = "destructive-action"
	RecorderReviewMissingPostcondition RecorderReviewReason = "missing-postcondition"
	RecorderReviewUnverifiedOutcome    RecorderReviewReason = "unverified-outcome"
	RecorderReviewMissingTrace         RecorderReviewReason = "missing-trace"
	RecorderReviewTruncatedFlow        RecorderReviewReason = "truncated-flow"
	RecorderReviewIncompleteTrace      RecorderReviewReason = "incomplete-trace"
	RecorderReviewUnknownImpact        RecorderReviewReason = "unknown-impact"
)

// RecordedTarget is a reusable semantic locator with its native window target
// deliberately omitted. Generated flows accept the operator-owned window as
// an explicit parameter instead of persisting a PID or native handle.
type RecordedTarget struct {
	ID              string           `json:"id"`
	WindowID        string           `json:"window_id"`
	SchemaVersion   string           `json:"schema_version"`
	Role            UIRole           `json:"role"`
	Name            string           `json:"name"`
	RequiredStates  []UIState        `json:"required_states,omitempty"`
	RequiredActions []UIAction       `json:"required_actions,omitempty"`
	Ancestors       []TargetAncestor `json:"ancestors,omitempty"`
}

// RecorderResolutionEvidence retains only fixed, payload-free resolver proof.
type RecorderResolutionEvidence struct {
	SchemaVersion          string                   `json:"schema_version"`
	Strategy               TargetResolutionStrategy `json:"strategy,omitempty"`
	Mode                   TargetResolutionMode     `json:"mode"`
	MatchedBy              []TargetEvidence         `json:"matched_by,omitempty"`
	Changed                []TargetEvidence         `json:"changed,omitempty"`
	CandidateCount         uint32                   `json:"candidate_count"`
	RejectedCandidateCount uint32                   `json:"rejected_candidate_count"`
	Ambiguous              bool                     `json:"ambiguous"`
	EvidenceSources        []TargetEvidenceSource   `json:"evidence_sources,omitempty"`
	ErrorCode              ErrorCode                `json:"error_code,omitempty"`
}

// RecorderActionOutcome captures terminal proof shape without backend payloads.
type RecorderActionOutcome struct {
	Status             ActionStatus             `json:"status"`
	ProofStatus        ActionProofStatus        `json:"proof_status,omitempty"`
	ExecutionStatus    ActionExecutionStatus    `json:"execution_status,omitempty"`
	VerificationStatus ActionVerificationStatus `json:"verification_status,omitempty"`
	ErrorCode          ErrorCode                `json:"error_code,omitempty"`
	CleanupComplete    bool                     `json:"cleanup_complete"`
}

// RecorderTraceLineage is the bounded privacy-safe link to Trace v1.
type RecorderTraceLineage struct {
	SchemaVersion   string           `json:"schema_version"`
	Tier            TracePrivacyTier `json:"tier"`
	TransactionID   string           `json:"transaction_id"`
	Truncated       bool             `json:"truncated"`
	Redacted        bool             `json:"redacted"`
	MissingEvidence bool             `json:"missing_evidence"`
	Expired         bool             `json:"expired"`
	CleanupComplete bool             `json:"cleanup_complete"`
}

// RecorderEvent is one bounded semantic event or non-executable review item.
type RecorderEvent struct {
	Sequence       uint32                       `json:"sequence"`
	Kind           RecorderEventKind            `json:"kind"`
	Operation      Operation                    `json:"operation"`
	ObservationKey string                       `json:"observation_key,omitempty"`
	TargetID       string                       `json:"target_id,omitempty"`
	Action         UIAction                     `json:"action,omitempty"`
	Impact         RecorderActionImpact         `json:"impact,omitempty"`
	Postcondition  *UIElementCondition          `json:"postcondition,omitempty"`
	Observation    *RecorderObservationEvidence `json:"observation,omitempty"`
	Resolution     *RecorderResolutionEvidence  `json:"resolution,omitempty"`
	Outcome        *RecorderActionOutcome       `json:"outcome,omitempty"`
	Trace          *RecorderTraceLineage        `json:"trace,omitempty"`
	ReviewRequired bool                         `json:"review_required"`
	ReviewReasons  []RecorderReviewReason       `json:"review_reasons,omitempty"`
	Executable     bool                         `json:"executable"`
}

// RecordedFlow is the terminal, detached result of an explicit Stop call.
// It contains no action values, coordinates, pixels, native references,
// capability IDs, raw backend errors, or observation element IDs.
type RecordedFlow struct {
	SchemaVersion      string           `json:"schema_version"`
	RecorderVersion    string           `json:"recorder_version"`
	TargetSpecVersion  string           `json:"target_spec_version"`
	ActionProofVersion string           `json:"action_proof_version"`
	TraceVersion       string           `json:"trace_version"`
	StartedAt          time.Time        `json:"started_at"`
	FinishedAt         time.Time        `json:"finished_at"`
	DurationMillis     int64            `json:"duration_ms"`
	Targets            []RecordedTarget `json:"targets"`
	Events             []RecorderEvent  `json:"events"`
	Truncated          bool             `json:"truncated"`
	CleanupComplete    bool             `json:"cleanup_complete"`
}

// FlowGenerationRequest selects stable Go identifiers. Generation never
// executes a mutation and never emits policy or confirmation grants.
type FlowGenerationRequest struct {
	PackageName  string `json:"package_name"`
	FunctionName string `json:"function_name"`
}

// GeneratedFlowArtifacts contains deterministic formatted source and an MCP
// request-template fixture. Both preserve required-review markers.
type GeneratedFlowArtifacts struct {
	SchemaVersion string `json:"schema_version"`
	GoSource      string `json:"go_source"`
	MCPFixture    string `json:"mcp_fixture"`
}

type recordedTargetBinding struct {
	targetID       string
	observationKey string
	reviews        []RecorderReviewReason
}

type recorderEventReservation struct {
	recorder *SemanticRecorder
	sequence uint32
	bound    bool
}

// SemanticRecorder is one explicitly started session-owned recorder.
type SemanticRecorder struct {
	session *Session

	mu                  sync.Mutex
	done                chan struct{}
	doneOnce            sync.Once
	startedAt           time.Time
	active              bool
	expired             bool
	truncated           bool
	usedBytes           uint64
	bindingBytes        uint64
	events              []RecorderEvent
	targets             []RecordedTarget
	targetByDigest      map[[sha256.Size]byte]string
	windowByDigest      map[[sha256.Size]byte]string
	observationByDigest map[[sha256.Size]byte]string
	bindingByLease      map[[sha256.Size]byte]recordedTargetBinding
	bindingByElement    map[[sha256.Size]byte]recordedTargetBinding
	timer               *time.Timer
	maxEvents           uint32
	maxBytes            uint64
}

// StartRecorder starts the only active semantic recorder for this Session.
func (s *Session) StartRecorder(ctx context.Context, request RecorderRequest) (*SemanticRecorder, error) {
	if s == nil {
		return nil, recorderActionError(ErrorSessionClosed, "semantic recorder session is unavailable", ErrSessionClosed)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, recorderContextError(ctx)
	}
	if request.SchemaVersion != SemanticRecorderSchemaVersion {
		return nil, recorderActionError(ErrorInvalidInput, "invalid semantic recorder request", errors.New("unsupported recorder schema"))
	}
	if err := s.ensureOpen(); err != nil {
		return nil, recorderActionError(ErrorSessionClosed, "semantic recorder session is closed", err)
	}
	if !s.policy.AllowRecorder {
		return nil, recorderActionError(ErrorPolicyDenied, "agent policy denied semantic recording", ErrPolicyDenied)
	}

	now := s.now().UTC()
	recorder := &SemanticRecorder{
		session: s, done: make(chan struct{}), startedAt: now, active: true,
		maxEvents: s.policy.MaxRecorderEvents, maxBytes: s.policy.MaxRecorderBytes,
		targetByDigest:      make(map[[sha256.Size]byte]string),
		windowByDigest:      make(map[[sha256.Size]byte]string),
		observationByDigest: make(map[[sha256.Size]byte]string),
		bindingByLease:      make(map[[sha256.Size]byte]recordedTargetBinding),
		bindingByElement:    make(map[[sha256.Size]byte]recordedTargetBinding),
	}
	s.recorderMu.Lock()
	select {
	case <-s.ctx.Done():
		s.recorderMu.Unlock()
		return nil, recorderActionError(ErrorSessionClosed, "semantic recorder session is closed", ErrSessionClosed)
	default:
	}
	if s.recorder != nil {
		s.recorderMu.Unlock()
		return nil, recorderActionError(ErrorRecorderActive, "another semantic recorder is already active", ErrRecorderActive)
	}
	recorder.mu.Lock()
	recorder.timer = time.AfterFunc(time.Duration(s.policy.RecorderLifetimeMillis)*time.Millisecond, recorder.expire)
	recorder.mu.Unlock()
	s.recorder = recorder
	s.recorderMu.Unlock()
	go recorder.expireWithSession()
	return recorder, nil
}

// Stop atomically detaches the recorder, returns a defensive terminal flow,
// and clears all session-owned temporary recorder state.
func (r *SemanticRecorder) Stop(ctx context.Context) (RecordedFlow, error) {
	if r == nil {
		return RecordedFlow{}, recorderActionError(ErrorRecorderStopped, "semantic recorder is stopped", ErrRecorderStopped)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		_ = r.Close()
		return RecordedFlow{}, recorderContextError(ctx)
	}
	if r.sessionExpired() {
		r.expire()
		return RecordedFlow{}, recorderActionError(ErrorRecorderExpired, "semantic recorder session lifetime expired", ErrRecorderExpired)
	}
	r.detach()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.expired {
		return RecordedFlow{}, recorderActionError(ErrorRecorderExpired, "semantic recorder lifetime expired", ErrRecorderExpired)
	}
	if !r.active {
		return RecordedFlow{}, recorderActionError(ErrorRecorderStopped, "semantic recorder is stopped", ErrRecorderStopped)
	}
	r.active = false
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.signalDone()
	finished := time.Now().UTC()
	if r.session != nil && r.session.now != nil {
		finished = r.session.now().UTC()
	}
	if finished.Before(r.startedAt) {
		finished = r.startedAt
	}
	flow := RecordedFlow{
		SchemaVersion: RecordedFlowSchemaVersion, RecorderVersion: SemanticRecorderSchemaVersion,
		TargetSpecVersion: TargetSpecSchemaVersion, ActionProofVersion: ActionProofSchemaVersion,
		TraceVersion: RobotGoTraceSchemaVersion, StartedAt: r.startedAt, FinishedAt: finished,
		DurationMillis: finished.Sub(r.startedAt).Milliseconds(),
		Targets:        cloneRecordedTargets(r.targets), Events: cloneRecorderEvents(r.events),
		Truncated: r.truncated, CleanupComplete: true,
	}
	r.clearLocked()
	return flow, nil
}

// Close cancels recording and clears all retained state without producing an artifact.
func (r *SemanticRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.detach()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.active = false
	r.clearLocked()
	r.signalDone()
	return nil
}

func (r *SemanticRecorder) expire() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.active {
		r.active = false
		r.expired = true
		r.clearLocked()
	}
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = nil
	r.signalDone()
	r.mu.Unlock()
	r.detach()
}

func (r *SemanticRecorder) expireWithSession() {
	if r == nil || r.session == nil {
		return
	}
	select {
	case <-r.session.ctx.Done():
		r.expire()
	case <-r.done:
	}
}

func (r *SemanticRecorder) sessionExpired() bool {
	if r == nil || r.session == nil {
		return false
	}
	select {
	case <-r.session.ctx.Done():
		return true
	default:
		return false
	}
}

func (r *SemanticRecorder) signalDone() {
	if r != nil && r.done != nil {
		r.doneOnce.Do(func() { close(r.done) })
	}
}

func (r *SemanticRecorder) detach() {
	if r == nil || r.session == nil {
		return
	}
	r.session.recorderMu.Lock()
	if r.session.recorder == r {
		r.session.recorder = nil
	}
	r.session.recorderMu.Unlock()
}

func (s *Session) closeRecorder() {
	if s == nil {
		return
	}
	s.recorderMu.Lock()
	recorder := s.recorder
	s.recorder = nil
	s.recorderMu.Unlock()
	if recorder != nil {
		_ = recorder.Close()
	}
}

func (s *Session) activeRecorder() *SemanticRecorder {
	if s == nil {
		return nil
	}
	s.recorderMu.Lock()
	recorder := s.recorder
	s.recorderMu.Unlock()
	if recorder != nil && recorder.sessionExpired() {
		recorder.expire()
		return nil
	}
	return recorder
}

func (s *Session) reserveRecorderEvent(operation Operation) recorderEventReservation {
	reservation := recorderEventReservation{bound: true}
	recorder := s.activeRecorder()
	if recorder == nil {
		return reservation
	}
	reservation.recorder = recorder
	reservation.sequence = recorder.reserveEvent(operation)
	return reservation
}

func (s *Session) recordTargetResolution(request ResolveUIRequest, result TargetResolutionResult, operationErr error) {
	recorder := s.activeRecorder()
	if recorder == nil {
		return
	}
	evidence := &RecorderResolutionEvidence{
		SchemaVersion: result.SchemaVersion, Strategy: result.Strategy, Mode: result.Mode,
		MatchedBy:      append([]TargetEvidence(nil), result.MatchedBy...),
		Changed:        append([]TargetEvidence(nil), result.Changed...),
		CandidateCount: result.CandidateCount, RejectedCandidateCount: result.RejectedCandidateCount,
		Ambiguous: result.Ambiguous, EvidenceSources: append([]TargetEvidenceSource(nil), result.EvidenceSources...),
		ErrorCode: classifyTargetResolutionError(operationErr),
	}
	reviews := resolutionReviewReasons(request, result, operationErr)
	event := RecorderEvent{
		Kind: RecorderEventResolution, Operation: OperationResolveUI, Resolution: evidence,
		ReviewRequired: len(reviews) > 0, ReviewReasons: reviews,
	}
	var target *RecordedTarget
	if validateResolveUIRequest(request) == nil {
		value := recordedTargetFromSpec(request.Target)
		target = &value
	}
	recorder.appendResolution(target, event, request, result, operationErr == nil)
}

func (s *Session) recordSemanticObservation(result UIObservation, operationErr error) {
	recorder := s.activeRecorder()
	if recorder == nil {
		return
	}
	evidence := &RecorderObservationEvidence{
		SchemaVersion: result.SchemaVersion, ElementCount: uint32(len(result.Elements)),
		Truncated: result.Truncated, ErrorCode: recorderErrorCode(operationErr),
	}
	reviews := []RecorderReviewReason(nil)
	if operationErr != nil || result.Truncated {
		reviews = append(reviews, RecorderReviewUnverifiedOutcome)
	}
	recorder.appendObservation(result.ObservationID, RecorderEvent{
		Kind: RecorderEventObservation, Operation: OperationInspectUI, Observation: evidence,
		ReviewRequired: len(reviews) > 0, ReviewReasons: reviews,
	})
}

func (s *Session) recordElementAction(
	request ElementActionRequest,
	presentedLeaseID string,
	result ActionResult,
	operationErr error,
) {
	s.recordElementActionReserved(recorderEventReservation{}, request, presentedLeaseID, result, operationErr)
}

func (s *Session) recordElementActionReserved(
	reservation recorderEventReservation,
	request ElementActionRequest,
	presentedLeaseID string,
	result ActionResult,
	operationErr error,
) {
	recorder := reservation.recorder
	if !reservation.bound {
		recorder = s.activeRecorder()
	}
	if recorder == nil {
		return
	}
	appendEvent := recorder.appendEvent
	if reservation.bound {
		if reservation.sequence == 0 {
			return
		}
		appendEvent = func(event RecorderEvent) {
			recorder.replaceReservedEvent(reservation.sequence, event)
		}
	}
	if validateElementActionRequest(request) != nil {
		reviews := []RecorderReviewReason{RecorderReviewUnsupportedAction, RecorderReviewUnverifiedOutcome}
		if request.Action == UIActionSetValue || request.Value != "" {
			reviews = append(reviews, RecorderReviewSecretInputOmitted, RecorderReviewDestructiveAction)
		}
		appendEvent(RecorderEvent{
			Kind: RecorderEventReview, Operation: OperationElementAct,
			ReviewRequired: true, ReviewReasons: reviews,
		})
		return
	}
	binding, found := recorder.lookupBinding(presentedLeaseID, request.ObservationID, request.ElementID)
	reviews := append([]RecorderReviewReason(nil), binding.reviews...)
	if !found {
		reviews = appendRecorderReview(reviews, RecorderReviewUnresolvedTarget)
	}
	switch {
	case request.Hint == nil || request.Hint.Impact == "":
		reviews = appendRecorderReview(reviews, RecorderReviewUnknownImpact)
	case request.Hint.Impact == RecorderActionDestructive:
		reviews = appendRecorderReview(reviews, RecorderReviewDestructiveAction)
	case request.Hint.Impact != RecorderActionReversible:
		reviews = appendRecorderReview(reviews, RecorderReviewUnknownImpact)
	}
	if request.Action == UIActionSetValue {
		reviews = appendRecorderReview(reviews, RecorderReviewSecretInputOmitted)
		reviews = appendRecorderReview(reviews, RecorderReviewDestructiveAction)
	}
	if request.Postcondition == nil {
		reviews = appendRecorderReview(reviews, RecorderReviewMissingPostcondition)
	}
	verified := result.Status == ActionSucceeded && result.Proof != nil &&
		result.Proof.Status == ActionProofVerified && result.Proof.Cleanup.TransientResourcesReleased &&
		result.Proof.Verification != nil && result.Proof.Verification.Status == ActionVerificationMatched
	if operationErr != nil || !verified {
		reviews = appendRecorderReview(reviews, RecorderReviewUnverifiedOutcome)
	}
	if result.Trace == nil {
		reviews = appendRecorderReview(reviews, RecorderReviewMissingTrace)
	} else if result.Trace.Truncated || result.Trace.MissingEvidence || result.Trace.Expired || !result.Trace.CleanupComplete {
		reviews = appendRecorderReview(reviews, RecorderReviewIncompleteTrace)
	}
	outcome := &RecorderActionOutcome{Status: result.Status}
	if result.Error != nil {
		outcome.ErrorCode = result.Error.Code
	}
	if result.Proof != nil {
		outcome.ProofStatus = result.Proof.Status
		outcome.ExecutionStatus = result.Proof.Execution.Status
		outcome.CleanupComplete = result.Proof.Cleanup.TransientResourcesReleased
		if result.Proof.Verification != nil {
			outcome.VerificationStatus = result.Proof.Verification.Status
		}
	}
	var lineage *RecorderTraceLineage
	if result.Trace != nil {
		lineage = &RecorderTraceLineage{
			SchemaVersion: result.Trace.SchemaVersion, Tier: result.Trace.Tier,
			TransactionID: result.Trace.TransactionID, Truncated: result.Trace.Truncated,
			Redacted: result.Trace.Redacted, MissingEvidence: result.Trace.MissingEvidence,
			Expired: result.Trace.Expired, CleanupComplete: result.Trace.CleanupComplete,
		}
	}
	event := RecorderEvent{
		Kind: RecorderEventAction, Operation: OperationElementAct,
		ObservationKey: binding.observationKey, TargetID: binding.targetID, Action: request.Action,
		Postcondition: cloneUIElementCondition(request.Postcondition), Outcome: outcome, Trace: lineage,
		ReviewRequired: len(reviews) > 0, ReviewReasons: reviews, Executable: len(reviews) == 0,
	}
	if request.Hint != nil && (request.Hint.Impact == RecorderActionReversible || request.Hint.Impact == RecorderActionDestructive) {
		event.Impact = request.Hint.Impact
	}
	appendEvent(event)
}

func (s *Session) recordNonSemanticAction(request ActionRequest, result ActionResult, operationErr error) {
	recorder := s.activeRecorder()
	if recorder == nil {
		return
	}
	operation := result.Operation
	if !knownOperation(operation) {
		operation = ""
	}
	reasons := []RecorderReviewReason{RecorderReviewUnsupportedAction}
	switch operation {
	case OperationMove, OperationClick, OperationScroll, OperationDrag:
		reasons = append(reasons, RecorderReviewCoordinateInput)
	case OperationTypeText, OperationKeyChord:
		reasons = append(reasons, RecorderReviewSecretInputOmitted)
	case OperationActivate:
		reasons = append(reasons, RecorderReviewNativeReference)
	}
	if operationErr != nil || result.Status != ActionSucceeded {
		reasons = appendRecorderReview(reasons, RecorderReviewUnverifiedOutcome)
	}
	recorder.appendEvent(RecorderEvent{
		Kind: RecorderEventReview, Operation: operation,
		ReviewRequired: true, ReviewReasons: reasons, Executable: false,
	})
}

func (r *SemanticRecorder) appendResolution(
	target *RecordedTarget,
	event RecorderEvent,
	request ResolveUIRequest,
	result TargetResolutionResult,
	bind bool,
) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return
	}
	var (
		newTarget         *RecordedTarget
		digest            [sha256.Size]byte
		windowDigest      [sha256.Size]byte
		observationDigest [sha256.Size]byte
		newWindow         bool
		newObservation    bool
	)
	if request.ObservationID != "" {
		observationDigest = recorderDigestStrings(request.ObservationID)
		if key, exists := r.observationByDigest[observationDigest]; exists {
			event.ObservationKey = key
		} else {
			event.ObservationKey = fmt.Sprintf("source-%d", len(r.observationByDigest)+1)
			newObservation = true
		}
	}
	if target != nil {
		windowPayload, _ := json.Marshal(request.Target.Window)
		windowDigest = sha256.Sum256(windowPayload)
		clear(windowPayload)
		value := cloneRecordedTarget(*target)
		if id, exists := r.windowByDigest[windowDigest]; exists {
			value.WindowID = id
		} else {
			value.WindowID = fmt.Sprintf("window-%d", len(r.windowByDigest)+1)
			newWindow = true
		}
		target = &value
		payload, _ := json.Marshal(target)
		digest = sha256.Sum256(payload)
		clear(payload)
		if id, exists := r.targetByDigest[digest]; exists {
			event.TargetID = id
		} else {
			value := cloneRecordedTarget(*target)
			value.ID = fmt.Sprintf("target-%d", len(r.targets)+1)
			event.TargetID = value.ID
			newTarget = &value
		}
	}
	event.Sequence = uint32(len(r.events) + 1)
	prospectiveTargets := r.targets
	if newTarget != nil {
		prospectiveTargets = append(prospectiveTargets, *newTarget)
	}
	prospectiveEvents := append(r.events, event)
	bindingBytes := uint64(0)
	if bind && event.TargetID != "" && result.CandidateCount == 1 && !result.Ambiguous {
		bindingBytes = recorderBindingSize(event.TargetID, event.ReviewReasons)
		if result.Lease != nil && result.Lease.ID != "" {
			bindingBytes += sha256.Size
		}
		if result.ObservationID != "" && result.ElementID != "" {
			bindingBytes += sha256.Size
		}
	}
	if newWindow {
		bindingBytes += sha256.Size + uint64(len(target.WindowID)+recorderBindingOverheadBytes)
	}
	if newObservation {
		bindingBytes += sha256.Size + uint64(len(event.ObservationKey)+recorderBindingOverheadBytes)
	}
	prospectiveBytes := recorderRetainedSize(r.startedAt, prospectiveTargets, prospectiveEvents) +
		r.bindingBytes + bindingBytes
	if uint32(len(r.events)) >= r.maxEvents || prospectiveBytes > r.maxBytes {
		r.truncated = true
		return
	}
	if newTarget != nil {
		r.targets = append(r.targets, *newTarget)
		r.targetByDigest[digest] = newTarget.ID
	}
	if newWindow {
		r.windowByDigest[windowDigest] = target.WindowID
	}
	if newObservation {
		r.observationByDigest[observationDigest] = event.ObservationKey
	}
	r.events = append(r.events, cloneRecorderEvent(event))
	r.bindingBytes += bindingBytes
	r.usedBytes = prospectiveBytes
	if !bind || event.TargetID == "" || result.CandidateCount != 1 || result.Ambiguous {
		return
	}
	binding := recordedTargetBinding{
		targetID: event.TargetID, observationKey: event.ObservationKey,
		reviews: append([]RecorderReviewReason(nil), event.ReviewReasons...),
	}
	if result.Lease != nil && result.Lease.ID != "" {
		r.bindingByLease[recorderDigestStrings(result.Lease.ID)] = binding
	}
	if result.ObservationID != "" && result.ElementID != "" {
		r.bindingByElement[recorderElementDigest(result.ObservationID, result.ElementID)] = binding
	}
}

func (r *SemanticRecorder) appendEvent(event RecorderEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return
	}
	event.Sequence = uint32(len(r.events) + 1)
	prospectiveEvents := append(r.events, event)
	prospectiveBytes := recorderRetainedSize(r.startedAt, r.targets, prospectiveEvents) + r.bindingBytes
	if uint32(len(r.events)) >= r.maxEvents || prospectiveBytes > r.maxBytes {
		r.truncated = true
		return
	}
	r.events = append(r.events, cloneRecorderEvent(event))
	r.usedBytes = prospectiveBytes
}

func (r *SemanticRecorder) reserveEvent(operation Operation) uint32 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return 0
	}
	event := RecorderEvent{
		Sequence: uint32(len(r.events) + 1), Kind: RecorderEventReview, Operation: operation,
		ReviewRequired: true,
		ReviewReasons:  []RecorderReviewReason{RecorderReviewUnverifiedOutcome, RecorderReviewMissingTrace},
	}
	prospectiveEvents := append(r.events, event)
	prospectiveBytes := recorderRetainedSize(r.startedAt, r.targets, prospectiveEvents) + r.bindingBytes
	if uint32(len(r.events)) >= r.maxEvents || prospectiveBytes > r.maxBytes {
		r.truncated = true
		return 0
	}
	r.events = append(r.events, cloneRecorderEvent(event))
	r.usedBytes = prospectiveBytes
	return event.Sequence
}

func (r *SemanticRecorder) replaceReservedEvent(sequence uint32, event RecorderEvent) {
	if r == nil || sequence == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index := int(sequence - 1)
	if !r.active || index >= len(r.events) || r.events[index].Sequence != sequence {
		return
	}
	event.Sequence = sequence
	prospectiveEvents := append([]RecorderEvent(nil), r.events...)
	prospectiveEvents[index] = event
	prospectiveBytes := recorderRetainedSize(r.startedAt, r.targets, prospectiveEvents) + r.bindingBytes
	if prospectiveBytes > r.maxBytes {
		r.truncated = true
		return
	}
	clearRecorderEvent(&r.events[index])
	r.events[index] = cloneRecorderEvent(event)
	r.usedBytes = prospectiveBytes
}

func (r *SemanticRecorder) appendObservation(observationID string, event RecorderEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return
	}
	var (
		digest         [sha256.Size]byte
		newObservation bool
	)
	if observationID != "" {
		digest = recorderDigestStrings(observationID)
		if key, exists := r.observationByDigest[digest]; exists {
			event.ObservationKey = key
		} else {
			event.ObservationKey = fmt.Sprintf("source-%d", len(r.observationByDigest)+1)
			newObservation = true
		}
	}
	event.Sequence = uint32(len(r.events) + 1)
	mapBytes := uint64(0)
	if newObservation {
		mapBytes = sha256.Size + uint64(len(event.ObservationKey)+recorderBindingOverheadBytes)
	}
	prospectiveEvents := append(r.events, event)
	prospectiveBytes := recorderRetainedSize(r.startedAt, r.targets, prospectiveEvents) + r.bindingBytes + mapBytes
	if uint32(len(r.events)) >= r.maxEvents || prospectiveBytes > r.maxBytes {
		r.truncated = true
		return
	}
	r.events = append(r.events, cloneRecorderEvent(event))
	r.bindingBytes += mapBytes
	r.usedBytes = prospectiveBytes
	if newObservation {
		r.observationByDigest[digest] = event.ObservationKey
	}
}

func (r *SemanticRecorder) lookupBinding(leaseID, observationID, elementID string) (recordedTargetBinding, bool) {
	if r == nil {
		return recordedTargetBinding{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return recordedTargetBinding{}, false
	}
	if leaseID != "" {
		binding, ok := r.bindingByLease[recorderDigestStrings(leaseID)]
		return cloneRecordedTargetBinding(binding), ok
	}
	binding, ok := r.bindingByElement[recorderElementDigest(observationID, elementID)]
	return cloneRecordedTargetBinding(binding), ok
}

func (r *SemanticRecorder) clearLocked() {
	for index := range r.events {
		clearRecorderEvent(&r.events[index])
	}
	for index := range r.targets {
		clearRecordedTarget(&r.targets[index])
	}
	clear(r.events)
	clear(r.targets)
	clear(r.targetByDigest)
	clear(r.windowByDigest)
	clear(r.observationByDigest)
	clear(r.bindingByLease)
	clear(r.bindingByElement)
	r.events = nil
	r.targets = nil
	r.targetByDigest = nil
	r.windowByDigest = nil
	r.observationByDigest = nil
	r.bindingByLease = nil
	r.bindingByElement = nil
	r.usedBytes = 0
	r.bindingBytes = 0
}

func recordedTargetFromSpec(spec TargetSpec) RecordedTarget {
	return RecordedTarget{
		SchemaVersion: TargetSpecSchemaVersion, Role: spec.Role, Name: spec.Name,
		RequiredStates:  append([]UIState(nil), spec.RequiredStates...),
		RequiredActions: append([]UIAction(nil), spec.RequiredActions...),
		Ancestors:       cloneTargetAncestors(spec.Ancestors),
	}
}

func resolutionReviewReasons(request ResolveUIRequest, result TargetResolutionResult, operationErr error) []RecorderReviewReason {
	var reasons []RecorderReviewReason
	if operationErr != nil || result.CandidateCount != 1 || result.Ambiguous {
		reasons = appendRecorderReview(reasons, RecorderReviewUnresolvedTarget)
	}
	if result.Patch != nil || len(result.Changed) > 0 || normalizeTargetResolutionMode(request.Mode) == TargetResolutionModeReview {
		reasons = appendRecorderReview(reasons, RecorderReviewLocatorPatch)
	}
	if len(request.Target.Evidence) > 0 || len(result.EvidenceSources) > 0 {
		reasons = appendRecorderReview(reasons, RecorderReviewVisualEvidence)
	}
	return reasons
}

func appendRecorderReview(reasons []RecorderReviewReason, reason RecorderReviewReason) []RecorderReviewReason {
	if !slices.Contains(reasons, reason) {
		reasons = append(reasons, reason)
	}
	return reasons
}

func recorderElementDigest(observationID, elementID string) [sha256.Size]byte {
	return recorderDigestStrings(observationID, elementID)
}

func recorderDigestStrings(parts ...string) [sha256.Size]byte {
	size := len(parts)
	for _, part := range parts {
		size += len(part)
	}
	payload := make([]byte, 0, size)
	for _, part := range parts {
		payload = append(payload, part...)
		payload = append(payload, 0)
	}
	digest := sha256.Sum256(payload)
	clear(payload)
	return digest
}

func recorderRetainedSize(startedAt time.Time, targets []RecordedTarget, events []RecorderEvent) uint64 {
	payload, err := json.Marshal(RecordedFlow{
		SchemaVersion: RecordedFlowSchemaVersion, RecorderVersion: SemanticRecorderSchemaVersion,
		TargetSpecVersion: TargetSpecSchemaVersion, ActionProofVersion: ActionProofSchemaVersion,
		TraceVersion: RobotGoTraceSchemaVersion, StartedAt: startedAt, FinishedAt: startedAt,
		Targets: targets, Events: events, CleanupComplete: true,
	})
	if err != nil {
		return maxAgentRecorderBytes + 1
	}
	size := uint64(len(payload)) + recorderFlowReserveBytes
	clear(payload)
	return size
}

func recorderBindingSize(targetID string, reviews []RecorderReviewReason) uint64 {
	size := uint64(len(targetID) + recorderBindingOverheadBytes)
	for _, review := range reviews {
		size += uint64(len(review))
	}
	return size
}

func recorderContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return recorderActionError(ErrorTimedOut, "semantic recorder deadline exceeded", ctx.Err())
	}
	return recorderActionError(ErrorCanceled, "semantic recorder canceled", ctx.Err())
}

func recorderActionError(code ErrorCode, message string, cause error) *ActionError {
	return &ActionError{Code: code, Message: message, cause: cause}
}

func recorderErrorCode(err error) ErrorCode {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return actionErr.Code
	}
	if err != nil {
		return ErrorBackendFailure
	}
	return ""
}

func cloneRecordedTargetBinding(source recordedTargetBinding) recordedTargetBinding {
	source.reviews = append([]RecorderReviewReason(nil), source.reviews...)
	return source
}

func cloneTargetAncestors(source []TargetAncestor) []TargetAncestor {
	result := make([]TargetAncestor, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].RequiredStates = append([]UIState(nil), source[index].RequiredStates...)
	}
	return result
}

func cloneRecordedTarget(source RecordedTarget) RecordedTarget {
	source.RequiredStates = append([]UIState(nil), source.RequiredStates...)
	source.RequiredActions = append([]UIAction(nil), source.RequiredActions...)
	source.Ancestors = cloneTargetAncestors(source.Ancestors)
	return source
}

func cloneRecordedTargets(source []RecordedTarget) []RecordedTarget {
	result := make([]RecordedTarget, len(source))
	for index := range source {
		result[index] = cloneRecordedTarget(source[index])
	}
	return result
}

func cloneRecorderEvent(source RecorderEvent) RecorderEvent {
	source.Postcondition = cloneUIElementCondition(source.Postcondition)
	source.ReviewReasons = append([]RecorderReviewReason(nil), source.ReviewReasons...)
	if source.Resolution != nil {
		value := *source.Resolution
		value.MatchedBy = append([]TargetEvidence(nil), source.Resolution.MatchedBy...)
		value.Changed = append([]TargetEvidence(nil), source.Resolution.Changed...)
		value.EvidenceSources = append([]TargetEvidenceSource(nil), source.Resolution.EvidenceSources...)
		source.Resolution = &value
	}
	if source.Observation != nil {
		value := *source.Observation
		source.Observation = &value
	}
	if source.Outcome != nil {
		value := *source.Outcome
		source.Outcome = &value
	}
	if source.Trace != nil {
		value := *source.Trace
		source.Trace = &value
	}
	return source
}

func cloneRecorderEvents(source []RecorderEvent) []RecorderEvent {
	result := make([]RecorderEvent, len(source))
	for index := range source {
		result[index] = cloneRecorderEvent(source[index])
	}
	return result
}

func clearRecordedTarget(target *RecordedTarget) {
	if target == nil {
		return
	}
	clear(target.RequiredStates)
	clear(target.RequiredActions)
	for index := range target.Ancestors {
		clear(target.Ancestors[index].RequiredStates)
	}
	clear(target.Ancestors)
	*target = RecordedTarget{}
}

func clearRecorderEvent(event *RecorderEvent) {
	if event == nil {
		return
	}
	if event.Resolution != nil {
		clear(event.Resolution.MatchedBy)
		clear(event.Resolution.Changed)
		clear(event.Resolution.EvidenceSources)
	}
	clear(event.ReviewReasons)
	*event = RecorderEvent{}
}

// Generate produces deterministic formatted Go and MCP artifacts without
// executing the flow or adding policy/confirmation grants.
func (flow RecordedFlow) Generate(request FlowGenerationRequest) (GeneratedFlowArtifacts, error) {
	if err := validateRecordedFlow(flow); err != nil {
		return GeneratedFlowArtifacts{}, err
	}
	if !validGeneratedPackageName(request.PackageName) || !validGeneratedFunctionName(request.FunctionName) ||
		request.PackageName == "main" && request.FunctionName == "main" {
		return GeneratedFlowArtifacts{}, recorderActionError(ErrorInvalidInput, "invalid generated Go identifier", errors.New("package and function names must be identifiers"))
	}
	generationFlow := flowForGeneration(flow)
	goSource, err := generateGoFlow(generationFlow, request)
	if err != nil {
		return GeneratedFlowArtifacts{}, err
	}
	mcpFixture, err := generateMCPFlow(generationFlow)
	if err != nil {
		return GeneratedFlowArtifacts{}, err
	}
	return GeneratedFlowArtifacts{
		SchemaVersion: GeneratedFlowSchemaVersion,
		GoSource:      string(goSource), MCPFixture: string(mcpFixture),
	}, nil
}

func validateRecordedFlow(flow RecordedFlow) error {
	if flow.SchemaVersion != RecordedFlowSchemaVersion ||
		flow.RecorderVersion != SemanticRecorderSchemaVersion ||
		flow.TargetSpecVersion != TargetSpecSchemaVersion ||
		flow.ActionProofVersion != ActionProofSchemaVersion ||
		flow.TraceVersion != RobotGoTraceSchemaVersion || !flow.CleanupComplete ||
		flow.StartedAt.IsZero() || flow.FinishedAt.Before(flow.StartedAt) ||
		flow.DurationMillis != flow.FinishedAt.Sub(flow.StartedAt).Milliseconds() ||
		len(flow.Events) > maxAgentRecorderEvents || len(flow.Targets) > len(flow.Events) {
		return recorderActionError(ErrorInvalidInput, "invalid recorded flow", errors.New("unsupported or incomplete flow contract"))
	}
	targets := make(map[string]struct{}, len(flow.Targets))
	windows := make(map[string]struct{})
	sources := make(map[string]struct{})
	for index, target := range flow.Targets {
		expectedID := fmt.Sprintf("target-%d", index+1)
		if target.ID != expectedID || target.SchemaVersion != TargetSpecSchemaVersion || target.Role == "" || target.WindowID == "" {
			return recorderActionError(ErrorInvalidInput, "invalid recorded target", errors.New("unstable target identity"))
		}
		if err := validateRecordedTarget(target); err != nil {
			return err
		}
		if _, exists := windows[target.WindowID]; !exists {
			if target.WindowID != fmt.Sprintf("window-%d", len(windows)+1) {
				return recorderActionError(ErrorInvalidInput, "invalid recorded window identity", errors.New("unstable window identity"))
			}
			windows[target.WindowID] = struct{}{}
		}
		targets[target.ID] = struct{}{}
	}
	for index, event := range flow.Events {
		if event.Sequence != uint32(index+1) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded event ordering", errors.New("non-monotonic event sequence"))
		}
		if event.TargetID != "" {
			if _, ok := targets[event.TargetID]; !ok {
				return recorderActionError(ErrorInvalidInput, "invalid recorded event target", errors.New("unknown recorded target"))
			}
		}
		if event.ObservationKey != "" {
			if _, exists := sources[event.ObservationKey]; !exists {
				if event.ObservationKey != fmt.Sprintf("source-%d", len(sources)+1) {
					return recorderActionError(ErrorInvalidInput, "invalid recorded observation identity", errors.New("unstable observation identity"))
				}
				sources[event.ObservationKey] = struct{}{}
			}
		}
		if err := validateRecorderEvent(event); err != nil {
			return err
		}
		if event.Executable && (event.Kind != RecorderEventAction || event.ReviewRequired || len(event.ReviewReasons) != 0 ||
			event.TargetID == "" || event.ObservationKey == "" || event.Impact != RecorderActionReversible ||
			event.Postcondition == nil || event.Trace == nil || event.Outcome == nil ||
			event.Outcome.Status != ActionSucceeded || event.Outcome.ProofStatus != ActionProofVerified ||
			event.Outcome.VerificationStatus != ActionVerificationMatched || !event.Outcome.CleanupComplete ||
			event.Trace.Truncated || event.Trace.MissingEvidence || event.Trace.Expired || !event.Trace.CleanupComplete) {
			return recorderActionError(ErrorInvalidInput, "invalid executable recorded event", errors.New("missing verified semantic evidence"))
		}
	}
	payload, err := json.Marshal(flow)
	payloadBytes := len(payload)
	clear(payload)
	if err != nil || payloadBytes > maxAgentRecorderBytes {
		return recorderActionError(ErrorInvalidInput, "recorded flow exceeds hard byte limit", err)
	}
	return nil
}

func validateRecordedTarget(target RecordedTarget) error {
	if !validTargetIdentity(target.Role, target.Name, target.RequiredStates) ||
		len(target.RequiredActions) > maxUIActionsPerNode || !validUniqueUIActions(target.RequiredActions) ||
		len(target.Ancestors) > maxTargetSpecAncestors {
		return recorderActionError(ErrorInvalidInput, "invalid recorded target", errors.New("invalid semantic target vocabulary"))
	}
	totalNameBytes := len(target.Name)
	for _, ancestor := range target.Ancestors {
		if len(ancestor.Name) > maxAgentUIStringBytes-totalNameBytes ||
			!validTargetIdentity(ancestor.Role, ancestor.Name, ancestor.RequiredStates) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded target ancestor", errors.New("invalid semantic ancestor vocabulary"))
		}
		totalNameBytes += len(ancestor.Name)
	}
	return nil
}

func validateRecorderEvent(event RecorderEvent) error {
	if len(event.ReviewReasons) > maxRecorderReviewReasons || !validRecorderErrorCode(recorderEventErrorCode(event)) {
		return recorderActionError(ErrorInvalidInput, "invalid recorded event evidence", errors.New("invalid bounded error or review vocabulary"))
	}
	switch event.Kind {
	case RecorderEventObservation:
		if event.Operation != OperationInspectUI || event.Observation == nil || event.Observation.SchemaVersion != UISchemaVersion {
			return recorderActionError(ErrorInvalidInput, "invalid recorded observation event", errors.New("invalid observation evidence"))
		}
		if event.Observation.ErrorCode == "" && event.ObservationKey == "" {
			return recorderActionError(ErrorInvalidInput, "invalid recorded observation lineage", errors.New("missing observation key"))
		}
		if (event.Observation.ErrorCode != "" || event.Observation.Truncated) &&
			!slices.Contains(event.ReviewReasons, RecorderReviewUnverifiedOutcome) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded observation review", errors.New("missing unverified review marker"))
		}
	case RecorderEventResolution:
		if event.Operation != OperationResolveUI || event.Resolution == nil ||
			event.Resolution.SchemaVersion != TargetResolutionSchemaVersion ||
			!validTargetResolutionMode(event.Resolution.Mode) ||
			(event.Resolution.Strategy != "" && !validRecordedResolutionStrategy(event.Resolution.Strategy)) ||
			(event.Resolution.Strategy == "" && event.Resolution.ErrorCode == "") {
			return recorderActionError(ErrorInvalidInput, "invalid recorded resolution event", errors.New("invalid resolver evidence"))
		}
		if len(event.Resolution.MatchedBy) > 10 || len(event.Resolution.Changed) > 10 ||
			len(event.Resolution.EvidenceSources) > len(allTargetEvidenceSources) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded resolution bounds", errors.New("resolver evidence exceeds hard limit"))
		}
		if event.TargetID != "" && event.ObservationKey == "" {
			return recorderActionError(ErrorInvalidInput, "invalid recorded resolution lineage", errors.New("missing observation key"))
		}
		for _, evidence := range append(append([]TargetEvidence(nil), event.Resolution.MatchedBy...), event.Resolution.Changed...) {
			if !validRecordedTargetEvidence(evidence) {
				return recorderActionError(ErrorInvalidInput, "invalid recorded resolution evidence", errors.New("unknown target evidence"))
			}
		}
		for _, source := range event.Resolution.EvidenceSources {
			if !validTargetEvidenceSource(source) {
				return recorderActionError(ErrorInvalidInput, "invalid recorded resolution source", errors.New("unknown target evidence source"))
			}
		}
		if (event.Resolution.ErrorCode != "" || event.Resolution.CandidateCount != 1 || event.Resolution.Ambiguous) &&
			!slices.Contains(event.ReviewReasons, RecorderReviewUnresolvedTarget) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded resolution review", errors.New("missing unresolved review marker"))
		}
		if len(event.Resolution.Changed) > 0 && !slices.Contains(event.ReviewReasons, RecorderReviewLocatorPatch) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded resolution review", errors.New("missing locator review marker"))
		}
		if len(event.Resolution.EvidenceSources) > 0 && !slices.Contains(event.ReviewReasons, RecorderReviewVisualEvidence) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded resolution review", errors.New("missing visual review marker"))
		}
	case RecorderEventAction:
		if event.Operation != OperationElementAct || !validUIAction(event.Action) || event.Outcome == nil ||
			(event.Impact != "" && event.Impact != RecorderActionReversible && event.Impact != RecorderActionDestructive) ||
			validateUIElementCondition(event.Action, event.Postcondition) != nil || !validRecorderActionOutcome(*event.Outcome) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded semantic action", errors.New("invalid action evidence"))
		}
		if event.Trace != nil && (event.Trace.SchemaVersion != RobotGoTraceSchemaVersion ||
			!validTracePrivacyTier(event.Trace.Tier) || event.Trace.TransactionID == "" ||
			!utf8.ValidString(event.Trace.TransactionID) || len(event.Trace.TransactionID) > 128) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded trace lineage", errors.New("invalid trace evidence"))
		}
		if event.Impact == RecorderActionDestructive && !slices.Contains(event.ReviewReasons, RecorderReviewDestructiveAction) {
			return recorderActionError(ErrorInvalidInput, "invalid destructive action review", errors.New("missing destructive review marker"))
		}
		if event.Impact == "" && !slices.Contains(event.ReviewReasons, RecorderReviewUnknownImpact) {
			return recorderActionError(ErrorInvalidInput, "invalid action impact review", errors.New("missing impact review marker"))
		}
		if event.Action == UIActionSetValue &&
			(!slices.Contains(event.ReviewReasons, RecorderReviewSecretInputOmitted) ||
				!slices.Contains(event.ReviewReasons, RecorderReviewDestructiveAction)) {
			return recorderActionError(ErrorInvalidInput, "invalid set-value review", errors.New("missing value review markers"))
		}
		if (event.TargetID == "" || event.ObservationKey == "") &&
			!slices.Contains(event.ReviewReasons, RecorderReviewUnresolvedTarget) {
			return recorderActionError(ErrorInvalidInput, "invalid action target review", errors.New("missing unresolved review marker"))
		}
		if event.Postcondition == nil && !slices.Contains(event.ReviewReasons, RecorderReviewMissingPostcondition) {
			return recorderActionError(ErrorInvalidInput, "invalid action postcondition review", errors.New("missing postcondition review marker"))
		}
		verified := event.Outcome.Status == ActionSucceeded && event.Outcome.ProofStatus == ActionProofVerified &&
			event.Outcome.VerificationStatus == ActionVerificationMatched && event.Outcome.CleanupComplete
		if !verified && !slices.Contains(event.ReviewReasons, RecorderReviewUnverifiedOutcome) {
			return recorderActionError(ErrorInvalidInput, "invalid action outcome review", errors.New("missing unverified review marker"))
		}
		if event.Trace == nil && !slices.Contains(event.ReviewReasons, RecorderReviewMissingTrace) {
			return recorderActionError(ErrorInvalidInput, "invalid action trace review", errors.New("missing trace review marker"))
		}
		if event.Trace != nil && (event.Trace.Truncated || event.Trace.MissingEvidence || event.Trace.Expired || !event.Trace.CleanupComplete) &&
			!slices.Contains(event.ReviewReasons, RecorderReviewIncompleteTrace) {
			return recorderActionError(ErrorInvalidInput, "invalid action trace review", errors.New("missing incomplete trace review marker"))
		}
	case RecorderEventReview:
		if event.Operation != "" && !knownOperation(event.Operation) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded review operation", errors.New("unknown operation"))
		}
	default:
		return recorderActionError(ErrorInvalidInput, "invalid recorded event kind", errors.New("unknown recorder event"))
	}
	if event.ReviewRequired != (len(event.ReviewReasons) > 0) || event.Executable && event.ReviewRequired {
		return recorderActionError(ErrorInvalidInput, "invalid recorded review state", errors.New("inconsistent review marker"))
	}
	seen := make(map[RecorderReviewReason]struct{}, len(event.ReviewReasons))
	for _, reason := range event.ReviewReasons {
		if !validRecorderReviewReason(reason) {
			return recorderActionError(ErrorInvalidInput, "invalid recorded review reason", errors.New("unknown review reason"))
		}
		if _, duplicate := seen[reason]; duplicate {
			return recorderActionError(ErrorInvalidInput, "invalid recorded review reason", errors.New("duplicate review reason"))
		}
		seen[reason] = struct{}{}
	}
	return nil
}

func recorderEventErrorCode(event RecorderEvent) ErrorCode {
	if event.Observation != nil {
		return event.Observation.ErrorCode
	}
	if event.Resolution != nil {
		return event.Resolution.ErrorCode
	}
	if event.Outcome != nil {
		return event.Outcome.ErrorCode
	}
	return ""
}

func validRecorderActionOutcome(outcome RecorderActionOutcome) bool {
	switch outcome.Status {
	case ActionPlanned, ActionSucceeded, ActionFailed, ActionUnverified:
	default:
		return false
	}
	if outcome.ProofStatus != "" {
		switch outcome.ProofStatus {
		case ActionProofRejectedBeforeDispatch, ActionProofFailedBeforeDispatch,
			ActionProofVerified, ActionProofUnverifiedAfterDispatch, ActionProofCleanupPending:
		default:
			return false
		}
	}
	if outcome.ExecutionStatus != "" {
		switch outcome.ExecutionStatus {
		case ActionExecutionNotDispatched, ActionExecutionSkippedAlreadySatisfied,
			ActionExecutionDispatched:
		default:
			return false
		}
	}
	if outcome.VerificationStatus != "" {
		switch outcome.VerificationStatus {
		case ActionVerificationNotRequested, ActionVerificationMatched,
			ActionVerificationNotMatched, ActionVerificationFailed:
		default:
			return false
		}
	}
	return true
}

func validRecorderErrorCode(code ErrorCode) bool {
	switch code {
	case "", ErrorInvalidInput, ErrorPolicyDenied, ErrorUnsupported, ErrorUnavailable,
		ErrorPermissionDenied, ErrorSessionClosed, ErrorSessionBusy, ErrorCanceled,
		ErrorTimedOut, ErrorBackendFailure, ErrorStaleTarget, ErrorVerification,
		ErrorAuditDelivery, ErrorConditionNotMet, ErrorCleanupFailed, ErrorTargetNotFound,
		ErrorAmbiguousTarget, ErrorIncompleteObservation, ErrorLeaseRequired,
		ErrorLeaseInvalid, ErrorLeaseExpired, ErrorLeaseConsumed, ErrorLeaseMismatch,
		ErrorTraceExport, ErrorRecorderActive, ErrorRecorderStopped, ErrorRecorderExpired:
		return true
	default:
		return false
	}
}

func validRecorderReviewReason(reason RecorderReviewReason) bool {
	switch reason {
	case RecorderReviewCoordinateInput, RecorderReviewSecretInputOmitted,
		RecorderReviewNativeReference, RecorderReviewUnsupportedAction,
		RecorderReviewUnresolvedTarget, RecorderReviewLocatorPatch,
		RecorderReviewVisualEvidence, RecorderReviewDestructiveAction,
		RecorderReviewMissingPostcondition, RecorderReviewUnverifiedOutcome,
		RecorderReviewMissingTrace, RecorderReviewTruncatedFlow,
		RecorderReviewIncompleteTrace, RecorderReviewUnknownImpact:
		return true
	default:
		return false
	}
}

func validRecordedResolutionStrategy(strategy TargetResolutionStrategy) bool {
	return slices.Contains(allTargetResolutionStrategies, strategy)
}

func validRecordedTargetEvidence(evidence TargetEvidence) bool {
	switch evidence {
	case TargetEvidenceWindowIdentity, TargetEvidenceRole, TargetEvidenceName,
		TargetEvidenceStates, TargetEvidenceActions, TargetEvidenceAncestors,
		TargetEvidenceImageObservation, TargetEvidenceAnalysisProvenance,
		TargetEvidenceOCRItem, TargetEvidenceVisualItem:
		return true
	default:
		return false
	}
}

func flowForGeneration(source RecordedFlow) RecordedFlow {
	result := source
	result.Targets = cloneRecordedTargets(source.Targets)
	result.Events = cloneRecorderEvents(source.Events)
	if !source.Truncated {
		return result
	}
	for index := range result.Events {
		if result.Events[index].Executable {
			result.Events[index].Executable = false
			result.Events[index].ReviewRequired = true
			result.Events[index].ReviewReasons = appendRecorderReview(
				result.Events[index].ReviewReasons, RecorderReviewTruncatedFlow,
			)
		}
	}
	result.Events = append(result.Events, RecorderEvent{
		Sequence: uint32(len(result.Events) + 1), Kind: RecorderEventReview,
		Operation: OperationElementAct, ReviewRequired: true,
		ReviewReasons: []RecorderReviewReason{RecorderReviewTruncatedFlow},
	})
	return result
}

func validGeneratedPackageName(value string) bool {
	return validGeneratedIdentifier(value) && value != "_"
}

func validGeneratedFunctionName(value string) bool {
	return validGeneratedIdentifier(value) && !generatedFunctionNameReserved(value)
}

func generatedFunctionNameReserved(value string) bool {
	if types.Universe.Lookup(value) != nil {
		return true
	}
	switch value {
	case "_", "init", "context", "errors", "agent", "flowSession",
		"generatedFlowSchemaVersion", "generatedTargetSpecVersion",
		"generatedCapabilityLeaseVersion", "generatedActionProofVersion",
		"generatedTraceRequestVersion", "generatedTraceVersion",
		"verifyGeneratedActionResult":
		return true
	default:
		return false
	}
}

func validGeneratedIdentifier(value string) bool {
	return token.IsIdentifier(value) && !token.Lookup(value).IsKeyword()
}

func generateGoFlow(flow RecordedFlow, request FlowGenerationRequest) ([]byte, error) {
	var source strings.Builder
	hasExecutable := slices.ContainsFunc(flow.Events, func(event RecorderEvent) bool { return event.Executable })
	fmt.Fprintf(&source, "package %s\n\n", request.PackageName)
	source.WriteString("import (\n\t\"context\"\n")
	if hasExecutable {
		source.WriteString("\t\"errors\"\n")
	}
	source.WriteString("\n")
	fmt.Fprintf(&source, "\t%q\n)\n\n", agentPackageImportPath)
	fmt.Fprintf(&source, "const (\n\tgeneratedFlowSchemaVersion = %q\n\tgeneratedTargetSpecVersion = %q\n\tgeneratedCapabilityLeaseVersion = %q\n\tgeneratedActionProofVersion = %q\n\tgeneratedTraceRequestVersion = %q\n\tgeneratedTraceVersion = %q\n)\n\n",
		GeneratedFlowSchemaVersion, flow.TargetSpecVersion, CapabilityLeaseSchemaVersion,
		flow.ActionProofVersion, TraceRequestSchemaVersion, flow.TraceVersion)
	source.WriteString("type flowSession interface {\n")
	source.WriteString("\tResolveUITarget(context.Context, agent.ResolveUIRequest) (agent.TargetResolutionResult, error)\n")
	source.WriteString("\tActUIElement(context.Context, agent.ElementActionRequest) (agent.ActionResult, error)\n}\n\n")
	source.WriteString("// Policy prerequisites: desktop.inspect-ui, desktop.resolve-ui, and desktop.element-act;\n")
	prerequisites := generationPrerequisites(flow)
	fmt.Fprintf(&source, "// Required UI roles: %s. Required UI properties: %s.\n",
		joinUIRoles(prerequisites.UIRoles), joinUIProperties(prerequisites.UIProperties))
	fmt.Fprintf(&source, "// Required UI actions: %s. Required trace tiers: %s.\n",
		joinUIActions(prerequisites.UIActions), joinTraceTiers(prerequisites.TraceTiers))
	source.WriteString("// Strict target resolution and bounded capability leases are required.\n")
	source.WriteString("// This generated function never creates policy and receives confirmation only from its caller.\n")
	fmt.Fprintf(&source, "func %s(ctx context.Context, session flowSession, observations map[string]string, windows map[string]agent.TargetWindowSpec, leaseDurationMillis int, confirmed bool) error {\n", request.FunctionName)
	source.WriteString("\t_ = generatedFlowSchemaVersion\n")

	targetVariables := make(map[string]string, len(flow.Targets))
	for _, target := range flow.Targets {
		if !slices.ContainsFunc(flow.Events, func(event RecorderEvent) bool {
			return event.Executable && event.TargetID == target.ID
		}) {
			continue
		}
		target = targetForGeneration(target, flow.Events)
		targetVariable := fmt.Sprintf("target%d", len(targetVariables)+1)
		targetVariables[target.ID] = targetVariable
		fmt.Fprintf(&source, "\t%s := agent.TargetSpec{\n", targetVariable)
		fmt.Fprintf(&source, "\t\tSchemaVersion: generatedTargetSpecVersion,\n\t\tWindow: windows[%q],\n", target.WindowID)
		fmt.Fprintf(&source, "\t\tRole: agent.UIRole(%q),\n\t\tName: %q,\n", target.Role, target.Name)
		writeGoUIStateSlice(&source, "RequiredStates", target.RequiredStates, 2)
		writeGoUIActionSlice(&source, "RequiredActions", target.RequiredActions, 2)
		writeGoAncestors(&source, target.Ancestors)
		source.WriteString("\t}\n")
	}
	executableIndex := 0
	for _, event := range flow.Events {
		if event.ReviewRequired {
			fmt.Fprintf(&source, "\t// REVIEW REQUIRED step %d: %s.\n", event.Sequence, joinReviewReasons(event.ReviewReasons))
		}
		if !event.Executable {
			continue
		}
		executableIndex++
		targetVariable := targetVariables[event.TargetID]
		resolutionVariable := fmt.Sprintf("resolution%d", executableIndex)
		fmt.Fprintf(&source, "\t%s, err := session.ResolveUITarget(ctx, agent.ResolveUIRequest{\n", resolutionVariable)
		fmt.Fprintf(&source, "\t\tObservationID: observations[%q],\n\t\tTarget: %s,\n\t\tMode: agent.TargetResolutionModeStrict,\n", event.ObservationKey, targetVariable)
		source.WriteString("\t\tLease: &agent.CapabilityLeaseRequest{\n\t\t\tSchemaVersion: generatedCapabilityLeaseVersion,\n")
		fmt.Fprintf(&source, "\t\t\tAction: agent.UIAction(%q),\n", event.Action)
		writeGoCondition(&source, event.Postcondition, 3, "Postcondition")
		source.WriteString("\t\t\tDurationMillis: leaseDurationMillis,\n\t\t},\n\t\tConfirmed: confirmed,\n\t})\n")
		source.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
		fmt.Fprintf(&source, "\tif %s.Lease == nil {\n\t\treturn errors.New(%q)\n\t}\n", resolutionVariable, "generated semantic resolution returned no capability lease")
		actionVariable := fmt.Sprintf("action%d", executableIndex)
		fmt.Fprintf(&source, "\t%s, err := session.ActUIElement(ctx, agent.ElementActionRequest{\n\t\tCapabilityLeaseID: %s.Lease.ID,\n", actionVariable, resolutionVariable)
		fmt.Fprintf(&source, "\t\tAction: agent.UIAction(%q),\n", event.Action)
		writeGoCondition(&source, event.Postcondition, 2, "Postcondition")
		source.WriteString("\t\tConfirmed: confirmed,\n")
		fmt.Fprintf(&source, "\t\tHint: &agent.RecorderActionHint{Impact: agent.RecorderActionImpact(%q)},\n", event.Impact)
		fmt.Fprintf(&source, "\t\tTrace: &agent.TraceRequest{SchemaVersion: generatedTraceRequestVersion, Tier: agent.TracePrivacyTier(%q)},\n", event.Trace.Tier)
		source.WriteString("\t})\n\tif err != nil {\n\t\treturn err\n\t}\n")
		fmt.Fprintf(&source, "\tif err := verifyGeneratedActionResult(%s, agent.TracePrivacyTier(%q)); err != nil {\n\t\treturn err\n\t}\n", actionVariable, event.Trace.Tier)
	}
	source.WriteString("\treturn nil\n}\n")
	if hasExecutable {
		source.WriteString("\nfunc verifyGeneratedActionResult(result agent.ActionResult, traceTier agent.TracePrivacyTier) error {\n")
		source.WriteString("\tif result.Status != agent.ActionSucceeded || result.Proof == nil ||\n")
		source.WriteString("\t\tresult.Proof.SchemaVersion != generatedActionProofVersion || result.Proof.Status != agent.ActionProofVerified ||\n")
		source.WriteString("\t\tresult.Proof.Verification == nil || result.Proof.Verification.Status != agent.ActionVerificationMatched ||\n")
		source.WriteString("\t\t!result.Proof.Cleanup.TransientResourcesReleased {\n")
		source.WriteString("\t\treturn errors.New(\"generated semantic action did not return complete verified proof\")\n\t}\n")
		source.WriteString("\tif result.Trace == nil || result.Trace.SchemaVersion != generatedTraceVersion ||\n")
		source.WriteString("\t\tresult.Trace.Tier != traceTier || result.Trace.TransactionID != result.Proof.TransactionID ||\n")
		source.WriteString("\t\tresult.Trace.Truncated || result.Trace.MissingEvidence || result.Trace.Expired || !result.Trace.CleanupComplete {\n")
		source.WriteString("\t\treturn errors.New(\"generated semantic action did not return complete trace evidence\")\n\t}\n")
		source.WriteString("\treturn nil\n}\n")
	}
	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, recorderActionError(ErrorInvalidInput, "generated Go source is invalid", err)
	}
	return formatted, nil
}

func writeGoUIStateSlice(source *strings.Builder, field string, values []UIState, indent int) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(source, "%s%s: []agent.UIState{", strings.Repeat("\t", indent), field)
	for index, value := range values {
		if index > 0 {
			source.WriteString(", ")
		}
		fmt.Fprintf(source, "agent.UIState(%q)", value)
	}
	source.WriteString("},\n")
}

func writeGoUIActionSlice(source *strings.Builder, field string, values []UIAction, indent int) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(source, "%s%s: []agent.UIAction{", strings.Repeat("\t", indent), field)
	for index, value := range values {
		if index > 0 {
			source.WriteString(", ")
		}
		fmt.Fprintf(source, "agent.UIAction(%q)", value)
	}
	source.WriteString("},\n")
}

func writeGoAncestors(source *strings.Builder, ancestors []TargetAncestor) {
	if len(ancestors) == 0 {
		return
	}
	source.WriteString("\t\tAncestors: []agent.TargetAncestor{\n")
	for _, ancestor := range ancestors {
		fmt.Fprintf(source, "\t\t\t{Role: agent.UIRole(%q), Name: %q", ancestor.Role, ancestor.Name)
		if len(ancestor.RequiredStates) > 0 {
			source.WriteString(", RequiredStates: []agent.UIState{")
			for index, state := range ancestor.RequiredStates {
				if index > 0 {
					source.WriteString(", ")
				}
				fmt.Fprintf(source, "agent.UIState(%q)", state)
			}
			source.WriteString("}")
		}
		source.WriteString("},\n")
	}
	source.WriteString("\t\t},\n")
}

func writeGoCondition(source *strings.Builder, condition *UIElementCondition, indent int, field string) {
	if condition == nil {
		return
	}
	fmt.Fprintf(source, "%s%s: &agent.UIElementCondition{Kind: agent.UIElementConditionKind(%q)", strings.Repeat("\t", indent), field, condition.Kind)
	if condition.State != "" {
		fmt.Fprintf(source, ", State: agent.UIState(%q)", condition.State)
	}
	source.WriteString("},\n")
}

func joinReviewReasons(reasons []RecorderReviewReason) string {
	values := make([]string, len(reasons))
	for index, reason := range reasons {
		values[index] = string(reason)
	}
	return strings.Join(values, ", ")
}

type mcpFlowFixture struct {
	SchemaVersion      string               `json:"schema_version"`
	TargetSpecVersion  string               `json:"target_spec_version"`
	ActionProofVersion string               `json:"action_proof_version"`
	TraceVersion       string               `json:"trace_version"`
	Prerequisites      mcpFlowPrerequisites `json:"policy_prerequisites"`
	Steps              []mcpFlowStep        `json:"steps"`
}

type mcpFlowPrerequisites struct {
	Operations                []Operation            `json:"operations"`
	TargetModes               []TargetResolutionMode `json:"target_modes"`
	UIRoles                   []UIRole               `json:"ui_roles,omitempty"`
	UIProperties              []UIProperty           `json:"ui_properties,omitempty"`
	UIActions                 []UIAction             `json:"ui_actions,omitempty"`
	TraceTiers                []TracePrivacyTier     `json:"trace_tiers,omitempty"`
	CapabilityLeaseRequired   bool                   `json:"capability_lease_required"`
	ConfirmationFromOperator  bool                   `json:"confirmation_from_operator"`
	GeneratedPolicyBroadening bool                   `json:"generated_policy_broadening"`
}

type mcpFlowStep struct {
	ID             string                 `json:"id"`
	TargetID       string                 `json:"target_id,omitempty"`
	ReviewRequired bool                   `json:"review_required"`
	ReviewReasons  []RecorderReviewReason `json:"review_reasons,omitempty"`
	Resolve        *mcpRequestTemplate    `json:"resolve,omitempty"`
	Act            *mcpRequestTemplate    `json:"act,omitempty"`
}

type mcpRequestTemplate struct {
	Tool           string         `json:"tool"`
	Arguments      map[string]any `json:"arguments"`
	ExpectedResult map[string]any `json:"expected_result,omitempty"`
}

func generateMCPFlow(flow RecordedFlow) ([]byte, error) {
	targets := make(map[string]RecordedTarget, len(flow.Targets))
	for _, target := range flow.Targets {
		targets[target.ID] = target
	}
	prerequisites := generationPrerequisites(flow)
	fixture := mcpFlowFixture{
		SchemaVersion: MCPFlowFixtureSchemaVersion, TargetSpecVersion: flow.TargetSpecVersion,
		ActionProofVersion: flow.ActionProofVersion, TraceVersion: flow.TraceVersion,
		Prerequisites: mcpFlowPrerequisites{
			Operations:              []Operation{OperationInspectUI, OperationResolveUI, OperationElementAct},
			TargetModes:             []TargetResolutionMode{TargetResolutionModeStrict},
			CapabilityLeaseRequired: true, ConfirmationFromOperator: true,
			GeneratedPolicyBroadening: false,
		},
	}
	fixture.Prerequisites.UIRoles = prerequisites.UIRoles
	fixture.Prerequisites.UIProperties = prerequisites.UIProperties
	for _, event := range flow.Events {
		if event.Kind != RecorderEventAction && !event.ReviewRequired {
			continue
		}
		step := mcpFlowStep{
			ID: fmt.Sprintf("step-%d", event.Sequence), TargetID: event.TargetID,
			ReviewRequired: event.ReviewRequired,
			ReviewReasons:  append([]RecorderReviewReason(nil), event.ReviewReasons...),
		}
		if event.Executable {
			target := targetForGeneration(targets[event.TargetID], flow.Events)
			resolveArguments := map[string]any{
				"observation_id": map[string]string{mcpTemplateReferenceKey: mcpOperatorObservationsRef + event.ObservationKey},
				"target":         mcpTargetTemplate(target),
				"mode":           TargetResolutionModeStrict,
				"lease": map[string]any{
					"schema_version": CapabilityLeaseSchemaVersion, "action": event.Action,
					"postcondition": event.Postcondition,
					"duration_ms":   map[string]string{mcpTemplateReferenceKey: mcpOperatorLeaseDurationRef},
				},
				"confirmed": map[string]string{mcpTemplateReferenceKey: mcpOperatorConfirmationRef},
			}
			step.Resolve = &mcpRequestTemplate{Tool: mcpToolResolveUIName, Arguments: resolveArguments}
			step.Act = &mcpRequestTemplate{Tool: mcpToolElementActName, Arguments: map[string]any{
				"capability_lease_id": map[string]string{mcpTemplateReferenceKey: step.ID + ".resolve.lease.id"},
				"action":              event.Action, "postcondition": event.Postcondition,
				"confirmed":     map[string]string{mcpTemplateReferenceKey: mcpOperatorConfirmationRef},
				"recorder_hint": map[string]any{"impact": event.Impact},
				"trace":         map[string]any{"schema_version": TraceRequestSchemaVersion, "tier": event.Trace.Tier},
			}, ExpectedResult: map[string]any{
				"status": ActionSucceeded,
				"proof": map[string]any{
					"schema_version": flow.ActionProofVersion, "status": ActionProofVerified,
					"verification": map[string]any{"status": ActionVerificationMatched},
					"cleanup":      map[string]any{"transient_resources_released": true},
				},
				"trace": map[string]any{
					"schema_version": flow.TraceVersion, "tier": event.Trace.Tier,
					"truncated": false, "missing_evidence": false, "expired": false, "cleanup_complete": true,
					"transaction_id": map[string]string{mcpTemplateReferenceKey: step.ID + ".act.proof.transaction_id"},
				},
			}}
			fixture.Prerequisites.UIActions = appendUniqueUIAction(fixture.Prerequisites.UIActions, event.Action)
			fixture.Prerequisites.TraceTiers = appendUniqueTraceTier(fixture.Prerequisites.TraceTiers, event.Trace.Tier)
		}
		fixture.Steps = append(fixture.Steps, step)
	}
	sort.Slice(fixture.Prerequisites.UIActions, func(i, j int) bool { return fixture.Prerequisites.UIActions[i] < fixture.Prerequisites.UIActions[j] })
	sort.Slice(fixture.Prerequisites.TraceTiers, func(i, j int) bool { return fixture.Prerequisites.TraceTiers[i] < fixture.Prerequisites.TraceTiers[j] })
	payload, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return nil, recorderActionError(ErrorInvalidInput, "MCP flow fixture generation failed", err)
	}
	return append(payload, '\n'), nil
}

func targetForGeneration(source RecordedTarget, events []RecorderEvent) RecordedTarget {
	result := cloneRecordedTarget(source)
	result.RequiredStates = appendUniqueUIState(result.RequiredStates, UIStateEnabled)
	for _, event := range events {
		if event.Executable && event.TargetID == result.ID {
			result.RequiredActions = appendUniqueUIAction(result.RequiredActions, event.Action)
		}
	}
	return result
}

func generationPrerequisites(flow RecordedFlow) mcpFlowPrerequisites {
	result := mcpFlowPrerequisites{
		Operations:              []Operation{OperationInspectUI, OperationResolveUI, OperationElementAct},
		TargetModes:             []TargetResolutionMode{TargetResolutionModeStrict},
		CapabilityLeaseRequired: true, ConfirmationFromOperator: true,
	}
	targets := make(map[string]RecordedTarget, len(flow.Targets))
	for _, target := range flow.Targets {
		targets[target.ID] = target
	}
	for _, event := range flow.Events {
		if !event.Executable {
			continue
		}
		target := targets[event.TargetID]
		result.UIRoles = appendUniqueUIRole(result.UIRoles, target.Role)
		for _, ancestor := range target.Ancestors {
			result.UIRoles = appendUniqueUIRole(result.UIRoles, ancestor.Role)
		}
		for _, property := range []UIProperty{
			UIPropertyRole, UIPropertyName, UIPropertyState, UIPropertyBounds, UIPropertyActions,
		} {
			result.UIProperties = appendUniqueUIProperty(result.UIProperties, property)
		}
		if len(target.Ancestors) > 0 {
			result.UIProperties = appendUniqueUIProperty(result.UIProperties, UIPropertyHierarchy)
		}
		if event.Postcondition != nil {
			switch event.Postcondition.Kind {
			case UIElementConditionFocused, UIElementConditionNotFocused:
				result.UIProperties = appendUniqueUIProperty(result.UIProperties, UIPropertyFocus)
			case UIElementConditionValueEqualsActionValue:
				result.UIProperties = appendUniqueUIProperty(result.UIProperties, UIPropertyValue)
			}
		}
		result.UIActions = appendUniqueUIAction(result.UIActions, event.Action)
		result.TraceTiers = appendUniqueTraceTier(result.TraceTiers, event.Trace.Tier)
	}
	sort.Slice(result.UIRoles, func(i, j int) bool { return result.UIRoles[i] < result.UIRoles[j] })
	sort.Slice(result.UIProperties, func(i, j int) bool { return result.UIProperties[i] < result.UIProperties[j] })
	sort.Slice(result.UIActions, func(i, j int) bool { return result.UIActions[i] < result.UIActions[j] })
	sort.Slice(result.TraceTiers, func(i, j int) bool { return result.TraceTiers[i] < result.TraceTiers[j] })
	return result
}

func appendUniqueUIState(values []UIState, value UIState) []UIState {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}

func appendUniqueUIRole(values []UIRole, value UIRole) []UIRole {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}

func appendUniqueUIProperty(values []UIProperty, value UIProperty) []UIProperty {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}

func joinUIRoles(values []UIRole) string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return strings.Join(result, ", ")
}

func joinUIProperties(values []UIProperty) string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return strings.Join(result, ", ")
}

func joinUIActions(values []UIAction) string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return strings.Join(result, ", ")
}

func joinTraceTiers(values []TracePrivacyTier) string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return strings.Join(result, ", ")
}

func mcpTargetTemplate(target RecordedTarget) map[string]any {
	result := map[string]any{
		"schema_version": target.SchemaVersion,
		"window":         map[string]string{mcpTemplateReferenceKey: mcpOperatorWindowsRef + target.WindowID},
		"role":           target.Role,
		"name":           target.Name,
	}
	if len(target.RequiredStates) > 0 {
		result["required_states"] = target.RequiredStates
	}
	if len(target.RequiredActions) > 0 {
		result["required_actions"] = target.RequiredActions
	}
	if len(target.Ancestors) > 0 {
		result["ancestors"] = target.Ancestors
	}
	return result
}

func appendUniqueUIAction(values []UIAction, value UIAction) []UIAction {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}

func appendUniqueTraceTier(values []TracePrivacyTier, value TracePrivacyTier) []TracePrivacyTier {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}
