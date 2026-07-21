package agent

import "fmt"

const (
	maxAgentCapturePixels          = 16 * 1024 * 1024
	maxAgentQueries                = 1_000_000
	maxAgentVerificationAttempts   = 100
	maxAgentVerificationIntervalMS = 60_000
	maxAgentVerificationTimeoutMS  = 300_000
	maxAgentWaitAttempts           = 100
	maxAgentWaitIntervalMS         = 60_000
	maxAgentWaitTimeoutMS          = 300_000
)

// Policy constrains every observation, query, and mutation performed by a Session.
// Empty allow lists deny access; callers must opt in explicitly.
type Policy struct {
	AllowedOperations          []Operation `json:"allowed_operations"`
	ConfirmOperations          []Operation `json:"confirm_operations,omitempty"`
	AllowedDisplayIDs          []int       `json:"allowed_display_ids,omitempty"`
	MaxActions                 uint64      `json:"max_actions"`
	MaxTextRunes               int         `json:"max_text_runes"`
	AllowDoubleClick           bool        `json:"allow_double_click,omitempty"`
	MaxObservations            uint64      `json:"max_observations,omitempty"`
	MaxCapturePixels           uint64      `json:"max_capture_pixels,omitempty"`
	MaxQueries                 uint64      `json:"max_queries,omitempty"`
	WaitAttempts               uint32      `json:"wait_attempts,omitempty"`
	WaitIntervalMillis         int         `json:"wait_interval_ms,omitempty"`
	WaitTimeoutMillis          int         `json:"wait_timeout_ms,omitempty"`
	VerificationAttempts       uint32      `json:"verification_attempts,omitempty"`
	VerificationIntervalMillis int         `json:"verification_interval_ms,omitempty"`
	VerificationTimeoutMillis  int         `json:"verification_timeout_ms,omitempty"`
	allowOperation             map[Operation]struct{}
	requireConfirmation        map[Operation]struct{}
	allowDisplay               map[int]struct{}
}

