package portal

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestOpenPipeWireCaptureRejectsNegativeStreamBeforePortal(t *testing.T) {
	_, err := OpenPipeWireCapture(context.Background(), ScreenCastOptions{Sources: ScreenCastSourceMonitor}, -1)
	if !errors.Is(err, ErrScreenCastNoStreams) {
		t.Fatalf("OpenPipeWireCapture error = %v, want ErrScreenCastNoStreams", err)
	}
}

func TestOpenPipeWireCaptureRejectsCursorMetadataBeforePortal(t *testing.T) {
	_, err := OpenPipeWireCapture(context.Background(), ScreenCastOptions{
		Sources: ScreenCastSourceMonitor,
		Cursor:  ScreenCastCursorMetadata,
	}, 0)
	if !errors.Is(err, ErrPipeWireUnavailable) || !strings.Contains(err.Error(), "cursor metadata") {
		t.Fatalf("OpenPipeWireCapture error = %v, want explicit cursor metadata error", err)
	}
}

type fakePipeWireFrameSource struct {
	frameImage *image.RGBA
	events     *[]string
	readyErr   error
}

func (s *fakePipeWireFrameSource) ready() error { return s.readyErr }

type blockingPipeWireFrameSource struct {
	entered     chan struct{}
	interrupted chan struct{}
	once        sync.Once
}

func (*blockingPipeWireFrameSource) ready() error { return nil }

func (s *blockingPipeWireFrameSource) frame(context.Context) (*image.RGBA, error) {
	close(s.entered)
	<-s.interrupted
	return nil, ErrScreenCastClosed
}

func (s *blockingPipeWireFrameSource) interrupt() {
	s.once.Do(func() { close(s.interrupted) })
}

func (*blockingPipeWireFrameSource) close() error { return nil }

func (s *fakePipeWireFrameSource) frame(context.Context) (*image.RGBA, error) {
	return s.frameImage, nil
}

func (*fakePipeWireFrameSource) interrupt() {}

func (s *fakePipeWireFrameSource) close() error {
	*s.events = append(*s.events, "backend")
	return nil
}

type fakePipeWireScreenCast struct {
	stream ScreenCastStream
	events *[]string
	closed chan struct{}
}

var fakePipeWireSessionOpen = make(chan struct{})

func (s *fakePipeWireScreenCast) Streams() []ScreenCastStream { return []ScreenCastStream{s.stream} }
func (*fakePipeWireScreenCast) RestoreToken() string          { return "restore-next" }
func (*fakePipeWireScreenCast) PipeWireFile() (*os.File, error) {
	return nil, ErrPipeWireUnavailable
}
func (s *fakePipeWireScreenCast) Closed() <-chan struct{} {
	if s.closed == nil {
		return fakePipeWireSessionOpen
	}
	return s.closed
}
func (s *fakePipeWireScreenCast) Close() error {
	*s.events = append(*s.events, "session")
	return nil
}

func TestPipeWireCaptureMapsLogicalFractionalScaleRegion(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 300, 200))
	stream := ScreenCastStream{
		Position: ScreenCastPoint{X: -150, Y: 20}, HasPosition: true,
		Size: ScreenCastSize{Width: 200, Height: 100}, HasSize: true,
	}
	events := []string{}
	capture := &PipeWireCapture{
		session: &fakePipeWireScreenCast{stream: stream, events: &events}, stream: stream,
		backend: &fakePipeWireFrameSource{frameImage: frame, events: &events},
	}
	if err := capture.Ready(); err != nil {
		t.Fatalf("Ready error: %v", err)
	}
	img, err := capture.Capture(context.Background(), -50, 45, 50, 25)
	if err != nil {
		t.Fatalf("Capture error: %v", err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 75, 50) {
		t.Fatalf("scaled crop bounds = %v, want 75x50", got)
	}
	if capture.RestoreToken() != "restore-next" || len(capture.Streams()) != 1 ||
		!reflect.DeepEqual(capture.SelectedStream(), stream) {
		t.Fatal("capture session metadata was not retained")
	}
	if err := capture.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"backend", "session"}) {
		t.Fatalf("cleanup order = %v, want backend then session", events)
	}
	if _, err := capture.Capture(context.Background(), 0, 0, 0, 0); err != ErrScreenCastClosed {
		t.Fatalf("Capture after close = %v, want ErrScreenCastClosed", err)
	}
	if err := capture.Ready(); err != ErrScreenCastClosed {
		t.Fatalf("Ready after close = %v, want ErrScreenCastClosed", err)
	}
}

func TestPipeWireCaptureReadyDetectsPortalClosure(t *testing.T) {
	done := make(chan struct{})
	session := &fakePipeWireScreenCast{closed: done}
	capture := &PipeWireCapture{session: session, backend: &fakePipeWireFrameSource{events: &[]string{}}}
	close(done)
	if err := capture.Ready(); err != ErrScreenCastClosed {
		t.Fatalf("Ready = %v, want ErrScreenCastClosed", err)
	}
}

func TestPipeWireCaptureReadyReportsBackendFailure(t *testing.T) {
	wantErr := errors.New("stream failed")
	capture := &PipeWireCapture{
		session: &fakePipeWireScreenCast{},
		backend: &fakePipeWireFrameSource{readyErr: wantErr},
	}
	if err := capture.Ready(); !errors.Is(err, wantErr) {
		t.Fatalf("Ready error = %v, want %v", err, wantErr)
	}
}

