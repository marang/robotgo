package portal

import (
	"context"
	"errors"
	"os"
)

const envDisablePortal = "ROBOTGO_DISABLE_PORTAL"

// ScreenCastSource is a bitmask of source types offered to the user.
type ScreenCastSource uint32

const (
	ScreenCastSourceMonitor ScreenCastSource = 1
	ScreenCastSourceWindow  ScreenCastSource = 2
	ScreenCastSourceVirtual ScreenCastSource = 4
	screenCastSourceAll                      = ScreenCastSourceMonitor | ScreenCastSourceWindow | ScreenCastSourceVirtual
)

// ScreenCastCursor controls how the cursor is represented in captured frames.
type ScreenCastCursor uint32

const (
	ScreenCastCursorHidden   ScreenCastCursor = 1
	ScreenCastCursorEmbedded ScreenCastCursor = 2
	ScreenCastCursorMetadata ScreenCastCursor = 4
	screenCastCursorAll                       = ScreenCastCursorHidden | ScreenCastCursorEmbedded | ScreenCastCursorMetadata
)

// ScreenCastPersist controls whether the portal may issue a restore token.
type ScreenCastPersist uint32

const (
	ScreenCastPersistNone        ScreenCastPersist = 0
	ScreenCastPersistApplication ScreenCastPersist = 1
	ScreenCastPersistExplicit    ScreenCastPersist = 2
)

// ScreenCastOptions configures a persistent ScreenCast portal session.
type ScreenCastOptions struct {
	Sources      ScreenCastSource
	Multiple     bool
	Cursor       ScreenCastCursor
	Persist      ScreenCastPersist
	RestoreToken string
}

// ScreenCastPoint is a logical compositor coordinate.
type ScreenCastPoint struct{ X, Y int32 }

// ScreenCastSize is a logical compositor size.
type ScreenCastSize struct{ Width, Height int32 }

// ScreenCastStream describes one PipeWire stream selected by the user.
type ScreenCastStream struct {
	NodeID         uint32
	ID             string
	Position       ScreenCastPoint
	HasPosition    bool
	Size           ScreenCastSize
	HasSize        bool
	SourceType     ScreenCastSource
	MappingID      string
	PipeWireSerial uint64
}

// ScreenCastCapability describes the live ScreenCast portal interface.
type ScreenCastCapability struct {
	Version       uint32
	Sources       ScreenCastSource
	CursorModes   ScreenCastCursor
	PipeWireReady bool
}

var (
	ErrScreenCastUnavailable = errors.New("screencast portal unavailable")
	ErrScreenCastCancelled   = errors.New("screencast portal request cancelled")
	ErrScreenCastRejected    = errors.New("screencast portal request rejected")
	ErrScreenCastClosed      = errors.New("screencast portal session closed")
	ErrScreenCastNoStreams   = errors.New("screencast portal returned no streams")
	ErrPipeWireUnavailable   = errors.New("PipeWire capture unavailable")
)

func validateScreenCastOptions(options ScreenCastOptions) error {
	if options.Sources == 0 || options.Sources&^screenCastSourceAll != 0 {
		return errors.New("screencast portal: invalid source mask")
	}
	if options.Cursor&^screenCastCursorAll != 0 || options.Cursor != 0 && options.Cursor&(options.Cursor-1) != 0 {
		return errors.New("screencast portal: invalid cursor mode")
	}
	if options.Persist > ScreenCastPersistExplicit {
		return errors.New("screencast portal: invalid persist mode")
	}
	return nil
}

// ScreenCast captures frames from one persistent portal session.
// Implementations are platform-specific.
type ScreenCast interface {
	Streams() []ScreenCastStream
	RestoreToken() string
	PipeWireFile() (*os.File, error)
	Closed() <-chan struct{}
	Close() error
}

// OpenScreenCast is implemented by the platform-specific portal client.
func OpenScreenCast(ctx context.Context, options ScreenCastOptions) (ScreenCast, error) {
	return openScreenCast(ctx, options)
}

// ProbeScreenCast queries ScreenCast capabilities without opening a session or
// displaying a consent dialog.
func ProbeScreenCast(ctx context.Context) (ScreenCastCapability, error) {
	return probeScreenCast(ctx)
}
