//go:build windows

package windowsinput

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"
	"unsafe"
)

type sendResult struct {
	inserted int
	err      error
}

type fakeSystem struct {
	keyboardErr error
	mouseErr    error
	cursorErr   error
	setErr      error
	layoutErr   error

	x int32
	y int32

	down       map[uint16]bool
	layout     map[uint16]uint16
	shiftState map[uint16]uint8
	sendPlan   []sendResult
	sends      [][]inputRecord
	positions  [][2]int32
}

func (system *fakeSystem) KeyboardReady() error { return system.keyboardErr }
func (system *fakeSystem) MouseReady() error    { return system.mouseErr }

func (system *fakeSystem) SendInput(records []inputRecord) (int, error) {
	system.sends = append(system.sends, append([]inputRecord(nil), records...))
	if len(system.sendPlan) == 0 {
		return len(records), nil
	}
	result := system.sendPlan[0]
	system.sendPlan = system.sendPlan[1:]
	return result.inserted, result.err
}

func (system *fakeSystem) CursorPosition() (int32, int32, error) {
	return system.x, system.y, system.cursorErr
}

func (system *fakeSystem) SetCursorPosition(x, y int32) error {
	if system.setErr != nil {
		return system.setErr
	}
	system.x, system.y = x, y
	system.positions = append(system.positions, [2]int32{x, y})
	return nil
}

func (system *fakeSystem) KeyDown(key uint16) bool {
	return system.down[key]
}

func (system *fakeSystem) VirtualKeyForRune(value uint16) (uint16, uint8, bool, error) {
	key, ok := system.layout[value]
	return key, system.shiftState[value], ok, system.layoutErr
}

func newFakeBackend(system *fakeSystem, sleeps *[]time.Duration) *Backend {
	if system.down == nil {
		system.down = make(map[uint16]bool)
	}
	if system.layout == nil {
		system.layout = map[uint16]uint16{
			'a': 0x41,
			'A': 0x41,
		}
	}
	if system.shiftState == nil {
		system.shiftState = map[uint16]uint8{'A': shiftStateShift}
	}
	return newBackend(system, func(delay time.Duration) {
		if sleeps != nil {
			*sleeps = append(*sleeps, delay)
		}
	})
}

func decodeKeyboard(record inputRecord) keyboardInput {
	return *(*keyboardInput)(unsafe.Pointer(&record.Payload))
}

func keyboardSummary(records []inputRecord) [][3]uint32 {
	result := make([][3]uint32, len(records))
	for index, record := range records {
		key := decodeKeyboard(record)
		result[index] = [3]uint32{uint32(key.VirtualKey), uint32(key.ScanCode), key.Flags}
	}
	return result
}

func TestInputRecordMatchesWin32Layout(t *testing.T) {
	t.Parallel()
	want := uintptr(40)
	if strconv.IntSize == 32 {
		want = 28
	}
	if got := unsafe.Sizeof(inputRecord{}); got != want {
		t.Fatalf("sizeof(INPUT) = %d, want %d", got, want)
	}
	if got := unsafe.Offsetof(inputRecord{}.Payload); got != unsafe.Alignof(uintptr(0)) {
		t.Fatalf("INPUT union offset = %d, want %d", got, unsafe.Alignof(uintptr(0)))
	}
}

func TestRequiredUser32ProceduresResolve(t *testing.T) {
	t.Parallel()
	system := newWin32System()
	if err := system.KeyboardReady(); err != nil {
		t.Fatalf("keyboard procedures: %v", err)
	}
	if err := system.MouseReady(); err != nil {
		t.Fatalf("pointer procedures: %v", err)
	}
}

