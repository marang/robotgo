//go:build linux && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

const (
	x11KeysymF1 = 0xffbe
)

var x11KeyHoldDelay = 5 * time.Millisecond
var x11BeforeTextTapHook func()

var x11ModifierNames = map[string]struct{}{
	"none": {},
	"alt":  {}, "lalt": {}, "ralt": {},
	"cmd": {}, "command": {}, "lcmd": {}, "rcmd": {},
	"ctrl": {}, "control": {}, "lctrl": {}, "rctrl": {},
	"shift": {}, "lshift": {}, "rshift": {}, "right_shift": {},
}

var x11ShiftedSpecialKeys = map[rune]rune{
	'`': '~', '1': '!', '2': '@', '3': '#', '4': '$', '5': '%',
	'6': '^', '7': '&', '8': '*', '9': '(', '0': ')', '-': '_',
	'=': '+', '[': '{', ']': '}', '\\': '|', ';': ':', '\'': '"',
	',': '<', '.': '>', '/': '?',
}

type x11ResolvedKey struct {
	code xproto.Keycode
}

type x11KeyboardMap struct {
	minimum    xproto.Keycode
	perKeycode byte
	keysyms    []xproto.Keysym
	modifiers  map[xproto.Keycode]struct{}
}

type x11ScratchSlot struct {
	code   xproto.Keycode
	keysym uint32
}

type x11HeldKey struct {
	code      xproto.Keycode
	modifiers []xproto.Keycode
}

func (backend *x11InputBackend) KeyboardReady() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	if err := backend.loadKeyboardMapLocked(); err != nil {
		return backend.failLocked("load keyboard map", err)
	}
	return nil
}

