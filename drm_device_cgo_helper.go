//go:build cgo && linux && wayland && test
// +build cgo,linux,wayland,test

package robotgo

/*
#cgo pkg-config: libdrm
#include <stdint.h>
#include <sys/types.h>
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

func drmFindRenderNode(maj, min uint32) int {
	return int(C.findRenderNode(C.uint32_t(maj), C.uint32_t(min)))
}
