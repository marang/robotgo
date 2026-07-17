//go:build windows

// Package windowsinput provides RobotGo's Pure-Go Windows input backend.
package windowsinput

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	doubleClickGap         = 200 * time.Millisecond
	dragStartDelay         = 50 * time.Millisecond
	maximumSmoothDelay     = 10_000
	maximumScrollMagnitude = math.MaxInt32 / wheelDelta
)

var (
	// ErrUnsupported reports an operation that Win32 cannot safely provide.
	ErrUnsupported = errors.New("Pure-Go Windows input operation is unsupported")
	// ErrOwnership reports an attempt to release or overwrite foreign input.
	ErrOwnership = errors.New("Pure-Go Windows input state is not owned by RobotGo")

	namedVirtualKeys = map[string]uint16{
		"backspace": vkBack, "delete": vkDelete,
		"enter": vkReturn, "tab": vkTab,
		"esc": vkEscape, "escape": vkEscape,
		"up": vkUp, "down": vkDown, "right": vkRight, "left": vkLeft,
		"home": vkHome, "end": vkEnd, "pageup": vkPrior, "pagedown": vkNext,
		"cmd": vkLWin, "command": vkLWin, "lcmd": vkLWin, "rcmd": vkRWin,
		"alt": vkMenu, "lalt": vkLMenu, "ralt": vkRMenu,
		"ctrl": vkControl, "control": vkControl, "lctrl": vkLControl, "rctrl": vkRControl,
		"shift": vkShift, "lshift": vkLShift, "rshift": vkRShift, "right_shift": vkRShift,
		"capslock": vkCapital, "space": vkSpace,
		"print": vkSnapshot, "printscreen": vkSnapshot,
		"insert": vkInsert, "menu": vkApps,
		"num0": vkNumpad0, "numpad_0": vkNumpad0,
		"num1": vkNumpad1, "numpad_1": vkNumpad1,
		"num2": vkNumpad2, "numpad_2": vkNumpad2,
		"num3": vkNumpad3, "numpad_3": vkNumpad3,
		"num4": vkNumpad4, "numpad_4": vkNumpad4,
		"num5": vkNumpad5, "numpad_5": vkNumpad5,
		"num6": vkNumpad6, "numpad_6": vkNumpad6,
		"num7": vkNumpad7, "numpad_7": vkNumpad7,
		"num8": vkNumpad8, "numpad_8": vkNumpad8,
		"num9": vkNumpad9, "numpad_9": vkNumpad9,
		"num_lock": vkNumLock, "numpad_lock": vkNumLock,
		"num.": vkDecimal, "num+": vkAdd, "num-": vkSubtract,
		"num*": vkMultiply, "num/": vkDivide, "num_enter": vkReturn,
		"num_equal":   vkOEMPlus,
		"scroll_lock": vkScroll, "pause_break": vkPause,
		"audio_mute": vkVolumeMute, "audio_vol_down": vkVolumeDown,
		"audio_vol_up": vkVolumeUp, "audio_play": vkMediaPlayPause,
		"audio_pause": vkMediaPlayPause, "audio_stop": vkMediaStop,
		"audio_prev": vkMediaPrevTrack, "audio_next": vkMediaNextTrack,
	}
)

// KeyEvent is one normalized keyboard operation.
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

// Backend serializes Win32 input transactions and tracks persistent holds.
type Backend struct {
	mu sync.Mutex

	system inputSystem
	sleep  func(time.Duration)

	ownedKeys        map[uint16]struct{}
	ownedKeyExtended map[uint16]bool
	ownedKeyOrder    []uint16
	ownedButtons     map[uint32]struct{}
	ownedButtonOrder []uint32
	ownedUnicode     []uint16
}

// New creates a backend backed by user32.dll.
func New() *Backend {
	return newBackend(newWin32System(), time.Sleep)
}