func (backend *x11InputBackend) loadKeyboardMapLocked() error {
	setup := xproto.Setup(backend.conn)
	if setup == nil {
		return errors.New("missing setup information")
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	if count <= 0 || count > math.MaxUint8 {
		return fmt.Errorf("invalid keycode range %d..%d", setup.MinKeycode, setup.MaxKeycode)
	}
	reply, err := xproto.GetKeyboardMapping(backend.conn, setup.MinKeycode, byte(count)).Reply()
	if err != nil {
		return err
	}
	if reply == nil || reply.KeysymsPerKeycode == 0 {
		return errors.New("server returned an empty keyboard map")
	}
	expected := count * int(reply.KeysymsPerKeycode)
	if len(reply.Keysyms) < expected {
		return fmt.Errorf("short keyboard map: got %d keysyms, want at least %d", len(reply.Keysyms), expected)
	}
	modifiers, err := backend.modifierKeycodesLocked()
	if err != nil {
		return err
	}
	keyboard := x11KeyboardMap{
		minimum:    setup.MinKeycode,
		perKeycode: reply.KeysymsPerKeycode,
		keysyms:    append([]xproto.Keysym(nil), reply.Keysyms[:expected]...),
		modifiers:  modifiers,
	}
	if err := backend.updateScratchStateLocked(&keyboard); err != nil {
		return err
	}
	backend.keyboard = keyboard
	return nil
}

func (backend *x11InputBackend) modifierKeycodesLocked() (map[xproto.Keycode]struct{}, error) {
	reply, err := xproto.GetModifierMapping(backend.conn).Reply()
	if err != nil {
		return nil, errors.Join(errX11Connection, err)
	}
	if reply == nil {
		return nil, errors.Join(errX11Connection, errors.New("server returned an empty modifier map"))
	}
	modifiers := make(map[xproto.Keycode]struct{})
	for _, code := range reply.Keycodes {
		if code != 0 {
			modifiers[code] = struct{}{}
		}
	}
	return modifiers, nil
}

func (keyboard *x11KeyboardMap) resolve(keysym uint32) (x11ResolvedKey, bool) {
	per := int(keyboard.perKeycode)
	if per == 0 {
		return x11ResolvedKey{}, false
	}
	for offset := 0; offset+per <= len(keyboard.keysyms); offset += per {
		// The core map begins G1L1, G1L2, G2L1, G2L2. Without an XKB state
		// client, a keycode is unambiguous only when every nonempty group and
		// level resolves to the same symbol.
		mapping := keyboard.keysyms[offset : offset+per]
		if x11MappingOwnedBy(mapping, keysym) {
			return x11ResolvedKey{
				code: keyboard.minimum + xproto.Keycode(offset/per),
			}, true
		}
	}
	return x11ResolvedKey{}, false
}

func (keyboard *x11KeyboardMap) resolveModifier(keysym uint32) (x11ResolvedKey, bool) {
	per := int(keyboard.perKeycode)
	if per == 0 {
		return x11ResolvedKey{}, false
	}
	for offset := 0; offset+per <= len(keyboard.keysyms); offset += per {
		code := keyboard.minimum + xproto.Keycode(offset/per)
		if _, modifier := keyboard.modifiers[code]; !modifier {
			continue
		}
		for _, value := range keyboard.keysyms[offset : offset+per] {
			if uint32(value) == keysym {
				return x11ResolvedKey{code: code}, true
			}
		}
	}
	return x11ResolvedKey{}, false
}

func (keyboard *x11KeyboardMap) mapping(code xproto.Keycode) ([]xproto.Keysym, bool) {
	per := int(keyboard.perKeycode)
	index := int(code) - int(keyboard.minimum)
	if per == 0 || index < 0 {
		return nil, false
	}
	offset := index * per
	if offset+per > len(keyboard.keysyms) {
		return nil, false
	}
	return keyboard.keysyms[offset : offset+per], true
}

func x11MappingIs(mapping []xproto.Keysym, keysym uint32) bool {
	if len(mapping) == 0 {
		return false
	}
	for _, value := range mapping {
		if uint32(value) != keysym {
			return false
		}
	}
	return true
}

func x11MappingOwnedBy(mapping []xproto.Keysym, keysym uint32) bool {
	if keysym == 0 {
		return x11MappingIs(mapping, 0)
	}
	if len(mapping) == 0 || uint32(mapping[0]) != keysym {
		return false
	}
	for _, value := range mapping[1:] {
		if value != 0 && uint32(value) != keysym {
			return false
		}
	}
	return true
}

func (backend *x11InputBackend) updateScratchStateLocked(keyboard *x11KeyboardMap) error {
	if !backend.scratchInitialized {
		per := int(keyboard.perKeycode)
		for offset := 0; offset+per <= len(keyboard.keysyms); offset += per {
			code := keyboard.minimum + xproto.Keycode(offset/per)
			if _, modifier := keyboard.modifiers[code]; modifier {
				continue
			}
			if x11MappingIs(keyboard.keysyms[offset:offset+per], 0) {
				backend.scratchSlots = append(backend.scratchSlots, x11ScratchSlot{
					code: code,
				})
			}
		}
		backend.scratchInitialized = true
		backend.scratchPerKeycode = keyboard.perKeycode
		backend.scratchByKeysym = make(map[uint32]xproto.Keycode)
		return nil
	}
	if keyboard.perKeycode != backend.scratchPerKeycode {
		return fmt.Errorf("X11 keysyms-per-keycode changed from %d to %d while RobotGo owns scratch mappings",
			backend.scratchPerKeycode, keyboard.perKeycode)
	}
	kept := backend.scratchSlots[:0]
	for _, slot := range backend.scratchSlots {
		if _, modifier := keyboard.modifiers[slot.code]; modifier {
			if slot.keysym != 0 {
				return fmt.Errorf("X11 scratch keycode %d became a modifier while RobotGo owns its mapping", slot.code)
			}
			continue
		}
		mapping, ok := keyboard.mapping(slot.code)
		if !ok {
			return fmt.Errorf("X11 scratch keycode %d disappeared from the keyboard map", slot.code)
		}
		if slot.keysym == 0 {
			if x11MappingIs(mapping, 0) {
				kept = append(kept, slot)
			}
			continue
		}
		if !x11MappingOwnedBy(mapping, slot.keysym) {
			return fmt.Errorf("X11 scratch keycode %d was changed by another client", slot.code)
		}
		kept = append(kept, slot)
	}
	backend.scratchSlots = kept
	return nil
}

func x11ScratchMappingCanRestore(current *xproto.GetKeyboardMappingReply, keysym uint32) (byte, bool, error) {
	if current == nil || current.KeysymsPerKeycode == 0 {
		return 0, false, errors.New("server returned an empty scratch-key mapping")
	}
	width := int(current.KeysymsPerKeycode)
	if len(current.Keysyms) < width {
		return 0, false, fmt.Errorf("server returned %d scratch keysyms, want at least %d", len(current.Keysyms), width)
	}
	return current.KeysymsPerKeycode, x11MappingOwnedBy(current.Keysyms[:width], keysym), nil
}

func (backend *x11InputBackend) restoreScratchMappingsLocked() error {
	if backend.conn == nil || backend.scratchPerKeycode == 0 {
		return nil
	}
	return backend.withServerGrabLocked(func() error {
		var restoreErr error
		var pressed []byte
		var modifiers map[xproto.Keycode]struct{}
		stateLoaded := false
		for index := range backend.scratchSlots {
			slot := &backend.scratchSlots[index]
			if slot.keysym == 0 {
				continue
			}
			current, err := xproto.GetKeyboardMapping(backend.conn, slot.code, 1).Reply()
			if err != nil {
				restoreErr = errors.Join(restoreErr, errX11Connection,
					fmt.Errorf("query X11 scratch keycode %d during cleanup: %w", slot.code, err))
				continue
			}
			width, owned, ownershipErr := x11ScratchMappingCanRestore(current, slot.keysym)
			if ownershipErr != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("query X11 scratch keycode %d during cleanup: %w", slot.code, ownershipErr))
				continue
			}
			if !owned {
				// Another client now owns this mapping. Preserve its state rather
				// than treating intentional non-ownership as a cleanup failure.
				delete(backend.scratchByKeysym, slot.keysym)
				slot.keysym = 0
				continue
			}
			if !stateLoaded {
				pressed, err = backend.pressedKeysLocked()
				if err != nil {
					return errors.Join(restoreErr, err)
				}
				modifiers, err = backend.modifierKeycodesLocked()
				if err != nil {
					return errors.Join(restoreErr, err)
				}
				stateLoaded = true
			}
			if x11KeycodePressed(pressed, slot.code) {
				restoreErr = errors.Join(restoreErr, fmt.Errorf(
					"cannot restore X11 scratch keycode %d while it is pressed; reset the mapping after the key is released", slot.code,
				))
				continue
			}
			if _, modifier := modifiers[slot.code]; modifier {
				restoreErr = errors.Join(restoreErr, fmt.Errorf(
					"cannot restore X11 scratch keycode %d after it became a modifier; reset the mapping after restoring the modifier map", slot.code,
				))
				continue
			}
			empty := make([]xproto.Keysym, int(width))
			if err := xproto.ChangeKeyboardMappingChecked(
				backend.conn, 1, slot.code, width, empty,
			).Check(); err != nil {
				restoreErr = errors.Join(restoreErr, errX11Connection,
					fmt.Errorf("restore X11 scratch keycode %d: %w", slot.code, err))
				continue
			}
			delete(backend.scratchByKeysym, slot.keysym)
			slot.keysym = 0
		}
		return restoreErr
	})
}

