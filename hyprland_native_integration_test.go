//go:build cgo && linux && wayland && hyprlandintegration

package robotgo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	envRequireHyprlandE2E = "ROBOTGO_REQUIRE_HYPRLAND_E2E"
	envHyprlandIsolated   = "ROBOTGO_HYPRLAND_ISOLATED"
	envHyprlandRuntimeDir = "XDG_RUNTIME_DIR"
	envHyprlandDesktop    = "XDG_CURRENT_DESKTOP"
	envHyprlandSession    = "XDG_SESSION_TYPE"
	hyprlandFixtureTitle  = "wev"
	hyprlandFixtureX      = 120
	hyprlandFixtureY      = 80
	hyprlandFixtureWidth  = 480
	hyprlandFixtureHeight = 320
	hyprlandOutputWidth   = 1280
	hyprlandOutputHeight  = 720
	hyprlandFixtureWait   = 5 * time.Second
)

type hyprlandMonitorIdentity struct {
	Name   string  `json:"name"`
	X      int     `json:"x"`
	Y      int     `json:"y"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
}

type hyprlandFixture struct {
	command *exec.Cmd
	cancel  context.CancelFunc
	done    chan error
	once    sync.Once
}

func TestHyprlandWindowRuntime(t *testing.T) {
	requireIsolatedHyprland(t)
	fixture := startHyprlandFixture(t)
	waitForHyprlandFixture(t, fixture.command.Process.Pid)
	waitForHyprlandFixtureGeometry(t)

	title, err := GetTitleE()
	if err != nil {
		t.Fatalf("query Hyprland fixture title: %v", err)
	}
	if title != hyprlandFixtureTitle {
		t.Fatalf("active Hyprland title = %q, want %q", title, hyprlandFixtureTitle)
	}
	pid, err := GetPidE()
	if err != nil {
		t.Fatalf("query Hyprland fixture pid: %v", err)
	}
	if pid != fixture.command.Process.Pid {
		t.Fatalf("active Hyprland pid = %d, want fixture pid %d", pid, fixture.command.Process.Pid)
	}

	var zero Handle
	if handle, activeErr := GetActiveE(); handle != zero ||
		!errors.Is(activeErr, ErrNotSupported) {
		t.Fatalf(
			"active Hyprland handle = %#v, %v; want zero and ErrNotSupported",
			handle,
			activeErr,
		)
	}
	if _, _, _, _, clientErr := GetClientE(0); !errors.Is(clientErr, ErrNotSupported) {
		t.Fatalf("Hyprland client geometry error = %v, want ErrNotSupported", clientErr)
	}
	if _, _, _, _, targetErr := GetBoundsE(os.Getpid()); !errors.Is(targetErr, ErrNotSupported) {
		t.Fatalf("pid-specific Hyprland bounds error = %v, want ErrNotSupported", targetErr)
	}

	capability := GetLinuxCapabilities().Window
	if !capability.Available || capability.Backend != windowBackendHypr {
		t.Fatalf("Hyprland window capability = %+v", capability)
	}
	if err := CloseWindowE(); err != nil {
		t.Fatalf("close self-owned Hyprland fixture: %v", err)
	}
	fixture.close(t, true)
}

func requireIsolatedHyprland(t *testing.T) {
	t.Helper()
	checks := map[string]string{
		envRequireHyprlandE2E: "1",
		envHyprlandIsolated:   "1",
		envHyprlandSession:    "wayland",
	}
	for name, want := range checks {
		if got := os.Getenv(name); got != want {
			t.Fatalf("isolated Hyprland contract requires %s=%q, got %q", name, want, got)
		}
	}
	if os.Getenv(envDisplay) != "" {
		t.Fatal("isolated Hyprland contract requires DISPLAY to be unset")
	}
	if !strings.EqualFold(os.Getenv(envHyprlandDesktop), "hyprland") {
		t.Fatal("isolated Hyprland contract requires the Hyprland desktop identity")
	}
	if os.Getenv(envHyprlandSignature) == "" {
		t.Fatal("isolated Hyprland instance signature is unavailable")
	}
	runtimeDirectory := filepath.Clean(os.Getenv(envHyprlandRuntimeDir))
	if !filepath.IsAbs(runtimeDirectory) || runtimeDirectory == string(filepath.Separator) {
		t.Fatal("isolated Hyprland runtime directory is invalid")
	}
	assertHyprlandSocketInRuntime(t, runtimeDirectory, os.Getenv(envWaylandDisplay))
	for _, devicePath := range []string{"/dev/dri", "/dev/input"} {
		if _, err := os.Lstat(devicePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("isolated Hyprland container exposes %s: %v", devicePath, err)
		}
	}

	var monitors []hyprlandMonitorIdentity
	runHyprlandJSON(t, &monitors, "monitors")
	if len(monitors) != 1 {
		t.Fatalf("isolated Hyprland monitor count = %d, want 1", len(monitors))
	}
	monitor := monitors[0]
	if !strings.HasPrefix(monitor.Name, "HEADLESS-") ||
		monitor.X != 0 || monitor.Y != 0 ||
		monitor.Width != hyprlandOutputWidth ||
		monitor.Height != hyprlandOutputHeight ||
		monitor.Scale != 1 {
		t.Fatalf("isolated Hyprland monitor identity is invalid: %+v", monitor)
	}

	var devices map[string]json.RawMessage
	runHyprlandJSON(t, &devices, "devices")
	for _, category := range []string{"mice", "keyboards", "tablets", "touch", "switches"} {
		raw, ok := devices[category]
		if !ok {
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			t.Fatalf("decode Hyprland %s devices: %v", category, err)
		}
		if len(entries) != 0 {
			t.Fatalf("isolated Hyprland exposes %d %s devices", len(entries), category)
		}
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Fatal("isolated Hyprland did not select the Wayland backend")
	}
}

func startHyprlandFixture(t *testing.T) *hyprlandFixture {
	t.Helper()
	for _, executable := range []string{"stdbuf", "wev"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Fatalf("%s fixture dependency is unavailable: %v", executable, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(
		ctx,
		"stdbuf", "-oL", "-eL",
		"wev",
		"-f", "xdg_surface",
	)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start self-owned Hyprland fixture: %v", err)
	}
	fixture := &hyprlandFixture{
		command: command,
		cancel:  cancel,
		done:    make(chan error, 1),
	}
	go func() { fixture.done <- command.Wait() }()
	t.Cleanup(func() { fixture.close(t, false) })
	return fixture
}

func waitForHyprlandFixture(t *testing.T, fixturePID int) {
	t.Helper()
	deadline := time.Now().Add(hyprlandFixtureWait)
	for time.Now().Before(deadline) {
		info, err := getHyprlandActiveWindow()
		if err == nil && info.Title == hyprlandFixtureTitle &&
			info.PID != nil && *info.PID == fixturePID {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("self-owned Hyprland fixture did not become the active window")
}

func waitForHyprlandFixtureGeometry(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(hyprlandFixtureWait)
	for time.Now().Before(deadline) {
		x, y, width, height, err := GetBoundsE(0)
		if err == nil &&
			x == hyprlandFixtureX && y == hyprlandFixtureY &&
			width == hyprlandFixtureWidth && height == hyprlandFixtureHeight {
			legacyX, legacyY, legacyWidth, legacyHeight := GetBounds(0)
			if legacyX != x || legacyY != y ||
				legacyWidth != width || legacyHeight != height {
				t.Fatalf(
					"legacy Hyprland geometry = %d,%d %dx%d, error API = %d,%d %dx%d",
					legacyX, legacyY, legacyWidth, legacyHeight,
					x, y, width, height,
				)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	info, _ := getHyprlandActiveWindow()
	t.Fatalf(
		"self-owned Hyprland fixture geometry = at=%v size=%v, want %d,%d %dx%d",
		info.At,
		info.Size,
		hyprlandFixtureX,
		hyprlandFixtureY,
		hyprlandFixtureWidth,
		hyprlandFixtureHeight,
	)
}

func runHyprlandJSON(t *testing.T, destination any, request string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, cmdHyprCtl, request, argJSON).Output()
	if err != nil {
		t.Fatalf("bounded Hyprland %s query failed: %v", request, err)
	}
	if err := json.Unmarshal(output, destination); err != nil {
		t.Fatalf("decode sanitized Hyprland %s response: %v", request, err)
	}
}

func assertHyprlandSocketInRuntime(t *testing.T, runtimeDirectory, value string) {
	t.Helper()
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(runtimeDirectory, value)
	}
	clean := filepath.Clean(path)
	relative, err := filepath.Rel(runtimeDirectory, clean)
	if err != nil || relative == "." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("isolated Hyprland socket escapes its private runtime directory")
	}
	info, err := os.Lstat(clean)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatal("isolated Hyprland socket is unavailable")
	}
}

func (fixture *hyprlandFixture) close(t *testing.T, alreadyClosed bool) {
	t.Helper()
	fixture.once.Do(func() {
		if !alreadyClosed {
			select {
			case err := <-fixture.done:
				fixture.cancel()
				if err != nil {
					t.Errorf("self-owned Hyprland fixture exited before cleanup: %v", err)
				}
				return
			default:
			}
			fixture.cancel()
		}
		select {
		case <-fixture.done:
			fixture.cancel()
		case <-time.After(hyprlandFixtureWait):
			fixture.cancel()
			_ = fixture.command.Process.Kill()
			<-fixture.done
			t.Error("self-owned Hyprland fixture did not exit before timeout")
		}
	})
}
