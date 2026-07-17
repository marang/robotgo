package darwininput

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// KeyEvent is one normalized Quartz keyboard operation.
type KeyEvent struct {
	Key       string
	Modifiers []string
	PID       int
	Down      bool
	Tap       bool
}

// TextEvent is one exact Unicode text operation.
type TextEvent struct {
	Text  string
	PID   int
	Delay time.Duration
}

type ownedKey struct {
	code uint16
	flag uint64
	pid  int32
	name string
}

var namedKeyCodes = map[string]uint16{
	"backspace": keyDelete,
	"delete":    keyForwardDelete,
	"enter":     keyReturn,
	"tab":       keyTab,
	"esc":       keyEscape,
	"escape":    keyEscape,
	"up":        keyUp,
	"down":      keyDown,
	"right":     keyRight,
	"left":      keyLeft,
	"home":      keyHome,
	"end":       keyEnd,
	"pageup":    keyPageUp,
	"pagedown":  keyPageDown,

	"cmd":         keyCommand,
	"command":     keyCommand,
	"lcmd":        keyCommand,
	"rcmd":        keyRightCommand,
	"alt":         keyOption,
	"lalt":        keyOption,
	"ralt":        keyRightOption,
	"ctrl":        keyControl,
	"control":     keyControl,
	"lctrl":       keyControl,
	"rctrl":       keyRightControl,
	"shift":       keyShift,
	"lshift":      keyShift,
	"rshift":      keyRightShift,
	"right_shift": keyRightShift,
	"capslock":    keyCapsLock,
	"space":       keySpace,
	"print":       keyF13,
	"printscreen": keyF13,
	"insert":      keyHelp,

	"num0": keyKeypad0, "numpad_0": keyKeypad0,
	"num1": keyKeypad1, "numpad_1": keyKeypad1,
	"num2": keyKeypad2, "numpad_2": keyKeypad2,
	"num3": keyKeypad3, "numpad_3": keyKeypad3,
	"num4": keyKeypad4, "numpad_4": keyKeypad4,
	"num5": keyKeypad5, "numpad_5": keyKeypad5,
	"num6": keyKeypad6, "numpad_6": keyKeypad6,
	"num7": keyKeypad7, "numpad_7": keyKeypad7,
	"num8": keyKeypad8, "numpad_8": keyKeypad8,
	"num9": keyKeypad9, "numpad_9": keyKeypad9,
	"num_lock": keyKeypadClear, "numpad_lock": keyKeypadClear,
	"num.": keyKeypadDecimal, "num+": keyKeypadPlus,
	"num-": keyKeypadMinus, "num*": keyKeypadMultiply,
	"num/": keyKeypadDivide, "num_clear": keyKeypadClear,
	"num_enter": keyKeypadEnter, "num_equal": keyKeypadEquals,
}

var functionKeyCodes = [...]uint16{
	keyF1, keyF2, keyF3, keyF4, keyF5,
	keyF6, keyF7, keyF8, keyF9, keyF10,
	keyF11, keyF12, keyF13, keyF14, keyF15,
	keyF16, keyF17, keyF18, keyF19, keyF20,
}

var asciiKeyCodes = map[byte]uint16{
	'a': keyA, 'b': keyB, 'c': keyC, 'd': keyD, 'e': keyE,
	'f': keyF, 'g': keyG, 'h': keyH, 'i': keyI, 'j': keyJ,
	'k': keyK, 'l': keyL, 'm': keyM, 'n': keyN, 'o': keyO,
	'p': keyP, 'q': keyQ, 'r': keyR, 's': keyS, 't': keyT,
	'u': keyU, 'v': keyV, 'w': keyW, 'x': keyX, 'y': keyY,
	'z': keyZ,
	'0': key0, '1': key1, '2': key2, '3': key3, '4': key4,
	'5': key5, '6': key6, '7': key7, '8': key8, '9': key9,
	'!': key1, '@': key2, '#': key3, '$': key4, '%': key5,
	'^': key6, '&': key7, '*': key8, '(': key9, ')': key0,
	'`': keyGrave, '~': keyGrave,
	'-': keyMinus, '_': keyMinus,
	'=': keyEqual, '+': keyEqual,
	'[': keyLeftBracket, '{': keyLeftBracket,
	']': keyRightBracket, '}': keyRightBracket,
	'\\': keyBackslash, '|': keyBackslash,
	';': keySemicolon, ':': keySemicolon,
	'\'': keyQuote, '"': keyQuote,
	',': keyComma, '<': keyComma,
	'.': keyPeriod, '>': keyPeriod,
	'/': keySlash, '?': keySlash,
	' ': keySpace,
}