func x11KeysymForKey(key string) (uint32, error) {
	if key == "" || !utf8.ValidString(key) {
		return 0, fmt.Errorf("%w: invalid X11 keyboard key %q", ErrNotSupported, key)
	}
	if named, ok := portalNamedKeysyms[strings.ToLower(key)]; ok {
		return uint32(named), nil
	}
	lower := strings.ToLower(key)
	if strings.HasPrefix(lower, "f") {
		number, err := strconv.Atoi(strings.TrimPrefix(lower, "f"))
		if err == nil && number >= 1 && number <= 24 {
			return x11KeysymF1 + uint32(number-1), nil
		}
	}
	if utf8.RuneCountInString(key) == 1 {
		value, _ := utf8.DecodeRuneInString(key)
		keysym, err := portalKeysymForRune(value)
		return uint32(keysym), err
	}
	return 0, fmt.Errorf("%w: X11 keyboard key %q", ErrNotSupported, key)
}

func (backend *x11InputBackend) sendKeyLocked(code xproto.Keycode, down bool) error {
	eventType := byte(xproto.KeyRelease)
	if down {
		eventType = byte(xproto.KeyPress)
	}
	if err := xtest.FakeInputChecked(backend.conn, eventType, byte(code), 0, backend.root, 0, 0, 0).Check(); err != nil {
		return errors.Join(errX11Connection, err)
	}
	return nil
}

