//go:build linux && clipboardintegration

package clipboard

import (
	"context"
	"encoding/json"
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
)

const (
	envRequireClipboardIntegration = "ROBOTGO_REQUIRE_WAYLAND_CLIPBOARD_INTEGRATION"
	envClipboardFixtureRuntime     = "ROBOTGO_CLIPBOARD_INTEGRATION_RUNTIME"
	envClipboardFixtureSocket      = "ROBOTGO_CLIPBOARD_INTEGRATION_SOCKET"
	envClipboardFixtureGroups      = "ROBOTGO_CLIPBOARD_INTEGRATION_GROUPS"
	clipboardFixtureTimeout        = 5 * time.Second
)

type headlessClipboardFixture struct {
	directory  string
	runtime    string
	socket     string
	ipcSocket  string
	groupsPath string
	spoolBase  string
	command    *exec.Cmd
	done       chan error
	swayPID    int
	closed     bool
}

func unsetClipboardFixtureEnv(t *testing.T, key string) {
	t.Helper()
	value, present := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var err error
		if present {
			err = os.Setenv(key, value)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			t.Error(err)
		}
	})
}

func startHeadlessClipboardFixture(t *testing.T) *headlessClipboardFixture {
	t.Helper()
	if os.Getenv(envRequireClipboardIntegration) != "1" {
		t.Skip("set ROBOTGO_REQUIRE_WAYLAND_CLIPBOARD_INTEGRATION=1 to create a private headless Sway clipboard fixture")
	}
	if os.Geteuid() == 0 {
		t.Fatal("headless Sway clipboard integration requires an unprivileged user")
	}
	for _, path := range []string{"/usr/bin/sway", "/usr/bin/swaymsg", "/usr/bin/dbus-run-session", "/usr/bin/wl-copy", "/usr/bin/wl-paste"} {
		info, err := os.Stat(path)
		if err != nil || info.Mode()&0o111 == 0 {
			t.Fatalf("required clipboard fixture executable is unavailable: %s", path)
		}
	}
	fixture := &headlessClipboardFixture{directory: t.TempDir()}
	fixture.runtime = filepath.Join(fixture.directory, "runtime")
	fixture.spoolBase = filepath.Join(fixture.directory, "spool")
	fixture.groupsPath = filepath.Join(fixture.directory, "helper-groups")
	t.Cleanup(func() { fixture.close(t) })
	for _, name := range []string{"runtime", "spool", "home", "config", "bin"} {
		if err := os.Mkdir(filepath.Join(fixture.directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(fixture.directory, "config", "sway")
	// No includes, commands, physical input devices, Xwayland, or parent desktop.
	if err := os.WriteFile(configPath, []byte("xwayland disable\noutput * mode 800x600\nseat seat0 fallback true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/usr/bin/dbus-run-session", "--", "/usr/bin/sway", "--config", configPath)
	command.Env = []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + filepath.Join(fixture.directory, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(fixture.directory, "config"),
		"XDG_RUNTIME_DIR=" + fixture.runtime,
		"XDG_SESSION_TYPE=wayland", "XDG_CURRENT_DESKTOP=sway",
		"WLR_BACKENDS=headless", "WLR_RENDERER=pixman",
		"WLR_RENDERER_ALLOW_SOFTWARE=1", "WLR_LIBINPUT_NO_DEVICES=1",
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 250 * time.Millisecond
	if err := command.Start(); err != nil {
		t.Fatalf("start private headless Sway: %v", err)
	}
	fixture.command, fixture.done = command, make(chan error, 1)
	go func() { fixture.done <- command.Wait() }()
	deadline := time.Now().Add(clipboardFixtureTimeout)
	for {
		waylandSockets, _ := filepath.Glob(filepath.Join(fixture.runtime, "wayland-*"))
		ipcSockets, _ := filepath.Glob(filepath.Join(fixture.runtime, "sway-ipc.*.sock"))
		for _, candidate := range waylandSockets {
			if info, err := os.Lstat(candidate); err == nil && info.Mode()&os.ModeSocket != 0 {
				fixture.socket = candidate
			}
		}
		if fixture.socket != "" && len(ipcSockets) == 1 {
			fixture.ipcSocket = ipcSockets[0]
			break
		}
		select {
		case err := <-fixture.done:
			fixture.done = nil
			t.Fatalf("private headless Sway exited before readiness: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("private headless Sway did not create its sockets")
		}
		time.Sleep(10 * time.Millisecond)
	}
	parts := strings.Split(filepath.Base(fixture.ipcSocket), ".")
	if len(parts) != 4 {
		t.Fatal("unexpected private Sway socket identity")
	}
	var err error
	fixture.swayPID, err = strconv.Atoi(parts[2])
	if err != nil || fixture.swayPID <= 0 {
		t.Fatal("private Sway process identity is invalid")
	}
	for _, key := range []string{"WAYLAND_DISPLAY", "WAYLAND_SOCKET", "DISPLAY", "SWAYSOCK", "WAYLAND_DEBUG", "DBUS_SESSION_BUS_ADDRESS"} {
		unsetClipboardFixtureEnv(t, key)
	}
	t.Setenv("XDG_RUNTIME_DIR", fixture.runtime)
	t.Setenv(envWaylandDisplay, fixture.socket)
	t.Setenv("SWAYSOCK", fixture.ipcSocket)
	t.Setenv(envClipboardTempDir, fixture.spoolBase)
	t.Setenv("HOME", filepath.Join(fixture.directory, "home"))
	t.Setenv(envClipboardFixtureRuntime, fixture.runtime)
	t.Setenv(envClipboardFixtureSocket, fixture.socket)
	t.Setenv(envClipboardFixtureGroups, fixture.groupsPath)
	fixture.installHelpers(t)
	previousTool, previousError := selectedTool, selectedToolError
	selectedTool, selectedToolError = detectUnixClipboardTool(os.Getenv, exec.LookPath)
	t.Cleanup(func() { selectedTool, selectedToolError = previousTool, previousError })
	if selectedToolError != nil || selectedTool != unixClipboardWayland {
		t.Fatal("private fixture did not select native Wayland helpers")
	}
	fixture.requireIsolation(t)
	return fixture
}

func (fixture *headlessClipboardFixture) installHelpers(t *testing.T) {
	t.Helper()
	for _, name := range []string{wlCopy, wlPaste} {
		script := `#!/bin/sh
[ "$XDG_RUNTIME_DIR" = "$ROBOTGO_CLIPBOARD_INTEGRATION_RUNTIME" ] || exit 90
[ "$WAYLAND_DISPLAY" = "$ROBOTGO_CLIPBOARD_INTEGRATION_SOCKET" ] || exit 91
[ -z "${WAYLAND_SOCKET+x}" ] && [ -z "${DISPLAY+x}" ] || exit 92
if [ "$1" != --version ]; then printf '%s\n' "$$" >> "$ROBOTGO_CLIPBOARD_INTEGRATION_GROUPS"; fi
exec /usr/bin/` + name + ` "$@"
`
		if err := os.WriteFile(filepath.Join(fixture.directory, "bin", name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", filepath.Join(fixture.directory, "bin")+":/usr/bin:/bin")
}

func (fixture *headlessClipboardFixture) requireIsolation(t *testing.T) {
	t.Helper()
	for _, path := range []string{fixture.socket, fixture.ipcSocket} {
		relative, err := filepath.Rel(fixture.runtime, path)
		info, statErr := os.Lstat(path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, "../") || statErr != nil || info.Mode()&os.ModeSocket == 0 {
			t.Fatal("clipboard fixture socket escapes the private runtime")
		}
	}
	query := func(kind string, result any) {
		ctx, cancel := context.WithTimeout(context.Background(), clipboardFixtureTimeout)
		defer cancel()
		command := exec.CommandContext(ctx, "/usr/bin/swaymsg", "--socket", fixture.ipcSocket, "--type", kind, "--raw")
		command.Env = []string{"PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=" + fixture.runtime}
		command.WaitDelay = 250 * time.Millisecond
		output, err := command.Output()
		if err != nil || json.Unmarshal(output, result) != nil {
			t.Fatalf("query private Sway %s failed: %v", kind, err)
		}
	}
	var inputs []json.RawMessage
	query("get_inputs", &inputs)
	if len(inputs) != 0 {
		t.Fatal("private clipboard compositor exposed input devices")
	}
	var outputs []struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	query("get_outputs", &outputs)
	if len(outputs) != 1 || !outputs[0].Active || !strings.HasPrefix(outputs[0].Name, "HEADLESS-") {
		t.Fatal("clipboard fixture is not a single headless output")
	}
}

func (fixture *headlessClipboardFixture) groups() ([]int, error) {
	data, err := os.ReadFile(fixture.groupsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var groups []int
	for _, field := range strings.Fields(string(data)) {
		group, err := strconv.Atoi(field)
		if err != nil || group <= 0 {
			return nil, errors.New("invalid owned clipboard process group")
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func clipboardFixtureGroupAlive(group int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		closingParen := strings.LastIndexByte(string(data), ')')
		if closingParen < 0 {
			continue
		}
		fields := strings.Fields(string(data[closingParen+1:]))
		if len(fields) >= 3 && fields[2] == strconv.Itoa(group) && fields[0] != "Z" && fields[0] != "X" {
			return true
		}
	}
	return false
}

func (fixture *headlessClipboardFixture) requireEmptySpool(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(fixture.spoolBase)
	if err != nil || len(entries) != 0 {
		t.Fatalf("private clipboard spool was not cleaned: entries=%d, error=%v", len(entries), err)
	}
}

func (fixture *headlessClipboardFixture) close(t *testing.T) {
	t.Helper()
	if fixture.closed {
		return
	}
	fixture.closed = true
	groups, err := fixture.groups()
	if err != nil {
		t.Error(err)
	}
	if fixture.command != nil {
		groups = append(groups, fixture.command.Process.Pid)
	}
	for _, group := range groups {
		_ = syscall.Kill(-group, syscall.SIGKILL)
	}
	if fixture.swayPID > 0 {
		_ = syscall.Kill(fixture.swayPID, syscall.SIGKILL)
	}
	if fixture.done != nil {
		select {
		case <-fixture.done:
		case <-time.After(clipboardFixtureTimeout):
			t.Error("private compositor did not stop")
		}
	}
	for _, group := range groups {
		deadline := time.Now().Add(clipboardFixtureTimeout)
		for clipboardFixtureGroupAlive(group) {
			if time.Now().After(deadline) {
				t.Errorf("private clipboard process group %d survived cleanup", group)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := os.RemoveAll(fixture.directory); err != nil {
		t.Error(err)
	}
	if _, err := os.Lstat(fixture.directory); !errors.Is(err, os.ErrNotExist) {
		t.Error("private clipboard runtime and spool directory survived cleanup")
	}
}

func TestWaylandClipboardHeadlessSway(t *testing.T) {
	fixture := startHeadlessClipboardFixture(t)
	values := map[Selection]string{
		SelectionClipboard: "regular 日本語 🙂\nsecond line\n",
		SelectionPrimary:   "primary éèê\n",
	}
	for _, selection := range []Selection{SelectionClipboard, SelectionPrimary} {
		ctx, cancel := context.WithTimeout(context.Background(), clipboardFixtureTimeout)
		err := WriteAllContext(ctx, values[selection], selection)
		cancel() // Successful owners must outlive the write's context.
		if err != nil {
			t.Fatalf("private selection %d write failed: %v", selection, err)
		}
		fixture.requireEmptySpool(t)
	}
	for _, selection := range []Selection{SelectionClipboard, SelectionPrimary, SelectionClipboard} {
		ctx, cancel := context.WithTimeout(context.Background(), clipboardFixtureTimeout)
		text, err := ReadAllContext(ctx, selection)
		cancel()
		if err != nil || text != values[selection] {
			t.Fatalf("private selection %d did not retain exact text after handoff: %v", selection, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipboardFixtureTimeout)
	if err := WriteAllContext(ctx, "", SelectionPrimary); err != nil {
		cancel()
		t.Fatal(err)
	}
	text, err := ReadAllContext(ctx, SelectionPrimary)
	cancel()
	if err != nil || text != "" {
		t.Fatalf("empty private selection did not round trip: %v", err)
	}
	fixture.requireEmptySpool(t)

	// A nonexistent socket remains inside this fixture and cannot reach the
	// developer's compositor or trigger an X11 fallback.
	t.Setenv(envWaylandDisplay, filepath.Join(fixture.runtime, "missing-wayland"))
	t.Setenv(envClipboardFixtureSocket, filepath.Join(fixture.runtime, "missing-wayland"))
	ctx, cancel = context.WithTimeout(context.Background(), clipboardFixtureTimeout)
	err = WriteAllContext(ctx, "fixed failure fixture")
	cancel()
	if err == nil {
		t.Fatal("unavailable private compositor returned success")
	}
	fixture.requireEmptySpool(t)
	t.Setenv(envWaylandDisplay, fixture.socket)
	t.Setenv(envClipboardFixtureSocket, fixture.socket)

	// Stop only the process identified by our private IPC socket. The actual
	// wl-copy then blocks in compositor setup and must be canceled and cleaned.
	if err := syscall.Kill(fixture.swayPID, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !fixture.closed {
			_ = syscall.Kill(fixture.swayPID, syscall.SIGCONT)
		}
	})
	before, err := fixture.groups()
	if err != nil {
		t.Fatal(err)
	}
	cancelCall, result := startCancelableClipboardTestCall(t, func(ctx context.Context) error {
		return WriteAllContext(ctx, "fixed cancellation fixture")
	})
	deadline := time.Now().Add(clipboardFixtureTimeout)
	var blockedGroup int
	for {
		groups, err := fixture.groups()
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) > len(before) {
			blockedGroup = groups[len(groups)-1]
			comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", blockedGroup))
			if strings.TrimSpace(string(comm)) == wlCopy {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("actual private wl-copy did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancelCall()
	requireCanceledClipboardTestCall(t, result)
	if clipboardFixtureGroupAlive(blockedGroup) {
		t.Fatal("canceled private wl-copy survived")
	}
	fixture.requireEmptySpool(t)
	if err := syscall.Kill(fixture.swayPID, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	fixture.close(t)
}
