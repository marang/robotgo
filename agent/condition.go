package agent

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	// ConditionSchemaVersion identifies the serialized visual-condition contract.
	ConditionSchemaVersion = "1"
	conditionIDPrefix      = "condition-"
	maximumRGBDistance     = 442.0
)

var conditionSerial atomic.Uint64

// ColorCondition describes one RGB target. Tolerance is a normalized
// Euclidean RGB distance in the inclusive range 0 through 1. Alpha is ignored.
type ColorCondition struct {
	Red       uint8   `json:"red"`
	Green     uint8   `json:"green"`
	Blue      uint8   `json:"blue"`
	Tolerance float64 `json:"tolerance"`
}

// FindColorRequest searches only a previously captured, live observation. It
// never causes an implicit desktop capture.
type FindColorRequest struct {
	ObservationID string         `json:"observation_id"`
	Condition     ColorCondition `json:"condition"`
	Confirmed     bool           `json:"confirmed,omitempty"`
}

// WaitColorRequest polls one explicit capture region using the immutable
// attempt, interval, timeout, and quota limits in Policy.
type WaitColorRequest struct {
	Region    CaptureRegion  `json:"region"`
	Condition ColorCondition `json:"condition"`
	Confirmed bool           `json:"confirmed,omitempty"`
}

// VisualMatch identifies the first matching global logical coordinate.
type VisualMatch struct {
	X         int `json:"x"`
	Y         int `json:"y"`
	DisplayID int `json:"display_id"`
}

// ConditionStatus identifies whether a visual condition matched.
type ConditionStatus string

const (
	ConditionMatched    ConditionStatus = "matched"
	ConditionNotMatched ConditionStatus = "not-matched"
)

// FindColorResult contains no pixels, capture digest, or target color.
type FindColorResult struct {
	SchemaVersion string          `json:"schema_version"`
	ConditionID   string          `json:"condition_id"`
	ObservationID string          `json:"observation_id"`
	Status        ConditionStatus `json:"status"`
	Match         *VisualMatch    `json:"match,omitempty"`
}

// WaitColorResult contains bounded polling evidence but no pixels, capture
// digest, or target color. A matched result identifies the retained observation.
type WaitColorResult struct {
	SchemaVersion string          `json:"schema_version"`
	ConditionID   string          `json:"condition_id"`
	Status        ConditionStatus `json:"status"`
	Attempts      uint32          `json:"attempts"`
	ObservationID string          `json:"observation_id,omitempty"`
	Match         *VisualMatch    `json:"match,omitempty"`
}

// FindColor evaluates a color condition against an explicit session-owned
// observation without touching the desktop.
func (s *Session) FindColor(ctx context.Context, request FindColorRequest) (FindColorResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := FindColorResult{
		SchemaVersion: ConditionSchemaVersion,
		ConditionID:   newConditionID(),
		Status:        ConditionNotMatched,
	}
	if err := validateFindColorRequest(request); err != nil {
		return result, conditionError(ErrorInvalidInput, OperationFindColor, "invalid color-search request", err)
	}
	result.ObservationID = request.ObservationID
	if err := s.acquire(ctx); err != nil {
		return result, conditionOperationError(OperationFindColor, err)
	}
	defer s.release()
	if err := ctx.Err(); err != nil {
		return result, conditionOperationError(OperationFindColor, observationContextError(ctx))
	}
	if err := s.ensureOpen(); err != nil {
		return result, conditionOperationError(OperationFindColor, err)
	}
	if err := s.authorizeCondition(OperationFindColor, request.Confirmed); err != nil {
		return result, err
	}
	if s.usedQueries >= s.policy.MaxQueries {
		return result, conditionError(ErrorPolicyDenied, OperationFindColor, "agent policy query limit reached", ErrPolicyDenied)
	}
	record, ok := s.observation(request.ObservationID)
	if !ok || !record.hasCapture || !record.capture.usable() {
		return result, conditionError(ErrorStaleTarget, OperationFindColor, "color-search observation is unavailable", ErrStaleTarget)
	}
	if err := s.emitConditionAudit(ctx, AuditConditionStarted, OperationFindColor, result.ConditionID, request.ObservationID, "", 0, ""); err != nil {
		return result, conditionError(ErrorAuditDelivery, OperationFindColor, "audit sink rejected color-search intent", err)
	}
	s.usedQueries++
	findCtx, cancel := context.WithCancel(ctx)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	defer func() {
		stopSessionCancel()
		cancel()
	}()
	match, err := findColorInCapture(findCtx, record.capture, record.region, request.Condition)
	if err != nil {
		err = s.normalizeConditionError(OperationFindColor, err)
		return result, s.finishConditionFailure(ctx, OperationFindColor, result.ConditionID, request.ObservationID, result.Status, 1, err)
	}
	if match != nil {
		result.Status = ConditionMatched
		result.Match = match
	}
	if err := s.emitConditionAudit(ctx, AuditConditionFinished, OperationFindColor, result.ConditionID, request.ObservationID, result.Status, 1, ""); err != nil {
		return result, conditionError(ErrorAuditDelivery, OperationFindColor, "color search completed but audit delivery failed", err)
	}
	return result, nil
}

