//go:build linux && cgo && x11integration && !wayland

package robotgo

import (
	"os"
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
)

func TestNativeX11StatefulInputDoesNotSwitchToPortalOnUp(t *testing.T) {
	display := os.Getenv(envDisplay)
	if display == "" {
		t.Skip("X11 integration test requires DISPLAY")
	}
	t.Setenv(envWaylandDisplay, "")
	if err := CloseMainDisplayE(); err != nil {
		t.Fatalf("reset native X11 display: %v", err)
	}
	previous := GetRuntimeConfig()
	config := previous
	config.KeyDelay = 0
	config.MouseDelay = 0
	if err := SetRuntimeConfig(config); err != nil {
		t.Fatalf("disable input delays: %v", err)
	}
	t.Cleanup(func() { _ = SetRuntimeConfig(previous) })
	t.Cleanup(func() {
		_ = os.Setenv(envWaylandDisplay, "")
		_ = os.Setenv(envDisplay, display)
		_ = KeyUp("a")
		_ = MouseUp("right")
		_ = CloseMainDisplayE()
	})

	if err := KeyDown("esc"); err != nil {
		t.Fatalf("native KeyDown with first alias: %v", err)
	}
	if err := KeyUp("escape"); err != nil {
		t.Fatalf("native KeyUp with equivalent alias: %v", err)
	}

	if err := KeyDown("a"); err != nil {
		t.Fatalf("native KeyDown: %v", err)
	}
	if err := MouseDown("right"); err != nil {
		t.Fatalf("native MouseDown: %v", err)
	}
	session := installFakeHighLevelPortalSession(
		t, inputportal.DeviceKeyboard|inputportal.DevicePointer,
	)

	// A fresh transaction now selects Wayland, where a portal is active. The
	// matching Ups must still target the native backend that owns the Downs.
	t.Setenv(envWaylandDisplay, "robotgo-portal-became-available")
	t.Setenv(envDisplay, "")
	if err := KeyUp("a"); err != nil {
		t.Fatalf("native-affine KeyUp after portal availability: %v", err)
	}
	if err := MouseUp("right"); err != nil {
		t.Fatalf("native-affine MouseUp after portal availability: %v", err)
	}
	if events, _ := session.snapshot(); len(events) != 0 {
		t.Fatalf("native-owned Ups emitted portal events: %v", events)
	}
}
