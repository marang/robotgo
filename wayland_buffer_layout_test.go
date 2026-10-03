//go:build cgo && linux && wayland && test

package robotgo

import "testing"

func TestWaylandBufferLayoutBounds(t *testing.T) {
	const signedLimit = 0x7fffffff
	for _, test := range []struct {
		name                  string
		width, height, stride uint32
		limit, want           uint64
	}{
		{"tight", 4, 4, 16, signedLimit, 64},
		{"padded", 4, 4, 24, signedLimit, 96},
		{"largest signed row", signedLimit / 4, 1, signedLimit - 3, signedLimit, signedLimit - 3},
		{"largest aligned pool", 1, signedLimit / 4, 4, signedLimit, signedLimit - 3},
		{"exact pool limit", 1, 1, signedLimit, signedLimit, signedLimit},
		{"zero width", 0, 1, 4, signedLimit, 0},
		{"zero height", 1, 0, 4, signedLimit, 0},
		{"zero stride", 1, 1, 0, signedLimit, 0},
		{"unsigned width", 0x80000000, 1, 4, signedLimit, 0},
		{"unsigned height", 1, 0x80000000, 4, signedLimit, 0},
		{"unsigned stride", 1, 1, 0x80000000, signedLimit, 0},
		{"short row", 1024, 2, 4, signedLimit, 0},
		{"signed row overflow", signedLimit/4 + 1, 1, 0x80000000, signedLimit, 0},
		{"pool overflow", 1, 0x20000000, 4, signedLimit, 0},
		{"32 bit product overflow", 1, signedLimit, signedLimit, 0xffffffff, 0},
		{"small limit", 4, 4, 24, 95, 0},
		{"zero limit", 1, 1, 4, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			size, ok := testWaylandBufferLayout(test.width, test.height, test.stride, test.limit)
			if ok != (test.want != 0) || size != test.want {
				t.Fatalf("layout = (%d, %v), want (%d, %v)", size, ok, test.want, test.want != 0)
			}
		})
	}
}
