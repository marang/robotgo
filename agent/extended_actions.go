package agent

import (
	"context"
	"errors"
	"math"
	"time"

	robotgo "github.com/marang/robotgo"
)

const maxDragInterpolationSteps = 120

var errPartialAction = errors.New("agent action may have partially executed")

func ambiguousMutationError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(errPartialAction, err)
}

func scrollDistance(action ScrollAction) uint64 {
	perEvent := saturatingAdd(absIntMagnitude(action.DeltaX), absIntMagnitude(action.DeltaY))
	return saturatingMultiply(perEvent, uint64(action.Events))
}

func dragDistance(action DragAction) uint64 {
	deltaX := axisDistance(action.StartX, action.EndX)
	deltaY := axisDistance(action.StartY, action.EndY)
	if deltaX > maxAgentDragDistance || deltaY > maxAgentDragDistance {
		return maxAgentDragDistance + 1
	}
	return uint64(math.Ceil(math.Hypot(float64(deltaX), float64(deltaY))))
}

func axisDistance(left, right int) uint64 {
	if left >= right {
		return uint64(left) - uint64(right)
	}
	return uint64(right) - uint64(left)
}

func absIntMagnitude(value int) uint64 {
	if value >= 0 {
		return uint64(value)
	}
	return uint64(-(value + 1)) + 1
}

func saturatingAdd(left, right uint64) uint64 {
	result := left + right
	if result < left {
		return ^uint64(0)
	}
	return result
}

func saturatingMultiply(left, right uint64) uint64 {
	if left != 0 && right > ^uint64(0)/left {
		return ^uint64(0)
	}
	return left * right
}

func (s *Session) validateActionRate() error {
	if s.policy.MinActionIntervalMillis == 0 || s.lastAction.IsZero() {
		return nil
	}
	elapsed := s.now().Sub(s.lastAction)
	if elapsed < 0 || elapsed < time.Duration(s.policy.MinActionIntervalMillis)*time.Millisecond {
		return ErrPolicyDenied
	}
	return nil
}

func (s *Session) executionError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return ErrSessionClosed
	}
	return nil
}

func (s *Session) waitForExecution(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return s.executionError(ctx)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.executionError(ctx)
	case <-timer.C:
		return nil
	}
}

func (s *Session) executeScroll(ctx context.Context, action ScrollAction) error {
	target := MoveAction{X: action.TargetX, Y: action.TargetY, DisplayID: action.DisplayID}
	if err := s.validateMoveTarget(target); err != nil {
		return err
	}
	if err := s.executionError(ctx); err != nil {
		return err
	}
	if err := s.driver.MoveImmediate(target.X, target.Y, target.DisplayID); err != nil {
		return errors.Join(errPartialAction, err)
	}
	for event := uint32(0); event < action.Events; event++ {
		if err := s.executionError(ctx); err != nil {
			return errors.Join(errPartialAction, err)
		}
		if err := s.driver.ScrollImmediate(action.DeltaX, action.DeltaY); err != nil {
			return errors.Join(errPartialAction, err)
		}
	}
	return nil
}

func (s *Session) executeDrag(ctx context.Context, action DragAction) (returnErr error) {
	start := MoveAction{X: action.StartX, Y: action.StartY, DisplayID: action.DisplayID}
	end := MoveAction{X: action.EndX, Y: action.EndY, DisplayID: action.DisplayID}
	if err := s.validateMoveTarget(start); err != nil {
		return err
	}
	if err := s.validateMoveTarget(end); err != nil {
		return err
	}
	if err := s.executionError(ctx); err != nil {
		return err
	}
	if err := s.driver.MoveImmediate(start.X, start.Y, start.DisplayID); err != nil {
		return errors.Join(errPartialAction, err)
	}
	if err := s.executionError(ctx); err != nil {
		return errors.Join(errPartialAction, err)
	}
	pressed, err := s.pressMouse(action.Button)
	if err != nil {
		return errors.Join(errPartialAction, err, s.releasePressedInput(pressed))
	}
	defer func() {
		if cleanupErr := s.releasePressedInput(pressed); cleanupErr != nil {
			returnErr = errors.Join(errPartialAction, returnErr, cleanupErr)
		}
	}()

	steps := dragInterpolationSteps(action)
	interval := time.Duration(action.DurationMillis) * time.Millisecond / time.Duration(steps)
	for step := 1; step <= steps; step++ {
		if err := s.waitForExecution(ctx, interval); err != nil {
			return errors.Join(errPartialAction, err)
		}
		x := interpolateCoordinate(action.StartX, action.EndX, step, steps)
		y := interpolateCoordinate(action.StartY, action.EndY, step, steps)
		if err := s.driver.MoveImmediate(x, y, action.DisplayID); err != nil {
			return errors.Join(errPartialAction, err)
		}
	}
	return nil
}

func dragInterpolationSteps(action DragAction) int {
	distanceSteps := dragDistance(action)
	if distanceSteps == 0 {
		return 1
	}
	if distanceSteps > maxDragInterpolationSteps {
		return maxDragInterpolationSteps
	}
	return int(distanceSteps)
}

func interpolateCoordinate(start, end, step, steps int) int {
	if step >= steps {
		return end
	}
	if end >= start {
		return start + (end-start)*step/steps
	}
	return start - (start-end)*step/steps
}

