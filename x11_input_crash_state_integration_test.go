//go:build linux && !cgo && x11integration && !wayland

package robotgo_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

type x11CrashWorkloadClaim struct {
	ready         bool
	heldKeycode   xproto.Keycode
	button        byte
	buttonMask    uint16
	scratchCode   xproto.Keycode
	scratchBefore []xproto.Keysym
	scratchAfter  []xproto.Keysym
}

type x11CrashObservedState struct {
	keyboard x11KeyboardState
	input    x11InputState
}

type x11CrashObservedWire struct {
	MinKeycode          xproto.Keycode   `json:"min_keycode"`
	KeysymsPerKeycode   byte             `json:"keysyms_per_keycode"`
	Keysyms             []xproto.Keysym  `json:"keysyms"`
	KeycodesPerModifier byte             `json:"keycodes_per_modifier"`
	ModifierKeycodes    []xproto.Keycode `json:"modifier_keycodes"`
	PressedKeys         []byte           `json:"pressed_keys"`
	PointerX            int              `json:"pointer_x"`
	PointerY            int              `json:"pointer_y"`
	PointerMask         uint16           `json:"pointer_mask"`
}

func x11CrashForeignKey(t *testing.T, harness *x11InputHarness, input x11InputState) xproto.Keycode {
	t.Helper()
	for _, keysym := range []uint32{'a', 'b', 'c'} {
		keycode, _ := harness.findKeycode(keysym)
		if keycode != 0 && !x11CrashKeyPressed(input.pressedKeys, keycode) {
			return keycode
		}
	}
	t.Fatal("cannot find an unpressed mapped key for the foreign-input baseline")
	return 0
}

func x11CrashForeignButton(t *testing.T, pointerMask uint16) (byte, uint16) {
	t.Helper()
	for _, candidate := range []struct {
		button byte
		mask   uint16
	}{
		{button: x11ButtonLeft, mask: xproto.ButtonMask1},
		{button: x11CrashMiddleButton, mask: xproto.ButtonMask2},
	} {
		if pointerMask&candidate.mask == 0 {
			return candidate.button, candidate.mask
		}
	}
	t.Fatal("cannot find an unpressed pointer button for the foreign-input baseline")
	return 0, 0
}

func x11CrashKeyPressed(pressed []byte, keycode xproto.Keycode) bool {
	index := int(keycode) / 8
	return index < len(pressed) && pressed[index]&(1<<uint(keycode%8)) != 0
}

func x11CrashClaim(before, during x11KeyboardState, ready x11CrashReadyMessage) (x11CrashWorkloadClaim, error) {
	beforeMapping, err := x11CrashMappingAt(before, ready.scratchCode)
	if err != nil {
		return x11CrashWorkloadClaim{}, fmt.Errorf("baseline scratch mapping: %w", err)
	}
	duringMapping, err := x11CrashMappingAt(during, ready.scratchCode)
	if err != nil {
		return x11CrashWorkloadClaim{}, fmt.Errorf("active scratch mapping: %w", err)
	}
	if !x11CrashMappingIs(beforeMapping, 0) {
		return x11CrashWorkloadClaim{}, fmt.Errorf("scratch keycode %d was not empty in baseline: %#v", ready.scratchCode, beforeMapping)
	}
	if !x11CrashMappingOwnedBy(duringMapping, xproto.Keysym(ready.keysym)) {
		return x11CrashWorkloadClaim{}, fmt.Errorf("scratch keycode %d is not the helper claim: %#v", ready.scratchCode, duringMapping)
	}
	return x11CrashWorkloadClaim{
		ready:         true,
		heldKeycode:   ready.heldKeycode,
		button:        x11ButtonRight,
		buttonMask:    xproto.ButtonMask3,
		scratchCode:   ready.scratchCode,
		scratchBefore: beforeMapping,
		scratchAfter:  duringMapping,
	}, nil
}

