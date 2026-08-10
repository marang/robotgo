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

// UIElementConditionKind is the fixed target-relative semantic condition
// vocabulary supported by Action Proof v1.
type UIElementConditionKind string

const (
	UIElementConditionStatePresent           UIElementConditionKind = "state-present"
	UIElementConditionStateAbsent            UIElementConditionKind = "state-absent"
	UIElementConditionFocused                UIElementConditionKind = "focused"
	UIElementConditionNotFocused             UIElementConditionKind = "not-focused"
	UIElementConditionValueEqualsActionValue UIElementConditionKind = "value-equals-action-value"
)

var allUIElementConditionKinds = []UIElementConditionKind{
	UIElementConditionStatePresent,
	UIElementConditionStateAbsent,
	UIElementConditionFocused,
	UIElementConditionNotFocused,
	UIElementConditionValueEqualsActionValue,
}

// UIElementCondition is one optional desired state on the same retained
// observation element. Values are never accepted as condition operands;
// value-equals-action-value compares privately with ElementActionRequest.Value.
type UIElementCondition struct {
	Kind  UIElementConditionKind `json:"kind"`
	State UIState                `json:"state,omitempty"`
}

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

// ElementActionRequest performs at most one native semantic action. Value is
// accepted only for set-value and is never retained in results or audit data.
type ElementActionRequest struct {
	ObservationID string               `json:"observation_id"`
	ElementID     string               `json:"element_id"`
	Action        UIAction             `json:"action"`
	Expected      UIElementExpectation `json:"expected"`
	Postcondition *UIElementCondition  `json:"postcondition,omitempty"`
	Value         string               `json:"value,omitempty"`
	Confirmed     bool                 `json:"confirmed,omitempty"`
}

type uiBackendElementAction struct {
	Target        uiBackendTarget
	Reference     []byte
	Expected      UIElementExpectation
	Action        UIAction
	Postcondition *UIElementCondition
	Value         []byte
	Backend       string
}

type uiBackendElementConditionResult struct {
	Satisfied       bool
	CleanupComplete bool
}

type uiBackendElementActionResult struct {
	Dispatched       bool
	AlreadySatisfied bool
	CleanupComplete  bool
}

type uiElementActDriver interface {
	ActUIElement(context.Context, uiBackendElementAction) (uiBackendElementActionResult, error)
}

type uiElementCheckDriver interface {
	CheckUIElement(context.Context, uiBackendElementAction) (uiBackendElementConditionResult, error)
}

type retainedUIElementAction struct {
	target    uiBackendTarget
	reference []byte
	expected  UIElementExpectation
	backend   string
}

func (robotGoDriver) ActUIElement(ctx context.Context, request uiBackendElementAction) (uiBackendElementActionResult, error) {
	return actPlatformUIElement(ctx, request)
}

func (robotGoDriver) CheckUIElement(ctx context.Context, request uiBackendElementAction) (uiBackendElementConditionResult, error) {
	return checkPlatformUIElement(ctx, request)
}

