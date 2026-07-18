//go:build linux && !cgo

package robotgo

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

const (
	linuxPIDFDOpenFlags              = 0
	linuxPIDFDSendSignalFlags        = 0
	linuxPIDFDProcessExistenceSignal = unix.Signal(0)
)

type linuxPIDFDRuntime struct {
	openPIDFD       func(int, int) (int, error)
	closeFD         func(int) error
	sendSignal      func(int, unix.Signal, *unix.Siginfo, int) error
	processIdentity func(int) (int64, error)
}

func closeWindowProcessExists(pid int) (bool, error) {
	return PidExists(pid)
}

func closeWindowProcessKill(pid int, identity int64) error {
	return closeWindowProcessKillLinux(
		pid,
		identity,
		linuxPIDFDRuntime{
			openPIDFD:       unix.PidfdOpen,
			closeFD:         unix.Close,
			sendSignal:      unix.PidfdSendSignal,
			processIdentity: closeWindowProcessIdentity,
		},
	)
}

func closeWindowProcessKillLinux(
	pid int,
	identity int64,
	runtime linuxPIDFDRuntime,
) (resultErr error) {
	if runtime.openPIDFD == nil || runtime.closeFD == nil ||
		runtime.sendSignal == nil || runtime.processIdentity == nil {
		return fmt.Errorf("%w: incomplete Linux pidfd runtime", ErrNotSupported)
	}
	pidfd, err := runtime.openPIDFD(pid, linuxPIDFDOpenFlags)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
			return fmt.Errorf(
				"%w: Linux pidfd force-kill is unavailable: %w",
				ErrNotSupported,
				err,
			)
		}
		return fmt.Errorf("open pidfd for process %d: %w", pid, err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, runtime.closeFD(pidfd))
	}()

	currentIdentity, err := runtime.processIdentity(pid)
	if err != nil {
		probeErr := runtime.sendSignal(
			pidfd,
			linuxPIDFDProcessExistenceSignal,
			nil,
			linuxPIDFDSendSignalFlags,
		)
		if errors.Is(probeErr, unix.ESRCH) {
			return nil
		}
		if probeErr != nil {
			return fmt.Errorf(
				"verify pidfd process %d after identity failure: %w",
				pid,
				errors.Join(err, probeErr),
			)
		}
		return fmt.Errorf("verify pidfd process %d identity: %w", pid, err)
	}
	if currentIdentity != identity {
		return nil
	}
	if err := runtime.sendSignal(
		pidfd,
		unix.SIGKILL,
		nil,
		linuxPIDFDSendSignalFlags,
	); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("force-kill pidfd process %d: %w", pid, err)
	}
	return nil
}
