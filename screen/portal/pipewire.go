package portal

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"sync"
	"sync/atomic"
)

type pipeWireFrameSource interface {
	ready() error
	frame(context.Context) (*image.RGBA, error)
	interrupt()
	close() error
}

// PipeWireCapture owns one ScreenCast portal session and a reusable PipeWire
// consumer for a selected stream.
type PipeWireCapture struct {
	mu      sync.Mutex
	session ScreenCast
	stream  ScreenCastStream
	backend pipeWireFrameSource
	closed  bool
	closing atomic.Bool
}

// Ready reports whether both the reusable consumer and its portal session are
// still active.
func (c *PipeWireCapture) Ready() error {
	if c == nil || c.closing.Load() {
		return ErrScreenCastClosed
	}
	session := c.session
	if session == nil {
		return ErrScreenCastClosed
	}
	select {
	case <-session.Closed():
		return ErrScreenCastClosed
	default:
		if c.backend == nil {
			return ErrPipeWireUnavailable
		}
		return c.backend.ready()
	}
}

// OpenPipeWireCapture presents the ScreenCast consent dialog once and creates
// a reusable frame consumer for streamIndex.
func OpenPipeWireCapture(ctx context.Context, options ScreenCastOptions, streamIndex int) (*PipeWireCapture, error) {
	if streamIndex < 0 {
		return nil, fmt.Errorf("%w: stream index=%d", ErrScreenCastNoStreams, streamIndex)
	}
	if options.Cursor == ScreenCastCursorMetadata {
		return nil, fmt.Errorf("%w: cursor metadata is not supported by the image capture backend; use embedded or hidden cursor mode", ErrPipeWireUnavailable)
	}
	if !pipeWireCaptureCompiled() {
		return nil, ErrPipeWireUnavailable
	}
	session, err := OpenScreenCast(ctx, options)
	if err != nil {
		return nil, err
	}
	streams := session.Streams()
	if streamIndex < 0 || streamIndex >= len(streams) {
		_ = session.Close()
		return nil, fmt.Errorf("%w: stream index=%d streams=%d", ErrScreenCastNoStreams, streamIndex, len(streams))
	}
	backend, err := newPipeWireFrameSource(ctx, session, streams[streamIndex])
	if err != nil {
		return nil, errors.Join(err, session.Close())
	}
	return &PipeWireCapture{session: session, stream: streams[streamIndex], backend: backend}, nil
}

// PipeWireCaptureCompiled reports whether this build includes libpipewire frame
// support. Portal capability and session negotiation remain available without it.
func PipeWireCaptureCompiled() bool { return pipeWireCaptureCompiled() }

// Streams returns the streams selected for the persistent portal session.
func (c *PipeWireCapture) Streams() []ScreenCastStream {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.Streams()
}

// RestoreToken returns the latest single-use ScreenCast restore token.
func (c *PipeWireCapture) RestoreToken() string {
	if c == nil || c.session == nil {
		return ""
	}
	return c.session.RestoreToken()
}

// Capture returns the next frame. A positive region is interpreted in logical
// compositor coordinates and scaled to the negotiated PipeWire frame size.
func (c *PipeWireCapture) Capture(ctx context.Context, x, y, width, height int) (image.Image, error) {
	if c == nil {
		return nil, ErrScreenCastClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.closing.Load() {
		return nil, ErrScreenCastClosed
	}
	frame, err := c.backend.frame(ctx)
	if err != nil {
		return nil, err
	}
	return cropPipeWireFrame(frame, c.stream, x, y, width, height)
}

// Close stops PipeWire before releasing the portal session and its original FD.
func (c *PipeWireCapture) Close() error {
	if c == nil {
		return nil
	}
	c.closing.Store(true)
	if c.backend != nil {
		c.backend.interrupt()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var backendErr, sessionErr error
	if c.backend != nil {
		backendErr = c.backend.close()
	}
	if c.session != nil {
		sessionErr = c.session.Close()
	}
	return errors.Join(backendErr, sessionErr)
}

func cropPipeWireFrame(frame *image.RGBA, stream ScreenCastStream, x, y, width, height int) (image.Image, error) {
	if frame == nil || frame.Bounds().Empty() {
		return nil, errors.New("PipeWire returned an empty frame")
	}
	if width == 0 && height == 0 {
		return frame, nil
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("PipeWire capture region has invalid size %dx%d", width, height)
	}
	frameBounds := frame.Bounds()
	logicalWidth, logicalHeight := int64(frameBounds.Dx()), int64(frameBounds.Dy())
	if stream.HasSize {
		if stream.Size.Width <= 0 || stream.Size.Height <= 0 {
			return nil, fmt.Errorf("PipeWire stream has invalid logical size %dx%d", stream.Size.Width, stream.Size.Height)
		}
		logicalWidth, logicalHeight = int64(stream.Size.Width), int64(stream.Size.Height)
	}
	localX, localY := int64(x), int64(y)
	if stream.HasPosition {
		localX = saturatingAdd64(localX, -int64(stream.Position.X))
		localY = saturatingAdd64(localY, -int64(stream.Position.Y))
	}
	requestedRight := saturatingAdd64(localX, int64(width))
	requestedBottom := saturatingAdd64(localY, int64(height))
	left := max64(localX, 0)
	top := max64(localY, 0)
	right := min64(requestedRight, logicalWidth)
	bottom := min64(requestedBottom, logicalHeight)
	if left >= right || top >= bottom {
		return nil, fmt.Errorf(
			"PipeWire capture region (%d,%d)-(%d,%d) is outside stream bounds (0,0)-(%d,%d)",
			localX, localY, requestedRight, requestedBottom, logicalWidth, logicalHeight,
		)
	}
	pixelRegion := image.Rect(
		frameBounds.Min.X+int(scaleFloor(left, int64(frameBounds.Dx()), logicalWidth)),
		frameBounds.Min.Y+int(scaleFloor(top, int64(frameBounds.Dy()), logicalHeight)),
		frameBounds.Min.X+int(scaleCeil(right, int64(frameBounds.Dx()), logicalWidth)),
		frameBounds.Min.Y+int(scaleCeil(bottom, int64(frameBounds.Dy()), logicalHeight)),
	).Intersect(frameBounds)
	result := image.NewRGBA(image.Rect(0, 0, pixelRegion.Dx(), pixelRegion.Dy()))
	draw.Draw(result, result.Bounds(), frame, pixelRegion.Min, draw.Src)
	return result, nil
}

// scaleFloor and scaleCeil map a non-negative logical coordinate into a pixel
// extent without overflowing when the pixel extent is close to the int limit.
func scaleFloor(value, extent, logicalExtent int64) int64 {
	return (extent/logicalExtent)*value + (extent%logicalExtent)*value/logicalExtent
}

func scaleCeil(value, extent, logicalExtent int64) int64 {
	quotient := (extent / logicalExtent) * value
	remainder := (extent % logicalExtent) * value
	result := quotient + remainder/logicalExtent
	if remainder%logicalExtent != 0 {
		result++
	}
	return result
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func saturatingAdd64(a, b int64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	const minInt64 = -maxInt64 - 1
	if b > 0 && a > maxInt64-b {
		return maxInt64
	}
	if b < 0 && a < minInt64-b {
		return minInt64
	}
	return a + b
}
