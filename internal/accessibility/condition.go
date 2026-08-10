package accessibility

import (
	"errors"
	"math"
	"slices"
	"strconv"
)

// ElementConditionKind identifies one privacy-preserving, element-relative
// postcondition. ValueEqualsActionValue compares against ActionRequest.Value;
// the condition itself never carries a value.
type ElementConditionKind string

const (
	ElementConditionStatePresent           ElementConditionKind = "state-present"
	ElementConditionStateAbsent            ElementConditionKind = "state-absent"
	ElementConditionFocused                ElementConditionKind = "focused"
	ElementConditionNotFocused             ElementConditionKind = "not-focused"
	ElementConditionValueEqualsActionValue ElementConditionKind = "value-equals-action-value"
)

const maxElementConditionValueBytes = 1 << 20

const (
	elementStateEnabled   = "enabled"
	elementStateDisabled  = "disabled"
	elementStateChecked   = "checked"
	elementStateSelected  = "selected"
	elementStateExpanded  = "expanded"
	elementStateCollapsed = "collapsed"
	elementStateReadOnly  = "read-only"
	elementStateRequired  = "required"
	elementStateInvalid   = "invalid"
)

// ElementCondition contains no native reference and no private action value.
type ElementCondition struct {
	Kind  ElementConditionKind
	State string
}

func validateElementCondition(request ActionRequest) error {
	condition := request.Postcondition
	if condition == nil {
		return ErrInvalidTree
	}
	switch condition.Kind {
	case ElementConditionStatePresent, ElementConditionStateAbsent:
		if !validElementConditionState(condition.State) {
			return ErrInvalidTree
		}
	case ElementConditionFocused, ElementConditionNotFocused:
		if condition.State != "" {
			return ErrInvalidTree
		}
	case ElementConditionValueEqualsActionValue:
		if condition.State != "" || request.Action != "set-value" ||
			len(request.Value) > maxElementConditionValueBytes {
			return ErrInvalidTree
		}
	default:
		return ErrInvalidTree
	}
	return nil
}

func validElementConditionState(state string) bool {
	switch state {
	case elementStateEnabled, elementStateDisabled, elementStateChecked,
		elementStateSelected, elementStateExpanded, elementStateCollapsed,
		elementStateReadOnly, elementStateRequired, elementStateInvalid:
		return true
	default:
		return false
	}
}

func elementConditionObservable(
	condition *ElementCondition,
	observableStates []string,
	focusObservable, valueObservable bool,
) bool {
	if condition == nil {
		return false
	}
	switch condition.Kind {
	case ElementConditionStatePresent, ElementConditionStateAbsent:
		return slices.Contains(observableStates, condition.State)
	case ElementConditionFocused, ElementConditionNotFocused:
		return focusObservable
	case ElementConditionValueEqualsActionValue:
		return valueObservable
	default:
		return false
	}
}

// conditionIdentityMatches permits only the state transition named by a state
// condition. Expansion may replace its offered action with the exact inverse;
// every other action-list change is stale. Exact identity is always restored
// by the final pre-dispatch validation when the condition is not yet satisfied.
func conditionIdentityMatches(
	condition *ElementCondition,
	action string,
	expectedStates, liveStates, expectedActions, liveActions []string,
) bool {
	if condition == nil {
		return false
	}
	switch condition.Kind {
	case ElementConditionStatePresent, ElementConditionStateAbsent:
		return conditionControlledStatesMatch(expectedStates, liveStates, *condition) &&
			conditionControlledActionsMatch(expectedActions, liveActions, action, *condition)
	case ElementConditionFocused, ElementConditionNotFocused,
		ElementConditionValueEqualsActionValue:
		return slices.Equal(expectedStates, liveStates) && slices.Equal(expectedActions, liveActions)
	default:
		return false
	}
}

func conditionControlledActionsMatch(
	expected, live []string,
	action string,
	condition ElementCondition,
) bool {
	if !hasUniqueStrings(expected) || !hasUniqueStrings(live) {
		return false
	}
	if slices.Equal(expected, live) {
		return true
	}
	if condition.State != elementStateExpanded && condition.State != elementStateCollapsed {
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

func conditionControlledStatesMatch(expected, live []string, condition ElementCondition) bool {
	if !hasUniqueStrings(expected) || !hasUniqueStrings(live) {
		return false
	}
	if sameStringSet(expected, live) {
		return true
	}
	desired := make(map[string]bool, len(expected)+1)
	for _, state := range expected {
		desired[state] = true
	}
	present := condition.Kind == ElementConditionStatePresent
	if present {
		desired[condition.State] = true
	} else {
		delete(desired, condition.State)
	}
	if inverse := inverseElementState(condition.State); inverse != "" {
		// Only force the inverse when this is an actual transition from one
		// side of the pair. An already-absent state with no observed inverse
		// remains unchanged.
		if slices.Contains(expected, condition.State) || slices.Contains(expected, inverse) {
			if present {
				delete(desired, inverse)
			} else {
				desired[inverse] = true
			}
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

func sameStringSet(left, right []string) bool {
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

func inverseElementState(state string) string {
	switch state {
	case elementStateEnabled:
		return elementStateDisabled
	case elementStateDisabled:
		return elementStateEnabled
	case elementStateExpanded:
		return elementStateCollapsed
	case elementStateCollapsed:
		return elementStateExpanded
	default:
		return ""
	}
}

func hasUniqueStrings(values []string) bool {
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

func elementConditionSatisfied(
	condition *ElementCondition,
	liveStates []string,
	focused bool,
	role, liveValue string,
	valueTruncated bool,
	actionValue []byte,
) (bool, error) {
	if condition == nil {
		return false, ErrInvalidTree
	}
	switch condition.Kind {
	case ElementConditionStatePresent:
		return slices.Contains(liveStates, condition.State), nil
	case ElementConditionStateAbsent:
		return !slices.Contains(liveStates, condition.State), nil
	case ElementConditionFocused:
		return focused, nil
	case ElementConditionNotFocused:
		return !focused, nil
	case ElementConditionValueEqualsActionValue:
		if valueTruncated {
			return false, nil
		}
		if role == "slider" {
			live, liveErr := strconv.ParseFloat(liveValue, 64)
			want, wantErr := strconv.ParseFloat(string(actionValue), 64)
			if liveErr != nil || wantErr != nil || math.IsNaN(live) || math.IsNaN(want) ||
				math.IsInf(live, 0) || math.IsInf(want, 0) {
				return false, ErrInvalidTree
			}
			return live == want, nil
		}
		return liveValue == string(actionValue), nil
	default:
		return false, ErrInvalidTree
	}
}

// finalActionGate is shared by the native adapters so the irreversible call
// has one auditable ordering: condition check, exact precondition validation,
// then dispatch by the caller. Errors are never interpreted as satisfaction.
func finalActionGate(
	condition *ElementCondition,
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
		return false, errors.New("accessibility: missing exact dispatch validator")
	}
	if err := validateExact(); err != nil {
		return false, err
	}
	return false, nil
}