func x11CrashMappingAt(state x11KeyboardState, keycode xproto.Keycode) ([]xproto.Keysym, error) {
	if state.keysymsPerKeycode == 0 || keycode < state.minKeycode {
		return nil, fmt.Errorf("keycode %d is outside keyboard-map baseline", keycode)
	}
	per := int(state.keysymsPerKeycode)
	offset := int(keycode-state.minKeycode) * per
	if offset < 0 || offset+per > len(state.keysyms) {
		return nil, fmt.Errorf("keycode %d is outside keyboard-map keysyms", keycode)
	}
	return append([]xproto.Keysym(nil), state.keysyms[offset:offset+per]...), nil
}

func x11CrashKeyboardBytes(state x11KeyboardState) []byte {
	buffer := bytes.NewBuffer(make([]byte, 0, 16+len(state.keysyms)*4+len(state.modifierKeycodes)))
	buffer.WriteString("robotgo-x11-keyboard-v1\x00")
	buffer.WriteByte(byte(state.minKeycode))
	buffer.WriteByte(state.keysymsPerKeycode)
	_ = binary.Write(buffer, binary.LittleEndian, uint32(len(state.keysyms)))
	for _, keysym := range state.keysyms {
		_ = binary.Write(buffer, binary.LittleEndian, uint32(keysym))
	}
	buffer.WriteByte(state.keycodesPerModifier)
	_ = binary.Write(buffer, binary.LittleEndian, uint32(len(state.modifierKeycodes)))
	for _, keycode := range state.modifierKeycodes {
		buffer.WriteByte(byte(keycode))
	}
	return buffer.Bytes()
}

func x11CrashScratchMutation(before, during x11KeyboardState) (xproto.Keycode, error) {
	if before.minKeycode != during.minKeycode || before.keysymsPerKeycode != during.keysymsPerKeycode || len(before.keysyms) != len(during.keysyms) {
		return 0, fmt.Errorf("keyboard-map shape changed: %s", x11KeyboardStateDifference(before, during))
	}
	if before.keycodesPerModifier != during.keycodesPerModifier || !bytes.Equal(x11CrashModifierBytes(before), x11CrashModifierBytes(during)) {
		return 0, fmt.Errorf("modifier map changed while establishing scratch mapping: %s", x11KeyboardStateDifference(before, during))
	}
	per := int(before.keysymsPerKeycode)
	want := xproto.Keysym(x11KeysymForRune('😀'))
	changed := make([]xproto.Keycode, 0, 1)
	for offset := 0; offset+per <= len(before.keysyms); offset += per {
		beforeMapping := before.keysyms[offset : offset+per]
		duringMapping := during.keysyms[offset : offset+per]
		if x11CrashMappingsEqual(beforeMapping, duringMapping) {
			continue
		}
		if !x11CrashMappingIs(beforeMapping, 0) || !x11CrashMappingOwnedBy(duringMapping, want) {
			code := before.minKeycode + xproto.Keycode(offset/per)
			return 0, fmt.Errorf("unexpected mapping change at keycode %d: before=%#v during=%#v", code, beforeMapping, duringMapping)
		}
		changed = append(changed, before.minKeycode+xproto.Keycode(offset/per))
	}
	if len(changed) != 1 {
		return 0, fmt.Errorf("changed scratch keycodes = %v, want exactly one", changed)
	}
	return changed[0], nil
}

func x11CrashModifierBytes(state x11KeyboardState) []byte {
	result := make([]byte, 1+len(state.modifierKeycodes))
	result[0] = state.keycodesPerModifier
	for index, keycode := range state.modifierKeycodes {
		result[index+1] = byte(keycode)
	}
	return result
}

func x11CrashMappingsEqual(left, right []xproto.Keysym) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func x11CrashMappingIs(mapping []xproto.Keysym, want xproto.Keysym) bool {
	if len(mapping) == 0 {
		return false
	}
	for _, keysym := range mapping {
		if keysym != want {
			return false
		}
	}
	return true
}

