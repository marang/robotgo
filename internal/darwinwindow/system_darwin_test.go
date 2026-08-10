//go:build darwin

package darwinwindow

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/marang/robotgo/internal/windowbackend"
)

func TestEnclosingRect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		position   point
		dimensions size
		want       windowbackend.Rect
		wantErr    bool
	}{
		{
			name:       "fractional outward rounding",
			position:   point{X: 10.25, Y: -20.75},
			dimensions: size{Width: 100.5, Height: 50.25},
			want:       windowbackend.Rect{X: 10, Y: -21, Width: 101, Height: 51},
		},
		{
			name:       "integral",
			position:   point{X: -100, Y: 200},
			dimensions: size{Width: 640, Height: 480},
			want:       windowbackend.Rect{X: -100, Y: 200, Width: 640, Height: 480},
		},
		{
			name:       "zero width",
			dimensions: size{Height: 10},
			wantErr:    true,
		},
		{
			name:       "non-finite position",
			position:   point{X: math.NaN()},
			dimensions: size{Width: 10, Height: 10},
			wantErr:    true,
		},
		{
			name:       "unrepresentable edge",
			position:   point{X: maximumExactCoordinate},
			dimensions: size{Width: 2, Height: 1},
			wantErr:    true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := enclosingRect(test.position, test.dimensions)
			if (err != nil) != test.wantErr {
				t.Fatalf("enclosingRect() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("enclosingRect() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestAXCallErrorClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result int32
		want   error
	}{
		{name: "success", result: axErrorSuccess},
		{name: "permission", result: axErrorAPIDisabled, want: ErrPermission},
		{name: "unsupported attribute", result: axErrorAttributeUnsupported, want: ErrUnsupported},
		{name: "unsupported action", result: axErrorActionUnsupported, want: ErrUnsupported},
		{name: "unsupported implementation", result: axErrorNotImplemented, want: ErrUnsupported},
		{name: "stale element", result: axErrorInvalidUIElement, want: errWindowUnavailable},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := axCallError("test", test.result)
			if !errors.Is(err, test.want) {
				t.Fatalf("axCallError(%d) = %v, want %v", test.result, err, test.want)
			}
		})
	}
}

func TestCopyAttributeRejectsNilSuccessValue(t *testing.T) {
	t.Parallel()
	api := &nativeAPI{
		axUIElementCopyAttributeValue: func(uintptr, uintptr, *uintptr) int32 {
			return axErrorSuccess
		},
	}
	if _, err := copyAttributeLocked(api, 1, 2); err == nil {
		t.Fatal("copyAttributeLocked accepted a nil value from a successful AX call")
	}
}

func TestSemanticOptionalControlValueStateDecodesNativeValues(t *testing.T) {
	t.Parallel()
	const (
		booleanTypeID = uintptr(11)
		numberTypeID  = uintptr(12)
		otherTypeID   = uintptr(13)
		valueRef      = uintptr(21)
	)
	tests := []struct {
		name        string
		role        string
		result      int32
		actualType  uintptr
		boolean     bool
		number      int32
		numberOK    bool
		wantActive  bool
		wantPresent bool
		wantErr     bool
		wantRelease int
	}{
		{
			name: "CoreFoundation boolean remains supported", role: "switch", actualType: booleanTypeID,
			boolean: true, wantActive: true, wantPresent: true, wantRelease: 1,
		},
		{
			name: "CoreFoundation boolean false remains supported", role: "checkbox", actualType: booleanTypeID,
			wantPresent: true, wantRelease: 1,
		},
		{
			name: "CoreFoundation number off", role: "checkbox", actualType: numberTypeID,
			numberOK: true, wantPresent: true, wantRelease: 1,
		},
		{
			name: "CoreFoundation number on", role: "checkbox", actualType: numberTypeID,
			number: 1, numberOK: true, wantActive: true, wantPresent: true, wantRelease: 1,
		},
		{
			name: "AppKit checkbox mixed remains observable", role: "checkbox", actualType: numberTypeID,
			number: -1, numberOK: true, wantPresent: true, wantRelease: 1,
		},
		{
			name: "AX checkbox mixed remains observable", role: "checkbox", actualType: numberTypeID,
			number: 2, numberOK: true, wantPresent: true, wantRelease: 1,
		},
		{
			name: "switch mixed fails closed", role: "switch", actualType: numberTypeID,
			number: 2, numberOK: true, wantErr: true, wantRelease: 1,
		},
		{
			name: "lossy number fails closed", role: "checkbox", actualType: numberTypeID,
			wantErr: true, wantRelease: 1,
		},
		{
			name: "unexpected type fails closed", role: "checkbox", actualType: otherTypeID,
			wantErr: true, wantRelease: 1,
		},
		{name: "missing value remains unobservable", role: "checkbox", result: axErrorNoValue},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			releases := 0
			api := &nativeAPI{
				axUIElementCopyAttributeValue: func(_ uintptr, _ uintptr, value *uintptr) int32 {
					if test.result == axErrorSuccess {
						*value = valueRef
					}
					return test.result
				},
				cfGetTypeID:        func(uintptr) uintptr { return test.actualType },
				cfBooleanGetTypeID: func() uintptr { return booleanTypeID },
				cfBooleanGetValue:  func(uintptr) bool { return test.boolean },
				cfNumberGetTypeID:  func() uintptr { return numberTypeID },
				cfNumberGetValue: func(_ uintptr, numberType int64, output unsafe.Pointer) bool {
					if numberType != cfNumberSInt32Type {
						t.Errorf("CFNumber type = %d, want %d", numberType, cfNumberSInt32Type)
					}
					if !test.numberOK {
						return false
					}
					*(*int32)(output) = test.number
					return true
				},
				cfRelease: func(value uintptr) {
					if value != valueRef {
						t.Errorf("released value = %#x, want %#x", value, valueRef)
					}
					releases++
				},
			}
			active, present, err := semanticOptionalControlValueState(api, 1, 2, test.role)
			if active != test.wantActive || present != test.wantPresent ||
				(err != nil) != test.wantErr ||
				(test.wantErr && !errors.Is(err, ErrAccessibilityInvalidTree)) {
				t.Fatalf(
					"semanticOptionalControlValueState() = %t, %t, %v; want %t, %t, invalid=%t",
					active, present, err, test.wantActive, test.wantPresent, test.wantErr,
				)
			}
			if releases != test.wantRelease {
				t.Fatalf("released values = %d, want %d", releases, test.wantRelease)
			}
		})
	}
}