// WaitColor captures and evaluates an explicit region until it matches or the
// bounded policy is exhausted. A successful result references the sole
// retained observation by ID; callers should pass it to ReleaseObservation
// when no longer needed. Every other temporary frame is zeroed before return.
func (s *Session) WaitColor(ctx context.Context, request WaitColorRequest) (WaitColorResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := WaitColorResult{
		SchemaVersion: ConditionSchemaVersion,
		ConditionID:   newConditionID(),
		Status:        ConditionNotMatched,
	}
	if err := validateWaitColorRequest(request); err != nil {
		return result, conditionError(ErrorInvalidInput, OperationWaitColor, "invalid color-wait request", err)
	}
	if err := s.acquire(ctx); err != nil {
		return result, conditionOperationError(OperationWaitColor, err)
	}
	defer s.release()
	if err := ctx.Err(); err != nil {
		return result, conditionOperationError(OperationWaitColor, observationContextError(ctx))
	}
	if err := s.ensureOpen(); err != nil {
		return result, conditionOperationError(OperationWaitColor, err)
	}
	if err := s.authorizeCondition(OperationWaitColor, request.Confirmed); err != nil {
		return result, err
	}
	if _, allowed := s.policy.allowDisplay[request.Region.DisplayID]; !allowed {
		return result, conditionError(ErrorPolicyDenied, OperationWaitColor, "agent policy denied the wait display", ErrPolicyDenied)
	}
	if err := validateCaptureRegion(request.Region, s.policy.MaxCapturePixels); err != nil {
		return result, conditionError(ErrorPolicyDenied, OperationWaitColor, "agent policy denied the wait capture size", ErrPolicyDenied)
	}
	if s.usedQueries >= s.policy.MaxQueries {
		return result, conditionError(ErrorPolicyDenied, OperationWaitColor, "agent policy query limit reached", ErrPolicyDenied)
	}
	required := uint64(s.policy.WaitAttempts)
	if s.usedObservations > s.policy.MaxObservations || required > s.policy.MaxObservations-s.usedObservations {
		return result, conditionError(ErrorPolicyDenied, OperationWaitColor, "agent policy observation limit cannot satisfy bounded wait", ErrPolicyDenied)
	}
	if err := s.emitConditionAudit(ctx, AuditConditionStarted, OperationWaitColor, result.ConditionID, "", "", 0, ""); err != nil {
		return result, conditionError(ErrorAuditDelivery, OperationWaitColor, "audit sink rejected color-wait intent", err)
	}
	s.usedQueries++
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(s.policy.WaitTimeoutMillis)*time.Millisecond)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	defer func() {
		stopSessionCancel()
		cancel()
	}()

	for attempt := uint32(1); attempt <= s.policy.WaitAttempts; attempt++ {
		result.Attempts = attempt
		frame, err := s.capture(waitCtx, request.Region, true)
		if err != nil {
			err = s.normalizeConditionError(OperationWaitColor, err)
			return result, s.finishConditionFailure(ctx, OperationWaitColor, result.ConditionID, "", result.Status, attempt, err)
		}
		match, matchErr := findColorInCapture(waitCtx, frame.buffer, request.Region, request.Condition)
		if matchErr != nil {
			_ = frame.buffer.close()
			matchErr = s.normalizeConditionError(OperationWaitColor, matchErr)
			return result, s.finishConditionFailure(ctx, OperationWaitColor, result.ConditionID, "", result.Status, attempt, matchErr)
		}
		if match != nil {
			diagnostics, diagnosticsErr := s.runtimeDiagnostics(waitCtx)
			if diagnosticsErr != nil {
				_ = frame.buffer.close()
				diagnosticsErr = s.normalizeConditionError(OperationWaitColor, diagnosticsErr)
				return result, s.finishConditionFailure(ctx, OperationWaitColor, result.ConditionID, "", result.Status, attempt, diagnosticsErr)
			}
			observation := observationFromFrame(frame, diagnostics)
			s.storeObservation(observation)
			result.Status = ConditionMatched
			result.Match = match
			result.ObservationID = observation.ObservationID
			if auditErr := s.emitConditionAudit(ctx, AuditConditionFinished, OperationWaitColor, result.ConditionID, observation.ObservationID, result.Status, attempt, ""); auditErr != nil {
				return result, conditionError(ErrorAuditDelivery, OperationWaitColor, "color wait completed but audit delivery failed", auditErr)
			}
			return result, nil
		}
		_ = frame.buffer.close()
		if attempt == s.policy.WaitAttempts {
			err = conditionError(ErrorConditionNotMet, OperationWaitColor, "color wait exhausted its bounded attempts", ErrConditionNotMet)
			return result, s.finishConditionFailure(ctx, OperationWaitColor, result.ConditionID, "", result.Status, attempt, err)
		}
		if err := waitForCondition(waitCtx, time.Duration(s.policy.WaitIntervalMillis)*time.Millisecond); err != nil {
			err = s.normalizeConditionError(OperationWaitColor, err)
			return result, s.finishConditionFailure(ctx, OperationWaitColor, result.ConditionID, "", result.Status, attempt, err)
		}
	}
	err := conditionError(ErrorConditionNotMet, OperationWaitColor, "color wait exhausted its bounded attempts", ErrConditionNotMet)
	return result, s.finishConditionFailure(ctx, OperationWaitColor, result.ConditionID, "", result.Status, result.Attempts, err)
}

