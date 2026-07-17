package darwininput

import (
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"
)

type recordedMouseEvent struct {
	eventType  uint32
	location   point
	button     uint32
	clickState int64
}

type recordedKeyEvent struct {
	key   uint16
	down  bool
	flags uint64
	pid   int32
}

type recordedUnicodeEvent struct {
	units []uint16
	down  bool
	flags uint64
	pid   int32
}

type fakeInputSystem struct {
	readyErr         error
	location         point
	locationErr      error
	buttons          map[uint32]bool
	buttonStateErr   error
	keys             map[uint16]bool
	keyStateErr      error
	modifierFlags    uint64
	modifierFlagsErr error
	mouseEvents      []recordedMouseEvent
	scrolls          [][2]int32
	keyEvents        []recordedKeyEvent
	unicodeEvents    []recordedUnicodeEvent
	postMouseCalls   int
	postMouseErrAt   int
	postKeyCalls     int
	postKeyErrAt     int
	postUnicodeCalls int
	postUnicodeErrAt int
	closeCalls       int
}

func (system *fakeInputSystem) Ready() error { return system.readyErr }

func (system *fakeInputSystem) KeyboardReady() error { return system.readyErr }

func (system *fakeInputSystem) CursorPosition() (point, error) {
	return system.location, system.locationErr
}

func (system *fakeInputSystem) ButtonDown(button uint32) (bool, error) {
	return system.buttons[button], system.buttonStateErr
}

func (system *fakeInputSystem) KeyDown(key uint16) (bool, error) {
	return system.keys[key], system.keyStateErr
}

func (system *fakeInputSystem) ModifierFlags() (uint64, error) {
	return system.modifierFlags, system.modifierFlagsErr
}

func (system *fakeInputSystem) PostMouse(
	eventType uint32,
	location point,
	button uint32,
	clickState int64,
) error {
	system.postMouseCalls++
	if system.postMouseErrAt == system.postMouseCalls {
		return errors.New("post mouse failed")
	}
	system.mouseEvents = append(system.mouseEvents, recordedMouseEvent{
		eventType: eventType, location: location,
		button: button, clickState: clickState,
	})
	system.location = location
	switch eventType {
	case eventLeftMouseDown, eventRightMouseDown, eventOtherMouseDown:
		system.buttons[button] = true
	case eventLeftMouseUp, eventRightMouseUp, eventOtherMouseUp:
		system.buttons[button] = false
	}
	return nil
}

func (system *fakeInputSystem) PostScroll(horizontal, vertical int32) error {
	system.scrolls = append(system.scrolls, [2]int32{horizontal, vertical})
	return nil
}

func (system *fakeInputSystem) PostKey(key uint16, down bool, flags uint64, pid int32) error {
	system.postKeyCalls++
	if system.postKeyErrAt == system.postKeyCalls {
		return errors.New("post key failed")
	}
	system.keyEvents = append(system.keyEvents, recordedKeyEvent{
		key: key, down: down, flags: flags, pid: pid,
	})
	system.keys[key] = down
	switch key {
	case keyShift, keyRightShift:
		if system.keys[keyShift] || system.keys[keyRightShift] {
			system.modifierFlags |= eventFlagMaskShift
		} else {
			system.modifierFlags &^= eventFlagMaskShift
		}
	case keyControl, keyRightControl:
		if system.keys[keyControl] || system.keys[keyRightControl] {
			system.modifierFlags |= eventFlagMaskControl
		} else {
			system.modifierFlags &^= eventFlagMaskControl
		}
	case keyOption, keyRightOption:
		if system.keys[keyOption] || system.keys[keyRightOption] {
			system.modifierFlags |= eventFlagMaskAlternate
		} else {
			system.modifierFlags &^= eventFlagMaskAlternate
		}
	case keyCommand, keyRightCommand:
		if system.keys[keyCommand] || system.keys[keyRightCommand] {
			system.modifierFlags |= eventFlagMaskCommand
		} else {
			system.modifierFlags &^= eventFlagMaskCommand
		}
	}
	return nil
}

