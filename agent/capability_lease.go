package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// CapabilityLeaseSchemaVersion identifies the opaque single-use authority contract.
const CapabilityLeaseSchemaVersion = "1"

const (
	// TargetPatchProposalSchemaVersion identifies the non-executable review contract.
	TargetPatchProposalSchemaVersion = "1"
	adaptiveBaseScore                = 60
	adaptiveNameScore                = 25
	adaptiveAncestorScore            = 15
)

// TargetResolutionMode fixes whether resolution is exact, adaptively semantic,
// or a non-executable review proposal.
type TargetResolutionMode string

const (
	TargetResolutionModeStrict   TargetResolutionMode = "strict"
	TargetResolutionModeAdaptive TargetResolutionMode = "adaptive"
	TargetResolutionModeReview   TargetResolutionMode = "review"
)

var allTargetResolutionModes = []TargetResolutionMode{
	TargetResolutionModeStrict, TargetResolutionModeAdaptive, TargetResolutionModeReview,
}

// CapabilityLeaseStatus is the payload-free lifecycle vocabulary exposed in
// Action Proof and audit evidence.
type CapabilityLeaseStatus string

const (
	CapabilityLeaseNotRequired CapabilityLeaseStatus = "not-required"
	CapabilityLeaseAbsent      CapabilityLeaseStatus = "absent"
	CapabilityLeaseIssued      CapabilityLeaseStatus = "issued"
	CapabilityLeaseReserved    CapabilityLeaseStatus = "reserved"
	CapabilityLeaseConsumed    CapabilityLeaseStatus = "consumed"
	CapabilityLeaseInvalidated CapabilityLeaseStatus = "invalidated"
	CapabilityLeaseExpired     CapabilityLeaseStatus = "expired"
)

// CapabilityLeaseRequest defines the exact future mutation authorized by a
// successful resolution. ActionValueSHA256 binds set-value without retaining
// or returning the value itself.
type CapabilityLeaseRequest struct {
	SchemaVersion     string              `json:"schema_version"`
	Action            UIAction            `json:"action"`
	Postcondition     *UIElementCondition `json:"postcondition,omitempty"`
	ActionValueSHA256 string              `json:"action_value_sha256,omitempty"`
	DurationMillis    int                 `json:"duration_ms"`
}

// CapabilityLease is an opaque, single-session, single-use bearer authority.
// It contains no desktop identity, native reference, policy, or action value.
type CapabilityLease struct {
	SchemaVersion string               `json:"schema_version"`
	ID            string               `json:"id"`
	Mode          TargetResolutionMode `json:"mode"`
	Action        UIAction             `json:"action"`
	Postcondition *UIElementCondition  `json:"postcondition,omitempty"`
	ExpiresAt     time.Time            `json:"expires_at"`
}

type capabilityLeaseRecord struct {
	status                CapabilityLeaseStatus
	mode                  TargetResolutionMode
	observationID         string
	evidenceObservationID string
	elementID             string
	expected              UIElementExpectation
	targetDigest          [sha256.Size]byte
	policyDigest          [sha256.Size]byte
	action                UIAction
	postcondition         *UIElementCondition
	valueDigest           [sha256.Size]byte
	hasValueDigest        bool
	expiresAt             time.Time
}

type capabilityLeaseReservation struct {
	key    [sha256.Size]byte
	record capabilityLeaseRecord
	active bool
}

// CapabilityLeaseActionValueDigest returns the lowercase SHA-256 binding used
// for a set-value lease. The raw value remains caller-owned.
func CapabilityLeaseActionValueDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validTargetResolutionMode(mode TargetResolutionMode) bool {
	for _, allowed := range allTargetResolutionModes {
		if mode == allowed {
			return true
		}
	}
	return false
}

func normalizeTargetResolutionMode(mode TargetResolutionMode) TargetResolutionMode {
	if mode == "" {
		return TargetResolutionModeStrict
	}
	return mode
}

func policyCapabilityDigest(policy Policy) [sha256.Size]byte {
	payload, _ := json.Marshal(policy)
	return sha256.Sum256(payload)
}

func targetSpecDigest(spec TargetSpec) [sha256.Size]byte {
	payload, _ := json.Marshal(spec)
	return sha256.Sum256(payload)
}