// ActUIElement revalidates and performs at most one observation-bound native
// semantic action. It never falls back to pointer, keyboard, capture, OCR, or
// shell behavior. Every return contains Action Proof v1.
func (s *Session) ActUIElement(ctx context.Context, request ElementActionRequest) (result ActionResult, returnErr error) {
	started := time.Now()
	id := fmt.Sprintf("action-%d", actionSerial.Add(1))
	if ctx == nil {
		ctx = context.Background()
	}
	proof := &ActionProof{
		SchemaVersion: ActionProofSchemaVersion,
		TransactionID: id,
		Status:        ActionProofRejectedBeforeDispatch,
		Execution: ActionExecutionProof{
			Status: ActionExecutionNotDispatched,
		},
	}
	result = ActionResult{
		ActionID: id, Operation: OperationElementAct, Status: ActionFailed, Proof: proof,
	}

	var (
		acquired               bool
		intentAccepted         bool
		terminalPhase          = UIConditionPhaseNotRequested
		retained               retainedUIElementAction
		backendRequest         uiBackendElementAction
		backendCleanupComplete = true
	)
	defer func() {
		clear(retained.reference)
		clear(backendRequest.Reference)
		clear(backendRequest.Value)
		retained = retainedUIElementAction{}
		backendRequest = uiBackendElementAction{}
		proof.Cleanup.TransientResourcesReleased = backendCleanupComplete
		result.DurationMillis = time.Since(started).Milliseconds()
		if intentAccepted {
			result, returnErr = s.finishElementActionAudit(result, terminalPhase, returnErr)
		}
		if acquired {
			s.release()
		}
	}()

	if err := s.acquire(ctx); err != nil {
		returnErr = setElementActionFailure(&result, s.elementActionOperationError(err), ActionProofRejectedBeforeDispatch, false)
		return result, returnErr
	}
	acquired = true
	if err := s.ensureOpen(); err != nil {
		returnErr = setElementActionFailure(&result, s.elementActionOperationError(err), ActionProofRejectedBeforeDispatch, false)
		return result, returnErr
	}
	if err := validateElementActionRequest(request); err != nil {
		returnErr = setElementActionFailure(&result,
			newActionError(ErrorInvalidInput, OperationElementAct, "invalid semantic element action", err),
			ActionProofRejectedBeforeDispatch, false)
		return result, returnErr
	}
	result.PreconditionObservationID = request.ObservationID
	proof.Execution.Action = request.Action

	_, confirmationRequired := s.policy.requireConfirmation[OperationElementAct]
	proof.Authorization = &ActionAuthorizationProof{
		ConfirmationRequired: confirmationRequired,
		Confirmed:            request.Confirmed,
	}
	if err := s.authorizeElementActionPolicy(request); err != nil {
		returnErr = setElementActionFailure(&result, err, ActionProofRejectedBeforeDispatch, false)
		return result, returnErr
	}
	proof.Authorization.PolicyAllowed = true
	if confirmationRequired && !request.Confirmed {
		returnErr = setElementActionFailure(&result,
			newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy requires semantic element action confirmation", ErrPolicyDenied),
			ActionProofRejectedBeforeDispatch, false)
		return result, returnErr
	}

	retained, _ = s.retainUIElementAction(request.ObservationID, request.ElementID)
	proof.Resolution = &ActionResolutionProof{Strategy: ActionResolutionRetainedReference}
	if len(retained.reference) == 0 {
		returnErr = setElementActionFailure(&result, staleElementActionError(), ActionProofRejectedBeforeDispatch, false)
		return result, returnErr
	}
	proof.Resolution.CandidateCount = 1
	proof.Execution.Backend = retained.backend
	result.Backend = retained.backend
	if !equalUIExpectation(request.Expected, retained.expected) || retained.expected.Sensitive ||
		!slices.Contains(retained.expected.Actions, request.Action) {
		returnErr = setElementActionFailure(&result, staleElementActionError(), ActionProofRejectedBeforeDispatch, false)
		return result, returnErr
	}
	proof.Resolution.Exact = true

	backendRequest = uiBackendElementAction{
		Target: retained.target, Reference: append([]byte(nil), retained.reference...),
		Expected: cloneUIElementExpectation(retained.expected), Action: request.Action,
		Postcondition: cloneUIElementCondition(request.Postcondition),
		Value:         append([]byte(nil), request.Value...), Backend: retained.backend,
	}
	if request.Postcondition != nil {
		proof.Verification = &ActionVerificationProof{
			ConditionKind: request.Postcondition.Kind,
			Status:        ActionVerificationNotMatched,
		}
		terminalPhase = UIConditionPhasePrecheck
		if !s.hasUIElementReadCapacity(uint64(s.policy.UIVerificationAttempts) + 1) {
			returnErr = setElementActionFailure(&result,
				newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy semantic verification quota reached", ErrPolicyDenied),
				ActionProofRejectedBeforeDispatch, false)
			return result, returnErr
		}
	} else {
		proof.Verification = &ActionVerificationProof{Status: ActionVerificationNotRequested}
		if s.used >= s.policy.MaxActions {
			returnErr = setElementActionFailure(&result,
				newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy action limit reached", ErrPolicyDenied),
				ActionProofRejectedBeforeDispatch, false)
			return result, returnErr
		}
		if err := s.validateActionRate(); err != nil {
			returnErr = setElementActionFailure(&result,
				newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy action rate limit reached", err),
				ActionProofRejectedBeforeDispatch, false)
			return result, returnErr
		}
	}

	startedEvent := AuditEvent{
		Kind: AuditActionStarted, Operation: OperationElementAct, ActionID: id,
		PreconditionObservationID: result.PreconditionObservationID,
		ActionExecutionStatus:     ActionExecutionNotDispatched,
	}
	if request.Postcondition != nil {
		startedEvent.UIConditionKind = request.Postcondition.Kind
		startedEvent.UIConditionPhase = UIConditionPhasePrecheck
	}
	if err := s.emitAudit(ctx, startedEvent); err != nil {
		returnErr = setElementActionFailure(&result,
			newActionError(ErrorAuditDelivery, OperationElementAct, "audit sink rejected semantic action intent", err),
			ActionProofRejectedBeforeDispatch, false)
		return result, returnErr
	}
	intentAccepted = true

	actDriver, ok := s.driver.(uiElementActDriver)
	if !ok {
		returnErr = setElementActionFailure(&result,
			newActionError(ErrorUnsupported, OperationElementAct, "semantic element actions are unsupported", errors.New("semantic action driver unavailable")),
			ActionProofFailedBeforeDispatch, false)
		return result, returnErr
	}

	if request.Postcondition != nil {
		checkDriver, ok := s.driver.(uiElementCheckDriver)
		if !ok {
			returnErr = setElementActionFailure(&result,
				newActionError(ErrorUnsupported, OperationElementAct, "semantic element verification is unsupported", errors.New("semantic verification driver unavailable")),
				ActionProofFailedBeforeDispatch, false)
			return result, returnErr
		}
		precheckResult, err := s.checkUIElementCondition(ctx, checkDriver, backendRequest)
		proof.Verification.PrecheckAttempts = precheckResult.attempts
		backendCleanupComplete = backendCleanupComplete && precheckResult.cleanupComplete
		if !precheckResult.cleanupComplete {
			returnErr = s.setElementActionCleanupPending(&result, err)
			return result, returnErr
		}
		if err != nil {
			proof.Verification.Status = ActionVerificationFailed
			returnErr = setElementActionFailure(&result, s.elementActionOperationError(err), ActionProofFailedBeforeDispatch, false)
			return result, returnErr
		}
		if precheckResult.satisfied {
			proof.Verification.Status = ActionVerificationMatched
			proof.Verification.AlreadySatisfied = true
			proof.Execution.Status = ActionExecutionSkippedAlreadySatisfied
			proof.Status = ActionProofVerified
			result.Status = ActionSucceeded
			return result, nil
		}

		if s.used >= s.policy.MaxActions {
			returnErr = setElementActionFailure(&result,
				newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy action limit reached", ErrPolicyDenied),
				ActionProofRejectedBeforeDispatch, false)
			return result, returnErr
		}
		if err := s.validateActionRate(); err != nil {
			returnErr = setElementActionFailure(&result,
				newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy action rate limit reached", err),
				ActionProofRejectedBeforeDispatch, false)
			return result, returnErr
		}
	}
	if err := s.elementActionContextError(ctx); err != nil {
		if request.Postcondition != nil {
			proof.Verification.Status = ActionVerificationFailed
		}
		returnErr = setElementActionFailure(
			&result, s.elementActionOperationError(err), ActionProofFailedBeforeDispatch, false,
		)
		return result, returnErr
	}

	actionCtx, cancelAction := s.elementActionContext(ctx, s.policy.UIActionTimeoutMillis)
	actResult, actErr := actDriver.ActUIElement(actionCtx, backendRequest)
	cancelAction()
	backendCleanupComplete = backendCleanupComplete && actResult.CleanupComplete
	if request.Postcondition != nil {
		// A pre-dispatch error may occur before native revalidation starts.
		// Only a skip or dispatch proves that the final gate completed.
		proof.Verification.FinalGateChecked = actResult.AlreadySatisfied || actResult.Dispatched
		terminalPhase = UIConditionPhaseFinalGate
	}
	if actResult.Dispatched {
		s.used++
		s.lastAction = s.now()
		proof.Execution.Status = ActionExecutionDispatched
	} else if actResult.AlreadySatisfied {
		proof.Execution.Status = ActionExecutionSkippedAlreadySatisfied
	}

	if actResult.Dispatched && actResult.AlreadySatisfied {
		actErr = errors.Join(actErr, errors.New("semantic backend returned contradictory action outcome"))
	}
	if !actResult.CleanupComplete {
		returnErr = s.setElementActionCleanupPending(&result, actErr)
		return result, returnErr
	}
	if actResult.AlreadySatisfied && actErr == nil {
		if request.Postcondition == nil {
			actErr = errors.New("semantic backend skipped an action without a postcondition")
		} else {
			proof.Verification.Status = ActionVerificationMatched
			proof.Verification.AlreadySatisfied = true
			proof.Status = ActionProofVerified
			result.Status = ActionSucceeded
			return result, nil
		}
	}
	if !actResult.Dispatched {
		if actErr == nil {
			actErr = errors.New("semantic backend returned without dispatch or an already-satisfied result")
		}
		returnErr = setElementActionFailure(&result, s.elementActionOperationError(actErr), ActionProofFailedBeforeDispatch, false)
		return result, returnErr
	}

	var executionErr *ActionError
	if actErr != nil {
		executionErr = s.elementActionOperationError(actErr)
	}
	if request.Postcondition == nil {
		proof.Status = ActionProofUnverifiedAfterDispatch
		result.Status = ActionSucceeded
		if executionErr != nil {
			returnErr = setElementActionFailure(&result, executionErr, ActionProofUnverifiedAfterDispatch, true)
		}
		return result, returnErr
	}

	terminalPhase = UIConditionPhasePostDispatch
	checkDriver := s.driver.(uiElementCheckDriver)
	verification, verificationErr := s.pollUIElementCondition(ctx, checkDriver, backendRequest)
	proof.Verification.Status = verification.status
	proof.Verification.PostconditionAttempts = verification.attempts
	backendCleanupComplete = backendCleanupComplete && verification.cleanupComplete
	if !verification.cleanupComplete {
		returnErr = s.setElementActionCleanupPending(&result, errors.Join(executionErr, verificationErr))
		return result, returnErr
	}
	if executionErr != nil {
		proof.Status = ActionProofUnverifiedAfterDispatch
		result.Status = ActionUnverified
		result.Error = executionErr
		proof.ErrorCode = executionErr.Code
		if verificationErr == nil {
			return result, executionErr
		}
		return result, errors.Join(executionErr, s.elementActionOperationError(verificationErr))
	}
	if verificationErr != nil {
		returnErr = setElementActionFailure(&result, s.elementActionOperationError(verificationErr), ActionProofUnverifiedAfterDispatch, true)
		return result, returnErr
	}
	if verification.status != ActionVerificationMatched {
		returnErr = setElementActionFailure(&result,
			newActionError(ErrorVerification, OperationElementAct, "semantic element postcondition was not verified", ErrVerification),
			ActionProofUnverifiedAfterDispatch, true)
		return result, returnErr
	}
	proof.Status = ActionProofVerified
	result.Status = ActionSucceeded
	return result, nil
}