func preparePolicy(input Policy) (Policy, error) {
	if input.MaxTextRunes < 0 {
		return Policy{}, fmt.Errorf("agent: max text runes must be non-negative")
	}
	if input.VerificationIntervalMillis < 0 {
		return Policy{}, fmt.Errorf("agent: verification interval must be non-negative")
	}
	if input.MaxQueries > maxAgentQueries {
		return Policy{}, fmt.Errorf("agent: max queries exceeds hard limit %d", maxAgentQueries)
	}
	if input.WaitAttempts > maxAgentWaitAttempts {
		return Policy{}, fmt.Errorf("agent: wait attempts exceeds hard limit %d", maxAgentWaitAttempts)
	}
	if input.WaitIntervalMillis < 0 || input.WaitIntervalMillis > maxAgentWaitIntervalMS {
		return Policy{}, fmt.Errorf("agent: wait interval must be between 0 and %dms", maxAgentWaitIntervalMS)
	}
	if input.WaitTimeoutMillis < 0 || input.WaitTimeoutMillis > maxAgentWaitTimeoutMS {
		return Policy{}, fmt.Errorf("agent: wait timeout must be between 0 and %dms", maxAgentWaitTimeoutMS)
	}
	if (input.WaitAttempts == 0) != (input.WaitTimeoutMillis == 0) {
		return Policy{}, fmt.Errorf("agent: wait attempts and timeout must both be zero or both be positive")
	}
	if input.MaxCapturePixels > maxAgentCapturePixels {
		return Policy{}, fmt.Errorf("agent: max capture pixels exceeds hard limit %d", maxAgentCapturePixels)
	}
	if input.VerificationAttempts > maxAgentVerificationAttempts {
		return Policy{}, fmt.Errorf("agent: verification attempts exceeds hard limit %d", maxAgentVerificationAttempts)
	}
	if input.VerificationIntervalMillis > maxAgentVerificationIntervalMS {
		return Policy{}, fmt.Errorf("agent: verification interval exceeds hard limit %dms", maxAgentVerificationIntervalMS)
	}
	if input.VerificationTimeoutMillis < 0 || input.VerificationTimeoutMillis > maxAgentVerificationTimeoutMS {
		return Policy{}, fmt.Errorf("agent: verification timeout must be between 0 and %dms", maxAgentVerificationTimeoutMS)
	}
	if (input.VerificationAttempts == 0) != (input.VerificationTimeoutMillis == 0) {
		return Policy{}, fmt.Errorf("agent: verification attempts and timeout must both be zero or both be positive")
	}
	prepared := Policy{
		AllowedOperations: append([]Operation(nil), input.AllowedOperations...),
		ConfirmOperations: append([]Operation(nil), input.ConfirmOperations...),
		AllowedDisplayIDs: append([]int(nil), input.AllowedDisplayIDs...),
		MaxActions:        input.MaxActions, MaxTextRunes: input.MaxTextRunes,
		AllowDoubleClick:           input.AllowDoubleClick,
		MaxObservations:            input.MaxObservations,
		MaxCapturePixels:           input.MaxCapturePixels,
		MaxQueries:                 input.MaxQueries,
		WaitAttempts:               input.WaitAttempts,
		WaitIntervalMillis:         input.WaitIntervalMillis,
		WaitTimeoutMillis:          input.WaitTimeoutMillis,
		VerificationAttempts:       input.VerificationAttempts,
		VerificationIntervalMillis: input.VerificationIntervalMillis,
		VerificationTimeoutMillis:  input.VerificationTimeoutMillis,
		allowOperation:             make(map[Operation]struct{}),
		requireConfirmation:        make(map[Operation]struct{}),
		allowDisplay:               make(map[int]struct{}),
	}
	for _, operation := range prepared.AllowedOperations {
		if !knownOperation(operation) {
			return Policy{}, fmt.Errorf("agent: unknown allowed operation %q", operation)
		}
		prepared.allowOperation[operation] = struct{}{}
	}
	if prepared.MaxActions == 0 && allowsMutation(prepared.allowOperation) {
		return Policy{}, fmt.Errorf("agent: max actions must be positive when a mutation is allowed")
	}
	if _, allowed := prepared.allowOperation[OperationObserve]; allowed && prepared.MaxObservations == 0 {
		return Policy{}, fmt.Errorf("agent: max observations must be positive when desktop.observe is allowed")
	}
	for _, operation := range prepared.ConfirmOperations {
		if _, allowed := prepared.allowOperation[operation]; !allowed {
			return Policy{}, fmt.Errorf("agent: confirmation operation %q is not allowed", operation)
		}
		prepared.requireConfirmation[operation] = struct{}{}
	}
	for _, displayID := range prepared.AllowedDisplayIDs {
		if displayID < 0 {
			return Policy{}, fmt.Errorf("agent: allowed display IDs must be non-negative")
		}
		prepared.allowDisplay[displayID] = struct{}{}
	}
	_, findAllowed := prepared.allowOperation[OperationFindColor]
	_, waitAllowed := prepared.allowOperation[OperationWaitColor]
	if (findAllowed || waitAllowed) && prepared.MaxQueries == 0 {
		return Policy{}, fmt.Errorf("agent: max queries must be positive when a visual query is allowed")
	}
	if findAllowed {
		if _, observeAllowed := prepared.allowOperation[OperationObserve]; !observeAllowed {
			return Policy{}, fmt.Errorf("agent: desktop.find-color requires desktop.observe")
		}
		if prepared.MaxCapturePixels == 0 || len(prepared.allowDisplay) == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.find-color requires allowed bounded captures")
		}
	}
	if waitAllowed {
		if prepared.WaitAttempts == 0 || prepared.WaitTimeoutMillis == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.wait-color requires bounded wait attempts and timeout")
		}
		if prepared.MaxCapturePixels == 0 || len(prepared.allowDisplay) == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.wait-color requires allowed bounded captures")
		}
		if prepared.MaxObservations < uint64(prepared.WaitAttempts) {
			return Policy{}, fmt.Errorf("agent: desktop.wait-color requires at least %d observations", prepared.WaitAttempts)
		}
	}
	if prepared.VerificationAttempts > 0 {
		if _, allowed := prepared.allowOperation[OperationObserve]; !allowed ||
			prepared.MaxCapturePixels == 0 || len(prepared.allowDisplay) == 0 {
			return Policy{}, fmt.Errorf("agent: verification requires allowed bounded capture observations")
		}
		minimumObservations := uint64(prepared.VerificationAttempts) + 2
		if prepared.MaxObservations < minimumObservations {
			return Policy{}, fmt.Errorf("agent: verification requires at least %d observations", minimumObservations)
		}
	}
	return prepared, nil
}

func allowsMutation(operations map[Operation]struct{}) bool {
	for _, operation := range []Operation{OperationMove, OperationClick, OperationTypeText} {
		if _, allowed := operations[operation]; allowed {
			return true
		}
	}
	return false
}

func knownOperation(operation Operation) bool {
	switch operation {
	case OperationMove, OperationClick, OperationTypeText, OperationObserve, OperationFindColor, OperationWaitColor:
		return true
	default:
		return false
	}
}
