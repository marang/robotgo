//go:build windows

package accessibility

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestBoundsFromWindowsRect(t *testing.T) {
	got, err := boundsFromWindowsRect(windows.Rect{Left: -10, Top: 20, Right: 31, Bottom: 65})
	if err != nil {
		t.Fatal(err)
	}
	want := Bounds{X: -10, Y: 20, Width: 41, Height: 45}
	if got == nil || *got != want {
		t.Fatalf("bounds = %+v, want %+v", got, want)
	}

	for _, rectangle := range []windows.Rect{
		{},
		{Left: 10, Top: 20, Right: 10, Bottom: 30},
		{Left: 10, Top: 20, Right: 20, Bottom: 20},
		{Left: 20, Top: 20, Right: 10, Bottom: 30},
	} {
		got, err := boundsFromWindowsRect(rectangle)
		if err != nil || got != nil {
			t.Fatalf("empty bounds for %+v = %+v, %v", rectangle, got, err)
		}
	}
}
