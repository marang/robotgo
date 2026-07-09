//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package robotgo

import (
	"os"
	"path/filepath"
	"syscall"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDrmFindRenderNodeSuccess(t *testing.T) {
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
	if drmFindRenderNode(maj, min) != 0 {
		t.Fatalf("expected success opening render node")
	}
}

func TestDrmFindRenderNodeFailure(t *testing.T) {
	if drmFindRenderNode(0, 0) == 0 {
		t.Fatalf("expected failure for invalid device")
	}
}
