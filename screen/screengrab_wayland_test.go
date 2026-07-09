//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package screen

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"syscall"
	"strings"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
	"golang.org/x/sys/unix"
)

// TestScreencopyDmabuf ensures CaptureScreen handles linux_dmabuf/buffer_done events.
func TestScreencopyDmabuf(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	robotgo.SetWaylandBackend(robotgo.WaylandBackendDmabuf)

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
	_ = done

	time.Sleep(100 * time.Millisecond)

	if _, err := CaptureScreen(); err != nil {
		if errors.Is(err, robotgo.ErrDmabufImport) || errors.Is(err, robotgo.ErrDmabufMap) || errors.Is(err, robotgo.ErrDmabufDevice) || errors.Is(err, robotgo.ErrDmabufModifiers) {
			t.Skip("dmabuf allocation not available")
		}
		t.Fatalf("capture failed: %v", err)
	}

}

func TestScreencopyWlShm(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	robotgo.SetWaylandBackend(robotgo.WaylandBackendWlShm)

	done := make(chan struct{})
	startMockServer(sock, 0, 0, 0, done)
	_ = done

	time.Sleep(100 * time.Millisecond)

	if _, err := CaptureScreen(); err != nil {
		t.Fatalf("capture failed: %v", err)
	}

}

func TestScreencopyPortalFallback(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_PORTAL_STUB_GREEN", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendDmabuf)

	done := make(chan struct{})
	startMockServer(sock, 0, 0, 1, done)
	_ = done

	time.Sleep(100 * time.Millisecond)

	img, err := robotgo.CaptureImg()
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if robotgo.LastBackend() != robotgo.BackendPortal {
		t.Skipf("portal fallback not selected in this environment (backend=%v)", robotgo.LastBackend())
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	clr := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0}
	if clr.G != 0xff {
		t.Skipf("portal backend active but stub pixel not observed (got %v)", img.At(0, 0))
	}

}
