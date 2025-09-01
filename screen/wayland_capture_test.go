//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package screen

import (
	"errors"
	"runtime"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
)

func TestPortalFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")

	bit, err := robotgo.CaptureScreen()
	if err != nil {
		t.Skipf("CaptureScreen failed: %v", err)
	}
	robotgo.FreeBitmap(bit)
	if robotgo.LastBackend() != robotgo.BackendPortal {
		t.Fatalf("expected portal backend, got %v", robotgo.LastBackend())
	}
}

func TestWaylandDmabufError(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")
	done := make(chan struct{})
	startMockServer(sock, done)
	time.Sleep(100 * time.Millisecond)
	_, err := robotgo.CaptureScreen()
	if !errors.Is(err, robotgo.ErrDmabufOnly) {
		t.Fatalf("expected ErrDmabufOnly, got %v", err)
	}
	<-done
}

func TestBothBackendsFail(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")
	t.Setenv("ROBOTGO_PORTAL_FAIL", "1")
	if _, err := robotgo.CaptureScreen(); err == nil {
		t.Fatalf("expected error when both backends fail")
	} else if errors.Is(err, robotgo.ErrWaylandDisplay) {
		t.Skip("no wayland display")
	}
}
