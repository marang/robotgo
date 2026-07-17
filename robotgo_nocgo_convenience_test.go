//go:build !cgo

package robotgo

import (
	"errors"
	"reflect"
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
