//go:build linux && x11integration && !wayland

package robotgo_test

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/marang/robotgo"
)

const (
	envExpectedX11Implementation = "ROBOTGO_EXPECT_X11_IMPLEMENTATION"
	x11KeysymControlL            = 0xffe3
	x11KeysymControlR            = 0xffe4
	x11ParityButtonWheelRight    = 7
)

type x11ObservedKeyEvent struct {
	pressed bool
	keycode xproto.Keycode
	keysym  xproto.Keysym
}

type x11KeyboardState struct {
	minKeycode          xproto.Keycode
	keysymsPerKeycode   byte
	keysyms             []xproto.Keysym
	keycodesPerModifier byte
	modifierKeycodes    []xproto.Keycode
}

// TestX11BackendBehavioralParity is intentionally compiled into both native
// CGO and Pure-Go test binaries. It defines the shared public X11 contract;
// backend-specific safety and ownership tests remain in the Pure-Go suite.
func TestX11BackendBehavioralParity(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)

	previousConfig := robotgo.GetRuntimeConfig()
	config := previousConfig
	config.MouseDelay = 0
	config.KeyDelay = 0
	config.Scale = false
	if err := robotgo.SetRuntimeConfig(config); err != nil {
		t.Fatalf("SetRuntimeConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := robotgo.SetRuntimeConfig(previousConfig); err != nil {
			t.Errorf("restore RuntimeConfig: %v", err)
		}
	})

	harness.drainEvents()
	readinessKeyboardState := harness.keyboardState()
	readinessInputState := harness.inputState()
	capabilities := robotgo.GetRuntimeCapabilities()
	for name, capability := range map[string]robotgo.FeatureCapability{
		"capture":  capabilities.Capture,
		"keyboard": capabilities.Keyboard,
		"mouse":    capabilities.Mouse,
	} {
		if !capability.Available {
			t.Errorf("%s capability unavailable: backend=%q reason=%q notes=%q", name, capability.Backend, capability.Reason, capability.Notes)
		}
	}
	if err := robotgo.KeyboardReady(); err != nil {
		t.Errorf("KeyboardReady: %v", err)
	}
	if err := robotgo.MouseReady(); err != nil {
		t.Errorf("MouseReady: %v", err)
	}
	harness.conn.Sync()
	assertX11KeyboardStateEqual(t, readinessKeyboardState, harness.keyboardState())
	assertX11InputStateEqual(t, readinessInputState, harness.inputState())
	harness.assertNoInputEvent("input event from capability/readiness probes", 100*time.Millisecond)

	t.Run("capture", func(t *testing.T) {
		const width, height = 640, 480
		x, y, displayWidth, displayHeight, err := robotgo.GetDisplayBoundsE(0)
		if err != nil {
			t.Fatalf("GetDisplayBoundsE: %v", err)
		}
		if x != 0 || y != 0 || displayWidth != 1280 || displayHeight != 720 {
			t.Fatalf(
				"GetDisplayBoundsE = %d,%d %dx%d, want 0,0 1280x720",
				x,
				y,
				displayWidth,
				displayHeight,
			)
		}
		compatibilityImage, err := robotgo.Capture(0, 0, 2, 2)
		if err != nil {
			t.Fatalf("Capture compatibility helper: %v", err)
		}
		if got := compatibilityImage.Bounds(); got.Min.X != 0 || got.Min.Y != 0 ||
			got.Dx() != 2 || got.Dy() != 2 {
			t.Fatalf("Capture compatibility bounds = %v, want 2x2", got)
		}
		if got := robotgo.LastBackend(); got != robotgo.BackendX11 {
			t.Fatalf("Capture compatibility LastBackend = %q, want %q", got, robotgo.BackendX11)
		}
		image, err := robotgo.CaptureImg(0, 0, width, height)
		if err != nil {
			t.Fatalf("CaptureImg: %v", err)
		}
		if got := image.Bounds(); got.Dx() != width || got.Dy() != height {
			t.Fatalf("CaptureImg bounds = %v, want %dx%d", got, width, height)
		}
		assertRGBNear(t, image.At(10, 10), 0, "black Xvfb root")
		assertRGBNear(t, image.At(x11WindowX+10, x11WindowY+10), 0xffff, "white test window")
		if got := robotgo.LastBackend(); got != robotgo.BackendX11 {
			t.Fatalf("LastBackend = %q, want %q", got, robotgo.BackendX11)
		}
	})

	t.Run("pointer", func(t *testing.T) {
		harness.drainEvents()
		const targetX, targetY = 180, 170
		if err := robotgo.MoveE(targetX, targetY); err != nil {
			t.Fatalf("MoveE: %v", err)
		}
		harness.waitForEvent("absolute pointer motion", func(event xgb.Event) bool {
			motion, ok := event.(xproto.MotionNotifyEvent)
			return ok && int(motion.RootX) == targetX && int(motion.RootY) == targetY
		})
		assertPointerLocation(t, harness, targetX, targetY)

		const deltaX, deltaY = 17, 13
		if err := robotgo.MoveRelativeE(deltaX, deltaY); err != nil {
			t.Fatalf("MoveRelativeE: %v", err)
		}
		harness.waitForEvent("relative pointer motion", func(event xgb.Event) bool {
			motion, ok := event.(xproto.MotionNotifyEvent)
			return ok && int(motion.RootX) == targetX+deltaX && int(motion.RootY) == targetY+deltaY
		})
		assertPointerLocation(t, harness, targetX+deltaX, targetY+deltaY)
	})

	t.Run("buttons and scroll", func(t *testing.T) {
		harness.drainEvents()
		if err := robotgo.ClickE("left"); err != nil {
			t.Fatalf("ClickE: %v", err)
		}
		assertButtonEvents(t, harness.waitForButtonEvents(2), []x11ButtonEvent{
			{pressed: true, button: x11ButtonLeft},
			{button: x11ButtonLeft},
		})

		if err := robotgo.Toggle("right", "down"); err != nil {
			t.Fatalf("Toggle right down: %v", err)
		}
		t.Cleanup(func() { _ = robotgo.Toggle("right", "up") })
		if err := robotgo.Toggle("right", "up"); err != nil {
			t.Fatalf("Toggle right up: %v", err)
		}
		assertButtonEvents(t, harness.waitForButtonEvents(2), []x11ButtonEvent{
			{pressed: true, button: x11ButtonRight},
			{button: x11ButtonRight},
		})

		if err := robotgo.ScrollE(0, 1, 0); err != nil {
			t.Fatalf("ScrollE: %v", err)
		}
		assertButtonEvents(t, harness.waitForButtonEvents(2), []x11ButtonEvent{
			{pressed: true, button: x11ButtonWheelUp},
			{button: x11ButtonWheelUp},
		})
	})

	t.Run("horizontal wheel backend contract", func(t *testing.T) {
		harness.drainEvents()
		if err := robotgo.MoveE(x11WindowX+10, x11WindowY+10); err != nil {
			t.Fatalf("position pointer for horizontal-wheel contract: %v", err)
		}
		harness.waitForEvent("pointer motion before horizontal-wheel contract", func(event xgb.Event) bool {
			motion, ok := event.(xproto.MotionNotifyEvent)
			return ok && int(motion.RootX) == x11WindowX+10 && int(motion.RootY) == x11WindowY+10
		})
		harness.drainEvents()
		if robotgo.GetRuntimeBackendInfo().BuildImplementation == robotgo.RuntimeImplementationPureGo {
			for _, operation := range []struct {
				name string
				run  func() error
			}{
				{name: "scroll", run: func() error { return robotgo.ScrollE(1, 0, 0) }},
				{name: "wheel-left click", run: func() error { return robotgo.ClickE("wheelLeft") }},
				{name: "wheel-right click", run: func() error { return robotgo.ClickE("wheelRight") }},
			} {
				if err := operation.run(); !errors.Is(err, robotgo.ErrNotSupported) {
					t.Fatalf("Pure-Go %s error = %v, want ErrNotSupported", operation.name, err)
				}
			}
			harness.assertNoInputEvent("input event from rejected Pure-Go horizontal-wheel operation", 100*time.Millisecond)
			return
		}
		if err := robotgo.ScrollE(1, 0, 0); err != nil {
			t.Fatalf("native horizontal ScrollE: %v", err)
		}
		assertButtonEvents(t, harness.waitForButtonEvents(2), []x11ButtonEvent{
			{pressed: true, button: x11ButtonWheelLeft},
			{button: x11ButtonWheelLeft},
		})
		if err := robotgo.ClickE("wheelRight"); err != nil {
			t.Fatalf("native wheel-right ClickE: %v", err)
		}
		assertButtonEvents(t, harness.waitForButtonEvents(2), []x11ButtonEvent{
			{pressed: true, button: x11ParityButtonWheelRight},
			{button: x11ParityButtonWheelRight},
		})
	})

	t.Run("named key", func(t *testing.T) {
		harness.drainEvents()
		if err := robotgo.KeyPress("enter"); err != nil {
			t.Fatalf("KeyPress: %v", err)
		}
		events := harness.waitForKeyEvents(2)
		if !events[0].pressed || events[0].keysym != x11KeysymEnter {
			t.Fatalf("Enter press = %+v, want keysym %#x down", events[0], x11KeysymEnter)
		}
		if events[1].pressed || events[1].keycode != events[0].keycode {
			t.Fatalf("Enter release = %+v, want keycode %d up", events[1], events[0].keycode)
		}
	})

	t.Run("modifier order", func(t *testing.T) {
		harness.drainEvents()
		if err := robotgo.KeyPress("a", "ctrl"); err != nil {
			t.Fatalf("KeyPress with Ctrl: %v", err)
		}
		events := harness.waitForKeyEvents(4)
		if !events[0].pressed || !isControlKeysym(events[0].keysym) {
			t.Fatalf("event 0 = %+v, want Control down", events[0])
		}
		if !events[1].pressed || (events[1].keysym != 'a' && events[1].keysym != 'A') {
			t.Fatalf("event 1 = %+v, want A down", events[1])
		}
		if events[2].pressed || events[2].keycode != events[1].keycode {
			t.Fatalf("event 2 = %+v, want main keycode %d up", events[2], events[1].keycode)
		}
		if events[3].pressed || events[3].keycode != events[0].keycode {
			t.Fatalf("event 3 = %+v, want Control keycode %d up", events[3], events[0].keycode)
		}
	})

	t.Run("text and keyboard state", func(t *testing.T) {
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Fatalf("reset keyboard backend before text: %v", err)
		}
		harness.conn.Sync()
		_, lines, _ := startXKBOracle(t, harness)
		before := harness.keyboardState()
		const text = "RobotGo42!"
		if err := robotgo.TypeStrE(text, 0, 0, 0); err != nil {
			t.Fatalf("TypeStrE: %v", err)
		}
		if got := string(waitForXKBOracleText(t, lines, len([]rune(text)))); got != text {
			t.Fatalf("XKB oracle text = %q, want %q", got, text)
		}
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Fatalf("CloseMainDisplayE after text: %v", err)
		}
		harness.conn.Sync()
		assertX11KeyboardStateEqual(t, before, harness.keyboardState())
	})
}

