//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package robotgo

/*
#cgo pkg-config: libdrm
#include <stdint.h>
#include <sys/sysmacros.h>
#include <unistd.h>

int drm_find_render_node(dev_t dev);

static int findRenderNode(uint32_t maj, uint32_t min) {
    dev_t d = makedev(maj, min);
    int fd = drm_find_render_node(d);
    if (fd >= 0) {
        close(fd);
        return 0;
    }
    return -1;
}
*/
import "C"

import (
	"os"
	"path/filepath"
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
			stat := info.Sys().(*unix.Stat_t)
			maj = uint32(unix.Major(uint64(stat.Rdev)))
			min = uint32(unix.Minor(uint64(stat.Rdev)))
			found = true
		}
		return nil
	})
	if !found {
		t.Skip("no drm render node")
	}
	if C.findRenderNode(C.uint32_t(maj), C.uint32_t(min)) != 0 {
		t.Fatalf("expected success opening render node")
	}
}

func TestDrmFindRenderNodeFailure(t *testing.T) {
	if C.findRenderNode(0, 0) == 0 {
		t.Fatalf("expected failure for invalid device")
	}
}