func newBackend(system inputSystem, sleep func(time.Duration)) *Backend {
	if sleep == nil {
		sleep = time.Sleep
	}
	return &Backend{
		system:           system,
		sleep:            sleep,
		ownedKeys:        make(map[uint16]struct{}),
		ownedKeyExtended: make(map[uint16]bool),
		ownedButtons:     make(map[uint32]struct{}),
	}
}

// KeyboardReady verifies that the required Win32 keyboard entry points exist.
func (backend *Backend) KeyboardReady() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.system.KeyboardReady()
}

// MouseReady verifies the pointer entry points and access to the input desktop.
func (backend *Backend) MouseReady() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.mouseReadyLocked()
}

func (backend *Backend) mouseReadyLocked() error {
	if err := backend.system.MouseReady(); err != nil {
		return err
	}
	_, _, err := backend.system.CursorPosition()
	return err
}

func namedVirtualKey(key string) (uint16, bool) {
	value, ok := namedVirtualKeys[strings.ToLower(key)]
	return value, ok
}

func functionVirtualKey(key string) (uint16, bool) {
	lower := strings.ToLower(key)
	if !strings.HasPrefix(lower, "f") {
		return 0, false
	}
	number, err := strconv.Atoi(strings.TrimPrefix(lower, "f"))
	if err != nil || number < 1 || number > 24 {
		return 0, false
	}
	return vkF1 + uint16(number-1), true
}

func modifierVirtualKey(name string) (uint16, error) {
	if strings.EqualFold(name, "none") {
		return 0, nil
	}
	key, ok := namedVirtualKey(name)
	if !ok {
		return 0, fmt.Errorf("%w: unknown modifier %q", ErrUnsupported, name)
	}
	switch key {
	case vkMenu, vkLMenu, vkRMenu,
		vkControl, vkLControl, vkRControl,
		vkShift, vkLShift, vkRShift,
		vkLWin, vkRWin:
		return key, nil
	default:
		return 0, fmt.Errorf("%w: %q is not a modifier", ErrUnsupported, name)
	}
}

func (backend *Backend) resolveKeyLocked(key string) (uint16, []uint16, error) {
	switch key {
	case "\n", "\r":
		return vkReturn, nil, nil
	case "\t":
		return vkTab, nil, nil
	case "\b":
		return vkBack, nil, nil
	}
	if named, ok := namedVirtualKey(key); ok {
		return named, nil, nil
	}
	if function, ok := functionVirtualKey(key); ok {
		return function, nil, nil
	}
	if !utf8.ValidString(key) || utf8.RuneCountInString(key) != 1 {
		return 0, nil, fmt.Errorf("%w: invalid key %q", ErrUnsupported, key)
	}
	value, _ := utf8.DecodeRuneInString(key)
	if value > math.MaxUint16 {
		return 0, nil, fmt.Errorf("%w: %U has no single Windows virtual key; use text input", ErrUnsupported, value)
	}
	virtualKey, shiftState, ok, err := backend.system.VirtualKeyForRune(uint16(value))
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		return 0, nil, fmt.Errorf("%w: foreground Windows keyboard layout cannot map %U; use text input", ErrUnsupported, value)
	}
	if virtualKey == 0 {
		return 0, nil, fmt.Errorf("%w: foreground Windows keyboard layout returned no virtual key for %U", ErrUnsupported, value)
	}
	if shiftState&^uint8(shiftStateShift|shiftStateControl|shiftStateAlt) != 0 {
		return 0, nil, fmt.Errorf("%w: foreground Windows keyboard layout requires unsupported shift state %#x for %U", ErrUnsupported, shiftState, value)
	}
	modifiers := make([]uint16, 0, 3)
	if shiftState&shiftStateShift != 0 {
		modifiers = append(modifiers, vkShift)
	}
	if shiftState&shiftStateControl != 0 {
		modifiers = append(modifiers, vkControl)
	}
	if shiftState&shiftStateAlt != 0 {
		modifiers = append(modifiers, vkMenu)
	}
	return virtualKey, modifiers, nil
}

