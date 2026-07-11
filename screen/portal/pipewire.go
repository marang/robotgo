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
	logical := image.Rect(0, 0, frame.Bounds().Dx(), frame.Bounds().Dy())
	if stream.HasSize {
		if stream.Size.Width <= 0 || stream.Size.Height <= 0 {
			return nil, fmt.Errorf("PipeWire stream has invalid logical size %dx%d", stream.Size.Width, stream.Size.Height)
		}
		logical = image.Rect(0, 0, int(stream.Size.Width), int(stream.Size.Height))
	}
	localX, localY := x, y
	if stream.HasPosition {
		localX -= int(stream.Position.X)
		localY -= int(stream.Position.Y)
	}
	requested := image.Rect(localX, localY, localX+width, localY+height).Intersect(logical)
	if requested.Empty() {
		return nil, fmt.Errorf("PipeWire capture region %v is outside stream bounds %v", image.Rect(localX, localY, localX+width, localY+height), logical)
	}
	frameBounds := frame.Bounds()
	left := requested.Min.X * frameBounds.Dx() / logical.Dx()
	top := requested.Min.Y * frameBounds.Dy() / logical.Dy()
	right := (requested.Max.X*frameBounds.Dx() + logical.Dx() - 1) / logical.Dx()
	bottom := (requested.Max.Y*frameBounds.Dy() + logical.Dy() - 1) / logical.Dy()
	pixelRegion := image.Rect(left, top, right, bottom).Intersect(frameBounds)
	result := image.NewRGBA(image.Rect(0, 0, pixelRegion.Dx(), pixelRegion.Dy()))
	draw.Draw(result, result.Bounds(), frame, pixelRegion.Min, draw.Src)
	return result, nil
}