func TestSemanticStatesKeepMixedCheckboxObservable(t *testing.T) {
	t.Parallel()
	const (
		booleanTypeID  = uintptr(11)
		numberTypeID   = uintptr(12)
		valueRef       = uintptr(21)
		valueAttribute = uintptr(22)
	)
	releases := 0
	api := &nativeAPI{
		axEnabledAttribute:  23,
		axValueAttribute:    valueAttribute,
		axSelectedAttribute: 24,
		axExpandedAttribute: 25,
		axUIElementCopyAttributeValue: func(_ uintptr, attribute uintptr, value *uintptr) int32 {
			if attribute != valueAttribute {
				return axErrorNoValue
			}
			*value = valueRef
			return axErrorSuccess
		},
		cfGetTypeID:        func(uintptr) uintptr { return numberTypeID },
		cfBooleanGetTypeID: func() uintptr { return booleanTypeID },
		cfNumberGetTypeID:  func() uintptr { return numberTypeID },
		cfNumberGetValue: func(_ uintptr, numberType int64, output unsafe.Pointer) bool {
			if numberType != cfNumberSInt32Type {
				t.Errorf("CFNumber type = %d, want %d", numberType, cfNumberSInt32Type)
			}
			*(*int32)(output) = 2
			return true
		},
		cfRelease: func(value uintptr) {
			if value != valueRef {
				t.Errorf("released value = %#x, want %#x", value, valueRef)
			}
			releases++
		},
	}
	states, observable, err := semanticStates(api, 1, "checkbox")
	if err != nil || len(states) != 0 || !slices.Equal(observable, []string{accessibilityStateChecked}) {
		t.Fatalf("mixed checkbox states = %v, observable=%v, err=%v", states, observable, err)
	}
	condition := &AccessibilityElementCondition{
		Kind:  AccessibilityElementConditionStateAbsent,
		State: accessibilityStateChecked,
	}
	if !accessibilityElementConditionObservable(condition, observable, false, false) {
		t.Fatal("mixed checkbox checked-state was not observable")
	}
	satisfied, err := accessibilityElementConditionSatisfied(
		condition, states, false, "checkbox", "", false, nil,
	)
	if err != nil || !satisfied {
		t.Fatalf("mixed checkbox state-absent result = %t, %v", satisfied, err)
	}
	if releases != 1 {
		t.Fatalf("released values = %d, want 1", releases)
	}
}

