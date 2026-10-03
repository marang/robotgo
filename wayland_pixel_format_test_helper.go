//go:build cgo && linux && wayland && test

package robotgo

/*
#cgo CFLAGS: -DROBOTGO_WAYLAND_TEST
#include <stdint.h>
#include <stddef.h>

int robotgo_wayland_buffer_layout(uint32_t width, uint32_t height,
                                  uint32_t stride, size_t limit, size_t *size);

int robotgo_wayland_pixel_to_bitmap_bgra(uint8_t *dst, const uint8_t *src,
                                         uint32_t format, int using_dmabuf);
int robotgo_wayland_buffer_pixel_to_bitmap_bgra(uint8_t *dst, const uint8_t *src,
                                                uint32_t format, int using_dmabuf,
                                                int height, int stride, int x,
                                                int y, uint32_t flags);

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

func testWaylandBufferLayout(width, height, stride uint32, limit uint64) (uint64, bool) {
	var size C.size_t
	ok := C.robotgo_wayland_buffer_layout(C.uint32_t(width), C.uint32_t(height), C.uint32_t(stride), C.size_t(limit), &size)
	return uint64(size), ok != 0
}

const (
	testWaylandFormatARGB = iota
	testWaylandFormatXRGB
	testWaylandFormatABGR
	testWaylandFormatXBGR
	testWaylandFormatWrongBackendARGB
	testWaylandFormatUnsupported
)

func testWaylandPixelToBitmapBGRA(format int, usingDMABuf bool, src [4]byte) ([4]byte, bool) {
	return testWaylandBufferPixelToBitmapBGRA(format, usingDMABuf, src[:], 1, 4, 0, 0, 0)
}

func testWaylandBufferPixelToBitmapBGRA(format int, usingDMABuf bool, src []byte, height, stride, x, y int, flags uint32) ([4]byte, bool) {
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
	ok := C.robotgo_wayland_buffer_pixel_to_bitmap_bgra(
		(*C.uint8_t)(&dst[0]),
		(*C.uint8_t)(&src[0]),
		cFormat,
		C.int(boolToInt(usingDMABuf)),
		C.int(height), C.int(stride), C.int(x), C.int(y), C.uint32_t(flags),
	)
	return dst, ok != 0
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
