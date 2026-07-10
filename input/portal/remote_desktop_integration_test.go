//go:build linux && integration

package portal_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/marang/robotgo"
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
	if !capability.Supports(devices) {
		t.Fatalf("RemoteDesktop portal does not advertise keyboard and pointer input: %+v", capability)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := robotgo.StartRemoteDesktopInput(ctx, robotgo.RemoteDesktopKeyboard|robotgo.RemoteDesktopPointer); err != nil {
		t.Fatalf("StartRemoteDesktopInput error: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = robotgo.CloseRemoteDesktopInput()
		}
	}()
	if err := robotgo.RemoteDesktopInputReady(robotgo.RemoteDesktopKeyboard | robotgo.RemoteDesktopPointer); err != nil {
		t.Fatalf("RemoteDesktopInputReady error: %v", err)
	}
	if err := robotgo.MoveRelativeE(1, 0); err != nil {
		t.Fatalf("first MoveRelativeE error: %v", err)
	}
	if err := robotgo.MoveRelativeE(-1, 0); err != nil {
		t.Fatalf("second MoveRelativeE error: %v", err)
	}
	// A modifier-only tap validates keyboard injection without typing text into
	// whichever application happens to own focus on the interactive runner.
	if err := robotgo.KeyTap("shift"); err != nil {
		t.Fatalf("KeyTap error: %v", err)
	}
	if err := robotgo.CloseRemoteDesktopInput(); err != nil {
		t.Fatalf("CloseRemoteDesktopInput error: %v", err)
	}
	closed = true
}