type uiElementConditionCheck struct {
	satisfied       bool
	attempts        uint32
	cleanupComplete bool
}

func (s *Session) checkUIElementCondition(
	ctx context.Context,
	driver uiElementCheckDriver,
	request uiBackendElementAction,
) (uiElementConditionCheck, error) {
	result := uiElementConditionCheck{cleanupComplete: true}
	checkCtx, cancel := s.elementActionContext(ctx, s.policy.UIVerificationTimeoutMillis)
	defer cancel()
	if err := s.elementActionContextError(checkCtx); err != nil {
		return result, err
	}
	if err := s.consumeUIElementRead(checkCtx); err != nil {
		return result, err
	}
	if err := s.elementActionContextError(checkCtx); err != nil {
		return result, err
	}
	result.attempts = 1
	backendResult, err := driver.CheckUIElement(checkCtx, request)
	result.satisfied = backendResult.Satisfied
	result.cleanupComplete = backendResult.CleanupComplete
	return result, err
}

type uiElementConditionPolling struct {
	status          ActionVerificationStatus
	attempts        uint32
	cleanupComplete bool
}

func (s *Session) pollUIElementCondition(
	ctx context.Context,
	driver uiElementCheckDriver,
	request uiBackendElementAction,
) (uiElementConditionPolling, error) {
	result := uiElementConditionPolling{
		status: ActionVerificationNotMatched, cleanupComplete: true,
	}
	verificationCtx, cancel := s.elementActionContext(ctx, s.policy.UIVerificationTimeoutMillis)
	defer cancel()
	for attempt := uint32(1); attempt <= s.policy.UIVerificationAttempts; attempt++ {
		if err := s.elementActionContextError(verificationCtx); err != nil {
			result.status = ActionVerificationFailed
			return result, err
		}
		if attempt > 1 {
			if err := s.waitUIElementVerification(verificationCtx); err != nil {
				result.status = ActionVerificationFailed
				return result, err
			}
		}
		if err := s.consumeUIElementRead(verificationCtx); err != nil {
			result.status = ActionVerificationFailed
			return result, err
		}
		if err := s.elementActionContextError(verificationCtx); err != nil {
			result.status = ActionVerificationFailed
			return result, err
		}
		result.attempts++
		backendResult, err := driver.CheckUIElement(verificationCtx, request)
		result.cleanupComplete = result.cleanupComplete && backendResult.CleanupComplete
		if !backendResult.CleanupComplete {
			result.status = ActionVerificationFailed
			return result, err
		}
		if err != nil {
			result.status = ActionVerificationFailed
			return result, err
		}
		if backendResult.Satisfied {
			result.status = ActionVerificationMatched
			return result, nil
		}
	}
	return result, nil
}