func x11CrashMappingOwnedBy(mapping []xproto.Keysym, want xproto.Keysym) bool {
	if len(mapping) == 0 || mapping[0] != want {
		return false
	}
	for _, keysym := range mapping[1:] {
		if keysym != 0 && keysym != want {
			return false
		}
	}
	return true
}

func x11CrashHeldInput(before, during x11InputState, harness *x11InputHarness) (xproto.Keycode, error) {
	added, removed := x11CrashPressedKeyDifference(before.pressedKeys, during.pressedKeys)
	if len(removed) != 0 || len(added) != 1 {
		return 0, fmt.Errorf("pressed-key delta added=%v removed=%v, want one added Enter", added, removed)
	}
	if got := harness.keysym(added[0]); got != x11KeysymEnter {
		return 0, fmt.Errorf("newly held keycode %d has keysym %#x, want Enter %#x", added[0], got, x11KeysymEnter)
	}
	if before.pointerMask&xproto.ButtonMask3 != 0 {
		return 0, errors.New("baseline unexpectedly has the right pointer button held")
	}
	if during.pointerMask != before.pointerMask|xproto.ButtonMask3 {
		return 0, fmt.Errorf("pointer mask changed from %#x to %#x, want only Button3", before.pointerMask, during.pointerMask)
	}
	if during.pointerX != before.pointerX || during.pointerY != before.pointerY {
		return 0, fmt.Errorf("pointer moved from (%d,%d) to (%d,%d)", before.pointerX, before.pointerY, during.pointerX, during.pointerY)
	}
	return added[0], nil
}

func x11CrashPressedKeyDifference(before, after []byte) (added, removed []xproto.Keycode) {
	length := len(before)
	if len(after) < length {
		length = len(after)
	}
	for index := 0; index < length*8; index++ {
		mask := byte(1 << uint(index%8))
		wasPressed := before[index/8]&mask != 0
		isPressed := after[index/8]&mask != 0
		switch {
		case !wasPressed && isPressed:
			added = append(added, xproto.Keycode(index))
		case wasPressed && !isPressed:
			removed = append(removed, xproto.Keycode(index))
		}
	}
	return added, removed
}

func x11CrashFindPressedKeysym(
	connection *xgb.Conn,
	setup *xproto.SetupInfo,
	pressed []byte,
	want xproto.Keysym,
) xproto.Keycode {
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	mapping, err := xproto.GetKeyboardMapping(connection, setup.MinKeycode, byte(count)).Reply()
	if err != nil || mapping == nil || mapping.KeysymsPerKeycode == 0 {
		return 0
	}
	per := int(mapping.KeysymsPerKeycode)
	for offset := 0; offset+per <= len(mapping.Keysyms); offset += per {
		code := setup.MinKeycode + xproto.Keycode(offset/per)
		if int(code)/8 >= len(pressed) || pressed[int(code)/8]&(1<<uint(code%8)) == 0 {
			continue
		}
		for _, keysym := range mapping.Keysyms[offset : offset+per] {
			if keysym == want {
				return code
			}
		}
	}
	return 0
}

