package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// UIElementExpectation is the caller-visible semantic identity that must
// still match both the retained observation and the live native element.
type UIElementExpectation struct {
	Role      UIRole     `json:"role"`
	Name      string     `json:"name,omitempty"`
	Sensitive bool       `json:"sensitive,omitempty"`
	States    []UIState  `json:"states"`
	Bounds    *UIBounds  `json:"bounds"`
	Actions   []UIAction `json:"actions"`
}

// ElementActionRequest performs exactly one native semantic action. Value is
// accepted only for set-value and is never retained in results or audit data.
type ElementActionRequest struct {
	ObservationID string               `json:"observation_id"`
	ElementID     string               `json:"element_id"`
	Action        UIAction             `json:"action"`
	Expected      UIElementExpectation `json:"expected"`
	Value         string               `json:"value,omitempty"`
	Confirmed     bool                 `json:"confirmed,omitempty"`
}

type uiBackendElementAction struct {
	Target    uiBackendTarget
	Reference []byte
	Expected  UIElementExpectation
	Action    UIAction
	Value     string
	Backend   string
}

type uiElementActDriver interface {
	ActUIElement(context.Context, uiBackendElementAction) (bool, error)
}

func (robotGoDriver) ActUIElement(ctx context.Context, request uiBackendElementAction) (bool, error) {
	return actPlatformUIElement(ctx, request)
}

