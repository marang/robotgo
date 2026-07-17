package darwininput

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestKeyboardReadyUsesLazyNonPromptingPreflight(t *testing.T) {
	wantErr := errors.Join(ErrPermission, errors.New("denied"))
	system := &fakeInputSystem{readyErr: wantErr}
	openCalls := 0
	backend := newBackend(func() (inputSystem, error) {
		openCalls++
		if system.buttons == nil {
			system.buttons = make(map[uint32]bool)
		}
		if system.keys == nil {
			system.keys = make(map[uint16]bool)
		}
		return system, nil
	}, nil)

	for attempt := 0; attempt < 2; attempt++ {
		if err := backend.KeyboardReady(); !errors.Is(err, ErrPermission) {
			t.Fatalf("KeyboardReady attempt %d error = %v, want ErrPermission", attempt+1, err)
		}
	}
	if openCalls != 1 {
		t.Fatalf("native opens = %d, want one cached system", openCalls)
	}
}

func TestKeyTapPostsModifiersAndProcessTarget(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{}
	backend := testBackend(system, &sleeps)

	err := backend.Key(KeyEvent{
		Key: "A", Modifiers: []string{"shift"},
		PID: 42, Tap: true,
	})
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	want := []recordedKeyEvent{
		{key: keyShift, down: true, flags: eventFlagMaskShift, pid: 42},
		{key: keyA, down: true, flags: eventFlagMaskShift, pid: 42},
		{key: keyA, flags: eventFlagMaskShift, pid: 42},
		{key: keyShift, pid: 42},
	}
	if !reflect.DeepEqual(system.keyEvents, want) {
		t.Fatalf("key events = %#v, want %#v", system.keyEvents, want)
	}
	if len(backend.ownedKeys) != 0 {
		t.Fatalf("tap left owned keys = %#v", backend.ownedKeys)
	}
}

func TestShiftedDigitUsesMatchingPhysicalKey(t *testing.T) {
	for character, code := range map[string]uint16{
		"!": key1, "@": key2, "#": key3, "$": key4, "%": key5,
		"^": key6, "&": key7, "*": key8, "(": key9, ")": key0,
	} {
		t.Run(character, func(t *testing.T) {
			key, err := resolveKey(character)
			if err != nil {
				t.Fatalf("resolveKey(%q): %v", character, err)
			}
			if key.code != code {
				t.Fatalf("resolveKey(%q) = %#x, want %#x", character, key.code, code)
			}
		})
	}
}

func TestKeypadEventsKeepNumericPadFlag(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{}
	backend := testBackend(system, &sleeps)

	if err := backend.Key(KeyEvent{Key: "num1", Tap: true}); err != nil {
		t.Fatalf("keypad tap: %v", err)
	}
	want := []recordedKeyEvent{
		{key: keyKeypad1, down: true, flags: eventFlagMaskNumericPad},
		{key: keyKeypad1, flags: eventFlagMaskNumericPad},
	}
	if !reflect.DeepEqual(system.keyEvents, want) {
		t.Fatalf("keypad events = %#v, want %#v", system.keyEvents, want)
	}
}

func TestKeyTapPreservesForeignModifier(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{
		keys:          map[uint16]bool{keyShift: true},
		modifierFlags: eventFlagMaskShift,
	}
	backend := testBackend(system, &sleeps)

	if err := backend.Key(KeyEvent{
		Key: "a", Modifiers: []string{"shift"}, Tap: true,
	}); err != nil {
		t.Fatalf("Key: %v", err)
	}
	want := []recordedKeyEvent{
		{key: keyA, down: true, flags: eventFlagMaskShift},
		{key: keyA, flags: eventFlagMaskShift},
	}
	if !reflect.DeepEqual(system.keyEvents, want) {
		t.Fatalf("key events = %#v, want %#v", system.keyEvents, want)
	}
	if !system.keys[keyShift] {
		t.Fatal("tap released a foreign Shift hold")
	}
}