func (s *Session) waitUIElementVerification(ctx context.Context) error {
	if err := s.elementActionContextError(ctx); err != nil {
		return err
	}
	delay := time.Duration(s.policy.UIVerificationIntervalMillis) * time.Millisecond
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-timer.C:
		return s.elementActionContextError(ctx)
	}
}

func (s *Session) consumeUIElementRead(ctx context.Context) error {
	if err := s.elementActionContextError(ctx); err != nil {
		return err
	}
	now := s.now()
	if !s.lastUIQuery.IsZero() {
		elapsed := now.Sub(s.lastUIQuery)
		minimum := time.Duration(s.policy.MinUIQueryIntervalMillis) * time.Millisecond
		if elapsed < 0 {
			return ErrPolicyDenied
		}
		if elapsed < minimum {
			timer := time.NewTimer(minimum - elapsed)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.ctx.Done():
				return s.ctx.Err()
			case <-timer.C:
			}
			next := s.lastUIQuery.Add(minimum)
			now = s.now()
			if now.Before(next) {
				now = next
			}
		}
	}
	if err := s.elementActionContextError(ctx); err != nil {
		return err
	}
	if s.usedQueries >= s.policy.MaxQueries || s.usedObservations >= s.policy.MaxObservations {
		return ErrPolicyDenied
	}
	s.usedQueries++
	s.usedObservations++
	s.lastUIQuery = now
	return nil
}

