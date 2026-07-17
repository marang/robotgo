//go:build !cgo

package robotgo

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

type fakePureGoInputBackend struct {
	name         string
	calls        []string
	location     Point
	keyboardErr  error
	mouseErr     error
	operationErr error
}

func (backend *fakePureGoInputBackend) Name() string {
	if backend.name != "" {
		return backend.name
	}
	return "pure-go-test-input"
}

func (backend *fakePureGoInputBackend) record(format string, args ...interface{}) error {
	backend.calls = append(backend.calls, fmt.Sprintf(format, args...))
	return backend.operationErr
}

func (backend *fakePureGoInputBackend) KeyboardReady() error {
	backend.calls = append(backend.calls, "keyboard-ready")
	return backend.keyboardErr
}

func (backend *fakePureGoInputBackend) MouseReady() error {
	backend.calls = append(backend.calls, "mouse-ready")
	return backend.mouseErr
}

func (backend *fakePureGoInputBackend) Key(event pureGoKeyEvent) error {
	return backend.record("key:%s:%v:%d:%t:%t", event.Key, event.Modifiers, event.PID, event.Down, event.Tap)
}

func (backend *fakePureGoInputBackend) Text(event pureGoTextEvent) error {
	return backend.record("text:%s:%d:%d", event.Text, event.PID, event.Delay)
}

func (backend *fakePureGoInputBackend) MoveAbsolute(x, y int, displayID []int) error {
	return backend.record("move:%d:%d:%v", x, y, displayID)
}

func (backend *fakePureGoInputBackend) MoveRelative(x, y int) error {
	return backend.record("relative:%d:%d", x, y)
}

func (backend *fakePureGoInputBackend) MoveSmooth(x, y int, relative bool, lowDelay, highDelay float64) error {
	return backend.record("smooth:%d:%d:%t:%g:%g", x, y, relative, lowDelay, highDelay)
}

func (backend *fakePureGoInputBackend) DragSmooth(x, y int, lowDelay, highDelay float64) error {
	return backend.record("drag:%d:%d:%g:%g", x, y, lowDelay, highDelay)
}

func (backend *fakePureGoInputBackend) Location() (int, int, error) {
	backend.calls = append(backend.calls, "location")
	return backend.location.X, backend.location.Y, backend.operationErr
}

func (backend *fakePureGoInputBackend) Click(button string, double bool) error {
	return backend.record("click:%s:%t", button, double)
}

func (backend *fakePureGoInputBackend) Toggle(button string, down bool) error {
	return backend.record("toggle:%s:%t", button, down)
}

func (backend *fakePureGoInputBackend) Scroll(x, y int) error {
	return backend.record("scroll:%d:%d", x, y)
}

func (backend *fakePureGoInputBackend) Close() error {
	backend.calls = append(backend.calls, "close")
	return backend.operationErr
}

func installFakePureGoInputBackend(t *testing.T, backend pureGoInputBackend) {
	t.Helper()
	previous := resolvePureGoInputBackend
	resolvePureGoInputBackend = func() pureGoInputBackend { return backend }
	t.Cleanup(func() { resolvePureGoInputBackend = previous })
}

func zeroInputDelays(t *testing.T) {
	t.Helper()
	previous := GetRuntimeConfig()
	config := previous
	config.MouseDelay = 0
	config.KeyDelay = 0
	if err := SetRuntimeConfig(config); err != nil {
		t.Fatalf("SetRuntimeConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := SetRuntimeConfig(previous); err != nil {
			t.Errorf("restore runtime config: %v", err)
		}
	})
}

