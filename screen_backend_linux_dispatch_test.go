//go:build linux

package robotgo

import (
	"errors"
	"image"
	"testing"
)

func TestDispatchLinuxDisplayBoundsSelectsSessionBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		server     DisplayServer
		wantBounds image.Rectangle
		wantCalls  [2]int
	}{
		{
			name:       "Wayland",
			server:     DisplayServerWayland,
			wantBounds: image.Rect(-100, 20, 1820, 1100),
			wantCalls:  [2]int{1, 0},
		},
		{
			name:       "X11",
			server:     DisplayServerX11,
			wantBounds: image.Rect(0, 0, 1280, 720),
			wantCalls:  [2]int{0, 1},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var calls [2]int
			bounds, err := dispatchLinuxDisplayBounds(
				test.server,
				2,
				func(index int) (image.Rectangle, error) {
					calls[0]++
					if index != 2 {
						t.Fatalf("Wayland display index = %d, want 2", index)
					}
					return image.Rect(-100, 20, 1820, 1100), nil
				},
				func(index int) (image.Rectangle, error) {
					calls[1]++
					if index != 2 {
						t.Fatalf("X11 display index = %d, want 2", index)
					}
					return image.Rect(0, 0, 1280, 720), nil
				},
			)
			if err != nil {
				t.Fatalf("dispatchLinuxDisplayBounds() error = %v", err)
			}
			if bounds != test.wantBounds {
				t.Fatalf(
					"dispatchLinuxDisplayBounds() = %v, want %v",
					bounds,
					test.wantBounds,
				)
			}
			if calls != test.wantCalls {
				t.Fatalf("backend calls = %v, want %v", calls, test.wantCalls)
			}
		})
	}
}

func TestDispatchLinuxDisplayBoundsRejectsUnavailableBackend(t *testing.T) {
	t.Parallel()

	_, err := dispatchLinuxDisplayBounds(
		DisplayServerWayland,
		0,
		nil,
		func(int) (image.Rectangle, error) {
			t.Fatal("X11 bounds backend called in Wayland session")
			return image.Rectangle{}, nil
		},
	)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("dispatchLinuxDisplayBounds() error = %v, want ErrNotSupported", err)
	}
}

func TestDispatchLinuxCaptureSelectsSessionBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		server    DisplayServer
		wantPixel byte
		wantCalls [2]int
	}{
		{name: "Wayland", server: DisplayServerWayland, wantPixel: 10, wantCalls: [2]int{1, 0}},
		{name: "X11", server: DisplayServerX11, wantPixel: 20, wantCalls: [2]int{0, 1}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var calls [2]int
			capture := func(callIndex int, pixel byte) linuxCaptureBackend {
				return func(x, y, width, height int) (*image.RGBA, error) {
					calls[callIndex]++
					if x != -4 || y != 7 || width != 2 || height != 3 {
						t.Fatalf("capture rectangle = %d,%d %dx%d", x, y, width, height)
					}
					img := image.NewRGBA(image.Rect(0, 0, width, height))
					img.Pix[0] = pixel
					return img, nil
				}
			}
			img, err := dispatchLinuxCapture(
				test.server,
				-4,
				7,
				2,
				3,
				capture(0, 10),
				capture(1, 20),
			)
			if err != nil {
				t.Fatalf("dispatchLinuxCapture() error = %v", err)
			}
			if img.Pix[0] != test.wantPixel {
				t.Fatalf("selected backend pixel = %d, want %d", img.Pix[0], test.wantPixel)
			}
			if calls != test.wantCalls {
				t.Fatalf("backend calls = %v, want %v", calls, test.wantCalls)
			}
		})
	}
}

func TestDispatchLinuxCaptureRejectsUnknownSession(t *testing.T) {
	t.Parallel()

	_, err := dispatchLinuxCapture(
		DisplayServerUnknown,
		0,
		0,
		1,
		1,
		func(int, int, int, int) (*image.RGBA, error) {
			t.Fatal("Wayland capture backend called without a selected session")
			return nil, nil
		},
		func(int, int, int, int) (*image.RGBA, error) {
			t.Fatal("X11 capture backend called without a selected session")
			return nil, nil
		},
	)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("dispatchLinuxCapture() error = %v, want ErrNotSupported", err)
	}
}
