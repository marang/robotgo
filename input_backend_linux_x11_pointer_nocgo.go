//go:build linux && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

const (
	x11DoubleClickGap     = 200 * time.Millisecond
	x11MaximumSmoothDelay = 10_000
	x11MaximumScrollSteps = 1_000
	x11RelativeMotion     = 1
)

const (
	x11ButtonLeft       byte = 1
	x11ButtonMiddle     byte = 2
	x11ButtonRight      byte = 3
	x11ButtonWheelUp    byte = 4
	x11ButtonWheelDown  byte = 5
	x11ButtonWheelLeft  byte = 6
	x11ButtonWheelRight byte = 7
)

func (backend *x11InputBackend) MouseReady() error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	if _, err := backend.pointerStateLocked(); err != nil {
		return backend.failLocked("query pointer", err)
	}
	return nil
}

func x11MouseButton(name string) (byte, error) {
	switch name {
	case "", "left":
		return x11ButtonLeft, nil
	case "center", "middle":
		return x11ButtonMiddle, nil
	case "right":
		return x11ButtonRight, nil
	case "wheelUp":
		return x11ButtonWheelUp, nil
	case "wheelDown":
		return x11ButtonWheelDown, nil
	case "wheelLeft":
		return x11ButtonWheelLeft, nil
	case "wheelRight":
		return x11ButtonWheelRight, nil
	default:
		return 0, fmt.Errorf("robotgo: invalid X11 pointer button %q", name)
	}
}

func x11Coordinate(value int) (int16, error) {
	if value < math.MinInt16 || value > math.MaxInt16 {
		return 0, fmt.Errorf("robotgo: X11 coordinate %d is outside [%d,%d]", value, math.MinInt16, math.MaxInt16)
	}
	return int16(value), nil
}

func x11Coordinates(x, y int) (int16, int16, error) {
	xCoordinate, err := x11Coordinate(x)
	if err != nil {
		return 0, 0, err
	}
	yCoordinate, err := x11Coordinate(y)
	if err != nil {
		return 0, 0, err
	}
	return xCoordinate, yCoordinate, nil
}

func (backend *x11InputBackend) sendButtonLocked(button byte, down bool) error {
	eventType := byte(xproto.ButtonRelease)
	if down {
		eventType = byte(xproto.ButtonPress)
	}
	if err := xtest.FakeInputChecked(backend.conn, eventType, button, 0, backend.root, 0, 0, 0).Check(); err != nil {
		return errors.Join(errX11Connection, err)
	}
	return nil
}

func x11ButtonMask(button byte) uint16 {
	switch button {
	case x11ButtonLeft:
		return xproto.ButtonMask1
	case x11ButtonMiddle:
		return xproto.ButtonMask2
	case x11ButtonRight:
		return xproto.ButtonMask3
	case x11ButtonWheelUp:
		return xproto.ButtonMask4
	case x11ButtonWheelDown:
		return xproto.ButtonMask5
	default:
		return 0
	}
}

func x11ButtonStateObservable(button byte) bool {
	return x11ButtonMask(button) != 0
}

func (backend *x11InputBackend) acquireButtonLocked(button byte, pointerMask uint16) error {
	if _, held := backend.buttons[button]; held {
		return fmt.Errorf("robotgo: X11 button %d is already held by this RobotGo backend", button)
	}
	if mask := x11ButtonMask(button); mask != 0 && pointerMask&mask != 0 {
		return fmt.Errorf("robotgo: X11 button %d is already held; refusing to alter foreign input state", button)
	}
	if err := backend.sendButtonLocked(button, true); err != nil {
		return err
	}
	if backend.buttons == nil {
		backend.buttons = make(map[byte]struct{})
	}
	backend.buttons[button] = struct{}{}
	backend.buttonOrder = append(backend.buttonOrder, button)
	return nil
}

