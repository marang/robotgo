//go:build linux && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/marang/robotgo/internal/x11input"
)

const x11KeysymF1 = 0xffbe

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

type x11InputBackend struct {
	core *x11input.Backend
}

var linuxX11Input = &x11InputBackend{core: x11input.New(x11input.Config{
	ResolveDisplay: resolvePureGoX11Display,
	Dialer:         x11input.NewGuardianDialer(x11input.GuardianOptions{}),
	KeyHoldDelay:   5 * time.Millisecond,
})}

func resolvePureGoX11Display() (string, error) {
	if DetectDisplayServer() != DisplayServerX11 {
		return "", fmt.Errorf("%w: Pure-Go X11 input requires DISPLAY without an active Wayland session", ErrNotSupported)
	}
	if pureGoX11EnvironmentConflict() {
		return "", fmt.Errorf("%w: %s selects Wayland while DISPLAY selects X11", ErrNotSupported, envXDGSessionType)
	}
	display := os.Getenv(envDisplay)
	if display == "" {
		return "", fmt.Errorf("%w: %s is unset", ErrNotSupported, envDisplay)
	}
	return display, nil
}

func platformPureGoInputBackend() pureGoInputBackend {
	if DetectDisplayServer() != DisplayServerX11 {
		return nil
	}
	return linuxX11Input
}

func closePureGoPlatformInput() error { return linuxX11Input.Close() }

func (*x11InputBackend) Name() string { return featureBackendPureGoX11 }

func translatePureGoX11Error(err error) error {
	if err == nil || !errors.Is(err, x11input.ErrUnsupported) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrNotSupported, err)
}

func (backend *x11InputBackend) KeyboardReady() error {
	return translatePureGoX11Error(backend.core.KeyboardReady())
}

func (backend *x11InputBackend) MouseReady() error {
	return translatePureGoX11Error(backend.core.MouseReady())
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

func x11LiteralKey(key string) bool {
	if !utf8.ValidString(key) {
		return false
	}
	if _, named := portalNamedKeysyms[strings.ToLower(key)]; named {
		return false
	}
	return utf8.RuneCountInString(key) == 1
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

func x11ModifierKeysyms(modifiers []string) ([]uint32, error) {
	result := make([]uint32, 0, len(modifiers))
	for _, modifier := range modifiers {
		if strings.EqualFold(modifier, "none") {
			continue
		}
		keysym, err := x11KeysymForKey(modifier)
		if err != nil {
			return nil, err
		}
		result = append(result, keysym)
	}
	return result, nil
}

func (backend *x11InputBackend) Key(event pureGoKeyEvent) error {
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
	modifiers, err := x11ModifierKeysyms(event.Modifiers)
	if err != nil {
		return err
	}
	return translatePureGoX11Error(backend.core.Key(x11input.KeyEvent{
		Keysym:       keysym,
		Modifiers:    modifiers,
		Down:         event.Down,
		Tap:          event.Tap,
		AllowScratch: event.Tap,
		ForceScratch: event.Tap && x11LiteralKey(event.Key),
	}))
}

func (backend *x11InputBackend) Text(event pureGoTextEvent) error {
	if event.PID != 0 {
		return fmt.Errorf("%w: Pure-Go X11 input cannot target a process", ErrNotSupported)
	}
	if event.Delay < 0 {
		return errors.New("robotgo: text delay must be non-negative")
	}
	if !utf8.ValidString(event.Text) {
		return errors.New("robotgo: text is not valid UTF-8")
	}
	keysyms := make([]uint32, 0, utf8.RuneCountInString(event.Text))
	for _, value := range event.Text {
		keysym, err := portalKeysymForRune(value)
		if err != nil {
			return err
		}
		keysyms = append(keysyms, uint32(keysym))
	}
	return translatePureGoX11Error(backend.core.Text(x11input.TextEvent{
		Keysyms: keysyms,
		Delay:   time.Duration(event.Delay) * time.Millisecond,
	}))
}

func x11MouseButton(name string) (x11input.Button, error) {
	switch name {
	case "", "left":
		return x11input.ButtonLeft, nil
	case "center", "middle":
		return x11input.ButtonMiddle, nil
	case "right":
		return x11input.ButtonRight, nil
	case "wheelUp":
		return x11input.ButtonWheelUp, nil
	case "wheelDown":
		return x11input.ButtonWheelDown, nil
	case "wheelLeft":
		return x11input.ButtonWheelLeft, nil
	case "wheelRight":
		return x11input.ButtonWheelRight, nil
	default:
		return 0, fmt.Errorf("robotgo: invalid X11 pointer button %q", name)
	}
}

func (backend *x11InputBackend) MoveAbsolute(x, y int, displayID []int) error {
	return translatePureGoX11Error(backend.core.MoveAbsolute(x, y, displayID))
}

func (backend *x11InputBackend) MoveRelative(x, y int) error {
	return translatePureGoX11Error(backend.core.MoveRelative(x, y))
}

func (backend *x11InputBackend) MoveSmooth(x, y int, relative bool, lowDelay, highDelay float64) error {
	return translatePureGoX11Error(backend.core.MoveSmooth(x, y, relative, lowDelay, highDelay))
}

func (backend *x11InputBackend) DragSmooth(x, y int, lowDelay, highDelay float64) error {
	return translatePureGoX11Error(backend.core.DragSmooth(x, y, lowDelay, highDelay))
}

func (backend *x11InputBackend) Location() (int, int, error) {
	x, y, err := backend.core.Location()
	return x, y, translatePureGoX11Error(err)
}

func (backend *x11InputBackend) Click(name string, double bool) error {
	button, err := x11MouseButton(name)
	if err != nil {
		return err
	}
	return translatePureGoX11Error(backend.core.Click(button, double))
}

func (backend *x11InputBackend) Toggle(name string, down bool) error {
	button, err := x11MouseButton(name)
	if err != nil {
		return err
	}
	return translatePureGoX11Error(backend.core.Toggle(button, down))
}

func (backend *x11InputBackend) Scroll(x, y int) error {
	return translatePureGoX11Error(backend.core.Scroll(x, y))
}

func (backend *x11InputBackend) Close() error {
	return translatePureGoX11Error(backend.core.Close())
}