var explicitlyUnsupportedKeys = map[string]struct{}{
	"f21": {}, "f22": {}, "f23": {}, "f24": {},
	"menu": {}, "scroll_lock": {}, "pause_break": {},
	"audio_mute": {}, "audio_vol_down": {}, "audio_vol_up": {},
	"audio_play": {}, "audio_stop": {}, "audio_pause": {},
	"audio_prev": {}, "audio_next": {}, "audio_rewind": {},
	"audio_forward": {}, "audio_repeat": {}, "audio_random": {},
	"lights_mon_up": {}, "lights_mon_down": {},
	"lights_kbd_toggle": {}, "lights_kbd_up": {}, "lights_kbd_down": {},
}

// KeyboardReady checks the non-prompting Accessibility preflight and Quartz access.
func (backend *Backend) KeyboardReady() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	_, err := backend.keyboardReadyLocked()
	return err
}

func (backend *Backend) keyboardReadyLocked() (inputSystem, error) {
	system, err := backend.systemLocked()
	if err != nil {
		return nil, err
	}
	if err := system.KeyboardReady(); err != nil {
		return nil, err
	}
	return system, nil
}

func validatePID(pid int) (int32, error) {
	if pid < 0 || int64(pid) > math.MaxInt32 {
		return 0, fmt.Errorf("Pure-Go macOS process ID %d is outside [0,%d]", pid, math.MaxInt32)
	}
	return int32(pid), nil
}

func resolveKey(name string) (ownedKey, error) {
	switch name {
	case "\n", "\r":
		return ownedKey{code: keyReturn, name: name}, nil
	case "\t":
		return ownedKey{code: keyTab, name: name}, nil
	case "\b":
		return ownedKey{code: keyDelete, name: name}, nil
	}
	lower := strings.ToLower(name)
	if code, ok := namedKeyCodes[lower]; ok {
		return ownedKey{code: code, flag: modifierFlag(code), name: lower}, nil
	}
	if strings.HasPrefix(lower, "f") {
		number, err := strconv.Atoi(strings.TrimPrefix(lower, "f"))
		if err == nil && number >= 1 && number <= len(functionKeyCodes) {
			return ownedKey{code: functionKeyCodes[number-1], name: lower}, nil
		}
	}
	if _, unsupported := explicitlyUnsupportedKeys[lower]; unsupported {
		return ownedKey{}, fmt.Errorf("%w: macOS Quartz has no safe key event for %q", ErrUnsupported, name)
	}
	if len(name) == 1 {
		character := name[0]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		if code, ok := asciiKeyCodes[character]; ok {
			return ownedKey{code: code, name: name}, nil
		}
	}
	if utf8.ValidString(name) && utf8.RuneCountInString(name) == 1 {
		value, _ := utf8.DecodeRuneInString(name)
		return ownedKey{}, fmt.Errorf(
			"%w: %U has no stable physical Quartz keycode; use TypeStrE for text",
			ErrUnsupported, value,
		)
	}
	return ownedKey{}, fmt.Errorf("%w: invalid macOS key %q", ErrUnsupported, name)
}

