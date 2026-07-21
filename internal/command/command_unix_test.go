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

func writeTestCommand(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backend")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func readTestProcessGroup(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	processGroup, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse process group %q: %v", data, err)
	}
	return processGroup
}

func cleanupTestProcessGroup(t *testing.T, processGroup *int) {
	t.Helper()
	t.Cleanup(func() {
		if *processGroup > 0 {
			_ = syscall.Kill(-*processGroup, syscall.SIGKILL)
		}
	})
}

func requireTestProcessGroupGone(t *testing.T, processGroup int) {
	t.Helper()
	deadline := time.Now().Add(commandTestTimeout)
	for {
		err := syscall.Kill(-processGroup, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatalf("probe process group %d: %v", processGroup, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("external command process group %d survived cleanup", processGroup)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForTestProcessGroup(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(commandTestTimeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			processGroup, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatalf("parse process group %q: %v", data, parseErr)
			}
			return processGroup
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
/bin/sleep 2 &
printf '%s' "$$" > "$ROBOTGO_COMMAND_TEST_PGID"
printf ready
`)
	processGroup := 0
	cleanupTestProcessGroup(t, &processGroup)

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
	processGroup = readTestProcessGroup(t, pidPath)
	requireTestProcessGroupGone(t, processGroup)
}

func TestRunBoundsInheritedStandardInput(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "process-group")
	t.Setenv("ROBOTGO_COMMAND_TEST_PGID", pidPath)
	backend := writeTestCommand(t, `#!/bin/sh
exec 3<&0
/bin/sleep 2 <&3 &
printf '%s' "$$" > "$ROBOTGO_COMMAND_TEST_PGID"
`)
	processGroup := 0
	cleanupTestProcessGroup(t, &processGroup)

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
	processGroup = readTestProcessGroup(t, pidPath)
	requireTestProcessGroupGone(t, processGroup)
}

func TestOutputCancellationTerminatesProcessGroup(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "process-group")
	t.Setenv("ROBOTGO_COMMAND_TEST_PGID", pidPath)
	backend := writeTestCommand(t, `#!/bin/sh
/bin/sleep 2 &
printf '%s' "$$" > "$ROBOTGO_COMMAND_TEST_PGID"
wait
`)
	processGroup := 0
	cleanupTestProcessGroup(t, &processGroup)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Output(ctx, backend)
		result <- err
	}()
	processGroup = waitForTestProcessGroup(t, pidPath)
	cancel()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled Output returned nil error")
		}
	case <-time.After(commandTestTimeout):
		t.Fatal("canceled Output did not return within cleanup bound")
	}
	requireTestProcessGroupGone(t, processGroup)
}
