// Package agent provides a typed, policy-gated session layer above RobotGo's
// low-level compatibility API. It deliberately contains no protocol adapter.
package agent

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CatalogSchemaVersion identifies the operation catalog JSON contract.
const CatalogSchemaVersion = "14"

// ActionProofSchemaVersion identifies the privacy-reduced semantic-action
// proof contract. Proofs deliberately contain no request or desktop payload.
const ActionProofSchemaVersion = "2"

// Operation identifies one strict agent operation.
type Operation string

const (
	OperationMove           Operation = "pointer.move"
	OperationClick          Operation = "pointer.click"
	OperationScroll         Operation = "pointer.scroll"
	OperationDrag           Operation = "pointer.drag"
	OperationTypeText       Operation = "keyboard.type-text"
	OperationKeyChord       Operation = "keyboard.chord"
	OperationActivate       Operation = "window.activate"
	OperationObserve        Operation = "desktop.observe"
	OperationView           Operation = "desktop.view"
	OperationOCR            Operation = "desktop.ocr"
	OperationDetectElements Operation = "desktop.detect-elements"
	OperationInspectUI      Operation = "desktop.inspect-ui"
	OperationResolveUI      Operation = "desktop.resolve-ui"
	OperationElementAct     Operation = "desktop.element-act"
	OperationFindColor      Operation = "desktop.find-color"
	OperationWaitColor      Operation = "desktop.wait-color"
)

// RiskClass describes the policy impact of an operation.
type RiskClass string

const (
	RiskSensitiveRead      RiskClass = "sensitive-read"
	RiskReversibleMutation RiskClass = "reversible-mutation"
	RiskElevatedMutation   RiskClass = "elevated-mutation"
)

// CancellationSupport describes where cancellation is enforceable.
type CancellationSupport string

const (
	CancellationPreflightOnly CancellationSupport = "preflight-only"
	CancellationCooperative   CancellationSupport = "cooperative"
)

// OperationCapability is one stable entry in an operation catalog.
type OperationCapability struct {
	Operation                    Operation                  `json:"operation"`
	Available                    bool                       `json:"available"`
	PolicyAllowed                bool                       `json:"policy_allowed"`
	Backend                      string                     `json:"backend,omitempty"`
	Fallback                     bool                       `json:"fallback"`
	Risk                         RiskClass                  `json:"risk"`
	ConfirmationRequired         bool                       `json:"confirmation_required"`
	Cancellation                 CancellationSupport        `json:"cancellation"`
	ProcessGlobalBackend         bool                       `json:"process_global_backend"`
	ExclusiveAgentSession        bool                       `json:"exclusive_agent_session"`
	Reason                       string                     `json:"reason,omitempty"`
	Remediation                  string                     `json:"remediation,omitempty"`
	UnavailableCode              ErrorCode                  `json:"unavailable_code,omitempty"`
	OptionalCapture              bool                       `json:"optional_capture,omitempty"`
	CaptureAvailable             bool                       `json:"capture_available,omitempty"`
	CapturePolicyAllowed         bool                       `json:"capture_policy_allowed,omitempty"`
	CaptureFallback              bool                       `json:"capture_fallback,omitempty"`
	CaptureBackend               string                     `json:"capture_backend,omitempty"`
	ScrollAxes                   []ScrollAxis               `json:"scroll_axes,omitempty"`
	ActionProofVersion           string                     `json:"action_proof_version,omitempty"`
	UIConditionKinds             []UIElementConditionKind   `json:"ui_condition_kinds,omitempty"`
	UIVerificationAttempts       uint32                     `json:"ui_verification_attempts,omitempty"`
	UIVerificationIntervalMillis int                        `json:"ui_verification_interval_ms,omitempty"`
	UIVerificationTimeoutMillis  int                        `json:"ui_verification_timeout_ms,omitempty"`
	TargetSpecVersion            string                     `json:"target_spec_version,omitempty"`
	TargetResolutionStrategies   []TargetResolutionStrategy `json:"target_resolution_strategies,omitempty"`
	TargetResolutionModes        []TargetResolutionMode     `json:"target_resolution_modes,omitempty"`
	CapabilityLeaseVersion       string                     `json:"capability_lease_version,omitempty"`
	CapabilityLeaseRequired      bool                       `json:"capability_lease_required,omitempty"`
	MaxCapabilityLeases          uint64                     `json:"max_capability_leases,omitempty"`
	MaxCapabilityLeaseMillis     int                        `json:"max_capability_lease_ms,omitempty"`
	AdaptiveTargetThreshold      uint32                     `json:"adaptive_target_threshold,omitempty"`
	MaxTargetAncestors           uint32                     `json:"max_target_ancestors,omitempty"`
	TargetEvidenceClauseVersion  string                     `json:"target_evidence_clause_version,omitempty"`
	TargetEvidenceSources        []TargetEvidenceSource     `json:"target_evidence_sources,omitempty"`
	TargetEvidenceProviders      []TargetEvidenceProvider   `json:"target_evidence_providers,omitempty"`
	MaxTargetEvidenceClauses     uint32                     `json:"max_target_evidence_clauses,omitempty"`
	MaxTargetEvidenceAgeMillis   int                        `json:"max_target_evidence_age_ms,omitempty"`
	MinTargetOCRConfidence       float64                    `json:"min_target_ocr_confidence,omitempty"`
	MinTargetVisualConfidence    float64                    `json:"min_target_visual_confidence,omitempty"`
	TraceSchemaVersion           string                     `json:"trace_schema_version,omitempty"`
	TracePrivacyTiers            []TracePrivacyTier         `json:"trace_privacy_tiers,omitempty"`
	MaxTraceEvents               uint32                     `json:"max_trace_events,omitempty"`
	MaxTraceBytes                uint64                     `json:"max_trace_bytes,omitempty"`
	TraceLifetimeMillis          int                        `json:"trace_lifetime_ms,omitempty"`
	TraceExportAllowed           bool                       `json:"trace_export_allowed,omitempty"`
}