func parseLeaseValueDigest(value string) ([sha256.Size]byte, bool) {
	var result [sha256.Size]byte
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return result, false
	}
	copy(result[:], decoded)
	return result, true
}

func cloneCapabilityLeaseRecord(record capabilityLeaseRecord) capabilityLeaseRecord {
	record.expected = cloneUIElementExpectation(record.expected)
	record.postcondition = cloneUIElementCondition(record.postcondition)
	return record
}

func (s *Session) issueCapabilityLease(request ResolveUIRequest, selected *retainedUITarget, evidenceExpiresAt time.Time) (*CapabilityLease, error) {
	if request.Lease == nil || selected == nil {
		return nil, ErrLeaseInvalid
	}
	var tokenBytes [32]byte
	defer clear(tokenBytes[:])
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	key := sha256.Sum256([]byte(token))
	now := s.now()
	record := capabilityLeaseRecord{
		status: CapabilityLeaseIssued, mode: normalizeTargetResolutionMode(request.Mode),
		observationID: request.ObservationID, elementID: selected.elementID,
		expected: cloneUIElementExpectation(selected.expected), targetDigest: targetSpecDigest(request.Target),
		policyDigest: s.policyDigest, action: request.Lease.Action,
		postcondition: cloneUIElementCondition(request.Lease.Postcondition),
		expiresAt:     now.Add(time.Duration(request.Lease.DurationMillis) * time.Millisecond),
	}
	if !evidenceExpiresAt.IsZero() && len(request.Target.Evidence) > 0 {
		record.evidenceObservationID = request.Target.Evidence[0].ObservationID
		if evidenceExpiresAt.Before(record.expiresAt) {
			record.expiresAt = evidenceExpiresAt
		}
		if !now.Before(record.expiresAt) {
			return nil, ErrStaleTarget
		}
	}
	if request.Lease.Action == UIActionSetValue {
		record.valueDigest, record.hasValueDigest = parseLeaseValueDigest(request.Lease.ActionValueSHA256)
	}
	// Lease publication and observation release share this lock order so a
	// release cannot linearize between the liveness check and insertion.
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	observation, ok := s.observations[request.ObservationID]
	expected, expectedOK := observation.uiExpected[selected.elementID]
	reference, referenceOK := observation.uiElements[selected.elementID]
	if !ok || observation.uiTarget == nil || !observation.uiActionable ||
		!expectedOK || !referenceOK || len(reference) == 0 ||
		!equalUIExpectation(expected, selected.expected) {
		return nil, ErrStaleTarget
	}
	if record.evidenceObservationID != "" {
		evidenceObservation, evidenceOK := s.observations[record.evidenceObservationID]
		if !evidenceOK || evidenceObservation.source != OperationView || evidenceObservation.capture == nil ||
			!evidenceObservation.capture.usable() {
			return nil, ErrStaleTarget
		}
		for _, clause := range request.Target.Evidence {
			evidence, exists := evidenceObservation.targetEvidence[clause.EvidenceID]
			if !exists || evidence.source != clause.Source || int(clause.ItemIndex) >= len(evidence.items) {
				return nil, ErrStaleTarget
			}
		}
	}
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if s.closedLeases || s.usedLeases >= s.policy.MaxCapabilityLeases {
		return nil, ErrPolicyDenied
	}
	s.leases[key] = record
	s.usedLeases++
	return &CapabilityLease{SchemaVersion: CapabilityLeaseSchemaVersion, ID: token,
		Mode: record.mode, Action: record.action, Postcondition: cloneUIElementCondition(record.postcondition), ExpiresAt: record.expiresAt.UTC()}, nil
}