func assertExpectedX11Implementation(t x11TestingT) {
	t.Helper()
	info := robotgo.GetRuntimeBackendInfo()
	if info.DisplayServer != robotgo.DisplayServerX11 {
		t.Fatalf("display server = %q, want %q", info.DisplayServer, robotgo.DisplayServerX11)
	}
	expected := os.Getenv(envExpectedX11Implementation)
	if expected == "" {
		return
	}
	if expected != string(robotgo.RuntimeImplementationNativeCGO) && expected != string(robotgo.RuntimeImplementationPureGo) {
		t.Fatalf("%s = %q, want %q or %q", envExpectedX11Implementation, expected, robotgo.RuntimeImplementationNativeCGO, robotgo.RuntimeImplementationPureGo)
	}
	if string(info.BuildImplementation) != expected {
		t.Fatalf("build implementation = %q, want %q", info.BuildImplementation, expected)
	}
	if info.CGOEnabled != (info.BuildImplementation == robotgo.RuntimeImplementationNativeCGO) {
		t.Fatalf("CGOEnabled = %v for implementation %q", info.CGOEnabled, info.BuildImplementation)
	}
}

func assertRGBNear(t *testing.T, sample color.Color, want uint32, description string) {
	t.Helper()
	red, green, blue, _ := sample.RGBA()
	const tolerance = 0x0100
	for component, got := range map[string]uint32{"red": red, "green": green, "blue": blue} {
		delta := int64(got) - int64(want)
		if delta < 0 {
			delta = -delta
		}
		if delta > tolerance {
			t.Fatalf("%s %s = %#x, want %#x (+/-%#x)", description, component, got, want, tolerance)
		}
	}
}

