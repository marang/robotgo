//go:build !cgo

package robotgo

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/marang/robotgo/internal/windowbackend"
)

type closeKillWindowBackend struct {
	activeHandle  windowbackend.Handle
	resolved      windowbackend.Handle
	pid           int
	pids          []int
	activeErr     error
	resolveErr    error
	pidErr        error
	closeErr      error
	resolveTarget int
	resolveHandle bool
	calls         []string
}

func (backend *closeKillWindowBackend) Active() (windowbackend.Handle, error) {
	backend.calls = append(backend.calls, "active")
	return backend.activeHandle, backend.activeErr
}

func (backend *closeKillWindowBackend) Resolve(
	target int,
	isHandle bool,
) (windowbackend.Handle, error) {
	backend.calls = append(backend.calls, "resolve")
	backend.resolveTarget = target
	backend.resolveHandle = isHandle
	return backend.resolved, backend.resolveErr
}

func (backend *closeKillWindowBackend) Select(int, bool) error { return nil }

func (backend *closeKillWindowBackend) Selected() windowbackend.Handle { return 0 }

func (backend *closeKillWindowBackend) PID(handle windowbackend.Handle) (int, error) {
	backend.calls = append(backend.calls, fmt.Sprintf("pid:%d", handle))
	if len(backend.pids) > 0 {
		pid := backend.pids[0]
		backend.pids = backend.pids[1:]
		return pid, backend.pidErr
	}
	return backend.pid, backend.pidErr
}

func (backend *closeKillWindowBackend) Title(windowbackend.Handle) (string, error) {
	return "", nil
}

func (backend *closeKillWindowBackend) Bounds(
	windowbackend.Handle,
	bool,
) (windowbackend.Rect, error) {
	return windowbackend.Rect{}, nil
}

func (backend *closeKillWindowBackend) Activate(windowbackend.Handle) error { return nil }

func (backend *closeKillWindowBackend) SetState(
	windowbackend.Handle,
	windowbackend.State,
	bool,
) error {
	return nil
}

func (backend *closeKillWindowBackend) State(
	windowbackend.Handle,
	windowbackend.State,
) (bool, error) {
	return false, nil
}

func (backend *closeKillWindowBackend) TopMost(windowbackend.Handle) (bool, error) {
	return false, nil
}

func (backend *closeKillWindowBackend) SetTopMost(windowbackend.Handle, bool) error {
	return nil
}

func (backend *closeKillWindowBackend) Close(handle windowbackend.Handle) error {
	backend.calls = append(backend.calls, fmt.Sprintf("close:%d", handle))
	return backend.closeErr
}

type fakeCloseKillRuntime struct {
	now            time.Time
	exists         []bool
	existsErr      error
	existsCalls    int
	identity       int64
	identities     []int64
	identityErr    error
	identityErrs   []error
	identityCalls  int
	sleepTotal     time.Duration
	killedPID      int
	killedIdentity int64
	killErr        error
	closeErr       error
	closeCalls     int
	unexpectedOp   bool
}

func (runtime *fakeCloseKillRuntime) dependencies() closeWindowKillRuntime {
	if runtime.identity == 0 {
		runtime.identity = 100
	}
	return closeWindowKillRuntime{
		now: func() time.Time {
			return runtime.now
		},
		sleep: func(delay time.Duration) {
			runtime.sleepTotal += delay
			runtime.now = runtime.now.Add(delay)
		},
		processIdentity: func(int) (closeWindowProcessFingerprint, error) {
			identity, err := runtime.nextIdentity()
			if err != nil {
				return closeWindowProcessFingerprint{}, err
			}
			return closeWindowProcessFingerprint{primary: uint64(identity)}, nil
		},
		openProcess: func(
			pid int,
			expected closeWindowProcessFingerprint,
		) (closeWindowProcess, error) {
			identity, err := runtime.nextIdentity()
			if err != nil {
				return nil, err
			}
			if expected.primary != uint64(identity) {
				return nil, errors.New("process identity changed during binding")
			}
			return &fakeCloseWindowProcess{
				pid:      pid,
				identity: int64(expected.primary),
				runtime:  runtime,
			}, nil
		},
	}
}

type fakeCloseWindowProcess struct {
	pid      int
	identity int64
	runtime  *fakeCloseKillRuntime
}

func (process *fakeCloseWindowProcess) Running() (bool, error) {
	runtime := process.runtime
	exists, err := runtime.nextExists()
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	identity, err := runtime.nextIdentity()
	if err != nil {
		stillExists, probeErr := runtime.nextExists()
		if probeErr != nil {
			return false, errors.Join(err, probeErr)
		}
		if !stillExists {
			return false, nil
		}
		return false, err
	}
	return identity == process.identity, nil
}

