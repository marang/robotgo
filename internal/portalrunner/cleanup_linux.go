//go:build linux

package portalrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const abandonedVMStopTimeout = 5 * time.Second

// CleanupAbandonedRuns stops verified QEMU processes and removes every
// sentinel-owned transient run under a validated state root. Persistent images
// and unrelated entries are never removed.
func CleanupAbandonedRuns(ctx context.Context, stateRoot string) error {
	if err := validateStateRoot(stateRoot); err != nil {
		return err
	}
	stateLock, err := AcquireStateLock(stateRoot)
	if err != nil {
		return err
	}
	defer func() { _ = stateLock.Close() }()

	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		return fmt.Errorf("read portal runner state: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "run-") {
			continue
		}
		runDirectory := filepath.Join(stateRoot, entry.Name())
		if !entry.IsDir() {
			return errors.New("portal runner runtime entry is not a directory")
		}
		if err := stopAbandonedVM(ctx, runDirectory); err != nil {
			return err
		}
		if err := CleanupRun(stateRoot, runDirectory); err != nil {
			return err
		}
	}
	return nil
}

func stopAbandonedVM(ctx context.Context, runDirectory string) error {
	pidFile := filepath.Join(runDirectory, "qemu.pid")
	info, err := os.Lstat(pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect abandoned portal runner VM PID: %w", err)
	}
	if !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 ||
		info.Size() <= 0 ||
		info.Size() > 32 {
		return errors.New("abandoned portal runner VM PID file is unsafe")
	}
	if err := validateCurrentOwner(info); err != nil {
		return err
	}
	pid, err := readPID(pidFile)
	if err != nil {
		return err
	}
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	} else if err != nil {
		return fmt.Errorf("probe abandoned portal runner VM: %w", err)
	}
	if err := validateAbandonedQEMUProcess(pid, runDirectory, pidFile); err != nil {
		if probeError := syscall.Kill(pid, 0); errors.Is(
			probeError,
			syscall.ESRCH,
		) {
			return nil
		}
		return err
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil &&
		!errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("stop abandoned portal runner VM: %w", err)
	}
	stopContext, cancel := context.WithTimeout(ctx, abandonedVMStopTimeout)
	defer cancel()
	if err := waitForProcessExit(
		stopContext,
		pid,
		abandonedVMStopTimeout,
	); err == nil {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil &&
		!errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill abandoned portal runner VM: %w", err)
	}
	killContext, killCancel := context.WithTimeout(
		context.Background(),
		abandonedVMStopTimeout,
	)
	defer killCancel()
	if err := waitForProcessExit(
		killContext,
		pid,
		abandonedVMStopTimeout,
	); err != nil {
		return errors.New("abandoned portal runner VM survived cleanup")
	}
	return nil
}

func validateAbandonedQEMUProcess(
	pid int,
	runDirectory,
	pidFile string,
) error {
	executable, err := os.Readlink(
		filepath.Join("/proc", fmt.Sprintf("%d", pid), "exe"),
	)
	if err != nil ||
		!strings.HasPrefix(filepath.Base(executable), "qemu-system-") {
		return errors.New("abandoned process is not an owned QEMU VM")
	}
	commandLine, err := os.ReadFile(
		filepath.Join("/proc", fmt.Sprintf("%d", pid), "cmdline"),
	)
	if err != nil || len(commandLine) == 0 || len(commandLine) > maximumBuildInput {
		return errors.New("abandoned QEMU command line is unavailable")
	}
	if !qemuArgumentsBindRun(commandLine, runDirectory, pidFile) {
		return errors.New("abandoned QEMU process is not bound to the owned runtime")
	}
	return nil
}

func qemuArgumentsBindRun(
	commandLine []byte,
	runDirectory,
	pidFile string,
) bool {
	arguments := bytes.Split(commandLine, []byte{0})
	hasPIDFile := false
	hasRunDisk := false
	for index, raw := range arguments {
		argument := string(raw)
		if argument == "-pidfile" &&
			index+1 < len(arguments) &&
			string(arguments[index+1]) == pidFile {
			hasPIDFile = true
		}
		if index == 0 ||
			string(arguments[index-1]) != "-drive" ||
			!strings.HasPrefix(argument, "file=") {
			continue
		}
		disk, _, _ := strings.Cut(
			strings.TrimPrefix(argument, "file="),
			",",
		)
		if filepath.Ext(disk) == ".qcow2" &&
			filepath.IsAbs(disk) &&
			filepath.Clean(disk) == disk &&
			filepath.Dir(disk) == runDirectory {
			hasRunDisk = true
		}
	}
	return hasPIDFile && hasRunDisk
}