func TestSemanticValueUsesActualUTF8SizeAndReportsTruncation(t *testing.T) {
	t.Parallel()
	const (
		stringTypeID = uintptr(11)
		valueRef     = uintptr(21)
	)
	for _, test := range []struct {
		name          string
		value         string
		limit         int64
		want          string
		wantTruncated bool
		wantSizing    int
	}{
		{
			name:  "large ASCII below actual-byte limit",
			value: strings.Repeat("a", 400<<10), limit: maximumAXStringBytes,
			want: strings.Repeat("a", 400<<10), wantSizing: 1,
		},
		{name: "bounded prefix", value: "abcdefgh", limit: 5, want: "abcde", wantTruncated: true},
		{name: "multibyte actual size", value: "ééx", limit: 4, want: "éé", wantTruncated: true, wantSizing: 1},
		{name: "surrogate pair fits boundary", value: "A😀B", limit: 5, want: "A😀", wantTruncated: true, wantSizing: 1},
		{name: "surrogate pair does not split", value: "A😀B", limit: 4, want: "A", wantTruncated: true, wantSizing: 2},
		{name: "multibyte exact boundary", value: "A😀", limit: 5, want: "A😀", wantSizing: 1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sizingCalls := 0
			releases := 0
			units := utf16.Encode([]rune(test.value))
			api := &nativeAPI{
				axValueAttribute: 1,
				axUIElementCopyAttributeValue: func(_ uintptr, attribute uintptr, result *uintptr) int32 {
					if attribute != 1 {
						t.Errorf("AX attribute = %#x", attribute)
					}
					*result = valueRef
					return axErrorSuccess
				},
				cfGetTypeID:       func(uintptr) uintptr { return stringTypeID },
				cfStringGetTypeID: func() uintptr { return stringTypeID },
				cfStringGetLength: func(uintptr) int64 { return int64(len(units)) },
				cfStringGetBytes: func(
					_ uintptr, valueRange cfRange, _ uint32, _ byte, _ bool,
					buffer *byte, maxBytes int64, used *int64,
				) int64 {
					if valueRange.Location < 0 || valueRange.Length < 0 ||
						valueRange.Location+valueRange.Length > int64(len(units)) {
						t.Errorf("CFRange = %+v", valueRange)
						return 0
					}
					if buffer == nil {
						sizingCalls++
					}
					var output []byte
					if buffer != nil {
						output = unsafe.Slice(buffer, int(maxBytes))
					}
					start := int(valueRange.Location)
					end := start + int(valueRange.Length)
					converted := 0
					for index := start; index < end; {
						unit := units[index]
						character := rune(unit)
						unitCount := 1
						if 0xD800 <= unit && unit <= 0xDBFF {
							if index+1 >= end || units[index+1] < 0xDC00 || units[index+1] > 0xDFFF {
								break
							}
							character = utf16.DecodeRune(rune(unit), rune(units[index+1]))
							unitCount = 2
						} else if 0xDC00 <= unit && unit <= 0xDFFF {
							break
						}
						encoded := make([]byte, utf8.UTFMax)
						encodedBytes := utf8.EncodeRune(encoded, character)
						if buffer != nil && *used+int64(encodedBytes) > maxBytes {
							break
						}
						if buffer != nil {
							copy(output[int(*used):], encoded[:encodedBytes])
						}
						*used += int64(encodedBytes)
						converted += unitCount
						index += unitCount
					}
					return int64(converted)
				},
				cfRelease: func(value uintptr) {
					if value != valueRef {
						t.Errorf("released value = %#x", value)
					}
					releases++
				},
			}
			got, observable, truncated, err := semanticValueAttribute(api, 42, "textbox", test.limit)
			if err != nil || got != test.want || observable != !test.wantTruncated ||
				truncated != test.wantTruncated || sizingCalls != test.wantSizing || releases != 1 {
				t.Fatalf("semanticValueAttribute() = %d bytes, observable=%t, truncated=%t, %v; want %d bytes, observable=%t, truncated=%t; sizing=%d, releases=%d",
					len(got), observable, truncated, err, len(test.want), !test.wantTruncated,
					test.wantTruncated, sizingCalls, releases)
			}
		})
	}
}

func TestAccessibilityValueConditionRejectsOversizedActionValue(t *testing.T) {
	t.Parallel()
	err := validateAccessibilityElementCondition(AccessibilityActionRequest{
		Action: "set-value", Value: make([]byte, maximumAXStringBytes+1),
		Postcondition: &AccessibilityElementCondition{
			Kind: AccessibilityElementConditionValueEqualsActionValue,
		},
	})
	if !errors.Is(err, ErrAccessibilityInvalidTree) {
		t.Fatalf("oversized value condition error = %v", err)
	}
}

func TestCFStringPrefixRejectsEarlyConversionFailure(t *testing.T) {
	t.Parallel()
	const stringTypeID = uintptr(11)
	api := &nativeAPI{
		cfGetTypeID:       func(uintptr) uintptr { return stringTypeID },
		cfStringGetTypeID: func() uintptr { return stringTypeID },
		cfStringGetLength: func(uintptr) int64 { return 8 },
		cfStringGetBytes: func(
			_ uintptr, _ cfRange, _ uint32, _ byte, _ bool,
			_ *byte, _ int64, used *int64,
		) int64 {
			*used = 0
			return 0
		},
	}
	if _, _, err := cfStringBytesLocked(api, 21, 5); err == nil {
		t.Fatal("early UTF-8 conversion failure was accepted as truncation")
	}
}

