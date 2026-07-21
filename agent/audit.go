package agent

import (
	"context"
	"time"
)

// AuditSchemaVersion identifies the payload-free audit event contract.
const AuditSchemaVersion = "1"

// AuditKind identifies a sanitized session lifecycle event.
type AuditKind string

const (
	AuditObservationStarted   AuditKind = "observation.started"
	AuditObservationFinished  AuditKind = "observation.finished"
	AuditActionStarted        AuditKind = "action.started"
	AuditActionFinished       AuditKind = "action.finished"
	AuditVerificationFinished AuditKind = "verification.finished"
)

// AuditEvent deliberately contains no request payload, coordinates, text,
// pixels, capture digest, backend error detail, or restore token.
type AuditEvent struct {
	SchemaVersion             string             `json:"schema_version"`
	Sequence                  uint64             `json:"sequence"`
	Kind                      AuditKind          `json:"kind"`
	Timestamp                 time.Time          `json:"timestamp"`
	Operation                 Operation          `json:"operation,omitempty"`
	ActionID                  string             `json:"action_id,omitempty"`
	ObservationID             string             `json:"observation_id,omitempty"`
	PreconditionObservationID string             `json:"precondition_observation_id,omitempty"`
	PostObservationID         string             `json:"post_observation_id,omitempty"`
	ActionStatus              ActionStatus       `json:"action_status,omitempty"`
	VerificationStatus        VerificationStatus `json:"verification_status,omitempty"`
	VerificationAttempts      uint32             `json:"verification_attempts,omitempty"`
	ErrorCode                 ErrorCode          `json:"error_code,omitempty"`
}

// AuditSink receives synchronous, payload-free events. Implementations should
// honor ctx and return promptly; an intent-event error prevents desktop I/O.
// A completion-event error is returned alongside the completed result so a
// caller never needs to guess whether a desktop mutation occurred. A sink must
// not call back into the Session that invoked it.
type AuditSink interface {
	Record(ctx context.Context, event AuditEvent) error
}

func (s *Session) emitAudit(ctx context.Context, event AuditEvent) error {
	if s.auditSink == nil {
		return nil
	}
	s.auditSequence++
	event.SchemaVersion = AuditSchemaVersion
	event.Sequence = s.auditSequence
	event.Timestamp = time.Now().UTC()
	return s.auditSink.Record(ctx, event)
}
