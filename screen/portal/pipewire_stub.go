//go:build !linux || !cgo || !pipewire

package portal

import "context"

func pipeWireCaptureCompiled() bool { return false }

func newPipeWireFrameSource(context.Context, ScreenCast, ScreenCastStream) (pipeWireFrameSource, error) {
	return nil, ErrPipeWireUnavailable
}
