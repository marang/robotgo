//go:build linux && cgo && x11integration && !wayland

package robotgo_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/marang/robotgo"
)

const x11TestUnicodeGrinningFace = uint32(0x1f600)

const envExpectedX11NoXTest = "ROBOTGO_EXPECT_X11_NO_XTEST"

const (
	x11KeysymCapsLock       = 0xffe5
	x11KeysymISOLevel3Shift = 0xfe03
)

func TestNativeX11UnicodeFailsBeforeMutation(t *testing.T) {
	harness := newX11InputHarness(t)
	info := robotgo.GetRuntimeBackendInfo()
	if info.BuildImplementation != robotgo.RuntimeImplementationNativeCGO || !info.CGOEnabled {
		t.Fatalf("runtime implementation = %+v, want native CGO", info)
	}

	previousConfig := robotgo.GetRuntimeConfig()
	config := previousConfig
	config.KeyDelay = 0
	if err := robotgo.SetRuntimeConfig(config); err != nil {
		t.Fatalf("SetRuntimeConfig: %v", err)
	}
	t.Cleanup(func() {
		if err := robotgo.SetRuntimeConfig(previousConfig); err != nil {
			t.Errorf("restore RuntimeConfig: %v", err)
		}
	})

	assertRejectedWithoutX11Mutation := func(t *testing.T, operation func() error) {
		t.Helper()
		harness.drainEvents()
		before := harness.keyboardState()
		if err := operation(); !errors.Is(err, robotgo.ErrNotSupported) {
			t.Fatalf("error = %v, want ErrNotSupported", err)
		}
		harness.assertNoMatchingEvent("key event after rejected Unicode input", 100*time.Millisecond, func(event xgb.Event) bool {
			switch event.(type) {
			case xproto.KeyPressEvent, xproto.KeyReleaseEvent:
				return true
			default:
				return false
			}
		})
		harness.conn.Sync()
		assertX11KeyboardStateEqual(t, before, harness.keyboardState())
	}

	t.Run("text preflight", func(t *testing.T) {
		assertRejectedWithoutX11Mutation(t, func() error {
			return robotgo.TypeStrE("Aä", 0, 0, 0)
		})
	})
	t.Run("code point", func(t *testing.T) {
		assertRejectedWithoutX11Mutation(t, func() error {
			return robotgo.UnicodeTypeE(x11TestUnicodeGrinningFace)
		})
	})
}

func TestNativeX11UnmappedKeyFailsBeforeModifierInput(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("reset native X11 display before keymap mutation: %v", err)
	}

	controlCode, _ := harness.findKeycode(x11KeysymControlL)
	if harness.keyPressed(controlCode) {
		t.Fatalf("Control keycode %d is already pressed", controlCode)
	}
	removeX11Keysym(t, harness, x11KeysymEnter)
	harness.drainEvents()

	if err := robotgo.KeyPress("enter", "ctrl"); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("KeyPress with unmapped main key error = %v, want ErrNotSupported", err)
	}
	harness.assertNoMatchingEvent("key event after failed native preflight", 100*time.Millisecond, func(event xgb.Event) bool {
		switch event.(type) {
		case xproto.KeyPressEvent, xproto.KeyReleaseEvent:
			return true
		default:
			return false
		}
	})
	if harness.keyPressed(controlCode) {
		t.Fatalf("failed KeyPress left Control keycode %d pressed", controlCode)
	}
}

func TestNativeX11KeyPressKeepsResolvedKeycodesAcrossKeymapChange(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("reset native X11 display before keymap mutation: %v", err)
	}

	enterCode, _ := harness.findKeycode(x11KeysymEnter)
	controlCode, _ := harness.findKeycode(x11KeysymControlL)
	beforeMap := harness.keyboardState()
	withoutEnter := append([]xproto.Keysym(nil), beforeMap.keysyms...)
	for index, keysym := range withoutEnter {
		if keysym == x11KeysymEnter {
			withoutEnter[index] = 0
		}
	}
	keycodeCount := len(withoutEnter) / int(beforeMap.keysymsPerKeycode)
	t.Cleanup(func() {
		if err := xproto.ChangeKeyboardMappingChecked(
			harness.conn,
			byte(keycodeCount),
			beforeMap.minKeycode,
			beforeMap.keysymsPerKeycode,
			beforeMap.keysyms,
		).Check(); err != nil {
			t.Errorf("restore X11 keyboard mapping: %v", err)
		}
		harness.conn.Sync()
	})

	harness.drainEvents()
	beforeInput := harness.inputState()
	pressDone := make(chan error, 1)
	go func() {
		pressDone <- robotgo.KeyPress("enter", "ctrl")
	}()
	modifierPress := harness.waitForKeyPress("modifier press before keymap change")
	if modifierPress.Detail != controlCode {
		t.Fatalf("first keycode = %d, want Control %d", modifierPress.Detail, controlCode)
	}

	// The native compound tap holds a short server grab. This request is queued
	// after the modifier press and cannot alter the mapping until the exact
	// pre-resolved main/modifier keycodes have both been released.
	xproto.ChangeKeyboardMapping(
		harness.conn,
		byte(keycodeCount),
		beforeMap.minKeycode,
		beforeMap.keysymsPerKeycode,
		withoutEnter,
	)
	mainPress := harness.waitForKeyPress("main press during queued keymap change")
	mainRelease := harness.waitForKeyRelease("main release during queued keymap change")
	modifierRelease := harness.waitForKeyRelease("modifier release during queued keymap change")
	if mainPress.Detail != enterCode || mainRelease.Detail != enterCode {
		t.Fatalf(
			"compound main keycodes = press %d/release %d, want pre-resolved Enter %d",
			mainPress.Detail, mainRelease.Detail, enterCode,
		)
	}
	if modifierRelease.Detail != controlCode {
		t.Fatalf("modifier release keycode = %d, want pre-resolved Control %d", modifierRelease.Detail, controlCode)
	}
	select {
	case err := <-pressDone:
		if err != nil {
			t.Fatalf("KeyPress during queued keymap change: %v", err)
		}
	case <-time.After(x11EventTimeout):
		t.Fatal("KeyPress did not finish after queued keymap change")
	}

	harness.conn.Sync()
	afterInput := harness.inputState()
	if string(beforeInput.pressedKeys) != string(afterInput.pressedKeys) {
		t.Fatalf("compound KeyPress left a key pressed after keymap mutation")
	}
}