func (s *Session) reserveCapabilityLease(request ElementActionRequest) (*capabilityLeaseReservation, *ActionError) {
	if request.CapabilityLeaseID == "" {
		if s.policy.RequireCapabilityLease {
			return nil, leaseActionError(ErrorLeaseRequired, ErrLeaseRequired)
		}
		return nil, nil
	}
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(request.CapabilityLeaseID)
	defer clear(decoded)
	if decodeErr != nil || len(decoded) != 32 {
		return nil, leaseActionError(ErrorLeaseInvalid, ErrLeaseInvalid)
	}
	key := sha256.Sum256([]byte(request.CapabilityLeaseID))
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	record, ok := s.leases[key]
	if !ok {
		return nil, leaseActionError(ErrorLeaseInvalid, ErrLeaseInvalid)
	}
	switch record.status {
	case CapabilityLeaseIssued:
	case CapabilityLeaseExpired:
		return nil, leaseActionError(ErrorLeaseExpired, ErrLeaseExpired)
	case CapabilityLeaseInvalidated:
		return nil, leaseActionError(ErrorLeaseInvalid, ErrLeaseInvalid)
	default:
		return nil, leaseActionError(ErrorLeaseConsumed, ErrLeaseConsumed)
	}
	if !s.now().Before(record.expiresAt) {
		record.status = CapabilityLeaseExpired
		s.leases[key] = record
		return nil, leaseActionError(ErrorLeaseExpired, ErrLeaseExpired)
	}
	valueDigest := sha256.Sum256([]byte(request.Value))
	postconditionMatches := equalUIElementCondition(request.Postcondition, record.postcondition)
	valueMatches := !record.hasValueDigest || subtle.ConstantTimeCompare(valueDigest[:], record.valueDigest[:]) == 1
	if request.Action != record.action || !postconditionMatches || !valueMatches || record.policyDigest != s.policyDigest {
		record.status = CapabilityLeaseInvalidated
		s.leases[key] = record
		return nil, leaseActionError(ErrorLeaseMismatch, ErrLeaseMismatch)
	}
	record.status = CapabilityLeaseReserved
	s.leases[key] = record
	return &capabilityLeaseReservation{key: key, record: cloneCapabilityLeaseRecord(record), active: true}, nil
}

func (s *Session) consumeCapabilityLease(reservation *capabilityLeaseReservation) error {
	if reservation == nil {
		return nil
	}
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	record, ok := s.leases[reservation.key]
	if !ok {
		return ErrLeaseInvalid
	}
	switch record.status {
	case CapabilityLeaseReserved:
	case CapabilityLeaseInvalidated:
		return ErrLeaseInvalid
	case CapabilityLeaseExpired:
		return ErrLeaseExpired
	default:
		return ErrLeaseConsumed
	}
	if !s.now().Before(record.expiresAt) {
		record.status = CapabilityLeaseExpired
		s.leases[reservation.key] = record
		reservation.active = false
		return ErrLeaseExpired
	}
	record.status = CapabilityLeaseConsumed
	s.leases[reservation.key] = record
	reservation.active = false
	return nil
}

func (s *Session) invalidateCapabilityLease(reservation *capabilityLeaseReservation) {
	if reservation == nil || !reservation.active {
		return
	}
	s.leaseMu.Lock()
	if record, ok := s.leases[reservation.key]; ok && record.status == CapabilityLeaseReserved {
		record.status = CapabilityLeaseInvalidated
		s.leases[reservation.key] = record
	}
	s.leaseMu.Unlock()
	reservation.active = false
}

func (s *Session) invalidateObservationLeases(observationID string) {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	for key, record := range s.leases {
		if (record.observationID == observationID || record.evidenceObservationID == observationID) &&
			(record.status == CapabilityLeaseIssued || record.status == CapabilityLeaseReserved) {
			record.status = CapabilityLeaseInvalidated
			s.leases[key] = record
		}
	}
}

func (s *Session) invalidateIssuedCapabilityLease(id string) {
	if id == "" {
		return
	}
	key := sha256.Sum256([]byte(id))
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if record, ok := s.leases[key]; ok && record.status == CapabilityLeaseIssued {
		record.status = CapabilityLeaseInvalidated
		s.leases[key] = record
	}
}

func (s *Session) invalidatePresentedCapabilityLease(id string) {
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	defer clear(decoded)
	if err != nil || len(decoded) != 32 {
		return
	}
	s.invalidateIssuedCapabilityLease(id)
}

func (s *Session) closeCapabilityLeases() {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	s.closedLeases = true
	for key, record := range s.leases {
		clear(record.expected.States)
		clear(record.expected.Actions)
		delete(s.leases, key)
	}
}

func leaseActionError(code ErrorCode, cause error) *ActionError {
	message := "semantic capability lease was rejected"
	if errors.Is(cause, ErrLeaseRequired) {
		message = "semantic capability lease is required"
	}
	return newActionError(code, OperationElementAct, message, cause)
}

func equalUIElementCondition(left, right *UIElementCondition) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
