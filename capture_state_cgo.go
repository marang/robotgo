//go:build cgo

package robotgo

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

// CaptureBackend reports which backend handled the most recent screen capture.
type CaptureBackend string

const (
	BackendNone       CaptureBackend = ""
	BackendScreencopy CaptureBackend = "screencopy"
	BackendPortal     CaptureBackend = "portal"
	BackendScreenCast CaptureBackend = "screencast"
	BackendX11        CaptureBackend = "x11"
	BackendPureGo     CaptureBackend = "pure-go"
)

var (
	captureStateMu sync.RWMutex
	lastBackend    CaptureBackend
	waylandBackend = WaylandBackendAuto
)

// LastBackend returns the backend used for the last CaptureScreen call.
func LastBackend() CaptureBackend {
	captureStateMu.RLock()
	defer captureStateMu.RUnlock()
	return lastBackend
}

func setLastBackend(backend CaptureBackend) {
	captureStateMu.Lock()
	lastBackend = backend
	captureStateMu.Unlock()
}

// WaylandBackend selects which Wayland backend to use at runtime.
type WaylandBackend int

const (
	WaylandBackendAuto   WaylandBackend = -1
	WaylandBackendDmabuf WaylandBackend = 0
	WaylandBackendWlShm  WaylandBackend = 1
)

// SetWaylandBackend allows tests and callers to force a specific Wayland
// capture backend.
func SetWaylandBackend(b WaylandBackend) {
	captureStateMu.Lock()
	waylandBackend = b
	captureStateMu.Unlock()
}

func selectedWaylandBackend() WaylandBackend {
	captureStateMu.RLock()
	defer captureStateMu.RUnlock()
	return waylandBackend
}

type captureTimeoutError struct {
	backend CaptureBackend
}

func (err captureTimeoutError) Error() string {
	return fmt.Sprintf("%s capture timed out", err.backend)
}

func (captureTimeoutError) Timeout() bool { return true }

func captureDebugf(format string, args ...interface{}) {
	if os.Getenv(envCaptureDebug) != "" {
		log.Printf("robotgo capture: "+format, args...)
	}
}

func waylandBackendFromEnv() (WaylandBackend, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envWaylandBackend))) {
	case "":
		return WaylandBackendAuto, false
	case "auto":
		return WaylandBackendAuto, true
	case "dmabuf":
		return WaylandBackendDmabuf, true
	case "wl_shm", "wlshm":
		return WaylandBackendWlShm, true
	default:
		return WaylandBackendAuto, false
	}
}

func portalStubEnabled() bool {
	return os.Getenv(envPortalStubGreen) != ""
}