func TestNativeX11TextKeymapFailureIsAtomic(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("reset native X11 display before keymap mutation: %v", err)
	}

	removeX11Keysym(t, harness, '!')
	harness.drainEvents()
	before := harness.keyboardState()
	if err := robotgo.TypeStrE("A!", 0, 0, 0); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("TypeStrE with a later unmapped character error = %v, want ErrNotSupported", err)
	}
	harness.assertNoMatchingEvent("partial native text before keymap failure", 100*time.Millisecond, func(event xgb.Event) bool {
		switch event.(type) {
		case xproto.KeyPressEvent, xproto.KeyReleaseEvent:
			return true
		default:
			return false
		}
	})
	assertX11KeyboardStateEqual(t, before, harness.keyboardState())

	harness.drainEvents()
	before = harness.keyboardState()
	if err := robotgo.TypeStrE("a\x00b", 0, 0, 0); err == nil ||
		errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("TypeStrE with embedded NUL error = %v, want argument error", err)
	}
	harness.assertNoMatchingEvent("embedded-NUL native text", 100*time.Millisecond, func(event xgb.Event) bool {
		switch event.(type) {
		case xproto.KeyPressEvent, xproto.KeyReleaseEvent:
			return true
		default:
			return false
		}
	})
	assertX11KeyboardStateEqual(t, before, harness.keyboardState())
}

func TestNativeX11TextUsesActiveGermanKeymap(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	setNativeX11Layout(t, "de")
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("reset native X11 display after layout switch: %v", err)
	}

	_, oracleLines, _ := startXKBOracle(t, harness)
	before := harness.keyboardState()
	const text = `/@{}`
	if err := robotgo.TypeStrE(text, 0, 0, 0); err != nil {
		t.Fatalf("TypeStrE on German XKB layout: %v", err)
	}
	if got := string(waitForXKBOracleText(t, oracleLines, len([]rune(text)))); got != text {
		t.Fatalf("independent XKB oracle received %q, want %q", got, text)
	}
	harness.conn.Sync()
	assertX11KeyboardStateEqual(t, before, harness.keyboardState())
}

func TestNativeX11TextRejectsMidStreamKeymapChange(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("reset native X11 display before keymap mutation: %v", err)
	}

	harness.drainEvents()
	before := harness.inputState()
	typeDone := make(chan error, 1)
	go func() {
		typeDone <- robotgo.TypeStrE("aa", 0, 250, 0)
	}()
	firstPress := harness.waitForKeyPress("first text key press before keymap change")
	firstRelease := harness.waitForKeyRelease("first text key release before keymap change")
	if firstPress.Detail != firstRelease.Detail {
		t.Fatalf("first text keycodes differ: press=%d release=%d", firstPress.Detail, firstRelease.Detail)
	}

	removeX11Keysym(t, harness, 'a')
	select {
	case err := <-typeDone:
		if !errors.Is(err, robotgo.ErrNotSupported) {
			t.Fatalf("TypeStrE after mid-text keymap mutation error = %v, want ErrNotSupported", err)
		}
	case <-time.After(x11EventTimeout):
		t.Fatal("TypeStrE did not finish after mid-text keymap mutation")
	}
	harness.assertNoMatchingEvent("stale second text key after keymap change", 100*time.Millisecond, func(event xgb.Event) bool {
		switch event.(type) {
		case xproto.KeyPressEvent, xproto.KeyReleaseEvent:
			return true
		default:
			return false
		}
	})

	after := harness.inputState()
	if string(before.pressedKeys) != string(after.pressedKeys) {
		t.Fatalf("mid-text keymap mutation left a key pressed")
	}
}

func TestNativeX11ForeignHeldKeysNeverReleased(t *testing.T) {
	t.Run("text main key and full string preflight", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		bCode, _ := harness.findKeycode('b')
		holdIndependentX11Key(t, harness, bCode)
		before := harness.inputState()

		if err := robotgo.TypeStrE("ab", 0, 0, 0); err == nil ||
			!strings.Contains(err.Error(), "owned by another input source") {
			t.Fatalf("TypeStrE with foreign-held later key error = %v, want ownership conflict", err)
		}
		assertNoNativeX11KeyEvent(t, harness, "partial text with foreign-held later key")
		assertX11InputStateEqual(t, before, harness.inputState())
		if !harness.keyPressed(bCode) {
			t.Fatalf("TypeStrE released foreign-held keycode %d", bCode)
		}
	})

	t.Run("compound key modifier", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		controlCode, _ := harness.findKeycode(x11KeysymControlL)
		holdIndependentX11Key(t, harness, controlCode)
		before := harness.inputState()

		if err := robotgo.KeyPress("enter", "ctrl"); err == nil ||
			!strings.Contains(err.Error(), "owned by another input source") {
			t.Fatalf("KeyPress with foreign-held Control error = %v, want ownership conflict", err)
		}
		assertNoNativeX11KeyEvent(t, harness, "compound key with foreign-held modifier")
		assertX11InputStateEqual(t, before, harness.inputState())
		if !harness.keyPressed(controlCode) {
			t.Fatalf("KeyPress released foreign-held Control keycode %d", controlCode)
		}
	})

	t.Run("unowned key-up", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		aCode, _ := harness.findKeycode('a')
		holdIndependentX11Key(t, harness, aCode)
		before := harness.inputState()

		if err := robotgo.KeyUp("a"); !errors.Is(err, robotgo.ErrInputOwnership) {
			t.Fatalf("KeyUp without RobotGo ownership error = %v, want ownership conflict", err)
		}
		assertNoNativeX11KeyEvent(t, harness, "unowned key-up")
		assertX11InputStateEqual(t, before, harness.inputState())
		if !harness.keyPressed(aCode) {
			t.Fatalf("KeyUp released foreign-held keycode %d", aCode)
		}
	})
}

