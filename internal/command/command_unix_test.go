//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package command

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const commandTestTimeout = 2 * time.Second

type testProcesses struct {
	processGroup int
	child        int
}

func writeTestCommand(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backend")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func parseTestProcesses(t *testing.T, data []byte) testProcesses {
	t.Helper()
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("process record = %q, want process group and child PID", data)
	}
	processGroup, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("parse process group %q: %v", data, err)
	}
	child, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("parse child PID %q: %v", data, err)
	}
	return testProcesses{processGroup: processGroup, child: child}
}

func readTestProcesses(t *testing.T, path string) testProcesses {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return parseTestProcesses(t, data)
}

func cleanupTestProcesses(t *testing.T, processes *testProcesses) {
	t.Helper()
	t.Cleanup(func() {
		if processes.processGroup > 0 {
			_ = syscall.Kill(-processes.processGroup, syscall.SIGKILL)
		}
		if processes.child > 0 {
			_ = syscall.Kill(processes.child, syscall.SIGKILL)
		}
	})
}

func requireTestProcessGone(t *testing.T, process int) {
	t.Helper()
	deadline := time.Now().Add(commandTestTimeout)
	for {
		err := syscall.Kill(process, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatalf("probe process %d: %v", process, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("external command child %d survived cleanup", process)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForTestProcesses(t *testing.T, path string) testProcesses {
	t.Helper()
	deadline := time.Now().Add(commandTestTimeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return parseTestProcesses(t, data)
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read process group: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for external command process group")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOutputBoundsInheritedStandardOutput(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "process-group")
	t.Setenv("ROBOTGO_COMMAND_TEST_PGID", pidPath)
	backend := writeTestCommand(t, `#!/bin/sh
/bin/sleep 5 &
child=$!
printf '%s %s' "$$" "$child" > "$ROBOTGO_COMMAND_TEST_PGID"
printf ready
`)
	processes := testProcesses{}
	cleanupTestProcesses(t, &processes)

	started := time.Now()
	output, err := Output(context.Background(), backend)
	elapsed := time.Since(started)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Output error = %v, want exec.ErrWaitDelay", err)
	}
	if string(output) != "ready" {
		t.Fatalf("Output = %q, want ready", output)
	}
	if elapsed >= commandTestTimeout {
		t.Fatalf("Output waited %s for inherited stdout", elapsed)
	}
	processes = readTestProcesses(t, pidPath)
	requireTestProcessGone(t, processes.child)
}

func TestRunBoundsInheritedStandardInput(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "process-group")
	t.Setenv("ROBOTGO_COMMAND_TEST_PGID", pidPath)
	backend := writeTestCommand(t, `#!/bin/sh
exec 3<&0
/bin/sleep 5 <&3 &
child=$!
printf '%s %s' "$$" "$child" > "$ROBOTGO_COMMAND_TEST_PGID"
`)
	processes := testProcesses{}
	cleanupTestProcesses(t, &processes)

	input := strings.NewReader(strings.Repeat("x", 8<<20))
	started := time.Now()
	err := Run(context.Background(), input, backend)
	elapsed := time.Since(started)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Run error = %v, want exec.ErrWaitDelay", err)
	}
	if elapsed >= commandTestTimeout {
		t.Fatalf("Run waited %s for inherited stdin", elapsed)
	}
	processes = readTestProcesses(t, pidPath)
	requireTestProcessGone(t, processes.child)
}

func TestOutputCancellationTerminatesProcessGroup(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "process-group")
	t.Setenv("ROBOTGO_COMMAND_TEST_PGID", pidPath)
	backend := writeTestCommand(t, `#!/bin/sh
/bin/sleep 5 &
child=$!
printf '%s %s' "$$" "$child" > "$ROBOTGO_COMMAND_TEST_PGID"
wait
`)
	processes := testProcesses{}
	cleanupTestProcesses(t, &processes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Output(ctx, backend)
		result <- err
	}()
	processes = waitForTestProcesses(t, pidPath)
	cancel()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled Output returned nil error")
		}
	case <-time.After(commandTestTimeout):
		t.Fatal("canceled Output did not return within cleanup bound")
	}
	requireTestProcessGone(t, processes.child)
}
