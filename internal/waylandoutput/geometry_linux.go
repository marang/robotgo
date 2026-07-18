//go:build linux

package waylandoutput

import (
	"fmt"
	"sort"
)

// Output is one logical compositor output rectangle.
type Output struct {
	GlobalName uint32
	Name       string
	X          int
	Y          int
	Width      int
	Height     int
	Scale      int
	Transform  int
	Logical    bool
}

// Snapshot is an atomic one-shot view of the compositor outputs.
type Snapshot struct {
	Outputs          []Output
	OutputVersion    uint32
	XDGOutputVersion uint32
}

type outputState struct {
	globalName uint32
	name       string

	coreX, coreY    int32
	modeWidth       int32
	modeHeight      int32
	scale           int32
	transform       int32
	haveGeometry    bool
	haveCurrentMode bool

	logicalX, logicalY          int32
	logicalWidth, logicalHeight int32
	haveLogicalPosition         bool
	haveLogicalSize             bool
}

func resolveOutputs(states []*outputState, requireLogical bool) ([]Output, error) {
	outputs := make([]Output, 0, len(states))
	for _, state := range states {
		output, err := state.resolve(requireLogical)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, output)
	}
	sort.Slice(outputs, func(i, j int) bool {
		left, right := outputs[i], outputs[j]
		leftPrimary := outputContainsOrigin(left)
		rightPrimary := outputContainsOrigin(right)
		if leftPrimary != rightPrimary {
			return leftPrimary
		}
		if left.Y != right.Y {
			return left.Y < right.Y
		}
		if left.X != right.X {
			return left.X < right.X
		}
		return left.GlobalName < right.GlobalName
	})
	return outputs, nil
}

func outputContainsOrigin(output Output) bool {
	return output.X <= 0 &&
		output.X+output.Width > 0 &&
		output.Y <= 0 &&
		output.Y+output.Height > 0
}

func (state *outputState) resolve(requireLogical bool) (Output, error) {
	if requireLogical {
		if !state.haveLogicalPosition || !state.haveLogicalSize {
			return Output{}, fmt.Errorf(
				"%w: xdg-output data incomplete for global %d",
				ErrProtocol,
				state.globalName,
			)
		}
		return checkedOutput(
			state,
			state.logicalX,
			state.logicalY,
			state.logicalWidth,
			state.logicalHeight,
			true,
		)
	}
	if !state.haveGeometry || !state.haveCurrentMode {
		return Output{}, fmt.Errorf(
			"%w: core output data incomplete for global %d",
			ErrProtocol,
			state.globalName,
		)
	}
	if state.scale <= 0 {
		return Output{}, fmt.Errorf(
			"%w: invalid scale %d for global %d",
			ErrProtocol,
			state.scale,
			state.globalName,
		)
	}
	width, height := state.modeWidth, state.modeHeight
	if transformRotates(state.transform) {
		width, height = height, width
	}
	width, ok := ceilDividePositive(width, state.scale)
	if !ok {
		return Output{}, fmt.Errorf("%w: invalid width for global %d", ErrProtocol, state.globalName)
	}
	height, ok = ceilDividePositive(height, state.scale)
	if !ok {
		return Output{}, fmt.Errorf("%w: invalid height for global %d", ErrProtocol, state.globalName)
	}
	return checkedOutput(state, state.coreX, state.coreY, width, height, false)
}

func checkedOutput(
	state *outputState,
	x, y, width, height int32,
	logical bool,
) (Output, error) {
	if width <= 0 || height <= 0 {
		return Output{}, fmt.Errorf(
			"%w: non-positive bounds %dx%d for global %d",
			ErrProtocol,
			width,
			height,
			state.globalName,
		)
	}
	maxInt := int64(^uint(0) >> 1)
	if int64(x)+int64(width) > maxInt || int64(y)+int64(height) > maxInt {
		return Output{}, fmt.Errorf("%w: bounds overflow for global %d", ErrProtocol, state.globalName)
	}
	return Output{
		GlobalName: state.globalName,
		Name:       state.name,
		X:          int(x),
		Y:          int(y),
		Width:      int(width),
		Height:     int(height),
		Scale:      int(state.scale),
		Transform:  int(state.transform),
		Logical:    logical,
	}, nil
}

func ceilDividePositive(value, divisor int32) (int32, bool) {
	if value <= 0 || divisor <= 0 {
		return 0, false
	}
	return int32((int64(value) + int64(divisor) - 1) / int64(divisor)), true
}

func transformRotates(transform int32) bool {
	switch transform {
	case 1, 3, 5, 7:
		return true
	default:
		return false
	}
}
