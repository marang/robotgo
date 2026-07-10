//go:build !cgo

package robotgo

import (
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
)

func TestNonCGOHighLevelPortalInput(t *testing.T) {
	oldKeySleep := KeySleep
	KeySleep = 0
	t.Cleanup(func() { KeySleep = oldKeySleep })
	session := installFakeHighLevelPortalSession(t, inputportal.DeviceKeyboard|inputportal.DevicePointer)
	if err := MoveRelativeE(1, 2); err != nil {
		t.Fatalf("MoveRelativeE error: %v", err)
	}
	if err := ClickE("left"); err != nil {
		t.Fatalf("ClickE error: %v", err)
	}
	if err := ScrollE(0, 2); err != nil {
		t.Fatalf("ScrollE error: %v", err)
	}
	if err := TypeStrE("x"); err != nil {
		t.Fatalf("TypeStrE error: %v", err)
	}
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
	events, _ := session.snapshot()
	if len(events) != 22 {
		t.Fatalf("events = %#v, want 22", events)
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
		"keysym:65505:false",
		"keysym:65505:true",
		"keysym:97:false",
		"keysym:65505:false",
	}
	for i, want := range wantTail {
		if got := events[len(events)-len(wantTail)+i]; got != want {
			t.Fatalf("tail event %d = %q, want %q (all=%#v)", i, got, want, events)
		}
	}
}
