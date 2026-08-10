package darwinwindow

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestAccessibilityConditionStateTransitionIsNarrow(t *testing.T) {
	t.Parallel()
	condition := AccessibilityElementCondition{
		Kind: AccessibilityElementConditionStatePresent, State: accessibilityStateExpanded,
	}
	if !accessibilityConditionStatesMatch(
		[]string{accessibilityStateEnabled, accessibilityStateCollapsed},
		[]string{accessibilityStateEnabled, accessibilityStateExpanded},
		condition,
	) {
		t.Fatal("controlled AX expanded transition was rejected")
	}
	if accessibilityConditionStatesMatch(
		[]string{accessibilityStateEnabled, accessibilityStateCollapsed},
		[]string{accessibilityStateEnabled, accessibilityStateExpanded, accessibilityStateSelected},
		condition,
	) {
		t.Fatal("AX condition accepted an unrelated state transition")
	}
}

func TestAccessibilityConditionActionTransitionIsNarrow(t *testing.T) {
	t.Parallel()
	condition := &AccessibilityElementCondition{
		Kind: AccessibilityElementConditionStatePresent, State: accessibilityStateExpanded,
	}
	if !accessibilityConditionIdentityMatches(
		condition, "expand",
		[]string{accessibilityStateEnabled, accessibilityStateCollapsed},
		[]string{accessibilityStateEnabled, accessibilityStateExpanded},
		[]string{"expand", "focus"}, []string{"collapse", "focus"},
	) {
		t.Fatal("AX condition rejected the exact expansion action transition")
	}
	if accessibilityConditionIdentityMatches(
		condition, "expand",
		[]string{accessibilityStateEnabled, accessibilityStateCollapsed},
		[]string{accessibilityStateEnabled, accessibilityStateExpanded},
		[]string{"expand", "focus"}, []string{"delete", "focus"},
	) {
		t.Fatal("AX condition accepted unrelated action drift")
	}
}

func TestAccessibilityCheckedStateIsLimitedToToggleRoles(t *testing.T) {
	t.Parallel()
	for role, state := range map[string]string{
		"checkbox": accessibilityStateChecked,
		"switch":   accessibilityStateChecked,
		"radio":    accessibilityStateSelected,
	} {
		if got := accessibilityRoleValueState(role); got != state {
			t.Fatalf("binary role %q state = %q, want %q", role, got, state)
		}
	}
	for _, role := range []string{"button", "slider", "textbox"} {
		if state := accessibilityRoleValueState(role); state != "" {
			t.Fatalf("non-binary role %q exposes state %q", role, state)
		}
	}
}

func TestAccessibilityElementConditionRequiresObservableProperty(t *testing.T) {
	t.Parallel()
	condition := &AccessibilityElementCondition{
		Kind: AccessibilityElementConditionStateAbsent, State: accessibilityStateInvalid,
	}
	if accessibilityElementConditionObservable(condition, []string{accessibilityStateEnabled}, true, true) {
		t.Fatal("unobservable AX invalid state was accepted")
	}
	condition.State = accessibilityStateExpanded
	if !accessibilityElementConditionObservable(condition, []string{
		accessibilityStateEnabled, accessibilityStateExpanded, accessibilityStateCollapsed,
	}, false, false) {
		t.Fatal("observable AX expansion state was rejected")
	}
	if accessibilityElementConditionObservable(
		&AccessibilityElementCondition{Kind: AccessibilityElementConditionNotFocused}, nil, false, true,
	) {
		t.Fatal("unobservable AX focus was accepted")
	}
	if accessibilityElementConditionObservable(
		&AccessibilityElementCondition{Kind: AccessibilityElementConditionValueEqualsActionValue}, nil, true, false,
	) {
		t.Fatal("unobservable AX value was accepted")
	}
}

func TestAppendAccessibilityValueStateFailsClosedWhenUnobservable(t *testing.T) {
	t.Parallel()
	base := []string{accessibilityStateEnabled}
	states, err := appendAccessibilityValueState(append([]string(nil), base...), "checkbox", false, false)
	if !errors.Is(err, ErrAccessibilityInvalidTree) || states != nil {
		t.Fatalf("missing checkbox AXValue = %v, %v", states, err)
	}
	states, err = appendAccessibilityValueState(append([]string(nil), base...), "checkbox", false, true)
	if err != nil || !slices.Equal(states, base) {
		t.Fatalf("unchecked checkbox state = %v, %v", states, err)
	}
	states, err = appendAccessibilityValueState(append([]string(nil), base...), "switch", true, true)
	if err != nil || !slices.Equal(states, []string{accessibilityStateEnabled, accessibilityStateChecked}) {
		t.Fatalf("checked switch state = %v, %v", states, err)
	}
	states, err = appendAccessibilityValueState(append([]string(nil), base...), "radio", true, true)
	if err != nil || !slices.Equal(states, []string{accessibilityStateEnabled, accessibilityStateSelected}) {
		t.Fatalf("selected radio state = %v, %v", states, err)
	}
	states, err = appendAccessibilityValueState(append([]string(nil), base...), "button", false, false)
	if err != nil || !slices.Equal(states, base) {
		t.Fatalf("non-toggle missing AXValue = %v, %v", states, err)
	}
}

func TestFinalAccessibilityActionGateFailsClosedAndOrdersExactValidation(t *testing.T) {
	t.Parallel()
	condition := &AccessibilityElementCondition{Kind: AccessibilityElementConditionFocused}
	var calls []string
	already, err := finalAccessibilityActionGate(condition, func() (bool, error) {
		calls = append(calls, "condition")
		return false, nil
	}, func() error {
		calls = append(calls, "exact")
		return nil
	})
	if err != nil || already || !slices.Equal(calls, []string{"condition", "exact"}) {
		t.Fatalf("AX final gate = %t, %v, calls=%v", already, err, calls)
	}

	backendErr := errors.New("AX condition read failed")
	calls = nil
	already, err = finalAccessibilityActionGate(condition, func() (bool, error) {
		calls = append(calls, "condition")
		return false, backendErr
	}, func() error {
		calls = append(calls, "exact")
		return nil
	})
	if !errors.Is(err, backendErr) || already || !slices.Equal(calls, []string{"condition"}) {
		t.Fatalf("failed AX final gate = %t, %v, calls=%v", already, err, calls)
	}
}

func TestDispatchAccessibilityMutationChecksCancellationAtCallBoundary(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	result, err := dispatchAccessibilityMutation(ctx, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || result.Dispatched || called {
		t.Fatalf("canceled mutation = %+v, %v, called=%t", result, err, called)
	}

	backendErr := errors.New("native AX call failed")
	result, err = dispatchAccessibilityMutation(t.Context(), func() error {
		called = true
		return backendErr
	})
	if !errors.Is(err, backendErr) || !result.Dispatched || !called {
		t.Fatalf("crossed mutation boundary = %+v, %v, called=%t", result, err, called)
	}
}