func modifierFlag(code uint16) uint64 {
	switch code {
	case keyShift, keyRightShift:
		return eventFlagMaskShift
	case keyControl, keyRightControl:
		return eventFlagMaskControl
	case keyOption, keyRightOption:
		return eventFlagMaskAlternate
	case keyCommand, keyRightCommand:
		return eventFlagMaskCommand
	default:
		return 0
	}
}

func intrinsicKeyFlags(code uint16) uint64 {
	switch code {
	case keyKeypadDecimal, keyKeypadMultiply, keyKeypadPlus,
		keyKeypadClear, keyKeypadDivide, keyKeypadEnter, keyKeypadMinus,
		keyKeypadEquals, keyKeypad0, keyKeypad1, keyKeypad2, keyKeypad3,
		keyKeypad4, keyKeypad5, keyKeypad6, keyKeypad7, keyKeypad8,
		keyKeypad9:
		return eventFlagMaskNumericPad
	case keyHelp:
		return eventFlagMaskHelp
	default:
		return 0
	}
}

func hasMomentaryKeyState(code uint16) bool {
	// Quartz reports Caps Lock's toggle state through CGEventSourceKeyState,
	// which is not evidence that another source currently holds the key down.
	return code != keyCapsLock
}

func modifierCodes(flag uint64) []uint16 {
	switch flag {
	case eventFlagMaskShift:
		return []uint16{keyShift, keyRightShift}
	case eventFlagMaskControl:
		return []uint16{keyControl, keyRightControl}
	case eventFlagMaskAlternate:
		return []uint16{keyOption, keyRightOption}
	case eventFlagMaskCommand:
		return []uint16{keyCommand, keyRightCommand}
	default:
		return nil
	}
}

func (backend *Backend) flagsAfterReleaseLocked(
	system inputSystem,
	key ownedKey,
	flags uint64,
	released map[uint16]struct{},
) (uint64, error) {
	if key.flag == 0 {
		return flags, nil
	}
	for _, code := range modifierCodes(key.flag) {
		if code == key.code {
			continue
		}
		if _, owned := backend.ownedKeys[code]; owned {
			return flags, nil
		}
		if _, releasedHere := released[code]; releasedHere {
			continue
		}
		down, err := system.KeyDown(code)
		if err != nil {
			return flags, err
		}
		if down {
			return flags, nil
		}
	}
	return flags &^ key.flag, nil
}

func (backend *Backend) ownedModifierFlagsLocked() uint64 {
	var flags uint64
	for _, key := range backend.ownedKeys {
		flags |= key.flag
	}
	return flags
}

func resolveModifiers(names []string) ([]ownedKey, error) {
	result := make([]ownedKey, 0, len(names))
	seen := make(map[uint16]struct{}, len(names))
	for _, name := range names {
		if strings.EqualFold(name, "none") {
			continue
		}
		key, err := resolveKey(name)
		if err != nil {
			return nil, err
		}
		if key.flag == 0 {
			return nil, fmt.Errorf("%w: %q is not a macOS modifier", ErrUnsupported, name)
		}
		if _, duplicate := seen[key.code]; duplicate {
			continue
		}
		seen[key.code] = struct{}{}
		result = append(result, key)
	}
	return result, nil
}

func removeOwnedKey(order []uint16, code uint16) []uint16 {
	for index := len(order) - 1; index >= 0; index-- {
		if order[index] == code {
			return append(order[:index], order[index+1:]...)
		}
	}
	return order
}

func (backend *Backend) pressKeyLocked(
	system inputSystem,
	key ownedKey,
	flags uint64,
) error {
	if err := system.PostKey(
		key.code, true, flags|intrinsicKeyFlags(key.code), key.pid,
	); err != nil {
		return err
	}
	backend.ownedKeys[key.code] = key
	backend.ownedKeyOrder = append(backend.ownedKeyOrder, key.code)
	return nil
}

func (backend *Backend) releaseKeyLocked(
	system inputSystem,
	key ownedKey,
	flags uint64,
) error {
	if err := system.PostKey(
		key.code, false, flags|intrinsicKeyFlags(key.code), key.pid,
	); err != nil {
		return err
	}
	delete(backend.ownedKeys, key.code)
	backend.ownedKeyOrder = removeOwnedKey(backend.ownedKeyOrder, key.code)
	return nil
}