func (s *Session) elementActionContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Session) hasUIElementReadCapacity(required uint64) bool {
	if s.usedQueries > s.policy.MaxQueries || s.usedObservations > s.policy.MaxObservations {
		return false
	}
	return required <= s.policy.MaxQueries-s.usedQueries &&
		required <= s.policy.MaxObservations-s.usedObservations
}

func (s *Session) elementActionContext(ctx context.Context, timeoutMillis int) (context.Context, context.CancelFunc) {
	phaseCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMillis)*time.Millisecond)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	return phaseCtx, func() {
		stopSessionCancel()
		cancel()
	}
}

func (s *Session) finishElementActionAudit(
	result ActionResult,
	phase UIConditionPhase,
	operationErr error,
) (ActionResult, error) {
	auditCtx, cancel := context.WithTimeout(context.Background(), uiCompletionAuditTimeout)
	defer cancel()
	verificationEvent := semanticActionAuditEvent(result, phase)
	verificationEvent.Kind = AuditVerificationFinished
	actionEvent := semanticActionAuditEvent(result, phase)
	actionEvent.Kind = AuditActionFinished
	actionEvent.ActionStatus = result.Status
	var auditErr error
	if err := s.emitAudit(auditCtx, verificationEvent); err != nil {
		auditErr = errors.Join(auditErr, err)
	}
	if err := s.emitAudit(auditCtx, actionEvent); err != nil {
		auditErr = errors.Join(auditErr, err)
	}
	if auditErr == nil {
		return result, operationErr
	}
	deliveryErr := newActionError(
		ErrorAuditDelivery, OperationElementAct,
		"semantic action completed but terminal audit delivery failed", auditErr,
	)
	return result, errors.Join(operationErr, deliveryErr)
}