func TestNativeX11TextHonorsActiveXKBState(t *testing.T) {
	t.Run("foreign Shift is reused but never released", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		setNativeX11Layout(t, "us")
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Fatalf("reset native display after layout switch: %v", err)
		}
		_, oracleLines, _ := startXKBOracle(t, harness)
		shiftCode, _ := harness.findKeycode(x11KeysymShiftL)
		holdIndependentX11Key(t, harness, shiftCode)

		if err := robotgo.TypeStrE("A", 0, 0, 0); err != nil {
			t.Fatalf("TypeStrE with foreign-held Shift: %v", err)
		}
		if got := string(waitForXKBOracleText(t, oracleLines, 1)); got != "A" {
			t.Fatalf("XKB oracle received %q with foreign-held Shift, want %q", got, "A")
		}
		if !harness.keyPressed(shiftCode) {
			t.Fatalf("TypeStrE released foreign-held Shift keycode %d", shiftCode)
		}
		if err := robotgo.TypeStrE("a", 0, 0, 0); err == nil ||
			!strings.Contains(err.Error(), "modifier or lock state") {
			t.Fatalf("lowercase with foreign-held Shift error = %v, want state conflict", err)
		}
		if !harness.keyPressed(shiftCode) {
			t.Fatalf("rejected lowercase input released foreign-held Shift keycode %d", shiftCode)
		}
	})

	t.Run("foreign Level3 selector is reused", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		setNativeX11Layout(t, "de")
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Fatalf("reset native display after layout switch: %v", err)
		}
		_, oracleLines, _ := startXKBOracle(t, harness)
		level3Code, _ := harness.findKeycode(x11KeysymISOLevel3Shift)
		holdIndependentX11Key(t, harness, level3Code)

		if err := robotgo.TypeStrE("@", 0, 0, 0); err != nil {
			t.Fatalf("TypeStrE with foreign-held Level3: %v", err)
		}
		if got := string(waitForXKBOracleText(t, oracleLines, 1)); got != "@" {
			t.Fatalf("XKB oracle received %q with foreign-held Level3, want %q", got, "@")
		}
		if !harness.keyPressed(level3Code) {
			t.Fatalf("TypeStrE released foreign-held Level3 keycode %d", level3Code)
		}
	})

	t.Run("Caps Lock state is preserved", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		setNativeX11Layout(t, "us")
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Fatalf("reset native display after layout switch: %v", err)
		}
		_, oracleLines, _ := startXKBOracle(t, harness)
		capsCode, _ := harness.findKeycode(x11KeysymCapsLock)
		toggleIndependentX11Lock(t, harness, capsCode)
		t.Cleanup(func() { toggleIndependentX11Lock(t, harness, capsCode) })
		before := harness.inputState()

		if err := robotgo.TypeStrE("aA", 0, 0, 0); err != nil {
			t.Fatalf("TypeStrE with Caps Lock: %v", err)
		}
		if got := string(waitForXKBOracleText(t, oracleLines, 2)); got != "aA" {
			t.Fatalf("XKB oracle received %q with Caps Lock, want %q", got, "aA")
		}
		assertX11InputStateEqual(t, before, harness.inputState())
	})

	t.Run("shortcut modifier fails closed", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		controlCode, _ := harness.findKeycode(x11KeysymControlL)
		holdIndependentX11Key(t, harness, controlCode)
		before := harness.inputState()

		if err := robotgo.TypeStrE("a", 0, 0, 0); err == nil ||
			!strings.Contains(err.Error(), "modifier or lock state") {
			t.Fatalf("TypeStrE with active Control error = %v, want state conflict", err)
		}
		assertNoNativeX11KeyEvent(t, harness, "text with active shortcut modifier")
		assertX11InputStateEqual(t, before, harness.inputState())
	})
}

func TestNativeX11KeyToggleOwnershipSurvivesKeymapChange(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	aCode, _ := harness.findKeycode('a')
	harness.drainEvents()

	if err := robotgo.KeyDown("a"); err != nil {
		t.Fatalf("KeyDown before keymap change: %v", err)
	}
	press := harness.waitForKeyPress("owned key press before keymap change")
	if press.Detail != aCode || !harness.keyPressed(aCode) {
		t.Fatalf("owned key press = %d pressed=%v, want keycode %d pressed", press.Detail, harness.keyPressed(aCode), aCode)
	}
	t.Cleanup(func() { _ = robotgo.KeyUp("a") })
	removeX11Keysym(t, harness, 'a')

	if err := robotgo.KeyUp("a"); err != nil {
		t.Fatalf("KeyUp after keymap change: %v", err)
	}
	release := harness.waitForKeyRelease("owned key release after keymap change")
	if release.Detail != aCode {
		t.Fatalf("owned key release = %d, want original keycode %d", release.Detail, aCode)
	}
	if harness.keyPressed(aCode) {
		t.Fatalf("KeyUp after keymap change left original keycode %d pressed", aCode)
	}
}

