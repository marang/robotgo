//go:build linux

package robotgo

import (
	"context"
	"errors"
	"image"
	"sync"
	"testing"

	portalpkg "github.com/marang/robotgo/screen/portal"
)

type fakeScreenCastCapture struct {
	mu           sync.Mutex
	frame        image.Image
	streams      []portalpkg.ScreenCastStream
	selected     portalpkg.ScreenCastStream
	restoreToken string
	readyErr     error
	closeErr     error
	closed       int
	captures     int
}

func (c *fakeScreenCastCapture) Ready() error { return c.readyErr }

func (c *fakeScreenCastCapture) Capture(context.Context, int, int, int, int) (image.Image, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.captures++
	return c.frame, nil
}

func (c *fakeScreenCastCapture) Streams() []portalpkg.ScreenCastStream {
	return append([]portalpkg.ScreenCastStream(nil), c.streams...)
}

func (c *fakeScreenCastCapture) SelectedStream() portalpkg.ScreenCastStream { return c.selected }

func (c *fakeScreenCastCapture) RestoreToken() string { return c.restoreToken }

func (c *fakeScreenCastCapture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return c.closeErr
}

func (c *fakeScreenCastCapture) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *fakeScreenCastCapture) captureCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.captures
}

func prepareScreenCastCaptureTest(t *testing.T) {
	t.Helper()
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	t.Setenv("DISPLAY", "")

	oldOpen := screenCastCaptureOpen
	oldCompiled := screenCastCaptureCompiled
	oldDisplayBounds := screenCastDisplayBounds
	oldDisplaysNum := screenCastDisplaysNum
	screenCastCaptureState.Lock()
	oldCapture := screenCastCaptureState.capture
	screenCastCaptureState.capture = nil
	screenCastCaptureState.Unlock()
	screenCastCaptureCompiled = func() bool { return true }
	t.Cleanup(func() {
		_ = CloseScreenCastCapture()
		screenCastCaptureOpen = oldOpen
		screenCastCaptureCompiled = oldCompiled
		screenCastDisplayBounds = oldDisplayBounds
		screenCastDisplaysNum = oldDisplaysNum
		screenCastCaptureState.Lock()
		screenCastCaptureState.capture = oldCapture
		screenCastCaptureState.Unlock()
	})
}

func TestStartScreenCastCaptureReusesMetadataAndReplacesSession(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	first := &fakeScreenCastCapture{}
	second := &fakeScreenCastCapture{
		streams:      []portalpkg.ScreenCastStream{{NodeID: 41}},
		restoreToken: "restore-next",
	}
	opened := 0
	screenCastCaptureOpen = func(_ context.Context, options portalpkg.ScreenCastOptions, streamIndex int) (screenCastFrameCapture, error) {
		if options.Sources != portalpkg.ScreenCastSourceMonitor {
			t.Fatalf("default source mask = %d, want monitor", options.Sources)
		}
		if streamIndex != opened {
			t.Fatalf("stream index = %d, want %d", streamIndex, opened)
		}
		opened++
		if opened == 1 {
			return first, nil
		}
		return second, nil
	}

	if err := StartScreenCastCapture(context.Background(), ScreenCastCaptureOptions{}); err != nil {
		t.Fatalf("first StartScreenCastCapture error: %v", err)
	}
	if err := StartScreenCastCapture(context.Background(), ScreenCastCaptureOptions{}, 1); err != nil {
		t.Fatalf("second StartScreenCastCapture error: %v", err)
	}
	if got := first.closeCount(); got != 1 {
		t.Fatalf("replaced capture close count = %d, want 1", got)
	}
	streams, err := ScreenCastCaptureStreams()
	if err != nil || len(streams) != 1 || streams[0].NodeID != 41 {
		t.Fatalf("ScreenCastCaptureStreams = (%v, %v), want node 41", streams, err)
	}
	streams[0].NodeID = 99
	streamsAgain, _ := ScreenCastCaptureStreams()
	if streamsAgain[0].NodeID != 41 {
		t.Fatal("ScreenCastCaptureStreams exposed mutable session metadata")
	}
	if got := ScreenCastCaptureRestoreToken(); got != "restore-next" {
		t.Fatalf("restore token = %q, want restore-next", got)
	}
}

func TestStartScreenCastCaptureFailurePreservesActiveSession(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	active := &fakeScreenCastCapture{}
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = active
	screenCastCaptureState.Unlock()
	wantErr := errors.New("portal rejected")
	screenCastCaptureOpen = func(context.Context, portalpkg.ScreenCastOptions, int) (screenCastFrameCapture, error) {
		return nil, wantErr
	}

	if err := StartScreenCastCapture(context.Background(), ScreenCastCaptureOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("StartScreenCastCapture error = %v, want %v", err, wantErr)
	}
	if err := ScreenCastCaptureReady(); err != nil {
		t.Fatalf("active capture lost after failed open: %v", err)
	}
	if got := active.closeCount(); got != 0 {
		t.Fatalf("active capture close count = %d, want 0", got)
	}
}

