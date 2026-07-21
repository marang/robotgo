//go:build cgo && linux && wayland && test

package robotgo

import (
	"syscall"
	"testing"
)

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

func TestWaylandBoundsPreserveLogicalDesktopGeometry(t *testing.T) {
	outputs := []waylandBoundsOutputForTest{
		{
			position:    [2]int{20, 30},
			mode:        [2]int{1920, 1080},
			scale:       1,
			logicalPos:  [2]int{0, 0},
			logicalSize: [2]int{1280, 720},
			hasLogical:  true,
			name:        30,
		},
		{
			position:    [2]int{-1024, 0},
			mode:        [2]int{1024, 768},
			scale:       1,
			logicalPos:  [2]int{-1024, 0},
			logicalSize: [2]int{1024, 768},
			hasLogical:  true,
			name:        10,
		},
		{
			position:  [2]int{1280, 0},
			mode:      [2]int{2160, 3840},
			transform: 1,
			scale:     2,
			name:      20,
		},
	}

	aggregate, ok := resolveWaylandBoundsForTest(outputs, -1)
	if !ok {
		t.Fatal("aggregate bounds resolution failed")
	}
	if want := [4]int{-1024, 0, 4224, 1080}; aggregate != want {
		t.Fatalf("aggregate bounds = %v, want %v", aggregate, want)
	}

	tests := []struct {
		displayID int
		want      [4]int
	}{
		{displayID: 0, want: [4]int{0, 0, 1280, 720}},
		{displayID: 1, want: [4]int{-1024, 0, 1024, 768}},
		{displayID: 2, want: [4]int{1280, 0, 1920, 1080}},
	}
	for _, test := range tests {
		got, ok := resolveWaylandBoundsForTest(outputs, test.displayID)
		if !ok {
			t.Fatalf("display %d bounds resolution failed", test.displayID)
		}
		if got != test.want {
			t.Fatalf("display %d bounds = %v, want %v", test.displayID, got, test.want)
		}
	}
	if _, ok := resolveWaylandBoundsForTest(outputs, len(outputs)); ok {
		t.Fatal("out-of-range display index unexpectedly resolved")
	}
}

func TestWaylandAbsolutePointerMappingPreservesAggregateOrigin(t *testing.T) {
	bounds := [4]int{-1024, -360, 4224, 1440}
	tests := []struct {
		name  string
		point [2]int
		ok    bool
	}{
		{name: "negative aggregate origin", point: [2]int{-1024, -360}, ok: true},
		{name: "primary origin", point: [2]int{0, 0}, ok: true},
		{name: "last aggregate pixel", point: [2]int{3199, 1079}, ok: true},
		{name: "left of aggregate", point: [2]int{-1025, 0}},
		{name: "above aggregate", point: [2]int{0, -361}},
		{name: "right edge is exclusive", point: [2]int{3200, 0}},
		{name: "bottom edge is exclusive", point: [2]int{0, 1080}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := mapWaylandPointerForTest(test.point, bounds)
			if ok != test.ok {
				t.Fatalf("mapping status = %t, want %t (mapped %v)", ok, test.ok, got)
			}
			if !ok {
				return
			}
			want := [2]uint32{
				uint32((int64(test.point[0]-bounds[0]) * 65535) / int64(bounds[2])),
				uint32((int64(test.point[1]-bounds[1]) * 65535) / int64(bounds[3])),
			}
			if got != want {
				t.Fatalf("mapped point = %v, want %v", got, want)
			}
		})
	}
}