type keyStep struct {
	key  ownedKey
	down bool
}

func (backend *Backend) executeKeyStepsLocked(
	system inputSystem,
	steps []keyStep,
	flags uint64,
) error {
	initiallyOwned := make(map[uint16]struct{}, len(backend.ownedKeys))
	for code := range backend.ownedKeys {
		initiallyOwned[code] = struct{}{}
	}
	var transactionOrder []uint16
	released := make(map[uint16]struct{})
	for _, step := range steps {
		nextFlags := flags
		if step.down && step.key.flag != 0 {
			nextFlags |= step.key.flag
		}
		if !step.down && step.key.flag != 0 {
			var flagsErr error
			nextFlags, flagsErr = backend.flagsAfterReleaseLocked(
				system, step.key, flags, released,
			)
			if flagsErr != nil {
				rollbackErr := backend.rollbackKeysLocked(
					system, transactionOrder, initiallyOwned, flags, released,
				)
				return errors.Join(flagsErr, rollbackErr)
			}
		}
		var err error
		if step.down {
			err = backend.pressKeyLocked(system, step.key, nextFlags)
			if err == nil {
				transactionOrder = append(transactionOrder, step.key.code)
			}
		} else {
			err = backend.releaseKeyLocked(system, step.key, nextFlags)
			if err == nil {
				released[step.key.code] = struct{}{}
			}
		}
		if err == nil {
			flags = nextFlags
			continue
		}
		rollbackErr := backend.rollbackKeysLocked(
			system, transactionOrder, initiallyOwned, flags, released,
		)
		return errors.Join(err, rollbackErr)
	}
	return nil
}

func (backend *Backend) rollbackKeysLocked(
	system inputSystem,
	order []uint16,
	initiallyOwned map[uint16]struct{},
	flags uint64,
	released map[uint16]struct{},
) error {
	var rollbackErr error
	for index := len(order) - 1; index >= 0; index-- {
		code := order[index]
		if _, existed := initiallyOwned[code]; existed {
			continue
		}
		key, owned := backend.ownedKeys[code]
		if !owned {
			continue
		}
		nextFlags, flagsErr := backend.flagsAfterReleaseLocked(
			system, key, flags, released,
		)
		if flagsErr != nil {
			rollbackErr = errors.Join(rollbackErr, flagsErr)
			continue
		}
		releaseErr := backend.releaseKeyLocked(system, key, nextFlags)
		rollbackErr = errors.Join(rollbackErr, releaseErr)
		if releaseErr == nil {
			flags = nextFlags
			released[key.code] = struct{}{}
		}
	}
	return rollbackErr
}

func (backend *Backend) temporaryModifiersLocked(
	system inputSystem,
	modifiers []ownedKey,
	main ownedKey,
	pid int32,
) ([]ownedKey, error) {
	result := make([]ownedKey, 0, len(modifiers))
	for _, modifier := range modifiers {
		if modifier.code == main.code {
			continue
		}
		modifier.pid = pid
		if owned, ok := backend.ownedKeys[modifier.code]; ok {
			if owned.pid != pid {
				return nil, fmt.Errorf(
					"%w: modifier %q is held for process %d, not %d",
					ErrOwnership, modifier.name, owned.pid, pid,
				)
			}
			continue
		}
		down, err := system.KeyDown(modifier.code)
		if err != nil {
			return nil, err
		}
		if down {
			continue
		}
		result = append(result, modifier)
	}
	return result, nil
}

func modifierSteps(modifiers []ownedKey, down bool) []keyStep {
	steps := make([]keyStep, 0, len(modifiers))
	if down {
		for _, modifier := range modifiers {
			steps = append(steps, keyStep{key: modifier, down: true})
		}
		return steps
	}
	for index := len(modifiers) - 1; index >= 0; index-- {
		steps = append(steps, keyStep{key: modifiers[index], down: false})
	}
	return steps
}