func TestNativeX11KeyToggleSharesOnlyOwnedModifiers(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	shiftCode, _ := harness.findKeycode(x11KeysymShiftL)
	aCode, _ := harness.findKeycode('a')
	bCode, _ := harness.findKeycode('b')
	harness.drainEvents()

	if err := robotgo.KeyDown("a", "shift"); err != nil {
		t.Fatalf("first modified KeyDown: %v", err)
	}
	if err := robotgo.KeyDown("b", "shift"); err != nil {
		_ = robotgo.KeyUp("a", "shift")
		t.Fatalf("second modified KeyDown sharing Shift: %v", err)
	}
	t.Cleanup(func() {
		_ = robotgo.KeyUp("a", "shift")
		_ = robotgo.KeyUp("b", "shift")
	})
	if !harness.keyPressed(shiftCode) || !harness.keyPressed(aCode) || !harness.keyPressed(bCode) {
		t.Fatalf("owned shared state after KeyDown: shift=%v a=%v b=%v",
			harness.keyPressed(shiftCode), harness.keyPressed(aCode), harness.keyPressed(bCode))
	}

	if err := robotgo.KeyUp("a", "shift"); err != nil {
		t.Fatalf("first modified KeyUp: %v", err)
	}
	if harness.keyPressed(aCode) || !harness.keyPressed(bCode) || !harness.keyPressed(shiftCode) {
		t.Fatalf("shared state after first KeyUp: shift=%v a=%v b=%v",
			harness.keyPressed(shiftCode), harness.keyPressed(aCode), harness.keyPressed(bCode))
	}
	if err := robotgo.KeyUp("b", "shift"); err != nil {
		t.Fatalf("second modified KeyUp: %v", err)
	}
	if harness.keyPressed(aCode) || harness.keyPressed(bCode) || harness.keyPressed(shiftCode) {
		t.Fatalf("owned keys remain after final KeyUp: shift=%v a=%v b=%v",
			harness.keyPressed(shiftCode), harness.keyPressed(aCode), harness.keyPressed(bCode))
	}

	// A physical modifier can be the main key in one ownership record and a
	// modifier in another. Releasing the main-key record must not emit the
	// physical release while the compound record still owns it.
	if err := robotgo.KeyDown("shift"); err != nil {
		t.Fatalf("standalone Shift KeyDown: %v", err)
	}
	if err := robotgo.KeyDown("a", "shift"); err != nil {
		_ = robotgo.KeyUp("shift")
		t.Fatalf("modified KeyDown sharing standalone Shift: %v", err)
	}
	if err := robotgo.KeyUp("shift"); err != nil {
		t.Fatalf("standalone Shift KeyUp while compound record owns it: %v", err)
	}
	if !harness.keyPressed(shiftCode) || !harness.keyPressed(aCode) {
		t.Fatalf("cross-role state after standalone Shift KeyUp: shift=%v a=%v",
			harness.keyPressed(shiftCode), harness.keyPressed(aCode))
	}
	if err := robotgo.KeyUp("a", "shift"); err != nil {
		t.Fatalf("modified KeyUp after cross-role sharing: %v", err)
	}
	if harness.keyPressed(shiftCode) || harness.keyPressed(aCode) {
		t.Fatalf("cross-role owned keys remain after final KeyUp: shift=%v a=%v",
			harness.keyPressed(shiftCode), harness.keyPressed(aCode))
	}
}

