//go:build linux && !cgo

package robotgo

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

func TestX11KeyboardMapResolvesActiveLayoutLevels(t *testing.T) {
	keyboard := x11KeyboardMap{
		minimum:    8,
		perKeycode: 6,
		modifiers:  map[xproto.Keycode]struct{}{11: {}},
		keysyms: []xproto.Keysym{
			'a', 'A', 0x010003b1, 0x01000391, '@', '#',
			0xffe1, 0, 0xffe1, 0, 0, 0,
			0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0,
		},
	}
	tests := []struct {
		keysym uint32
		want   x11ResolvedKey
	}{
		{keysym: 0xffe1, want: x11ResolvedKey{code: 9}},
	}
	for _, test := range tests {
		got, ok := keyboard.resolve(test.keysym)
		if !ok || got != test.want {
			t.Fatalf("resolve(%#x) = (%+v,%v), want (%+v,true)", test.keysym, got, ok, test.want)
		}
	}
	for _, ambiguous := range []uint32{'a', 'A', 0x010003b1, '@'} {
		if got, ok := keyboard.resolve(ambiguous); ok {
			t.Fatalf("resolve(%#x) = %+v, want no unsafe core-map guess", ambiguous, got)
		}
	}
	backend := &x11InputBackend{}
	if err := backend.updateScratchStateLocked(&keyboard); err != nil {
		t.Fatalf("initialize scratch state: %v", err)
	}
	if len(backend.scratchSlots) != 1 || backend.scratchSlots[0].code != 10 {
		t.Fatalf("scratch slots = %+v, want only non-modifier keycode 10", backend.scratchSlots)
	}
	keyboard.modifiers[10] = struct{}{}
	if err := backend.updateScratchStateLocked(&keyboard); err != nil {
		t.Fatalf("drop newly assigned modifier from unused scratch pool: %v", err)
	}
	if len(backend.scratchSlots) != 0 {
		t.Fatalf("modifier keycode remained in scratch pool: %+v", backend.scratchSlots)
	}
}

func TestX11ModifierResolutionRequiresModifierMapMembership(t *testing.T) {
	keyboard := x11KeyboardMap{
		minimum:    8,
		perKeycode: 2,
		keysyms: []xproto.Keysym{
			0xffe3, 0, // Control_L-shaped key that is not a modifier.
			0xffe3, 0xffe7, // Actual Control modifier with a second symbol.
		},
		modifiers: map[xproto.Keycode]struct{}{9: {}},
	}
	got, ok := keyboard.resolveModifier(0xffe3)
	if !ok || got.code != 9 {
		t.Fatalf("resolveModifier(Control_L) = (%+v,%t), want keycode 9", got, ok)
	}
	delete(keyboard.modifiers, 9)
	if got, ok := keyboard.resolveModifier(0xffe3); ok {
		t.Fatalf("resolveModifier without modifier-map membership = %+v, want no match", got)
	}
}

func TestX11ScratchCapacityFailsBeforeMapping(t *testing.T) {
	backend := &x11InputBackend{
		scratchInitialized: true,
		scratchPerKeycode:  2,
		scratchSlots:       []x11ScratchSlot{{code: 10}},
		scratchByKeysym:    make(map[uint32]xproto.Keycode),
	}
	err := backend.validateScratchCapacityLocked([]uint32{'a', 'b'}, nil)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("scratch capacity error = %v, want ErrNotSupported", err)
	}
	if backend.scratchSlots[0].keysym != 0 || len(backend.scratchByKeysym) != 0 {
		t.Fatalf("capacity failure mutated scratch state: slots=%+v mappings=%v", backend.scratchSlots, backend.scratchByKeysym)
	}
}

func TestX11ScratchCapacityExcludesPressedEmptyKeycodes(t *testing.T) {
	backend := &x11InputBackend{
		scratchInitialized: true,
		scratchPerKeycode:  2,
		scratchSlots:       []x11ScratchSlot{{code: 10}},
		scratchByKeysym:    make(map[uint32]xproto.Keycode),
	}
	pressed := make([]byte, 32)
	pressed[10/8] |= 1 << uint(10%8)
	if err := backend.validateScratchCapacityLocked([]uint32{'a'}, pressed); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("pressed scratch capacity error = %v, want ErrNotSupported", err)
	}
}

