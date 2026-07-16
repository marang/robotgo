//go:build linux

package robotgo

import (
	"reflect"
	"testing"

	inputportal "github.com/marang/robotgo/input/portal"
)

func TestKeyArgumentAndValidationParityThroughPortalFallback(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "robotgo-key-parity-test")
	t.Setenv("DISPLAY", "")
	session := installFakeHighLevelPortalSession(t, inputportal.DeviceKeyboard)
	previous := GetRuntimeConfig()
	config := previous
	config.KeyDelay = 0
	if err := SetRuntimeConfig(config); err != nil {
		t.Fatalf("disable key delay: %v", err)
	}
	t.Cleanup(func() { _ = SetRuntimeConfig(previous) })

	if err := KeyDown("d", 0, []string{"CTRL"}); err != nil {
		t.Fatalf("KeyDown with mixed argument forms: %v", err)
	}
	if err := KeyUp("d", []string{"ctrl"}, 0); err != nil {
		t.Fatalf("KeyUp with mixed argument forms: %v", err)
	}
	if err := KeyPress("+", 0, "CTRL"); err != nil {
		t.Fatalf("KeyPress with shifted special key: %v", err)
	}
	if err := KeyPress("Enter", 0); err != nil {
		t.Fatalf("KeyPress with mixed-case named key: %v", err)
	}
	if err := KeyPress("numpad_0", 0); err != nil {
		t.Fatalf("KeyPress with legacy numpad alias: %v", err)
	}
	if err := KeyPress("é", 0); err != nil {
		t.Fatalf("KeyPress with a UTF-8 single rune: %v", err)
	}
	if err := KeyDown("Ä", 0); err != nil {
		t.Fatalf("KeyDown with an uppercase UTF-8 single rune: %v", err)
	}
	if err := KeyUp("Ä", 0); err != nil {
		t.Fatalf("KeyUp with an uppercase UTF-8 single rune: %v", err)
	}
	events, _ := session.snapshot()
	want := []string{
		"keysym:65507:true",
		"keysym:100:true",
		"keysym:100:false",
		"keysym:65507:false",
		"keysym:65507:true",
		"keysym:65505:true",
		"keysym:61:true",
		"keysym:61:false",
		"keysym:65505:false",
		"keysym:65507:false",
		"keysym:65293:true",
		"keysym:65293:false",
		"keysym:65456:true",
		"keysym:65456:false",
		"keysym:233:true",
		"keysym:233:false",
		"keysym:65505:true",
		"keysym:228:true",
		"keysym:228:false",
		"keysym:65505:false",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("key events = %v, want %v", events, want)
	}

	for _, call := range []func() error{
		func() error { return KeyTap("") },
		func() error { return KeyTap("not-a-key") },
		func() error { return KeyTap("enter", "not-a-modifier") },
	} {
		if err := call(); err == nil {
			t.Fatal("invalid key input unexpectedly succeeded")
		}
	}
	after, _ := session.snapshot()
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("invalid key input produced portal events: before=%v after=%v", want, after)
	}
}
