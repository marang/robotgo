package agent

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	robotgo "github.com/marang/robotgo"
)

type inputDriver interface {
	DisplayBounds(displayID int) (displayBounds, error)
	Move(x, y, displayID int) error
	Click(button MouseButton, double bool) error
	TypeText(text string) error
	RuntimeCapabilities() robotgo.RuntimeCapabilities
	Capture(context.Context, CaptureRegion) (image.Image, error)
}

type robotGoDriver struct{}

func (robotGoDriver) DisplayBounds(displayID int) (displayBounds, error) {
	x, y, width, height, err := robotgo.GetDisplayBoundsE(displayID)
	return displayBounds{x: x, y: y, width: width, height: height}, err
}
func (robotGoDriver) Move(x, y, displayID int) error { return robotgo.MoveE(x, y, displayID) }
func (robotGoDriver) Click(button MouseButton, double bool) error {
	return robotgo.ClickE(string(button), double)
}
func (robotGoDriver) TypeText(text string) error { return robotgo.TypeStrE(text) }

// Config defines immutable session policy.
type Config struct {
	Policy    Policy    `json:"policy"`
	AuditSink AuditSink `json:"-"`
}

// Session serializes policy-gated desktop mutations. The underlying RobotGo
// input backends remain process-global, so only one agent Session may exist.
type Session struct {
	policy  Policy
	driver  inputDriver
	catalog OperationCatalog
	ctx     context.Context
	cancel  context.CancelFunc

	actionGate       chan struct{}
	used             uint64
	closeOnce        sync.Once
	observationMu    sync.Mutex
	observations     map[string]observationRecord
	usedObservations uint64
	auditSink        AuditSink
	auditSequence    uint64
}

var (
	ownerMu      sync.Mutex
	activeOwner  *Session
	actionSerial atomic.Uint64
)

// NewSession creates the single active agent session for this process. Runtime
// capability discovery is bounded and never opens a consent dialog.
func NewSession(config Config) (*Session, error) {
	policy, err := preparePolicy(config.Policy)
	if err != nil {
		return nil, err
	}
	capabilities := robotgo.GetRuntimeCapabilities()
	return newSessionWithAudit(policy, robotGoDriver{}, capabilities, config.AuditSink)
}

func newSession(policy Policy, driver inputDriver, capabilities robotgo.RuntimeCapabilities) (*Session, error) {
	return newSessionWithAudit(policy, driver, capabilities, nil)
}

func newSessionWithAudit(policy Policy, driver inputDriver, capabilities robotgo.RuntimeCapabilities, auditSink AuditSink) (*Session, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		policy: policy, driver: driver, catalog: buildCatalog(policy, capabilities),
		ctx: ctx, cancel: cancel, actionGate: make(chan struct{}, 1),
		observations: make(map[string]observationRecord), auditSink: auditSink,
	}
	s.actionGate <- struct{}{}
	ownerMu.Lock()
	defer ownerMu.Unlock()
	if activeOwner != nil {
		cancel()
		return nil, &ActionError{Code: ErrorSessionBusy, Message: "another agent session is already active", cause: ErrSessionBusy}
	}
	activeOwner = s
	return s, nil
}

// Catalog returns a defensive copy of the session's immutable catalog.
func (s *Session) Catalog() OperationCatalog {
	return cloneCatalog(s.catalog)
}

// Close prevents future actions, waits for an active synchronous mutation, and
// releases the process-wide agent-session claim. It is safe to call repeatedly.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.actionGate
		s.closeObservations()
		s.actionGate <- struct{}{}
		ownerMu.Lock()
		if activeOwner == s {
			activeOwner = nil
		}
		ownerMu.Unlock()
	})
	return nil
}

// DryRun performs the same shape, policy, quota, capability, and cancellation
// preflight as Execute without injecting input or consuming action quota. A
// supplied observation precondition is recaptured and consumes observation
// quota because stale-target validation is a real sensitive read.
func (s *Session) DryRun(ctx context.Context, request ActionRequest) (ActionResult, error) {
	return s.run(ctx, request, true)
}

// Execute validates and serially performs one typed desktop mutation.
func (s *Session) Execute(ctx context.Context, request ActionRequest) (ActionResult, error) {
	return s.run(ctx, request, false)
}

