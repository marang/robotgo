//go:build cgo

package robotgo

import (
	"errors"
	"testing"
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
