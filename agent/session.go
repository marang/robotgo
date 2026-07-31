package agent

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
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
	Scroll(deltaX, deltaY int) error
	ToggleMouse(button MouseButton, down bool) error
	TypeText(text string) error
	ToggleKey(key string, modifiers []KeyModifier, down bool) error
	TapKey(key string, modifiers []KeyModifier) error
	ActiveWindowPID() (int, error)
	ActiveWindowTitle() (string, error)
	WindowTitle(target int, kind WindowTargetKind) (string, error)
	ActivateWindow(target int, kind WindowTargetKind) error
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
func (robotGoDriver) Scroll(deltaX, deltaY int) error {
	return robotgo.ScrollE(deltaX, deltaY)
}
func (robotGoDriver) ToggleMouse(button MouseButton, down bool) error {
	state := "down"
	if !down {
		state = "up"
	}
	return robotgo.Toggle(string(button), state)
}
func (robotGoDriver) TypeText(text string) error { return robotgo.TypeStrE(text) }
func (robotGoDriver) ToggleKey(key string, modifiers []KeyModifier, down bool) error {
	args := robotGoKeyArguments(modifiers)
	if !down {
		args = append([]interface{}{"up"}, args...)
	}
	return robotgo.KeyToggle(key, args...)
}
func (robotGoDriver) TapKey(key string, modifiers []KeyModifier) error {
	return robotgo.KeyTap(key, robotGoKeyArguments(modifiers)...)
}
func (robotGoDriver) ActiveWindowPID() (int, error) { return robotgo.GetPidE() }
func (robotGoDriver) ActiveWindowTitle() (string, error) {
	return robotgo.GetTitleE()
}
func (robotGoDriver) WindowTitle(target int, kind WindowTargetKind) (string, error) {
	if kind == WindowTargetHandle {
		if nativeMacOSHandleUnsupported() {
			return "", fmt.Errorf("%w: native macOS window handles are not serializable activation targets", robotgo.ErrNotSupported)
		}
		return robotgo.GetTitleE(target, 1)
	}
	return robotgo.GetTitleE(target)
}
func (robotGoDriver) ActivateWindow(target int, kind WindowTargetKind) error {
	if kind == WindowTargetHandle {
		if nativeMacOSHandleUnsupported() {
			return fmt.Errorf("%w: native macOS window handles are not serializable activation targets", robotgo.ErrNotSupported)
		}
		return robotgo.ActivePid(target, 1)
	}
	return robotgo.ActivePid(target)
}

func nativeMacOSHandleUnsupported() bool {
	return runtime.GOOS == "darwin" && robotgo.GetRuntimeBackendInfo().CGOEnabled
}

func robotGoKeyModifier(modifier KeyModifier) string {
	switch modifier {
	case KeyModifierControl:
		return "ctrl"
	case KeyModifierMeta:
		return "cmd"
	default:
		return string(modifier)
	}
}

func robotGoKeyArguments(modifiers []KeyModifier) []interface{} {
	args := make([]interface{}, 0, len(modifiers))
	for _, modifier := range modifiers {
		args = append(args, robotGoKeyModifier(modifier))
	}
	return args
}

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
	dispatchMu       sync.Mutex
	used             uint64
	closeOnce        sync.Once
	closeMu          sync.Mutex
	cleanupComplete  bool
	observationMu    sync.Mutex
	observations     map[string]observationRecord
	usedObservations uint64
	usedQueries      uint64
	auditSink        AuditSink
	auditSequence    uint64
	pressedInputs    []pressedInput
	inputTainted     bool
	lastAction       time.Time
	now              func() time.Time
}