func (process *fakeCloseWindowProcess) Kill() error {
	process.runtime.killedPID = process.pid
	process.runtime.killedIdentity = process.identity
	return process.runtime.killErr
}

func (process *fakeCloseWindowProcess) Close() error {
	process.runtime.closeCalls++
	return process.runtime.closeErr
}

func (runtime *fakeCloseKillRuntime) nextIdentity() (int64, error) {
	runtime.identityCalls++
	if len(runtime.identityErrs) > 0 {
		identityErr := runtime.identityErrs[0]
		runtime.identityErrs = runtime.identityErrs[1:]
		if identityErr != nil {
			return 0, identityErr
		}
	}
	if runtime.identityErr != nil {
		return 0, runtime.identityErr
	}
	if len(runtime.identities) == 0 {
		return runtime.identity, nil
	}
	identity := runtime.identities[0]
	if len(runtime.identities) > 1 {
		runtime.identities = runtime.identities[1:]
	}
	return identity, nil
}

func (runtime *fakeCloseKillRuntime) nextExists() (bool, error) {
	runtime.existsCalls++
	if runtime.existsErr != nil {
		return false, runtime.existsErr
	}
	if len(runtime.exists) == 0 {
		runtime.unexpectedOp = true
		return false, errors.New("unexpected process probe")
	}
	exists := runtime.exists[0]
	if len(runtime.exists) > 1 {
		runtime.exists = runtime.exists[1:]
	}
	return exists, nil
}

func TestCloseWindowKillWithPIDAllowsGracefulExit(t *testing.T) {
	backend := &closeKillWindowBackend{resolved: 70, pid: 42}
	runtime := &fakeCloseKillRuntime{exists: []bool{true, false}}

	err := closeWindowKillWith(
		backend,
		[]int{42},
		false,
		runtime.dependencies(),
	)
	if err != nil {
		t.Fatalf("closeWindowKillWith() error = %v", err)
	}
	if backend.resolveTarget != 42 || backend.resolveHandle {
		t.Fatalf(
			"Resolve() = (%d, %v), want PID target",
			backend.resolveTarget,
			backend.resolveHandle,
		)
	}
	if got, want := backend.calls, []string{"resolve", "pid:70", "pid:70", "close:70"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("backend calls = %v, want %v", got, want)
	}
	if runtime.killedPID != 0 {
		t.Fatalf("killed PID = %d, want no force-kill", runtime.killedPID)
	}
	if runtime.sleepTotal != closeWindowKillPollInterval {
		t.Fatalf("sleep total = %v, want %v", runtime.sleepTotal, closeWindowKillPollInterval)
	}
}

func TestCloseWindowKillWithHandleForceKillsAfterGracePeriod(t *testing.T) {
	backend := &closeKillWindowBackend{resolved: 70, pid: 42}
	runtime := &fakeCloseKillRuntime{exists: []bool{true}}

	err := closeWindowKillWith(
		backend,
		[]int{70, 1},
		false,
		runtime.dependencies(),
	)
	if err != nil {
		t.Fatalf("closeWindowKillWith() error = %v", err)
	}
	if !backend.resolveHandle {
		t.Fatal("Resolve() treated explicit handle as PID")
	}
	if runtime.sleepTotal != closeWindowKillGracePeriod {
		t.Fatalf("sleep total = %v, want %v", runtime.sleepTotal, closeWindowKillGracePeriod)
	}
	if runtime.killedPID != 42 {
		t.Fatalf("killed PID = %d, want 42", runtime.killedPID)
	}
	if runtime.killedIdentity != 100 {
		t.Fatalf("killed process identity = %d, want 100", runtime.killedIdentity)
	}
	if runtime.unexpectedOp {
		t.Fatal("process polling consumed an unexpected scripted result")
	}
	if runtime.closeCalls != 1 {
		t.Fatalf("stable process reference closes = %d, want 1", runtime.closeCalls)
	}
}

func TestCloseWindowKillWithUsesActiveWindowWithoutTarget(t *testing.T) {
	backend := &closeKillWindowBackend{activeHandle: 70, pid: 42}
	runtime := &fakeCloseKillRuntime{exists: []bool{false}}

	err := closeWindowKillWith(backend, nil, false, runtime.dependencies())
	if err != nil {
		t.Fatalf("closeWindowKillWith() error = %v", err)
	}
	if got, want := backend.calls, []string{"active", "pid:70", "pid:70", "close:70"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("backend calls = %v, want %v", got, want)
	}
}

func TestCloseWindowKillWithHonorsConfiguredHandleMode(t *testing.T) {
	backend := &closeKillWindowBackend{resolved: 70, pid: 42}
	runtime := &fakeCloseKillRuntime{exists: []bool{false}}

	err := closeWindowKillWith(
		backend,
		[]int{70},
		true,
		runtime.dependencies(),
	)
	if err != nil {
		t.Fatalf("closeWindowKillWith() error = %v", err)
	}
	if !backend.resolveHandle {
		t.Fatal("Resolve() ignored configured handle mode")
	}
}