func TestReleasingOneSidedModifierPreservesOtherSideFlag(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{
		keys:          map[uint16]bool{keyShift: true},
		modifierFlags: eventFlagMaskShift,
	}
	backend := testBackend(system, &sleeps)

	if err := backend.Key(KeyEvent{Key: "rshift", Tap: true}); err != nil {
		t.Fatalf("right Shift tap: %v", err)
	}
	want := []recordedKeyEvent{
		{key: keyRightShift, down: true, flags: eventFlagMaskShift},
		{key: keyRightShift, flags: eventFlagMaskShift},
	}
	if !reflect.DeepEqual(system.keyEvents, want) {
		t.Fatalf("key events = %#v, want %#v", system.keyEvents, want)
	}
	if system.modifierFlags != eventFlagMaskShift || !system.keys[keyShift] {
		t.Fatalf(
			"left Shift state was lost: flags=%#x keys=%#v",
			system.modifierFlags, system.keys,
		)
	}
}

func TestReleasedOwnedSiblingDoesNotLookForeignWhenQuartzStateLags(t *testing.T) {
	system := &fakeInputSystem{
		keys: map[uint16]bool{keyRightShift: true},
	}
	backend := &Backend{
		ownedKeys: map[uint16]ownedKey{
			keyShift: {code: keyShift, flag: eventFlagMaskShift},
		},
	}
	flags, err := backend.flagsAfterReleaseLocked(
		system,
		ownedKey{code: keyShift, flag: eventFlagMaskShift},
		eventFlagMaskShift,
		map[uint16]struct{}{keyRightShift: {}},
	)
	if err != nil {
		t.Fatalf("flagsAfterReleaseLocked: %v", err)
	}
	if flags != 0 {
		t.Fatalf("flags after final owned Shift release = %#x, want 0", flags)
	}
}

func TestOwnedPIDModifierSuppliesFlagsWhenGlobalStateOmitsIt(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{}
	backend := testBackend(system, &sleeps)

	if err := backend.Key(KeyEvent{Key: "cmd", PID: 17, Down: true}); err != nil {
		t.Fatalf("command down: %v", err)
	}
	// CGEventPostToPid does not have to expose target-local state through the
	// combined global source table. The backend's ownership remains authoritative.
	system.modifierFlags = 0
	system.keys[keyCommand] = false
	if err := backend.Key(KeyEvent{
		Key: "a", Modifiers: []string{"cmd"}, PID: 17, Tap: true,
	}); err != nil {
		t.Fatalf("command-a: %v", err)
	}
	got := system.keyEvents[len(system.keyEvents)-2:]
	want := []recordedKeyEvent{
		{key: keyA, down: true, flags: eventFlagMaskCommand, pid: 17},
		{key: keyA, flags: eventFlagMaskCommand, pid: 17},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command-a events = %#v, want %#v", got, want)
	}
}

func TestKeyToggleEnforcesOwnershipAndPID(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{keys: map[uint16]bool{keyB: true}}
	backend := testBackend(system, &sleeps)

	if err := backend.Key(KeyEvent{Key: "b", Down: true}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("foreign key down error = %v, want ErrOwnership", err)
	}
	system.keys[keyB] = false
	if err := backend.Key(KeyEvent{Key: "b", PID: 17, Down: true}); err != nil {
		t.Fatalf("owned key down: %v", err)
	}
	if err := backend.Key(KeyEvent{Key: "b", PID: 17, Down: true}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("duplicate key down error = %v, want ErrOwnership", err)
	}
	if err := backend.Key(KeyEvent{Key: "b", PID: 18}); !errors.Is(err, ErrOwnership) {
		t.Fatalf("wrong-PID key up error = %v, want ErrOwnership", err)
	}
	if err := backend.Key(KeyEvent{Key: "b", PID: 17}); err != nil {
		t.Fatalf("owned key up: %v", err)
	}
	if system.keys[keyB] {
		t.Fatal("owned key remained down")
	}
}

func TestCapsLockToggleStateIsNotTreatedAsForeignHold(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{
		keys: map[uint16]bool{keyCapsLock: true},
	}
	backend := testBackend(system, &sleeps)

	if err := backend.Key(KeyEvent{Key: "capslock", Tap: true}); err != nil {
		t.Fatalf("Caps Lock tap with active toggle state: %v", err)
	}
	if len(system.keyEvents) != 2 ||
		!system.keyEvents[0].down ||
		system.keyEvents[1].down {
		t.Fatalf("Caps Lock events = %#v, want one pulse", system.keyEvents)
	}
}