func TestStartScreenCastCaptureCloseFailureLeavesNoActiveSession(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	closeErr := errors.New("close failed")
	active := &fakeScreenCastCapture{closeErr: closeErr}
	replacement := &fakeScreenCastCapture{}
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = active
	screenCastCaptureState.Unlock()
	screenCastCaptureOpen = func(context.Context, portalpkg.ScreenCastOptions, int) (screenCastFrameCapture, error) {
		return replacement, nil
	}

	if err := StartScreenCastCapture(context.Background(), ScreenCastCaptureOptions{}); !errors.Is(err, closeErr) {
		t.Fatalf("StartScreenCastCapture error = %v, want close failure", err)
	}
	if err := ScreenCastCaptureReady(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("ScreenCastCaptureReady error = %v, want ErrNotSupported", err)
	}
	if got := replacement.closeCount(); got != 1 {
		t.Fatalf("replacement close count = %d, want 1", got)
	}
}

func TestCloseScreenCastCaptureCancelsPendingStart(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	entered := make(chan struct{})
	screenCastCaptureOpen = func(ctx context.Context, _ portalpkg.ScreenCastOptions, _ int) (screenCastFrameCapture, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	startErr := make(chan error, 1)
	go func() {
		startErr <- StartScreenCastCapture(context.Background(), ScreenCastCaptureOptions{})
	}()
	<-entered
	if err := CloseScreenCastCapture(); err != nil {
		t.Fatalf("CloseScreenCastCapture error: %v", err)
	}
	if err := <-startErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("StartScreenCastCapture error = %v, want context.Canceled", err)
	}
}

func TestCloseScreenCastCaptureIsIdempotent(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	capture := &fakeScreenCastCapture{}
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = capture
	screenCastCaptureState.Unlock()

	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- CloseScreenCastCapture() }()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("CloseScreenCastCapture error: %v", err)
		}
	}
	if got := capture.closeCount(); got != 1 {
		t.Fatalf("capture close count = %d, want 1", got)
	}
}

func TestCaptureScreenCastUsesActiveSessionAndValidatesRegion(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	want := image.NewRGBA(image.Rect(0, 0, 3, 2))
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = &fakeScreenCastCapture{frame: want}
	screenCastCaptureState.Unlock()

	got, err := CaptureScreenCast(context.Background(), 10, 20, 30, 40)
	if err != nil || got != want {
		t.Fatalf("CaptureScreenCast = (%v, %v), want fake frame", got, err)
	}
	if _, err := CaptureScreenCast(context.Background(), 1, 2); err == nil {
		t.Fatal("CaptureScreenCast accepted an incomplete region")
	}
	if _, err := CaptureScreenCast(context.Background(), 0, 0, 10, 0); err == nil {
		t.Fatal("CaptureScreenCast accepted a non-positive region")
	}
}

func TestCaptureScreenCastDisplayBindsSelectedMonitor(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	want := image.NewRGBA(image.Rect(0, 0, 3, 2))
	capture := &fakeScreenCastCapture{
		frame: want,
		selected: portalpkg.ScreenCastStream{
			SourceType: portalpkg.ScreenCastSourceMonitor,
			Position:   portalpkg.ScreenCastPoint{X: -1920, Y: 0}, HasPosition: true,
			Size: portalpkg.ScreenCastSize{Width: 1920, Height: 1080}, HasSize: true,
		},
	}
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = capture
	screenCastCaptureState.Unlock()
	screenCastDisplaysNum = func() (int, error) { return 2, nil }
	screenCastDisplayBounds = func(displayID int) (int, int, int, int, error) {
		if displayID == 0 {
			return -1920, 0, 1920, 1080, nil
		}
		return 0, 0, 2560, 1440, nil
	}

	got, err := CaptureScreenCastDisplay(context.Background(), 0, -1900, 10, 20, 20)
	if err != nil || got != want || capture.captureCount() != 1 {
		t.Fatalf("CaptureScreenCastDisplay = (%v, %v), captures=%d", got, err, capture.captureCount())
	}
}

