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
	now             func() time.Time
	sleep           func(time.Duration)
	pidExists       func(int) (bool, error)
	processIdentity func(int) (int64, error)
	kill            func(int) error
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
		now:             time.Now,
		sleep:           time.Sleep,
		pidExists:       PidExists,
		processIdentity: closeWindowProcessIdentity,
		kill:            Kill,
	}
}

func closeWindowKillWith(
	backend windowbackend.Backend,
	args []int,
	treatAsHandle bool,
	runtime closeWindowKillRuntime,
) error {
	if err := runtime.validate(); err != nil {
		return err
	}
	handle, pid, err := resolveCloseWindowKillTarget(backend, args, treatAsHandle)
	if err != nil {
		return err
	}
	identity, err := runtime.processIdentity(pid)
	if err != nil {
		return fmt.Errorf("capture window process %d identity: %w", pid, err)
	}
	if identity <= 0 {
		return fmt.Errorf(
			"%w: window process %d returned invalid identity %d",
			windowbackend.ErrOperation,
			pid,
			identity,
		)
	}
	if err := backend.Close(handle); err != nil {
		return err
	}
	return waitForWindowProcessExit(pid, identity, runtime)
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
	identity int64,
	runtime closeWindowKillRuntime,
) error {
	if err := runtime.validate(); err != nil {
		return err
	}
	if identity <= 0 {
		return fmt.Errorf(
			"%w: invalid process identity %d for pid %d",
			windowbackend.ErrOperation,
			identity,
			pid,
		)
	}
	deadline := runtime.now().Add(closeWindowKillGracePeriod)
	for {
		exists, err := runtime.pidExists(pid)
		if err != nil {
			return fmt.Errorf("check window process %d after graceful close: %w", pid, err)
		}
		if !exists {
			return nil
		}
		currentIdentity, err := runtime.processIdentity(pid)
		if err != nil {
			stillExists, probeErr := runtime.pidExists(pid)
			if probeErr != nil {
				return fmt.Errorf(
					"verify window process %d identity after probe failure: %w",
					pid,
					errors.Join(err, probeErr),
				)
			}
			if !stillExists {
				return nil
			}
			return fmt.Errorf("verify window process %d identity: %w", pid, err)
		}
		if currentIdentity != identity {
			return nil
		}

		remaining := deadline.Sub(runtime.now())
		if remaining <= 0 {
			break
		}
		delay := min(closeWindowKillPollInterval, remaining)
		runtime.sleep(delay)
	}
	if err := runtime.kill(pid); err != nil {
		return fmt.Errorf("force-kill window process %d: %w", pid, err)
	}
	return nil
}

func (runtime closeWindowKillRuntime) validate() error {
	if runtime.now == nil || runtime.sleep == nil ||
		runtime.pidExists == nil || runtime.processIdentity == nil ||
		runtime.kill == nil {
		return fmt.Errorf(
			"%w: incomplete CloseWindowKill runtime",
			windowbackend.ErrOperation,
		)
	}
	return nil
}