func TestPipeWireCaptureCloseInterruptsInFlightFrame(t *testing.T) {
	events := []string{}
	backend := &blockingPipeWireFrameSource{entered: make(chan struct{}), interrupted: make(chan struct{})}
	capture := &PipeWireCapture{session: &fakePipeWireScreenCast{events: &events}, backend: backend}
	captureErr := make(chan error, 1)
	go func() {
		_, err := capture.Capture(context.Background(), 0, 0, 0, 0)
		captureErr <- err
	}()
	<-backend.entered
	if err := capture.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := <-captureErr; !errors.Is(err, ErrScreenCastClosed) {
		t.Fatalf("Capture error = %v, want ErrScreenCastClosed", err)
	}
}

func TestCropPipeWireFrameRejectsDisjointLogicalRegion(t *testing.T) {
	stream := ScreenCastStream{Size: ScreenCastSize{Width: 100, Height: 100}, HasSize: true}
	if _, err := cropPipeWireFrame(image.NewRGBA(image.Rect(0, 0, 200, 200)), stream, 200, 0, 10, 10); err == nil {
		t.Fatal("disjoint logical region unexpectedly accepted")
	}
}

func TestCropPipeWireFrameRejectsInvalidLogicalSize(t *testing.T) {
	stream := ScreenCastStream{Size: ScreenCastSize{}, HasSize: true}
	if _, err := cropPipeWireFrame(image.NewRGBA(image.Rect(0, 0, 2, 2)), stream, 0, 0, 1, 1); err == nil {
		t.Fatal("zero logical stream size unexpectedly accepted")
	}
}

func TestCropPipeWireFrameRejectsPartialOrNegativeSize(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for _, size := range [][2]int{{1, 0}, {0, 1}, {-1, 1}, {1, -1}} {
		if _, err := cropPipeWireFrame(frame, ScreenCastStream{}, 0, 0, size[0], size[1]); err == nil {
			t.Fatalf("size %v unexpectedly accepted", size)
		}
	}
}

func TestCropPipeWireFrameOutputGeometryMatrix(t *testing.T) {
	tests := []struct {
		name           string
		frame          image.Rectangle
		stream         ScreenCastStream
		region         image.Rectangle
		want           image.Rectangle
		wantFirstPixel image.Point
	}{
		{
			name:  "negative-origin fractional-scale output",
			frame: image.Rect(0, 0, 2560, 1440),
			stream: ScreenCastStream{
				Position: ScreenCastPoint{X: -1920}, HasPosition: true,
				Size: ScreenCastSize{Width: 1920, Height: 1080}, HasSize: true,
			},
			region: image.Rect(-1440, 270, -960, 810),
			want:   image.Rect(0, 0, 640, 720),
		},
		{
			name:  "positive-origin output with negative vertical origin",
			frame: image.Rect(0, 0, 1920, 1080),
			stream: ScreenCastStream{
				Position: ScreenCastPoint{X: 1280, Y: -720}, HasPosition: true,
				Size: ScreenCastSize{Width: 1280, Height: 720}, HasSize: true,
			},
			region: image.Rect(1600, -600, 1920, -360),
			want:   image.Rect(0, 0, 480, 360),
		},
		{
			name:  "left edge is clipped to selected output",
			frame: image.Rect(0, 0, 1500, 1000),
			stream: ScreenCastStream{
				Position: ScreenCastPoint{X: -1000, Y: 100}, HasPosition: true,
				Size: ScreenCastSize{Width: 1000, Height: 500}, HasSize: true,
			},
			region: image.Rect(-1100, 200, -800, 300),
			want:   image.Rect(0, 0, 300, 200),
		},
		{
			name:  "fractional rounding encloses every touched pixel",
			frame: image.Rect(0, 0, 1920, 1080),
			stream: ScreenCastStream{
				Size: ScreenCastSize{Width: 1279, Height: 719}, HasSize: true,
			},
			region: image.Rect(1, 1, 2, 2),
			want:   image.Rect(0, 0, 3, 3),
		},
		{
			name:           "non-zero frame origin is preserved while copying",
			frame:          image.Rect(10, 20, 14, 24),
			stream:         ScreenCastStream{},
			region:         image.Rect(1, 1, 3, 3),
			want:           image.Rect(0, 0, 2, 2),
			wantFirstPixel: image.Pt(11, 21),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame := image.NewRGBA(test.frame)
			for y := test.frame.Min.Y; y < test.frame.Max.Y; y++ {
				for x := test.frame.Min.X; x < test.frame.Max.X; x++ {
					frame.SetRGBA(x, y, coordinateColor(x, y))
				}
			}
			got, err := cropPipeWireFrame(frame, test.stream, test.region.Min.X, test.region.Min.Y, test.region.Dx(), test.region.Dy())
			if err != nil {
				t.Fatalf("cropPipeWireFrame error: %v", err)
			}
			if got.Bounds() != test.want {
				t.Fatalf("crop bounds = %v, want %v", got.Bounds(), test.want)
			}
			if test.wantFirstPixel != (image.Point{}) {
				wantColor := coordinateColor(test.wantFirstPixel.X, test.wantFirstPixel.Y)
				if gotColor := got.At(0, 0); gotColor != wantColor {
					t.Fatalf("first pixel = %v, want source pixel %v at %v", gotColor, wantColor, test.wantFirstPixel)
				}
			}
		})
	}
}

func TestCropPipeWireFrameRejectsOverflowingRegion(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
	stream := ScreenCastStream{Size: ScreenCastSize{Width: 2, Height: 2}, HasSize: true}
	if _, err := cropPipeWireFrame(frame, stream, math.MaxInt, 0, math.MaxInt, 1); err == nil {
		t.Fatal("overflowing disjoint region unexpectedly accepted")
	}
}

func coordinateColor(x, y int) color.RGBA {
	return color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 0xff}
}
