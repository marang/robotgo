package robotgo

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"

	portalpkg "github.com/marang/robotgo/screen/portal"
)

type (
	ScreenCastCaptureOptions = portalpkg.ScreenCastOptions
	ScreenCastCaptureSource  = portalpkg.ScreenCastSource
	ScreenCastCaptureCursor  = portalpkg.ScreenCastCursor
	ScreenCastCapturePersist = portalpkg.ScreenCastPersist
	ScreenCastCaptureStream  = portalpkg.ScreenCastStream
)

const (
	ScreenCastSourceMonitor   = portalpkg.ScreenCastSourceMonitor
	ScreenCastSourceWindow    = portalpkg.ScreenCastSourceWindow
	ScreenCastSourceVirtual   = portalpkg.ScreenCastSourceVirtual
	ScreenCastCursorHidden    = portalpkg.ScreenCastCursorHidden
	ScreenCastCursorEmbedded  = portalpkg.ScreenCastCursorEmbedded
	ScreenCastCursorMetadata  = portalpkg.ScreenCastCursorMetadata
	ScreenCastPersistNone     = portalpkg.ScreenCastPersistNone
	ScreenCastPersistApp      = portalpkg.ScreenCastPersistApplication
	ScreenCastPersistExplicit = portalpkg.ScreenCastPersistExplicit
)

type screenCastFrameCapture interface {
	Ready() error
	Capture(context.Context, int, int, int, int) (image.Image, error)
	Streams() []portalpkg.ScreenCastStream
	SelectedStream() portalpkg.ScreenCastStream
	RestoreToken() string
	Close() error
}

var (
	screenCastCaptureOpen = func(ctx context.Context, options portalpkg.ScreenCastOptions, streamIndex int) (screenCastFrameCapture, error) {
		return portalpkg.OpenPipeWireCapture(ctx, options, streamIndex)
	}
	screenCastCaptureCompiled  = portalpkg.PipeWireCaptureCompiled
	screenCastDisplayBounds    = GetDisplayBoundsE
	screenCastDisplaysNum      = DisplaysNumE
	screenCastCaptureOperation sync.Mutex
	screenCastCapturePending   struct {
		sync.Mutex
		start   context.CancelFunc
		closing int
	}
	screenCastCaptureState struct {
		sync.RWMutex
		capture screenCastFrameCapture
	}
)

