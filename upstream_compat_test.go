package robotgo

import (
	"errors"
	"image"
	"reflect"
	"testing"
)

var (
	_ func() error                      = CmdV
	_ func(string) error                = Paste
	_ func(string, ...int)              = Type
	_ func(string, int)                 = TypeDelay
	_ func(...interface{})              = ClickV1
	_ func(string, int, ...bool) error  = MultiClick
	_ func(...int) (*image.RGBA, error) = Capture1
	_ func(string, ...int) error        = SaveCaptureGo
)

func TestCmdVWith(t *testing.T) {
	t.Parallel()

	var key string
	var modifiers []interface{}
	err := cmdVWith("control", func(gotKey string, gotModifiers ...interface{}) error {
		key = gotKey
		modifiers = gotModifiers
		return nil
	})
	if err != nil {
		t.Fatalf("cmdVWith() error = %v", err)
	}
	if key != "v" {
		t.Fatalf("cmdVWith() key = %q, want %q", key, "v")
	}
	if want := []interface{}{"control"}; !reflect.DeepEqual(modifiers, want) {
		t.Fatalf("cmdVWith() modifiers = %#v, want %#v", modifiers, want)
	}
}

func TestCmdVWithPreservesBackendError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("tap failed")
	err := cmdVWith("command", func(string, ...interface{}) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("cmdVWith() error = %v, want %v", err, wantErr)
	}
}

func TestMultiClickWith(t *testing.T) {
	t.Parallel()

	var calls [][]interface{}
	err := multiClickWith("right", 3, nil, func(args ...interface{}) error {
		calls = append(calls, append([]interface{}(nil), args...))
		return nil
	})
	if err != nil {
		t.Fatalf("multiClickWith() error = %v", err)
	}
	want := [][]interface{}{
		{"right", false},
		{"right", false},
		{"right", false},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("multiClickWith() calls = %#v, want %#v", calls, want)
	}
}

func TestMultiClickWithNonPositiveCountDoesNothing(t *testing.T) {
	t.Parallel()

	called := false
	err := multiClickWith("left", -1, []bool{true}, func(...interface{}) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("multiClickWith() error = %v", err)
	}
	if called {
		t.Fatal("multiClickWith() called backend for a non-positive count")
	}
}

func TestMultiClickWithStopsAtFirstError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("click failed")
	calls := 0
	err := multiClickWith("left", 4, nil, func(...interface{}) error {
		calls++
		if calls == 2 {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("multiClickWith() error = %v, want wrapped %v", err, wantErr)
	}
	if calls != 2 {
		t.Fatalf("multiClickWith() calls = %d, want 2", calls)
	}
}