func x11CrashAwaitRestoration(
	beforeKeyboard x11KeyboardState,
	beforeKeyboardBytes []byte,
	beforeInput x11InputState,
	beforeXKB []byte,
	compareXKB bool,
) error {
	deadline := time.Now().Add(x11CrashRestoreTimeout)
	var lastDifference string
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New(lastDifference)
		}
		observerTimeout := x11CrashObserverTimeout
		if observerTimeout > remaining {
			observerTimeout = remaining
		}
		afterKeyboard, afterInput, snapshotErr := x11CrashObserveState(observerTimeout)
		if snapshotErr != nil {
			lastDifference = fmt.Sprintf("bounded X11 state snapshot failed: %v", snapshotErr)
			x11CrashWaitForNextPoll(deadline)
			continue
		}
		keyboardEqual := bytes.Equal(x11CrashKeyboardBytes(afterKeyboard), beforeKeyboardBytes)
		inputEqual := x11CrashInputEqual(afterInput, beforeInput)
		if keyboardEqual && inputEqual {
			if !compareXKB {
				return nil
			}
			remaining = time.Until(deadline)
			if remaining <= 0 {
				return errors.New(lastDifference)
			}
			xkbTimeout := x11CrashXKBCompTimeout
			if xkbTimeout > remaining {
				xkbTimeout = remaining
			}
			afterXKB, xkbErr := x11CrashXKBCompSnapshotRaw(xkbTimeout)
			if xkbErr == nil && bytes.Equal(afterXKB, beforeXKB) {
				return nil
			}
			if xkbErr != nil {
				lastDifference = fmt.Sprintf("xkbcomp snapshot failed while polling restoration: %v", xkbErr)
			} else {
				lastDifference = fmt.Sprintf(
					"core/input restored but xkbcomp differs: before=%x after=%x",
					sha256.Sum256(beforeXKB),
					sha256.Sum256(afterXKB),
				)
			}
		} else {
			keyboardDifference := "equal"
			if !keyboardEqual {
				keyboardDifference = x11KeyboardStateDifference(beforeKeyboard, afterKeyboard)
			}
			lastDifference = fmt.Sprintf(
				"keyboard=%s input_before=%+v input_after=%+v",
				keyboardDifference,
				beforeInput,
				afterInput,
			)
		}
		x11CrashWaitForNextPoll(deadline)
	}
}

func x11CrashObserveState(timeout time.Duration) (x11KeyboardState, x11InputState, error) {
	if timeout <= 0 {
		return x11KeyboardState{}, x11InputState{}, errors.New("X11 state snapshot timeout is exhausted")
	}
	executable, err := os.Executable()
	if err != nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("resolve observer executable: %w", err)
	}
	resultRead, resultWrite, err := os.Pipe()
	if err != nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("create observer result pipe: %w", err)
	}
	defer func() { _ = resultRead.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		executable,
		"-test.run="+x11CrashTestPattern,
		"-test.count=1",
		"-test.timeout="+(timeout+x11CrashObserverShutdownWindow).String(),
	)
	command.Env = x11CrashObserverEnvironment(os.Environ())
	command.ExtraFiles = []*os.File{resultWrite}
	command.WaitDelay = x11CrashObserverShutdownWindow
	var log bytes.Buffer
	command.Stdout = &log
	command.Stderr = &log
	if err := command.Start(); err != nil {
		_ = resultWrite.Close()
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("start isolated X11 observer: %w", err)
	}
	if err := resultWrite.Close(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("close parent observer result writer: %w", err)
	}
	payload, readErr := io.ReadAll(resultRead)
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("X11 state snapshot timed out after %s: %w", timeout, ctx.Err())
	}
	if waitErr != nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("isolated X11 observer failed: %w: %s", waitErr, strings.TrimSpace(log.String()))
	}
	if readErr != nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("read isolated X11 observer result: %w", readErr)
	}
	var wire x11CrashObservedWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("decode isolated X11 observer result: %w", err)
	}
	observed := wire.observedState()
	return observed.keyboard, observed.input, nil
}

func x11CrashWriteObserverSnapshot(t *testing.T) {
	t.Helper()
	resultFile := os.NewFile(x11CrashReadyFD, "robotgo-x11-crash-observer-result")
	if resultFile == nil {
		t.Fatalf("crash observer has no result fd %d", x11CrashReadyFD)
	}
	defer func() { _ = resultFile.Close() }()
	connection, err := x11CrashOpenObserver()
	if err != nil {
		t.Fatalf("open isolated X11 observer: %v", err)
	}
	defer connection.Close()
	keyboard, input, err := x11CrashObserveStateOnConnection(connection)
	if err != nil {
		t.Fatalf("take isolated X11 observer snapshot: %v", err)
	}
	if err := json.NewEncoder(resultFile).Encode(x11CrashWireState(keyboard, input)); err != nil {
		t.Fatalf("write isolated X11 observer snapshot: %v", err)
	}
	if err := resultFile.Close(); err != nil {
		t.Fatalf("close isolated X11 observer result: %v", err)
	}
}

