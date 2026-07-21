package agent

import "fmt"

// Policy constrains every mutation performed by a Session. Empty allow lists
// deny access; callers must opt in explicitly.
type Policy struct {
	AllowedOperations   []Operation `json:"allowed_operations"`
	ConfirmOperations   []Operation `json:"confirm_operations,omitempty"`
	AllowedDisplayIDs   []int       `json:"allowed_display_ids,omitempty"`
	MaxActions          uint64      `json:"max_actions"`
	MaxTextRunes        int         `json:"max_text_runes"`
	AllowDoubleClick    bool        `json:"allow_double_click,omitempty"`
	allowOperation      map[Operation]struct{}
	requireConfirmation map[Operation]struct{}
	allowDisplay        map[int]struct{}
}

func preparePolicy(input Policy) (Policy, error) {
	if input.MaxActions == 0 {
		return Policy{}, fmt.Errorf("agent: max actions must be positive")
	}
	if input.MaxTextRunes < 0 {
		return Policy{}, fmt.Errorf("agent: max text runes must be non-negative")
	}
	prepared := Policy{
		AllowedOperations: append([]Operation(nil), input.AllowedOperations...),
		ConfirmOperations: append([]Operation(nil), input.ConfirmOperations...),
		AllowedDisplayIDs: append([]int(nil), input.AllowedDisplayIDs...),
		MaxActions:        input.MaxActions, MaxTextRunes: input.MaxTextRunes,
		AllowDoubleClick:    input.AllowDoubleClick,
		allowOperation:      make(map[Operation]struct{}),
		requireConfirmation: make(map[Operation]struct{}),
		allowDisplay:        make(map[int]struct{}),
	}
	for _, operation := range prepared.AllowedOperations {
		if !knownOperation(operation) {
			return Policy{}, fmt.Errorf("agent: unknown allowed operation %q", operation)
		}
		prepared.allowOperation[operation] = struct{}{}
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
	return prepared, nil
}

func knownOperation(operation Operation) bool {
	switch operation {
	case OperationMove, OperationClick, OperationTypeText:
		return true
	default:
		return false
	}
}