func assertButtonEvents(t *testing.T, got, want []x11ButtonEvent) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("button events = %+v, want %+v", got, want)
	}
}

func assertX11KeyboardStateEqual(t *testing.T, before, after x11KeyboardState) {
	t.Helper()
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("X11 keyboard state changed: %s", x11KeyboardStateDifference(before, after))
	}
}

func assertX11InputStateEqual(t *testing.T, before, after x11InputState) {
	t.Helper()
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("X11 input state changed: before=%+v after=%+v", before, after)
	}
}

func isControlKeysym(keysym xproto.Keysym) bool {
	return keysym == x11KeysymControlL || keysym == x11KeysymControlR
}

func (h *x11InputHarness) waitForKeyEvents(count int) []x11ObservedKeyEvent {
	h.t.Helper()
	events := make([]x11ObservedKeyEvent, 0, count)
	for len(events) < count {
		event := h.waitForEvent("X11 key event", func(event xgb.Event) bool {
			switch event.(type) {
			case xproto.KeyPressEvent, xproto.KeyReleaseEvent:
				return true
			default:
				return false
			}
		})
		switch value := event.(type) {
		case xproto.KeyPressEvent:
			events = append(events, x11ObservedKeyEvent{pressed: true, keycode: value.Detail, keysym: h.keysym(value.Detail)})
		case xproto.KeyReleaseEvent:
			events = append(events, x11ObservedKeyEvent{keycode: value.Detail, keysym: h.keysym(value.Detail)})
		}
	}
	return events
}