func semanticActionAuditEvent(result ActionResult, phase UIConditionPhase) AuditEvent {
	event := AuditEvent{
		Operation: OperationElementAct, ActionID: result.ActionID,
		PreconditionObservationID: result.PreconditionObservationID,
		UIConditionPhase:          phase,
	}
	if result.Error != nil {
		event.ErrorCode = result.Error.Code
	}
	if result.Proof == nil {
		return event
	}
	event.ActionProofStatus = result.Proof.Status
	event.ActionExecutionStatus = result.Proof.Execution.Status
	if result.Proof.Verification != nil {
		event.UIConditionKind = result.Proof.Verification.ConditionKind
		event.UIPrecheckAttempts = result.Proof.Verification.PrecheckAttempts
		event.UIFinalGateChecked = result.Proof.Verification.FinalGateChecked
		event.UIPostconditionAttempts = result.Proof.Verification.PostconditionAttempts
	}
	return event
}

func (s *Session) retainUIElementAction(observationID, elementID string) (retainedUIElementAction, bool) {
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	record, ok := s.observations[observationID]
	if !ok || record.uiTarget == nil || record.uiBackend == "" || !record.uiActionable {
		return retainedUIElementAction{}, false
	}
	reference, referenceOK := record.uiElements[elementID]
	expected, expectedOK := record.uiExpected[elementID]
	if !referenceOK || !expectedOK || len(reference) == 0 {
		return retainedUIElementAction{}, false
	}
	return retainedUIElementAction{
		target: *record.uiTarget, reference: append([]byte(nil), reference...),
		expected: cloneUIElementExpectation(expected), backend: record.uiBackend,
	}, true
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
	return validateUIElementCondition(request.Action, request.Postcondition)
}

func validateUIElementCondition(action UIAction, condition *UIElementCondition) error {
	if condition == nil {
		return nil
	}
	switch condition.Kind {
	case UIElementConditionStatePresent, UIElementConditionStateAbsent:
		if !validUIState(condition.State) {
			return errors.New("state condition requires exactly one valid state")
		}
	case UIElementConditionFocused, UIElementConditionNotFocused:
		if condition.State != "" {
			return errors.New("focus condition does not accept a state")
		}
	case UIElementConditionValueEqualsActionValue:
		if condition.State != "" || action != UIActionSetValue {
			return errors.New("value condition requires set-value and does not accept a state")
		}
	default:
		return errors.New("unknown semantic element condition")
	}
	return nil
}