func appendUniqueKeycode(codes []xproto.Keycode, code xproto.Keycode) []xproto.Keycode {
	if code == 0 {
		return codes
	}
	for _, existing := range codes {
		if existing == code {
			return codes
		}
	}
	return append(codes, code)
}

func (backend *x11InputBackend) modifierCodesLocked(names []string) ([]xproto.Keycode, error) {
	codes := make([]xproto.Keycode, 0, len(names)+2)
	for _, name := range names {
		if strings.EqualFold(name, "none") {
			continue
		}
		keysym, err := x11KeysymForKey(name)
		if err != nil {
			return nil, err
		}
		modifier, ok := backend.keyboard.resolveModifier(keysym)
		if !ok {
			return nil, fmt.Errorf("%w: modifier %q is absent from the active X11 keymap", ErrNotSupported, name)
		}
		codes = appendUniqueKeycode(codes, modifier.code)
	}
	return codes, nil
}

func (backend *x11InputBackend) reserveScratchMappingsLocked(keysyms []uint32) error {
	return backend.withServerGrabLocked(func() error {
		// Refresh after grabbing the server. Another client may have claimed an
		// originally empty keycode before this transaction; never overwrite it.
		if err := backend.loadKeyboardMapLocked(); err != nil {
			return errors.Join(errX11KeyboardMap, err)
		}
		return backend.reserveScratchMappingsUnderGrabLocked(keysyms)
	})
}

func (backend *x11InputBackend) reserveScratchMappingsUnderGrabLocked(keysyms []uint32) error {
	pressed, err := backend.pressedKeysLocked()
	if err != nil {
		return err
	}
	if err := backend.validateScratchCapacityLocked(keysyms, pressed); err != nil {
		return err
	}
	for _, keysym := range keysyms {
		if _, assigned := backend.scratchByKeysym[keysym]; assigned {
			continue
		}
		if _, err := backend.assignScratchMappingLocked(keysym, pressed); err != nil {
			return err
		}
	}
	return nil
}

func (backend *x11InputBackend) validateScratchCapacityLocked(keysyms []uint32, pressed []byte) error {
	missing := make(map[uint32]struct{}, len(keysyms))
	for _, keysym := range keysyms {
		if _, assigned := backend.scratchByKeysym[keysym]; !assigned {
			missing[keysym] = struct{}{}
		}
	}
	available := 0
	for _, slot := range backend.scratchSlots {
		if slot.keysym == 0 && !x11KeycodePressed(pressed, slot.code) {
			available++
		}
	}
	if len(missing) > available {
		return fmt.Errorf("%w: X11 keyboard input requires %d new stable scratch keycodes, but only %d are available; call CloseMainDisplayE after the target has processed prior keyboard input to reset the pool",
			ErrNotSupported, len(missing), available)
	}
	return nil
}

func (backend *x11InputBackend) assignScratchMappingLocked(keysym uint32, pressed []byte) (xproto.Keycode, error) {
	if code, assigned := backend.scratchByKeysym[keysym]; assigned {
		return code, nil
	}
	for index := range backend.scratchSlots {
		slot := &backend.scratchSlots[index]
		if slot.keysym != 0 || x11KeycodePressed(pressed, slot.code) {
			continue
		}
		replacement := make([]xproto.Keysym, int(backend.keyboard.perKeycode))
		for column := range replacement {
			replacement[column] = xproto.Keysym(keysym)
		}
		if err := xproto.ChangeKeyboardMappingChecked(
			backend.conn, 1, slot.code, backend.keyboard.perKeycode, replacement,
		).Check(); err != nil {
			return 0, errors.Join(errX11Connection, err)
		}
		slot.keysym = keysym
		backend.scratchByKeysym[keysym] = slot.code
		mapping, ok := backend.keyboard.mapping(slot.code)
		if !ok {
			return 0, errors.Join(errX11Connection, fmt.Errorf("assigned X11 scratch keycode %d disappeared", slot.code))
		}
		copy(mapping, replacement)
		return slot.code, nil
	}
	return 0, fmt.Errorf("%w: no stable X11 scratch keycode is available", ErrNotSupported)
}

