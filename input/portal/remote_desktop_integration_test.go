//go:build linux && integration

package portal_test

import (
	"context"
	"os"
	"testing"
	"time"

	portalinput "github.com/marang/robotgo/input/portal"
)

const envRemoteDesktopE2E = "ROBOTGO_REMOTE_DESKTOP_E2E"

func TestRemoteDesktopPortalRuntime(t *testing.T) {
	if os.Getenv(envRemoteDesktopE2E) == "" {
		t.Skip("set ROBOTGO_REMOTE_DESKTOP_E2E=1 to allow a portal consent dialog and pointer motion")
	}

	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
	capability, err := portalinput.Probe(probeCtx)
	cancelProbe()
	if err != nil {
		t.Fatalf("Probe error: %v", err)
	}
	devices := portalinput.DeviceKeyboard | portalinput.DevicePointer
	if capability.Supports(portalinput.DeviceTouchscreen) {
		devices |= portalinput.DeviceTouchscreen
	}
	if !capability.Supports(devices) {
		t.Fatalf("RemoteDesktop portal does not advertise keyboard and pointer input: %+v", capability)
	}
	if !capability.SupportsSources(portalinput.SourceMonitor) {
		t.Fatalf("ScreenCast portal does not advertise monitor sources: %+v", capability)
	}
	if !capability.SupportsCursorMode(portalinput.CursorHidden) {
		t.Fatalf("ScreenCast portal does not advertise hidden cursor mode: %+v", capability)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	options := portalinput.OpenOptions{
		Devices: devices, Sources: portalinput.SourceMonitor,
		CursorMode: portalinput.CursorHidden,
	}
	session, err := portalinput.OpenWithOptions(ctx, options)
	if err != nil {
		t.Fatalf("OpenWithOptions error: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = session.Close()
		}
	}()
	if granted := session.Devices(); granted&devices != devices {
		t.Fatalf("granted devices=%d, want all requested devices=%d", granted, devices)
	}
	runPortalEvent(t, "first PointerMotion", func(eventCtx context.Context) error {
		return session.PointerMotion(eventCtx, 1, 0)
	})
	runPortalEvent(t, "second PointerMotion", func(eventCtx context.Context) error {
		return session.PointerMotion(eventCtx, -1, 0)
	})
	streams := session.Streams()
	if len(streams) == 0 {
		t.Fatal("portal session returned no ScreenCast streams")
	}
	stream := streams[0]
	runPortalEvent(t, "PointerMotionAbsolute", func(eventCtx context.Context) error {
		return session.PointerMotionAbsolute(eventCtx, stream.NodeID, 1, 1)
	})
	if devices&portalinput.DeviceTouchscreen != 0 {
		runPortalEvent(t, "TouchDown", func(eventCtx context.Context) error {
			return session.TouchDown(eventCtx, stream.NodeID, 0, 1, 1)
		})
		runPortalEvent(t, "TouchUp", func(eventCtx context.Context) error {
			return session.TouchUp(eventCtx, 0)
		})
	}
	// A modifier-only tap validates keyboard injection without typing text into
	// whichever application happens to own focus on the interactive runner.
	runPortalEvent(t, "modifier press", func(eventCtx context.Context) error {
		return session.KeyboardKeysym(eventCtx, 0xffe1, true)
	})
	runPortalEvent(t, "modifier release", func(eventCtx context.Context) error {
		return session.KeyboardKeysym(eventCtx, 0xffe1, false)
	})
	if err := session.Close(); err != nil {
		t.Fatalf("portal session Close error: %v", err)
	}
	closed = true
}

func runPortalEvent(t *testing.T, action string, event func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := event(ctx); err != nil {
		t.Fatalf("portal %s error: %v", action, err)
	}
}
