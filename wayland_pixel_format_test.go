//go:build cgo && linux && wayland && test

package robotgo

import (
	"fmt"
	"testing"
)

func TestWaylandPixelFormatsUseBitmapBGRA(t *testing.T) {
	formats := []struct {
		name   string
		format int
		src    [4]byte
		want   [4]byte
		ok     bool
	}{
		{name: "ARGB8888", format: testWaylandFormatARGB, src: [4]byte{0x65, 0x43, 0x21, 0x7f}, want: [4]byte{0x65, 0x43, 0x21, 0x7f}, ok: true},
		{name: "XRGB8888", format: testWaylandFormatXRGB, src: [4]byte{0x65, 0x43, 0x21, 0x00}, want: [4]byte{0x65, 0x43, 0x21, 0xff}, ok: true},
		{name: "ABGR8888", format: testWaylandFormatABGR, src: [4]byte{0x21, 0x43, 0x65, 0x7f}, want: [4]byte{0x65, 0x43, 0x21, 0x7f}, ok: true},
		{name: "XBGR8888", format: testWaylandFormatXBGR, src: [4]byte{0x21, 0x43, 0x65, 0x00}, want: [4]byte{0x65, 0x43, 0x21, 0xff}, ok: true},
		{name: "wrong backend ARGB8888", format: testWaylandFormatWrongBackendARGB, src: [4]byte{1, 2, 3, 4}, ok: false},
		{name: "unsupported", format: testWaylandFormatUnsupported, src: [4]byte{1, 2, 3, 4}, ok: false},
	}

	for _, backend := range []struct {
		name        string
		usingDMABuf bool
	}{
		{name: "wl_shm"},
		{name: "dmabuf", usingDMABuf: true},
	} {
		t.Run(backend.name, func(t *testing.T) {
			for _, format := range formats {
				t.Run(format.name, func(t *testing.T) {
					got, ok := testWaylandPixelToBitmapBGRA(format.format, backend.usingDMABuf, format.src)
					if ok != format.ok {
						t.Fatalf("supported = %t, want %t", ok, format.ok)
					}
					if got != format.want {
						t.Fatalf("pixel = %#v, want %#v", got, format.want)
					}
				})
			}
		})
	}
}

func TestWaylandPixelRowsHonorYInvert(t *testing.T) {
	const width, height, stride = 3, 4, 16
	const yInvert = uint32(1)
	for _, dmabuf := range []bool{false, true} {
		for _, format := range []int{testWaylandFormatARGB, testWaylandFormatXRGB, testWaylandFormatABGR, testWaylandFormatXBGR} {
			for _, flags := range []uint32{0, yInvert, 4, yInvert | 4} {
				t.Run(fmt.Sprintf("dmabuf=%t/format=%d/flags=%d", dmabuf, format, flags), func(t *testing.T) {
					pixels := make([]byte, stride*height)
					for index := range pixels {
						pixels[index] = 0xee
					}
					for y := 0; y < height; y++ {
						row := y
						if flags&yInvert != 0 {
							row = height - 1 - y
						}
						for x := 0; x < width; x++ {
							pixel := [4]byte{byte(71 + y*11), byte(13 + x*19), byte(29 + y*23 + x), 0x7f}
							if format == testWaylandFormatABGR || format == testWaylandFormatXBGR {
								pixel[0], pixel[2] = pixel[2], pixel[0]
							}
							copy(pixels[row*stride+x*4:], pixel[:])
						}
					}
					for _, crop := range [][4]int{{0, 0, width, height}, {1, 0, 2, 2}, {0, 2, 2, 2}} {
						for y := 0; y < crop[3]; y++ {
							for x := 0; x < crop[2]; x++ {
								sx, sy := x+crop[0], y+crop[1]
								want := [4]byte{byte(71 + sy*11), byte(13 + sx*19), byte(29 + sy*23 + sx), 0x7f}
								if format == testWaylandFormatXRGB || format == testWaylandFormatXBGR {
									want[3] = 0xff
								}
								got, ok := testWaylandBufferPixelToBitmapBGRA(format, dmabuf, pixels, height, stride, sx, sy, flags)
								if !ok || got != want {
									t.Fatalf("crop %v pixel (%d,%d) = %v supported=%t, want %v", crop, x, y, got, ok, want)
								}
							}
						}
					}
				})
			}
		}
	}
}