func (system *fakeInputSystem) PostUnicode(
	units []uint16,
	down bool,
	flags uint64,
	pid int32,
) error {
	system.postUnicodeCalls++
	if system.postUnicodeErrAt == system.postUnicodeCalls {
		return errors.New("post unicode failed")
	}
	system.unicodeEvents = append(system.unicodeEvents, recordedUnicodeEvent{
		units: append([]uint16(nil), units...),
		down:  down, flags: flags, pid: pid,
	})
	return nil
}

func (system *fakeInputSystem) Close() error {
	system.closeCalls++
	return nil
}

func testBackend(system *fakeInputSystem, sleeps *[]time.Duration) *Backend {
	if system.buttons == nil {
		system.buttons = make(map[uint32]bool)
	}
	if system.keys == nil {
		system.keys = make(map[uint16]bool)
	}
	return newBackend(
		func() (inputSystem, error) { return system, nil },
		func(delay time.Duration) { *sleeps = append(*sleeps, delay) },
	)
}

func TestMouseReadyIsLazyAndPreservesPermissionError(t *testing.T) {
	wantErr := errors.Join(ErrPermission, errors.New("denied"))
	system := &fakeInputSystem{buttons: make(map[uint32]bool), readyErr: wantErr}
	openCalls := 0
	backend := newBackend(func() (inputSystem, error) {
		openCalls++
		return system, nil
	}, nil)

	if err := backend.MouseReady(); !errors.Is(err, ErrPermission) {
		t.Fatalf("MouseReady error = %v, want ErrPermission", err)
	}
	if err := backend.MouseReady(); !errors.Is(err, ErrPermission) {
		t.Fatalf("second MouseReady error = %v, want ErrPermission", err)
	}
	if openCalls != 1 {
		t.Fatalf("native opens = %d, want one cached system", openCalls)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if system.closeCalls != 1 {
		t.Fatalf("native closes = %d, want 1", system.closeCalls)
	}
}

func TestMouseReadyRetriesFailedNativeOpen(t *testing.T) {
	openCalls := 0
	backend := newBackend(func() (inputSystem, error) {
		openCalls++
		return nil, ErrUnsupported
	}, nil)
	for attempt := 0; attempt < 2; attempt++ {
		if err := backend.MouseReady(); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("MouseReady attempt %d error = %v, want ErrUnsupported", attempt+1, err)
		}
	}
	if openCalls != 2 {
		t.Fatalf("failed native opens = %d, want retry on each call", openCalls)
	}
}

func TestPointerMoveLocationAndRelativeCoordinates(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{location: point{X: -10.4, Y: 20.6}}
	backend := testBackend(system, &sleeps)

	x, y, err := backend.Location()
	if err != nil || x != -10 || y != 20 {
		t.Fatalf("Location = (%d,%d,%v), want (-10,20,nil)", x, y, err)
	}
	if err := backend.MoveRelative(3, -4); err != nil {
		t.Fatalf("MoveRelative: %v", err)
	}
	if err := backend.MoveAbsolute(-50, 60); err != nil {
		t.Fatalf("MoveAbsolute: %v", err)
	}
	want := []recordedMouseEvent{
		{eventType: eventMouseMoved, location: point{X: -7, Y: 16}, button: buttonLeft},
		{eventType: eventMouseMoved, location: point{X: -50, Y: 60}, button: buttonLeft},
	}
	if !reflect.DeepEqual(system.mouseEvents, want) {
		t.Fatalf("mouse events = %#v, want %#v", system.mouseEvents, want)
	}
	if strconv.IntSize == 64 {
		overflow := maximumCoordinate + 1
		if err := backend.MoveAbsolute(int(overflow), 0); err == nil {
			t.Fatal("inexact CoreGraphics coordinate unexpectedly succeeded")
		}
	}
}