// prepareKeysymUnderGrabLocked must run while this X client owns the server
// grab and after loadKeyboardMapLocked refreshed the global keymap.
func (backend *x11InputBackend) prepareKeysymUnderGrabLocked(keysym uint32, allowScratch, forceScratch bool) (x11ResolvedKey, error) {
	if !forceScratch {
		if resolved, ok := backend.keyboard.resolve(keysym); ok {
			return resolved, nil
		}
	}
	if !allowScratch {
		return x11ResolvedKey{}, fmt.Errorf("%w: keysym %#x has no unambiguous mapping in the active X11 keymap", ErrNotSupported, keysym)
	}
	if err := backend.reserveScratchMappingsUnderGrabLocked([]uint32{keysym}); err != nil {
		return x11ResolvedKey{}, err
	}
	return x11ResolvedKey{code: backend.scratchByKeysym[keysym]}, nil
}

func x11LiteralKey(key string) bool {
	if !utf8.ValidString(key) {
		return false
	}
	if _, named := portalNamedKeysyms[strings.ToLower(key)]; named {
		return false
	}
	if utf8.RuneCountInString(key) != 1 {
		return false
	}
	_, size := utf8.DecodeRuneInString(key)
	return size > 0
}

func validateX11KeyEvent(event pureGoKeyEvent) error {
	if !event.Tap && event.Down && x11LiteralKey(event.Key) {
		return fmt.Errorf("%w: Pure-Go X11 cannot safely hold literal keys across XKB layout groups; use KeyTap, KeyPress, or a named key", ErrNotSupported)
	}
	for _, modifier := range event.Modifiers {
		if _, supported := x11ModifierNames[strings.ToLower(modifier)]; !supported {
			return fmt.Errorf("%w: Pure-Go X11 modifier %q is not a supported modifier key", ErrNotSupported, modifier)
		}
	}
	return nil
}

func x11EventKeysym(event pureGoKeyEvent) (uint32, error) {
	key := event.Key
	if x11LiteralKey(key) && x11EventHasShift(event.Modifiers) {
		value, _ := utf8.DecodeRuneInString(key)
		if shifted, ok := x11ShiftedSpecialKeys[value]; ok {
			value = shifted
		} else {
			value = unicode.ToUpper(value)
		}
		key = string(value)
	}
	return x11KeysymForKey(key)
}

func x11EventHasShift(modifiers []string) bool {
	for _, modifier := range modifiers {
		switch strings.ToLower(modifier) {
		case "shift", "lshift", "rshift", "right_shift":
			return true
		}
	}
	return false
}

func (backend *x11InputBackend) pressedKeysLocked() ([]byte, error) {
	reply, err := xproto.QueryKeymap(backend.conn).Reply()
	if err != nil {
		return nil, errors.Join(errX11Connection, err)
	}
	if reply == nil || len(reply.Keys) != 32 {
		return nil, errors.Join(errX11Connection, errors.New("X11 returned an invalid pressed-key map"))
	}
	return reply.Keys, nil
}

func x11KeycodePressed(keys []byte, code xproto.Keycode) bool {
	index := int(code) / 8
	return index < len(keys) && keys[index]&(1<<uint(code%8)) != 0
}

func (backend *x11InputBackend) acquireModifierLocked(code xproto.Keycode, pressed []byte) (bool, error) {
	if backend.keyRefs == nil {
		backend.keyRefs = make(map[xproto.Keycode]int)
	}
	if backend.keyRefs[code] > 0 {
		backend.keyRefs[code]++
		return true, nil
	}
	if x11KeycodePressed(pressed, code) {
		return false, nil
	}
	if err := backend.sendKeyLocked(code, true); err != nil {
		return false, err
	}
	backend.keyRefs[code] = 1
	backend.keyOrder = append(backend.keyOrder, code)
	return true, nil
}