func (s *Session) executeKeyChord(ctx context.Context, action KeyChordAction) (returnErr error) {
	if err := s.executionError(ctx); err != nil {
		return err
	}
	if err := s.validateActiveWindow(action); err != nil {
		return err
	}
	if err := s.executionError(ctx); err != nil {
		return err
	}
	pressed, err := s.pressKey(action.Key, action.Modifiers, action.TargetPID)
	if err != nil {
		if errors.Is(err, robotgo.ErrInputNotApplied) ||
			errors.Is(err, robotgo.ErrInputOwnership) {
			return err
		}
		return errors.Join(errPartialAction, err, s.releasePressedInput(pressed))
	}
	defer func() {
		if cleanupErr := s.releasePressedInput(pressed); cleanupErr != nil {
			returnErr = errors.Join(errPartialAction, returnErr, cleanupErr)
		}
	}()
	if err := s.executionError(ctx); err != nil {
		return errors.Join(errPartialAction, err)
	}
	return nil
}

func (s *Session) validateActiveWindow(action KeyChordAction) error {
	identity := windowTargetIdentity{target: action.TargetPID, kind: WindowTargetProcess}
	target := s.policy.allowWindow[identity]
	title, err := s.driver.WindowTitle(action.TargetPID, WindowTargetProcess)
	if err != nil {
		return err
	}
	if title != target.ExpectedTitle {
		return ErrStaleTarget
	}
	return nil
}

func (s *Session) executeActivate(ctx context.Context, action ActivateWindowAction) error {
	handle, err := s.resolveValidatedWindow(action)
	if err != nil {
		return err
	}
	if err := s.executionError(ctx); err != nil {
		return err
	}
	return ambiguousMutationError(s.driver.ActivateWindow(handle, WindowTargetHandle))
}

func (s *Session) validateWindowIdentity(action ActivateWindowAction) error {
	_, err := s.resolveValidatedWindow(action)
	return err
}

func (s *Session) resolveValidatedWindow(action ActivateWindowAction) (int, error) {
	identity := windowTargetIdentity{target: action.Target, kind: action.Kind}
	target := s.policy.allowWindow[identity]
	handle, err := s.driver.ResolveWindow(action.Target, action.Kind)
	if err != nil {
		return 0, err
	}
	title, err := s.driver.WindowTitle(handle, WindowTargetHandle)
	if err != nil {
		return 0, err
	}
	if title != target.ExpectedTitle {
		return 0, ErrStaleTarget
	}
	return handle, nil
}

func (s *Session) pressMouse(button MouseButton) (pressedInput, error) {
	pressed := pressedInput{button: button}
	s.pressedInputs = append(s.pressedInputs, pressed)
	err := s.driver.ToggleMouse(button, true)
	if inputDownKnownNotApplied(err) {
		s.pressedInputs = s.pressedInputs[:len(s.pressedInputs)-1]
		return pressedInput{}, err
	}
	return pressed, err
}

func (s *Session) pressKey(
	key string,
	modifiers []KeyModifier,
	targetPID int,
) (pressedInput, error) {
	ownedModifiers := append([]KeyModifier(nil), modifiers...)
	pressed := pressedInput{
		key: key, modifiers: ownedModifiers, targetPID: targetPID, keyboard: true,
	}
	s.pressedInputs = append(s.pressedInputs, pressed)
	err := s.driver.ToggleKeyImmediate(key, ownedModifiers, targetPID, true)
	if inputDownKnownNotApplied(err) {
		s.pressedInputs = s.pressedInputs[:len(s.pressedInputs)-1]
		return pressedInput{}, err
	}
	return pressed, err
}

func inputDownKnownNotApplied(err error) bool {
	return errors.Is(err, robotgo.ErrInputNotApplied) ||
		errors.Is(err, robotgo.ErrInputOwnership)
}

func (s *Session) releasePressedInput(pressed pressedInput) error {
	index := s.pressedInputIndex(pressed)
	if index < 0 {
		return nil
	}
	var err error
	if pressed.keyboard {
		err = s.driver.ToggleKeyImmediate(
			pressed.key,
			pressed.modifiers,
			pressed.targetPID,
			false,
		)
	} else {
		err = s.driver.ToggleMouse(pressed.button, false)
	}
	if err != nil {
		if errors.Is(err, robotgo.ErrInputOwnership) {
			s.pressedInputs = append(s.pressedInputs[:index], s.pressedInputs[index+1:]...)
			if len(s.pressedInputs) == 0 {
				s.inputTainted = false
			}
			return nil
		}
		s.inputTainted = true
		return errors.Join(ErrInputCleanup, err)
	}
	s.pressedInputs = append(s.pressedInputs[:index], s.pressedInputs[index+1:]...)
	if len(s.pressedInputs) == 0 {
		s.inputTainted = false
	}
	return nil
}

func (s *Session) pressedInputIndex(pressed pressedInput) int {
	for index := len(s.pressedInputs) - 1; index >= 0; index-- {
		candidate := s.pressedInputs[index]
		if candidate.keyboard != pressed.keyboard ||
			candidate.button != pressed.button ||
			candidate.key != pressed.key ||
			candidate.targetPID != pressed.targetPID ||
			!sameModifiers(candidate.modifiers, pressed.modifiers) {
			continue
		}
		return index
	}
	return -1
}

func sameModifiers(left, right []KeyModifier) bool {
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

func (s *Session) releaseAllInputs() error {
	var releaseErr error
	for len(s.pressedInputs) > 0 {
		before := len(s.pressedInputs)
		pressed := s.pressedInputs[before-1]
		releaseErr = errors.Join(releaseErr, s.releasePressedInput(pressed))
		if len(s.pressedInputs) == before {
			break
		}
	}
	return releaseErr
}
