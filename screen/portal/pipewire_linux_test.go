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

func TestNativePipeWirePackedPixelFormats(t *testing.T) {
	tests := []struct {
		name   string
		format uint32
		input  []byte
		want   color.RGBA
	}{
		{"BGRx", pipeWireTestFormatBGRx, []byte{3, 2, 1, 0}, color.RGBA{1, 2, 3, 255}},
		{"BGRA", pipeWireTestFormatBGRA, []byte{3, 2, 1, 4}, color.RGBA{1, 2, 3, 4}},
		{"RGBx", pipeWireTestFormatRGBx, []byte{1, 2, 3, 0}, color.RGBA{1, 2, 3, 255}},
		{"RGBA", pipeWireTestFormatRGBA, []byte{1, 2, 3, 4}, color.RGBA{1, 2, 3, 4}},
		{"BGR", pipeWireTestFormatBGR, []byte{3, 2, 1}, color.RGBA{1, 2, 3, 255}},
		{"RGB", pipeWireTestFormatRGB, []byte{1, 2, 3}, color.RGBA{1, 2, 3, 255}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame, err := pipeWireNativeFrameForTest(test.input, 1, 1, len(test.input), test.format, image.Rectangle{}, false, 0)
			if err != nil {
				t.Fatalf("pipeWireNativeFrameForTest error: %v", err)
			}
			if got := frame.RGBAAt(0, 0); got != test.want {
				t.Fatalf("pixel = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestNativePipeWireCropAndTransformMetadata(t *testing.T) {
	input := []byte{
		1, 0, 0, 255, 2, 0, 0, 255,
		3, 0, 0, 255, 4, 0, 0, 255,
	}
	frame, err := pipeWireNativeFrameForTest(input, 2, 2, 8, pipeWireTestFormatRGBA, image.Rect(1, 0, 2, 2), true, 2)
	if err != nil {
		t.Fatalf("pipeWireNativeFrameForTest error: %v", err)
	}
	if frame.Bounds() != image.Rect(0, 0, 1, 2) {
		t.Fatalf("bounds = %v, want 1x2", frame.Bounds())
	}
	if got := []uint8{frame.RGBAAt(0, 0).R, frame.RGBAAt(0, 1).R}; !reflect.DeepEqual(got, []uint8{4, 2}) {
		t.Fatalf("transformed crop = %v, want [4 2]", got)
	}
}

func TestNativePipeWireNegativeStride(t *testing.T) {
	input := []byte{
		2, 0, 0, 255,
		1, 0, 0, 255,
	}
	frame, err := pipeWireNativeFrameForTest(input, 1, 2, -4, pipeWireTestFormatRGBA, image.Rectangle{}, false, 0)
	if err != nil {
		t.Fatalf("pipeWireNativeFrameForTest error: %v", err)
	}
	if got := []uint8{frame.RGBAAt(0, 0).R, frame.RGBAAt(0, 1).R}; !reflect.DeepEqual(got, []uint8{1, 2}) {
		t.Fatalf("negative-stride pixels = %v, want [1 2]", got)
	}
}