func TestWaylandFlushRetriesTransientBackpressure(t *testing.T) {
	tests := []struct {
		name          string
		flushErrnos   []int
		waitResults   []int
		attempts      int
		wantResult    int
		wantFlushes   int
		wantWaitCalls int
		wantDelivered bool
	}{
		{name: "immediate success", flushErrnos: []int{0}, attempts: 3, wantResult: 0, wantFlushes: 1, wantDelivered: true},
		{name: "retry EAGAIN", flushErrnos: []int{int(syscall.EAGAIN), 0}, waitResults: []int{1}, attempts: 3, wantResult: 0, wantFlushes: 2, wantWaitCalls: 1, wantDelivered: true},
		{name: "bounded queued EAGAIN", flushErrnos: []int{int(syscall.EAGAIN)}, waitResults: []int{0, 0, 0}, attempts: 3, wantResult: 1, wantFlushes: 1, wantWaitCalls: 3},
		{name: "EINTR then writable", flushErrnos: []int{int(syscall.EAGAIN), 0}, waitResults: []int{-int(syscall.EINTR), 1}, attempts: 3, wantResult: 0, wantFlushes: 2, wantWaitCalls: 2, wantDelivered: true},
		{name: "permanent flush failure", flushErrnos: []int{int(syscall.EPIPE)}, attempts: 3, wantResult: -1, wantFlushes: 1},
		{name: "permanent poll failure", flushErrnos: []int{int(syscall.EAGAIN)}, waitResults: []int{-int(syscall.EPIPE)}, attempts: 3, wantResult: -1, wantFlushes: 1, wantWaitCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, flushCalls, waitCalls, delivered := waylandFlushRetryForTest(
				test.flushErrnos, test.waitResults, test.attempts,
			)
			if result != test.wantResult || flushCalls != test.wantFlushes || waitCalls != test.wantWaitCalls || delivered != test.wantDelivered {
				t.Fatalf(
					"flush retry = (result=%d flushes=%d waits=%d delivered=%t), want (%d,%d,%d,%t)",
					result, flushCalls, waitCalls, delivered,
					test.wantResult, test.wantFlushes, test.wantWaitCalls, test.wantDelivered,
				)
			}
		})
	}
}

func TestWaylandBoundsAcceptLogicalGeometryWithoutCoreMode(t *testing.T) {
	outputs := []waylandBoundsOutputForTest{{
		logicalPos:  [2]int{-640, -360},
		logicalSize: [2]int{1280, 720},
		hasLogical:  true,
		name:        1,
	}}
	got, ok := resolveWaylandBoundsForTest(outputs, -1)
	if !ok {
		t.Fatal("logical-only output bounds resolution failed")
	}
	if want := [4]int{-640, -360, 1280, 720}; got != want {
		t.Fatalf("logical-only bounds = %v, want %v", got, want)
	}
}

func TestWaylandBoundsRejectUnrepresentableAggregate(t *testing.T) {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	outputs := []waylandBoundsOutputForTest{
		{
			logicalPos:  [2]int{minInt32, 0},
			logicalSize: [2]int{1, 1},
			hasLogical:  true,
			name:        1,
		},
		{
			logicalPos:  [2]int{maxInt32 - 1, 0},
			logicalSize: [2]int{1, 1},
			hasLogical:  true,
			name:        2,
		},
	}
	if bounds, ok := resolveWaylandBoundsForTest(outputs, -1); ok {
		t.Fatalf("unrepresentable aggregate unexpectedly resolved as %v", bounds)
	}
}

func TestWaylandBoundsFallbackAppliesScaleAndTransforms(t *testing.T) {
	for transform := 0; transform < 8; transform++ {
		t.Run(string(rune('0'+transform)), func(t *testing.T) {
			got, ok := resolveWaylandBoundsForTest(
				[]waylandBoundsOutputForTest{{
					position:  [2]int{-10, 20},
					mode:      [2]int{2400, 1600},
					transform: transform,
					scale:     2,
					name:      1,
				}},
				-1,
			)
			if !ok {
				t.Fatal("fallback output bounds resolution failed")
			}
			want := [4]int{-10, 20, 1200, 800}
			if transform == 1 || transform == 3 || transform == 5 || transform == 7 {
				want = [4]int{-10, 20, 800, 1200}
			}
			if got != want {
				t.Fatalf("transform %d bounds = %v, want %v", transform, got, want)
			}
		})
	}
}

func TestWaylandExplicitDisplayIndexMatchesBoundsOrder(t *testing.T) {
	outputs := [][5]int{
		{-1920, 0, 1920, 1080, 58},
		{0, 0, 1280, 720, 99},
		{0, 720, 1280, 1024, 12},
	}
	tests := []struct {
		displayID int
		wantName  int
	}{
		{displayID: 0, wantName: 99},
		{displayID: 1, wantName: 58},
		{displayID: 2, wantName: 12},
		{displayID: 3, wantName: -1},
	}
	for _, test := range tests {
		if got := stableWaylandOutputNameForTest(outputs, test.displayID); got != test.wantName {
			t.Fatalf(
				"display %d selected registry output %d, want %d",
				test.displayID,
				got,
				test.wantName,
			)
		}
	}
}