func TestCloseWindowKillWithFailsClosed(t *testing.T) {
	t.Run("identity failure before close", func(t *testing.T) {
		identityErr := errors.New("identity failed")
		backend := &closeKillWindowBackend{resolved: 70, pid: 42}
		runtime := &fakeCloseKillRuntime{
			exists:      []bool{true},
			identityErr: identityErr,
		}

		err := closeWindowKillWith(
			backend,
			[]int{42},
			false,
			runtime.dependencies(),
		)
		if !errors.Is(err, identityErr) {
			t.Fatalf("error = %v, want identity error", err)
		}
		if got, want := backend.calls, []string{"resolve", "pid:70"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("backend calls = %v, want %v", got, want)
		}
		if runtime.existsCalls != 0 || runtime.killedPID != 0 {
			t.Fatalf(
				"process operations = probes %d, kill %d; want none",
				runtime.existsCalls,
				runtime.killedPID,
			)
		}
	})

	t.Run("close failure", func(t *testing.T) {
		closeErr := errors.New("close failed")
		backend := &closeKillWindowBackend{
			resolved: 70,
			pid:      42,
			closeErr: closeErr,
		}
		runtime := &fakeCloseKillRuntime{exists: []bool{true}}

		err := closeWindowKillWith(
			backend,
			[]int{42},
			false,
			runtime.dependencies(),
		)
		if !errors.Is(err, closeErr) {
			t.Fatalf("error = %v, want close error", err)
		}
		if runtime.existsCalls != 0 || runtime.killedPID != 0 {
			t.Fatalf(
				"process operations = probes %d, kill %d; want none",
				runtime.existsCalls,
				runtime.killedPID,
			)
		}
		if runtime.closeCalls != 1 {
			t.Fatalf("stable process reference closes = %d, want 1", runtime.closeCalls)
		}
	})

	t.Run("process probe failure", func(t *testing.T) {
		probeErr := errors.New("probe failed")
		backend := &closeKillWindowBackend{resolved: 70, pid: 42}
		runtime := &fakeCloseKillRuntime{
			exists:    []bool{true},
			existsErr: probeErr,
		}

		err := closeWindowKillWith(
			backend,
			[]int{42},
			false,
			runtime.dependencies(),
		)
		if !errors.Is(err, probeErr) {
			t.Fatalf("error = %v, want process probe error", err)
		}
		if runtime.killedPID != 0 {
			t.Fatalf("killed PID = %d after failed probe", runtime.killedPID)
		}
	})

	t.Run("identity failure after close", func(t *testing.T) {
		identityErr := errors.New("identity failed after close")
		backend := &closeKillWindowBackend{resolved: 70, pid: 42}
		runtime := &fakeCloseKillRuntime{
			exists:       []bool{true},
			identityErrs: []error{nil, nil, identityErr},
		}

		err := closeWindowKillWith(
			backend,
			[]int{42},
			false,
			runtime.dependencies(),
		)
		if !errors.Is(err, identityErr) {
			t.Fatalf("error = %v, want identity error", err)
		}
		if runtime.killedPID != 0 {
			t.Fatalf("killed PID = %d after failed identity check", runtime.killedPID)
		}
	})

	t.Run("stable process reference close failure", func(t *testing.T) {
		closeErr := errors.New("stable process close failed")
		backend := &closeKillWindowBackend{resolved: 70, pid: 42}
		runtime := &fakeCloseKillRuntime{
			exists:   []bool{false},
			closeErr: closeErr,
		}

		err := closeWindowKillWith(
			backend,
			[]int{42},
			false,
			runtime.dependencies(),
		)
		if !errors.Is(err, closeErr) {
			t.Fatalf("error = %v, want process-reference close error", err)
		}
		if runtime.closeCalls != 1 {
			t.Fatalf("stable process reference closes = %d, want 1", runtime.closeCalls)
		}
	})

	t.Run("invalid resolved pid", func(t *testing.T) {
		backend := &closeKillWindowBackend{resolved: 70}
		runtime := &fakeCloseKillRuntime{exists: []bool{true}}

		err := closeWindowKillWith(
			backend,
			[]int{42},
			false,
			runtime.dependencies(),
		)
		if !errors.Is(err, windowbackend.ErrWindowNotFound) {
			t.Fatalf("error = %v, want invalid-window process error", err)
		}
		if got, want := backend.calls, []string{"resolve", "pid:70"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("backend calls = %v, want %v", got, want)
		}
	})
}

