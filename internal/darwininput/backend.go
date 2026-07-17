// Package darwininput provides RobotGo's Pure-Go macOS pointer backend.
package darwininput

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	doubleClickGap           = 200 * time.Millisecond
	dragStartDelay           = 50 * time.Millisecond
	maximumSmoothDelay       = 10_000
	maximumCoordinate  int64 = 1 << 53
)

var (
	// ErrUnsupported reports an operation unavailable through the Quartz backend.
	ErrUnsupported = errors.New("Pure-Go macOS input operation is unsupported")
	// ErrPermission reports missing macOS Accessibility permission.
	ErrPermission = errors.New("Pure-Go macOS input requires Accessibility permission")
	// ErrOwnership reports an attempt to release or overwrite foreign input state.
	ErrOwnership = errors.New("Pure-Go macOS input state is not owned by RobotGo")
)

type point struct {
	X float64
	Y float64
}

type mouseButton struct {
	code      uint32
	downType  uint32
	upType    uint32
	dragType  uint32
	canonical string
}

type inputSystem interface {
	Ready() error
	CursorPosition() (point, error)
	ButtonDown(uint32) (bool, error)
	PostMouse(eventType uint32, location point, button uint32, clickState int64) error
	PostScroll(horizontal, vertical int32) error
	Close() error
}

type openSystem func() (inputSystem, error)

// Backend serializes Quartz pointer transactions and tracks persistent holds.
type Backend struct {
	mu sync.Mutex

	open   openSystem
	system inputSystem
	sleep  func(time.Duration)

	ownedButtons map[uint32]mouseButton
	ownedOrder   []uint32
	lastLocation point
	hasLocation  bool
}

// New creates a lazily initialized backend backed by macOS frameworks.
func New() *Backend {
	return newBackend(openNativeSystem, time.Sleep)
}

func newBackend(open openSystem, sleep func(time.Duration)) *Backend {
	if sleep == nil {
		sleep = time.Sleep
	}
	return &Backend{
		open:         open,
		sleep:        sleep,
		ownedButtons: make(map[uint32]mouseButton),
	}
}

func (backend *Backend) systemLocked() (inputSystem, error) {
	if backend.system != nil {
		return backend.system, nil
	}
	system, err := backend.open()
	if err != nil {
		return nil, err
	}
	backend.system = system
	return system, nil
}

func (backend *Backend) readyLocked() (inputSystem, error) {
	system, err := backend.systemLocked()
	if err != nil {
		return nil, err
	}
	if err := system.Ready(); err != nil {
		return nil, err
	}
	return system, nil
}

// MouseReady checks the non-prompting Accessibility preflight and Quartz access.
func (backend *Backend) MouseReady() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	_, err := backend.readyLocked()
	return err
}

func validateCoordinate(value int) error {
	if int64(value) < -maximumCoordinate || int64(value) > maximumCoordinate {
		return fmt.Errorf("Pure-Go macOS coordinate %d cannot be represented exactly by CoreGraphics", value)
	}
	return nil
}

func integralCoordinate(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) ||
		value < -float64(maximumCoordinate) || value > float64(maximumCoordinate) {
		return 0, fmt.Errorf("CoreGraphics returned invalid pointer coordinate %v", value)
	}
	return int64(value), nil
}