func TestNativeX11ToggleOwnershipFollowsDisplayLifecycle(t *testing.T) {
	t.Run("close releases and invalidates ownership", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		aCode, _ := harness.findKeycode('a')
		harness.drainEvents()

		if err := robotgo.KeyDown("a"); err != nil {
			t.Fatalf("KeyDown before display close: %v", err)
		}
		if press := harness.waitForKeyPress("owned key before display close"); press.Detail != aCode {
			t.Fatalf("KeyDown before display close keycode=%d, want %d", press.Detail, aCode)
		}
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Fatalf("CloseMainDisplayE with owned key: %v", err)
		}
		if release := harness.waitForKeyRelease("owned key released by display close"); release.Detail != aCode {
			t.Fatalf("display-close release keycode=%d, want %d", release.Detail, aCode)
		}
		if harness.keyPressed(aCode) {
			t.Fatalf("display close left owned keycode %d pressed", aCode)
		}

		if err := robotgo.KeyDown("a"); err != nil {
			t.Fatalf("KeyDown after display generation change: %v", err)
		}
		if press := harness.waitForKeyPress("owned key after display reopen"); press.Detail != aCode {
			t.Fatalf("KeyDown after display reopen keycode=%d, want %d", press.Detail, aCode)
		}
		if err := robotgo.KeyUp("a"); err != nil {
			t.Fatalf("KeyUp after display reopen: %v", err)
		}
		if release := harness.waitForKeyRelease("owned key after display reopen"); release.Detail != aCode {
			t.Fatalf("KeyUp after display reopen keycode=%d, want %d", release.Detail, aCode)
		}
	})

	t.Run("retarget releases and invalidates ownership", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		original := robotgo.GetXDisplayName()
		bCode, _ := harness.findKeycode('b')
		harness.drainEvents()

		if err := robotgo.KeyDown("b"); err != nil {
			t.Fatalf("KeyDown before display retarget: %v", err)
		}
		if press := harness.waitForKeyPress("owned key before display retarget"); press.Detail != bCode {
			t.Fatalf("KeyDown before retarget keycode=%d, want %d", press.Detail, bCode)
		}
		if err := robotgo.SetXDisplayName(original); err != nil {
			t.Fatalf("SetXDisplayName with owned key: %v", err)
		}
		if release := harness.waitForKeyRelease("owned key released by display retarget"); release.Detail != bCode {
			t.Fatalf("display-retarget release keycode=%d, want %d", release.Detail, bCode)
		}
		if harness.keyPressed(bCode) {
			t.Fatalf("display retarget left owned keycode %d pressed", bCode)
		}

		if err := robotgo.KeyDown("b"); err != nil {
			t.Fatalf("KeyDown after display retarget: %v", err)
		}
		if press := harness.waitForKeyPress("owned key after display retarget"); press.Detail != bCode {
			t.Fatalf("KeyDown after retarget keycode=%d, want %d", press.Detail, bCode)
		}
		if err := robotgo.KeyUp("b"); err != nil {
			t.Fatalf("KeyUp after display retarget: %v", err)
		}
		if release := harness.waitForKeyRelease("owned key after display retarget"); release.Detail != bCode {
			t.Fatalf("KeyUp after retarget keycode=%d, want %d", release.Detail, bCode)
		}
	})

	t.Run("close releases and invalidates mouse ownership", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		if err := robotgo.MoveE(x11WindowX+20, x11WindowY+20); err != nil {
			t.Fatalf("move pointer into lifecycle test window: %v", err)
		}
		harness.waitForEvent("pointer in lifecycle test window", func(event xgb.Event) bool {
			motion, ok := event.(xproto.MotionNotifyEvent)
			return ok && int(motion.RootX) == x11WindowX+20 && int(motion.RootY) == x11WindowY+20
		})
		harness.drainEvents()

		if err := robotgo.MouseDown("right"); err != nil {
			t.Fatalf("MouseDown before display close: %v", err)
		}
		if event := harness.waitForButtonEvents(1)[0]; !event.pressed || event.button != x11ButtonRight {
			t.Fatalf("MouseDown event = %+v, want right-button press", event)
		}
		if mask := harness.inputState().pointerMask; mask&xproto.KeyButMaskButton3 == 0 {
			t.Fatalf("MouseDown did not leave right button pressed: mask=%#x", mask)
		}
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Fatalf("CloseMainDisplayE with owned mouse button: %v", err)
		}
		if event := harness.waitForButtonEvents(1)[0]; event.pressed || event.button != x11ButtonRight {
			t.Fatalf("display-close mouse event = %+v, want right-button release", event)
		}
		if mask := harness.inputState().pointerMask; mask&xproto.KeyButMaskButton3 != 0 {
			t.Fatalf("display close left right button pressed: mask=%#x", mask)
		}

		if err := robotgo.MouseDown("right"); err != nil {
			t.Fatalf("MouseDown after display generation change: %v", err)
		}
		if err := robotgo.MouseUp("right"); err != nil {
			t.Fatalf("MouseUp after display generation change: %v", err)
		}
	})

	t.Run("stateful unobservable wheel buttons are unsupported", func(t *testing.T) {
		harness := newX11InputHarness(t)
		assertExpectedX11Implementation(t)
		harness.fakeButton(6, true)
		t.Cleanup(func() { harness.fakeButton(6, false) })
		harness.drainEvents()

		if err := robotgo.MouseDown("wheelLeft"); !errors.Is(err, robotgo.ErrNotSupported) {
			t.Fatalf("stateful horizontal-wheel toggle error = %v, want ErrNotSupported", err)
		}

		harness.fakeButton(6, false)
		harness.drainEvents()
		if err := robotgo.ClickE("wheelLeft"); err != nil {
			t.Fatalf("stateless horizontal-wheel click: %v", err)
		}
		events := harness.waitForButtonEvents(2)
		if !events[0].pressed || events[0].button != 6 || events[1].pressed || events[1].button != 6 {
			t.Fatalf("horizontal-wheel click events = %+v, want button-6 press/release", events)
		}
	})
}

func TestNativeX11ExplicitInvalidDisplayNeverFallsBack(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	original := robotgo.GetXDisplayName()
	t.Cleanup(func() {
		if err := robotgo.SetXDisplayName(original); err != nil {
			t.Errorf("restore X11 display override: %v", err)
		}
		if err := robotgo.CloseMainDisplayE(); err != nil {
			t.Errorf("close restored X11 display: %v", err)
		}
	})

	const unavailableDisplay = ":65535"
	if err := robotgo.SetXDisplayName(unavailableDisplay); err != nil {
		t.Fatalf("SetXDisplayName: %v", err)
	}
	if got := robotgo.GetXDisplayName(); got != unavailableDisplay {
		t.Fatalf("GetXDisplayName = %q, want %q", got, unavailableDisplay)
	}
	harness.drainEvents()
	beforeInput := harness.inputState()
	if err := robotgo.KeyboardReady(); !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("KeyboardReady with invalid display error = %v, want ErrNotSupported", err)
	}
	for name, operation := range map[string]func() error{
		"TypeStrE": func() error { return robotgo.TypeStrE("A", 0, 0, 0) },
		"KeyPress": func() error { return robotgo.KeyPress("enter") },
		"KeyDown":  func() error { return robotgo.KeyDown("a") },
	} {
		if err := operation(); !errors.Is(err, robotgo.ErrNotSupported) {
			t.Errorf("%s with invalid display error = %v, want ErrNotSupported", name, err)
		}
	}
	assertNoNativeX11KeyEvent(t, harness, "native input falling back from invalid explicit display")
	assertX11InputStateEqual(t, beforeInput, harness.inputState())
	if _, err := robotgo.CaptureImg(0, 0, 8, 8); err == nil {
		t.Fatal("CaptureImg silently fell back from an invalid explicit X11 display")
	}
	if rect := robotgo.GetScreenRect(); rect.W != 0 || rect.H != 0 {
		t.Fatalf("GetScreenRect with invalid explicit display = %+v, want zero dimensions", rect)
	}

	if err := robotgo.SetXDisplayName(original); err != nil {
		t.Fatalf("restore X11 display override: %v", err)
	}
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("close invalid X11 display: %v", err)
	}
	if err := robotgo.KeyboardReady(); err != nil {
		t.Fatalf("KeyboardReady after restoring DISPLAY: %v", err)
	}
	if x, y := harness.queryPointer(); x < 0 || y < 0 {
		t.Fatalf("invalid pointer after restoring DISPLAY: (%d, %d)", x, y)
	}
}