func validateFindColorRequest(request FindColorRequest) error {
	if !validObservationID(request.ObservationID) {
		return errors.New("color search requires a valid RobotGo observation ID")
	}
	return validateColorCondition(request.Condition)
}

func validateWaitColorRequest(request WaitColorRequest) error {
	if err := validateCaptureRegion(request.Region, maxAgentCapturePixels); err != nil {
		return err
	}
	return validateColorCondition(request.Condition)
}

func validateColorCondition(condition ColorCondition) error {
	if math.IsNaN(condition.Tolerance) || math.IsInf(condition.Tolerance, 0) ||
		condition.Tolerance < 0 || condition.Tolerance > 1 {
		return errors.New("color tolerance must be a finite value from 0 through 1")
	}
	return nil
}

func (s *Session) authorizeCondition(operation Operation, confirmed bool) error {
	if _, allowed := s.policy.allowOperation[operation]; !allowed {
		return conditionError(ErrorPolicyDenied, operation, "agent policy denied the visual condition", ErrPolicyDenied)
	}
	if _, required := s.policy.requireConfirmation[operation]; required && !confirmed {
		return conditionError(ErrorPolicyDenied, operation, "agent policy requires visual-condition confirmation", ErrPolicyDenied)
	}
	return nil
}

func findColorInCapture(ctx context.Context, capture *captureBuffer, region CaptureRegion, condition ColorCondition) (*VisualMatch, error) {
	if !capture.acquireUse() {
		return nil, conditionError(ErrorStaleTarget, OperationFindColor, "visual-condition observation is unavailable or closed", ErrStaleTarget)
	}
	defer capture.releaseUse()
	pixels := capture.pixels
	for y := pixels.Bounds().Min.Y; y < pixels.Bounds().Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := pixels.Bounds().Min.X; x < pixels.Bounds().Max.X; x++ {
			offset := pixels.PixOffset(x, y)
			if colorMatches(pixels.Pix[offset], pixels.Pix[offset+1], pixels.Pix[offset+2], condition) {
				return &VisualMatch{
					X:         region.X + x - pixels.Bounds().Min.X,
					Y:         region.Y + y - pixels.Bounds().Min.Y,
					DisplayID: region.DisplayID,
				}, nil
			}
		}
	}
	return nil, nil
}

