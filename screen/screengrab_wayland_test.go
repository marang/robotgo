//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package screen

import (
	"context"
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
	mockModeRegistryStall   = 3
	mockModePixels          = 4
	mockModePixelsYInvert   = 5
	mockModePixelsYScale    = 6
	mockModePointer         = 7
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

	bitmap, err := CaptureScreen()
	if err != nil {
		if errors.Is(err, robotgo.ErrDmabufImport) || errors.Is(err, robotgo.ErrDmabufMap) || errors.Is(err, robotgo.ErrDmabufDevice) || errors.Is(err, robotgo.ErrDmabufModifiers) {
			t.Skip("dmabuf allocation not available")
		}
		t.Fatalf("capture failed: %v", err)
	}
	t.Cleanup(func() { robotgo.FreeBitmap(bitmap) })
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

	bitmap, err := CaptureScreen()
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	t.Cleanup(func() { robotgo.FreeBitmap(bitmap) })
	if got := robotgo.LastBackend(); got != robotgo.BackendScreencopy {
		t.Fatalf("backend = %q, want %q", got, robotgo.BackendScreencopy)
	}

}

func TestScreencopyYInvertPreservesLogicalCrop(t *testing.T) {
	for _, test := range []struct {
		name   string
		mode   uint32
		region [4]int
		want   [4]int
	}{
		{"normal full", mockModePixels, [4]int{0, 0, 4, 4}, [4]int{0, 0, 4, 4}},
		{"inverted full", mockModePixelsYInvert, [4]int{0, 0, 4, 4}, [4]int{0, 0, 4, 4}},
		{"inverted upper crop", mockModePixelsYInvert, [4]int{1, 0, 2, 2}, [4]int{1, 0, 2, 2}},
		{"inverted lower crop", mockModePixelsYInvert, [4]int{1, 2, 2, 2}, [4]int{1, 2, 2, 2}},
		{"inverted scaled crop", mockModePixelsYScale, [4]int{0, 1, 1, 1}, [4]int{0, 2, 2, 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			const socket = "robotgo-wl-pixels"
			t.Setenv("XDG_RUNTIME_DIR", dir)
			t.Setenv("WAYLAND_DISPLAY", socket)
			t.Setenv("DISPLAY", "")
			t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
			robotgo.SetWaylandBackend(robotgo.WaylandBackendWlShm)
			t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })
			done := make(chan struct{})
			startMockServerMode(socket, 0, 0, 0, test.mode, done)
			t.Cleanup(func() { cleanupMockServer(t, done) })
			waitForMockServer(t, dir, socket)
			img, err := robotgo.CaptureImg(test.region[:]...)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() != test.want[2] || img.Bounds().Dy() != test.want[3] {
				t.Fatalf("capture size = %v, want %dx%d", img.Bounds(), test.want[2], test.want[3])
			}
			for y := 0; y < test.want[3]; y++ {
				for x := 0; x < test.want[2]; x++ {
					want := color.RGBA{R: uint8(17 + (y+test.want[1])*31), G: uint8(5 + (x+test.want[0])*29), B: 73, A: 0xff}
					if got := color.RGBAModel.Convert(img.At(x, y)); got != want {
						t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestNativeWaylandScrollDirections(t *testing.T) {
	dir := t.TempDir()
	const socket = "robotgo-wl-scroll"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", socket)
	t.Setenv("DISPLAY", "")
	done := make(chan struct{})
	startMockServerMode(socket, 0, 0, 0, mockModePointer, done)
	t.Cleanup(func() {
		robotgo.CloseWaylandInput()
		stopMockServer()
		cleanupMockServer(t, done)
	})
	waitForMockServer(t, dir, socket)
	for index, test := range []struct {
		direction string
		axis      uint32
		discrete  int32
	}{
		{"left", 1, -1}, {"right", 1, 1}, {"up", 0, -1}, {"down", 0, 1},
	} {
		robotgo.ScrollDir(1, test.direction)
		deadline := time.Now().Add(time.Second)
		for mockPointerFrameCount() < uint32(index+1) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if got := mockPointerFrameCount(); got != uint32(index+1) {
			t.Fatalf("%s frames = %d, want %d", test.direction, got, index+1)
		}
		sourceCount, source, value, discreteValue, discrete := mockPointerScroll(test.axis)
		const wheelSource = 0
		const fixedWheelStep = 15 * 256
		if sourceCount != uint32(index+1) || source != wheelSource || value != test.discrete*fixedWheelStep || discreteValue != value || discrete != test.discrete {
			t.Errorf("%s scroll = source(count=%d,value=%d), axis=%d, discrete(value=%d,count=%d); want wheel, value=%d, count=%d", test.direction, sourceCount, source, value, discreteValue, discrete, test.discrete*fixedWheelStep, test.discrete)
		}
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

func TestScreencopyContextDeadlineIsBounded(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl-context-timeout"
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

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	img, err := robotgo.CaptureImgNativeContext(ctx)
	if img != nil {
		t.Fatal("stalled context capture returned an image")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("capture error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > time.Second {
		t.Fatalf("capture did not respect the context deadline: %v", elapsed)
	}
}

func TestScreencopyContextDeadlineBoundsRegistryRoundtrip(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl-registry-timeout"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendWlShm)
	t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })

	done := make(chan struct{})
	startMockServerMode(sock, 0, 0, 0, mockModeRegistryStall, done)
	t.Cleanup(func() {
		cleanupMockServer(t, done)
	})
	waitForMockServer(t, dir, sock)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	img, err := robotgo.CaptureImgNativeContext(ctx)
	if img != nil {
		t.Fatal("stalled registry roundtrip returned an image")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("capture error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > time.Second {
		t.Fatalf("registry roundtrip did not respect the context deadline: %v", elapsed)
	}
}

func TestScreencopyBackendTimeoutPrecedesLongContextDeadline(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-wl-backend-timeout"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("ROBOTGO_DISABLE_PORTAL", "1")
	robotgo.SetWaylandBackend(robotgo.WaylandBackendWlShm)
	t.Cleanup(func() { robotgo.SetWaylandBackend(robotgo.WaylandBackendAuto) })

	done := make(chan struct{})
	startMockServerMode(sock, 0, 0, 0, mockModeStall, done)
	t.Cleanup(func() { cleanupMockServer(t, done) })
	waitForMockServer(t, dir, sock)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	img, err := robotgo.CaptureImgNativeContext(ctx)
	if img != nil {
		t.Fatal("stalled backend capture returned an image")
	}
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("backend timeout was reported as caller deadline: err=%v ctx=%v", err, ctx.Err())
	}
	var timeout interface{ Timeout() bool }
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("capture error = %v, want backend timeout", err)
	}
	if elapsed := time.Since(started); elapsed < 1500*time.Millisecond || elapsed > 2500*time.Millisecond {
		t.Fatalf("backend timeout returned outside its safety window: %v", elapsed)
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
