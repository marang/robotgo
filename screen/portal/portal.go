//go:build linux && portal
// +build linux,portal

package portal

import (
    "context"
    "errors"
    "fmt"
    "os"
    "time"

    "github.com/godbus/dbus/v5"
)

/*
#cgo linux pkg-config: libpipewire-0.3 libportal
#include "../screengrab_c.h"
*/
import "C"

// CBitmap mirrors robotgo.CBitmap without importing the root package.
type CBitmap = C.MMBitmapRef

// Capture captures a screen region using the freedesktop portal screencast
// API. The real implementation negotiates a session over D-Bus and reads
// frames from PipeWire. This placeholder connects to the session bus and
// then delegates to the C fallback used in tests.
func Capture(ctx context.Context, x, y, w, h int) (CBitmap, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if conn, err := dbus.SessionBus(); err == nil {
		defer conn.Close()
		obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_ = obj.CallWithContext(cctx, "org.freedesktop.DBus.Peer.Ping", 0).Err
	}

	if os.Getenv("ROBOTGO_PORTAL_FAIL") != "" {
		return nil, errors.New("portal capture forced failure")
	}

	var cerr C.int32_t
	bit := C.capture_screen_portal(C.int32_t(x), C.int32_t(y), C.int32_t(w), C.int32_t(h), 0, 0, &cerr)
	if bit == nil {
		return nil, fmt.Errorf("portal capture failed: %d", int(cerr))
	}
	return bit, nil
}

// CaptureRegionImage is implemented in screenshot_portal.go.