// StartScreenCastCapture opens one consent-aware ScreenCast/PipeWire session.
// CaptureScreen reuses it after native Wayland screencopy is unavailable, or
// immediately when ROBOTGO_WAYLAND_BACKEND=screencast is selected.
func StartScreenCastCapture(ctx context.Context, options ScreenCastCaptureOptions, streamIndex ...int) error {
	if runtime.GOOS != "linux" || DetectDisplayServer() != DisplayServerWayland {
		return fmt.Errorf("%w: ScreenCast capture requires a Linux Wayland session", ErrNotSupported)
	}
	if !screenCastCaptureCompiled() {
		return fmt.Errorf("%w: build with -tags pipewire and install libpipewire development files", ErrNotSupported)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(streamIndex) > 1 {
		return errors.New("ScreenCast capture accepts at most one stream index")
	}
	if options.Sources == 0 {
		options.Sources = ScreenCastSourceMonitor
	}
	selected := 0
	if len(streamIndex) > 0 {
		selected = streamIndex[0]
	}
	screenCastCaptureOperation.Lock()
	defer screenCastCaptureOperation.Unlock()

	openCtx, cancel := context.WithCancel(ctx)
	screenCastCapturePending.Lock()
	if screenCastCapturePending.closing != 0 {
		screenCastCapturePending.Unlock()
		cancel()
		return fmt.Errorf("ScreenCast capture is closing: %w", context.Canceled)
	}
	screenCastCapturePending.start = cancel
	screenCastCapturePending.Unlock()
	defer func() {
		screenCastCapturePending.Lock()
		screenCastCapturePending.start = nil
		screenCastCapturePending.Unlock()
		cancel()
	}()

	capture, err := screenCastCaptureOpen(openCtx, portalpkg.ScreenCastOptions(options), selected)
	if err != nil {
		return err
	}
	screenCastCaptureState.RLock()
	previous := screenCastCaptureState.capture
	screenCastCaptureState.RUnlock()
	if previous != nil {
		if err := previous.Close(); err != nil {
			screenCastCaptureState.Lock()
			if screenCastCaptureState.capture == previous {
				screenCastCaptureState.capture = nil
			}
			screenCastCaptureState.Unlock()
			return errors.Join(fmt.Errorf("close previous ScreenCast capture: %w", err), capture.Close())
		}
	}
	screenCastCaptureState.Lock()
	screenCastCaptureState.capture = capture
	screenCastCaptureState.Unlock()
	return nil
}

// ScreenCastCaptureReady reports whether a reusable PipeWire capture is active.
func ScreenCastCaptureReady() error {
	screenCastCaptureState.RLock()
	capture := screenCastCaptureState.capture
	screenCastCaptureState.RUnlock()
	if capture == nil {
		return fmt.Errorf("%w: call StartScreenCastCapture first", ErrNotSupported)
	}
	return capture.Ready()
}

// ScreenCastCaptureStreams returns selected portal stream metadata.
func ScreenCastCaptureStreams() ([]ScreenCastCaptureStream, error) {
	screenCastCaptureState.RLock()
	defer screenCastCaptureState.RUnlock()
	if screenCastCaptureState.capture == nil {
		return nil, fmt.Errorf("%w: call StartScreenCastCapture first", ErrNotSupported)
	}
	return screenCastCaptureState.capture.Streams(), nil
}

// ScreenCastCaptureRestoreToken returns the latest single-use restore token.
func ScreenCastCaptureRestoreToken() string {
	screenCastCaptureState.RLock()
	defer screenCastCaptureState.RUnlock()
	if screenCastCaptureState.capture == nil {
		return ""
	}
	return screenCastCaptureState.capture.RestoreToken()
}

// CaptureScreenCast returns the next frame from the active persistent session.
// With no region it returns the complete selected stream. A region must be
// supplied as x, y, width, height in logical compositor coordinates.
func CaptureScreenCast(ctx context.Context, region ...int) (image.Image, error) {
	if len(region) != 0 && len(region) != 4 {
		return nil, errors.New("ScreenCast capture region must be empty or x, y, width, height")
	}
	if len(region) == 4 && (region[2] <= 0 || region[3] <= 0) {
		return nil, fmt.Errorf("ScreenCast capture region has invalid size %dx%d", region[2], region[3])
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(region) == 0 {
		return captureViaScreenCast(ctx, 0, 0, 0, 0)
	}
	return captureViaScreenCast(ctx, region[0], region[1], region[2], region[3])
}

// CaptureScreenCastDisplay returns a region only when the active selected
// stream is an unambiguous monitor match for displayID. Window, virtual,
// metadata-incomplete, mismatched, and geometrically ambiguous streams fail
// closed before any frame is read.
func CaptureScreenCastDisplay(ctx context.Context, displayID int, region ...int) (image.Image, error) {
	if displayID < 0 {
		return nil, fmt.Errorf("ScreenCast capture requires a non-negative display ID")
	}
	if len(region) != 0 && len(region) != 4 {
		return nil, errors.New("ScreenCast capture region must be empty or x, y, width, height")
	}
	if len(region) == 4 && (region[2] <= 0 || region[3] <= 0) {
		return nil, fmt.Errorf("ScreenCast capture region has invalid size %dx%d", region[2], region[3])
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	screenCastCaptureState.RLock()
	capture := screenCastCaptureState.capture
	screenCastCaptureState.RUnlock()
	if capture == nil {
		return nil, fmt.Errorf("%w: no active ScreenCast capture", ErrNotSupported)
	}
	if err := validateScreenCastDisplay(capture.SelectedStream(), displayID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(region) == 0 {
		return capture.Capture(ctx, 0, 0, 0, 0)
	}
	return capture.Capture(ctx, region[0], region[1], region[2], region[3])
}

func validateScreenCastDisplay(stream portalpkg.ScreenCastStream, displayID int) error {
	if stream.SourceType != portalpkg.ScreenCastSourceMonitor {
		return fmt.Errorf("%w: selected ScreenCast stream is not a monitor", ErrNotSupported)
	}
	if !stream.HasPosition || !stream.HasSize || stream.Size.Width <= 0 || stream.Size.Height <= 0 {
		return fmt.Errorf("%w: selected ScreenCast monitor lacks complete logical geometry", ErrNotSupported)
	}
	x, y, width, height, err := screenCastDisplayBounds(displayID)
	if err != nil {
		return err
	}
	if !screenCastGeometryMatches(stream, x, y, width, height) {
		return fmt.Errorf("%w: selected ScreenCast monitor does not match display %d", ErrNotSupported, displayID)
	}
	displays, err := screenCastDisplaysNum()
	if err != nil {
		return err
	}
	for candidate := 0; candidate < displays; candidate++ {
		if candidate == displayID {
			continue
		}
		otherX, otherY, otherWidth, otherHeight, boundsErr := screenCastDisplayBounds(candidate)
		if boundsErr != nil {
			return boundsErr
		}
		if screenCastGeometryMatches(stream, otherX, otherY, otherWidth, otherHeight) {
			return fmt.Errorf("%w: selected ScreenCast monitor geometry matches multiple displays", ErrNotSupported)
		}
	}
	return nil
}

func screenCastGeometryMatches(stream portalpkg.ScreenCastStream, x, y, width, height int) bool {
	return int64(stream.Position.X) == int64(x) && int64(stream.Position.Y) == int64(y) &&
		int64(stream.Size.Width) == int64(width) && int64(stream.Size.Height) == int64(height)
}

// CloseScreenCastCapture stops PipeWire and closes the portal session.
func CloseScreenCastCapture() error {
	screenCastCapturePending.Lock()
	screenCastCapturePending.closing++
	if screenCastCapturePending.start != nil {
		screenCastCapturePending.start()
	}
	screenCastCapturePending.Unlock()

	screenCastCaptureOperation.Lock()
	defer func() {
		screenCastCapturePending.Lock()
		screenCastCapturePending.closing--
		screenCastCapturePending.Unlock()
		screenCastCaptureOperation.Unlock()
	}()
	screenCastCaptureState.Lock()
	capture := screenCastCaptureState.capture
	screenCastCaptureState.capture = nil
	screenCastCaptureState.Unlock()
	if capture == nil {
		return nil
	}
	return capture.Close()
}

func captureViaScreenCast(ctx context.Context, x, y, width, height int) (image.Image, error) {
	screenCastCaptureState.RLock()
	capture := screenCastCaptureState.capture
	screenCastCaptureState.RUnlock()
	if capture == nil {
		return nil, fmt.Errorf("%w: no active ScreenCast capture", ErrNotSupported)
	}
	return capture.Capture(ctx, x, y, width, height)
}
