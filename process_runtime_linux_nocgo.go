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
	openPIDFD  func(int, int) (int, error)
	closeFD    func(int) error
	sendSignal func(int, unix.Signal, *unix.Siginfo, int) error
}

type linuxCloseWindowProcess struct {
	pid     int
	pidfd   int
	runtime linuxPIDFDRuntime
}

func openCloseWindowProcess(pid int) (closeWindowProcess, error) {
	return openCloseWindowProcessLinux(
		pid,
		linuxPIDFDRuntime{
			openPIDFD:  unix.PidfdOpen,
			closeFD:    unix.Close,
			sendSignal: unix.PidfdSendSignal,
		},
	)
}

func openCloseWindowProcessLinux(
	pid int,
	runtime linuxPIDFDRuntime,
) (closeWindowProcess, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("invalid Linux process id %d", pid)
	}
	if runtime.openPIDFD == nil || runtime.closeFD == nil || runtime.sendSignal == nil {
		return nil, fmt.Errorf("%w: incomplete Linux pidfd runtime", ErrNotSupported)
	}
	pidfd, err := runtime.openPIDFD(pid, linuxPIDFDOpenFlags)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
			return nil, fmt.Errorf(
				"%w: Linux pidfd process binding is unavailable: %w",
				ErrNotSupported,
				err,
			)
		}
		return nil, fmt.Errorf("open pidfd for process %d: %w", pid, err)
	}
	return &linuxCloseWindowProcess{
		pid:     pid,
		pidfd:   pidfd,
		runtime: runtime,
	}, nil
}

func (process *linuxCloseWindowProcess) Running() (bool, error) {
	if process == nil || process.pidfd < 0 {
		return false, fmt.Errorf("%w: Linux pidfd process reference is closed", ErrNotSupported)
	}
	err := process.runtime.sendSignal(
		process.pidfd,
		linuxPIDFDProcessExistenceSignal,
		nil,
		linuxPIDFDSendSignalFlags,
	)
	switch {
	case errors.Is(err, unix.ESRCH):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("probe pidfd process %d: %w", process.pid, err)
	default:
		return true, nil
	}
}

func (process *linuxCloseWindowProcess) Kill() error {
	if process == nil || process.pidfd < 0 {
		return fmt.Errorf("%w: Linux pidfd process reference is closed", ErrNotSupported)
	}
	err := process.runtime.sendSignal(
		process.pidfd,
		unix.SIGKILL,
		nil,
		linuxPIDFDSendSignalFlags,
	)
	if err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("force-kill pidfd process %d: %w", process.pid, err)
	}
	return nil
}

func (process *linuxCloseWindowProcess) Close() error {
	if process == nil || process.pidfd < 0 {
		return nil
	}
	pidfd := process.pidfd
	process.pidfd = -1
	if err := process.runtime.closeFD(pidfd); err != nil {
		return fmt.Errorf("close pidfd for process %d: %w", process.pid, err)
	}
	return nil
}