// ActUIElement revalidates and performs one observation-bound native semantic
// action. It never falls back to pointer or keyboard injection.
func (s *Session) ActUIElement(ctx context.Context, request ElementActionRequest) (ActionResult, error) {
	started := time.Now()
	id := fmt.Sprintf("action-%d", actionSerial.Add(1))
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.acquire(ctx); err != nil {
		return elementActionFailure(id, started, elementActionOperationError(err), false)
	}
	defer s.release()
	if err := s.ensureOpen(); err != nil {
		return elementActionFailure(id, started, elementActionOperationError(err), false)
	}
	if err := validateElementActionRequest(request); err != nil {
		return elementActionFailure(id, started, newActionError(ErrorInvalidInput, OperationElementAct, "invalid semantic element action", err), false)
	}
	if _, allowed := s.policy.allowOperation[OperationElementAct]; !allowed {
		return elementActionFailure(id, started, newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy denied semantic element actions", ErrPolicyDenied), false)
	}
	if _, required := s.policy.requireConfirmation[OperationElementAct]; required && !request.Confirmed {
		return elementActionFailure(id, started, newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy requires semantic element action confirmation", ErrPolicyDenied), false)
	}
	if _, allowed := s.policy.allowUIAction[request.Action]; !allowed {
		return elementActionFailure(id, started, newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy denied the semantic action", ErrPolicyDenied), false)
	}
	if request.Action == UIActionSetValue && len(request.Value) > int(s.policy.MaxUIActionValueBytes) {
		return elementActionFailure(id, started, newActionError(ErrorPolicyDenied, OperationElementAct, "semantic action value exceeds policy", ErrPolicyDenied), false)
	}
	record, ok := s.observation(request.ObservationID)
	if !ok || record.uiTarget == nil || record.uiBackend == "" || !record.uiActionable {
		return elementActionStale(id, started)
	}
	reference, ok := record.uiElements[request.ElementID]
	retainedExpected, expectedOK := record.uiExpected[request.ElementID]
	if !ok || !expectedOK || len(reference) == 0 || !equalUIExpectation(request.Expected, retainedExpected) {
		return elementActionStale(id, started)
	}
	if retainedExpected.Sensitive || !slices.Contains(retainedExpected.Actions, request.Action) {
		return elementActionStale(id, started)
	}
	if s.used >= s.policy.MaxActions {
		return elementActionFailure(id, started, newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy action limit reached", ErrPolicyDenied), false)
	}
	if err := s.validateActionRate(); err != nil {
		return elementActionFailure(id, started, newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy action rate limit reached", err), false)
	}
	if err := s.emitAudit(ctx, AuditEvent{Kind: AuditActionStarted, Operation: OperationElementAct, ActionID: id}); err != nil {
		return elementActionFailure(id, started, newActionError(ErrorAuditDelivery, OperationElementAct, "audit sink rejected semantic action intent", err), false)
	}
	s.used++
	s.lastAction = s.now()
	driver, ok := s.driver.(uiElementActDriver)
	if !ok {
		result, actionErr := elementActionFailure(id, started, newActionError(ErrorUnsupported, OperationElementAct, "semantic element actions are unsupported", errors.New("semantic action driver unavailable")), false)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	backendRequest := uiBackendElementAction{
		Target: *record.uiTarget, Reference: append([]byte(nil), reference...),
		Expected: retainedExpected, Action: request.Action, Value: request.Value,
		Backend: record.uiBackend,
	}
	defer clear(backendRequest.Reference)
	actionCtx, cancel := context.WithTimeout(ctx, time.Duration(s.policy.UIActionTimeoutMillis)*time.Millisecond)
	defer cancel()
	dispatched, err := driver.ActUIElement(actionCtx, backendRequest)
	if err != nil {
		result, actionErr := elementActionFailure(id, started, elementActionOperationError(err), dispatched)
		return s.finishFailedActionAudit(ctx, result, actionErr)
	}
	result := ActionResult{ActionID: id, Operation: OperationElementAct, Status: ActionSucceeded,
		Backend: record.uiBackend, DurationMillis: time.Since(started).Milliseconds()}
	return s.finishSuccessfulActionAudit(ctx, result)
}

func validateElementActionRequest(request ElementActionRequest) error {
	if !validObservationID(request.ObservationID) || !validUIElementID(request.ObservationID, request.ElementID) ||
		!validUIRole(request.Expected.Role) || !validUIAction(request.Action) ||
		!validUniqueUIStates(request.Expected.States) ||
		!validUniqueUIActions(request.Expected.Actions) ||
		!utf8.ValidString(request.Expected.Name) || len(request.Expected.Name) > maxAgentUIStringBytes {
		return errors.New("invalid semantic element identity or action")
	}
	if request.Expected.Sensitive || !slices.Contains(request.Expected.States, UIStateEnabled) ||
		!slices.Contains(request.Expected.Actions, request.Action) {
		return errors.New("semantic action was not offered by a non-sensitive observed element")
	}
	if !validUIBounds(request.Expected.Bounds) {
		return errors.New("invalid semantic element bounds")
	}
	if request.Action == UIActionSetValue {
		if !utf8.ValidString(request.Value) {
			return errors.New("set-value requires valid UTF-8")
		}
	} else if request.Value != "" {
		return errors.New("only set-value accepts a value")
	}
	return nil
}

func validUIElementID(observationID, elementID string) bool {
	prefix := observationID + "-element-"
	digits := strings.TrimPrefix(elementID, prefix)
	if digits == elementID || digits == "" || len(digits) > 20 || digits[0] == '0' {
		return false
	}
	value, err := strconv.ParseUint(digits, 10, 64)
	return err == nil && value != 0 && strconv.FormatUint(value, 10) == digits
}

func expectationFromUIElement(element *UIElement) UIElementExpectation {
	result := UIElementExpectation{
		Role: element.Role, Name: element.Name, Sensitive: element.Sensitive,
		States: append([]UIState(nil), element.States...), Actions: append([]UIAction(nil), element.Actions...),
	}
	if element.Bounds != nil {
		bounds := *element.Bounds
		result.Bounds = &bounds
	}
	return result
}

func equalUIExpectation(left, right UIElementExpectation) bool {
	return left.Role == right.Role && left.Name == right.Name && left.Sensitive == right.Sensitive &&
		slices.Equal(left.States, right.States) && slices.Equal(left.Actions, right.Actions) &&
		((left.Bounds == nil && right.Bounds == nil) || (left.Bounds != nil && right.Bounds != nil && *left.Bounds == *right.Bounds))
}

func elementActionStale(id string, started time.Time) (ActionResult, error) {
	return elementActionFailure(id, started, newActionError(ErrorStaleTarget, OperationElementAct, "semantic element target is stale", ErrStaleTarget), false)
}

func elementActionFailure(id string, started time.Time, actionErr *ActionError, dispatched bool) (ActionResult, error) {
	status := ActionFailed
	if dispatched {
		status = ActionUnverified
	}
	result := ActionResult{ActionID: id, Operation: OperationElementAct, Status: status,
		DurationMillis: time.Since(started).Milliseconds(), Error: actionErr}
	return result, actionErr
}

func elementActionOperationError(err error) *ActionError {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return newActionError(actionErr.Code, OperationElementAct, actionErr.Message, err)
	}
	code, message := classifyBackendError(err)
	return newActionError(code, OperationElementAct, message, err)
}