func appendUniqueKey(keys []uint16, key uint16) []uint16 {
	if key == 0 {
		return keys
	}
	for _, existing := range keys {
		if existing == key {
			return keys
		}
	}
	return append(keys, key)
}

func (backend *Backend) resolveModifiersLocked(names []string, implicit []uint16) ([]uint16, error) {
	result := append([]uint16(nil), implicit...)
	for _, name := range names {
		key, err := modifierVirtualKey(name)
		if err != nil {
			return nil, err
		}
		result = appendUniqueKey(result, key)
	}
	return result, nil
}

func (backend *Backend) temporaryModifierInputsLocked(modifiers []uint16, main uint16) (down, up []trackedInput) {
	for _, modifier := range modifiers {
		if modifier == main {
			continue
		}
		if _, owned := backend.ownedKeys[modifier]; owned || backend.system.KeyDown(modifier) {
			continue
		}
		down = append(down, trackedKeyInput(modifier, true))
		up = append([]trackedInput{trackedKeyInput(modifier, false)}, up...)
	}
	return down, up
}

// Key injects one foreground-layout-aware key transaction.
func (backend *Backend) Key(event KeyEvent) error {
	if event.PID != 0 {
		return fmt.Errorf("%w: Windows SendInput cannot target a process", ErrUnsupported)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.system.KeyboardReady(); err != nil {
		return err
	}
	main, implicit, err := backend.resolveKeyLocked(event.Key)
	if err != nil {
		return err
	}
	modifiers, err := backend.resolveModifiersLocked(event.Modifiers, implicit)
	if err != nil {
		return err
	}
	modifierDown, modifierUp := backend.temporaryModifierInputsLocked(modifiers, main)
	mainExtended := strings.EqualFold(event.Key, "num_enter")
	mainInput := func(down bool) trackedInput {
		return trackedKeyInputExtended(main, down, mainExtended)
	}
	if event.Tap {
		if _, owned := backend.ownedKeys[main]; owned || backend.system.KeyDown(main) {
			return fmt.Errorf("%w: key %q is already down", ErrOwnership, event.Key)
		}
		inputs := append(modifierDown, mainInput(true))
		inputs = append(inputs, mainInput(false))
		inputs = append(inputs, modifierUp...)
		return backend.sendTrackedLocked(inputs)
	}
	if event.Down {
		if _, owned := backend.ownedKeys[main]; owned || backend.system.KeyDown(main) {
			return fmt.Errorf("%w: key %q is already down", ErrOwnership, event.Key)
		}
		inputs := append(modifierDown, mainInput(true))
		inputs = append(inputs, modifierUp...)
		return backend.sendTrackedLocked(inputs)
	}
	ownedExtended, owned := backend.ownedKeyExtended[main]
	if !owned {
		return fmt.Errorf("%w: key %q was not pressed by this backend", ErrOwnership, event.Key)
	}
	if ownedExtended != mainExtended {
		ownedKind := "standard"
		if ownedExtended {
			ownedKind = "extended"
		}
		return fmt.Errorf(
			"%w: key %q does not match the owned %s key",
			ErrOwnership, event.Key, ownedKind,
		)
	}
	inputs := append(modifierDown, mainInput(false))
	inputs = append(inputs, modifierUp...)
	return backend.sendTrackedLocked(inputs)
}

// Text injects exact UTF-16 code units through KEYEVENTF_UNICODE.
func (backend *Backend) Text(event TextEvent) error {
	if event.PID != 0 {
		return fmt.Errorf("%w: Windows SendInput cannot target a process", ErrUnsupported)
	}
	if event.Delay < 0 {
		return errors.New("Pure-Go Windows text delay must be non-negative")
	}
	if !utf8.ValidString(event.Text) {
		return errors.New("Pure-Go Windows text is not valid UTF-8")
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.system.KeyboardReady(); err != nil {
		return err
	}
	for index, value := range event.Text {
		var units [2]uint16
		unitCount := 1
		if value <= math.MaxUint16 {
			units[0] = uint16(value)
		} else {
			high, low := utf16.EncodeRune(value)
			units[0], units[1] = uint16(high), uint16(low)
			unitCount = 2
		}
		inputs := make([]trackedInput, 0, unitCount*2)
		for _, unit := range units[:unitCount] {
			inputs = append(inputs,
				trackedUnicodeInput(unit, true),
				trackedUnicodeInput(unit, false),
			)
		}
		if err := backend.sendTrackedLocked(inputs); err != nil {
			return fmt.Errorf("type %U: %w", value, err)
		}
		if event.Delay > 0 && index+utf8.RuneLen(value) < len(event.Text) {
			backend.sleep(event.Delay)
		}
	}
	return nil
}

func validateCoordinate(value int) error {
	if int(int32(value)) != value {
		return fmt.Errorf("Pure-Go Windows coordinate %d is outside [%d,%d]", value, math.MinInt32, math.MaxInt32)
	}
	return nil
}

// MoveAbsolute moves the pointer in virtual-screen coordinates.
func (backend *Backend) MoveAbsolute(x, y int, _ []int) error {
	if err := validateCoordinate(x); err != nil {
		return err
	}
	if err := validateCoordinate(y); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.mouseReadyLocked(); err != nil {
		return err
	}
	return backend.system.SetCursorPosition(int32(x), int32(y))
}

// MoveRelative moves from the current pointer location without acceleration.
func (backend *Backend) MoveRelative(x, y int) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.system.MouseReady(); err != nil {
		return err
	}
	currentX, currentY, err := backend.system.CursorPosition()
	if err != nil {
		return err
	}
	targetX, targetY := int64(currentX)+int64(x), int64(currentY)+int64(y)
	if targetX < math.MinInt32 || targetX > math.MaxInt32 ||
		targetY < math.MinInt32 || targetY > math.MaxInt32 {
		return errors.New("Pure-Go Windows relative pointer target exceeds Win32 coordinates")
	}
	return backend.system.SetCursorPosition(int32(targetX), int32(targetY))
}

func validSmoothDelayRange(lowDelay, highDelay float64) bool {
	return !math.IsNaN(lowDelay) && !math.IsNaN(highDelay) &&
		!math.IsInf(lowDelay, 0) && !math.IsInf(highDelay, 0) &&
		lowDelay >= 0 && highDelay >= lowDelay && highDelay <= maximumSmoothDelay
}

type smoothMovePlan struct {
	startX, startY   int64
	targetX, targetY int64
	steps            int
	delay            time.Duration
}

func (backend *Backend) planSmoothMoveLocked(
	x, y int,
	relative bool,
	lowDelay, highDelay float64,
) (smoothMovePlan, error) {
	if !validSmoothDelayRange(lowDelay, highDelay) {
		return smoothMovePlan{}, fmt.Errorf(
			"Pure-Go Windows invalid smooth-move delay range %g..%g ms",
			lowDelay, highDelay,
		)
	}
	startX, startY, err := backend.system.CursorPosition()
	if err != nil {
		return smoothMovePlan{}, err
	}
	targetX, targetY := int64(x), int64(y)
	if relative {
		targetX += int64(startX)
		targetY += int64(startY)
	}
	if targetX < math.MinInt32 || targetX > math.MaxInt32 ||
		targetY < math.MinInt32 || targetY > math.MaxInt32 {
		return smoothMovePlan{}, errors.New("Pure-Go Windows smooth pointer target exceeds Win32 coordinates")
	}
	distance := math.Hypot(float64(targetX-int64(startX)), float64(targetY-int64(startY)))
	steps := int(math.Ceil(distance / 8))
	if steps < 1 {
		steps = 1
	}
	if steps > 240 {
		steps = 240
	}
	return smoothMovePlan{
		startX: int64(startX), startY: int64(startY),
		targetX: targetX, targetY: targetY,
		steps: steps,
		delay: time.Duration((lowDelay + highDelay) / 2 * float64(time.Millisecond)),
	}, nil
}

func (backend *Backend) executeSmoothMoveLocked(plan smoothMovePlan) error {
	lastX, lastY := plan.startX, plan.startY
	for step := 1; step <= plan.steps; step++ {
		progress := float64(step) / float64(plan.steps)
		if progress < 0.5 {
			progress = 4 * progress * progress * progress
		} else {
			inverse := -2*progress + 2
			progress = 1 - inverse*inverse*inverse/2
		}
		currentX := int64(math.Round(float64(plan.startX) + float64(plan.targetX-plan.startX)*progress))
		currentY := int64(math.Round(float64(plan.startY) + float64(plan.targetY-plan.startY)*progress))
		if currentX == lastX && currentY == lastY && step != plan.steps {
			continue
		}
		if err := backend.system.SetCursorPosition(int32(currentX), int32(currentY)); err != nil {
			return err
		}
		lastX, lastY = currentX, currentY
		if plan.delay > 0 && step != plan.steps {
			backend.sleep(plan.delay)
		}
	}
	return nil
}

func (backend *Backend) moveSmoothLocked(x, y int, relative bool, lowDelay, highDelay float64) error {
	plan, err := backend.planSmoothMoveLocked(x, y, relative, lowDelay, highDelay)
	if err != nil {
		return err
	}
	return backend.executeSmoothMoveLocked(plan)
}

// MoveSmooth performs a bounded eased movement.
func (backend *Backend) MoveSmooth(x, y int, relative bool, lowDelay, highDelay float64) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.system.MouseReady(); err != nil {
		return err
	}
	return backend.moveSmoothLocked(x, y, relative, lowDelay, highDelay)
}

