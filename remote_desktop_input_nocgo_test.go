//go:build !cgo

package robotgo

import (
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
)

func TestNonCGOHighLevelPortalInput(t *testing.T) {
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
		t.Fatalf("MoveE error: %v", err)
	}
	if err := MoveRelativeE(1, 2); err != nil {
		t.Fatalf("MoveRelativeE error: %v", err)
	}
	if err := ClickE("left"); err != nil {
		t.Fatalf("ClickE error: %v", err)
	}
	if err := ScrollE(0, 2, 7); err != nil {
		t.Fatalf("ScrollE error: %v", err)
	}
	if err := TypeStrE("x"); err != nil {
		t.Fatalf("TypeStrE error: %v", err)
	}
	TypeStrDelay("y", 0)
	if err := UnicodeTypeE('€'); err != nil {
		t.Fatalf("UnicodeTypeE error: %v", err)
	}
	if err := KeyTap("A", "ctrl"); err != nil {
		t.Fatalf("KeyTap error: %v", err)
	}
	if err := KeyToggle("enter", "down"); err != nil {
		t.Fatalf("KeyToggle down error: %v", err)
	}
	if err := KeyToggle("enter", "up"); err != nil {
		t.Fatalf("KeyToggle up error: %v", err)
	}
	if err := KeyPress("A", "shift"); err != nil {
		t.Fatalf("KeyPress error: %v", err)
	}
	assertRemoteDesktopMouseDelays(t, *delays, []int{23, 23, 23, 30})
	events, _ := session.snapshot()
	if len(events) != 23 {
		t.Fatalf("events = %#v, want 23", events)
	}
	wantTail := []string{
		"keysym:65507:true",
		"keysym:65505:true",
		"keysym:97:true",
		"keysym:97:false",
		"keysym:65505:false",
		"keysym:65507:false",
		"keysym:65293:true",
		"keysym:65293:false",
		"keysym:65505:true",
		"keysym:97:true",
		"keysym:97:false",
		"keysym:65505:false",
	}
	for i, want := range wantTail {
		if got := events[len(events)-len(wantTail)+i]; got != want {
			t.Fatalf("tail event %d = %q, want %q (all=%#v)", i, got, want, events)
		}
	}
}
