//go:build !cgo

package robotgo

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestPasteStringWithStopsBeforeClipboardWhenKeyboardIsUnavailable(t *testing.T) {
	readinessErr := errors.New("keyboard unavailable")
	var calls []string

	err := pasteStringWith(
		"text",
		"windows",
		func() error {
			calls = append(calls, "ready")
			return readinessErr
		},
		func(string) error {
			calls = append(calls, "write")
			return nil
		},
		func(string, ...interface{}) error {
			calls = append(calls, "tap")
			return nil
		},
	)

	if !errors.Is(err, readinessErr) {
		t.Fatalf("pasteStringWith error = %v, want %v", err, readinessErr)
	}
	if want := []string{"ready"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestPasteStringWithStopsAfterClipboardFailure(t *testing.T) {
	writeErr := errors.New("clipboard unavailable")
	var calls []string

	err := pasteStringWith(
		"text",
		"windows",
		func() error {
			calls = append(calls, "ready")
			return nil
		},
		func(text string) error {
			calls = append(calls, "write:"+text)
			return writeErr
		},
		func(string, ...interface{}) error {
			calls = append(calls, "tap")
			return nil
		},
	)

	if !errors.Is(err, writeErr) {
		t.Fatalf("pasteStringWith error = %v, want %v", err, writeErr)
	}
	if want := []string{"ready", "write:text"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestPasteStringWithSelectsPlatformModifier(t *testing.T) {
	for _, test := range []struct {
		name string
		goos string
		want string
	}{
		{name: "Windows", goos: "windows", want: "control"},
		{name: "Linux", goos: "linux", want: "control"},
		{name: "macOS", goos: "darwin", want: "command"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var gotKey string
			var gotModifiers []interface{}
			err := pasteStringWith(
				"RobotGo",
				test.goos,
				func() error { return nil },
				func(text string) error {
					if text != "RobotGo" {
						t.Fatalf("clipboard text = %q", text)
					}
					return nil
				},
				func(key string, modifiers ...interface{}) error {
					gotKey = key
					gotModifiers = append([]interface{}(nil), modifiers...)
					return nil
				},
			)
			if err != nil {
				t.Fatalf("pasteStringWith: %v", err)
			}
			if gotKey != "v" {
				t.Fatalf("key = %q, want v", gotKey)
			}
			if want := []interface{}{test.want}; !reflect.DeepEqual(gotModifiers, want) {
				t.Fatalf("modifiers = %v, want %v", gotModifiers, want)
			}
		})
	}
}

func TestGetLocationColorWithForwardsCoordinatesAndDisplay(t *testing.T) {
	color, err := getLocationColorWith(
		[]int{2},
		func() (int, int, error) { return -30, 47, nil },
		func(x, y int, displayID ...int) (string, error) {
			if x != -30 || y != 47 {
				t.Fatalf("coordinates = (%d, %d)", x, y)
			}
			if want := []int{2}; !reflect.DeepEqual(displayID, want) {
				t.Fatalf("display IDs = %v, want %v", displayID, want)
			}
			return "a1b2c3", nil
		},
	)
	if err != nil || color != "a1b2c3" {
		t.Fatalf("getLocationColorWith = %q, %v", color, err)
	}
}

func TestGetLocationColorWithPropagatesLocationFailure(t *testing.T) {
	locationErr := errors.New("location unavailable")
	pixelCalled := false

	color, err := getLocationColorWith(
		nil,
		func() (int, int, error) { return 0, 0, locationErr },
		func(int, int, ...int) (string, error) {
			pixelCalled = true
			return "", nil
		},
	)
	if !errors.Is(err, locationErr) {
		t.Fatalf("error = %v, want %v", err, locationErr)
	}
	if color != "" {
		t.Fatalf("color = %q, want empty", color)
	}
	if pixelCalled {
		t.Fatal("pixel backend called after location failure")
	}
}

func TestPureGoSysScaleForwardsWindowsTarget(t *testing.T) {
	var got []int
	scale := pureGoSysScale("windows", []int{42}, func(displayID ...int) float64 {
		got = append([]int(nil), displayID...)
		return 1.5
	})
	if scale != 1.5 {
		t.Fatalf("scale = %v, want 1.5", scale)
	}
	if want := []int{42}; !reflect.DeepEqual(got, want) {
		t.Fatalf("display IDs = %v, want %v", got, want)
	}
}

func TestPureGoSysScaleUsesNeutralFactorOutsideWindows(t *testing.T) {
	called := false
	scale := pureGoSysScale("linux", []int{42}, func(...int) float64 {
		called = true
		return 2
	})
	if scale != 1 {
		t.Fatalf("scale = %v, want 1", scale)
	}
	if called {
		t.Fatal("Windows scale callback called on Linux")
	}
}

func TestPureGoSysScaleRejectsInvalidWindowsFactor(t *testing.T) {
	for _, invalid := range []float64{0, -1, math.NaN()} {
		if scale := pureGoSysScale("windows", nil, func(...int) float64 {
			return invalid
		}); scale != 1 {
			t.Fatalf("scale for %v = %v, want neutral factor 1", invalid, scale)
		}
	}
}

func TestDragWithUsesSelectedButtonAndReleasesIt(t *testing.T) {
	var calls []string
	err := dragWith(
		80,
		90,
		[]string{"right"},
		func(args ...interface{}) error {
			calls = append(calls, "toggle:"+interfacesLabel(args))
			return nil
		},
		func(x, y int, displayID ...int) error {
			calls = append(calls, "move:80,90")
			if x != 80 || y != 90 || len(displayID) != 0 {
				t.Fatalf("move = (%d, %d, %v)", x, y, displayID)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("dragWith: %v", err)
	}
	if want := []string{"toggle:right", "move:80,90", "toggle:right,up"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestDragWithDefaultsToLeftButton(t *testing.T) {
	var toggles [][]interface{}
	err := dragWith(
		1,
		2,
		nil,
		func(args ...interface{}) error {
			toggles = append(toggles, append([]interface{}(nil), args...))
			return nil
		},
		func(int, int, ...int) error { return nil },
	)
	if err != nil {
		t.Fatalf("dragWith: %v", err)
	}
	want := [][]interface{}{{"left"}, {"left", "up"}}
	if !reflect.DeepEqual(toggles, want) {
		t.Fatalf("toggles = %v, want %v", toggles, want)
	}
}

func TestDragWithStopsWhenButtonDownFails(t *testing.T) {
	downErr := errors.New("button down failed")
	moveCalled := false
	toggleCalls := 0
	err := dragWith(
		1,
		2,
		nil,
		func(...interface{}) error {
			toggleCalls++
			return downErr
		},
		func(int, int, ...int) error {
			moveCalled = true
			return nil
		},
	)
	if !errors.Is(err, downErr) {
		t.Fatalf("error = %v, want %v", err, downErr)
	}
	if toggleCalls != 1 || moveCalled {
		t.Fatalf("toggle calls = %d, move called = %v", toggleCalls, moveCalled)
	}
}

func TestDragWithReleasesButtonAfterMoveFailure(t *testing.T) {
	moveErr := errors.New("move failed")
	releaseErr := errors.New("release failed")
	toggleCalls := 0
	err := dragWith(
		1,
		2,
		nil,
		func(...interface{}) error {
			toggleCalls++
			if toggleCalls == 2 {
				return releaseErr
			}
			return nil
		},
		func(int, int, ...int) error { return moveErr },
	)
	if !errors.Is(err, moveErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("error = %v, want joined move and release errors", err)
	}
	if toggleCalls != 2 {
		t.Fatalf("toggle calls = %d, want down and release", toggleCalls)
	}
}

func interfacesLabel(values []interface{}) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = value.(string)
	}
	return strings.Join(parts, ",")
}