// ScrollAxis identifies an axis accepted by a scroll backend.
type ScrollAxis string

const (
	ScrollAxisHorizontal ScrollAxis = "horizontal"
	ScrollAxisVertical   ScrollAxis = "vertical"
)

// OperationCatalog is an immutable snapshot of operation availability.
type OperationCatalog struct {
	SchemaVersion string                `json:"schema_version"`
	Operations    []OperationCapability `json:"operations"`
}

// MouseButton is a validated pointer button name.
type MouseButton string

const (
	MouseButtonLeft   MouseButton = "left"
	MouseButtonMiddle MouseButton = "center"
	MouseButtonRight  MouseButton = "right"
)

// MoveAction moves the pointer to global coordinates that must fall within the
// live bounds of the explicitly selected display.
type MoveAction struct {
	X         int `json:"x"`
	Y         int `json:"y"`
	DisplayID int `json:"display_id"`
}

// ClickAction clicks one validated pointer button.
type ClickAction struct {
	Button MouseButton `json:"button"`
	Double bool        `json:"double,omitempty"`
}

// TypeTextAction types UTF-8 text. The text is never copied into results.
type TypeTextAction struct {
	Text string `json:"text"`
}

// ScrollAction positions the pointer at one validated global coordinate and
// emits one bounded scroll delta repeatedly there. The explicit target keeps
// the operation usable on Wayland, which does not expose the live global
// pointer position.
type ScrollAction struct {
	TargetX   int    `json:"target_x"`
	TargetY   int    `json:"target_y"`
	DeltaX    int    `json:"delta_x"`
	DeltaY    int    `json:"delta_y"`
	Events    uint32 `json:"events"`
	DisplayID int    `json:"display_id"`
}

// DragAction moves from Start to End while holding one pointer button. Both
// coordinates are global and must remain on the same explicitly selected
// display.
type DragAction struct {
	StartX         int         `json:"start_x"`
	StartY         int         `json:"start_y"`
	EndX           int         `json:"end_x"`
	EndY           int         `json:"end_y"`
	DisplayID      int         `json:"display_id"`
	Button         MouseButton `json:"button"`
	DurationMillis int         `json:"duration_ms"`
}

// KeyModifier is the unambiguous, platform-neutral modifier vocabulary exposed
// by the agent contract. Legacy aliases such as cmd, ctrl, and right_shift are
// deliberately not accepted here.
type KeyModifier string

const (
	KeyModifierAlt     KeyModifier = "alt"
	KeyModifierControl KeyModifier = "control"
	KeyModifierMeta    KeyModifier = "meta"
	KeyModifierShift   KeyModifier = "shift"
)

