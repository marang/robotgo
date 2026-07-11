package robotgo

import (
	"context"
	"fmt"

	inputportal "github.com/marang/robotgo/input/portal"
)

// RemoteDesktopInputStreams returns the selected ScreenCast stream metadata.
func RemoteDesktopInputStreams() ([]RemoteDesktopStream, error) {
	remoteDesktopInputState.RLock()
	session := remoteDesktopInputState.session
	remoteDesktopInputState.RUnlock()
	if session == nil {
		return nil, fmt.Errorf("%w: call StartRemoteDesktopInputWithOptions with ScreenCast sources", ErrNotSupported)
	}
	select {
	case <-session.Closed():
		return nil, inputportal.ErrClosed
	default:
	}
	streams := session.Streams()
	if len(streams) == 0 {
		return nil, inputportal.ErrScreenCastRequired
	}
	return streams, nil
}

// RemoteDesktopInputRestoreToken returns the latest retained session's
// single-use restore token, or an empty string if persistence was not granted.
// CloseRemoteDesktopInput removes access to the retained token.
func RemoteDesktopInputRestoreToken() string {
	remoteDesktopInputState.RLock()
	defer remoteDesktopInputState.RUnlock()
	if remoteDesktopInputState.session == nil {
		return ""
	}
	return remoteDesktopInputState.session.RestoreToken()
}

func tryRemoteDesktopMoveAbsolute(x, y int, displayID []int) (bool, error) {
	return withRemoteDesktopInput(inputportal.DevicePointer, func(session remoteDesktopInputSession) error {
		stream, localX, localY, err := remoteDesktopTargetStream(session.Streams(), x, y, displayID)
		if err != nil {
			return err
		}
		return remoteDesktopEvent(func(ctx context.Context) error {
			return session.PointerMotionAbsolute(ctx, stream.NodeID, localX, localY)
		})
	})
}

func remoteDesktopTargetStream(streams []inputportal.Stream, x, y int, displayID []int) (inputportal.Stream, float64, float64, error) {
	if len(streams) == 0 {
		return inputportal.Stream{}, 0, 0, inputportal.ErrScreenCastRequired
	}
	if len(displayID) > 0 && displayID[0] >= 0 {
		if displayID[0] >= len(streams) {
			return inputportal.Stream{}, 0, 0, fmt.Errorf("%w: display index=%d streams=%d", inputportal.ErrStreamNotFound, displayID[0], len(streams))
		}
		stream := streams[displayID[0]]
		localX, localY := x, y
		if stream.HasPosition {
			localX -= int(stream.Position.X)
			localY -= int(stream.Position.Y)
		}
		return stream, float64(localX), float64(localY), nil
	}
	for _, stream := range streams {
		if !stream.HasPosition || !stream.HasSize {
			continue
		}
		left, top := int(stream.Position.X), int(stream.Position.Y)
		right, bottom := left+int(stream.Size.Width), top+int(stream.Size.Height)
		if x >= left && x < right && y >= top && y < bottom {
			return stream, float64(x - left), float64(y - top), nil
		}
	}
	if len(streams) == 1 {
		localX, localY := x, y
		if streams[0].HasPosition {
			localX -= int(streams[0].Position.X)
			localY -= int(streams[0].Position.Y)
		}
		return streams[0], float64(localX), float64(localY), nil
	}
	return inputportal.Stream{}, 0, 0, fmt.Errorf("%w: no selected stream contains global coordinate (%d,%d)", inputportal.ErrStreamNotFound, x, y)
}

// RemoteDesktopTouchDown starts a touch contact on a selected stream.
func RemoteDesktopTouchDown(stream, slot uint32, x, y float64) error {
	used, err := withRemoteDesktopInput(inputportal.DeviceTouchscreen, func(session remoteDesktopInputSession) error {
		return remoteDesktopEvent(func(ctx context.Context) error { return session.TouchDown(ctx, stream, slot, x, y) })
	})
	if !used {
		return ErrNotSupported
	}
	return err
}

// RemoteDesktopTouchMotion moves a touch contact on a selected stream.
func RemoteDesktopTouchMotion(stream, slot uint32, x, y float64) error {
	used, err := withRemoteDesktopInput(inputportal.DeviceTouchscreen, func(session remoteDesktopInputSession) error {
		return remoteDesktopEvent(func(ctx context.Context) error { return session.TouchMotion(ctx, stream, slot, x, y) })
	})
	if !used {
		return ErrNotSupported
	}
	return err
}

// RemoteDesktopTouchUp ends a touch contact.
func RemoteDesktopTouchUp(slot uint32) error {
	used, err := withRemoteDesktopInput(inputportal.DeviceTouchscreen, func(session remoteDesktopInputSession) error {
		return remoteDesktopEvent(func(ctx context.Context) error { return session.TouchUp(ctx, slot) })
	})
	if !used {
		return ErrNotSupported
	}
	return err
}