func x11CrashWireState(keyboard x11KeyboardState, input x11InputState) x11CrashObservedWire {
	return x11CrashObservedWire{
		MinKeycode:          keyboard.minKeycode,
		KeysymsPerKeycode:   keyboard.keysymsPerKeycode,
		Keysyms:             keyboard.keysyms,
		KeycodesPerModifier: keyboard.keycodesPerModifier,
		ModifierKeycodes:    keyboard.modifierKeycodes,
		PressedKeys:         input.pressedKeys,
		PointerX:            input.pointerX,
		PointerY:            input.pointerY,
		PointerMask:         input.pointerMask,
	}
}

func (wire x11CrashObservedWire) observedState() x11CrashObservedState {
	return x11CrashObservedState{
		keyboard: x11KeyboardState{
			minKeycode:          wire.MinKeycode,
			keysymsPerKeycode:   wire.KeysymsPerKeycode,
			keysyms:             append([]xproto.Keysym(nil), wire.Keysyms...),
			keycodesPerModifier: wire.KeycodesPerModifier,
			modifierKeycodes:    append([]xproto.Keycode(nil), wire.ModifierKeycodes...),
		},
		input: x11InputState{
			pressedKeys: append([]byte(nil), wire.PressedKeys...),
			pointerX:    wire.PointerX,
			pointerY:    wire.PointerY,
			pointerMask: wire.PointerMask,
		},
	}
}

func x11CrashOpenObserver() (*xgb.Conn, error) {
	connection, err := xgb.NewConnDisplay(os.Getenv(x11CrashDisplayEnv))
	if err != nil {
		return nil, err
	}
	return connection, nil
}

func x11CrashObserveStateOnConnection(connection *xgb.Conn) (x11KeyboardState, x11InputState, error) {
	setup := xproto.Setup(connection)
	if setup == nil {
		return x11KeyboardState{}, x11InputState{}, errors.New("X11 observer has no setup")
	}
	screen := setup.DefaultScreen(connection)
	if screen == nil {
		return x11KeyboardState{}, x11InputState{}, errors.New("X11 observer has no default screen")
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	keyboard, err := xproto.GetKeyboardMapping(connection, setup.MinKeycode, byte(count)).Reply()
	if err != nil || keyboard == nil || keyboard.KeysymsPerKeycode == 0 {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("query observer keyboard map: reply=%+v err=%v", keyboard, err)
	}
	modifiers, err := xproto.GetModifierMapping(connection).Reply()
	if err != nil || modifiers == nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("query observer modifier map: reply=%+v err=%v", modifiers, err)
	}
	keys, err := xproto.QueryKeymap(connection).Reply()
	if err != nil || keys == nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("query observer pressed keys: reply=%+v err=%v", keys, err)
	}
	pointer, err := xproto.QueryPointer(connection, screen.Root).Reply()
	if err != nil || pointer == nil {
		return x11KeyboardState{}, x11InputState{}, fmt.Errorf("query observer pointer: reply=%+v err=%v", pointer, err)
	}
	return x11KeyboardState{
			minKeycode:          setup.MinKeycode,
			keysymsPerKeycode:   keyboard.KeysymsPerKeycode,
			keysyms:             append([]xproto.Keysym(nil), keyboard.Keysyms...),
			keycodesPerModifier: modifiers.KeycodesPerModifier,
			modifierKeycodes:    append([]xproto.Keycode(nil), modifiers.Keycodes...),
		}, x11InputState{
			pressedKeys: append([]byte(nil), keys.Keys...),
			pointerX:    int(pointer.RootX),
			pointerY:    int(pointer.RootY),
			pointerMask: pointer.Mask,
		}, nil
}