// KeyChordAction presses one bounded key with zero or more canonical
// modifiers after verifying that TargetPID is still the active, allow-listed
// process. It is input, not text entry.
type KeyChordAction struct {
	Key       string        `json:"key"`
	Modifiers []KeyModifier `json:"modifiers,omitempty"`
	TargetPID int           `json:"target_pid"`
}

// WindowTargetKind identifies whether a window target is a process ID or a
// native platform handle.
type WindowTargetKind string

const (
	WindowTargetProcess WindowTargetKind = "process"
	WindowTargetHandle  WindowTargetKind = "handle"
)

// ActivateWindowAction activates one immutable policy-approved process or
// native handle. The session revalidates its expected title immediately before
// dispatch so a stale or reused identity cannot silently target another
// window.
type ActivateWindowAction struct {
	Target int              `json:"target"`
	Kind   WindowTargetKind `json:"kind"`
}

// ActionRequest is a strict JSON-serializable action union. Exactly one action
// payload must be present and must match Operation.
type ActionRequest struct {
	Operation    Operation                `json:"operation"`
	Confirmed    bool                     `json:"confirmed,omitempty"`
	Move         *MoveAction              `json:"move,omitempty"`
	Click        *ClickAction             `json:"click,omitempty"`
	Scroll       *ScrollAction            `json:"scroll,omitempty"`
	Drag         *DragAction              `json:"drag,omitempty"`
	TypeText     *TypeTextAction          `json:"type_text,omitempty"`
	KeyChord     *KeyChordAction          `json:"key_chord,omitempty"`
	Activate     *ActivateWindowAction    `json:"activate,omitempty"`
	Precondition *ObservationPrecondition `json:"precondition,omitempty"`
	Verification *VerificationRequest     `json:"verification,omitempty"`
}

// ActionStatus identifies the outcome of an action request.
type ActionStatus string

const (
	ActionPlanned    ActionStatus = "planned"
	ActionSucceeded  ActionStatus = "succeeded"
	ActionFailed     ActionStatus = "failed"
	ActionUnverified ActionStatus = "unverified"
)

// ActionProofStatus summarizes the semantic transaction without hiding a
// post-dispatch execution or cleanup failure behind later matching evidence.
type ActionProofStatus string

const (
	ActionProofRejectedBeforeDispatch  ActionProofStatus = "rejected-before-dispatch"
	ActionProofFailedBeforeDispatch    ActionProofStatus = "failed-before-dispatch"
	ActionProofVerified                ActionProofStatus = "verified"
	ActionProofUnverifiedAfterDispatch ActionProofStatus = "unverified-after-dispatch"
	ActionProofCleanupPending          ActionProofStatus = "cleanup-pending"
)

// ActionResolutionStrategy is the bounded target-resolution vocabulary used
// by Action Proof v2. It distinguishes exact retained selection from an
// adaptive selection privately bound into a capability lease.
type ActionResolutionStrategy string

const (
	ActionResolutionRetainedReference ActionResolutionStrategy = "retained-reference"
	ActionResolutionAdaptiveLease     ActionResolutionStrategy = "adaptive-capability-lease"
)

// ActionResolutionProof reports only structural resolution evidence.
type ActionResolutionProof struct {
	Strategy       ActionResolutionStrategy `json:"strategy"`
	CandidateCount uint32                   `json:"candidate_count"`
	Exact          bool                     `json:"exact"`
	Healing        bool                     `json:"healing"`
}

// ActionAuthorizationProof reports the fixed policy and confirmation decision
// without copying any policy target or request payload.
type ActionAuthorizationProof struct {
	PolicyAllowed        bool `json:"policy_allowed"`
	ConfirmationRequired bool `json:"confirmation_required"`
	Confirmed            bool `json:"confirmed"`
}

// CapabilityLeaseProof reports only the lifecycle decision for a single-use
// lease. The opaque lease token and its binding digests are never serialized.
type CapabilityLeaseProof struct {
	Required  bool                  `json:"required"`
	Presented bool                  `json:"presented"`
	Status    CapabilityLeaseStatus `json:"status"`
}

// ActionExecutionStatus distinguishes a skipped idempotent retry from the one
// permitted native dispatch.
type ActionExecutionStatus string

