//go:build cgo && linux && wayland && test

package robotgo

import "testing"

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
