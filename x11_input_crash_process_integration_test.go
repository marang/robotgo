//go:build linux && !cgo && x11integration && !wayland

package robotgo_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/jezek/xgb/xproto"
	"github.com/marang/robotgo"
	"golang.org/x/sys/unix"
)

const (
	x11CrashHelperEnv              = "ROBOTGO_X11_CRASH_WORKLOAD_HELPER"
	x11CrashObserverEnv            = "ROBOTGO_X11_CRASH_OBSERVER_HELPER"
	x11CrashDisplayEnv             = "DISPLAY"
	x11CrashWaylandDisplayEnv      = "WAYLAND_DISPLAY"
	x11CrashSessionTypeEnv         = "XDG_SESSION_TYPE"
	x11CrashProcRoot               = "/proc"
	x11CrashReadyFD                = 3
	x11CrashReadyVersion           = "robotgo-x11-crash-ready-v2"
	x11CrashTestPattern            = "^TestPureGoX11CrashRestoresScratchAndOwnedInput$"
	x11CrashHelperTimeout          = 20 * time.Second
	x11CrashReadyTimeout           = 5 * time.Second
	x11CrashWaitTimeout            = 5 * time.Second
	x11CrashGuardianTimeout        = 8 * time.Second
	x11CrashRestoreTimeout         = 8 * time.Second
	x11CrashObserverTimeout        = 2 * time.Second
	x11CrashObserverShutdownWindow = 250 * time.Millisecond
	x11CrashPollInterval           = 100 * time.Millisecond
	x11CrashXKBCompTimeout         = 3 * time.Second
	x11CrashKeycodeMaximum         = 255
	x11CrashPRSetChildSubreaper    = 36
	x11CrashPRGetChildSubreaper    = 37
	x11CrashMiddleButton           = 2
	x11CrashEmergencyReason        = "emergency restoration after failed crash-lifecycle assertion"
)

type x11CrashReadyMessage struct {
	pid         int
	guardianPID int
	heldKeycode xproto.Keycode
	scratchCode xproto.Keycode
	keysym      uint32
}

func x11CrashHelperEnvironment(environment []string) []string {
	filtered := x11CrashFilteredEnvironment(environment)
	return append(
		filtered,
		x11CrashHelperEnv+"=1",
		x11RequiredEnv+"=1",
		envExpectedX11Implementation+"="+string(robotgo.RuntimeImplementationPureGo),
	)
}

func x11CrashObserverEnvironment(environment []string) []string {
	return append(x11CrashFilteredEnvironment(environment), x11CrashObserverEnv+"=1")
}