func (backend *x11InputBackend) releaseOwnedButtonLocked(button byte) error {
	if _, held := backend.buttons[button]; !held {
		return fmt.Errorf("robotgo: X11 button %d is not held by this RobotGo backend", button)
	}
	if err := backend.sendButtonLocked(button, false); err != nil {
		return err
	}
	delete(backend.buttons, button)
	backend.buttonOrder = removeX11Button(backend.buttonOrder, button)
	return nil
}

func removeX11Button(buttons []byte, button byte) []byte {
	for index := len(buttons) - 1; index >= 0; index-- {
		if buttons[index] != button {
			continue
		}
		copy(buttons[index:], buttons[index+1:])
		buttons[len(buttons)-1] = 0
		return buttons[:len(buttons)-1]
	}
	return buttons
}

func (backend *x11InputBackend) moveLocked(x, y int) error {
	xCoordinate, yCoordinate, err := x11Coordinates(x, y)
	if err != nil {
		return err
	}
	if err := xtest.FakeInputChecked(
		backend.conn, byte(xproto.MotionNotify), 0, 0, backend.root,
		xCoordinate, yCoordinate, 0,
	).Check(); err != nil {
		return errors.Join(errX11Connection, err)
	}
	return nil
}

func (backend *x11InputBackend) moveRelativeLocked(x, y int) error {
	xDelta, yDelta, err := x11Coordinates(x, y)
	if err != nil {
		return err
	}
	if err := xtest.FakeInputChecked(
		backend.conn, byte(xproto.MotionNotify), x11RelativeMotion, 0,
		xproto.Window(0), xDelta, yDelta, 0,
	).Check(); err != nil {
		return errors.Join(errX11Connection, err)
	}
	return nil
}

func (backend *x11InputBackend) MoveAbsolute(x, y int, _ []int) error {
	if _, _, err := x11Coordinates(x, y); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	if err := backend.moveLocked(x, y); err != nil {
		return backend.failLocked("move pointer", err)
	}
	return nil
}

func (backend *x11InputBackend) pointerStateLocked() (*xproto.QueryPointerReply, error) {
	reply, err := xproto.QueryPointer(backend.conn, backend.root).Reply()
	if err != nil {
		return nil, errors.Join(errX11Connection, err)
	}
	if reply == nil {
		return nil, errors.Join(errX11Connection, errors.New("server returned no pointer reply"))
	}
	return reply, nil
}

func (backend *x11InputBackend) pointerLocationLocked() (int, int, error) {
	reply, err := backend.pointerStateLocked()
	if err != nil {
		return 0, 0, err
	}
	return int(reply.RootX), int(reply.RootY), nil
}

func (backend *x11InputBackend) MoveRelative(x, y int) error {
	if _, _, err := x11Coordinates(x, y); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	if err := backend.moveRelativeLocked(x, y); err != nil {
		return backend.failLocked("move pointer relatively", err)
	}
	return nil
}

type x11SmoothMovePlan struct {
	relative         bool
	startX, startY   int
	targetX, targetY int
	steps            int
	delay            time.Duration
}

func validateX11SmoothMove(x, y int, lowDelay, highDelay float64) error {
	if !validSmoothDelayRange(lowDelay, highDelay) || highDelay > x11MaximumSmoothDelay {
		return fmt.Errorf("robotgo: invalid X11 smooth-move delay range %g..%g ms", lowDelay, highDelay)
	}
	if _, _, err := x11Coordinates(x, y); err != nil {
		return err
	}
	return nil
}

func (backend *x11InputBackend) planSmoothMoveLocked(x, y int, relative bool, lowDelay, highDelay float64) (x11SmoothMovePlan, error) {
	plan := x11SmoothMovePlan{relative: relative, targetX: x, targetY: y}
	if !relative {
		startX, startY, err := backend.pointerLocationLocked()
		if err != nil {
			return x11SmoothMovePlan{}, err
		}
		plan.startX, plan.startY = startX, startY
	}
	distance := math.Hypot(float64(plan.targetX-plan.startX), float64(plan.targetY-plan.startY))
	steps := int(math.Ceil(distance / 8))
	if steps < 1 {
		steps = 1
	}
	if steps > 240 {
		steps = 240
	}
	plan.steps = steps
	plan.delay = time.Duration((lowDelay + highDelay) / 2 * float64(time.Millisecond))
	return plan, nil
}

