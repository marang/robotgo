package robotgo

import "testing"

func TestPrimaryWaylandOutputBoundsUsesStableOutputNameTieBreak(t *testing.T) {
	bounds := []waylandOutputBounds{
		{x: 0, y: 0, w: 800, h: 600, name: 20, named: true},
		{x: 0, y: 0, w: 1024, h: 768, name: 10, named: true},
	}
	got, ok := primaryWaylandOutputBounds(bounds)
	if !ok {
		t.Fatal("primary Wayland output resolution failed")
	}
	if got != (Rect{
		Point: Point{X: 0, Y: 0},
		Size:  Size{W: 1024, H: 768},
	}) {
		t.Fatalf("primary Wayland output = %+v, want lower stable output name", got)
	}
}
