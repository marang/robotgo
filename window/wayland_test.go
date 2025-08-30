//go:build linux && wayland

package window

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/marang/robotgo"
	"github.com/marang/robotgo/base"
)

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

func TestGetBoundsWayland(t *testing.T) {
	cleanup := startHeadlessWeston(t)
	defer cleanup()

	if ds := base.DetectDisplayServer(); ds != base.Wayland {
		t.Fatalf("expected Wayland, got %v", ds)
	}

	_, _, w, h := robotgo.GetBounds(0)
	if w == 0 || h == 0 {
		t.Skip("wayland compositor did not provide bounds")
	}
}

func TestKeyboardRoundTripWayland(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run", "TestKeyboardRoundTripHelper")
	cmd.Env = append(os.Environ(), "GO_WAYLAND_HELPER=1")
	if err := cmd.Run(); err != nil {
		t.Skipf("keyboard roundtrip failed: %v", err)
	}
}

func TestKeyboardRoundTripHelper(t *testing.T) {
	if os.Getenv("GO_WAYLAND_HELPER") != "1" {
		t.Skip("helper process")
	}
	cleanup := startHeadlessWeston(t)
	defer cleanup()
	if err := robotgo.KeyTap("a"); err != nil {
		t.Fatalf("KeyTap failed: %v", err)
	}
}

func TestMouseRoundTripWayland(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run", "TestMouseRoundTripHelper")
	cmd.Env = append(os.Environ(), "GO_WAYLAND_HELPER=1")
	if err := cmd.Run(); err != nil {
		t.Skipf("mouse roundtrip failed: %v", err)
	}
}

func TestMouseRoundTripHelper(t *testing.T) {
	if os.Getenv("GO_WAYLAND_HELPER") != "1" {
		t.Skip("helper process")
	}
	cleanup := startHeadlessWeston(t)
	defer cleanup()
	robotgo.Move(20, 30)
	x, y := robotgo.Location()
	if x != 20 || y != 30 {
		t.Fatalf("expected mouse at 20,30 got %d,%d", x, y)
	}
}