func locationLocked(system inputSystem) (int64, int64, error) {
	location, err := system.CursorPosition()
	if err != nil {
		return 0, 0, err
	}
	x, err := integralCoordinate(location.X)
	if err != nil {
		return 0, 0, err
	}
	y, err := integralCoordinate(location.Y)
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

// MoveAbsolute moves the pointer in the CoreGraphics global coordinate space.
func (backend *Backend) MoveAbsolute(x, y int) error {
	if err := validateCoordinate(x); err != nil {
		return err
	}
	if err := validateCoordinate(y); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.readyLocked()
	if err != nil {
		return err
	}
	target := point{X: float64(x), Y: float64(y)}
	if err := system.PostMouse(eventMouseMoved, target, buttonLeft, 0); err != nil {
		return err
	}
	backend.lastLocation, backend.hasLocation = target, true
	return nil
}

// MoveRelative moves from the current pointer location.
func (backend *Backend) MoveRelative(x, y int) error {
	if err := validateCoordinate(x); err != nil {
		return err
	}
	if err := validateCoordinate(y); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.readyLocked()
	if err != nil {
		return err
	}
	currentX, currentY, err := locationLocked(system)
	if err != nil {
		return err
	}
	targetX, targetY := currentX+int64(x), currentY+int64(y)
	if targetX < -maximumCoordinate || targetX > maximumCoordinate ||
		targetY < -maximumCoordinate || targetY > maximumCoordinate {
		return errors.New("Pure-Go macOS relative pointer target cannot be represented exactly by CoreGraphics")
	}
	target := point{X: float64(targetX), Y: float64(targetY)}
	if err := system.PostMouse(eventMouseMoved, target, buttonLeft, 0); err != nil {
		return err
	}
	backend.lastLocation, backend.hasLocation = target, true
	return nil
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
	system inputSystem,
	x, y int,
	relative bool,
	lowDelay, highDelay float64,
) (smoothMovePlan, error) {
	if !validSmoothDelayRange(lowDelay, highDelay) {
		return smoothMovePlan{}, fmt.Errorf(
			"Pure-Go macOS invalid smooth-move delay range %g..%g ms",
			lowDelay, highDelay,
		)
	}
	if err := validateCoordinate(x); err != nil {
		return smoothMovePlan{}, err
	}
	if err := validateCoordinate(y); err != nil {
		return smoothMovePlan{}, err
	}
	startX, startY, err := locationLocked(system)
	if err != nil {
		return smoothMovePlan{}, err
	}
	targetX, targetY := int64(x), int64(y)
	if relative {
		targetX += startX
		targetY += startY
	}
	if targetX < -maximumCoordinate || targetX > maximumCoordinate ||
		targetY < -maximumCoordinate || targetY > maximumCoordinate {
		return smoothMovePlan{}, errors.New("Pure-Go macOS smooth pointer target cannot be represented exactly by CoreGraphics")
	}
	distance := math.Hypot(float64(targetX-startX), float64(targetY-startY))
	steps := int(math.Ceil(distance / 8))
	if steps < 1 {
		steps = 1
	}
	if steps > 240 {
		steps = 240
	}
	return smoothMovePlan{
		startX: startX, startY: startY,
		targetX: targetX, targetY: targetY,
		steps: steps,
		delay: time.Duration((lowDelay + highDelay) / 2 * float64(time.Millisecond)),
	}, nil
}

func (backend *Backend) executeSmoothMoveLocked(
	system inputSystem,
	plan smoothMovePlan,
	eventType, button uint32,
) error {
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
		if err := system.PostMouse(eventType, point{X: float64(currentX), Y: float64(currentY)}, button, 0); err != nil {
			return err
		}
		backend.lastLocation = point{X: float64(currentX), Y: float64(currentY)}
		backend.hasLocation = true
		lastX, lastY = currentX, currentY
		if plan.delay > 0 && step != plan.steps {
			backend.sleep(plan.delay)
		}
	}
	return nil
}

// MoveSmooth performs a bounded eased movement.
func (backend *Backend) MoveSmooth(x, y int, relative bool, lowDelay, highDelay float64) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.readyLocked()
	if err != nil {
		return err
	}
	plan, err := backend.planSmoothMoveLocked(system, x, y, relative, lowDelay, highDelay)
	if err != nil {
		return err
	}
	return backend.executeSmoothMoveLocked(system, plan, eventMouseMoved, buttonLeft)
}

func resolveMouseButton(name string) (mouseButton, error) {
	switch name {
	case "", "left":
		return mouseButton{
			code: buttonLeft, downType: eventLeftMouseDown,
			upType: eventLeftMouseUp, dragType: eventLeftMouseDragged,
			canonical: "left",
		}, nil
	case "right":
		return mouseButton{
			code: buttonRight, downType: eventRightMouseDown,
			upType: eventRightMouseUp, dragType: eventRightMouseDragged,
			canonical: "right",
		}, nil
	case "center", "middle":
		return mouseButton{
			code: buttonCenter, downType: eventOtherMouseDown,
			upType: eventOtherMouseUp, dragType: eventOtherMouseDragged,
			canonical: "center",
		}, nil
	case "wheelUp", "wheelDown", "wheelLeft", "wheelRight":
		return mouseButton{}, fmt.Errorf("%w: use Scroll for wheel input instead of button %q", ErrUnsupported, name)
	default:
		return mouseButton{}, fmt.Errorf("invalid Pure-Go macOS pointer button %q", name)
	}
}

func (backend *Backend) buttonAvailableLocked(system inputSystem, button mouseButton) error {
	if _, owned := backend.ownedButtons[button.code]; owned {
		return fmt.Errorf("%w: pointer button %q is already held by RobotGo", ErrOwnership, button.canonical)
	}
	down, err := system.ButtonDown(button.code)
	if err != nil {
		return err
	}
	if down {
		return fmt.Errorf("%w: pointer button %q is already down", ErrOwnership, button.canonical)
	}
	return nil
}

func (backend *Backend) pressLocked(system inputSystem, button mouseButton, location point, clickState int64) error {
	if err := system.PostMouse(button.downType, location, button.code, clickState); err != nil {
		return err
	}
	backend.lastLocation, backend.hasLocation = location, true
	backend.ownedButtons[button.code] = button
	backend.ownedOrder = append(backend.ownedOrder, button.code)
	return nil
}