func TestNativeX11PixelColorOutOfBoundsReturnsError(t *testing.T) {
	newX11InputHarness(t)
	assertExpectedX11Implementation(t)

	rect := robotgo.GetScreenRect()
	if rect.W <= 0 || rect.H <= 0 {
		t.Fatalf("GetScreenRect = %+v, want non-zero dimensions", rect)
	}
	if _, err := robotgo.GetPxColor(rect.X, rect.Y); err != nil {
		t.Fatalf("GetPxColor at visible origin: %v", err)
	}
	if _, err := robotgo.GetPxColor(rect.X+rect.W+1, rect.Y+rect.H+1); err == nil {
		t.Fatal("GetPxColor outside the X11 root returned black without an error")
	}
}

func TestNativeX11DisplayLifecycleConcurrent(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	display := os.Getenv("DISPLAY")
	active := robotgo.GetActive()
	original := robotgo.GetXDisplayName()
	t.Cleanup(func() {
		if err := robotgo.SetXDisplayName(original); err != nil {
			t.Errorf("restore X11 display override: %v", err)
		}
		_ = robotgo.CloseMainDisplayE()
	})

	previousConfig := robotgo.GetRuntimeConfig()
	config := previousConfig
	config.KeyDelay = 0
	config.MouseDelay = 0
	config.Scale = false
	if err := robotgo.SetRuntimeConfig(config); err != nil {
		t.Fatalf("SetRuntimeConfig: %v", err)
	}
	t.Cleanup(func() { _ = robotgo.SetRuntimeConfig(previousConfig) })

	const iterations = 200
	errorsFound := make(chan error, 4*iterations)
	start := make(chan struct{})
	var ready sync.WaitGroup
	var workers sync.WaitGroup
	run := func(name string, operation func(int) error) {
		ready.Add(1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			ready.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				if err := operation(iteration); err != nil {
					errorsFound <- formatX11LifecycleError(name, iteration, err)
				}
			}
		}()
	}
	run("capture", func(iteration int) error {
		if iteration%50 == 0 {
			image, err := robotgo.CaptureImg()
			if err != nil {
				return err
			}
			if bounds := image.Bounds(); bounds.Dx() <= 0 || bounds.Dy() <= 0 {
				return fmt.Errorf("argumentless capture bounds = %v, want non-zero dimensions", bounds)
			}
			return nil
		}
		image, err := robotgo.CaptureImg(0, 0, 16, 16)
		if err != nil {
			return err
		}
		if bounds := image.Bounds(); bounds.Dx() != 16 || bounds.Dy() != 16 {
			return fmt.Errorf("capture bounds = %v, want 16x16", bounds)
		}
		return nil
	})
	run("keyboard", func(_ int) error { return robotgo.KeyPress("enter") })
	run("pointer", func(iteration int) error {
		return robotgo.MoveE(180+iteration%2, 170)
	})
	run("display lifecycle", func(_ int) error {
		if err := robotgo.SetXDisplayName(display); err != nil {
			return err
		}
		runtime.Gosched()
		return robotgo.CloseMainDisplayE()
	})
	run("window error handler", func(_ int) error {
		robotgo.SetHandle(int(harness.window))
		_ = robotgo.SetActiveE(active)
		_ = robotgo.GetPid()
		_ = robotgo.SysScale()
		return nil
	})
	ready.Wait()
	close(start)
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent X11 lifecycle operation: %v", err)
	}
	if t.Failed() {
		return
	}
	if err := robotgo.KeyboardReady(); err != nil {
		t.Fatalf("KeyboardReady after lifecycle stress: %v", err)
	}
	if x, y := harness.queryPointer(); x < 0 || y < 0 {
		t.Fatalf("invalid pointer after lifecycle stress: (%d, %d)", x, y)
	}
}