func x11CrashFilteredEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment)+3)
	for _, value := range environment {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case x11CrashHelperEnv, x11CrashObserverEnv, x11RequiredEnv,
			x11CrashWaylandDisplayEnv, x11CrashSessionTypeEnv,
			envExpectedX11Implementation:
			continue
		default:
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func x11CrashReadReady(file *os.File) (x11CrashReadyMessage, error) {
	if err := file.SetReadDeadline(time.Now().Add(x11CrashReadyTimeout)); err != nil {
		return x11CrashReadyMessage{}, fmt.Errorf("set READY deadline: %w", err)
	}
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil {
		return x11CrashReadyMessage{}, err
	}
	fields := strings.Fields(line)
	if len(fields) != 6 || fields[0] != x11CrashReadyVersion {
		return x11CrashReadyMessage{}, fmt.Errorf("invalid READY payload %q", strings.TrimSpace(line))
	}
	values := make(map[string]uint64, 5)
	for _, field := range fields[1:] {
		name, raw, found := strings.Cut(field, "=")
		if !found {
			return x11CrashReadyMessage{}, fmt.Errorf("invalid READY field %q", field)
		}
		value, parseErr := strconv.ParseUint(raw, 10, 32)
		if parseErr != nil {
			return x11CrashReadyMessage{}, fmt.Errorf("parse READY field %q: %w", field, parseErr)
		}
		values[name] = value
	}
	if values["pid"] == 0 || values["guardian"] == 0 || values["held"] == 0 || values["scratch"] == 0 || values["keysym"] == 0 {
		return x11CrashReadyMessage{}, fmt.Errorf("incomplete READY payload %q", strings.TrimSpace(line))
	}
	if values["held"] > x11CrashKeycodeMaximum || values["scratch"] > x11CrashKeycodeMaximum {
		return x11CrashReadyMessage{}, fmt.Errorf("READY keycode is outside 1..%d in %q", x11CrashKeycodeMaximum, strings.TrimSpace(line))
	}
	return x11CrashReadyMessage{
		pid:         int(values["pid"]),
		guardianPID: int(values["guardian"]),
		heldKeycode: xproto.Keycode(values["held"]),
		scratchCode: xproto.Keycode(values["scratch"]),
		keysym:      uint32(values["keysym"]),
	}, nil
}

func x11CrashEnableSubreaper() (func() error, error) {
	var previous int32
	if err := unix.Prctl(
		x11CrashPRGetChildSubreaper,
		uintptr(unsafe.Pointer(&previous)),
		0,
		0,
		0,
	); err != nil {
		return nil, fmt.Errorf("get child-subreaper state: %w", err)
	}
	runtime.KeepAlive(&previous)
	if previous != 0 && previous != 1 {
		return nil, fmt.Errorf("invalid child-subreaper state %d", previous)
	}
	if previous == 0 {
		if err := unix.Prctl(x11CrashPRSetChildSubreaper, 1, 0, 0, 0); err != nil {
			return nil, fmt.Errorf("set child subreaper: %w", err)
		}
	}
	return func() error {
		if previous == 1 {
			return nil
		}
		if err := unix.Prctl(x11CrashPRSetChildSubreaper, 0, 0, 0, 0); err != nil {
			return fmt.Errorf("clear child subreaper: %w", err)
		}
		return nil
	}, nil
}

func x11CrashOnlyChildPID(parentPID int) (int, error) {
	taskPath := fmt.Sprintf("%s/%d/task", x11CrashProcRoot, parentPID)
	tasks, err := os.ReadDir(taskPath)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", taskPath, err)
	}
	children := make(map[int]struct{})
	for _, task := range tasks {
		if !task.IsDir() {
			continue
		}
		path := fmt.Sprintf("%s/%s/children", taskPath, task.Name())
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return 0, fmt.Errorf("read %s: %w", path, readErr)
		}
		for _, field := range strings.Fields(string(content)) {
			pid, parseErr := strconv.Atoi(field)
			if parseErr != nil || pid <= 0 {
				return 0, fmt.Errorf("parse guardian child pid %q: %v", field, parseErr)
			}
			children[pid] = struct{}{}
		}
	}
	if len(children) != 1 {
		return 0, fmt.Errorf("workload child pids = %v, want exactly one guardian", children)
	}
	for pid := range children {
		return pid, nil
	}
	return 0, errors.New("workload guardian child disappeared")
}

func x11CrashWaitAdoptedChild(pid int, timeout time.Duration) (syscall.WaitStatus, bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		var status syscall.WaitStatus
		waitedPID, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		switch {
		case err == nil && waitedPID == pid:
			return status, true, nil
		case err == nil && waitedPID == 0:
		case errors.Is(err, syscall.EINTR):
		case err != nil:
			return 0, false, err
		default:
			return 0, false, fmt.Errorf("wait4 returned pid %d for guardian %d", waitedPID, pid)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, false, nil
		}
		if remaining > x11CrashPollInterval {
			remaining = x11CrashPollInterval
		}
		timer := time.NewTimer(remaining)
		<-timer.C
	}
}

func x11CrashKillAndReapGuardian(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill: %w", err)
	}
	_, reaped, err := x11CrashWaitAdoptedChild(pid, x11CrashWaitTimeout)
	if err != nil {
		return fmt.Errorf("reap: %w", err)
	}
	if !reaped {
		return fmt.Errorf("not reaped within %s", x11CrashWaitTimeout)
	}
	return nil
}

func x11CrashAssertGuardianExit(status syscall.WaitStatus) error {
	if !status.Exited() || status.ExitStatus() != 0 {
		return fmt.Errorf("wait status %v, want successful exit", status)
	}
	return nil
}

func x11CrashAssertSIGKILL(waitErr error) error {
	if waitErr == nil {
		return errors.New("workload exited successfully instead of being killed")
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		return fmt.Errorf("wait error %T %v, want *exec.ExitError", waitErr, waitErr)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return fmt.Errorf("exit status %T, want syscall.WaitStatus", exitErr.Sys())
	}
	if !status.Signaled() || status.Signal() != syscall.SIGKILL {
		return fmt.Errorf("wait status %v, want signal %s", status, syscall.SIGKILL)
	}
	return nil
}

func x11CrashHelperLog(path string) string {
	output, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<read helper log: %v>", err)
	}
	if len(output) == 0 {
		return "<empty>"
	}
	return strings.TrimSpace(string(output))
}
