//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package screen

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
	"golang.org/x/sys/unix"
)

const (
	mockModeStall           = 1
	mockModeFailAfterDmabuf = 2
)

func cleanupMockServer(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		return
	case <-time.After(time.Second):
		stopMockServer()
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("mock Wayland server did not stop")
	}
}

func waitForMockServer(t *testing.T, runtimeDir, socket string) {
	t.Helper()
	path := filepath.Join(runtimeDir, socket)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("mock Wayland server did not create socket %q", path)
}

// TestScreencopyDmabuf ensures CaptureScreen handles linux_dmabuf/buffer_done events.
func TestScreencopyDmabuf(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendDmabuf)
	t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })

	var maj, min uint32
	found := false
	filepath.Walk("/dev/dri", func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return nil
		}
		if info.Mode()&os.ModeCharDevice != 0 && strings.HasPrefix(info.Name(), "renderD") {
			switch stat := info.Sys().(type) {
			case *syscall.Stat_t:
				maj = uint32(unix.Major(uint64(stat.Rdev)))
				min = uint32(unix.Minor(uint64(stat.Rdev)))
			default:
				return nil
			}
			found = true
		}
		return nil
	})
	if !found {
		t.Skip("no drm render node")
	}

	done := make(chan struct{})
	startMockServer(sock, maj, min, 0, done)
	t.Cleanup(func() { cleanupMockServer(t, done) })

	waitForMockServer(t, dir, sock)

	if _, err := CaptureScreen(); err != nil {
		if errors.Is(err, robotgo.ErrDmabufImport) || errors.Is(err, robotgo.ErrDmabufMap) || errors.Is(err, robotgo.ErrDmabufDevice) || errors.Is(err, robotgo.ErrDmabufModifiers) {
			t.Skip("dmabuf allocation not available")
		}
		t.Fatalf("capture failed: %v", err)
	}
	if got := robotgo.LastBackend(); got != robotgo.BackendScreencopy {
		t.Fatalf("backend = %q, want %q", got, robotgo.BackendScreencopy)
	}

}

func TestScreencopyWlShm(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendWlShm)
	t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })

	done := make(chan struct{})
	startMockServer(sock, 0, 0, 0, done)
	t.Cleanup(func() { cleanupMockServer(t, done) })

	waitForMockServer(t, dir, sock)

	if _, err := CaptureScreen(); err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if got := robotgo.LastBackend(); got != robotgo.BackendScreencopy {
		t.Fatalf("backend = %q, want %q", got, robotgo.BackendScreencopy)
	}

}

func TestScreencopyBitmapStringHelper(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl-bitmap-string"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendWlShm)
	t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })

	done := make(chan struct{})
	startMockServer(sock, 0, 0, 0, done)
	t.Cleanup(func() { cleanupMockServer(t, done) })
	waitForMockServer(t, dir, sock)

	serialized, err := robotgo.CaptureBitmapStr()
	if err != nil {
		t.Fatalf("CaptureBitmapStr failed: %v", err)
	}
	decoded, err := robotgo.BitmapFromStr(serialized)
	if err != nil {
		t.Fatalf("BitmapFromStr failed: %v", err)
	}
	if decoded.Width <= 0 || decoded.Height <= 0 {
		t.Fatalf("decoded bitmap has invalid dimensions %dx%d", decoded.Width, decoded.Height)
	}
	if got := robotgo.LastBackend(); got != robotgo.BackendScreencopy {
		t.Fatalf("backend = %q, want %q", got, robotgo.BackendScreencopy)
	}
}

func TestScreencopyPortalFallback(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_PORTAL_STUB_GREEN", "1")
	t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendDmabuf)
	t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })

	done := make(chan struct{})
	startMockServer(sock, 0, 0, 1, done)
	t.Cleanup(func() { cleanupMockServer(t, done) })

	waitForMockServer(t, dir, sock)

	img, err := robotgo.CaptureImg()
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if robotgo.LastBackend() != robotgo.BackendPortal {
		t.Fatalf("portal fallback not selected (backend=%v)", robotgo.LastBackend())
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	clr := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0}
	if clr.G != 0xff {
		t.Fatalf("portal backend active but stub pixel not observed (got %v)", img.At(0, 0))
	}

}

func TestScreencopyTimeoutIsBounded(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl-timeout"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendWlShm)
	t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })

	done := make(chan struct{})
	startMockServerMode(sock, 0, 0, 0, mockModeStall, done)
	t.Cleanup(func() {
		cleanupMockServer(t, done)
	})
	waitForMockServer(t, dir, sock)

	started := time.Now()
	bit, err := robotgo.CaptureScreen()
	if bit != nil {
		robotgo.FreeBitmap(bit)
	}
	if err == nil {
		t.Fatal("expected stalled compositor capture to fail")
	}
	if elapsed := time.Since(started); elapsed < 1500*time.Millisecond || elapsed > 4*time.Second {
		t.Fatalf("capture did not respect the configured timeout window: %v", elapsed)
	}
}

func TestScreencopyDmabufFailureDoesNotCloseStdin(t *testing.T) {
	if _, err := unix.FcntlInt(0, unix.F_GETFD, 0); err != nil {
		t.Skipf("stdin is not open in this test environment: %v", err)
	}

	dir := t.TempDir()
	sock := "robotgo-wl-fd"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendDmabuf)
	t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })

	done := make(chan struct{})
	startMockServerMode(sock, 1, 1, 0, mockModeFailAfterDmabuf, done)
	t.Cleanup(func() {
		cleanupMockServer(t, done)
	})
	waitForMockServer(t, dir, sock)

	bit, err := robotgo.CaptureScreen()
	if bit != nil {
		robotgo.FreeBitmap(bit)
	}
	if err == nil {
		t.Fatal("expected compositor failure")
	}
	if _, err := unix.FcntlInt(0, unix.F_GETFD, 0); err != nil {
		t.Fatalf("capture failure closed stdin: %v", err)
	}
}