func TestAccessibilityValueConditionRejectsBoundedPrefix(t *testing.T) {
	t.Parallel()
	condition := &AccessibilityElementCondition{Kind: AccessibilityElementConditionValueEqualsActionValue}
	details := axSemanticDetails{ValueTruncated: true}
	if !accessibilityElementConditionObservable(
		condition, nil, false, details.ValueObservable || details.ValueTruncated,
	) {
		t.Fatal("truncated AX value was treated as unobservable")
	}
	satisfied, err := accessibilityElementConditionSatisfied(
		condition, nil, false, "textbox", "prefix", true, []byte("prefix"),
	)
	if err != nil || satisfied {
		t.Fatalf("truncated value condition = %t, %v", satisfied, err)
	}
}

func TestDispatchAXExpansionRevalidatesAfterPreparation(t *testing.T) {
	prepared := false
	mutated := false
	api := &nativeAPI{
		axExpandedAttribute: 1,
		cfBooleanTrue:       2,
		axUIElementIsAttributeSettable: func(_ uintptr, _ uintptr, settable *bool) int32 {
			prepared = true
			*settable = true
			return axErrorSuccess
		},
		axUIElementSetAttributeValue: func(uintptr, uintptr, uintptr) int32 {
			mutated = true
			return axErrorSuccess
		},
	}
	validate := func() error {
		if !prepared {
			t.Fatal("window validation ran before expansion preparation")
		}
		return ErrAccessibilityStaleTarget
	}

	result, err := dispatchAXAction(
		context.Background(), api, func() uintptr { return 42 }, "button",
		AccessibilityActionRequest{Action: "expand"},
		func() error { return nil }, func() (bool, error) { return false, nil }, validate,
	)
	if !errors.Is(err, ErrAccessibilityStaleTarget) || result.Dispatched || mutated {
		t.Fatalf("stale expansion = %+v, %v, mutated=%v", result, err, mutated)
	}
}

func TestDispatchAXFinalConditionSkipsPreparedMutation(t *testing.T) {
	prepared := false
	mutated := false
	api := &nativeAPI{
		axExpandedAttribute: 1,
		cfBooleanTrue:       2,
		axUIElementIsAttributeSettable: func(_ uintptr, _ uintptr, settable *bool) int32 {
			prepared = true
			*settable = true
			return axErrorSuccess
		},
		axUIElementSetAttributeValue: func(uintptr, uintptr, uintptr) int32 {
			mutated = true
			return axErrorSuccess
		},
	}
	request := AccessibilityActionRequest{
		Action: "expand",
		Postcondition: &AccessibilityElementCondition{
			Kind: AccessibilityElementConditionStatePresent, State: accessibilityStateExpanded,
		},
	}
	result, err := dispatchAXAction(
		context.Background(), api, func() uintptr { return 42 }, "button", request,
		func() error {
			t.Fatal("exact validation ran after a satisfied condition")
			return nil
		},
		func() (bool, error) {
			if !prepared {
				t.Fatal("condition check ran before expansion preparation")
			}
			return true, nil
		},
		func() error { return nil },
	)
	if err != nil || result.Dispatched || !result.AlreadySatisfied || mutated {
		t.Fatalf("already-satisfied expansion = %+v, %v, mutated=%v", result, err, mutated)
	}
}

func TestDispatchAXReportsErrorsAfterNativeBoundaryAsDispatched(t *testing.T) {
	var calls []string
	api := &nativeAPI{
		axPressAction: 1,
		axUIElementPerformAction: func(uintptr, uintptr) int32 {
			calls = append(calls, "dispatch")
			return axErrorCannotComplete
		},
	}
	result, err := dispatchAXAction(
		context.Background(), api, func() uintptr { return 42 }, "button",
		AccessibilityActionRequest{Action: "press"},
		func() error {
			calls = append(calls, "exact")
			return nil
		},
		func() (bool, error) {
			t.Fatal("nil postcondition unexpectedly ran a condition check")
			return false, nil
		},
		func() error {
			calls = append(calls, "window")
			return nil
		},
	)
	if err == nil || !result.Dispatched || result.AlreadySatisfied ||
		!slices.Equal(calls, []string{"exact", "window", "dispatch"}) {
		t.Fatalf("post-boundary AX error = %+v, %v, calls=%v", result, err, calls)
	}
}

func TestValidateAXElementWindowRejectsReparentedElement(t *testing.T) {
	api := &nativeAPI{
		axUIElementGetWindow: func(_ uintptr, windowID *uint32) int32 {
			*windowID = 99
			return axErrorSuccess
		},
	}
	if err := validateAXElementWindow(api, 42, 77); !errors.Is(err, ErrAccessibilityStaleTarget) {
		t.Fatalf("validateAXElementWindow() error = %v", err)
	}
}
