//go:build cgo && linux

package robotgo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	commandpkg "github.com/marang/robotgo/internal/command"
)

const (
	windowHelperStartTimeout = 2 * time.Second
	windowHelperTestTimeout  = 500 * time.Millisecond
)

type windowHelperProcesses struct {
	group int
	child int
}

func writeWindowHelperTestCommand(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compositor-helper")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write compositor helper: %v", err)
	}
	return path
}

func readWindowHelperProcesses(t *testing.T, path string) windowHelperProcesses {
	t.Helper()
	deadline := time.Now().Add(windowHelperStartTimeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			fields := strings.Fields(string(data))
			if len(fields) != 2 {
				t.Fatalf("process record = %q, want process group and child PID", data)
			}
			group, groupErr := strconv.Atoi(fields[0])
			child, childErr := strconv.Atoi(fields[1])
			if groupErr != nil || childErr != nil || group <= 0 || child <= 0 {
				t.Fatalf(
					"invalid process record %q: group error=%v child error=%v",
					data,
					groupErr,
					childErr,
				)
			}
			return windowHelperProcesses{group: group, child: child}
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read process record: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for compositor helper process record")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func cleanupWindowHelperProcesses(t *testing.T, processes *windowHelperProcesses) {
	t.Helper()
	t.Cleanup(func() {
		if processes.group > 0 {
			_ = syscall.Kill(-processes.group, syscall.SIGKILL)
		}
		if processes.child > 0 {
			_ = syscall.Kill(processes.child, syscall.SIGKILL)
		}
	})
}

func windowHelperProcessTerminated(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	closingParen := strings.LastIndexByte(string(data), ')')
	if closingParen < 0 || closingParen+2 >= len(data) {
		return false, fmt.Errorf("parse /proc status for process %d", pid)
	}
	state := data[closingParen+2]
	return state == 'Z' || state == 'X', nil
}

func requireWindowHelperProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(windowHelperStartTimeout)
	for {
		terminated, err := windowHelperProcessTerminated(pid)
		if terminated {
			return
		}
		if err != nil {
			t.Fatalf("probe compositor helper process %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("compositor helper process %d survived bounded cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func useRealWindowCommandRunner(t *testing.T) {
	t.Helper()
	old := runWindowCommand
	runWindowCommand = commandpkg.Output
	t.Cleanup(func() { runWindowCommand = old })
}

func TestWindowCommandTimeoutTerminatesProcessGroup(t *testing.T) {
	useRealWindowCommandRunner(t)
	pidPath := filepath.Join(t.TempDir(), "processes")
	t.Setenv("ROBOTGO_WINDOW_HELPER_TEST_PIDS", pidPath)
	helper := writeWindowHelperTestCommand(t, `#!/bin/sh
/bin/sleep 30 &
child=$!
printf '%s %s' "$$" "$child" > "$ROBOTGO_WINDOW_HELPER_TEST_PIDS"
wait
`)

	processes := windowHelperProcesses{}
	cleanupWindowHelperProcesses(t, &processes)

	started := time.Now()
	output, err := runWindowCommandWithin(windowHelperTestTimeout, helper)
	elapsed := time.Since(started)
	if len(output) != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf(
			"timed window command = %q, %v; want empty output and deadline exceeded",
			output,
			err,
		)
	}
	if elapsed >= windowHelperStartTimeout {
		t.Fatalf("timed window command returned after %s", elapsed)
	}

	processes = readWindowHelperProcesses(t, pidPath)
	requireWindowHelperProcessGone(t, processes.group)
	requireWindowHelperProcessGone(t, processes.child)
}

func TestWindowCommandInheritedOutputTerminatesDescendant(t *testing.T) {
	useRealWindowCommandRunner(t)
	pidPath := filepath.Join(t.TempDir(), "processes")
	t.Setenv("ROBOTGO_WINDOW_HELPER_TEST_PIDS", pidPath)
	helper := writeWindowHelperTestCommand(t, `#!/bin/sh
/bin/sleep 30 &
child=$!
printf '%s %s' "$$" "$child" > "$ROBOTGO_WINDOW_HELPER_TEST_PIDS"
printf ready
`)

	processes := windowHelperProcesses{}
	cleanupWindowHelperProcesses(t, &processes)

	started := time.Now()
	output, err := runWindowCommandWithin(windowHelperStartTimeout, helper)
	elapsed := time.Since(started)
	if string(output) != "ready" || !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf(
			"window command = %q, %v; want ready and exec.ErrWaitDelay",
			output,
			err,
		)
	}
	if elapsed >= windowHelperStartTimeout {
		t.Fatalf("inherited-output window command returned after %s", elapsed)
	}

	processes = readWindowHelperProcesses(t, pidPath)
	requireWindowHelperProcessGone(t, processes.child)
}