func TestCaptureScreenCastDisplayRejectsUnboundStreamsBeforeFrameRead(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	monitor := portalpkg.ScreenCastStream{
		SourceType: portalpkg.ScreenCastSourceMonitor,
		Position:   portalpkg.ScreenCastPoint{X: 0, Y: 0}, HasPosition: true,
		Size: portalpkg.ScreenCastSize{Width: 100, Height: 100}, HasSize: true,
	}
	tests := []struct {
		name     string
		stream   portalpkg.ScreenCastStream
		displays [][4]int
	}{
		{name: "window source", stream: func() portalpkg.ScreenCastStream {
			stream := monitor
			stream.SourceType = portalpkg.ScreenCastSourceWindow
			return stream
		}(), displays: [][4]int{{0, 0, 100, 100}}},
		{name: "missing position", stream: func() portalpkg.ScreenCastStream {
			stream := monitor
			stream.HasPosition = false
			return stream
		}(), displays: [][4]int{{0, 0, 100, 100}}},
		{name: "different monitor", stream: monitor, displays: [][4]int{{100, 0, 100, 100}}},
		{name: "ambiguous mirrored geometry", stream: monitor, displays: [][4]int{{0, 0, 100, 100}, {0, 0, 100, 100}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capture := &fakeScreenCastCapture{frame: image.NewRGBA(image.Rect(0, 0, 1, 1)), selected: test.stream}
			screenCastCaptureState.Lock()
			screenCastCaptureState.capture = capture
			screenCastCaptureState.Unlock()
			screenCastDisplaysNum = func() (int, error) { return len(test.displays), nil }
			screenCastDisplayBounds = func(displayID int) (int, int, int, int, error) {
				bounds := test.displays[displayID]
				return bounds[0], bounds[1], bounds[2], bounds[3], nil
			}

			if _, err := CaptureScreenCastDisplay(context.Background(), 0); !errors.Is(err, ErrNotSupported) {
				t.Fatalf("CaptureScreenCastDisplay error = %v, want ErrNotSupported", err)
			}
			if capture.captureCount() != 0 {
				t.Fatalf("unbound stream read %d frames", capture.captureCount())
			}
		})
	}
}

func TestCaptureScreenCastDisplayUsesValidatedCaptureInstance(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	stream := portalpkg.ScreenCastStream{
		SourceType: portalpkg.ScreenCastSourceMonitor,
		Position:   portalpkg.ScreenCastPoint{}, HasPosition: true,
		Size: portalpkg.ScreenCastSize{Width: 100, Height: 100}, HasSize: true,
	}
	want := image.NewRGBA(image.Rect(0, 0, 2, 2))
	original := &fakeScreenCastCapture{frame: want, selected: stream}
	replacement := &fakeScreenCastCapture{frame: image.NewRGBA(image.Rect(0, 0, 9, 9))}
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = original
	screenCastCaptureState.Unlock()
	screenCastDisplaysNum = func() (int, error) { return 1, nil }
	screenCastDisplayBounds = func(int) (int, int, int, int, error) {
		screenCastCaptureState.Lock()
		screenCastCaptureState.capture = replacement
		screenCastCaptureState.Unlock()
		return 0, 0, 100, 100, nil
	}

	got, err := CaptureScreenCastDisplay(context.Background(), 0)
	if err != nil || got != want || original.captureCount() != 1 || replacement.captureCount() != 0 {
		t.Fatalf(
			"CaptureScreenCastDisplay = (%v, %v), original=%d replacement=%d",
			got, err, original.captureCount(), replacement.captureCount(),
		)
	}
}

func TestScreenCastCaptureReadyReportsClosedPortalSession(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = &fakeScreenCastCapture{readyErr: portalpkg.ErrScreenCastClosed}
	screenCastCaptureState.Unlock()
	if err := ScreenCastCaptureReady(); !errors.Is(err, portalpkg.ErrScreenCastClosed) {
		t.Fatalf("ScreenCastCaptureReady error = %v, want ErrScreenCastClosed", err)
	}
}

func TestStartScreenCastCaptureRequiresWaylandAndPipeWireBuild(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	if err := StartScreenCastCapture(context.Background(), ScreenCastCaptureOptions{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("non-Wayland error = %v, want ErrNotSupported", err)
	}
	t.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
	screenCastCaptureCompiled = func() bool { return false }
	if err := StartScreenCastCapture(context.Background(), ScreenCastCaptureOptions{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("non-PipeWire error = %v, want ErrNotSupported", err)
	}
}

func TestStartScreenCastCaptureRejectsMultipleStreamIndexes(t *testing.T) {
	prepareScreenCastCaptureTest(t)
	if err := StartScreenCastCapture(context.Background(), ScreenCastCaptureOptions{}, 0, 1); err == nil {
		t.Fatal("StartScreenCastCapture accepted multiple stream indexes")
	}
}
