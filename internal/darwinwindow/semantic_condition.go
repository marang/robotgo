package darwinwindow

import (
	"context"
	"slices"
)

type AccessibilityElementConditionKind string

const (
	AccessibilityElementConditionStatePresent           AccessibilityElementConditionKind = "state-present"
	AccessibilityElementConditionStateAbsent            AccessibilityElementConditionKind = "state-absent"
	AccessibilityElementConditionFocused                AccessibilityElementConditionKind = "focused"
	AccessibilityElementConditionNotFocused             AccessibilityElementConditionKind = "not-focused"
	AccessibilityElementConditionValueEqualsActionValue AccessibilityElementConditionKind = "value-equals-action-value"
)

const (
	accessibilityStateEnabled   = "enabled"
	accessibilityStateDisabled  = "disabled"
	accessibilityStateChecked   = "checked"
	accessibilityStateSelected  = "selected"
	accessibilityStateExpanded  = "expanded"
	accessibilityStateCollapsed = "collapsed"
	accessibilityStateReadOnly  = "read-only"
	accessibilityStateRequired  = "required"
	accessibilityStateInvalid   = "invalid"
)

type AccessibilityElementCondition struct {
	Kind  AccessibilityElementConditionKind
	State string
}

func accessibilityElementConditionObservable(
	condition *AccessibilityElementCondition,
	observableStates []string,
	focusObservable, valueObservable bool,
) bool {
	if condition == nil {
		return false
	}
	switch condition.Kind {
	case AccessibilityElementConditionStatePresent, AccessibilityElementConditionStateAbsent:
		return slices.Contains(observableStates, condition.State)
	case AccessibilityElementConditionFocused, AccessibilityElementConditionNotFocused:
		return focusObservable
	case AccessibilityElementConditionValueEqualsActionValue:
		return valueObservable
	default:
		return false
	}
}

func accessibilityRoleValueState(role string) string {
	switch role {
	case "checkbox", "switch":
		return accessibilityStateChecked
	case "radio":
		return accessibilityStateSelected
	default:
		return ""
	}
}

func accessibilityControlValueState(role string, value int32) (bool, bool) {
	switch value {
	case 0:
		return false, true
	case 1:
		return true, true
	case -1, 2:
		// AppKit and AX providers expose the regular NSButton mixed state with
		// either sentinel. Action v1 has no mixed state, so keep it observable
		// without reporting checked/selected, matching the UIA normalization.
		return false, role == "checkbox" || role == "radio"
	default:
		return false, false
	}
}

func appendAccessibilityValueState(
	states []string,
	role string,
	active bool,
	present bool,
) ([]string, error) {
	state := accessibilityRoleValueState(role)
	if state == "" {
		return states, nil
	}
	if !present {
		return nil, ErrAccessibilityInvalidTree
	}
	if active {
		states = append(states, state)
	}
	return states, nil
}

func accessibilityConditionIdentityMatches(
	condition *AccessibilityElementCondition,
	action string,
	expectedStates, liveStates, expectedActions, liveActions []string,
) bool {
	if condition == nil {
		return false
	}
	switch condition.Kind {
	case AccessibilityElementConditionStatePresent, AccessibilityElementConditionStateAbsent:
		return accessibilityConditionStatesMatch(expectedStates, liveStates, *condition) &&
			accessibilityConditionActionsMatch(expectedActions, liveActions, action, *condition)
	case AccessibilityElementConditionFocused, AccessibilityElementConditionNotFocused,
		AccessibilityElementConditionValueEqualsActionValue:
		return slices.Equal(expectedStates, liveStates) && slices.Equal(expectedActions, liveActions)
	default:
		return false
	}
}

func accessibilityConditionActionsMatch(
	expected, live []string,
	action string,
	condition AccessibilityElementCondition,
) bool {
	if !uniqueAccessibilityConditionStrings(expected) || !uniqueAccessibilityConditionStrings(live) {
		return false
	}
	if slices.Equal(expected, live) {
		return true
	}
	if condition.State != accessibilityStateExpanded && condition.State != accessibilityStateCollapsed {
		return false
	}
	inverse := ""
	switch action {
	case "expand":
		inverse = "collapse"
	case "collapse":
		inverse = "expand"
	default:
		return false
	}
	index := slices.Index(expected, action)
	if index < 0 || slices.Contains(expected, inverse) {
		return false
	}
	desired := append([]string(nil), expected...)
	desired[index] = inverse
	return slices.Equal(desired, live)
}

func accessibilityConditionStatesMatch(
	expected, live []string,
	condition AccessibilityElementCondition,
) bool {
	if !uniqueAccessibilityConditionStrings(expected) || !uniqueAccessibilityConditionStrings(live) {
		return false
	}
	if equalAccessibilityConditionStateSet(expected, live) {
		return true
	}
	desired := make(map[string]bool, len(expected)+1)
	for _, state := range expected {
		desired[state] = true
	}
	present := condition.Kind == AccessibilityElementConditionStatePresent
	if present {
		desired[condition.State] = true
	} else {
		delete(desired, condition.State)
	}
	if inverse := inverseAccessibilityState(condition.State); inverse != "" &&
		(slices.Contains(expected, condition.State) || slices.Contains(expected, inverse)) {
		if present {
			delete(desired, inverse)
		} else {
			desired[inverse] = true
		}
	}
	if len(desired) != len(live) {
		return false
	}
	for _, state := range live {
		if !desired[state] {
			return false
		}
	}
	return true
}

func uniqueAccessibilityConditionStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func equalAccessibilityConditionStateSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	values := make(map[string]struct{}, len(left))
	for _, value := range left {
		values[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := values[value]; !ok {
			return false
		}
	}
	return true
}

func inverseAccessibilityState(state string) string {
	switch state {
	case accessibilityStateEnabled:
		return accessibilityStateDisabled
	case accessibilityStateDisabled:
		return accessibilityStateEnabled
	case accessibilityStateExpanded:
		return accessibilityStateCollapsed
	case accessibilityStateCollapsed:
		return accessibilityStateExpanded
	default:
		return ""
	}
}

func finalAccessibilityActionGate(
	condition *AccessibilityElementCondition,
	checkCondition func() (bool, error),
	validateExact func() error,
) (bool, error) {
	if condition != nil {
		satisfied, err := checkCondition()
		if err != nil {
			return false, err
		}
		if satisfied {
			return true, nil
		}
	}
	if validateExact == nil {
		return false, ErrAccessibilityInvalidTree
	}
	if err := validateExact(); err != nil {
		return false, err
	}
	return false, nil
}

func dispatchAccessibilityMutation(
	ctx context.Context,
	mutation func() error,
) (AccessibilityActionResult, error) {
	if err := ctx.Err(); err != nil {
		return AccessibilityActionResult{}, err
	}
	if mutation == nil {
		return AccessibilityActionResult{}, ErrAccessibilityInvalidTree
	}
	err := mutation()
	return AccessibilityActionResult{Dispatched: true}, err
}