// Key injects one physical Quartz key transaction.
func (backend *Backend) Key(event KeyEvent) error {
	pid, err := validatePID(event.PID)
	if err != nil {
		return err
	}
	main, err := resolveKey(event.Key)
	if err != nil {
		return err
	}
	modifiers, err := resolveModifiers(event.Modifiers)
	if err != nil {
		return err
	}
	main.pid = pid

	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.keyboardReadyLocked()
	if err != nil {
		return err
	}
	flags, err := system.ModifierFlags()
	if err != nil {
		return err
	}
	flags |= backend.ownedModifierFlagsLocked()
	temporary, err := backend.temporaryModifiersLocked(system, modifiers, main, pid)
	if err != nil {
		return err
	}

	if event.Tap || event.Down {
		if _, owned := backend.ownedKeys[main.code]; owned {
			return fmt.Errorf("%w: key %q is already held by RobotGo", ErrOwnership, event.Key)
		}
		if hasMomentaryKeyState(main.code) {
			down, stateErr := system.KeyDown(main.code)
			if stateErr != nil {
				return stateErr
			}
			if down {
				return fmt.Errorf("%w: key %q is already down", ErrOwnership, event.Key)
			}
		}
	}

	steps := modifierSteps(temporary, true)
	switch {
	case event.Tap:
		steps = append(steps, keyStep{key: main, down: true}, keyStep{key: main})
	case event.Down:
		steps = append(steps, keyStep{key: main, down: true})
	default:
		owned, ok := backend.ownedKeys[main.code]
		if !ok {
			return fmt.Errorf("%w: key %q was not pressed by this backend", ErrOwnership, event.Key)
		}
		if owned.pid != pid {
			return fmt.Errorf(
				"%w: key %q is held for process %d, not %d",
				ErrOwnership, event.Key, owned.pid, pid,
			)
		}
		steps = append(steps, keyStep{key: owned})
	}
	steps = append(steps, modifierSteps(temporary, false)...)
	return backend.executeKeyStepsLocked(system, steps, flags)
}

// Text injects exact UTF-16 strings through Quartz Unicode keyboard events.
func (backend *Backend) Text(event TextEvent) error {
	pid, err := validatePID(event.PID)
	if err != nil {
		return err
	}
	if event.Delay < 0 {
		return errors.New("Pure-Go macOS text delay must be non-negative")
	}
	if !utf8.ValidString(event.Text) {
		return errors.New("Pure-Go macOS text is not valid UTF-8")
	}
	if strings.IndexByte(event.Text, 0) >= 0 {
		return errors.New("Pure-Go macOS text cannot contain NUL")
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.keyboardReadyLocked()
	if err != nil {
		return err
	}
	flags, err := system.ModifierFlags()
	if err != nil {
		return err
	}
	if flags&keyboardShortcutFlags != 0 {
		return fmt.Errorf(
			"%w: release active Shift, Control, Option, and Command keys before exact text input",
			ErrOwnership,
		)
	}
	for _, key := range backend.ownedKeys {
		if key.flag != 0 {
			return fmt.Errorf(
				"%w: release RobotGo-owned modifier %q before exact text input",
				ErrOwnership, key.name,
			)
		}
	}

	runes := []rune(event.Text)
	for index, value := range runes {
		units := utf16.Encode([]rune{value})
		if err := system.PostUnicode(units, true, 0, pid); err != nil {
			return fmt.Errorf("type %U key down: %w", value, err)
		}
		if err := system.PostUnicode(units, false, 0, pid); err != nil {
			cleanupErr := system.PostUnicode(units, false, 0, pid)
			return fmt.Errorf("type %U key up: %w", value, errors.Join(err, cleanupErr))
		}
		if event.Delay > 0 && index+1 < len(runes) {
			backend.sleep(event.Delay)
		}
	}
	return nil
}
