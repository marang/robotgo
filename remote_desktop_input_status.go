package robotgo

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	inputportal "github.com/marang/robotgo/input/portal"
)

// RemoteDesktopPermissionStatus describes the last consent-session outcome.
type RemoteDesktopPermissionStatus string

const (
	RemoteDesktopPermissionNotRequested RemoteDesktopPermissionStatus = "not-requested"
	RemoteDesktopPermissionGranted      RemoteDesktopPermissionStatus = "granted"
	RemoteDesktopPermissionClosed       RemoteDesktopPermissionStatus = "closed"
	RemoteDesktopPermissionCancelled    RemoteDesktopPermissionStatus = "cancelled"
	RemoteDesktopPermissionTimedOut     RemoteDesktopPermissionStatus = "timed-out"
	RemoteDesktopPermissionDenied       RemoteDesktopPermissionStatus = "denied"
	RemoteDesktopPermissionFailed       RemoteDesktopPermissionStatus = "failed"
	RemoteDesktopPermissionUnavailable  RemoteDesktopPermissionStatus = "unavailable"
)

// RemoteDesktopInputStatus reports portal protocol support and the current
// consent session without opening a dialog or exposing restore-token contents.
type RemoteDesktopInputStatus struct {
	PortalAvailable       bool
	PortalVersion         uint32
	AvailableDevices      RemoteDesktopDevice
	ScreenCastVersion     uint32
	AvailableSources      RemoteDesktopSource
	AvailableCursorModes  RemoteDesktopCursorMode
	ScreenCastReason      string
	Permission            RemoteDesktopPermissionStatus
	SessionActive         bool
	GrantedDevices        RemoteDesktopDevice
	Streams               []RemoteDesktopStream
	RestoreTokenAvailable bool
	Reason                string
}

func permissionStatusForError(err error) RemoteDesktopPermissionStatus {
	switch {
	case errors.Is(err, inputportal.ErrCancelled), errors.Is(err, context.Canceled):
		return RemoteDesktopPermissionCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return RemoteDesktopPermissionTimedOut
	case errors.Is(err, inputportal.ErrDeviceNotGranted):
		return RemoteDesktopPermissionDenied
	case errors.Is(err, inputportal.ErrRejected):
		return RemoteDesktopPermissionFailed
	default:
		return RemoteDesktopPermissionUnavailable
	}
}

// GetRemoteDesktopInputStatus probes portal capabilities and reports the active
// consent state. It never opens a session or displays a permission dialog.
func GetRemoteDesktopInputStatus(ctx context.Context) (RemoteDesktopInputStatus, error) {
	status := RemoteDesktopInputStatus{Permission: RemoteDesktopPermissionNotRequested}
	if runtime.GOOS != "linux" || DetectDisplayServer() != DisplayServerWayland {
		status.Reason = "RemoteDesktop portal input requires a Linux Wayland session"
		return status, fmt.Errorf("%w: %s", ErrNotSupported, status.Reason)
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
	}
	capability, probeErr := remoteDesktopStatusProbe(ctx)
	if probeErr == nil || capability.ScreenCastIssue != "" {
		status.PortalAvailable = true
		status.PortalVersion = capability.Version
		status.AvailableDevices = capability.AvailableDevices
		status.ScreenCastVersion = capability.ScreenCastVersion
		status.AvailableSources = capability.AvailableSources
		status.AvailableCursorModes = capability.AvailableCursorModes
		status.ScreenCastReason = capability.ScreenCastIssue
	}

	remoteDesktopInputState.RLock()
	session := remoteDesktopInputState.session
	lastPermission := remoteDesktopInputState.permission
	lastReason := remoteDesktopInputState.reason
	remoteDesktopInputState.RUnlock()
	if session != nil {
		status.RestoreTokenAvailable = session.RestoreToken() != ""
		select {
		case <-session.Closed():
			status.Permission = RemoteDesktopPermissionClosed
			status.Reason = "the previously authorized portal session is closed; start a new session"
		default:
			status.PortalAvailable = true
			status.Permission = RemoteDesktopPermissionGranted
			status.SessionActive = true
			status.GrantedDevices = session.Devices()
			status.Streams = session.Streams()
			status.Reason = "portal consent session is active"
		}
	} else if lastPermission != "" {
		status.Permission = lastPermission
		status.Reason = lastReason
	} else if probeErr == nil {
		if capability.AvailableDevices == 0 {
			status.Reason = "RemoteDesktop portal is available but advertises no input devices"
		} else {
			status.Reason = "portal is available; call StartRemoteDesktopInput to request consent"
		}
	}
	if probeErr != nil {
		if status.Reason == "" {
			status.Reason = probeErr.Error()
		}
		return status, probeErr
	}
	return status, nil
}
