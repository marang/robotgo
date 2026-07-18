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
	linuxProcPIDPathFormat           = "/proc/%d"
)

type linuxPIDFDRuntime struct {
	openPIDFD       func(int, int) (int, error)
	closeFD         func(int) error
	sendSignal      func(int, unix.Signal, *unix.Siginfo, int) error
	processIdentity func(int) (closeWindowProcessFingerprint, error)
}

type linuxCloseWindowProcess struct {
	pid     int
	pidfd   int
	runtime linuxPIDFDRuntime
}

func captureCloseWindowProcessIdentity(
	pid int,
) (closeWindowProcessFingerprint, error) {
	if pid <= 0 {
		return closeWindowProcessFingerprint{}, fmt.Errorf("invalid Linux process id %d", pid)
	}
	var status unix.Stat_t
	if err := unix.Stat(fmt.Sprintf(linuxProcPIDPathFormat, pid), &status); err != nil {
		return closeWindowProcessFingerprint{}, fmt.Errorf(
			"stat Linux process %d identity: %w",
			pid,
			err,
		)
	}
	identity := closeWindowProcessFingerprint{
		primary:   uint64(status.Dev),
		secondary: status.Ino,
	}
	if !identity.valid() {
		return closeWindowProcessFingerprint{}, fmt.Errorf(
			"linux process %d returned an invalid procfs identity",
			pid,
		)
	}
	return identity, nil
}

func openCloseWindowProcess(
	pid int,
	expected closeWindowProcessFingerprint,
) (closeWindowProcess, error) {
	return openCloseWindowProcessLinux(
		pid,
		expected,
		linuxPIDFDRuntime{
			openPIDFD:       unix.PidfdOpen,
			closeFD:         unix.Close,
			sendSignal:      unix.PidfdSendSignal,
			processIdentity: captureCloseWindowProcessIdentity,
		},
	)
}

func openCloseWindowProcessLinux(
	pid int,
	expected closeWindowProcessFingerprint,
	runtime linuxPIDFDRuntime,
) (closeWindowProcess, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("invalid Linux process id %d", pid)
	}
	if !expected.valid() {
		return nil, fmt.Errorf("invalid expected Linux process identity for pid %d", pid)
	}
	if runtime.openPIDFD == nil || runtime.closeFD == nil ||
		runtime.sendSignal == nil || runtime.processIdentity == nil {
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
	current, identityErr := runtime.processIdentity(pid)
	if identityErr != nil {
		return nil, errors.Join(
			fmt.Errorf("verify pidfd process %d identity: %w", pid, identityErr),
			runtime.closeFD(pidfd),
		)
	}
	if current != expected {
		return nil, errors.Join(
			fmt.Errorf("process %d identity changed while opening pidfd", pid),
			runtime.closeFD(pidfd),
		)
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