func TestNativeX11EnvironmentToggleLifecycleConcurrent(t *testing.T) {
	harness := newX11InputHarness(t)
	assertExpectedX11Implementation(t)
	display := os.Getenv("DISPLAY")
	active := robotgo.GetActive()
	originalDisplay, displayWasSet := os.LookupEnv("DISPLAY")
	originalWayland, waylandWasSet := os.LookupEnv("WAYLAND_DISPLAY")
	originalDisablePortal, disablePortalWasSet := os.LookupEnv("ROBOTGO_DISABLE_PORTAL")
	originalXDisplay := robotgo.GetXDisplayName()

	restoreEnvironment := func() {
		restoreX11EnvironmentVariable("DISPLAY", originalDisplay, displayWasSet)
		restoreX11EnvironmentVariable("WAYLAND_DISPLAY", originalWayland, waylandWasSet)
		restoreX11EnvironmentVariable("ROBOTGO_DISABLE_PORTAL", originalDisablePortal, disablePortalWasSet)
	}
	t.Cleanup(func() {
		restoreEnvironment()
		if err := robotgo.SetXDisplayName(originalXDisplay); err != nil {
			t.Errorf("restore X11 display override: %v", err)
		}
		_ = robotgo.CloseMainDisplayE()
	})

	if err := os.Setenv("ROBOTGO_DISABLE_PORTAL", "1"); err != nil {
		t.Fatalf("disable portal during environment stress: %v", err)
	}
	if err := robotgo.SetXDisplayName(display); err != nil {
		t.Fatalf("SetXDisplayName before environment stress: %v", err)
	}

	const iterations = 150
	errorsFound := make(chan error, 4*iterations)
	start := make(chan struct{})
	var ready sync.WaitGroup
	var workers sync.WaitGroup
	run := func(name string, operation func(int) error) {
		ready.Add(1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			ready.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				if err := operation(iteration); err != nil {
					errorsFound <- formatX11LifecycleError(name, iteration, err)
				}
			}
		}()
	}
	run("capture", func(_ int) error {
		image, err := robotgo.CaptureImg(0, 0, 8, 8)
		if err != nil {
			return nil // Backend errors are expected while the environment is transient.
		}
		if bounds := image.Bounds(); bounds.Dx() != 8 || bounds.Dy() != 8 {
			return fmt.Errorf("successful capture bounds = %v, want 8x8", bounds)
		}
		return nil
	})
	run("window and scale", func(_ int) error {
		robotgo.SetHandle(int(harness.window))
		_ = robotgo.SetActiveE(active)
		_ = robotgo.GetPid()
		_ = robotgo.SysScale()
		return nil
	})
	run("display lifecycle", func(_ int) error {
		if err := robotgo.SetXDisplayName(display); err != nil {
			return err
		}
		runtime.Gosched()
		return robotgo.CloseMainDisplayE()
	})
	run("environment", func(iteration int) error {
		switch iteration % 3 {
		case 0:
			if err := os.Setenv("DISPLAY", display); err != nil {
				return err
			}
			return os.Unsetenv("WAYLAND_DISPLAY")
		case 1:
			if err := os.Unsetenv("DISPLAY"); err != nil {
				return err
			}
			return os.Setenv("WAYLAND_DISPLAY", "robotgo-test-wayland")
		default:
			if err := os.Unsetenv("DISPLAY"); err != nil {
				return err
			}
			return os.Unsetenv("WAYLAND_DISPLAY")
		}
	})
	ready.Wait()
	close(start)
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent X11 environment/lifecycle operation: %v", err)
	}
	if t.Failed() {
		return
	}

	restoreEnvironment()
	if err := robotgo.SetXDisplayName(display); err != nil {
		t.Fatalf("restore active X11 display after environment stress: %v", err)
	}
	if err := robotgo.CloseMainDisplayE(); err != nil {
		t.Fatalf("reset native display after environment stress: %v", err)
	}
	image, err := robotgo.CaptureImg()
	if err != nil {
		t.Fatalf("argumentless CaptureImg after environment stress: %v", err)
	}
	if bounds := image.Bounds(); bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Fatalf("argumentless capture bounds after environment stress = %v", bounds)
	}
	if x, y := harness.queryPointer(); x < 0 || y < 0 {
		t.Fatalf("invalid pointer after environment stress: (%d, %d)", x, y)
	}
}

func TestX11MissingXTestReadinessContract(t *testing.T) {
	if os.Getenv(envExpectedX11NoXTest) != "1" {
		t.Skipf("set %s=1 under an X11 server with XTEST disabled", envExpectedX11NoXTest)
	}
	display := os.Getenv("DISPLAY")
	if display == "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Fatalf("missing-XTEST test requires an X11-primary DISPLAY, got DISPLAY=%q WAYLAND_DISPLAY=%q", display, os.Getenv("WAYLAND_DISPLAY"))
	}
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		t.Fatalf("connect to X11 server without XTEST: %v", err)
	}
	setup := xproto.Setup(conn)
	if setup == nil {
		conn.Close()
		t.Fatal("X11 server without XTEST has no default screen")
	}
	harness := &x11InputHarness{t: t, conn: conn, root: setup.DefaultScreen(conn).Root}
	t.Cleanup(conn.Close)
	t.Cleanup(func() { _ = robotgo.CloseMainDisplayE() })

	harness.drainEvents()
	beforeKeyboard := harness.keyboardState()
	beforeInput := harness.inputState()
	for name, readiness := range map[string]func() error{
		"keyboard": robotgo.KeyboardReady,
		"mouse":    robotgo.MouseReady,
	} {
		err := readiness()
		if !errors.Is(err, robotgo.ErrNotSupported) || !strings.Contains(err.Error(), "XTEST") {
			t.Errorf("%s readiness error = %v, want ErrNotSupported mentioning XTEST", name, err)
		}
	}
	for name, operation := range map[string]func() error{
		"TypeStrE": func() error { return robotgo.TypeStrE("A", 0, 0, 0) },
		"KeyPress": func() error { return robotgo.KeyPress("enter") },
		"KeyDown":  func() error { return robotgo.KeyDown("a") },
	} {
		err := operation()
		if !errors.Is(err, robotgo.ErrNotSupported) || !strings.Contains(err.Error(), "XTEST") {
			t.Errorf("%s without XTEST error = %v, want ErrNotSupported mentioning XTEST", name, err)
		}
	}
	if err := robotgo.KeyUp("a"); !errors.Is(err, robotgo.ErrInputOwnership) {
		t.Errorf("orphan KeyUp without XTEST error = %v, want ErrInputOwnership", err)
	}
	harness.conn.Sync()
	assertX11KeyboardStateEqual(t, beforeKeyboard, harness.keyboardState())
	assertX11InputStateEqual(t, beforeInput, harness.inputState())
	harness.assertNoInputEvent("input event from rejected readiness probes", 100*time.Millisecond)

	capabilities := robotgo.GetRuntimeCapabilities()
	if !capabilities.Capture.Available || !capabilities.Bounds.Available {
		t.Errorf("display capabilities unavailable without XTEST: capture=%+v bounds=%+v", capabilities.Capture, capabilities.Bounds)
	}
	for name, capability := range map[string]robotgo.FeatureCapability{
		"keyboard": capabilities.Keyboard,
		"mouse":    capabilities.Mouse,
	} {
		if capability.Available || !strings.Contains(capability.Reason, "XTEST") {
			t.Errorf("%s capability = %+v, want unavailable with XTEST reason", name, capability)
		}
	}
	image, err := robotgo.CaptureImg(0, 0, 8, 8)
	if err != nil {
		t.Fatalf("CaptureImg without XTEST: %v", err)
	}
	if got := image.Bounds(); got.Dx() != 8 || got.Dy() != 8 {
		t.Fatalf("CaptureImg bounds without XTEST = %v, want 8x8", got)
	}
	if rect := robotgo.GetScreenRect(); rect.W <= 0 || rect.H <= 0 {
		t.Fatalf("GetScreenRect without XTEST = %+v, want non-zero dimensions", rect)
	}
}