func (backend *x11InputBackend) executeSmoothMoveLocked(plan x11SmoothMovePlan) error {
	lastX, lastY := plan.startX, plan.startY
	for step := 1; step <= plan.steps; step++ {
		progress := float64(step) / float64(plan.steps)
		if progress < 0.5 {
			progress = 4 * progress * progress * progress
		} else {
			inverse := -2*progress + 2
			progress = 1 - inverse*inverse*inverse/2
		}
		currentX := int(math.Round(float64(plan.startX) + float64(plan.targetX-plan.startX)*progress))
		currentY := int(math.Round(float64(plan.startY) + float64(plan.targetY-plan.startY)*progress))
		if currentX == lastX && currentY == lastY && step != plan.steps {
			continue
		}
		var err error
		if plan.relative {
			err = backend.moveRelativeLocked(currentX-lastX, currentY-lastY)
		} else {
			err = backend.moveLocked(currentX, currentY)
		}
		if err != nil {
			return err
		}
		lastX, lastY = currentX, currentY
		if plan.delay > 0 && step != plan.steps {
			time.Sleep(plan.delay)
		}
	}
	return nil
}

func (backend *x11InputBackend) MoveSmooth(x, y int, relative bool, lowDelay, highDelay float64) error {
	if err := validateX11SmoothMove(x, y, lowDelay, highDelay); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	plan, err := backend.planSmoothMoveLocked(x, y, relative, lowDelay, highDelay)
	if err != nil {
		return backend.failLocked("plan smooth pointer move", err)
	}
	if err := backend.executeSmoothMoveLocked(plan); err != nil {
		return backend.failLocked("move pointer smoothly", err)
	}
	return nil
}

func (backend *x11InputBackend) DragSmooth(x, y int, lowDelay, highDelay float64) error {
	if err := validateX11SmoothMove(x, y, lowDelay, highDelay); err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	plan, err := backend.planSmoothMoveLocked(x, y, false, lowDelay, highDelay)
	if err != nil {
		return backend.failLocked("plan smooth pointer drag", err)
	}
	downErr := backend.acquireButtonTransactionLocked(x11ButtonLeft)
	if downErr != nil && !errors.Is(downErr, errX11Connection) {
		return downErr
	}
	if downErr == nil {
		time.Sleep(50 * time.Millisecond)
	}
	moveErr := downErr
	if downErr == nil {
		moveErr = backend.executeSmoothMoveLocked(plan)
	}
	var upErr error
	if downErr == nil {
		upErr = backend.releaseButtonTransactionLocked(x11ButtonLeft)
	}
	eventErr := errors.Join(moveErr, upErr)
	if eventErr != nil {
		return backend.failLocked("drag pointer smoothly", eventErr)
	}
	return nil
}

func (backend *x11InputBackend) Location() (int, int, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return 0, 0, err
	}
	x, y, err := backend.pointerLocationLocked()
	if err != nil {
		return 0, 0, backend.failLocked("query pointer location", err)
	}
	return x, y, nil
}

func (backend *x11InputBackend) Click(name string, double bool) error {
	button, err := x11MouseButton(name)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	clicks := 1
	if double {
		clicks = 2
	}
	var eventErr error
	for click := 0; click < clicks; click++ {
		clickErr := backend.clickButtonTransactionLocked(button)
		eventErr = errors.Join(eventErr, clickErr)
		if clickErr != nil {
			break
		}
		if click+1 < clicks {
			time.Sleep(x11DoubleClickGap)
		}
	}
	if eventErr != nil {
		if !errors.Is(eventErr, errX11Connection) {
			return eventErr
		}
		return backend.failLocked("click pointer", eventErr)
	}
	return nil
}