func mouseButton(name string) (flags uint32, virtualKey uint16, err error) {
	switch name {
	case "", "left":
		return mouseEventLeftDown, vkLButton, nil
	case "center", "middle":
		return mouseEventMiddleDown, vkMButton, nil
	case "right":
		return mouseEventRightDown, vkRButton, nil
	case "wheelUp", "wheelDown", "wheelLeft", "wheelRight":
		return 0, 0, fmt.Errorf("%w: use Scroll for wheel input instead of button %q", ErrUnsupported, name)
	default:
		return 0, 0, fmt.Errorf("invalid Pure-Go Windows pointer button %q", name)
	}
}

func mouseButtonUpFlag(downFlag uint32) uint32 {
	switch downFlag {
	case mouseEventLeftDown:
		return mouseEventLeftUp
	case mouseEventMiddleDown:
		return mouseEventMiddleUp
	default:
		return mouseEventRightUp
	}
}

func (backend *Backend) buttonAvailableLocked(flag uint32, virtualKey uint16) error {
	if _, owned := backend.ownedButtons[flag]; owned || backend.system.KeyDown(virtualKey) {
		return fmt.Errorf("%w: pointer button is already down", ErrOwnership)
	}
	return nil
}

// Click injects one or two complete button pulses.
func (backend *Backend) Click(name string, double bool) error {
	flag, virtualKey, err := mouseButton(name)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.mouseReadyLocked(); err != nil {
		return err
	}
	count := 1
	if double {
		count = 2
	}
	for click := 0; click < count; click++ {
		if err := backend.buttonAvailableLocked(flag, virtualKey); err != nil {
			return err
		}
		if err := backend.sendTrackedLocked([]trackedInput{
			trackedButtonInput(flag, true),
			trackedButtonInput(flag, false),
		}); err != nil {
			return err
		}
		if click+1 < count {
			backend.sleep(doubleClickGap)
		}
	}
	return nil
}