func (s *Session) authorizeElementActionPolicy(request ElementActionRequest) *ActionError {
	if _, allowed := s.policy.allowOperation[OperationElementAct]; !allowed {
		return newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy denied semantic element actions", ErrPolicyDenied)
	}
	if _, allowed := s.policy.allowUIAction[request.Action]; !allowed {
		return newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy denied the semantic action", ErrPolicyDenied)
	}
	if request.Action == UIActionSetValue && len(request.Value) > int(s.policy.MaxUIActionValueBytes) {
		return newActionError(ErrorPolicyDenied, OperationElementAct, "semantic action value exceeds policy", ErrPolicyDenied)
	}
	if request.Postcondition == nil {
		return nil
	}
	if s.policy.UIVerificationAttempts == 0 || s.policy.UIVerificationTimeoutMillis == 0 {
		return newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy denied semantic verification", ErrPolicyDenied)
	}
	requiredProperty := UIPropertyState
	switch request.Postcondition.Kind {
	case UIElementConditionFocused, UIElementConditionNotFocused:
		requiredProperty = UIPropertyFocus
	case UIElementConditionValueEqualsActionValue:
		requiredProperty = UIPropertyValue
	}
	if _, allowed := s.policy.allowUIProperty[requiredProperty]; !allowed {
		return newActionError(ErrorPolicyDenied, OperationElementAct, "agent policy denied the semantic verification property", ErrPolicyDenied)
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

func cloneUIElementExpectation(expected UIElementExpectation) UIElementExpectation {
	result := expected
	result.States = append([]UIState(nil), expected.States...)
	result.Actions = append([]UIAction(nil), expected.Actions...)
	if expected.Bounds != nil {
		bounds := *expected.Bounds
		result.Bounds = &bounds
	}
	return result
}

func cloneUIElementCondition(condition *UIElementCondition) *UIElementCondition {
	if condition == nil {
		return nil
	}
	result := *condition
	return &result
}

func equalUIExpectation(left, right UIElementExpectation) bool {
	return left.Role == right.Role && left.Name == right.Name && left.Sensitive == right.Sensitive &&
		slices.Equal(left.States, right.States) && slices.Equal(left.Actions, right.Actions) &&
		((left.Bounds == nil && right.Bounds == nil) || (left.Bounds != nil && right.Bounds != nil && *left.Bounds == *right.Bounds))
}

func staleElementActionError() *ActionError {
	return newActionError(ErrorStaleTarget, OperationElementAct, "semantic element target is stale", ErrStaleTarget)
}

func setElementActionFailure(
	result *ActionResult,
	actionErr *ActionError,
	proofStatus ActionProofStatus,
	dispatched bool,
) error {
	result.Status = ActionFailed
	if dispatched {
		result.Status = ActionUnverified
	}
	result.Error = actionErr
	result.Proof.Status = proofStatus
	result.Proof.ErrorCode = actionErr.Code
	return actionErr
}

func (s *Session) setElementActionCleanupPending(result *ActionResult, cause error) error {
	var safeCause error
	if cause != nil {
		safeCause = s.elementActionOperationError(cause)
	}
	cleanupErr := newActionError(
		ErrorCleanupFailed, OperationElementAct,
		"semantic action transient cleanup is incomplete; do not retry", ErrInputCleanup,
	)
	result.Status = ActionUnverified
	result.Proof.Status = ActionProofCleanupPending
	result.Proof.ErrorCode = ErrorCleanupFailed
	if result.Proof.Verification != nil && result.Proof.Verification.Status != ActionVerificationNotRequested {
		result.Proof.Verification.Status = ActionVerificationFailed
	}
	if result.Error == nil {
		result.Error = cleanupErr
	}
	// Unknown backend cleanup may leave an operation-owned native resource
	// alive. Cancel the entire session so no later action can reuse that state.
	s.cancel()
	if safeCause == nil {
		return cleanupErr
	}
	// Cleanup uncertainty dominates every earlier failure: errors.As must expose
	// cleanup-failed first so direct callers cannot mistake this for retryable
	// pre-dispatch backend failure.
	return errors.Join(cleanupErr, safeCause)
}

func (s *Session) elementActionOperationError(err error) *ActionError {
	if sessionErr := s.ctx.Err(); sessionErr != nil {
		if errors.Is(sessionErr, context.DeadlineExceeded) {
			return newActionError(ErrorTimedOut, OperationElementAct, "agent session lifetime expired", context.DeadlineExceeded)
		}
		return newActionError(ErrorSessionClosed, OperationElementAct, "agent session is closed", ErrSessionClosed)
	}
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return newActionError(actionErr.Code, OperationElementAct, actionErr.Message, err)
	}
	code, message := classifyBackendError(err)
	return newActionError(code, OperationElementAct, message, err)
}