func TestNonCGOInputDispatchUsesSelectedPlatformBackend(t *testing.T) {
	backend := &fakePureGoInputBackend{location: Point{X: -12, Y: 34}}
	installFakePureGoInputBackend(t, backend)
	zeroInputDelays(t)

	if err := KeyboardReady(); err != nil {
		t.Fatalf("KeyboardReady: %v", err)
	}
	if err := MouseReady(); err != nil {
		t.Fatalf("MouseReady: %v", err)
	}
	if err := KeyTap("a", "ctrl"); err != nil {
		t.Fatalf("KeyTap: %v", err)
	}
	if err := KeyTap("A", "ctrl"); err != nil {
		t.Fatalf("uppercase KeyTap: %v", err)
	}
	if err := KeyTap("+"); err != nil {
		t.Fatalf("special-symbol KeyTap: %v", err)
	}
	if err := KeyPress("b", []string{"shift"}); err != nil {
		t.Fatalf("KeyPress: %v", err)
	}
	if err := KeyToggle("c", "up", "alt"); err != nil {
		t.Fatalf("KeyToggle: %v", err)
	}
	if err := KeyDown("d", 0, []string{"ctrl"}); err != nil {
		t.Fatalf("KeyDown: %v", err)
	}
	if err := KeyUp("d", []string{"ctrl"}, 0); err != nil {
		t.Fatalf("KeyUp: %v", err)
	}
	if err := TypeStrE("text", 0, 7); err != nil {
		t.Fatalf("TypeStrE: %v", err)
	}
	if err := UnicodeTypeE('€'); err != nil {
		t.Fatalf("UnicodeTypeE: %v", err)
	}
	if err := MoveE(10, 20, 2); err != nil {
		t.Fatalf("MoveE: %v", err)
	}
	if err := MoveRelativeE(-3, 4); err != nil {
		t.Fatalf("MoveRelativeE: %v", err)
	}
	if !MoveSmooth(30, 40, 1.0, 2.0, 0) {
		t.Fatal("MoveSmooth returned false")
	}
	MoveSmoothRelative(3, -2, 1.0, 2.0, 0)
	DragSmooth(50, 60, 1.0, 2.0, 0)
	if err := ClickE("right", true); err != nil {
		t.Fatalf("ClickE: %v", err)
	}
	if err := Toggle("center", "up"); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if err := ScrollE(1, -2, 0); err != nil {
		t.Fatalf("ScrollE: %v", err)
	}
	x, y, err := LocationE()
	if err != nil || x != -12 || y != 34 {
		t.Fatalf("LocationE = (%d,%d,%v), want (-12,34,nil)", x, y, err)
	}

	want := []string{
		"keyboard-ready",
		"mouse-ready",
		"key:a:[ctrl]:0:true:true",
		"key:A:[ctrl shift]:0:true:true",
		"key:+:[shift]:0:true:true",
		"key:b:[shift]:0:true:true",
		"key:c:[alt]:0:false:false",
		"key:d:[ctrl]:0:true:false",
		"key:d:[ctrl]:0:false:false",
		"text:text:0:7",
		"text:€:0:0",
		"move:10:20:[2]",
		"relative:-3:4",
		"smooth:30:40:false:1:2",
		"smooth:3:-2:true:1:2",
		"drag:50:60:1:2",
		"click:right:true",
		"toggle:center:false",
		"scroll:1:-2",
		"location",
	}
	if !reflect.DeepEqual(backend.calls, want) {
		t.Fatalf("backend calls = %#v, want %#v", backend.calls, want)
	}
}

func TestNonCGOKeyArgumentsRejectMultipleProcessIDs(t *testing.T) {
	backend := &fakePureGoInputBackend{}
	installFakePureGoInputBackend(t, backend)
	if err := KeyTap("a", 1, 2); err == nil {
		t.Fatal("KeyTap accepted multiple process IDs")
	}
	if len(backend.calls) != 0 {
		t.Fatalf("invalid key arguments reached backend: %#v", backend.calls)
	}
}

func TestNonCGOSmoothMoveRejectsInvalidLegacyArguments(t *testing.T) {
	backend := &fakePureGoInputBackend{}
	installFakePureGoInputBackend(t, backend)
	for _, args := range [][]interface{}{
		{1, 2},
		{3.0, 2.0},
		{1.0, 2.0, -1},
		{math.NaN(), 2.0},
		{1.0, math.Inf(1)},
	} {
		if MoveSmooth(1, 2, args...) {
			t.Fatalf("MoveSmooth accepted invalid args %#v", args)
		}
	}
	if len(backend.calls) != 0 {
		t.Fatalf("invalid smooth move reached backend: %#v", backend.calls)
	}
}

