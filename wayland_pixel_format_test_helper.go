//go:build cgo && linux && wayland && test

package robotgo

/*
#cgo CFLAGS: -DROBOTGO_WAYLAND_TEST
#include <stdint.h>

int robotgo_wayland_pixel_to_bitmap_bgra(uint8_t *dst, const uint8_t *src,
                                         uint32_t format, int using_dmabuf);

#define ROBOTGO_WL_SHM_FORMAT_ARGB8888 0
#define ROBOTGO_WL_SHM_FORMAT_XRGB8888 1
#define ROBOTGO_WL_SHM_FORMAT_ABGR8888 0x34324241
#define ROBOTGO_WL_SHM_FORMAT_XBGR8888 0x34324258
#define ROBOTGO_DRM_FORMAT_ARGB8888 0x34325241
#define ROBOTGO_DRM_FORMAT_XRGB8888 0x34325258
#define ROBOTGO_DRM_FORMAT_ABGR8888 0x34324241
#define ROBOTGO_DRM_FORMAT_XBGR8888 0x34324258
*/
import "C"

const (
	testWaylandFormatARGB = iota
	testWaylandFormatXRGB
	testWaylandFormatABGR
	testWaylandFormatXBGR
	testWaylandFormatWrongBackendARGB
	testWaylandFormatUnsupported
)

func testWaylandPixelToBitmapBGRA(format int, usingDMABuf bool, src [4]byte) ([4]byte, bool) {
	var cFormat C.uint32_t
	switch format {
	case testWaylandFormatARGB:
		if usingDMABuf {
			cFormat = C.ROBOTGO_DRM_FORMAT_ARGB8888
		} else {
			cFormat = C.ROBOTGO_WL_SHM_FORMAT_ARGB8888
		}
	case testWaylandFormatXRGB:
		if usingDMABuf {
			cFormat = C.ROBOTGO_DRM_FORMAT_XRGB8888
		} else {
			cFormat = C.ROBOTGO_WL_SHM_FORMAT_XRGB8888
		}
	case testWaylandFormatABGR:
		if usingDMABuf {
			cFormat = C.ROBOTGO_DRM_FORMAT_ABGR8888
		} else {
			cFormat = C.ROBOTGO_WL_SHM_FORMAT_ABGR8888
		}
	case testWaylandFormatXBGR:
		if usingDMABuf {
			cFormat = C.ROBOTGO_DRM_FORMAT_XBGR8888
		} else {
			cFormat = C.ROBOTGO_WL_SHM_FORMAT_XBGR8888
		}
	case testWaylandFormatWrongBackendARGB:
		if usingDMABuf {
			cFormat = C.ROBOTGO_WL_SHM_FORMAT_ARGB8888
		} else {
			cFormat = C.ROBOTGO_DRM_FORMAT_ARGB8888
		}
	default:
		cFormat = 0xffffffff
	}

	var dst [4]byte
	ok := C.robotgo_wayland_pixel_to_bitmap_bgra(
		(*C.uint8_t)(&dst[0]),
		(*C.uint8_t)(&src[0]),
		cFormat,
		C.int(boolToInt(usingDMABuf)),
	)
	return dst, ok != 0
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
