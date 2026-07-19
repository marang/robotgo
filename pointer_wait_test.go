package robotgo

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var errPointerPositionTimeout = errors.New("pointer position observation timed out")

type pointerPosition struct {
	x int
	y int
}

type pointerPositionWaiter struct {
	observe func() (int, int, error)
	now     func() time.Time
	sleep   func(time.Duration)
}

func (waiter pointerPositionWaiter) wait(
	want pointerPosition,
	timeout time.Duration,
	pollInterval time.Duration,
) error {
	deadline := waiter.now().Add(timeout)
	last := pointerPosition{}
	for {
		x, y, err := waiter.observe()
		if err != nil {
			return fmt.Errorf("observe pointer position: %w", err)
		}
		last = pointerPosition{x: x, y: y}
		if last == want {
			return nil
		}
		if !waiter.now().Before(deadline) {
			return fmt.Errorf(
				"%w after %s: want (%d,%d), last observed (%d,%d)",
				errPointerPositionTimeout,
				timeout,
				want.x,
				want.y,
				last.x,
				last.y,
			)
		}
		waiter.sleep(pollInterval)
	}
}

func TestPointerPositionWaiterDelayedObservation(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	observations := []pointerPosition{
		{x: 10, y: 20},
		{x: 30, y: 40},
	}
	observeCount := 0
	sleepCount := 0
	waiter := pointerPositionWaiter{
		observe: func() (int, int, error) {
			position := observations[observeCount]
			observeCount++
			return position.x, position.y, nil
		},
		now: func() time.Time {
			return now
		},
		sleep: func(duration time.Duration) {
			sleepCount++
			now = now.Add(duration)
		},
	}

	err := waiter.wait(pointerPosition{x: 30, y: 40}, time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("wait for delayed pointer position: %v", err)
	}
	if observeCount != 2 {
		t.Errorf("observation count = %d, want 2", observeCount)
	}
	if sleepCount != 1 {
		t.Errorf("sleep count = %d, want 1", sleepCount)
	}
}

func TestPointerPositionWaiterTimeoutIncludesLastPosition(t *testing.T) {
	t.Parallel()

	now := time.Unix(0, 0)
	waiter := pointerPositionWaiter{
		observe: func() (int, int, error) {
			return 7, 9, nil
		},
		now: func() time.Time {
			return now
		},
		sleep: func(duration time.Duration) {
			now = now.Add(duration)
		},
	}

	err := waiter.wait(pointerPosition{x: 11, y: 13}, 20*time.Millisecond, 10*time.Millisecond)
	if !errors.Is(err, errPointerPositionTimeout) {
		t.Fatalf("wait error = %v, want %v", err, errPointerPositionTimeout)
	}
	for _, detail := range []string{"want (11,13)", "last observed (7,9)"} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("wait error %q does not contain %q", err, detail)
		}
	}
}

func TestPointerPositionWaiterObserverFailure(t *testing.T) {
	t.Parallel()

	observeErr := errors.New("location unavailable")
	sleepCalled := false
	waiter := pointerPositionWaiter{
		observe: func() (int, int, error) {
			return 0, 0, observeErr
		},
		now: time.Now,
		sleep: func(time.Duration) {
			sleepCalled = true
		},
	}

	err := waiter.wait(pointerPosition{}, time.Second, 10*time.Millisecond)
	if !errors.Is(err, observeErr) {
		t.Fatalf("wait error = %v, want %v", err, observeErr)
	}
	if sleepCalled {
		t.Fatal("waiter slept after observer failure")
	}
}
