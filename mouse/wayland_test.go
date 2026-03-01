//go:build linux && wayland && integration

package mouse

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/marang/robotgo"
	"github.com/marang/robotgo/base"
)

func requireDisplay(t *testing.T) {
	t.Helper()
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display available")
	}
}

// startHeadlessWeston launches a headless Wayland compositor using weston.
// It returns a cleanup function to stop the compositor and remove temp files.
func startHeadlessWeston(t *testing.T) func() {
	t.Helper()

	runtimeDir := t.TempDir()
	socket := "robotgo-test"
	cmd := exec.Command("weston", "--backend=headless", "--socket="+socket, "--width=800", "--height=600")
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir)
	if err := cmd.Start(); err != nil {
		t.Skipf("weston not available: %v", err)
	}

	// Wait for socket file to appear indicating compositor is ready.
	sockPath := filepath.Join(runtimeDir, socket)
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Allow compositor time to finish startup.
	time.Sleep(200 * time.Millisecond)

	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("WAYLAND_DISPLAY", socket)
	t.Setenv("DISPLAY", "")

	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func TestMouseRelativeWayland(t *testing.T) {
	requireDisplay(t)
	cmd := exec.Command(os.Args[0], "-test.run", "TestMouseRelativeWaylandHelper")
	cmd.Env = append(os.Environ(), "GO_WAYLAND_HELPER=1")
	if err := cmd.Run(); err != nil {
		t.Skipf("mouse relative roundtrip failed: %v", err)
	}
}

func TestMouseScrollWayland(t *testing.T) {
    requireDisplay(t)
    cmd := exec.Command(os.Args[0], "-test.run", "TestMouseScrollWaylandHelper")
    cmd.Env = append(os.Environ(), "GO_WAYLAND_HELPER=1")
    if err := cmd.Run(); err != nil {
        t.Skipf("mouse scroll roundtrip failed: %v", err)
    }
}

func TestMouseScrollWaylandHelper(t *testing.T) {
    if os.Getenv("GO_WAYLAND_HELPER") != "1" {
        t.Skip("helper process")
    }
    requireDisplay(t)
    cleanup := startHeadlessWeston(t)
    defer cleanup()
    if ds := base.DetectDisplayServer(); ds != base.Wayland {
        t.Fatalf("expected Wayland, got %v", ds)
    }
    // Exercise vertical and horizontal scroll; ensure no crash.
    robotgo.Scroll(0, 3)
    robotgo.Scroll(2, 0)
    robotgo.ScrollDir(5, "down")
    robotgo.ScrollDir(5, "right")
}

func TestMouseRelativeWaylandHelper(t *testing.T) {
	if os.Getenv("GO_WAYLAND_HELPER") != "1" {
		t.Skip("helper process")
	}
	requireDisplay(t)
	cleanup := startHeadlessWeston(t)
	defer cleanup()
	if ds := base.DetectDisplayServer(); ds != base.Wayland {
		t.Fatalf("expected Wayland, got %v", ds)
	}
	robotgo.Move(100, 100)
	robotgo.MoveRelative(10, -5)
	x, y := robotgo.Location()
	if x != 110 || y != 95 {
		t.Fatalf("expected mouse at 110,95 got %d,%d", x, y)
	}
}
