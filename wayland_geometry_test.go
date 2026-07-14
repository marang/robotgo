//go:build cgo && linux && wayland && test

package robotgo

import "testing"

func TestWaylandLogicalCropTransformMatrix(t *testing.T) {
	tests := []struct {
		name      string
		transform int
		capture   [2]int
		want      [4]int
	}{
		{name: "normal", transform: 0, capture: [2]int{200, 100}, want: [4]int{20, 20, 40, 20}},
		{name: "90", transform: 1, capture: [2]int{100, 200}, want: [4]int{20, 140, 20, 40}},
		{name: "180", transform: 2, capture: [2]int{200, 100}, want: [4]int{140, 60, 40, 20}},
		{name: "270", transform: 3, capture: [2]int{100, 200}, want: [4]int{60, 20, 20, 40}},
		{name: "flipped", transform: 4, capture: [2]int{200, 100}, want: [4]int{140, 20, 40, 20}},
		{name: "flipped-90", transform: 5, capture: [2]int{100, 200}, want: [4]int{20, 20, 20, 40}},
		{name: "flipped-180", transform: 6, capture: [2]int{200, 100}, want: [4]int{20, 60, 40, 20}},
		{name: "flipped-270", transform: 7, capture: [2]int{100, 200}, want: [4]int{60, 140, 20, 40}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mapWaylandLogicalRectForTest(test.capture[0], test.capture[1], 100, 50, test.transform, [4]int{10, 10, 20, 10})
			if got != test.want {
				t.Fatalf("mapped crop = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWaylandLogicalCropFractionalScaleEnclosesTouchedPixels(t *testing.T) {
	got := mapWaylandLogicalRectForTest(1920, 1080, 1279, 719, 0, [4]int{1, 1, 1, 1})
	want := [4]int{1, 1, 3, 3}
	if got != want {
		t.Fatalf("mapped crop = %v, want %v", got, want)
	}
}

func TestWaylandLogicalCropFractionalScaleTransformMatrix(t *testing.T) {
	for transform := 0; transform < 8; transform++ {
		t.Run(string(rune('0'+transform)), func(t *testing.T) {
			capture := [2]int{1920, 1080}
			if transform == 1 || transform == 3 || transform == 5 || transform == 7 {
				capture = [2]int{1080, 1920}
			}
			got := mapWaylandLogicalRectForTest(capture[0], capture[1], 1279, 719, transform, [4]int{1, 1, 1, 1})
			if got[2] != 3 || got[3] != 3 {
				t.Fatalf("mapped fractional crop = %v, want 3x3 pixels", got)
			}
			if got[0] < 0 || got[1] < 0 || got[0]+got[2] > capture[0] || got[1]+got[3] > capture[1] {
				t.Fatalf("mapped fractional crop %v exceeds capture %v", got, capture)
			}
		})
	}
}

func TestWaylandLogicalCropClipsOverflowingRegion(t *testing.T) {
	const maxInt32 = int(^uint32(0) >> 1)
	got := mapWaylandLogicalRectForTest(200, 100, 100, 50, 0, [4]int{90, 40, maxInt32, maxInt32})
	want := [4]int{180, 80, 20, 20}
	if got != want {
		t.Fatalf("mapped crop = %v, want %v", got, want)
	}
}

func TestWaylandLogicalCropClipsWithoutShiftingOppositeEdge(t *testing.T) {
	tests := []struct {
		name string
		rect [4]int
		want [4]int
	}{
		{name: "left", rect: [4]int{-10, 0, 30, 10}, want: [4]int{0, 0, 40, 20}},
		{name: "right", rect: [4]int{90, 0, 30, 10}, want: [4]int{180, 0, 20, 20}},
		{name: "top", rect: [4]int{0, -10, 10, 30}, want: [4]int{0, 0, 20, 40}},
		{name: "bottom", rect: [4]int{0, 40, 10, 30}, want: [4]int{0, 80, 20, 20}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := mapWaylandLogicalRectForTest(200, 100, 100, 50, 0, test.rect)
			if got != test.want {
				t.Fatalf("mapped clipped crop = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWaylandMultiOutputSelectionMatrix(t *testing.T) {
	outputs := [][4]int{
		{-1920, 0, 1920, 1080},
		{0, 0, 1280, 720},
		{0, 720, 1280, 1024},
	}
	tests := []struct {
		name    string
		request [4]int
		want    int
	}{
		{name: "origin selects primary instead of registry-first negative output", request: [4]int{0, 0, 1, 1}, want: 1},
		{name: "negative origin selects left output", request: [4]int{-1200, 100, 100, 100}, want: 0},
		{name: "positive vertical origin selects lower output", request: [4]int{100, 900, 100, 100}, want: 2},
		{name: "largest intersection wins across output edge", request: [4]int{-100, 0, 300, 100}, want: 1},
		{name: "disjoint request falls back deterministically", request: [4]int{5000, 5000, 100, 100}, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selectWaylandOutputRectForTest(test.request, outputs); got != test.want {
				t.Fatalf("selected output = %d, want %d", got, test.want)
			}
		})
	}
}
