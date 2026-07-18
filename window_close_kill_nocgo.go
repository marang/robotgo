//go:build !cgo

package robotgo

import (
	"errors"
	"fmt"
	"time"

	"github.com/marang/robotgo/internal/windowbackend"
)

const (
	closeWindowKillGracePeriod  = 1500 * time.Millisecond
	closeWindowKillPollInterval = 100 * time.Millisecond
)

type closeWindowKillRuntime struct {
	now         func() time.Time
	sleep       func(time.Duration)
	openProcess func(int) (closeWindowProcess, error)
}

type closeWindowProcess interface {
	Running() (bool, error)
	Kill() error
	Close() error
}

// CloseWindowKill closes the target window and ensures the owning process
// terminates. A PID is the default target; a second argument or NotPid treats
// the first argument as a native handle. If no target is supplied, the active
// window is used. The process is force-killed only after a successful graceful
// close and a bounded grace period.
func CloseWindowKill(args ...int) error {
	backend, err := pureGoWindowBackend()
	if err != nil {
		return err
	}
	return closeWindowKillWith(
		backend,
		args,
		currentTreatAsHandle(),
		platformCloseWindowKillRuntime(),
	)
}

func platformCloseWindowKillRuntime() closeWindowKillRuntime {
	return closeWindowKillRuntime{
		now:         time.Now,
		sleep:       time.Sleep,
		openProcess: openCloseWindowProcess,
	}
}

func closeWindowKillWith(
	backend windowbackend.Backend,
	args []int,
	treatAsHandle bool,
	runtime closeWindowKillRuntime,
) (resultErr error) {
	if err := runtime.validate(); err != nil {
		return err
	}
	handle, pid, err := resolveCloseWindowKillTarget(backend, args, treatAsHandle)
	if err != nil {
		return err
	}
	process, err := runtime.openProcess(pid)
	if err != nil {
		return fmt.Errorf("bind window process %d before close: %w", pid, err)
	}
	if process == nil {
		return fmt.Errorf("%w: process binder returned nil for pid %d", windowbackend.ErrOperation, pid)
	}
	defer func() {
		resultErr = errors.Join(resultErr, process.Close())
	}()

	// Re-read ownership after acquiring the stable process reference. If the
	// original owner exited during acquisition, do not close the stale window
	// or bind a destructive fallback to a replacement that reused its PID.
	currentPID, err := backend.PID(handle)
	if err != nil {
		return err
	}
	if currentPID != pid {
		return fmt.Errorf(
			"%w: window handle %#x owner changed from pid %d to %d",
			windowbackend.ErrWindowNotFound,
			uintptr(handle),
			pid,
			currentPID,
		)
	}
	if err := backend.Close(handle); err != nil {
		return err
	}
	return waitForWindowProcessExit(pid, process, runtime)
}

func resolveCloseWindowKillTarget(
	backend windowbackend.Backend,
	args []int,
	treatAsHandle bool,
) (windowbackend.Handle, int, error) {
	var (
		handle windowbackend.Handle
		err    error
	)
	if len(args) == 0 {
		handle, err = backend.Active()
	} else {
		handle, err = backend.Resolve(args[0], len(args) > 1 || treatAsHandle)
	}
	if err != nil {
		return 0, 0, err
	}
	pid, err := backend.PID(handle)
	if err != nil {
		return 0, 0, err
	}
	if pid <= 0 {
		return 0, 0, fmt.Errorf(
			"%w: window handle %#x returned invalid pid %d",
			windowbackend.ErrWindowNotFound,
			uintptr(handle),
			pid,
		)
	}
	return handle, pid, nil
}

func waitForWindowProcessExit(
	pid int,
	process closeWindowProcess,
	runtime closeWindowKillRuntime,
) error {
	if err := runtime.validate(); err != nil {
		return err
	}
	if process == nil {
		return fmt.Errorf("%w: nil process reference for pid %d", windowbackend.ErrOperation, pid)
	}
	deadline := runtime.now().Add(closeWindowKillGracePeriod)
	for runtime.now().Before(deadline) {
		running, err := process.Running()
		if err != nil {
			return fmt.Errorf("check window process %d after graceful close: %w", pid, err)
		}
		if !running {
			return nil
		}

		remaining := deadline.Sub(runtime.now())
		delay := min(closeWindowKillPollInterval, remaining)
		runtime.sleep(delay)
	}
	running, err := process.Running()
	if err != nil {
		return fmt.Errorf("check window process %d at grace deadline: %w", pid, err)
	}
	if !running {
		return nil
	}
	if err := process.Kill(); err != nil {
		return fmt.Errorf("force-kill window process %d: %w", pid, err)
	}
	return nil
}

func (runtime closeWindowKillRuntime) validate() error {
	if runtime.now == nil || runtime.sleep == nil || runtime.openProcess == nil {
		return fmt.Errorf(
			"%w: incomplete CloseWindowKill runtime",
			windowbackend.ErrOperation,
		)
	}
	return nil
}