func TestX11ScratchCleanupUsesCurrentMappingWidth(t *testing.T) {
	const keysym = uint32(0x010020ac)
	current := &xproto.GetKeyboardMappingReply{
		KeysymsPerKeycode: 3,
		Keysyms: []xproto.Keysym{
			xproto.Keysym(keysym), xproto.Keysym(keysym), xproto.Keysym(keysym),
		},
	}
	width, owned, err := x11ScratchMappingCanRestore(current, keysym)
	if err != nil || !owned || width != 3 {
		t.Fatalf("restore plan = (width=%d owned=%t err=%v), want (3,true,nil)", width, owned, err)
	}
	current.Keysyms[2] = 'x'
	if _, owned, err := x11ScratchMappingCanRestore(current, keysym); err != nil || owned {
		t.Fatalf("foreign restore plan = (owned=%t err=%v), want (false,nil)", owned, err)
	}
}

func TestX11ScratchOwnershipRecognizesServerCanonicalization(t *testing.T) {
	keysym := uint32(0x010020ac)
	for _, test := range []struct {
		mapping []xproto.Keysym
		want    bool
	}{
		{mapping: []xproto.Keysym{xproto.Keysym(keysym), xproto.Keysym(keysym), 0}, want: true},
		{mapping: []xproto.Keysym{xproto.Keysym(keysym)}, want: true},
		{mapping: []xproto.Keysym{0, 0}},
		{mapping: []xproto.Keysym{xproto.Keysym(keysym), 'x'}},
	} {
		if got := x11MappingOwnedBy(test.mapping, keysym); got != test.want {
			t.Fatalf("x11MappingOwnedBy(%v,%#x) = %t, want %t", test.mapping, keysym, got, test.want)
		}
	}
}

func TestX11ScratchOwnershipRejectsModifierReassignment(t *testing.T) {
	const keysym = uint32(0x010020ac)
	backend := &x11InputBackend{
		scratchInitialized: true,
		scratchPerKeycode:  2,
		scratchSlots:       []x11ScratchSlot{{code: 10, keysym: keysym}},
		scratchByKeysym:    map[uint32]xproto.Keycode{keysym: 10},
	}
	keyboard := x11KeyboardMap{
		minimum:    10,
		perKeycode: 2,
		keysyms:    []xproto.Keysym{xproto.Keysym(keysym), xproto.Keysym(keysym)},
		modifiers:  map[xproto.Keycode]struct{}{10: {}},
	}
	if err := backend.updateScratchStateLocked(&keyboard); err == nil {
		t.Fatal("owned scratch keycode becoming a modifier unexpectedly succeeded")
	}
}

func TestX11VersionAndLiteralContracts(t *testing.T) {
	for _, test := range []struct {
		version *xtest.GetVersionReply
		want    bool
	}{
		{version: nil},
		{version: &xtest.GetVersionReply{MajorVersion: 2, MinorVersion: 1}},
		{version: &xtest.GetVersionReply{MajorVersion: 2, MinorVersion: 2}, want: true},
		{version: &xtest.GetVersionReply{MajorVersion: 3}, want: true},
	} {
		if got := x11XTestVersionSupported(test.version); got != test.want {
			t.Fatalf("x11XTestVersionSupported(%+v) = %t, want %t", test.version, got, test.want)
		}
	}
	for key, want := range map[string]bool{"a": true, "€": true, "enter": false, "F12": false} {
		if got := x11LiteralKey(key); got != want {
			t.Fatalf("x11LiteralKey(%q) = %t, want %t", key, got, want)
		}
	}
	if _, err := x11Coordinate(math.MaxInt16 + 1); err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("coordinate validation error = %v, want non-ErrNotSupported", err)
	}
	if _, err := x11MouseButton("invalid"); err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("button validation error = %v, want non-ErrNotSupported", err)
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
			t.Fatalf("x11EventKeysym(%q,%v) = (%#x,%v), want (%#x,nil)",
				test.key, test.modifiers, got, err, test.want)
		}
	}
}