func (backend *x11InputBackend) Toggle(name string, down bool) error {
	button, err := x11MouseButton(name)
	if err != nil {
		return err
	}
	if !x11ButtonStateObservable(button) {
		return fmt.Errorf("%w: Pure-Go X11 cannot safely own button %d because core X11 exposes state only for buttons 1-5", ErrNotSupported, button)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	var eventErr error
	if down {
		eventErr = backend.acquireButtonTransactionLocked(button)
	} else {
		eventErr = backend.releaseButtonTransactionLocked(button)
	}
	if eventErr != nil {
		if !errors.Is(eventErr, errX11Connection) {
			return eventErr
		}
		return backend.failLocked("toggle pointer button", eventErr)
	}
	return nil
}

func x11ScrollSteps(value int) (uint64, error) {
	var steps uint64
	if value < 0 {
		steps = uint64(-(value + 1)) + 1
	} else {
		steps = uint64(value)
	}
	if steps > x11MaximumScrollSteps {
		return 0, fmt.Errorf("robotgo: Pure-Go X11 scroll magnitude %d exceeds the per-axis limit %d", steps, x11MaximumScrollSteps)
	}
	return steps, nil
}

func (backend *x11InputBackend) scrollWheelLocked(button byte, count uint64) error {
	for step := uint64(0); step < count; step++ {
		if err := backend.pulseButtonTransactionLocked(button); err != nil {
			return err
		}
	}
	return nil
}

func (backend *x11InputBackend) pulseButtonTransactionLocked(button byte) error {
	return backend.withServerGrabLocked(func() error {
		pointer, err := backend.pointerStateLocked()
		if err != nil {
			return err
		}
		if err := backend.acquireButtonLocked(button, pointer.Mask); err != nil {
			return err
		}
		return backend.releaseOwnedButtonLocked(button)
	})
}

func (backend *x11InputBackend) Scroll(x, y int) error {
	xSteps, err := x11ScrollSteps(x)
	if err != nil {
		return err
	}
	ySteps, err := x11ScrollSteps(y)
	if err != nil {
		return err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.openLocked(); err != nil {
		return err
	}
	if x != 0 {
		button := x11ButtonWheelLeft
		if x < 0 {
			button = x11ButtonWheelRight
		}
		if err := backend.scrollWheelLocked(button, xSteps); err != nil {
			if errors.Is(err, errX11Connection) {
				return backend.failLocked("scroll pointer horizontally", err)
			}
			return err
		}
	}
	if y != 0 {
		button := x11ButtonWheelUp
		if y < 0 {
			button = x11ButtonWheelDown
		}
		if err := backend.scrollWheelLocked(button, ySteps); err != nil {
			if errors.Is(err, errX11Connection) {
				return backend.failLocked("scroll pointer vertically", err)
			}
			return err
		}
	}
	return nil
}

func (backend *x11InputBackend) acquireButtonTransactionLocked(button byte) error {
	return backend.withServerGrabLocked(func() error {
		pointer, err := backend.pointerStateLocked()
		if err != nil {
			return err
		}
		return backend.acquireButtonLocked(button, pointer.Mask)
	})
}

func (backend *x11InputBackend) releaseButtonTransactionLocked(button byte) error {
	return backend.withServerGrabLocked(func() error {
		return backend.releaseOwnedButtonLocked(button)
	})
}

func (backend *x11InputBackend) clickButtonTransactionLocked(button byte) error {
	return backend.withServerGrabLocked(func() error {
		pointer, err := backend.pointerStateLocked()
		if err != nil {
			return err
		}
		downErr := backend.acquireButtonLocked(button, pointer.Mask)
		if downErr == nil {
			time.Sleep(x11KeyHoldDelay)
		}
		var upErr error
		if downErr == nil {
			upErr = backend.releaseOwnedButtonLocked(button)
		}
		return errors.Join(downErr, upErr)
	})
}
