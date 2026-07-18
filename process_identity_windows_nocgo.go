//go:build windows && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"math"

	"golang.org/x/sys/windows"
)

type windowsCloseWindowProcess struct {
	pid    int
	handle windows.Handle
}

func captureCloseWindowProcessIdentity(
	pid int,
) (closeWindowProcessFingerprint, error) {
	identity, err := closeWindowProcessIdentity(pid)
	if err != nil {
		return closeWindowProcessFingerprint{}, err
	}
	return closeWindowProcessFingerprint{primary: uint64(identity)}, nil
}

func openCloseWindowProcess(
	pid int,
	expected closeWindowProcessFingerprint,
) (closeWindowProcess, error) {
	if !expected.valid() {
		return nil, fmt.Errorf("invalid expected Windows process identity for pid %d", pid)
	}
	nativePID, err := windowsCloseWindowProcessID(pid)
	if err != nil {
		return nil, err
	}
	handle, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE|
			windows.PROCESS_QUERY_LIMITED_INFORMATION|
			windows.SYNCHRONIZE,
		false,
		nativePID,
	)
	if err != nil {
		return nil, fmt.Errorf("open stable process handle for pid %d: %w", pid, err)
	}
	current, identityErr := windowsProcessIdentityFromHandle(handle, pid)
	if identityErr != nil {
		return nil, errors.Join(identityErr, windows.CloseHandle(handle))
	}
	if uint64(current) != expected.primary {
		return nil, errors.Join(
			fmt.Errorf("process %d identity changed while opening stable handle", pid),
			windows.CloseHandle(handle),
		)
	}
	return &windowsCloseWindowProcess{pid: pid, handle: handle}, nil
}

func closeWindowProcessIdentity(pid int) (int64, error) {
	nativePID, err := windowsCloseWindowProcessID(pid)
	if err != nil {
		return 0, err
	}
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		nativePID,
	)
	if err != nil {
		return 0, fmt.Errorf("open process %d for identity: %w", pid, err)
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()

	return windowsProcessIdentityFromHandle(handle, pid)
}

func (process *windowsCloseWindowProcess) Running() (bool, error) {
	if process == nil || process.handle == 0 {
		return false, fmt.Errorf("%w: Windows process handle is closed", ErrNotSupported)
	}
	status, err := windows.WaitForSingleObject(process.handle, 0)
	if err != nil {
		return false, fmt.Errorf("wait for process %d: %w", process.pid, err)
	}
	switch status {
	case uint32(windows.WAIT_TIMEOUT):
		return true, nil
	case windows.WAIT_OBJECT_0:
		return false, nil
	default:
		return false, fmt.Errorf("wait for process %d returned status %#x", process.pid, status)
	}
}

func (process *windowsCloseWindowProcess) Kill() error {
	if process == nil || process.handle == 0 {
		return fmt.Errorf("%w: Windows process handle is closed", ErrNotSupported)
	}
	if err := windows.TerminateProcess(process.handle, 1); err != nil {
		if running, waitErr := process.Running(); waitErr == nil && !running {
			return nil
		}
		return fmt.Errorf("terminate process %d: %w", process.pid, err)
	}
	return nil
}

func (process *windowsCloseWindowProcess) Close() error {
	if process == nil || process.handle == 0 {
		return nil
	}
	handle := process.handle
	process.handle = 0
	if err := windows.CloseHandle(handle); err != nil {
		return fmt.Errorf("close process %d handle: %w", process.pid, err)
	}
	return nil
}

func windowsProcessIdentityFromHandle(
	handle windows.Handle,
	pid int,
) (int64, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(
		handle,
		&creation,
		&exit,
		&kernel,
		&user,
	); err != nil {
		return 0, fmt.Errorf("read process %d creation time: %w", pid, err)
	}
	createdAt := creation.Nanoseconds()
	if createdAt <= 0 {
		return 0, fmt.Errorf("process %d returned invalid creation time %d", pid, createdAt)
	}
	return createdAt, nil
}

func windowsCloseWindowProcessID(pid int) (uint32, error) {
	if pid <= 0 || uint64(pid) > math.MaxUint32 {
		return 0, fmt.Errorf("invalid Windows process id %d", pid)
	}
	return uint32(pid), nil
}