func TestPointerRejectsInvalidNativeCoordinates(t *testing.T) {
	for _, location := range []point{
		{X: math.NaN()},
		{Y: math.Inf(1)},
		{X: float64(maximumCoordinate + 2)},
	} {
		var sleeps []time.Duration
		backend := testBackend(&fakeInputSystem{location: location}, &sleeps)
		if _, _, err := backend.Location(); err == nil {
			t.Fatalf("Location unexpectedly accepted %+v", location)
		}
	}
}

func TestSmoothMoveIsBoundedAndEndsAtTarget(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{location: point{X: 0, Y: 0}}
	backend := testBackend(system, &sleeps)

	if err := backend.MoveSmooth(24, 0, false, 1, 3); err != nil {
		t.Fatalf("MoveSmooth: %v", err)
	}
	if len(system.mouseEvents) != 3 {
		t.Fatalf("smooth events = %d, want 3", len(system.mouseEvents))
	}
	last := system.mouseEvents[len(system.mouseEvents)-1]
	if last.location != (point{X: 24, Y: 0}) || last.eventType != eventMouseMoved {
		t.Fatalf("last smooth event = %+v, want target mouse-moved", last)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{2 * time.Millisecond, 2 * time.Millisecond}) {
		t.Fatalf("smooth sleeps = %v, want two 2ms intervals", sleeps)
	}
	for _, delays := range [][2]float64{
		{-1, 1},
		{2, 1},
		{0, math.NaN()},
		{0, math.Inf(1)},
		{0, maximumSmoothDelay + 1},
	} {
		if err := backend.MoveSmooth(1, 1, false, delays[0], delays[1]); err == nil {
			t.Fatalf("invalid delays %v unexpectedly succeeded", delays)
		}
	}
}

func TestDoubleClickUsesMatchingClickStates(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{}
	backend := testBackend(system, &sleeps)

	if err := backend.Click("right", true); err != nil {
		t.Fatalf("Click: %v", err)
	}
	want := []recordedMouseEvent{
		{eventType: eventRightMouseDown, button: buttonRight, clickState: 1},
		{eventType: eventRightMouseUp, button: buttonRight, clickState: 1},
		{eventType: eventRightMouseDown, button: buttonRight, clickState: 2},
		{eventType: eventRightMouseUp, button: buttonRight, clickState: 2},
	}
	if !reflect.DeepEqual(system.mouseEvents, want) {
		t.Fatalf("double-click events = %#v, want %#v", system.mouseEvents, want)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{doubleClickGap}) {
		t.Fatalf("double-click sleeps = %v, want %v", sleeps, doubleClickGap)
	}
}

func TestPointerButtonMappingsAndWheelContract(t *testing.T) {
	for _, test := range []struct {
		name       string
		down, up   uint32
		buttonCode uint32
	}{
		{name: "left", down: eventLeftMouseDown, up: eventLeftMouseUp, buttonCode: buttonLeft},
		{name: "right", down: eventRightMouseDown, up: eventRightMouseUp, buttonCode: buttonRight},
		{name: "middle", down: eventOtherMouseDown, up: eventOtherMouseUp, buttonCode: buttonCenter},
	} {
		var sleeps []time.Duration
		system := &fakeInputSystem{}
		backend := testBackend(system, &sleeps)
		if err := backend.Click(test.name, false); err != nil {
			t.Fatalf("Click(%q): %v", test.name, err)
		}
		if len(system.mouseEvents) != 2 ||
			system.mouseEvents[0].eventType != test.down ||
			system.mouseEvents[1].eventType != test.up ||
			system.mouseEvents[0].button != test.buttonCode {
			t.Fatalf("Click(%q) events = %#v", test.name, system.mouseEvents)
		}
	}

	var sleeps []time.Duration
	backend := testBackend(&fakeInputSystem{}, &sleeps)
	for _, name := range []string{"wheelUp", "wheelDown", "wheelLeft", "wheelRight"} {
		if err := backend.Click(name, false); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("Click(%q) error = %v, want ErrUnsupported", name, err)
		}
	}
	if err := backend.Click("side", false); err == nil {
		t.Fatal("unknown pointer button unexpectedly succeeded")
	}
}