const (
	ActionExecutionNotDispatched           ActionExecutionStatus = "not-dispatched"
	ActionExecutionSkippedAlreadySatisfied ActionExecutionStatus = "skipped-already-satisfied"
	ActionExecutionDispatched              ActionExecutionStatus = "dispatched"
)

// ActionExecutionProof contains only fixed action/backend metadata.
type ActionExecutionProof struct {
	Backend  string                `json:"backend,omitempty"`
	Action   UIAction              `json:"action,omitempty"`
	Status   ActionExecutionStatus `json:"status"`
	Fallback bool                  `json:"fallback"`
}

// ActionVerificationStatus is the bounded semantic postcondition outcome.
// NotMatched requires at least one completed probe that returned false;
// Failed may carry zero attempts when a requested probe could not start.
type ActionVerificationStatus string

const (
	ActionVerificationNotRequested ActionVerificationStatus = "not-requested"
	ActionVerificationMatched      ActionVerificationStatus = "matched"
	ActionVerificationNotMatched   ActionVerificationStatus = "not-matched"
	ActionVerificationFailed       ActionVerificationStatus = "failed"
)

// ActionVerificationProof keeps precheck, final-gate, and post-dispatch reads
// separate so its counts correspond exactly to quota-bearing backend probes.
type ActionVerificationProof struct {
	ConditionKind         UIElementConditionKind   `json:"condition_kind,omitempty"`
	Status                ActionVerificationStatus `json:"status"`
	PrecheckAttempts      uint32                   `json:"precheck_attempts"`
	FinalGateChecked      bool                     `json:"final_gate_checked"`
	PostconditionAttempts uint32                   `json:"postcondition_attempts"`
	AlreadySatisfied      bool                     `json:"already_satisfied"`
}

// ActionCleanupProof attests only that RobotGo retained no operation-owned
// transient handles, references, or clearable buffers after the call. It does
// not claim release of the still-live source observation or caller-owned data.
type ActionCleanupProof struct {
	TransientResourcesReleased bool `json:"transient_resources_released"`
}

// ActionProof is the versioned, privacy-reduced proof returned by semantic
// element actions. Nested stages may be absent when processing stopped before
// that stage could truthfully be evaluated.
type ActionProof struct {
	SchemaVersion string                    `json:"schema_version"`
	TransactionID string                    `json:"transaction_id"`
	Status        ActionProofStatus         `json:"status"`
	Resolution    *ActionResolutionProof    `json:"resolution,omitempty"`
	Authorization *ActionAuthorizationProof `json:"authorization,omitempty"`
	Lease         *CapabilityLeaseProof     `json:"lease,omitempty"`
	Execution     ActionExecutionProof      `json:"execution"`
	Verification  *ActionVerificationProof  `json:"verification,omitempty"`
	Cleanup       ActionCleanupProof        `json:"cleanup"`
	ErrorCode     ErrorCode                 `json:"error_code,omitempty"`
}

// ErrorCode is a stable machine-readable action failure category.
type ErrorCode string

const (
	ErrorInvalidInput          ErrorCode = "invalid-input"
	ErrorPolicyDenied          ErrorCode = "policy-denied"
	ErrorUnsupported           ErrorCode = "unsupported"
	ErrorUnavailable           ErrorCode = "unavailable"
	ErrorPermissionDenied      ErrorCode = "permission-denied"
	ErrorSessionClosed         ErrorCode = "session-closed"
	ErrorSessionBusy           ErrorCode = "session-busy"
	ErrorCanceled              ErrorCode = "canceled"
	ErrorTimedOut              ErrorCode = "timed-out"
	ErrorBackendFailure        ErrorCode = "backend-failure"
	ErrorStaleTarget           ErrorCode = "stale-target"
	ErrorVerification          ErrorCode = "verification-failed"
	ErrorAuditDelivery         ErrorCode = "audit-delivery-failed"
	ErrorConditionNotMet       ErrorCode = "condition-not-met"
	ErrorCleanupFailed         ErrorCode = "cleanup-failed"
	ErrorTargetNotFound        ErrorCode = "target-not-found"
	ErrorAmbiguousTarget       ErrorCode = "ambiguous-target"
	ErrorIncompleteObservation ErrorCode = "incomplete-observation"
	ErrorLeaseRequired         ErrorCode = "capability-lease-required"
	ErrorLeaseInvalid          ErrorCode = "capability-lease-invalid"
	ErrorLeaseExpired          ErrorCode = "capability-lease-expired"
	ErrorLeaseConsumed         ErrorCode = "capability-lease-consumed"
	ErrorLeaseMismatch         ErrorCode = "capability-lease-mismatch"
	ErrorTraceExport           ErrorCode = "trace-export-failed"
)