func (s *Session) run(ctx context.Context, request ActionRequest, dryRun bool) (ActionResult, error) {
	started := time.Now()
	id := fmt.Sprintf("action-%d", actionSerial.Add(1))
	resultOperation := request.Operation
	if !knownOperation(resultOperation) {
		resultOperation = ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return contextFailure(ctx, id, resultOperation, started)
	case <-s.ctx.Done():
		return actionFailure(id, resultOperation, started, ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
	case <-s.actionGate:
	}
	defer func() { s.actionGate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return contextFailure(ctx, id, resultOperation, started)
	}
	select {
	case <-s.ctx.Done():
		return actionFailure(id, resultOperation, started, ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
	default:
	}
	capability, ok := s.capability(request.Operation)
	if !ok {
		return invalidAction(id, resultOperation, started, "unknown operation")
	}
	if err := validateRequest(request); err != nil {
		return invalidAction(id, request.Operation, started, "%v", err)
	}
	if err := s.authorize(request); err != nil {
		return actionFailure(id, request.Operation, started, ErrorPolicyDenied, "agent policy denied the action", err)
	}
	if !capability.Available {
		return actionFailure(id, request.Operation, started, ErrorUnsupported, "operation is unavailable on the selected backend", robotgo.ErrNotSupported)
	}
	if s.used >= s.policy.MaxActions {
		return actionFailure(id, request.Operation, started, ErrorPolicyDenied, "agent policy action limit reached", ErrPolicyDenied)
	}
	if err := s.emitAudit(ctx, AuditEvent{
		Kind: AuditActionStarted, Operation: request.Operation, ActionID: id,
		PreconditionObservationID: preconditionID(request),
	}); err != nil {
		return actionFailure(id, request.Operation, started, ErrorAuditDelivery, "audit sink rejected action intent", err)
	}
	if err := ctx.Err(); err != nil {
		result, actionErr := contextFailure(ctx, id, request.Operation, started)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	select {
	case <-s.ctx.Done():
		result, actionErr := actionFailure(id, request.Operation, started, ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	default:
	}
	if request.Move != nil {
		if err := s.validateMoveTarget(*request.Move); err != nil {
			if errors.Is(err, ErrPolicyDenied) {
				result, actionErr := actionFailure(id, request.Operation, started, ErrorPolicyDenied, "agent policy denied the action", err)
				return s.finishFailedActionAudit(ctx, result, actionErr)
			}
			code, message := classifyBackendError(err)
			result, actionErr := actionFailure(id, request.Operation, started, code, message, err)
			result.Backend = capability.Backend
			return s.finishFailedActionAudit(ctx, result, actionErr)
		}
	}
	if err := ctx.Err(); err != nil {
		result, actionErr := contextFailure(ctx, id, request.Operation, started)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	select {
	case <-s.ctx.Done():
		result, actionErr := actionFailure(id, request.Operation, started, ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	default:
	}
	lineage, err := s.prepareActionLineage(ctx, request, dryRun)
	if err != nil {
		code, message := classifyLineageError(err)
		result, actionErr := actionFailure(id, request.Operation, started, code, message, err)
		result.Backend = capability.Backend
		result.PreconditionObservationID = preconditionID(request)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	if dryRun {
		result := ActionResult{
			ActionID: id, Operation: request.Operation, Status: ActionPlanned,
			Backend: capability.Backend, DurationMillis: time.Since(started).Milliseconds(),
			PreconditionObservationID: preconditionID(request),
		}
		return s.finishSuccessfulActionAudit(ctx, result)
	}
	s.used++
	if err := s.execute(request); err != nil {
		code, message := classifyBackendError(err)
		result, actionErr := actionFailure(id, request.Operation, started, code, message, err)
		result.Backend = capability.Backend
		result.PreconditionObservationID = preconditionID(request)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	result := ActionResult{
		ActionID: id, Operation: request.Operation, Status: ActionSucceeded,
		Backend: capability.Backend, PreconditionObservationID: preconditionID(request),
	}
	if request.Verification != nil {
		post, verification, verifyErr := s.verifyAction(ctx, id, request, lineage)
		result.Verification = &verification
		if post != nil {
			result.PostObservationID = post.ObservationID
		}
		if verifyErr != nil {
			code, message := classifyLineageError(verifyErr)
			actionErr := newActionError(code, request.Operation, message, verifyErr)
			if code == ErrorAuditDelivery && verification.Status == VerificationPassed {
				result.DurationMillis = time.Since(started).Milliseconds()
				finished, finishErr := s.finishSuccessfulActionAudit(ctx, result)
				if finishErr != nil {
					return finished, errors.Join(actionErr, finishErr)
				}
				return finished, actionErr
			}
			result.Status = ActionUnverified
			result.Error = actionErr
			result.DurationMillis = time.Since(started).Milliseconds()
			return s.finishFailedActionAudit(ctx, result, actionErr)
		}
	}
	result.DurationMillis = time.Since(started).Milliseconds()
	return s.finishSuccessfulActionAudit(ctx, result)
}

func (s *Session) finishSuccessfulActionAudit(ctx context.Context, result ActionResult) (ActionResult, error) {
	if err := s.emitAudit(ctx, actionFinishedEvent(result)); err != nil {
		return result, newActionError(ErrorAuditDelivery, result.Operation, "action completed but audit delivery failed", err)
	}
	return result, nil
}

func (s *Session) finishFailedActionAudit(ctx context.Context, result ActionResult, actionErr error) (ActionResult, error) {
	if err := s.emitAudit(ctx, actionFinishedEvent(result)); err != nil {
		return result, errors.Join(actionErr,
			newActionError(ErrorAuditDelivery, result.Operation, "action completed but audit delivery failed", err))
	}
	return result, actionErr
}

func actionFinishedEvent(result ActionResult) AuditEvent {
	event := AuditEvent{
		Kind: AuditActionFinished, Operation: result.Operation, ActionID: result.ActionID,
		PreconditionObservationID: result.PreconditionObservationID,
		PostObservationID:         result.PostObservationID, ActionStatus: result.Status,
	}
	if result.Error != nil {
		event.ErrorCode = result.Error.Code
	}
	if result.Verification != nil {
		event.VerificationStatus = result.Verification.Status
		event.VerificationAttempts = result.Verification.Attempts
	}
	return event
}

func preconditionID(request ActionRequest) string {
	if request.Precondition == nil {
		return ""
	}
	return request.Precondition.ObservationID
}

type displayBounds struct {
	x      int
	y      int
	width  int
	height int
}

func (b displayBounds) contains(x, y int) bool {
	return containsAxis(x, b.x, b.width) && containsAxis(y, b.y, b.height)
}

func containsAxis(value, minimum, size int) bool {
	return size > 0 && value >= minimum && uint(value)-uint(minimum) < uint(size)
}

func (s *Session) validateMoveTarget(move MoveAction) error {
	bounds, err := s.driver.DisplayBounds(move.DisplayID)
	if err != nil {
		return err
	}
	if !bounds.contains(move.X, move.Y) {
		return ErrPolicyDenied
	}
	return nil
}

func (s *Session) capability(operation Operation) (OperationCapability, bool) {
	for _, capability := range s.catalog.Operations {
		if capability.Operation == operation {
			return capability, true
		}
	}
	return OperationCapability{}, false
}

func (s *Session) authorize(request ActionRequest) error {
	if _, allowed := s.policy.allowOperation[request.Operation]; !allowed {
		return ErrPolicyDenied
	}
	if _, required := s.policy.requireConfirmation[request.Operation]; required && !request.Confirmed {
		return ErrPolicyDenied
	}
	if request.Move != nil {
		if _, allowed := s.policy.allowDisplay[request.Move.DisplayID]; !allowed {
			return ErrPolicyDenied
		}
	}
	if request.Click != nil && request.Click.Double && !s.policy.AllowDoubleClick {
		return ErrPolicyDenied
	}
	if request.TypeText != nil && utf8.RuneCountInString(request.TypeText.Text) > s.policy.MaxTextRunes {
		return ErrPolicyDenied
	}
	return nil
}

func validateRequest(request ActionRequest) error {
	if request.Operation == OperationObserve {
		return errors.New("desktop.observe must use Session.Observe")
	}
	if request.Precondition != nil && !validObservationID(request.Precondition.ObservationID) {
		return errors.New("precondition requires a valid RobotGo observation ID")
	}
	if request.Verification != nil {
		if request.Precondition == nil {
			return errors.New("verification requires an observation precondition")
		}
		switch request.Verification.Condition {
		case VerificationCaptureChanged, VerificationCaptureUnchanged:
		default:
			return errors.New("unsupported verification condition")
		}
	}
	payloads := 0
	if request.Move != nil {
		payloads++
	}
	if request.Click != nil {
		payloads++
	}
	if request.TypeText != nil {
		payloads++
	}
	if payloads != 1 {
		return errors.New("exactly one action payload is required")
	}
	switch request.Operation {
	case OperationMove:
		if request.Move == nil || request.Move.DisplayID < 0 {
			return errors.New("move requires a non-negative display ID")
		}
	case OperationClick:
		if request.Click == nil {
			return errors.New("click payload does not match operation")
		}
		switch request.Click.Button {
		case MouseButtonLeft, MouseButtonMiddle, MouseButtonRight:
		default:
			return errors.New("unsupported mouse button")
		}
	case OperationTypeText:
		if request.TypeText == nil || request.TypeText.Text == "" || !utf8.ValidString(request.TypeText.Text) {
			return errors.New("type-text requires non-empty valid UTF-8")
		}
	default:
		return errors.New("unknown operation")
	}
	return nil
}

func (s *Session) execute(request ActionRequest) error {
	switch request.Operation {
	case OperationMove:
		return s.driver.Move(request.Move.X, request.Move.Y, request.Move.DisplayID)
	case OperationClick:
		return s.driver.Click(request.Click.Button, request.Click.Double)
	case OperationTypeText:
		return s.driver.TypeText(request.TypeText.Text)
	default:
		return fmt.Errorf("%w: unknown operation", robotgo.ErrNotSupported)
	}
}

func classifyBackendError(err error) (ErrorCode, string) {
	switch {
	case errors.Is(err, robotgo.ErrNotSupported):
		return ErrorUnsupported, "operation is unsupported by the selected backend"
	case errors.Is(err, robotgo.ErrPermissionDenied):
		return ErrorPermissionDenied, "desktop permission denied"
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorTimedOut, "backend action deadline exceeded"
	case errors.Is(err, context.Canceled):
		return ErrorCanceled, "backend action canceled"
	default:
		return ErrorBackendFailure, "desktop backend operation failed"
	}
}