func (h *x11InputHarness) keyboardState() x11KeyboardState {
	h.t.Helper()
	setup := xproto.Setup(h.conn)
	if setup == nil {
		h.t.Fatal("X11 connection has no setup while snapshotting keyboard state")
		return x11KeyboardState{}
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	keyboard, err := xproto.GetKeyboardMapping(h.conn, setup.MinKeycode, byte(count)).Reply()
	if err != nil || keyboard == nil || keyboard.KeysymsPerKeycode == 0 {
		h.t.Fatalf("query X11 keyboard state: reply=%+v err=%v", keyboard, err)
	}
	modifiers, err := xproto.GetModifierMapping(h.conn).Reply()
	if err != nil || modifiers == nil {
		h.t.Fatalf("query X11 modifier state: reply=%+v err=%v", modifiers, err)
	}
	return x11KeyboardState{
		minKeycode:          setup.MinKeycode,
		keysymsPerKeycode:   keyboard.KeysymsPerKeycode,
		keysyms:             append([]xproto.Keysym(nil), keyboard.Keysyms...),
		keycodesPerModifier: modifiers.KeycodesPerModifier,
		modifierKeycodes:    append([]xproto.Keycode(nil), modifiers.Keycodes...),
	}
}

func x11KeyboardStateDifference(before, after x11KeyboardState) string {
	if before.minKeycode != after.minKeycode {
		return fmt.Sprintf("minimum keycode changed from %d to %d", before.minKeycode, after.minKeycode)
	}
	if before.keysymsPerKeycode != after.keysymsPerKeycode {
		return fmt.Sprintf("keysyms per keycode changed from %d to %d", before.keysymsPerKeycode, after.keysymsPerKeycode)
	}
	if len(before.keysyms) != len(after.keysyms) {
		return fmt.Sprintf("keysym count changed from %d to %d", len(before.keysyms), len(after.keysyms))
	}
	for index := range before.keysyms {
		if before.keysyms[index] != after.keysyms[index] {
			per := int(before.keysymsPerKeycode)
			keycode := int(before.minKeycode) + index/per
			return fmt.Sprintf(
				"keycode %d column %d changed from %#x to %#x",
				keycode, index%per, before.keysyms[index], after.keysyms[index],
			)
		}
	}
	if before.keycodesPerModifier != after.keycodesPerModifier {
		return fmt.Sprintf("keycodes per modifier changed from %d to %d", before.keycodesPerModifier, after.keycodesPerModifier)
	}
	if len(before.modifierKeycodes) != len(after.modifierKeycodes) {
		return fmt.Sprintf("modifier keycode count changed from %d to %d", len(before.modifierKeycodes), len(after.modifierKeycodes))
	}
	for index := range before.modifierKeycodes {
		if before.modifierKeycodes[index] != after.modifierKeycodes[index] {
			return fmt.Sprintf(
				"modifier keycode %d changed from %d to %d",
				index, before.modifierKeycodes[index], after.modifierKeycodes[index],
			)
		}
	}
	return "state differs without a field-level difference"
}

func (event x11ObservedKeyEvent) String() string {
	direction := "up"
	if event.pressed {
		direction = "down"
	}
	return fmt.Sprintf("keycode=%d keysym=%#x %s", event.keycode, event.keysym, direction)
}
