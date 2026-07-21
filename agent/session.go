package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	robotgo "github.com/marang/robotgo"
)

type inputDriver interface {
	Move(x, y, displayID int) error
	Click(button MouseButton, double bool) error
	TypeText(text string) error
}

type robotGoDriver struct{}

func (robotGoDriver) Move(x, y, displayID int) error { return robotgo.MoveE(x, y, displayID) }
func (robotGoDriver) Click(button MouseButton, double bool) error {
	return robotgo.ClickE(string(button), double)
}
func (robotGoDriver) TypeText(text string) error { return robotgo.TypeStrE(text) }

// Config defines immutable session policy.
type Config struct {
	Policy Policy `json:"policy"`
}

// Session serializes policy-gated desktop mutations. The underlying RobotGo
// input backends remain process-global, so only one agent Session may exist.
type Session struct {
	policy  Policy
	driver  inputDriver
	catalog OperationCatalog
	ctx     context.Context
	cancel  context.CancelFunc

	actionGate chan struct{}
	used       uint64
	closeOnce  sync.Once
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
	return newSession(policy, robotGoDriver{}, capabilities)
}

func newSession(policy Policy, driver inputDriver, capabilities robotgo.RuntimeCapabilities) (*Session, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		policy: policy, driver: driver, catalog: buildCatalog(policy, capabilities),
		ctx: ctx, cancel: cancel, actionGate: make(chan struct{}, 1),
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
// preflight as Execute without injecting input or consuming action quota.
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
	if dryRun {
		return ActionResult{
			ActionID: id, Operation: request.Operation, Status: ActionPlanned,
			Backend: capability.Backend, DurationMillis: time.Since(started).Milliseconds(),
		}, nil
	}
	s.used++
	if err := s.execute(request); err != nil {
		code, message := classifyBackendError(err)
		result, actionErr := actionFailure(id, request.Operation, started, code, message, err)
		result.Backend = capability.Backend
		return result, actionErr
	}
	return ActionResult{
		ActionID: id, Operation: request.Operation, Status: ActionSucceeded,
		Backend: capability.Backend, DurationMillis: time.Since(started).Milliseconds(),
	}, nil
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
		return ErrorBackendFailure, "desktop backend action failed"
	}
}
