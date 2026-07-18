package robotgo

import (
	"errors"
	"testing"
)

func TestKillProcessWithRejectsUnsafePIDWithoutCallingKiller(t *testing.T) {
	tests := []struct {
		name string
		pid  int
	}{
		{name: "zero", pid: 0},
		{name: "negative", pid: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			err := killProcessWith(test.pid, func(int) error {
				called = true
				return nil
			})
			if !errors.Is(err, ErrInvalidPID) {
				t.Fatalf("killProcessWith(%d) error = %v, want ErrInvalidPID", test.pid, err)
			}
			if called {
				t.Fatalf("killProcessWith(%d) called destructive backend", test.pid)
			}
		})
	}
}

func TestKillProcessWithForwardsSafePIDAndError(t *testing.T) {
	killErr := errors.New("injected kill failure")
	calledPID := 0

	err := killProcessWith(42, func(pid int) error {
		calledPID = pid
		return killErr
	})
	if !errors.Is(err, killErr) {
		t.Fatalf("killProcessWith(42) error = %v, want injected failure", err)
	}
	if calledPID != 42 {
		t.Fatalf("destructive backend pid = %d, want 42", calledPID)
	}
}
