//go:build cgo

package robotgo

import (
	"errors"
	"testing"
	"time"
)

func TestToggleTrackedNativeMouseHoldCoordinatesOwnership(t *testing.T) {
	holds := make(map[string]mouseHold)
	var transitions []bool
	toggle := func(down bool) error {
		transitions = append(transitions, down)
		return nil
	}

	if err := toggleTrackedNativeMouseHold(holds, "left", true, toggle); err != nil {
		t.Fatalf("first down: %v", err)
	}
	if err := toggleTrackedNativeMouseHold(holds, "left", true, toggle); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("duplicate down error = %v", err)
	}
	if len(transitions) != 1 || !transitions[0] {
		t.Fatalf("duplicate down transitions = %v", transitions)
	}

	if err := toggleTrackedNativeMouseHold(holds, "left", false, toggle); err != nil {
		t.Fatalf("owned up: %v", err)
	}
	if len(transitions) != 2 || transitions[1] {
		t.Fatalf("owned up transitions = %v", transitions)
	}
	if _, held := holds["left"]; held {
		t.Fatal("successful up retained native mouse ownership")
	}

	if err := toggleTrackedNativeMouseHold(holds, "left", false, toggle); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("orphan up error = %v", err)
	}
	if len(transitions) != 2 {
		t.Fatalf("orphan up emitted transition: %v", transitions)
	}
}

func TestToggleTrackedNativeMouseHoldRetainsAmbiguousRelease(t *testing.T) {
	holds := map[string]mouseHold{
		"right": {backend: persistentInputBackendNative},
	}
	releaseFailures := 1
	toggle := func(bool) error {
		if releaseFailures > 0 {
			releaseFailures--
			return errors.New("release failed")
		}
		return nil
	}

	if err := toggleTrackedNativeMouseHold(holds, "right", false, toggle); err == nil {
		t.Fatal("ambiguous release succeeded")
	}
	if _, held := holds["right"]; !held {
		t.Fatal("ambiguous release discarded retryable ownership")
	}
	if err := toggleTrackedNativeMouseHold(holds, "right", false, toggle); err != nil {
		t.Fatalf("release retry: %v", err)
	}
	if _, held := holds["right"]; held {
		t.Fatal("successful release retry retained ownership")
	}
}

func TestToggleTrackedNativeMouseHoldClearsLostBackendOwnership(t *testing.T) {
	holds := map[string]mouseHold{
		"center": {backend: persistentInputBackendNative},
	}
	if err := toggleTrackedNativeMouseHold(
		holds,
		"center",
		false,
		func(bool) error { return ErrInputOwnership },
	); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("lost backend ownership error = %v", err)
	}
	if _, held := holds["center"]; held {
		t.Fatal("lost backend ownership retained an unreleasable hold")
	}
}

func TestToggleTrackedNativeMouseHoldDoesNotReleaseOtherBackend(t *testing.T) {
	holds := map[string]mouseHold{
		"left": {backend: persistentInputBackendPortal},
	}
	called := false
	if err := toggleTrackedNativeMouseHold(
		holds,
		"left",
		false,
		func(bool) error {
			called = true
			return nil
		},
	); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("foreign backend release error = %v", err)
	}
	if called {
		t.Fatal("foreign backend release reached native toggle")
	}
	if _, held := holds["left"]; !held {
		t.Fatal("foreign backend release discarded portal ownership")
	}
}

func TestToggleTrackedNativeMouseHoldDoesNotRecordRejectedDown(t *testing.T) {
	holds := make(map[string]mouseHold)
	wantErr := errors.New("down rejected")
	if err := toggleTrackedNativeMouseHold(
		holds,
		"left",
		true,
		func(bool) error { return wantErr },
	); !errors.Is(err, wantErr) {
		t.Fatalf("rejected down error = %v", err)
	}
	if len(holds) != 0 {
		t.Fatalf("rejected down recorded ownership: %+v", holds)
	}
}

func TestClickTrackedNativeMouseHoldRetainsAmbiguousRelease(t *testing.T) {
	holds := make(map[string]mouseHold)
	wantErr := errors.New("release unconfirmed")
	var transitions []bool
	var delays []time.Duration
	toggle := func(down bool) error {
		transitions = append(transitions, down)
		if !down {
			return wantErr
		}
		return nil
	}

	err := clickTrackedNativeMouseHold(
		holds,
		"left",
		false,
		toggle,
		func(delay time.Duration) { delays = append(delays, delay) },
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("click error = %v", err)
	}
	if len(transitions) != 2 || !transitions[0] || transitions[1] {
		t.Fatalf("click transitions = %v", transitions)
	}
	if len(delays) != 1 || delays[0] != 5*time.Millisecond {
		t.Fatalf("click delays = %v", delays)
	}
	if hold, held := holds["left"]; !held || hold.backend != persistentInputBackendNative {
		t.Fatalf("ambiguous click release ownership = %+v, held = %v", hold, held)
	}
}

func TestClickTrackedNativeMouseHoldTracksBothDoubleClickReleases(t *testing.T) {
	holds := make(map[string]mouseHold)
	var transitions []bool
	var delays []time.Duration

	if err := clickTrackedNativeMouseHold(
		holds,
		"right",
		true,
		func(down bool) error {
			transitions = append(transitions, down)
			return nil
		},
		func(delay time.Duration) { delays = append(delays, delay) },
	); err != nil {
		t.Fatalf("double click: %v", err)
	}
	wantTransitions := []bool{true, false, true, false}
	if len(transitions) != len(wantTransitions) {
		t.Fatalf("double-click transitions = %v", transitions)
	}
	for i := range wantTransitions {
		if transitions[i] != wantTransitions[i] {
			t.Fatalf("double-click transitions = %v", transitions)
		}
	}
	wantDelays := []time.Duration{
		5 * time.Millisecond,
		200 * time.Millisecond,
		5 * time.Millisecond,
	}
	if len(delays) != len(wantDelays) {
		t.Fatalf("double-click delays = %v", delays)
	}
	for i := range wantDelays {
		if delays[i] != wantDelays[i] {
			t.Fatalf("double-click delays = %v", delays)
		}
	}
	if _, held := holds["right"]; held {
		t.Fatal("successful double click retained native mouse ownership")
	}
}
