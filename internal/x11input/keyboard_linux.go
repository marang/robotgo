//go:build linux

package x11input

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jezek/xgb/xproto"
)

// KeyEvent is a normalized X11 keysym transaction. Public key names and
// aliases are resolved by the caller before entering the stateful core.
type KeyEvent struct {
	Keysym       uint32
	Modifiers    []uint32
	Down         bool
	Tap          bool
	AllowScratch bool
	ForceScratch bool
}

// TextEvent is one prevalidated text transaction expressed as X11 keysyms.
type TextEvent struct {
	Keysyms []uint32
	Delay   time.Duration
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

func (backend *Backend) KeyboardReady() error {
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

func (backend *Backend) loadKeyboardMapLocked() error {
	setup, err := backend.conn.Setup()
	if err != nil {
		return errors.Join(errX11Connection, err)
	}
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	if count <= 0 || count > math.MaxUint8 {
		return fmt.Errorf("invalid keycode range %d..%d", setup.MinKeycode, setup.MaxKeycode)
	}
	reply, err := backend.conn.KeyboardMapping(setup.MinKeycode, byte(count))
	if err != nil {
		return err
	}
	if reply.KeysymsPerKeycode == 0 {
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

func (backend *Backend) modifierKeycodesLocked() (map[xproto.Keycode]struct{}, error) {
	keycodes, err := backend.conn.ModifierMapping()
	if err != nil {
		return nil, errors.Join(errX11Connection, err)
	}
	modifiers := make(map[xproto.Keycode]struct{})
	for _, code := range keycodes {
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

func (backend *Backend) updateScratchStateLocked(keyboard *x11KeyboardMap) error {
	if !backend.scratchInitialized {
		backend.initializeScratchStateLocked(keyboard)
		return nil
	}
	if keyboard.perKeycode != backend.scratchPerKeycode {
		if len(backend.scratchByKeysym) == 0 {
			backend.initializeScratchStateLocked(keyboard)
			return nil
		}
		return fmt.Errorf("X11 keysyms-per-keycode changed from %d to %d while RobotGo owns scratch mappings",
			backend.scratchPerKeycode, keyboard.perKeycode)
	}
	kept := make([]x11ScratchSlot, 0, len(backend.scratchSlots))
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

func (backend *Backend) initializeScratchStateLocked(keyboard *x11KeyboardMap) {
	per := int(keyboard.perKeycode)
	slots := make([]x11ScratchSlot, 0)
	for offset := 0; offset+per <= len(keyboard.keysyms); offset += per {
		code := keyboard.minimum + xproto.Keycode(offset/per)
		if _, modifier := keyboard.modifiers[code]; modifier {
			continue
		}
		if x11MappingIs(keyboard.keysyms[offset:offset+per], 0) {
			slots = append(slots, x11ScratchSlot{code: code})
		}
	}
	backend.scratchInitialized = true
	backend.scratchPerKeycode = keyboard.perKeycode
	backend.scratchSlots = slots
	backend.scratchByKeysym = make(map[uint32]xproto.Keycode)
}

func x11ScratchMappingCanRestore(current KeyboardMapping, keysym uint32) (byte, bool, error) {
	if current.KeysymsPerKeycode == 0 {
		return 0, false, errors.New("server returned an empty scratch-key mapping")
	}
	width := int(current.KeysymsPerKeycode)
	if len(current.Keysyms) < width {
		return 0, false, fmt.Errorf("server returned %d scratch keysyms, want at least %d", len(current.Keysyms), width)
	}
	return current.KeysymsPerKeycode, x11MappingOwnedBy(current.Keysyms[:width], keysym), nil
}

func (backend *Backend) restoreScratchMappingsLocked() error {
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
			current, err := backend.conn.KeyboardMapping(slot.code, 1)
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
			if err := backend.conn.ChangeKeyboardMapping(slot.code, width, empty); err != nil {
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

func (backend *Backend) sendKeyLocked(code xproto.Keycode, down bool) error {
	eventType := byte(xproto.KeyRelease)
	if down {
		eventType = byte(xproto.KeyPress)
	}
	if err := backend.conn.FakeInput(eventType, byte(code), backend.root, 0, 0); err != nil {
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

func (backend *Backend) modifierCodesLocked(keysyms []uint32) ([]xproto.Keycode, error) {
	codes := make([]xproto.Keycode, 0, len(keysyms)+2)
	for _, keysym := range keysyms {
		modifier, ok := backend.keyboard.resolveModifier(keysym)
		if !ok {
			return nil, fmt.Errorf("%w: modifier keysym %#x is absent from the active X11 modifier map", ErrUnsupported, keysym)
		}
		codes = appendUniqueKeycode(codes, modifier.code)
	}
	return codes, nil
}

func (backend *Backend) reserveScratchMappingsLocked(keysyms []uint32) error {
	return backend.withServerGrabLocked(func() error {
		// Refresh after grabbing the server. Another client may have claimed an
		// originally empty keycode before this transaction; never overwrite it.
		if err := backend.loadKeyboardMapLocked(); err != nil {
			return errors.Join(errX11KeyboardMap, err)
		}
		return backend.reserveScratchMappingsUnderGrabLocked(keysyms)
	})
}

func (backend *Backend) reserveScratchMappingsUnderGrabLocked(keysyms []uint32) error {
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

func (backend *Backend) validateScratchCapacityLocked(keysyms []uint32, pressed []byte) error {
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
			ErrUnsupported, len(missing), available)
	}
	return nil
}

func (backend *Backend) assignScratchMappingLocked(keysym uint32, pressed []byte) (xproto.Keycode, error) {
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
		if err := backend.conn.ChangeKeyboardMapping(
			slot.code, backend.keyboard.perKeycode, replacement,
		); err != nil {
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
	return 0, fmt.Errorf("%w: no stable X11 scratch keycode is available", ErrUnsupported)
}

// prepareKeysymUnderGrabLocked must run while this X client owns the server
// grab and after loadKeyboardMapLocked refreshed the global keymap.
func (backend *Backend) prepareKeysymUnderGrabLocked(keysym uint32, allowScratch, forceScratch bool) (x11ResolvedKey, error) {
	if !forceScratch {
		if resolved, ok := backend.keyboard.resolve(keysym); ok {
			return resolved, nil
		}
	}
	if !allowScratch {
		return x11ResolvedKey{}, fmt.Errorf("%w: keysym %#x has no unambiguous mapping in the active X11 keymap", ErrUnsupported, keysym)
	}
	if err := backend.reserveScratchMappingsUnderGrabLocked([]uint32{keysym}); err != nil {
		return x11ResolvedKey{}, err
	}
	return x11ResolvedKey{code: backend.scratchByKeysym[keysym]}, nil
}

func (backend *Backend) pressedKeysLocked() ([]byte, error) {
	keys, err := backend.conn.PressedKeys()
	if err != nil {
		return nil, errors.Join(errX11Connection, err)
	}
	if len(keys) != 32 {
		return nil, errors.Join(errX11Connection, errors.New("X11 returned an invalid pressed-key map"))
	}
	return keys, nil
}

func x11KeycodePressed(keys []byte, code xproto.Keycode) bool {
	index := int(code) / 8
	return index < len(keys) && keys[index]&(1<<uint(code%8)) != 0
}

func (backend *Backend) acquireModifierLocked(code xproto.Keycode, pressed []byte) (bool, error) {
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

func (backend *Backend) acquireMainKeyLocked(code xproto.Keycode, pressed []byte) error {
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

func (backend *Backend) releaseOwnedKeyLocked(code xproto.Keycode) error {
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

func (backend *Backend) releaseOwnedModifiersLocked(codes []xproto.Keycode) error {
	var releaseErr error
	for index := len(codes) - 1; index >= 0; index-- {
		releaseErr = errors.Join(releaseErr, backend.releaseOwnedKeyLocked(codes[index]))
	}
	return releaseErr
}

func (backend *Backend) acquireModifiersLocked(codes []xproto.Keycode, pressed []byte) ([]xproto.Keycode, error) {
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

func (backend *Backend) tapResolvedLocked(resolved x11ResolvedKey, modifierCodes []xproto.Keycode) error {
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
		backend.config.Sleep(backend.config.KeyHoldDelay)
		mainUp = backend.releaseOwnedKeyLocked(resolved.code)
	}
	return errors.Join(mainDown, mainUp, backend.releaseOwnedModifiersLocked(ownedModifiers))
}

func (backend *Backend) tapKeysymLocked(keysym uint32, modifiers []uint32, allowScratch, forceScratch bool) error {
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

func (backend *Backend) toggleDownResolvedLocked(keysym uint32, resolved x11ResolvedKey, modifierCodes []xproto.Keycode) error {
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

func (backend *Backend) toggleDownKeysymLocked(keysym uint32, modifiers []uint32) error {
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

func (backend *Backend) toggleUpLocked(keysym uint32) error {
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

func (backend *Backend) Key(event KeyEvent) (err error) {
	keysym := event.Keysym
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
		err = backend.tapKeysymLocked(keysym, event.Modifiers, event.AllowScratch, event.ForceScratch)
	} else {
		err = backend.toggleDownKeysymLocked(keysym, event.Modifiers)
	}
	if errors.Is(err, errX11Connection) || errors.Is(err, errX11KeyboardMap) {
		return backend.failLocked("inject keyboard input", err)
	}
	return err
}

func (backend *Backend) Text(event TextEvent) (err error) {
	if event.Delay < 0 {
		return errors.New("robotgo: text delay must be non-negative")
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	keysyms := append([]uint32(nil), event.Keysyms...)
	if reserveErr := backend.reserveScratchMappingsLocked(keysyms); reserveErr != nil {
		if errors.Is(reserveErr, errX11Connection) || errors.Is(reserveErr, errX11KeyboardMap) {
			return backend.failLocked("prepare Unicode keyboard mappings", reserveErr)
		}
		return reserveErr
	}
	if backend.beforeTextTap != nil {
		backend.beforeTextTap()
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
		backend.config.Sleep(event.Delay)
	}
	return nil
}