func assertNoNativeX11KeyEvent(t *testing.T, harness *x11InputHarness, description string) {
	t.Helper()
	harness.assertNoMatchingEvent(description, 100*time.Millisecond, func(event xgb.Event) bool {
		switch event.(type) {
		case xproto.KeyPressEvent, xproto.KeyReleaseEvent:
			return true
		default:
			return false
		}
	})
}

func holdIndependentX11Key(t *testing.T, harness *x11InputHarness, code xproto.Keycode) {
	t.Helper()
	if harness.keyPressed(code) {
		t.Fatalf("cannot establish foreign ownership: keycode %d is already pressed", code)
	}
	harness.fakeKey(code, true)
	harness.conn.Sync()
	if !harness.keyPressed(code) {
		t.Fatalf("independent connection did not hold keycode %d", code)
	}
	harness.drainEvents()
	t.Cleanup(func() {
		if harness.keyPressed(code) {
			harness.fakeKey(code, false)
			harness.conn.Sync()
		}
	})
}

func toggleIndependentX11Lock(t *testing.T, harness *x11InputHarness, code xproto.Keycode) {
	t.Helper()
	harness.fakeKey(code, true)
	harness.fakeKey(code, false)
	harness.conn.Sync()
}

func removeX11Keysym(t *testing.T, harness *x11InputHarness, keysym xproto.Keysym) {
	t.Helper()
	before := harness.keyboardState()
	without := append([]xproto.Keysym(nil), before.keysyms...)
	replaced := 0
	for index, value := range without {
		if value == keysym {
			without[index] = 0
			replaced++
		}
	}
	if replaced == 0 {
		t.Fatalf("X11 keymap does not contain keysym %#x", keysym)
	}
	keycodeCount := len(without) / int(before.keysymsPerKeycode)
	t.Cleanup(func() {
		if err := xproto.ChangeKeyboardMappingChecked(
			harness.conn,
			byte(keycodeCount),
			before.minKeycode,
			before.keysymsPerKeycode,
			before.keysyms,
		).Check(); err != nil {
			t.Errorf("restore X11 keyboard mapping: %v", err)
		}
		harness.conn.Sync()
	})
	if err := xproto.ChangeKeyboardMappingChecked(
		harness.conn,
		byte(keycodeCount),
		before.minKeycode,
		before.keysymsPerKeycode,
		without,
	).Check(); err != nil {
		t.Fatalf("remove X11 keysym %#x: %v", keysym, err)
	}
	harness.conn.Sync()
}

func formatX11LifecycleError(operation string, iteration int, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s iteration %d: %w", operation, iteration, err)
}

func restoreX11EnvironmentVariable(name, value string, wasSet bool) {
	if wasSet {
		_ = os.Setenv(name, value)
		return
	}
	_ = os.Unsetenv(name)
}

func setNativeX11Layout(t *testing.T, layout string) {
	t.Helper()
	path, err := exec.LookPath("setxkbmap")
	if err != nil {
		x11Unavailable(t, "native X11 layout test requires setxkbmap: %v", err)
	}
	query, err := exec.Command(path, "-query").Output()
	if err != nil {
		x11Unavailable(t, "query active X11 keymap: %v", err)
	}
	restoreArgs := x11SetxkbmapArgs(query)
	t.Cleanup(func() {
		_ = robotgo.CloseMainDisplayE()
		if output, restoreErr := exec.Command(path, restoreArgs...).CombinedOutput(); restoreErr != nil {
			t.Errorf("restore X11 keymap: %v (%s)", restoreErr, strings.TrimSpace(string(output)))
		}
		_ = robotgo.CloseMainDisplayE()
	})

	output, err := exec.Command(path, "-layout", layout, "-variant", "", "-option", "").CombinedOutput()
	if err != nil {
		x11Unavailable(t, "activate X11 layout %q: %v (%s)", layout, err, strings.TrimSpace(string(output)))
	}
}

func x11SetxkbmapArgs(query []byte) []string {
	values := make(map[string]string)
	for _, line := range strings.Split(string(query), "\n") {
		name, value, found := strings.Cut(line, ":")
		if found {
			values[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	args := make([]string, 0, 12)
	for _, item := range []struct {
		name string
		flag string
	}{
		{name: "rules", flag: "-rules"},
		{name: "model", flag: "-model"},
		{name: "layout", flag: "-layout"},
		{name: "variant", flag: "-variant"},
	} {
		if value := values[item.name]; value != "" {
			args = append(args, item.flag, value)
		}
	}
	args = append(args, "-option", "")
	if options := values["options"]; options != "" {
		args = append(args, "-option", options)
	}
	return args
}
