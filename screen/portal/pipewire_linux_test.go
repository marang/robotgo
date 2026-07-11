//go:build linux && cgo && pipewire

package portal

import (
	"image"
	"image/color"
	"reflect"
	"testing"
)

func TestTransformPipeWireFrameAllOrientations(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2, 3))
	value := uint8(1)
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			source.SetRGBA(x, y, color.RGBA{R: value, A: 255})
			value++
		}
	}
	tests := []struct {
		name      string
		transform uint32
		width     int
		values    []uint8
	}{
		{"none", 0, 2, []uint8{1, 2, 3, 4, 5, 6}},
		{"90", 1, 3, []uint8{2, 4, 6, 1, 3, 5}},
		{"180", 2, 2, []uint8{6, 5, 4, 3, 2, 1}},
		{"270", 3, 3, []uint8{5, 3, 1, 6, 4, 2}},
		{"flipped", 4, 2, []uint8{2, 1, 4, 3, 6, 5}},
		{"flipped-90", 5, 3, []uint8{1, 3, 5, 2, 4, 6}},
		{"flipped-180", 6, 2, []uint8{5, 6, 3, 4, 1, 2}},
		{"flipped-270", 7, 3, []uint8{6, 4, 2, 5, 3, 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame, err := transformPipeWireFrame(source, test.transform)
			if err != nil {
				t.Fatalf("transformPipeWireFrame error: %v", err)
			}
			if frame.Bounds().Dx() != test.width || frame.Bounds().Dx()*frame.Bounds().Dy() != len(test.values) {
				t.Fatalf("bounds = %v, width=%d pixels=%d", frame.Bounds(), test.width, len(test.values))
			}
			got := make([]uint8, 0, len(test.values))
			for y := 0; y < frame.Bounds().Dy(); y++ {
				for x := 0; x < frame.Bounds().Dx(); x++ {
					got = append(got, frame.RGBAAt(x, y).R)
				}
			}
			if !reflect.DeepEqual(got, test.values) {
				t.Fatalf("pixels = %v, want %v", got, test.values)
			}
		})
	}
}

func TestTransformPipeWireFrameRejectsUnknownOrientation(t *testing.T) {
	if _, err := transformPipeWireFrame(image.NewRGBA(image.Rect(0, 0, 1, 1)), 8); err == nil {
		t.Fatal("unknown transform unexpectedly accepted")
	}
}