func TestNonCGOInputDispatchPreservesBackendErrors(t *testing.T) {
	wantErr := errors.New("backend injection failed")
	backend := &fakePureGoInputBackend{operationErr: wantErr}
	installFakePureGoInputBackend(t, backend)
	zeroInputDelays(t)
	if err := KeyTap("a"); !errors.Is(err, wantErr) {
		t.Fatalf("KeyTap error = %v, want %v", err, wantErr)
	}
	if err := MoveE(1, 2); !errors.Is(err, wantErr) {
		t.Fatalf("MoveE error = %v, want %v", err, wantErr)
	}
}

func TestPureGoInputCapabilitiesDoNotPerformLiveProbes(t *testing.T) {
	keyboardErr := errors.New("keyboard unavailable")
	backend := &fakePureGoInputBackend{keyboardErr: keyboardErr}
	installFakePureGoInputBackend(t, backend)
	keyboard, mouse := pureGoInputCapabilities()
	if !keyboard.Available || keyboard.Backend != backend.Name() {
		t.Fatalf("keyboard capability = %+v", keyboard)
	}
	if !mouse.Available || mouse.Backend != backend.Name() {
		t.Fatalf("mouse capability = %+v", mouse)
	}
	if len(backend.calls) != 0 {
		t.Fatalf("capability inspection performed live backend calls: %v", backend.calls)
	}
}

func TestPureGoQuartzCapabilitiesPreflightPointerWithoutClaimingKeyboard(t *testing.T) {
	mouseErr := errors.Join(ErrPermissionDenied, errors.New("Accessibility denied"))
	backend := &fakePureGoInputBackend{
		name:     featureBackendPureGoQuartzInput,
		mouseErr: mouseErr,
	}
	installFakePureGoInputBackend(t, backend)

	keyboard, mouse := pureGoInputCapabilities()
	if keyboard.Available || keyboard.Backend != featureBackendPureGoQuartzInput ||
		keyboard.Reason != ErrNotSupported.Error() {
		t.Fatalf("keyboard capability = %+v, want explicit unsupported Quartz keyboard", keyboard)
	}
	if mouse.Available || mouse.Backend != featureBackendPureGoQuartzInput {
		t.Fatalf("mouse capability = %+v, want unavailable Quartz pointer", mouse)
	}
	if !reflect.DeepEqual(backend.calls, []string{"mouse-ready"}) {
		t.Fatalf("Quartz capability probe calls = %v, want only mouse-ready", backend.calls)
	}
}

func TestUnicodeTypeRejectsInvalidScalarBeforeDispatch(t *testing.T) {
	backend := &fakePureGoInputBackend{}
	installFakePureGoInputBackend(t, backend)
	for _, value := range []uint32{0xd800, 0x110000} {
		if err := UnicodeTypeE(value); err == nil {
			t.Fatalf("UnicodeTypeE(%#x) unexpectedly succeeded", value)
		}
	}
	if len(backend.calls) != 0 {
		t.Fatalf("invalid Unicode reached backend: %#v", backend.calls)
	}
}

func TestTypeStrRejectsInvalidInputBeforeDispatch(t *testing.T) {
	backend := &fakePureGoInputBackend{}
	installFakePureGoInputBackend(t, backend)
	for _, test := range []struct {
		name string
		text string
		args []int
	}{
		{name: "negative delay", text: "text", args: []int{0, -1}},
		{name: "invalid UTF-8", text: string([]byte{0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := TypeStrE(test.text, test.args...); err == nil {
				t.Fatal("invalid text input unexpectedly succeeded")
			}
		})
	}
	if len(backend.calls) != 0 {
		t.Fatalf("invalid text input reached backend: %#v", backend.calls)
	}
}
