//go:build darwin

package darwinwindow

import (
	"context"
	"errors"
	"math"
	"testing"

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
		context.Background(), api, 42, "button",
		AccessibilityActionRequest{Action: "expand"}, validate,
	)
	if !errors.Is(err, ErrAccessibilityStaleTarget) || result.Dispatched || mutated {
		t.Fatalf("stale expansion = %+v, %v, mutated=%v", result, err, mutated)
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
