//go:build linux

package x11input

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
)

func TestGuardianFakeInputSequencePreservesOrderAndOwnership(t *testing.T) {
	transport := newGuardianTestConnection()
	connection, done := newInProcessGuardian(t, transport)

	steps := []fakeInputStep{
		{eventType: byte(xproto.KeyPress), detail: 10, root: 1},
		{eventType: byte(xproto.KeyRelease), detail: 10, root: 1},
		{eventType: byte(xproto.ButtonPress), detail: 1, root: 1},
		{eventType: byte(xproto.ButtonRelease), detail: 1, root: 1},
	}
	if err := connection.FakeInputSequence(steps); err != nil {
		t.Fatalf("FakeInputSequence: %v", err)
	}

	transport.mu.Lock()
	inputs := append([]guardianTestInput(nil), transport.inputs...)
	pressed := append([]byte(nil), transport.pressed...)
	mask := transport.pointer.Mask
	transport.mu.Unlock()
	want := []guardianTestInput{
		{eventType: byte(xproto.KeyPress), detail: 10},
		{eventType: byte(xproto.KeyRelease), detail: 10},
		{eventType: byte(xproto.ButtonPress), detail: 1},
		{eventType: byte(xproto.ButtonRelease), detail: 1},
	}
	if !reflect.DeepEqual(inputs, want) {
		t.Fatalf("input sequence = %+v, want %+v", inputs, want)
	}
	if guardianKeycodePressed(pressed, 10) || mask != 0 {
		t.Fatalf("balanced sequence retained input: pressed=%t mask=%#x", guardianKeycodePressed(pressed, 10), mask)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server: %v", err)
	}
}

func TestGuardianFakeInputSequenceFailureRetainsCrashCleanupOwnership(t *testing.T) {
	transport := newGuardianTestConnection()
	release := guardianTestInput{eventType: byte(xproto.ButtonRelease), detail: 1}
	transport.fakeInputFailures[release] = 1
	connection, done := newInProcessGuardian(t, transport)

	err := connection.FakeInputSequence([]fakeInputStep{
		{eventType: byte(xproto.ButtonPress), detail: 1, root: 1},
		{eventType: byte(xproto.ButtonRelease), detail: 1, root: 1},
	})
	if err == nil || !strings.Contains(err.Error(), "step 1") {
		t.Fatalf("FakeInputSequence error = %v, want indexed release failure", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close after ambiguous release: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server after ambiguous release: %v", err)
	}

	transport.mu.Lock()
	inputs := append([]guardianTestInput(nil), transport.inputs...)
	mask := transport.pointer.Mask
	transport.mu.Unlock()
	want := []guardianTestInput{
		{eventType: byte(xproto.ButtonPress), detail: 1},
		{eventType: byte(xproto.ButtonRelease), detail: 1},
		{eventType: byte(xproto.ButtonRelease), detail: 1},
	}
	if !reflect.DeepEqual(inputs, want) {
		t.Fatalf("input sequence with cleanup = %+v, want %+v", inputs, want)
	}
	if mask != 0 {
		t.Fatalf("cleanup retained button mask %#x", mask)
	}
}

func TestGuardianFakeInputSequenceRejectsInvalidBatchBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name  string
		steps []fakeInputStep
	}{
		{name: "empty"},
		{
			name: "final delay",
			steps: []fakeInputStep{{
				eventType:  byte(xproto.KeyPress),
				detail:     10,
				root:       1,
				delayAfter: time.Millisecond,
			}},
		},
		{
			name: "delay exceeds request",
			steps: []fakeInputStep{
				{
					eventType:  byte(xproto.KeyPress),
					detail:     10,
					root:       1,
					delayAfter: 2 * time.Second,
				},
				{eventType: byte(xproto.KeyRelease), detail: 10, root: 1},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := newGuardianTestConnection()
			connection, done := newInProcessGuardian(t, transport)
			if err := connection.FakeInputSequence(test.steps); err == nil {
				t.Fatal("invalid FakeInputSequence unexpectedly succeeded")
			}
			transport.mu.Lock()
			inputCount := len(transport.inputs)
			transport.mu.Unlock()
			if inputCount != 0 {
				t.Fatalf("invalid sequence executed %d inputs", inputCount)
			}
			if err := connection.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if err := <-done; err != nil {
				t.Fatalf("guardian server: %v", err)
			}
		})
	}
}