func x11CrashWaitForNextPoll(deadline time.Time) {
	delay := time.Until(deadline)
	if delay > x11CrashPollInterval {
		delay = x11CrashPollInterval
	}
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	<-timer.C
}

func x11CrashInputEqual(left, right x11InputState) bool {
	return bytes.Equal(left.pressedKeys, right.pressedKeys) &&
		left.pointerX == right.pointerX &&
		left.pointerY == right.pointerY &&
		left.pointerMask == right.pointerMask
}

func x11CrashXKBCompSnapshot(t *testing.T) ([]byte, bool) {
	t.Helper()
	output, err := x11CrashXKBCompSnapshotRaw(x11CrashXKBCompTimeout)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) && os.Getenv(x11RequiredEnv) != "1" {
			t.Logf("xkbcomp snapshot unavailable: %v", err)
			return nil, false
		}
		t.Fatalf("snapshot XKB map with xkbcomp: %v", err)
	}
	return output, true
}

func x11CrashXKBCompSnapshotRaw(timeout time.Duration) ([]byte, error) {
	path, err := exec.LookPath("xkbcomp")
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		return nil, errors.New("xkbcomp timeout is exhausted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, path, "-xkb", os.Getenv(x11CrashDisplayEnv), "-")
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("run xkbcomp: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return bytes.TrimSpace(output), nil
}

func x11CrashEmergencyRestore(
	harness *x11InputHarness,
	claim x11CrashWorkloadClaim,
) (restoreErr error) {
	keys, err := xproto.QueryKeymap(harness.conn).Reply()
	if err != nil {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("query held keys for emergency cleanup: %w", err))
	} else if keys == nil {
		restoreErr = errors.Join(restoreErr, errors.New("query held keys for emergency cleanup returned no reply"))
	} else if x11CrashKeyPressed(keys.Keys, claim.heldKeycode) {
		if err := xtest.FakeInputChecked(
			harness.conn,
			byte(xproto.KeyRelease),
			byte(claim.heldKeycode),
			0,
			harness.root,
			0,
			0,
			0,
		).Check(); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("release claimed emergency keycode %d: %w", claim.heldKeycode, err))
		}
	}
	pointer, err := xproto.QueryPointer(harness.conn, harness.root).Reply()
	if err != nil {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("query pointer for emergency cleanup: %w", err))
	} else if pointer == nil {
		restoreErr = errors.Join(restoreErr, errors.New("query pointer for emergency cleanup returned no reply"))
	} else if pointer.Mask&claim.buttonMask != 0 {
		if err := xtest.FakeInputChecked(
			harness.conn,
			byte(xproto.ButtonRelease),
			claim.button,
			0,
			harness.root,
			0,
			0,
			0,
		).Check(); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("release claimed emergency button %d: %w", claim.button, err))
		}
	}

	// Release only the two exact synthetic holds before grabbing the server.
	// The mapping is restored only if its final image still exactly matches the
	// recorded claim and the keycode is neither pressed nor a modifier.
	if err := xproto.GrabServerChecked(harness.conn).Check(); err != nil {
		return errors.Join(restoreErr, fmt.Errorf("grab server for emergency keymap restore: %w", err))
	}
	defer func() {
		if err := xproto.UngrabServerChecked(harness.conn).Check(); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("ungrab server after emergency keymap restore: %w", err))
		}
	}()
	current, err := xproto.GetKeyboardMapping(harness.conn, claim.scratchCode, 1).Reply()
	if err != nil {
		return errors.Join(restoreErr, fmt.Errorf("query claimed emergency scratch mapping: %w", err))
	}
	if current == nil || current.KeysymsPerKeycode != byte(len(claim.scratchAfter)) || len(current.Keysyms) != len(claim.scratchAfter) {
		return errors.Join(restoreErr, fmt.Errorf("query claimed emergency scratch mapping returned incompatible reply: %+v", current))
	}
	if x11CrashMappingsEqual(current.Keysyms, claim.scratchBefore) {
		return restoreErr
	}
	if !x11CrashMappingsEqual(current.Keysyms, claim.scratchAfter) {
		return errors.Join(restoreErr, fmt.Errorf("preserved foreign replacement of claimed scratch keycode %d: %#v", claim.scratchCode, current.Keysyms))
	}
	held, heldErr := xproto.QueryKeymap(harness.conn).Reply()
	modifiers, modifierErr := xproto.GetModifierMapping(harness.conn).Reply()
	if heldErr != nil || held == nil {
		return errors.Join(restoreErr, fmt.Errorf("refuse emergency scratch restore: query pressed state for keycode %d: reply=%+v err=%v", claim.scratchCode, held, heldErr))
	}
	if modifierErr != nil || modifiers == nil {
		return errors.Join(restoreErr, fmt.Errorf("refuse emergency scratch restore: query modifier state for keycode %d: reply=%+v err=%v", claim.scratchCode, modifiers, modifierErr))
	}
	if x11CrashKeyPressed(held.Keys, claim.scratchCode) {
		return errors.Join(restoreErr, fmt.Errorf("refuse emergency scratch restore: claimed keycode %d is pressed", claim.scratchCode))
	}
	if x11CrashModifierContains(modifiers.Keycodes, claim.scratchCode) {
		return errors.Join(restoreErr, fmt.Errorf("refuse emergency scratch restore: claimed keycode %d is assigned as a modifier", claim.scratchCode))
	}
	if err := xproto.ChangeKeyboardMappingChecked(
		harness.conn,
		1,
		claim.scratchCode,
		byte(len(claim.scratchBefore)),
		claim.scratchBefore,
	).Check(); err != nil {
		return errors.Join(restoreErr, fmt.Errorf("restore claimed emergency scratch keycode %d: %w", claim.scratchCode, err))
	}
	verified, err := xproto.GetKeyboardMapping(harness.conn, claim.scratchCode, 1).Reply()
	if err != nil || verified == nil || !x11CrashMappingsEqual(verified.Keysyms, claim.scratchBefore) {
		return errors.Join(restoreErr, fmt.Errorf("verify claimed emergency scratch restore: reply=%+v err=%v", verified, err))
	}
	return restoreErr
}

