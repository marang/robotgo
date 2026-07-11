//go:build !linux || !cgo || !pipewire

package portal

func pipeWireCaptureCompiled() bool { return false }

func newPipeWireFrameSource(ScreenCast, ScreenCastStream) (pipeWireFrameSource, error) {
	return nil, ErrPipeWireUnavailable
}