// Toggle changes a persistent pointer-button state with ownership checks.
func (backend *Backend) Toggle(name string, down bool) error {
	flag, virtualKey, err := mouseButton(name)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.mouseReadyLocked(); err != nil {
		return err
	}
	if down {
		if err := backend.buttonAvailableLocked(flag, virtualKey); err != nil {
			return err
		}
	} else if _, owned := backend.ownedButtons[flag]; !owned {
		return fmt.Errorf("%w: pointer button was not pressed by this backend", ErrOwnership)
	}
	return backend.sendTrackedLocked([]trackedInput{trackedButtonInput(flag, down)})
}

// DragSmooth owns the left button for the complete drag transaction.
func (backend *Backend) DragSmooth(x, y int, lowDelay, highDelay float64) error {
	if !validSmoothDelayRange(lowDelay, highDelay) {
		return fmt.Errorf("Pure-Go Windows invalid smooth-move delay range %g..%g ms", lowDelay, highDelay)
	}
	if err := validateCoordinate(x); err != nil {
		return err
	}
	if err := validateCoordinate(y); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.system.MouseReady(); err != nil {
		return err
	}
	plan, err := backend.planSmoothMoveLocked(x, y, false, lowDelay, highDelay)
	if err != nil {
		return err
	}
	if err := backend.buttonAvailableLocked(mouseEventLeftDown, vkLButton); err != nil {
		return err
	}
	downErr := backend.sendTrackedLocked([]trackedInput{trackedButtonInput(mouseEventLeftDown, true)})
	if downErr != nil {
		return downErr
	}
	backend.sleep(dragStartDelay)
	moveErr := backend.executeSmoothMoveLocked(plan)
	upErr := backend.sendTrackedLocked([]trackedInput{trackedButtonInput(mouseEventLeftDown, false)})
	return errors.Join(moveErr, upErr)
}

