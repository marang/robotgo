//go:build cgo && linux

package robotgo

import (
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
)

func TestHighLevelInputFallsBackToActiveRemoteDesktopSession(t *testing.T) {
	t.Setenv(envWaylandDisplay, "robotgo-missing-wayland")
	t.Setenv(envDisplay, "")
	stubCaptureCapabilityProbes(t, false, false)
	oldMouseSleep, oldKeySleep := MouseSleep, KeySleep
	MouseSleep, KeySleep = 23, 0
	t.Cleanup(func() { MouseSleep, KeySleep = oldMouseSleep, oldKeySleep })
	delays := installRemoteDesktopMouseDelayRecorder(t)

	session := installFakeHighLevelPortalSession(t, inputportal.DeviceKeyboard|inputportal.DevicePointer)
	session.streams = []inputportal.Stream{{
		NodeID: 77, Position: inputportal.Point{X: -1920, Y: 0}, HasPosition: true,
		Size: inputportal.Size{Width: 1920, Height: 1080}, HasSize: true,
	}}
	if err := MoveE(-1900, 100); err != nil {
		t.Fatalf("MoveE absolute portal fallback error: %v", err)
	}
	if err := MoveRelativeE(4, -3); err != nil {
		t.Fatalf("MoveRelativeE error: %v", err)
	}
	if err := ClickE("left"); err != nil {
		t.Fatalf("ClickE error: %v", err)
	}
	if err := Toggle("right"); err != nil {
		t.Fatalf("Toggle error: %v", err)
	}
	if err := ScrollE(2, -3, 7); err != nil {
		t.Fatalf("ScrollE error: %v", err)
	}
	if err := KeyTap("a"); err != nil {
		t.Fatalf("KeyTap error: %v", err)
	}
	if err := KeyTap("A", []string{"ctrl"}); err != nil {
		t.Fatalf("modified uppercase KeyTap error: %v", err)
	}
	if err := TypeStrE("A"); err != nil {
		t.Fatalf("TypeStrE error: %v", err)
	}
	if err := UnicodeTypeE('€'); err != nil {
		t.Fatalf("UnicodeTypeE error: %v", err)
	}
	capabilities := GetLinuxCapabilities()
	if capabilities.Keyboard.Backend != "portal-remote-desktop" || !capabilities.Keyboard.Available {
		t.Fatalf("keyboard capability did not select active portal session: %+v", capabilities.Keyboard)
	}
	if capabilities.Mouse.Backend != "portal-remote-desktop" || !capabilities.Mouse.Available {
		t.Fatalf("mouse capability did not select active portal session: %+v", capabilities.Mouse)
	}
	assertRemoteDesktopMouseDelays(t, *delays, []int{23, 23, 23, 30})

	events, _ := session.snapshot()
	wantPrefixes := []string{
		"absolute:77:20:100",
		"motion:4:-3",
		"button:272:true",
		"button:272:false",
		"button:273:true",
		"axis:1:2",
		"axis:0:3",
		"keysym:97:true",
		"keysym:97:false",
		"keysym:65507:true",
		"keysym:65505:true",
		"keysym:97:true",
		"keysym:97:false",
		"keysym:65505:false",
		"keysym:65507:false",
		"keysym:65:true",
		"keysym:65:false",
		"keysym:16785580:true",
		"keysym:16785580:false",
	}
	if len(events) != len(wantPrefixes) {
		t.Fatalf("events = %#v, want %d events", events, len(wantPrefixes))
	}
	for i, want := range wantPrefixes {
		if events[i] != want {
			t.Fatalf("event %d = %q, want %q (all=%#v)", i, events[i], want, events)
		}
	}
}