// ActionError is safe to serialize: Message never contains action payloads.
type ActionError struct {
	Code      ErrorCode `json:"code"`
	Operation Operation `json:"operation,omitempty"`
	Message   string    `json:"message"`
	cause     error
}

func (e *ActionError) Error() string { return e.Message }
func (e *ActionError) Unwrap() error { return e.cause }

// ActionResult reports one planned or attempted action without retaining its
// input payload. Trace is present only for an explicit policy-approved Trace
// request on a semantic action.
type ActionResult struct {
	ActionID                  string              `json:"action_id"`
	Operation                 Operation           `json:"operation"`
	Status                    ActionStatus        `json:"status"`
	Backend                   string              `json:"backend,omitempty"`
	DurationMillis            int64               `json:"duration_ms"`
	Error                     *ActionError        `json:"error,omitempty"`
	PreconditionObservationID string              `json:"precondition_observation_id,omitempty"`
	PostObservationID         string              `json:"post_observation_id,omitempty"`
	Verification              *VerificationResult `json:"verification,omitempty"`
	Proof                     *ActionProof        `json:"proof,omitempty"`
	Trace                     *RobotGoTrace       `json:"trace,omitempty"`
}

var (
	ErrSessionBusy           = errors.New("another agent session is already active")
	ErrSessionClosed         = errors.New("agent session is closed")
	ErrPolicyDenied          = errors.New("agent policy denied the action")
	ErrStaleTarget           = errors.New("agent observation target is stale")
	ErrVerification          = errors.New("agent action verification failed")
	ErrAuditDelivery         = errors.New("agent audit delivery failed")
	ErrConditionNotMet       = errors.New("agent visual condition was not met")
	ErrInputCleanup          = errors.New("agent input cleanup failed")
	ErrTargetNotFound        = errors.New("agent target was not found")
	ErrAmbiguousTarget       = errors.New("agent target is ambiguous")
	ErrIncompleteObservation = errors.New("agent observation is incomplete")
	ErrLeaseRequired         = errors.New("agent capability lease is required")
	ErrLeaseInvalid          = errors.New("agent capability lease is invalid")
	ErrLeaseExpired          = errors.New("agent capability lease is expired")
	ErrLeaseConsumed         = errors.New("agent capability lease is consumed")
	ErrLeaseMismatch         = errors.New("agent capability lease binding does not match")
	ErrTraceExport           = errors.New("agent transaction trace export failed")
)

func newActionError(code ErrorCode, operation Operation, message string, cause error) *ActionError {
	if code == ErrorAuditDelivery && !errors.Is(cause, ErrAuditDelivery) {
		cause = errors.Join(ErrAuditDelivery, cause)
	}
	if code == ErrorTraceExport && !errors.Is(cause, ErrTraceExport) {
		cause = errors.Join(ErrTraceExport, cause)
	}
	return &ActionError{Code: code, Operation: operation, Message: message, cause: cause}
}

func actionFailure(id string, operation Operation, started time.Time, code ErrorCode, message string, cause error) (ActionResult, error) {
	actionErr := newActionError(code, operation, message, cause)
	return ActionResult{
		ActionID: id, Operation: operation, Status: ActionFailed,
		DurationMillis: time.Since(started).Milliseconds(), Error: actionErr,
	}, actionErr
}

func contextFailure(ctx context.Context, id string, operation Operation, started time.Time) (ActionResult, error) {
	err := ctx.Err()
	if errors.Is(err, context.DeadlineExceeded) {
		return actionFailure(id, operation, started, ErrorTimedOut, "action deadline exceeded", err)
	}
	return actionFailure(id, operation, started, ErrorCanceled, "action canceled", err)
}

func invalidAction(id string, operation Operation, started time.Time, format string, args ...any) (ActionResult, error) {
	return actionFailure(id, operation, started, ErrorInvalidInput, fmt.Sprintf(format, args...), nil)
}
