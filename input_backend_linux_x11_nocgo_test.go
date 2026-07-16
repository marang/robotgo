//go:build linux && !cgo

package robotgo

import (
	"errors"
	"testing"

	"github.com/marang/robotgo/internal/x11input"
)

func TestTranslatePureGoX11ErrorPreservesJoinedCauses(t *testing.T) {
	cause := errors.New("specific X11 failure")
	err := translatePureGoX11Error(errors.Join(x11input.ErrUnsupported, cause))
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("translated error = %v, want ErrNotSupported", err)
	}
	if !errors.Is(err, x11input.ErrUnsupported) || !errors.Is(err, cause) {
		t.Fatalf("translated error lost joined causes: %v", err)
	}
}

func TestX11LiteralAndModifierContracts(t *testing.T) {
	for key, want := range map[string]bool{"a": true, "€": true, "enter": false, "F12": false} {
		if got := x11LiteralKey(key); got != want {
			t.Fatalf("x11LiteralKey(%q) = %t, want %t", key, got, want)
		}
	}
	if err := validateX11KeyEvent(pureGoKeyEvent{Key: "a", Down: true}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("persistent literal key validation = %v, want ErrNotSupported", err)
	}
	for _, event := range []pureGoKeyEvent{
		{Key: "a", Tap: true},
		{Key: "shift", Down: true},
		{Key: "a", Down: false},
		{Key: "enter", Tap: true, Modifiers: []string{"ctrl", "right_shift", "NONE"}},
	} {
		if err := validateX11KeyEvent(event); err != nil {
			t.Fatalf("validateX11KeyEvent(%+v) = %v, want nil", event, err)
		}
	}
	if err := validateX11KeyEvent(pureGoKeyEvent{Key: "enter", Tap: true, Modifiers: []string{"y"}}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("literal modifier validation = %v, want ErrNotSupported", err)
	}
}

func TestX11KeysymResolutionPreservesCaseAndNamedKeys(t *testing.T) {
	tests := []struct {
		key  string
		want uint32
	}{
		{key: "A", want: 'A'},
		{key: "a", want: 'a'},
		{key: "enter", want: 0xff0d},
		{key: "F24", want: x11KeysymF1 + 23},
		{key: "€", want: 0x010020ac},
	}
	for _, test := range tests {
		got, err := x11KeysymForKey(test.key)
		if err != nil || got != test.want {
			t.Fatalf("x11KeysymForKey(%q) = (%#x,%v), want (%#x,nil)", test.key, got, err, test.want)
		}
	}
	for _, key := range []string{"", "not-a-key", string([]byte{0xff})} {
		if _, err := x11KeysymForKey(key); err == nil {
			t.Fatalf("x11KeysymForKey(%q) unexpectedly succeeded", key)
		}
	}
}

func TestX11EventKeysymAppliesExplicitShiftToLiteralKeys(t *testing.T) {
	for _, test := range []struct {
		key       string
		modifiers []string
		want      uint32
	}{
		{key: "a", modifiers: []string{"shift"}, want: 'A'},
		{key: "1", modifiers: []string{"right_shift"}, want: '!'},
		{key: "+", modifiers: []string{"shift"}, want: '+'},
		{key: "a", modifiers: []string{"ctrl"}, want: 'a'},
		{key: "enter", modifiers: []string{"shift"}, want: 0xff0d},
	} {
		got, err := x11EventKeysym(pureGoKeyEvent{Key: test.key, Modifiers: test.modifiers, Tap: true})
		if err != nil || got != test.want {
			t.Fatalf("x11EventKeysym(%q,%v) = (%#x,%v), want (%#x,nil)", test.key, test.modifiers, got, err, test.want)
		}
	}
}

func TestX11MouseButtonRejectsUnknownNames(t *testing.T) {
	tests := map[string]byte{
		"": 1, "left": 1, "center": 2, "middle": 2, "right": 3,
		"wheelUp": 4, "wheelDown": 5, "wheelLeft": 6, "wheelRight": 7,
	}
	for name, want := range tests {
		got, err := x11MouseButton(name)
		if err != nil || got != want {
			t.Fatalf("x11MouseButton(%q) = (%d,%v), want (%d,nil)", name, got, err, want)
		}
	}
	if _, err := x11MouseButton("primary"); err == nil {
		t.Fatal("unknown X11 mouse button unexpectedly succeeded")
	}
}

func TestX11BackendResolverNeverUsesX11InWaylandSession(t *testing.T) {
	t.Setenv(envDisplay, ":99")
	t.Setenv(envWaylandDisplay, "wayland-test")
	if backend := platformPureGoInputBackend(); backend != nil {
		t.Fatalf("Wayland session selected X11 backend %q", backend.Name())
	}
	t.Setenv(envWaylandDisplay, "")
	if backend := platformPureGoInputBackend(); backend == nil || backend.Name() != featureBackendPureGoX11 {
		t.Fatalf("X11 session backend = %#v, want %q", backend, featureBackendPureGoX11)
	}
}

func TestX11CapabilityInspectionDoesNotProbeDisplay(t *testing.T) {
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, ":robotgo-definitely-unreachable")
	t.Setenv(envXDGSessionType, "x11")
	capabilities := GetRuntimeCapabilities()
	if !capabilities.Keyboard.Available || !capabilities.Mouse.Available {
		t.Fatalf("X11 capabilities = keyboard %+v, mouse %+v", capabilities.Keyboard, capabilities.Mouse)
	}
}

func TestX11CapabilityInspectionRejectsSessionConflictWithoutProbe(t *testing.T) {
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, ":99")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	capabilities := GetRuntimeCapabilities()
	for name, capability := range map[string]FeatureCapability{
		"keyboard": capabilities.Keyboard,
		"mouse":    capabilities.Mouse,
	} {
		if capability.Available || capability.Backend != featureBackendPureGoX11 {
			t.Fatalf("%s conflict capability = %+v", name, capability)
		}
	}
}