func colorMatches(red, green, blue uint8, condition ColorCondition) bool {
	if condition.Tolerance == 0 {
		return red == condition.Red && green == condition.Green && blue == condition.Blue
	}
	dr := float64(int(red) - int(condition.Red))
	dg := float64(int(green) - int(condition.Green))
	db := float64(int(blue) - int(condition.Blue))
	maximum := condition.Tolerance * maximumRGBDistance
	return dr*dr+dg*dg+db*db <= maximum*maximum
}

func waitForCondition(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func newConditionID() string {
	return conditionIDPrefix + strconv.FormatUint(conditionSerial.Add(1), 10)
}

func (s *Session) normalizeConditionError(operation Operation, err error) error {
	select {
	case <-s.ctx.Done():
		return conditionError(ErrorSessionClosed, operation, "agent session closed during visual condition", ErrSessionClosed)
	default:
	}
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		if actionErr.Operation == operation {
			return err
		}
		return conditionError(actionErr.Code, operation, actionErr.Message, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return conditionError(ErrorTimedOut, operation, "visual condition deadline exceeded", err)
	}
	if errors.Is(err, context.Canceled) {
		return conditionError(ErrorCanceled, operation, "visual condition canceled", err)
	}
	code, message := classifyBackendError(err)
	return conditionError(code, operation, message, err)
}

func conditionOperationError(operation Operation, err error) error {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return conditionError(actionErr.Code, operation, actionErr.Message, err)
	}
	return err
}

func conditionError(code ErrorCode, operation Operation, message string, cause error) *ActionError {
	return newActionError(code, operation, message, cause)
}

func (s *Session) finishConditionFailure(
	ctx context.Context,
	operation Operation,
	conditionID string,
	observationID string,
	status ConditionStatus,
	attempts uint32,
	err error,
) error {
	code := classifyObservationError(err)
	if auditErr := s.emitConditionAudit(ctx, AuditConditionFinished, operation, conditionID, observationID, status, attempts, code); auditErr != nil {
		return errors.Join(err, conditionError(ErrorAuditDelivery, operation, "visual condition failed and audit delivery failed", auditErr))
	}
	return err
}

func (s *Session) emitConditionAudit(
	ctx context.Context,
	kind AuditKind,
	operation Operation,
	conditionID string,
	observationID string,
	status ConditionStatus,
	attempts uint32,
	code ErrorCode,
) error {
	return s.emitAudit(ctx, AuditEvent{
		Kind: kind, Operation: operation, ConditionID: conditionID,
		ObservationID: observationID, ConditionStatus: status,
		ConditionAttempts: attempts, ErrorCode: code,
	})
}