// Location returns the pointer location in virtual-screen coordinates.
func (backend *Backend) Location() (int, int, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.system.MouseReady(); err != nil {
		return 0, 0, err
	}
	x, y, err := backend.system.CursorPosition()
	return int(x), int(y), err
}

// Scroll injects horizontal and vertical wheel deltas.
func (backend *Backend) Scroll(x, y int) error {
	if x < -maximumScrollMagnitude || x > maximumScrollMagnitude ||
		y < -maximumScrollMagnitude || y > maximumScrollMagnitude {
		return fmt.Errorf("Pure-Go Windows scroll magnitude exceeds %d", maximumScrollMagnitude)
	}
	inputs := make([]trackedInput, 0, 2)
	if x != 0 {
		// RobotGo positive x means left; Win32 positive HWHEEL means right.
		inputs = append(inputs, trackedMouseInput(mouseEventHWheel, int32(-x*wheelDelta)))
	}
	if y != 0 {
		inputs = append(inputs, trackedMouseInput(mouseEventWheel, int32(y*wheelDelta)))
	}
	if len(inputs) == 0 {
		return nil
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.mouseReadyLocked(); err != nil {
		return err
	}
	return backend.sendTrackedLocked(inputs)
}

// Close releases every key, Unicode unit, and button still owned by RobotGo.
func (backend *Backend) Close() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	var closeErr error
	for index := len(backend.ownedUnicode) - 1; index >= 0; index-- {
		closeErr = errors.Join(closeErr, backend.sendTrackedLocked(
			[]trackedInput{trackedUnicodeInput(backend.ownedUnicode[index], false)},
		))
	}
	for index := len(backend.ownedKeyOrder) - 1; index >= 0; index-- {
		key := backend.ownedKeyOrder[index]
		if _, owned := backend.ownedKeys[key]; owned {
			closeErr = errors.Join(closeErr, backend.sendTrackedLocked(
				[]trackedInput{trackedKeyInputExtended(key, false, backend.ownedKeyExtended[key])},
			))
		}
	}
	for index := len(backend.ownedButtonOrder) - 1; index >= 0; index-- {
		button := backend.ownedButtonOrder[index]
		if _, owned := backend.ownedButtons[button]; owned {
			closeErr = errors.Join(closeErr, backend.sendTrackedLocked(
				[]trackedInput{trackedButtonInput(button, false)},
			))
		}
	}
	return closeErr
}