func TestToggleRejectsForeignStateAndCloseReleasesOwnedButtons(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{
		location: point{X: 12, Y: 34},
		buttons:  map[uint32]bool{buttonRight: true},
	}
	backend := testBackend(system, &sleeps)

	if err := backend.Toggle("right", true); !errors.Is(err, ErrOwnership) {
		t.Fatalf("foreign right-button error = %v, want ErrOwnership", err)
	}
	if err := backend.Toggle("left", false); !errors.Is(err, ErrOwnership) {
		t.Fatalf("unowned left release = %v, want ErrOwnership", err)
	}
	if err := backend.Toggle("left", true); err != nil {
		t.Fatalf("left down: %v", err)
	}
	system.locationErr = errors.New("location unavailable during close")
	if err := backend.Close(); err != nil {
		t.Fatalf("Close with cached held location: %v", err)
	}
	wantTypes := []uint32{eventLeftMouseDown, eventLeftMouseUp}
	var gotTypes []uint32
	for _, event := range system.mouseEvents {
		gotTypes = append(gotTypes, event.eventType)
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("event types = %v, want %v", gotTypes, wantTypes)
	}
	if system.buttons[buttonLeft] {
		t.Fatal("Close left a RobotGo-owned button down")
	}
	if system.closeCalls != 1 {
		t.Fatalf("native closes = %d, want 1", system.closeCalls)
	}
}

func TestCloseKeepsBackendOpenWhenOwnedReleaseFails(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{location: point{X: 1, Y: 2}}
	backend := testBackend(system, &sleeps)

	if err := backend.Toggle("left", true); err != nil {
		t.Fatalf("left down: %v", err)
	}
	system.postMouseErrAt = 2
	if err := backend.Close(); err == nil {
		t.Fatal("Close unexpectedly hid owned-button release failure")
	}
	if system.closeCalls != 0 {
		t.Fatalf("native closes = %d, want backend retained for retry", system.closeCalls)
	}
	system.postMouseErrAt = 0
	if err := backend.Close(); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if system.closeCalls != 1 {
		t.Fatalf("native closes after retry = %d, want 1", system.closeCalls)
	}
}

func TestDragAlwaysAttemptsReleaseAndScrollPreservesAxes(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{location: point{X: 0, Y: 0}}
	backend := testBackend(system, &sleeps)

	system.postMouseErrAt = 2
	if err := backend.DragSmooth(16, 0, 0, 0); err == nil {
		t.Fatal("DragSmooth unexpectedly hid move failure")
	}
	if system.buttons[buttonLeft] {
		t.Fatal("DragSmooth failure left the owned button down")
	}
	if len(system.mouseEvents) != 2 ||
		system.mouseEvents[0].eventType != eventLeftMouseDown ||
		system.mouseEvents[1].eventType != eventLeftMouseUp {
		t.Fatalf("drag events = %#v, want down then attempted move then up", system.mouseEvents)
	}
	if err := backend.Scroll(-3, 4); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	if !reflect.DeepEqual(system.scrolls, [][2]int32{{-3, 4}}) {
		t.Fatalf("scrolls = %v, want horizontal=-3 vertical=4", system.scrolls)
	}
}

func TestZeroScrollDoesNotOpenNativeSystem(t *testing.T) {
	openCalls := 0
	backend := newBackend(func() (inputSystem, error) {
		openCalls++
		return &fakeInputSystem{}, nil
	}, nil)
	if err := backend.Scroll(0, 0); err != nil {
		t.Fatalf("zero Scroll: %v", err)
	}
	if openCalls != 0 {
		t.Fatalf("zero Scroll native opens = %d, want 0", openCalls)
	}
}

func TestScrollRejectsInt32Overflow(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("Go int cannot represent an overflowing CoreGraphics scroll delta")
	}
	var sleeps []time.Duration
	backend := testBackend(&fakeInputSystem{}, &sleeps)
	overflow := int64(1) << 31
	if err := backend.Scroll(int(overflow), 0); err == nil {
		t.Fatal("overflowing horizontal scroll unexpectedly succeeded")
	}
}
