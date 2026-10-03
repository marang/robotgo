//go:build windows

package windowsinput

import (
	"errors"
	"reflect"
	"testing"
)

func TestMainKeyPreservesExtendedFlag(t *testing.T) {
	for _, test := range []struct {
		key      string
		extended bool
	}{
		{"rctrl", true}, {"ralt", true}, {"right", true},
		{"delete", true}, {"num/", true}, {"num_lock", true},
		{"audio_play", true}, {"num_enter", true},
		{"a", false}, {"enter", false}, {"num1", false}, {"num_clear", false},
		{"pause_break", false},
	} {
		t.Run(test.key, func(t *testing.T) {
			system := &fakeSystem{}
			backend := newFakeBackend(system, nil)
			if err := backend.Key(KeyEvent{Key: test.key, Tap: true}); err != nil {
				t.Fatal(err)
			}
			if len(system.sends) != 1 || len(system.sends[0]) != 2 {
				t.Fatalf("tap transactions = %#v", system.sends)
			}
			for index, record := range system.sends[0] {
				flags := uint32(0)
				if test.extended {
					flags = keyEventExtended
				}
				if index == 1 {
					flags |= keyEventKeyUp
				}
				if event := decodeKeyboard(record); event.Flags != flags {
					t.Fatalf("event %d flags = %#x, want %#x", index, event.Flags, flags)
				}
			}
		})
	}
}