func (backend *Backend) releaseLocked(system inputSystem, button mouseButton, location point, clickState int64) error {
	if err := system.PostMouse(button.upType, location, button.code, clickState); err != nil {
		return err
	}
	backend.lastLocation, backend.hasLocation = location, true
	delete(backend.ownedButtons, button.code)
	for index := len(backend.ownedOrder) - 1; index >= 0; index-- {
		if backend.ownedOrder[index] != button.code {
			continue
		}
		backend.ownedOrder = append(backend.ownedOrder[:index], backend.ownedOrder[index+1:]...)
		break
	}
	return nil
}

// Click injects one or two complete button pulses.
func (backend *Backend) Click(name string, double bool) error {
	button, err := resolveMouseButton(name)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.readyLocked()
	if err != nil {
		return err
	}
	location, err := system.CursorPosition()
	if err != nil {
		return err
	}
	count := 1
	if double {
		count = 2
	}
	for click := 1; click <= count; click++ {
		if err := backend.buttonAvailableLocked(system, button); err != nil {
			return err
		}
		clickState := int64(click)
		if err := backend.pressLocked(system, button, location, clickState); err != nil {
			return err
		}
		if err := backend.releaseLocked(system, button, location, clickState); err != nil {
			return err
		}
		if click != count {
			backend.sleep(doubleClickGap)
		}
	}
	return nil
}

// Toggle changes a persistent pointer-button state with ownership checks.
func (backend *Backend) Toggle(name string, down bool) error {
	button, err := resolveMouseButton(name)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.readyLocked()
	if err != nil {
		return err
	}
	location, err := system.CursorPosition()
	if err != nil {
		return err
	}
	if down {
		if err := backend.buttonAvailableLocked(system, button); err != nil {
			return err
		}
		return backend.pressLocked(system, button, location, 1)
	}
	owned, ok := backend.ownedButtons[button.code]
	if !ok {
		return fmt.Errorf("%w: pointer button %q was not pressed by this backend", ErrOwnership, button.canonical)
	}
	return backend.releaseLocked(system, owned, location, 1)
}

// DragSmooth owns the left button for the complete drag transaction.
func (backend *Backend) DragSmooth(x, y int, lowDelay, highDelay float64) error {
	button, _ := resolveMouseButton("left")
	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.readyLocked()
	if err != nil {
		return err
	}
	plan, err := backend.planSmoothMoveLocked(system, x, y, false, lowDelay, highDelay)
	if err != nil {
		return err
	}
	if err := backend.buttonAvailableLocked(system, button); err != nil {
		return err
	}
	start := point{X: float64(plan.startX), Y: float64(plan.startY)}
	if err := backend.pressLocked(system, button, start, 1); err != nil {
		return err
	}
	backend.sleep(dragStartDelay)
	moveErr := backend.executeSmoothMoveLocked(system, plan, button.dragType, button.code)
	releaseLocation := point{X: float64(plan.targetX), Y: float64(plan.targetY)}
	if moveErr != nil {
		if current, locationErr := system.CursorPosition(); locationErr == nil {
			releaseLocation = current
		} else if backend.hasLocation {
			releaseLocation = backend.lastLocation
		}
	}
	releaseErr := backend.releaseLocked(system, button, releaseLocation, 1)
	return errors.Join(moveErr, releaseErr)
}

// Location returns the pointer location in global display coordinates.
func (backend *Backend) Location() (int, int, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.readyLocked()
	if err != nil {
		return 0, 0, err
	}
	x, y, err := locationLocked(system)
	return int(x), int(y), err
}

// Scroll injects horizontal and vertical pixel-based wheel deltas.
func (backend *Backend) Scroll(x, y int) error {
	if int(int32(x)) != x || int(int32(y)) != y {
		return errors.New("Pure-Go macOS scroll delta exceeds CoreGraphics int32 range")
	}
	if x == 0 && y == 0 {
		return nil
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	system, err := backend.readyLocked()
	if err != nil {
		return err
	}
	return system.PostScroll(int32(x), int32(y))
}

// Close releases RobotGo-owned buttons and unloads the native frameworks.
func (backend *Backend) Close() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.system == nil {
		return nil
	}
	system := backend.system
	var closeErr error
	if len(backend.ownedButtons) > 0 {
		location, locationErr := system.CursorPosition()
		if locationErr != nil && backend.hasLocation {
			location = backend.lastLocation
			locationErr = nil
		}
		if locationErr != nil {
			return locationErr
		}
		ownedOrder := append([]uint32(nil), backend.ownedOrder...)
		for index := len(ownedOrder) - 1; index >= 0; index-- {
			code := ownedOrder[index]
			button, owned := backend.ownedButtons[code]
			if !owned {
				continue
			}
			closeErr = errors.Join(closeErr, backend.releaseLocked(system, button, location, 1))
		}
	}
	if len(backend.ownedButtons) > 0 {
		return closeErr
	}
	closeErr = errors.Join(closeErr, system.Close())
	backend.system = nil
	backend.ownedButtons = make(map[uint32]mouseButton)
	backend.ownedOrder = backend.ownedOrder[:0]
	backend.hasLocation = false
	return closeErr
}