type pressedInput struct {
	button    MouseButton
	key       string
	modifiers []KeyModifier
	keyboard  bool
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
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if policy.SessionTimeoutMillis > 0 {
		ctx, cancel = context.WithTimeout(
			context.Background(),
			time.Duration(policy.SessionTimeoutMillis)*time.Millisecond,
		)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	s := &Session{
		policy: policy, driver: driver, catalog: buildCatalog(policy, capabilities),
		ctx: ctx, cancel: cancel, actionGate: make(chan struct{}, 1),
		observations: make(map[string]observationRecord), auditSink: auditSink,
		now: time.Now,
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

// Close prevents future actions, waits for an active mutation, and releases
// every session-owned pressed input before relinquishing the process-wide
// agent-session claim. A cleanup failure retains ownership so a later Close
// call can retry safely.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.actionGate
		s.closeObservations()
		s.actionGate <- struct{}{}
	})

	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.cleanupComplete {
		return nil
	}
	s.dispatchMu.Lock()
	cleanupErr := s.releaseAllInputs()
	s.dispatchMu.Unlock()
	if cleanupErr != nil {
		return cleanupErr
	}
	s.cleanupComplete = true
	ownerMu.Lock()
	defer ownerMu.Unlock()
	if activeOwner == s {
		activeOwner = nil
	}
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
		return s.sessionFailure(id, resultOperation, started)
	case <-s.actionGate:
	}
	defer func() { s.actionGate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return contextFailure(ctx, id, resultOperation, started)
	}
	select {
	case <-s.ctx.Done():
		return s.sessionFailure(id, resultOperation, started)
	default:
	}
	if s.inputTainted {
		return actionFailure(
			id, resultOperation, started, ErrorCleanupFailed,
			"pressed input cleanup failed; close the session before continuing",
			ErrInputCleanup,
		)
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
		code := capability.UnavailableCode
		if code == "" {
			code = ErrorUnavailable
		}
		return actionFailure(
			id, request.Operation, started, code,
			"operation is unavailable on the selected backend",
			capabilityUnavailableCause(code),
		)
	}
	if s.used >= s.policy.MaxActions {
		return actionFailure(id, request.Operation, started, ErrorPolicyDenied, "agent policy action limit reached", ErrPolicyDenied)
	}
	if err := s.validateActionRate(); err != nil {
		return actionFailure(id, request.Operation, started, ErrorPolicyDenied, "agent policy action rate limit reached", err)
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
		result, actionErr := s.sessionFailure(id, request.Operation, started)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	default:
	}
	if err := s.validateActionTargets(request); err != nil {
		if errors.Is(err, ErrPolicyDenied) {
			result, actionErr := actionFailure(id, request.Operation, started, ErrorPolicyDenied, "agent policy denied the action", err)
			return s.finishFailedActionAudit(ctx, result, actionErr)
		}
		code, message := classifyBackendError(err)
		result, actionErr := actionFailure(id, request.Operation, started, code, message, err)
		result.Backend = capability.Backend
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	if err := ctx.Err(); err != nil {
		result, actionErr := contextFailure(ctx, id, request.Operation, started)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	select {
	case <-s.ctx.Done():
		result, actionErr := s.sessionFailure(id, request.Operation, started)
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
	if lineage != nil {
		defer lineage.release()
	}
	if err := ctx.Err(); err != nil {
		result, actionErr := contextFailure(ctx, id, request.Operation, started)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	select {
	case <-s.ctx.Done():
		result, actionErr := s.sessionFailure(id, request.Operation, started)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	default:
	}
	if dryRun {
		lineage.release()
		result := ActionResult{
			ActionID: id, Operation: request.Operation, Status: ActionPlanned,
			Backend: capability.Backend, DurationMillis: time.Since(started).Milliseconds(),
			PreconditionObservationID: preconditionID(request),
		}
		return s.finishSuccessfulActionAudit(ctx, result)
	}
	if err := s.executeAuthorized(ctx, request); err != nil {
		lineage.release()
		code, message := classifyBackendError(err)
		result, actionErr := actionFailure(id, request.Operation, started, code, message, err)
		if code == ErrorCleanupFailed || errors.Is(err, errPartialAction) {
			result.Status = ActionUnverified
		}
		result.Backend = capability.Backend
		result.PreconditionObservationID = preconditionID(request)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	lineage.release()
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

func capabilityUnavailableCause(code ErrorCode) error {
	switch code {
	case ErrorUnsupported:
		return robotgo.ErrNotSupported
	case ErrorPermissionDenied:
		return robotgo.ErrPermissionDenied
	default:
		return nil
	}
}

func (s *Session) sessionFailure(
	id string,
	operation Operation,
	started time.Time,
) (ActionResult, error) {
	if errors.Is(s.ctx.Err(), context.DeadlineExceeded) {
		return actionFailure(
			id, operation, started, ErrorTimedOut,
			"agent session lifetime expired", context.DeadlineExceeded,
		)
	}
	return actionFailure(
		id, operation, started, ErrorSessionClosed,
		"agent session is closed", ErrSessionClosed,
	)
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

func (s *Session) validateActionTargets(request ActionRequest) error {
	switch request.Operation {
	case OperationMove:
		return s.validateMoveTarget(*request.Move)
	case OperationScroll:
		return s.validateMoveTarget(MoveAction{
			X: request.Scroll.TargetX, Y: request.Scroll.TargetY,
			DisplayID: request.Scroll.DisplayID,
		})
	case OperationDrag:
		if err := s.validateMoveTarget(MoveAction{
			X: request.Drag.StartX, Y: request.Drag.StartY,
			DisplayID: request.Drag.DisplayID,
		}); err != nil {
			return err
		}
		return s.validateMoveTarget(MoveAction{
			X: request.Drag.EndX, Y: request.Drag.EndY,
			DisplayID: request.Drag.DisplayID,
		})
	case OperationKeyChord:
		return s.validateActiveWindow(*request.KeyChord)
	case OperationActivate:
		return s.validateWindowIdentity(*request.Activate)
	default:
		return nil
	}
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
	_, policyConfirmation := s.policy.requireConfirmation[request.Operation]
	if (policyConfirmation || mandatoryConfirmation(request.Operation)) && !request.Confirmed {
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
	if request.Click != nil {
		if _, allowed := s.policy.allowButton[request.Click.Button]; !allowed {
			return ErrPolicyDenied
		}
	}
	if request.TypeText != nil && utf8.RuneCountInString(request.TypeText.Text) > s.policy.MaxTextRunes {
		return ErrPolicyDenied
	}
	if request.Scroll != nil {
		if _, allowed := s.policy.allowDisplay[request.Scroll.DisplayID]; !allowed ||
			request.Scroll.Events > s.policy.MaxScrollEvents ||
			scrollDistance(*request.Scroll) > s.policy.MaxScrollDistance {
			return ErrPolicyDenied
		}
	}
	if request.Drag != nil {
		if _, allowed := s.policy.allowDisplay[request.Drag.DisplayID]; !allowed {
			return ErrPolicyDenied
		}
		if _, allowed := s.policy.allowButton[request.Drag.Button]; !allowed ||
			dragDistance(*request.Drag) > s.policy.MaxDragDistance ||
			request.Drag.DurationMillis > s.policy.MaxDragDurationMillis {
			return ErrPolicyDenied
		}
	}
	if request.KeyChord != nil {
		if _, allowed := s.policy.allowKey[request.KeyChord.Key]; !allowed ||
			uint32(len(request.KeyChord.Modifiers)+1) > s.policy.MaxChordKeys {
			return ErrPolicyDenied
		}
		for _, modifier := range request.KeyChord.Modifiers {
			if _, allowed := s.policy.allowModifier[modifier]; !allowed {
				return ErrPolicyDenied
			}
		}
		identity := windowTargetIdentity{
			target: request.KeyChord.TargetPID, kind: WindowTargetProcess,
		}
		if _, allowed := s.policy.allowWindow[identity]; !allowed {
			return ErrPolicyDenied
		}
	}
	if request.Activate != nil {
		identity := windowTargetIdentity{target: request.Activate.Target, kind: request.Activate.Kind}
		if _, allowed := s.policy.allowWindow[identity]; !allowed {
			return ErrPolicyDenied
		}
	}
	return nil
}

func validateRequest(request ActionRequest) error {
	switch request.Operation {
	case OperationObserve:
		return errors.New("desktop.observe must use Session.Observe")
	case OperationFindColor:
		return errors.New("desktop.find-color must use Session.FindColor")
	case OperationWaitColor:
		return errors.New("desktop.wait-color must use Session.WaitColor")
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
	if request.Scroll != nil {
		payloads++
	}
	if request.Drag != nil {
		payloads++
	}
	if request.TypeText != nil {
		payloads++
	}
	if request.KeyChord != nil {
		payloads++
	}
	if request.Activate != nil {
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
		if !validMouseButton(request.Click.Button) {
			return errors.New("unsupported mouse button")
		}
	case OperationScroll:
		if request.Scroll == nil || request.Scroll.DisplayID < 0 ||
			request.Scroll.Events == 0 ||
			request.Scroll.DeltaX == 0 && request.Scroll.DeltaY == 0 {
			return errors.New("scroll requires a display, non-zero delta, and positive event count")
		}
	case OperationDrag:
		if request.Drag == nil || request.Drag.DisplayID < 0 ||
			!validMouseButton(request.Drag.Button) ||
			request.Drag.DurationMillis <= 0 ||
			request.Drag.StartX == request.Drag.EndX && request.Drag.StartY == request.Drag.EndY {
			return errors.New("drag requires a display, button, distinct coordinates, and positive duration")
		}
	case OperationTypeText:
		if request.TypeText == nil || request.TypeText.Text == "" || !utf8.ValidString(request.TypeText.Text) {
			return errors.New("type-text requires non-empty valid UTF-8")
		}
	case OperationKeyChord:
		if request.KeyChord == nil || !validChordKey(request.KeyChord.Key) ||
			request.KeyChord.TargetPID <= 0 {
			return errors.New("keyboard chord requires one canonical supported key and positive target PID")
		}
		seen := make(map[KeyModifier]struct{}, len(request.KeyChord.Modifiers))
		for _, modifier := range request.KeyChord.Modifiers {
			if !validKeyModifier(modifier) {
				return errors.New("keyboard chord contains an unsupported modifier")
			}
			if _, duplicate := seen[modifier]; duplicate {
				return errors.New("keyboard chord contains a duplicate modifier")
			}
			seen[modifier] = struct{}{}
		}
	case OperationActivate:
		if request.Activate == nil || request.Activate.Target <= 0 ||
			!validWindowTargetKind(request.Activate.Kind) {
			return errors.New("window activation requires a positive target and valid kind")
		}
	default:
		return errors.New("unknown operation")
	}
	return nil
}

func (s *Session) execute(ctx context.Context, request ActionRequest) error {
	switch request.Operation {
	case OperationMove:
		return s.driver.Move(request.Move.X, request.Move.Y, request.Move.DisplayID)
	case OperationClick:
		return s.driver.Click(request.Click.Button, request.Click.Double)
	case OperationScroll:
		return s.executeScroll(ctx, *request.Scroll)
	case OperationDrag:
		return s.executeDrag(ctx, *request.Drag)
	case OperationTypeText:
		return s.driver.TypeText(request.TypeText.Text)
	case OperationKeyChord:
		return s.executeKeyChord(ctx, *request.KeyChord)
	case OperationActivate:
		return s.executeActivate(ctx, *request.Activate)
	default:
		return fmt.Errorf("%w: unknown operation", robotgo.ErrNotSupported)
	}
}

func (s *Session) executeAuthorized(ctx context.Context, request ActionRequest) error {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	if err := s.executionError(ctx); err != nil {
		return err
	}
	if err := s.validateActionRate(); err != nil {
		return err
	}
	s.used++
	s.lastAction = s.now()
	return s.execute(ctx, request)
}

func classifyBackendError(err error) (ErrorCode, string) {
	switch {
	case errors.Is(err, ErrSessionClosed):
		return ErrorSessionClosed, "agent session is closed"
	case errors.Is(err, ErrPolicyDenied):
		return ErrorPolicyDenied, "agent policy denied the action"
	case errors.Is(err, ErrStaleTarget):
		return ErrorStaleTarget, "agent observation target is stale"
	case errors.Is(err, ErrInputCleanup):
		return ErrorCleanupFailed, "pressed input cleanup failed; do not retry the action"
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
