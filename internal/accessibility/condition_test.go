package accessibility

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestConditionControlledStatesPermitOnlyNamedTransition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		expected  []string
		live      []string
		condition ElementCondition
		want      bool
	}{
		{
			name: "checked becomes present", expected: []string{elementStateEnabled},
			live:      []string{elementStateEnabled, elementStateChecked},
			condition: ElementCondition{Kind: ElementConditionStatePresent, State: elementStateChecked}, want: true,
		},
		{
			name: "expanded replaces collapsed", expected: []string{elementStateEnabled, elementStateCollapsed},
			live:      []string{elementStateEnabled, elementStateExpanded},
			condition: ElementCondition{Kind: ElementConditionStatePresent, State: elementStateExpanded}, want: true,
		},
		{
			name: "disabled replaces enabled", expected: []string{elementStateEnabled},
			live:      []string{elementStateDisabled},
			condition: ElementCondition{Kind: ElementConditionStatePresent, State: elementStateDisabled}, want: true,
		},
		{
			name: "absent expanded becomes collapsed", expected: []string{elementStateEnabled, elementStateExpanded},
			live:      []string{elementStateEnabled, elementStateCollapsed},
			condition: ElementCondition{Kind: ElementConditionStateAbsent, State: elementStateExpanded}, want: true,
		},
		{
			name: "already absent remains unchanged", expected: []string{elementStateEnabled},
			live:      []string{elementStateEnabled},
			condition: ElementCondition{Kind: ElementConditionStateAbsent, State: elementStateExpanded}, want: true,
		},
		{
			name: "unrelated state is rejected", expected: []string{elementStateEnabled},
			live:      []string{elementStateEnabled, elementStateChecked, elementStateSelected},
			condition: ElementCondition{Kind: ElementConditionStatePresent, State: elementStateChecked}, want: false,
		},
		{
			name: "duplicate live state is rejected", expected: []string{elementStateEnabled},
			live:      []string{elementStateEnabled, elementStateChecked, elementStateChecked},
			condition: ElementCondition{Kind: ElementConditionStatePresent, State: elementStateChecked}, want: false,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := conditionControlledStatesMatch(test.expected, test.live, test.condition); got != test.want {
				t.Fatalf("conditionControlledStatesMatch() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestConditionIdentityUsesExactNonStateFacts(t *testing.T) {
	t.Parallel()
	expectedStates := []string{elementStateEnabled, elementStateCollapsed}
	liveStates := []string{elementStateEnabled, elementStateExpanded}
	expectedActions := []string{"expand"}
	liveActions := []string{"collapse"}
	state := &ElementCondition{Kind: ElementConditionStatePresent, State: elementStateExpanded}
	if !conditionIdentityMatches(state, "expand", expectedStates, liveStates, expectedActions, liveActions) {
		t.Fatal("state condition rejected its controlled state/action transition")
	}
	if conditionIdentityMatches(state, "toggle", expectedStates, liveStates, expectedActions, liveActions) {
		t.Fatal("state condition accepted an inverse action transition for a non-expansion action")
	}
	if conditionIdentityMatches(
		&ElementCondition{Kind: ElementConditionStatePresent, State: elementStateChecked},
		"toggle", []string{elementStateEnabled}, []string{elementStateEnabled, elementStateChecked},
		[]string{"toggle"}, []string{"delete"},
	) {
		t.Fatal("state condition accepted unrelated action drift")
	}
	focused := &ElementCondition{Kind: ElementConditionFocused}
	if conditionIdentityMatches(focused, "focus", expectedStates, expectedStates, expectedActions, liveActions) {
		t.Fatal("focus condition accepted changed actions")
	}
}

func TestElementConditionRequiresObservableProperty(t *testing.T) {
	t.Parallel()
	condition := &ElementCondition{Kind: ElementConditionStateAbsent, State: elementStateInvalid}
	if elementConditionObservable(condition, []string{elementStateEnabled}, true, true) {
		t.Fatal("unobservable state was accepted")
	}
	condition.State = elementStateChecked
	if !elementConditionObservable(condition, []string{elementStateEnabled, elementStateChecked}, false, false) {
		t.Fatal("observable state was rejected")
	}
	if elementConditionObservable(&ElementCondition{Kind: ElementConditionNotFocused}, nil, false, true) {
		t.Fatal("unobservable focus was accepted")
	}
	if elementConditionObservable(
		&ElementCondition{Kind: ElementConditionValueEqualsActionValue}, nil, true, false,
	) {
		t.Fatal("unobservable value was accepted")
	}
}

func TestValueConditionRejectsOversizedActionValue(t *testing.T) {
	t.Parallel()
	request := ActionRequest{
		Action: "set-value", Value: make([]byte, maxElementConditionValueBytes),
		Postcondition: &ElementCondition{Kind: ElementConditionValueEqualsActionValue},
	}
	if err := validateElementCondition(request); err != nil {
		t.Fatalf("boundary value condition: %v", err)
	}
	request.Value = append(request.Value, 0)
	if err := validateElementCondition(request); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("oversized value condition error = %v, want invalid tree", err)
	}
}

func TestElementConditionSatisfiedKeepsActionValuePrivate(t *testing.T) {
	t.Parallel()
	value := []byte("8.0")
	satisfied, err := elementConditionSatisfied(
		&ElementCondition{Kind: ElementConditionValueEqualsActionValue},
		nil, false, "slider", "8", false, value,
	)
	if err != nil || !satisfied || !slices.Equal(value, []byte("8.0")) {
		t.Fatalf("numeric condition = %t, %v, value=%q", satisfied, err, value)
	}
	if satisfied, err = elementConditionSatisfied(
		&ElementCondition{Kind: ElementConditionValueEqualsActionValue},
		nil, false, "slider", "not-a-number", false, value,
	); !errors.Is(err, ErrInvalidTree) || satisfied {
		t.Fatalf("invalid numeric condition = %t, %v", satisfied, err)
	}
	if satisfied, err = elementConditionSatisfied(
		&ElementCondition{Kind: ElementConditionValueEqualsActionValue},
		nil, false, "textbox", "private value", true, []byte("private value"),
	); err != nil || satisfied {
		t.Fatalf("truncated value condition = %t, %v; want unsatisfied without error", satisfied, err)
	}
}

func TestFinalActionGateOrdersConditionBeforeExactValidation(t *testing.T) {
	t.Parallel()
	condition := &ElementCondition{Kind: ElementConditionFocused}
	var calls []string
	beforeCondition := func() error {
		calls = append(calls, "quota")
		return nil
	}
	already, err := finalActionGate(condition, beforeCondition, func() (bool, error) {
		calls = append(calls, "condition")
		return false, nil
	}, func() error {
		calls = append(calls, "exact")
		return nil
	})
	if err != nil || already || !slices.Equal(calls, []string{"quota", "condition", "exact"}) {
		t.Fatalf("unsatisfied gate = %t, %v, calls=%v", already, err, calls)
	}

	calls = nil
	already, err = finalActionGate(condition, beforeCondition, func() (bool, error) {
		calls = append(calls, "condition")
		return true, nil
	}, func() error {
		calls = append(calls, "exact")
		return nil
	})
	if err != nil || !already || !slices.Equal(calls, []string{"quota", "condition"}) {
		t.Fatalf("satisfied gate = %t, %v, calls=%v", already, err, calls)
	}

	backendErr := errors.New("transient backend failure")
	calls = nil
	already, err = finalActionGate(condition, beforeCondition, func() (bool, error) {
		calls = append(calls, "condition")
		return false, backendErr
	}, func() error {
		calls = append(calls, "exact")
		return nil
	})
	if !errors.Is(err, backendErr) || already || !slices.Equal(calls, []string{"quota", "condition"}) {
		t.Fatalf("failed gate = %t, %v, calls=%v", already, err, calls)
	}

	quotaErr := errors.New("quota exhausted")
	calls = nil
	already, err = finalActionGate(condition, func() error {
		calls = append(calls, "quota")
		return quotaErr
	}, func() (bool, error) {
		calls = append(calls, "condition")
		return true, nil
	}, func() error {
		calls = append(calls, "exact")
		return nil
	})
	if !errors.Is(err, quotaErr) || already || !slices.Equal(calls, []string{"quota"}) {
		t.Fatalf("quota-rejected gate = %t, %v, calls=%v", already, err, calls)
	}
}

func TestFinalGateCallbackErrorPreservesCauseWithoutExposingItsMessage(t *testing.T) {
	t.Parallel()
	cause := errors.New("private policy detail")
	err := runFinalGateCallback(t.Context(), func(context.Context) error { return cause })
	if err == nil || !errors.Is(err, cause) || strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("wrapped callback error = %v", err)
	}
	got, ok := finalGateCallbackCause(fmt.Errorf("native wrapper: %w", err))
	if !ok || got != cause {
		t.Fatalf("callback cause = %v, %t", got, ok)
	}
	if err := runFinalGateCallback(t.Context(), nil); err != nil {
		t.Fatalf("nil callback = %v", err)
	}
}
