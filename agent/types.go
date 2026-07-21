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
const CatalogSchemaVersion = "1"

// Operation identifies one strict agent operation.
type Operation string

const (
	OperationMove     Operation = "pointer.move"
	OperationClick    Operation = "pointer.click"
	OperationTypeText Operation = "keyboard.type-text"
)

// RiskClass describes the policy impact of an operation.
type RiskClass string

const RiskReversibleMutation RiskClass = "reversible-mutation"

// CancellationSupport describes where cancellation is enforceable.
type CancellationSupport string

const CancellationPreflightOnly CancellationSupport = "preflight-only"

// OperationCapability is one stable entry in an operation catalog.
type OperationCapability struct {
	Operation             Operation           `json:"operation"`
	Available             bool                `json:"available"`
	PolicyAllowed         bool                `json:"policy_allowed"`
	Backend               string              `json:"backend,omitempty"`
	Fallback              bool                `json:"fallback"`
	Risk                  RiskClass           `json:"risk"`
	ConfirmationRequired  bool                `json:"confirmation_required"`
	Cancellation          CancellationSupport `json:"cancellation"`
	ProcessGlobalBackend  bool                `json:"process_global_backend"`
	ExclusiveAgentSession bool                `json:"exclusive_agent_session"`
	Reason                string              `json:"reason,omitempty"`
	Remediation           string              `json:"remediation,omitempty"`
}

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

// MoveAction moves the pointer on one explicit display.
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

// ActionRequest is a strict JSON-serializable action union. Exactly one action
// payload must be present and must match Operation.
type ActionRequest struct {
	Operation Operation       `json:"operation"`
	Confirmed bool            `json:"confirmed,omitempty"`
	Move      *MoveAction     `json:"move,omitempty"`
	Click     *ClickAction    `json:"click,omitempty"`
	TypeText  *TypeTextAction `json:"type_text,omitempty"`
}

// ActionStatus identifies the outcome of an action request.
type ActionStatus string

const (
	ActionPlanned   ActionStatus = "planned"
	ActionSucceeded ActionStatus = "succeeded"
	ActionFailed    ActionStatus = "failed"
)

// ErrorCode is a stable machine-readable action failure category.
type ErrorCode string

const (
	ErrorInvalidInput     ErrorCode = "invalid-input"
	ErrorPolicyDenied     ErrorCode = "policy-denied"
	ErrorUnsupported      ErrorCode = "unsupported"
	ErrorPermissionDenied ErrorCode = "permission-denied"
	ErrorSessionClosed    ErrorCode = "session-closed"
	ErrorSessionBusy      ErrorCode = "session-busy"
	ErrorCanceled         ErrorCode = "canceled"
	ErrorTimedOut         ErrorCode = "timed-out"
	ErrorBackendFailure   ErrorCode = "backend-failure"
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
// input payload.
type ActionResult struct {
	ActionID       string       `json:"action_id"`
	Operation      Operation    `json:"operation"`
	Status         ActionStatus `json:"status"`
	Backend        string       `json:"backend,omitempty"`
	DurationMillis int64        `json:"duration_ms"`
	Error          *ActionError `json:"error,omitempty"`
}

var (
	ErrSessionBusy   = errors.New("another agent session is already active")
	ErrSessionClosed = errors.New("agent session is closed")
	ErrPolicyDenied  = errors.New("agent policy denied the action")
)

func actionFailure(id string, operation Operation, started time.Time, code ErrorCode, message string, cause error) (ActionResult, error) {
	actionErr := &ActionError{Code: code, Operation: operation, Message: message, cause: cause}
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
