//go:build linux && cgo && wayland

package robotgo

import (
	"errors"
	"testing"
)

func TestWaylandMouseToggleRejectsNonButtonCodes(t *testing.T) {
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, ":environment-flipped-after-wayland-snapshot")
	if !nativeWaylandMouseBackendSelectedForTest() {
		t.Fatal("Wayland-only native mouse backend re-read the changed display-server environment")
	}

	for _, name := range []string{"wheelUp", "wheelDown", "wheelLeft", "wheelRight"} {
		code, err := nativeWaylandMouseButtonCodeForTest(name)
		if !errors.Is(err, ErrNotSupported) {
			t.Fatalf("Wayland mapping for %s error = %v, want ErrNotSupported", name, err)
		}
		if code != 0 {
			t.Fatalf("Wayland mapping for %s code = %d, want no BTN_LEFT fallback", name, code)
		}
	}

	for name, want := range map[string]uint32{
		"left":   0x110,
		"right":  0x111,
		"center": 0x112,
		"middle": 0x112,
	} {
		code, err := nativeWaylandMouseButtonCodeForTest(name)
		if err != nil || code != want {
			t.Fatalf("Wayland mapping for %s = (%#x,%v), want (%#x,nil)", name, code, err, want)
		}
	}
}