func TestKeyUsesForegroundLayoutAndCallerModifiers(t *testing.T) {
	system := &fakeSystem{
		layout:     map[uint16]uint16{'@': 0x51},
		shiftState: map[uint16]uint8{'@': shiftStateControl | shiftStateAlt},
	}
	backend := newFakeBackend(system, nil)
	if err := backend.Key(KeyEvent{
		Key: "@", Modifiers: []string{"shift"}, Down: true, Tap: true,
	}); err != nil {
		t.Fatalf("Key: %v", err)
	}
	if len(system.sends) != 1 {
		t.Fatalf("SendInput calls = %d, want 1", len(system.sends))
	}
	want := [][3]uint32{
		{vkControl, 0, 0},
		{vkMenu, 0, 0},
		{vkShift, 0, 0},
		{0x51, 0, 0},
		{0x51, 0, keyEventKeyUp},
		{vkShift, 0, keyEventKeyUp},
		{vkMenu, 0, keyEventKeyUp},
		{vkControl, 0, keyEventKeyUp},
	}
	if got := keyboardSummary(system.sends[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("keyboard transaction = %#v, want %#v", got, want)
	}
	if len(backend.ownedKeys) != 0 {
		t.Fatalf("tap left owned keys: %#v", backend.ownedKeys)
	}
}

func TestKeyPreservesForegroundLayoutFailure(t *testing.T) {
	layoutErr := fmt.Errorf("%w: no foreground window", ErrUnsupported)
	system := &fakeSystem{layoutErr: layoutErr}
	backend := newFakeBackend(system, nil)
	if err := backend.Key(KeyEvent{Key: "a", Tap: true}); !errors.Is(err, layoutErr) {
		t.Fatalf("Key error = %v, want foreground-layout error", err)
	}
	if len(system.sends) != 0 {
		t.Fatalf("layout failure injected %d input transactions", len(system.sends))
	}
}

func TestKeyPreservesAlreadyHeldModifier(t *testing.T) {
	system := &fakeSystem{down: map[uint16]bool{vkControl: true}}
	backend := newFakeBackend(system, nil)
	if err := backend.Key(KeyEvent{
		Key: "a", Modifiers: []string{"ctrl"}, Down: true, Tap: true,
	}); err != nil {
		t.Fatalf("Key: %v", err)
	}
	want := [][3]uint32{
		{0x41, 0, 0},
		{0x41, 0, keyEventKeyUp},
	}
	if got := keyboardSummary(system.sends[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("keyboard transaction = %#v, want %#v", got, want)
	}
}

func TestKeyHoldRequiresOwnershipAndCloseReleases(t *testing.T) {
	system := &fakeSystem{}
	backend := newFakeBackend(system, nil)
	if err := backend.Key(KeyEvent{Key: "enter", Down: false}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("orphan key up error = %v, want ErrOwnership", err)
	}
	if err := backend.Key(KeyEvent{Key: "enter", Down: true}); err != nil {
		t.Fatalf("key down: %v", err)
	}
	if _, owned := backend.ownedKeys[vkReturn]; !owned {
		t.Fatal("key down was not tracked")
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(backend.ownedKeys) != 0 {
		t.Fatalf("Close left owned keys: %#v", backend.ownedKeys)
	}
	if got := keyboardSummary(system.sends[len(system.sends)-1]); !reflect.DeepEqual(got, [][3]uint32{
		{vkReturn, 0, keyEventKeyUp},
	}) {
		t.Fatalf("Close transaction = %#v", got)
	}
}

func TestKeyReleasePreservesExtendedIdentity(t *testing.T) {
	system := &fakeSystem{}
	backend := newFakeBackend(system, nil)
	if err := backend.Key(KeyEvent{Key: "num_enter", Down: true}); err != nil {
		t.Fatalf("numpad enter down: %v", err)
	}
	if err := backend.Key(KeyEvent{Key: "enter", Down: false}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("standard enter release error = %v, want ErrOwnership", err)
	}
	if len(system.sends) != 1 {
		t.Fatalf("mismatched release performed %d SendInput calls, want 1", len(system.sends))
	}
	if err := backend.Key(KeyEvent{Key: "num_enter", Down: false}); err != nil {
		t.Fatalf("numpad enter up: %v", err)
	}
	release := decodeKeyboard(system.sends[1][0])
	if release.VirtualKey != vkReturn ||
		release.Flags != keyEventExtended|keyEventKeyUp {
		t.Fatalf("numpad enter release = %+v", release)
	}
}

func TestPartialKeyTransactionRollsBackNewHolds(t *testing.T) {
	injectionErr := errors.New("partial injection")
	system := &fakeSystem{sendPlan: []sendResult{
		{inserted: 2, err: injectionErr},
		{inserted: 1},
		{inserted: 1},
	}}
	backend := newFakeBackend(system, nil)
	err := backend.Key(KeyEvent{
		Key: "a", Modifiers: []string{"ctrl"}, Down: true,
	})
	if !errors.Is(err, injectionErr) {
		t.Fatalf("Key error = %v, want injection error", err)
	}
	if len(backend.ownedKeys) != 0 {
		t.Fatalf("rollback left owned keys: %#v", backend.ownedKeys)
	}
	if len(system.sends) != 3 {
		t.Fatalf("SendInput calls = %d, want transaction plus two releases", len(system.sends))
	}
	gotReleaseKeys := []uint16{
		decodeKeyboard(system.sends[1][0]).VirtualKey,
		decodeKeyboard(system.sends[2][0]).VirtualKey,
	}
	if want := []uint16{0x41, vkControl}; !reflect.DeepEqual(gotReleaseKeys, want) {
		t.Fatalf("rollback keys = %#v, want %#v", gotReleaseKeys, want)
	}
}

func TestTextUsesUTF16PairsAndRuneDelay(t *testing.T) {
	system := &fakeSystem{}
	var sleeps []time.Duration
	backend := newFakeBackend(system, &sleeps)
	if err := backend.Text(TextEvent{
		Text: "😀A", Delay: 7 * time.Millisecond,
	}); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if len(system.sends) != 2 {
		t.Fatalf("SendInput calls = %d, want one per rune", len(system.sends))
	}
	first := keyboardSummary(system.sends[0])
	wantFirst := [][3]uint32{
		{0, 0xd83d, keyEventUnicode},
		{0, 0xd83d, keyEventUnicode | keyEventKeyUp},
		{0, 0xde00, keyEventUnicode},
		{0, 0xde00, keyEventUnicode | keyEventKeyUp},
	}
	if !reflect.DeepEqual(first, wantFirst) {
		t.Fatalf("surrogate transaction = %#v, want %#v", first, wantFirst)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{7 * time.Millisecond}) {
		t.Fatalf("text sleeps = %#v", sleeps)
	}
}

func TestPartialUnicodeTransactionReleasesPressedUnit(t *testing.T) {
	injectionErr := errors.New("unicode partial")
	system := &fakeSystem{sendPlan: []sendResult{
		{inserted: 1, err: injectionErr},
		{inserted: 1},
	}}
	backend := newFakeBackend(system, nil)
	err := backend.Text(TextEvent{Text: "A"})
	if !errors.Is(err, injectionErr) {
		t.Fatalf("Text error = %v, want injection error", err)
	}
	if len(backend.ownedUnicode) != 0 {
		t.Fatalf("rollback left Unicode units: %#v", backend.ownedUnicode)
	}
	release := decodeKeyboard(system.sends[1][0])
	if release.ScanCode != 'A' || release.Flags != keyEventUnicode|keyEventKeyUp {
		t.Fatalf("Unicode rollback = %+v", release)
	}
}

func TestPartialButtonTransactionReleasesPressedButton(t *testing.T) {
	injectionErr := errors.New("button partial")
	system := &fakeSystem{sendPlan: []sendResult{
		{inserted: 1, err: injectionErr},
		{inserted: 1},
	}}
	backend := newFakeBackend(system, nil)
	if err := backend.Click("left", false); !errors.Is(err, injectionErr) {
		t.Fatalf("Click error = %v, want injection error", err)
	}
	if len(backend.ownedButtons) != 0 {
		t.Fatalf("rollback left owned buttons: %#v", backend.ownedButtons)
	}
	if len(system.sends) != 2 ||
		system.sends[1][0].Payload.Flags != mouseEventLeftUp {
		t.Fatalf("button rollback transactions = %#v", system.sends)
	}
}

func TestClickDoubleAndScrollDirections(t *testing.T) {
	system := &fakeSystem{}
	var sleeps []time.Duration
	backend := newFakeBackend(system, &sleeps)
	if err := backend.Click("right", true); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{doubleClickGap}) {
		t.Fatalf("double-click sleeps = %#v", sleeps)
	}
	for index, records := range system.sends[:2] {
		if len(records) != 2 ||
			records[0].Payload.Flags != mouseEventRightDown ||
			records[1].Payload.Flags != mouseEventRightUp {
			t.Fatalf("click %d records = %#v", index, records)
		}
	}
	if err := backend.Scroll(2, -3); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	scroll := system.sends[2]
	if len(scroll) != 2 {
		t.Fatalf("scroll records = %d, want 2", len(scroll))
	}
	if got := int32(scroll[0].Payload.MouseData); scroll[0].Payload.Flags != mouseEventHWheel || got != -2*wheelDelta {
		t.Fatalf("horizontal scroll = flags %#x data %d", scroll[0].Payload.Flags, got)
	}
	if got := int32(scroll[1].Payload.MouseData); scroll[1].Payload.Flags != mouseEventWheel || got != -3*wheelDelta {
		t.Fatalf("vertical scroll = flags %#x data %d", scroll[1].Payload.Flags, got)
	}
}

func TestDoubleClickRechecksForeignButtonState(t *testing.T) {
	system := &fakeSystem{}
	backend := newBackend(system, func(time.Duration) {
		system.down[vkLButton] = true
	})
	system.down = make(map[uint16]bool)
	if err := backend.Click("left", true); !errors.Is(err, ErrOwnership) {
		t.Fatalf("Click error = %v, want ownership conflict", err)
	}
	if len(system.sends) != 1 {
		t.Fatalf("double click injected %d pulses, want only the first", len(system.sends))
	}
	if len(backend.ownedButtons) != 0 {
		t.Fatalf("double click left owned buttons: %#v", backend.ownedButtons)
	}
}

func TestPointerOwnershipAndSmoothMovement(t *testing.T) {
	system := &fakeSystem{x: -10, y: 20}
	var sleeps []time.Duration
	backend := newFakeBackend(system, &sleeps)
	if err := backend.Toggle("left", false); !errors.Is(err, ErrOwnership) {
		t.Fatalf("orphan button up error = %v, want ErrOwnership", err)
	}
	if err := backend.MoveRelative(5, -8); err != nil {
		t.Fatalf("MoveRelative: %v", err)
	}
	if system.x != -5 || system.y != 12 {
		t.Fatalf("relative position = (%d,%d), want (-5,12)", system.x, system.y)
	}
	if err := backend.MoveSmooth(11, 12, false, 0, 0); err != nil {
		t.Fatalf("MoveSmooth: %v", err)
	}
	if system.x != 11 || system.y != 12 {
		t.Fatalf("smooth position = (%d,%d), want (11,12)", system.x, system.y)
	}
	if len(sleeps) != 0 {
		t.Fatalf("zero-delay smooth move slept: %#v", sleeps)
	}
}

func TestReadyErrorsArePreserved(t *testing.T) {
	keyboardErr := errors.New("keyboard unavailable")
	mouseErr := errors.New("mouse unavailable")
	system := &fakeSystem{keyboardErr: keyboardErr, mouseErr: mouseErr}
	backend := newFakeBackend(system, nil)
	if err := backend.KeyboardReady(); !errors.Is(err, keyboardErr) {
		t.Fatalf("KeyboardReady = %v", err)
	}
	if err := backend.MouseReady(); !errors.Is(err, mouseErr) {
		t.Fatalf("MouseReady = %v", err)
	}
}

func TestPointerOperationsCheckReadinessBeforeSideEffects(t *testing.T) {
	mouseErr := errors.New("input desktop unavailable")
	system := &fakeSystem{mouseErr: mouseErr}
	backend := newFakeBackend(system, nil)
	operations := map[string]func() error{
		"move absolute": func() error { return backend.MoveAbsolute(1, 2, nil) },
		"move relative": func() error { return backend.MoveRelative(1, 2) },
		"move smooth":   func() error { return backend.MoveSmooth(1, 2, false, 0, 0) },
		"drag smooth":   func() error { return backend.DragSmooth(1, 2, 0, 0) },
		"location": func() error {
			_, _, err := backend.Location()
			return err
		},
		"click":  func() error { return backend.Click("left", false) },
		"toggle": func() error { return backend.Toggle("left", true) },
		"scroll": func() error { return backend.Scroll(0, 1) },
	}
	for name, operation := range operations {
		if err := operation(); !errors.Is(err, mouseErr) {
			t.Errorf("%s error = %v, want readiness error", name, err)
		}
	}
	if len(system.sends) != 0 {
		t.Fatalf("readiness failures injected %d input transactions", len(system.sends))
	}
	if len(system.positions) != 0 {
		t.Fatalf("readiness failures moved the pointer: %#v", system.positions)
	}
}

func TestInvalidDragIsSideEffectFree(t *testing.T) {
	system := &fakeSystem{}
	backend := newFakeBackend(system, nil)
	if err := backend.DragSmooth(10, 20, 0, maximumSmoothDelay+1); err == nil {
		t.Fatal("DragSmooth accepted an excessive delay")
	}
	if strconv.IntSize == 64 {
		outOfRange := int64(math.MaxInt32) + 1
		if err := backend.DragSmooth(int(outOfRange), 20, 0, 0); err == nil {
			t.Fatal("DragSmooth accepted an out-of-range coordinate")
		}
	}
	if len(system.sends) != 0 {
		t.Fatalf("invalid drag injected %d input transactions", len(system.sends))
	}
	if len(system.positions) != 0 {
		t.Fatalf("invalid drag moved the pointer: %#v", system.positions)
	}
}

func TestDragPlanningFailureIsSideEffectFree(t *testing.T) {
	cursorErr := errors.New("cursor query failed")
	system := &fakeSystem{cursorErr: cursorErr}
	backend := newFakeBackend(system, nil)
	if err := backend.DragSmooth(10, 20, 0, 0); !errors.Is(err, cursorErr) {
		t.Fatalf("DragSmooth error = %v, want cursor error", err)
	}
	if len(system.sends) != 0 {
		t.Fatalf("failed drag planning injected %d input transactions", len(system.sends))
	}
}