func TestCloseWindowKillDoesNotKillReusedPID(t *testing.T) {
	backend := &closeKillWindowBackend{resolved: 70, pid: 42}
	runtime := &fakeCloseKillRuntime{
		exists:     []bool{true},
		identities: []int64{100, 200},
	}

	err := closeWindowKillWith(
		backend,
		[]int{42},
		false,
		runtime.dependencies(),
	)
	if err == nil {
		t.Fatal("closeWindowKillWith() accepted reused PID during process binding")
	}
	if runtime.killedPID != 0 {
		t.Fatalf("reused PID was force-killed: %d", runtime.killedPID)
	}
	if runtime.sleepTotal != 0 {
		t.Fatalf("slept %v after PID identity changed", runtime.sleepTotal)
	}
	if got, want := backend.calls, []string{"resolve", "pid:70"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("backend calls = %v, want %v", got, want)
	}
}

func TestCloseWindowKillRejectsOwnerChangeAfterProcessBinding(t *testing.T) {
	backend := &closeKillWindowBackend{
		resolved: 70,
		pids:     []int{42, 43},
	}
	runtime := &fakeCloseKillRuntime{exists: []bool{true}}

	err := closeWindowKillWith(
		backend,
		[]int{42},
		false,
		runtime.dependencies(),
	)
	if !errors.Is(err, windowbackend.ErrWindowNotFound) {
		t.Fatalf("error = %v, want changed-owner window error", err)
	}
	if got, want := backend.calls, []string{"resolve", "pid:70", "pid:70"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("backend calls = %v, want %v", got, want)
	}
	if runtime.killedPID != 0 {
		t.Fatalf("replacement process was force-killed: %d", runtime.killedPID)
	}
	if runtime.closeCalls != 1 {
		t.Fatalf("stable process reference closes = %d, want 1", runtime.closeCalls)
	}
}

func TestCloseWindowKillHandlesExitBetweenExistenceAndIdentityProbe(t *testing.T) {
	backend := &closeKillWindowBackend{resolved: 70, pid: 42}
	processExitedErr := errors.New("process exited during identity probe")
	runtime := &fakeCloseKillRuntime{
		exists:       []bool{true, false},
		identityErrs: []error{nil, nil, processExitedErr},
	}

	err := closeWindowKillWith(
		backend,
		[]int{42},
		false,
		runtime.dependencies(),
	)
	if err != nil {
		t.Fatalf("closeWindowKillWith() error = %v", err)
	}
	if runtime.killedPID != 0 {
		t.Fatalf("exited process was force-killed: %d", runtime.killedPID)
	}
	if runtime.sleepTotal != 0 {
		t.Fatalf("slept %v after process exit", runtime.sleepTotal)
	}
}

func TestCloseWindowKillRevalidatesAfterFinalSleep(t *testing.T) {
	backend := &closeKillWindowBackend{resolved: 70, pid: 42}
	exists := make([]bool, 16)
	for index := 0; index < len(exists)-1; index++ {
		exists[index] = true
	}
	runtime := &fakeCloseKillRuntime{exists: exists}

	err := closeWindowKillWith(
		backend,
		[]int{42},
		false,
		runtime.dependencies(),
	)
	if err != nil {
		t.Fatalf("closeWindowKillWith() error = %v", err)
	}
	if runtime.sleepTotal != closeWindowKillGracePeriod {
		t.Fatalf("sleep total = %v, want %v", runtime.sleepTotal, closeWindowKillGracePeriod)
	}
	if runtime.existsCalls != len(exists) {
		t.Fatalf("existence probes = %d, want %d", runtime.existsCalls, len(exists))
	}
	if runtime.killedPID != 0 {
		t.Fatalf("process that exited after final sleep was killed: %d", runtime.killedPID)
	}
}

func TestWaitForWindowProcessExitReportsKillFailure(t *testing.T) {
	killErr := errors.New("kill failed")
	runtime := &fakeCloseKillRuntime{
		exists:  []bool{true},
		killErr: killErr,
	}

	dependencies := runtime.dependencies()
	identity, identityErr := dependencies.processIdentity(42)
	if identityErr != nil {
		t.Fatalf("capture fake process identity: %v", identityErr)
	}
	process, openErr := dependencies.openProcess(42, identity)
	if openErr != nil {
		t.Fatalf("open fake process: %v", openErr)
	}
	err := waitForWindowProcessExit(42, process, dependencies)
	if !errors.Is(err, killErr) {
		t.Fatalf("error = %v, want kill error", err)
	}
	if runtime.killedPID != 42 {
		t.Fatalf("killed PID = %d, want 42", runtime.killedPID)
	}
}

func TestWaitForWindowProcessExitRejectsIncompleteRuntime(t *testing.T) {
	err := waitForWindowProcessExit(42, nil, closeWindowKillRuntime{})
	if !errors.Is(err, windowbackend.ErrOperation) {
		t.Fatalf("error = %v, want window operation error", err)
	}
}