func x11CrashModifierContains(keycodes []xproto.Keycode, keycode xproto.Keycode) bool {
	for _, candidate := range keycodes {
		if candidate == keycode {
			return true
		}
	}
	return false
}

func x11CrashVerifyBaseline(
	keyboard []byte,
	input x11InputState,
	xkbSnapshot []byte,
	compareXKB bool,
) error {
	var verifyErr error
	afterKeyboard, afterInput, err := x11CrashObserveState(x11CrashObserverTimeout)
	if err != nil {
		return fmt.Errorf("verify emergency X11 baseline: %w", err)
	}
	if !bytes.Equal(x11CrashKeyboardBytes(afterKeyboard), keyboard) {
		verifyErr = errors.Join(verifyErr, errors.New("emergency cleanup did not restore the canonical core/modifier map"))
	}
	if !x11CrashInputEqual(afterInput, input) {
		verifyErr = errors.Join(verifyErr, fmt.Errorf(
			"emergency cleanup input mismatch: before=%+v after=%+v",
			input,
			afterInput,
		))
	}
	if compareXKB {
		afterXKB, err := x11CrashXKBCompSnapshotRaw(x11CrashXKBCompTimeout)
		if err != nil {
			verifyErr = errors.Join(verifyErr, fmt.Errorf("verify emergency XKB snapshot: %w", err))
		} else if !bytes.Equal(afterXKB, xkbSnapshot) {
			verifyErr = errors.Join(verifyErr, fmt.Errorf(
				"emergency XKB snapshot mismatch: before=%x after=%x",
				sha256.Sum256(xkbSnapshot),
				sha256.Sum256(afterXKB),
			))
		}
	}
	return verifyErr
}