func TestKeyFailureRollsBackNewHolds(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{postKeyErrAt: 3}
	backend := testBackend(system, &sleeps)

	err := backend.Key(KeyEvent{
		Key: "c", Modifiers: []string{"cmd"}, Tap: true,
	})
	if err == nil {
		t.Fatal("Key unexpectedly hid main-key release failure")
	}
	want := []recordedKeyEvent{
		{key: keyCommand, down: true, flags: eventFlagMaskCommand},
		{key: keyC, down: true, flags: eventFlagMaskCommand},
		{key: keyC, flags: eventFlagMaskCommand},
		{key: keyCommand},
	}
	if !reflect.DeepEqual(system.keyEvents, want) {
		t.Fatalf("rollback events = %#v, want %#v", system.keyEvents, want)
	}
	if len(backend.ownedKeys) != 0 || system.keys[keyCommand] || system.keys[keyC] {
		t.Fatalf("rollback left keys held: owned=%#v native=%#v", backend.ownedKeys, system.keys)
	}
}

func TestCloseReleasesKeysBeforeModifiersInReverseOrder(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{}
	backend := testBackend(system, &sleeps)

	if err := backend.Key(KeyEvent{Key: "cmd", Down: true}); err != nil {
		t.Fatalf("command down: %v", err)
	}
	if err := backend.Key(KeyEvent{
		Key: "a", Modifiers: []string{"cmd"}, Down: true,
	}); err != nil {
		t.Fatalf("a down: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wantTail := []recordedKeyEvent{
		{key: keyA, flags: eventFlagMaskCommand},
		{key: keyCommand},
	}
	gotTail := system.keyEvents[len(system.keyEvents)-2:]
	if !reflect.DeepEqual(gotTail, wantTail) {
		t.Fatalf("close tail = %#v, want %#v", gotTail, wantTail)
	}
	if system.closeCalls != 1 || len(backend.ownedKeys) != 0 {
		t.Fatalf("Close state: closes=%d owned=%#v", system.closeCalls, backend.ownedKeys)
	}
}

func TestTextPostsWholeUnicodeScalarsAndDelay(t *testing.T) {
	var sleeps []time.Duration
	system := &fakeInputSystem{}
	backend := testBackend(system, &sleeps)

	if err := backend.Text(TextEvent{
		Text: "A😀", PID: 9, Delay: 5 * time.Millisecond,
	}); err != nil {
		t.Fatalf("Text: %v", err)
	}
	want := []recordedUnicodeEvent{
		{units: []uint16{'A'}, down: true, pid: 9},
		{units: []uint16{'A'}, pid: 9},
		{units: []uint16{0xd83d, 0xde00}, down: true, pid: 9},
		{units: []uint16{0xd83d, 0xde00}, pid: 9},
	}
	if !reflect.DeepEqual(system.unicodeEvents, want) {
		t.Fatalf("Unicode events = %#v, want %#v", system.unicodeEvents, want)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{5 * time.Millisecond}) {
		t.Fatalf("text sleeps = %v, want one 5ms delay", sleeps)
	}
}

func TestTextRejectsUnsafeStateAndInvalidInput(t *testing.T) {
	for _, test := range []struct {
		name  string
		event TextEvent
		setup func(*fakeInputSystem)
	}{
		{name: "negative PID", event: TextEvent{Text: "x", PID: -1}},
		{name: "negative delay", event: TextEvent{Text: "x", Delay: -time.Millisecond}},
		{name: "NUL", event: TextEvent{Text: "x\x00y"}},
		{
			name: "foreign command", event: TextEvent{Text: "x"},
			setup: func(system *fakeInputSystem) {
				system.modifierFlags = eventFlagMaskCommand
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sleeps []time.Duration
			system := &fakeInputSystem{}
			if test.setup != nil {
				test.setup(system)
			}
			backend := testBackend(system, &sleeps)
			if err := backend.Text(test.event); err == nil {
				t.Fatal("Text unexpectedly succeeded")
			}
			if len(system.unicodeEvents) != 0 {
				t.Fatalf("invalid text posted events: %#v", system.unicodeEvents)
			}
		})
	}
}

func TestUnsupportedQuartzKeysFailExplicitly(t *testing.T) {
	for _, name := range []string{"f21", "menu", "audio_play", "é"} {
		var sleeps []time.Duration
		backend := testBackend(&fakeInputSystem{}, &sleeps)
		if err := backend.Key(KeyEvent{Key: name, Tap: true}); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("Key(%q) error = %v, want ErrUnsupported", name, err)
		}
	}
}
