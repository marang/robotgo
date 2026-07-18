//go:build windows && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"math"

	"golang.org/x/sys/windows"
)

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

func closeWindowProcessExists(pid int) (bool, error) {
	nativePID, err := windowsCloseWindowProcessID(pid)
	if err != nil {
		return false, err
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, nativePID)
	switch {
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return true, nil
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("open process %d for existence check: %w", pid, err)
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()

	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, fmt.Errorf("wait for process %d: %w", pid, err)
	}
	switch status {
	case uint32(windows.WAIT_TIMEOUT):
		return true, nil
	case windows.WAIT_OBJECT_0:
		return false, nil
	default:
		return false, fmt.Errorf("wait for process %d returned status %#x", pid, status)
	}
}

func closeWindowProcessKill(pid int, identity int64) error {
	nativePID, err := windowsCloseWindowProcessID(pid)
	if err != nil {
		return err
	}
	handle, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		nativePID,
	)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open process %d for termination: %w", pid, err)
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()
	currentIdentity, err := windowsProcessIdentityFromHandle(handle, pid)
	if err != nil {
		return fmt.Errorf("verify process %d before termination: %w", pid, err)
	}
	if currentIdentity != identity {
		return nil
	}
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("terminate process %d: %w", pid, err)
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