func (backend *x11InputBackend) acquireMainKeyLocked(code xproto.Keycode, pressed []byte) error {
	if backend.keyRefs[code] > 0 || x11KeycodePressed(pressed, code) {
		return fmt.Errorf("robotgo: X11 keycode %d is already held; refusing to alter foreign input state", code)
	}
	if err := backend.sendKeyLocked(code, true); err != nil {
		return err
	}
	if backend.keyRefs == nil {
		backend.keyRefs = make(map[xproto.Keycode]int)
	}
	backend.keyRefs[code] = 1
	backend.keyOrder = append(backend.keyOrder, code)
	return nil
}

func (backend *x11InputBackend) releaseOwnedKeyLocked(code xproto.Keycode) error {
	refs := backend.keyRefs[code]
	if refs <= 0 {
		return nil
	}
	if refs > 1 {
		backend.keyRefs[code] = refs - 1
		return nil
	}
	if err := backend.sendKeyLocked(code, false); err != nil {
		return err
	}
	delete(backend.keyRefs, code)
	backend.keyOrder = removeX11Keycode(backend.keyOrder, code)
	return nil
}

func removeX11Keycode(codes []xproto.Keycode, code xproto.Keycode) []xproto.Keycode {
	for index := len(codes) - 1; index >= 0; index-- {
		if codes[index] != code {
			continue
		}
		copy(codes[index:], codes[index+1:])
		codes[len(codes)-1] = 0
		return codes[:len(codes)-1]
	}
	return codes
}

func (backend *x11InputBackend) releaseOwnedModifiersLocked(codes []xproto.Keycode) error {
	var releaseErr error
	for index := len(codes) - 1; index >= 0; index-- {
		releaseErr = errors.Join(releaseErr, backend.releaseOwnedKeyLocked(codes[index]))
	}
	return releaseErr
}

func (backend *x11InputBackend) acquireModifiersLocked(codes []xproto.Keycode, pressed []byte) ([]xproto.Keycode, error) {
	owned := make([]xproto.Keycode, 0, len(codes))
	for _, code := range codes {
		acquired, err := backend.acquireModifierLocked(code, pressed)
		if err != nil {
			return nil, errors.Join(err, backend.releaseOwnedModifiersLocked(owned))
		}
		if acquired {
			owned = append(owned, code)
		}
	}
	return owned, nil
}

func (backend *x11InputBackend) tapResolvedLocked(resolved x11ResolvedKey, modifierCodes []xproto.Keycode) error {
	pressed, err := backend.pressedKeysLocked()
	if err != nil {
		return err
	}
	ownedModifiers, err := backend.acquireModifiersLocked(modifierCodes, pressed)
	if err != nil {
		return err
	}
	mainDown := backend.acquireMainKeyLocked(resolved.code, pressed)
	var mainUp error
	if mainDown == nil {
		time.Sleep(x11KeyHoldDelay)
		mainUp = backend.releaseOwnedKeyLocked(resolved.code)
	}
	return errors.Join(mainDown, mainUp, backend.releaseOwnedModifiersLocked(ownedModifiers))
}

func (backend *x11InputBackend) tapKeysymLocked(keysym uint32, modifiers []string, allowScratch, forceScratch bool) error {
	return backend.withServerGrabLocked(func() error {
		if err := backend.loadKeyboardMapLocked(); err != nil {
			return errors.Join(errX11KeyboardMap, err)
		}
		modifierCodes, err := backend.modifierCodesLocked(modifiers)
		if err != nil {
			return err
		}
		resolved, err := backend.prepareKeysymUnderGrabLocked(keysym, allowScratch, forceScratch)
		if err != nil {
			return err
		}
		return backend.tapResolvedLocked(resolved, modifierCodes)
	})
}

func (backend *x11InputBackend) toggleDownResolvedLocked(keysym uint32, resolved x11ResolvedKey, modifierCodes []xproto.Keycode) error {
	if backend.heldKeys != nil {
		if _, held := backend.heldKeys[keysym]; held {
			return nil
		}
	}
	pressed, err := backend.pressedKeysLocked()
	if err != nil {
		return err
	}
	ownedModifiers, err := backend.acquireModifiersLocked(modifierCodes, pressed)
	if err != nil {
		return err
	}
	if err := backend.acquireMainKeyLocked(resolved.code, pressed); err != nil {
		return errors.Join(err, backend.releaseOwnedModifiersLocked(ownedModifiers))
	}
	if backend.heldKeys == nil {
		backend.heldKeys = make(map[uint32]x11HeldKey)
	}
	backend.heldKeys[keysym] = x11HeldKey{code: resolved.code, modifiers: ownedModifiers}
	return nil
}