func TestX11PointerArgumentsAreStrictlyBounded(t *testing.T) {
	for _, value := range []int{math.MinInt16, 0, math.MaxInt16} {
		if got, err := x11Coordinate(value); err != nil || int(got) != value {
			t.Fatalf("x11Coordinate(%d) = (%d,%v)", value, got, err)
		}
	}
	for _, value := range []int{math.MinInt16 - 1, math.MaxInt16 + 1} {
		if _, err := x11Coordinate(value); err == nil {
			t.Fatalf("x11Coordinate(%d) unexpectedly succeeded", value)
		}
	}
	for value, want := range map[int]uint64{-1000: 1000, 0: 0, 1000: 1000} {
		if got, err := x11ScrollSteps(value); err != nil || got != want {
			t.Fatalf("x11ScrollSteps(%d) = (%d,%v), want (%d,nil)", value, got, err, want)
		}
	}
	for _, value := range []int{-1001, 1001, math.MinInt, math.MaxInt} {
		if _, err := x11ScrollSteps(value); err == nil {
			t.Fatalf("x11ScrollSteps(%d) unexpectedly succeeded", value)
		}
	}
}

func TestX11MouseButtonRejectsUnknownNames(t *testing.T) {
	tests := map[string]byte{
		"": x11ButtonLeft, "left": x11ButtonLeft, "center": x11ButtonMiddle,
		"middle": x11ButtonMiddle, "right": x11ButtonRight,
		"wheelUp": x11ButtonWheelUp, "wheelDown": x11ButtonWheelDown,
		"wheelLeft": x11ButtonWheelLeft, "wheelRight": x11ButtonWheelRight,
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
	for _, button := range []byte{x11ButtonLeft, x11ButtonMiddle, x11ButtonRight, x11ButtonWheelUp, x11ButtonWheelDown} {
		if !x11ButtonStateObservable(button) {
			t.Fatalf("button %d state unexpectedly unobservable", button)
		}
	}
	for _, button := range []byte{x11ButtonWheelLeft, x11ButtonWheelRight} {
		if x11ButtonStateObservable(button) {
			t.Fatalf("button %d state unexpectedly observable", button)
		}
	}
}

func TestX11BackendResolverNeverUsesX11InWaylandSession(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	if backend := platformPureGoInputBackend(); backend != nil {
		t.Fatalf("Wayland session selected X11 backend %q", backend.Name())
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	if backend := platformPureGoInputBackend(); backend == nil || backend.Name() != featureBackendPureGoX11 {
		t.Fatalf("X11 session backend = %#v, want %q", backend, featureBackendPureGoX11)
	}
}

func TestX11CapabilityInspectionDoesNotWaitForBackendMutex(t *testing.T) {
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, ":99")
	t.Setenv(envXDGSessionType, "x11")
	linuxX11Input.mu.Lock()
	done := make(chan RuntimeCapabilities, 1)
	go func() { done <- GetRuntimeCapabilities() }()
	select {
	case capabilities := <-done:
		linuxX11Input.mu.Unlock()
		if !capabilities.Keyboard.Available || !capabilities.Mouse.Available {
			t.Fatalf("X11 capabilities = keyboard %+v, mouse %+v", capabilities.Keyboard, capabilities.Mouse)
		}
	case <-time.After(250 * time.Millisecond):
		linuxX11Input.mu.Unlock()
		<-done
		t.Fatal("capability inspection waited for the persistent X11 backend mutex")
	}
}

func TestX11CapabilityInspectionDoesNotCleanUpDeselectedBackend(t *testing.T) {
	t.Setenv(envWaylandDisplay, "wayland-test")
	t.Setenv(envDisplay, ":99")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	linuxX11Input.mu.Lock()
	done := make(chan RuntimeCapabilities, 1)
	go func() { done <- GetRuntimeCapabilities() }()
	select {
	case capabilities := <-done:
		linuxX11Input.mu.Unlock()
		if capabilities.Keyboard.Backend == featureBackendPureGoX11 ||
			capabilities.Mouse.Backend == featureBackendPureGoX11 {
			t.Fatalf("Wayland capabilities selected Pure-Go X11: %+v", capabilities)
		}
	case <-time.After(250 * time.Millisecond):
		linuxX11Input.mu.Unlock()
		<-done
		t.Fatal("capability inspection tried to clean up the deselected X11 backend")
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