func TestExtendedMainKeyCleanupKeepsIdentity(t *testing.T) {
	for _, cleanup := range []string{"release", "close", "rollback"} {
		t.Run(cleanup, func(t *testing.T) {
			system := &fakeSystem{}
			backend := newFakeBackend(system, nil)
			if cleanup == "rollback" {
				injectionErr := errors.New("partial extended-key tap")
				system.sendPlan = []sendResult{{inserted: 1, err: injectionErr}, {inserted: 1}}
				if err := backend.Key(KeyEvent{Key: "rctrl", Tap: true}); !errors.Is(err, injectionErr) {
					t.Fatalf("tap error = %v", err)
				}
			} else {
				if err := backend.Key(KeyEvent{Key: "rctrl", Down: true}); err != nil {
					t.Fatal(err)
				}
				var err error
				if cleanup == "close" {
					err = backend.Close()
				} else {
					err = backend.Key(KeyEvent{Key: "rctrl"})
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(system.sends) != 2 {
				t.Fatalf("SendInput calls = %d, want 2", len(system.sends))
			}
			if release := decodeKeyboard(system.sends[1][0]); release.VirtualKey != vkRControl || release.Flags != keyEventExtended|keyEventKeyUp {
				t.Fatalf("cleanup event = %+v", release)
			}
			if len(backend.ownedKeys) != 0 {
				t.Fatal("cleanup retained owned key")
			}
		})
	}
}

func TestPauseCleanupKeepsNonExtendedScanMetadata(t *testing.T) {
	for _, cleanup := range []string{"release", "close", "rollback"} {
		t.Run(cleanup, func(t *testing.T) {
			system := &fakeSystem{}
			backend := newFakeBackend(system, nil)
			if cleanup == "rollback" {
				injectionErr := errors.New("partial Pause tap")
				system.sendPlan = []sendResult{{inserted: 1, err: injectionErr}, {inserted: 1}}
				if err := backend.Key(KeyEvent{Key: "pause_break", Tap: true}); !errors.Is(err, injectionErr) {
					t.Fatalf("tap error = %v", err)
				}
			} else {
				if err := backend.Key(KeyEvent{Key: "pause_break", Down: true}); err != nil {
					t.Fatal(err)
				}
				var err error
				if cleanup == "close" {
					err = backend.Close()
				} else {
					err = backend.Key(KeyEvent{Key: "pause_break"})
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(system.sends) != 2 || len(backend.ownedKeys) != 0 {
				t.Fatal("Pause cleanup retained ownership or emitted extra transactions")
			}
			for _, transaction := range system.sends {
				prepared := withPhysicalScanCodes(transaction, func(uint16) uint16 { return 0x45 })
				for _, record := range prepared {
					if event := decodeKeyboard(record); event.VirtualKey != vkPause || event.ScanCode != 0x45 || event.Flags&^keyEventKeyUp != 0 {
						t.Fatalf("Pause physical event = %+v", event)
					}
				}
			}
			if event := decodeKeyboard(system.sends[1][0]); event.Flags != keyEventKeyUp {
				t.Fatalf("Pause cleanup flags = %#x", event.Flags)
			}
		})
	}
}

func TestNumClearUsesItsOwnVirtualKey(t *testing.T) {
	system := &fakeSystem{}
	backend := newFakeBackend(system, nil)
	if err := backend.Key(KeyEvent{Key: "num_clear", Down: true}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Key(KeyEvent{Key: "num_clear"}); err != nil {
		t.Fatal(err)
	}
	for index, transaction := range system.sends {
		wantFlags := uint32(0)
		if index == 1 {
			wantFlags = keyEventKeyUp
		}
		if len(transaction) != 1 || decodeKeyboard(transaction[0]).VirtualKey != 0x0c || decodeKeyboard(transaction[0]).Flags != wantFlags {
			t.Fatalf("NumClear transaction %d = %#v", index, keyboardSummary(transaction))
		}
	}
	if len(system.sends) != 2 || len(backend.ownedKeys) != 0 {
		t.Fatal("NumClear release retained ownership or emitted extra events")
	}
}

func TestExplicitModifierSidesSatisfyImplicitFamilies(t *testing.T) {
	for _, test := range []struct {
		name      string
		key       string
		modifiers []string
		want      []uint16
	}{
		{"left shift", "A", []string{"lshift"}, []uint16{vkLShift}},
		{"right shift", "A", []string{"rshift"}, []uint16{vkRShift}},
		{"right shift alias", "A", []string{"right_shift"}, []uint16{vkRShift}},
		{"generic then side", "A", []string{"shift", "rshift"}, []uint16{vkRShift}},
		{"side then generic", "A", []string{"rshift", "shift"}, []uint16{vkRShift}},
		{"two explicit sides", "A", []string{"lshift", "rshift"}, []uint16{vkLShift, vkRShift}},
		{"AltGr sides", "@", []string{"rctrl", "ralt"}, []uint16{vkRControl, vkRMenu}},
		{"control aliases", "@", []string{"ctrl", "control"}, []uint16{vkControl, vkMenu}},
	} {
		t.Run(test.name, func(t *testing.T) {
			system := &fakeSystem{
				layout:     map[uint16]uint16{'A': 0x41, '@': 0x51},
				shiftState: map[uint16]uint8{'A': shiftStateShift, '@': shiftStateControl | shiftStateAlt},
			}
			backend := newFakeBackend(system, nil)
			if err := backend.Key(KeyEvent{Key: test.key, Modifiers: test.modifiers, Tap: true}); err != nil {
				t.Fatal(err)
			}
			var got []uint16
			for _, record := range system.sends[0][:len(test.want)] {
				got = append(got, decodeKeyboard(record).VirtualKey)
			}
			if !reflect.DeepEqual(got, test.want) || len(system.sends[0]) != 2*len(test.want)+2 {
				t.Fatalf("modifier transaction = %#v, want sides %v", keyboardSummary(system.sends[0]), test.want)
			}
		})
	}
}

func TestImplicitModifierDoesNotTouchForeignSidedHold(t *testing.T) {
	system := &fakeSystem{down: map[uint16]bool{vkRShift: true}}
	backend := newFakeBackend(system, nil)
	if err := backend.Key(KeyEvent{Key: "A", Modifiers: []string{"rshift"}, Tap: true}); err != nil {
		t.Fatal(err)
	}
	if len(system.sends) != 1 || len(system.sends[0]) != 2 {
		t.Fatalf("foreign modifier caused extra events: %#v", system.sends)
	}
	if !system.down[vkRShift] || len(backend.ownedKeys) != 0 {
		t.Fatal("tap altered foreign modifier ownership")
	}
}

func TestPhysicalScanMetadataPreservesUnicodeAndInputRecords(t *testing.T) {
	physical := []inputRecord{
		trackedKeyInput(0x41, true).record,
		trackedKeyInput(0x41, false).record,
		trackedKeyInput(vkRControl, false).record,
		trackedUnicodeInput(0xd83d, true).record,
		trackedUnicodeInput(0xde00, false).record,
		trackedMouseInput(mouseEventWheel, 120).record,
	}
	original := append([]inputRecord(nil), physical...)
	var translated []uint16
	prepared := withPhysicalScanCodes(physical, func(key uint16) uint16 {
		translated = append(translated, key)
		if key == 0x41 {
			return 0x1e
		}
		return 0x1d
	})
	if !reflect.DeepEqual(translated, []uint16{0x41, 0x41, vkRControl}) {
		t.Fatalf("translated keys = %v", translated)
	}
	want := [][3]uint32{
		{0x41, 0x1e, 0}, {0x41, 0x1e, keyEventKeyUp},
		{vkRControl, 0x1d, keyEventExtended | keyEventKeyUp},
		{0, 0xd83d, keyEventUnicode}, {0, 0xde00, keyEventUnicode | keyEventKeyUp},
	}
	if got := keyboardSummary(prepared[:5]); !reflect.DeepEqual(got, want) {
		t.Fatalf("scan metadata = %#v, want %#v", got, want)
	}
	if prepared[5] != original[5] || !reflect.DeepEqual(physical, original) {
		t.Fatal("scan translation mutated caller records or pointer input")
	}
}

func TestKeyRejectsNonBMPWithoutLayoutOrInjection(t *testing.T) {
	system := &fakeSystem{layoutErr: errors.New("unexpected layout query")}
	backend := newFakeBackend(system, nil)
	for _, key := range []string{"\U00010061", "\U00010041", "\U00010030", "\U00100061", "😀"} {
		if err := backend.Key(KeyEvent{Key: key, Tap: true}); !errors.Is(err, ErrUnsupported) || errors.Is(err, system.layoutErr) {
			t.Fatalf("Key(%q) error = %v", key, err)
		}
	}
	if len(system.sends) != 0 {
		t.Fatal("non-BMP key injected input")
	}
}

func TestMissingScanTranslationKeepsVirtualKeyEvent(t *testing.T) {
	records := []inputRecord{trackedKeyInput(vkMediaPlayPause, false).record}
	prepared := withPhysicalScanCodes(records, func(uint16) uint16 { return 0 })
	if event := decodeKeyboard(prepared[0]); event.VirtualKey != vkMediaPlayPause || event.ScanCode != 0 || event.Flags != keyEventExtended|keyEventKeyUp {
		t.Fatalf("untranslated event = %+v", event)
	}
}