func (backend *x11InputBackend) toggleDownKeysymLocked(keysym uint32, modifiers []string) error {
	return backend.withServerGrabLocked(func() error {
		if err := backend.loadKeyboardMapLocked(); err != nil {
			return errors.Join(errX11KeyboardMap, err)
		}
		modifierCodes, err := backend.modifierCodesLocked(modifiers)
		if err != nil {
			return err
		}
		resolved, err := backend.prepareKeysymUnderGrabLocked(keysym, false, false)
		if err != nil {
			return err
		}
		return backend.toggleDownResolvedLocked(keysym, resolved, modifierCodes)
	})
}

func (backend *x11InputBackend) toggleUpLocked(keysym uint32) error {
	held, ok := backend.heldKeys[keysym]
	if !ok {
		return fmt.Errorf("robotgo: X11 key %#x is not held by this RobotGo backend", keysym)
	}
	err := errors.Join(
		backend.releaseOwnedKeyLocked(held.code),
		backend.releaseOwnedModifiersLocked(held.modifiers),
	)
	if err == nil {
		delete(backend.heldKeys, keysym)
	}
	return err
}

func (backend *x11InputBackend) Key(event pureGoKeyEvent) (err error) {
	if event.PID != 0 {
		return fmt.Errorf("%w: Pure-Go X11 input cannot target a process", ErrNotSupported)
	}
	if err := validateX11KeyEvent(event); err != nil {
		return err
	}
	keysym, err := x11EventKeysym(event)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	if !event.Tap && !event.Down {
		err = backend.withServerGrabLocked(func() error { return backend.toggleUpLocked(keysym) })
		if errors.Is(err, errX11Connection) {
			err = errors.Join(err, backend.closeLocked())
		}
		return err
	}
	if event.Tap {
		err = backend.tapKeysymLocked(keysym, event.Modifiers, true, x11LiteralKey(event.Key))
	} else {
		err = backend.toggleDownKeysymLocked(keysym, event.Modifiers)
	}
	if errors.Is(err, errX11Connection) || errors.Is(err, errX11KeyboardMap) {
		return backend.failLocked("inject keyboard input", err)
	}
	return err
}

func (backend *x11InputBackend) Text(event pureGoTextEvent) (err error) {
	if event.PID != 0 {
		return fmt.Errorf("%w: Pure-Go X11 input cannot target a process", ErrNotSupported)
	}
	if event.Delay < 0 {
		return errors.New("robotgo: text delay must be non-negative")
	}
	if !utf8.ValidString(event.Text) {
		return errors.New("robotgo: text is not valid UTF-8")
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	keysyms := make([]uint32, 0, utf8.RuneCountInString(event.Text))
	for _, value := range event.Text {
		keysym, keysymErr := portalKeysymForRune(value)
		if keysymErr != nil {
			return keysymErr
		}
		keysyms = append(keysyms, uint32(keysym))
	}
	if reserveErr := backend.reserveScratchMappingsLocked(keysyms); reserveErr != nil {
		if errors.Is(reserveErr, errX11Connection) || errors.Is(reserveErr, errX11KeyboardMap) {
			return backend.failLocked("prepare Unicode keyboard mappings", reserveErr)
		}
		return reserveErr
	}
	if x11BeforeTextTapHook != nil {
		x11BeforeTextTapHook()
	}
	for _, keysym := range keysyms {
		tapErr := backend.tapKeysymLocked(keysym, nil, true, true)
		if tapErr != nil {
			err = tapErr
			if errors.Is(tapErr, errX11Connection) || errors.Is(tapErr, errX11KeyboardMap) {
				err = errors.Join(err, backend.closeLocked())
			}
			return err
		}
		MilliSleep(event.Delay)
	}
	return nil
}
