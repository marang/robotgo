package agent

import (
	"context"
	"errors"
	"time"
)

type actionLineage struct {
	preconditionID string
	record         observationRecord
}

func (s *Session) prepareActionLineage(ctx context.Context, request ActionRequest, dryRun bool) (*actionLineage, error) {
	if request.Precondition == nil {
		return nil, nil
	}
	if _, allowed := s.policy.allowOperation[OperationObserve]; !allowed {
		return nil, actionLineageError(ErrorPolicyDenied, "agent policy denies observation lineage", ErrPolicyDenied)
	}
	if _, required := s.policy.requireConfirmation[OperationObserve]; required && !request.Confirmed {
		return nil, actionLineageError(ErrorPolicyDenied, "agent policy requires observation confirmation", ErrPolicyDenied)
	}
	record, ok := s.observation(request.Precondition.ObservationID)
	if !ok || !record.hasCapture || !record.capture.usable() {
		return nil, actionLineageError(ErrorStaleTarget, "precondition observation is unavailable or closed", ErrStaleTarget)
	}
	requiredCaptures := uint64(1)
	if request.Verification != nil {
		if s.policy.VerificationAttempts == 0 || s.policy.VerificationTimeoutMillis == 0 {
			return nil, actionLineageError(ErrorPolicyDenied, "agent policy does not permit post-action verification", ErrPolicyDenied)
		}
		if !dryRun {
			requiredCaptures += uint64(s.policy.VerificationAttempts)
		}
	}
	if s.usedObservations > s.policy.MaxObservations || requiredCaptures > s.policy.MaxObservations-s.usedObservations {
		return nil, actionLineageError(ErrorPolicyDenied, "agent policy observation limit cannot satisfy lineage proof", ErrPolicyDenied)
	}

	current, err := s.capture(ctx, record.region, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = current.buffer.close() }()
	if current.metadata.SHA256 != record.digest {
		return nil, actionLineageError(ErrorStaleTarget, "desktop target changed since the precondition observation", ErrStaleTarget)
	}
	return &actionLineage{preconditionID: request.Precondition.ObservationID, record: record}, nil
}

func (s *Session) verifyAction(ctx context.Context, actionID string, request ActionRequest, lineage *actionLineage) (*Observation, VerificationResult, error) {
	result := VerificationResult{Condition: request.Verification.Condition}
	verifyCtx, cancel := context.WithTimeout(ctx, time.Duration(s.policy.VerificationTimeoutMillis)*time.Millisecond)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	defer func() {
		stopSessionCancel()
		cancel()
	}()

	for attempt := uint32(1); attempt <= s.policy.VerificationAttempts; attempt++ {
		frame, err := s.capture(verifyCtx, lineage.record.region, true)
		result.Attempts = attempt
		if err != nil {
			err = s.normalizeVerificationError(err)
			result.Status = VerificationFailed
			if auditErr := s.emitVerificationAudit(ctx, request.Operation, actionID, lineage.preconditionID, "", result, classifyObservationError(err)); auditErr != nil {
				return nil, result, errors.Join(err, actionLineageError(ErrorAuditDelivery, "verification failed and audit delivery failed", auditErr))
			}
			return nil, result, err
		}
		matched := verificationMatches(request.Verification.Condition, frame.metadata.SHA256 == lineage.record.digest)
		if matched || attempt == s.policy.VerificationAttempts {
			diagnostics, diagnosticsErr := s.runtimeDiagnostics(verifyCtx)
			if diagnosticsErr != nil {
				_ = frame.buffer.close()
				diagnosticsErr = s.normalizeVerificationError(diagnosticsErr)
				result.Status = VerificationFailed
				if auditErr := s.emitVerificationAudit(ctx, request.Operation, actionID, lineage.preconditionID, "", result, classifyObservationError(diagnosticsErr)); auditErr != nil {
					return nil, result, errors.Join(diagnosticsErr, actionLineageError(ErrorAuditDelivery, "verification failed and audit delivery failed", auditErr))
				}
				return nil, result, diagnosticsErr
			}
			observation := observationFromFrame(frame, diagnostics)
			s.storeObservation(observation)
			if matched {
				result.Status = VerificationPassed
				if err := s.emitVerificationAudit(ctx, request.Operation, actionID, lineage.preconditionID, observation.ObservationID, result, ""); err != nil {
					return observation, result, actionLineageError(ErrorAuditDelivery, "verification completed but audit delivery failed", err)
				}
				return observation, result, nil
			}
			result.Status = VerificationFailed
			if err := s.emitVerificationAudit(ctx, request.Operation, actionID, lineage.preconditionID, observation.ObservationID, result, ErrorVerification); err != nil {
				return observation, result, errors.Join(
					actionLineageError(ErrorVerification, "post-action verification condition was not satisfied", ErrVerification),
					actionLineageError(ErrorAuditDelivery, "verification failed and audit delivery failed", err),
				)
			}
			return observation, result, actionLineageError(ErrorVerification, "post-action verification condition was not satisfied", ErrVerification)
		}
		_ = frame.buffer.close()
		if err := waitForVerification(verifyCtx, time.Duration(s.policy.VerificationIntervalMillis)*time.Millisecond); err != nil {
			err = s.normalizeVerificationError(err)
			result.Status = VerificationFailed
			if auditErr := s.emitVerificationAudit(ctx, request.Operation, actionID, lineage.preconditionID, "", result, classifyObservationError(err)); auditErr != nil {
				return nil, result, errors.Join(err, actionLineageError(ErrorAuditDelivery, "verification failed and audit delivery failed", auditErr))
			}
			return nil, result, err
		}
	}
	return nil, result, actionLineageError(ErrorVerification, "post-action verification exhausted its bounded attempts", ErrVerification)
}

func (s *Session) normalizeVerificationError(err error) error {
	select {
	case <-s.ctx.Done():
		return actionLineageError(ErrorSessionClosed, "agent session closed during post-action verification", ErrSessionClosed)
	default:
		return err
	}
}

func verificationMatches(condition VerificationCondition, unchanged bool) bool {
	switch condition {
	case VerificationCaptureChanged:
		return !unchanged
	case VerificationCaptureUnchanged:
		return unchanged
	default:
		return false
	}
}

func waitForVerification(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		select {
		case <-ctx.Done():
			return observationContextError(ctx)
		default:
			return nil
		}
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return observationContextError(ctx)
	case <-timer.C:
		return nil
	}
}

func observationFromFrame(frame *capturedFrame, diagnostics RuntimeDiagnostics) *Observation {
	return &Observation{
		SchemaVersion: ObservationSchemaVersion,
		ObservationID: newObservationID(),
		CreatedAt:     time.Now().UTC(),
		Diagnostics:   diagnostics,
		Capture:       &frame.metadata,
		capture:       frame.buffer,
	}
}

func (s *Session) emitVerificationAudit(ctx context.Context, operation Operation, actionID, preconditionID, postID string, result VerificationResult, code ErrorCode) error {
	return s.emitAudit(ctx, AuditEvent{
		Kind: AuditVerificationFinished, Operation: operation, ActionID: actionID,
		PreconditionObservationID: preconditionID, PostObservationID: postID,
		VerificationStatus: result.Status, VerificationAttempts: result.Attempts,
		ErrorCode: code,
	})
}

func actionLineageError(code ErrorCode, message string, cause error) *ActionError {
	return newActionError(code, "", message, cause)
}

func classifyLineageError(err error) (ErrorCode, string) {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return actionErr.Code, actionErr.Message
	}
	code, _ := classifyBackendError(err)
	return code, "agent lineage proof failed"
}
