//go:build linux

package waylandoutput

import "fmt"

func (c *wireClient) dispatch(
	sender uint32,
	opcode uint16,
	payload []byte,
	callbackID uint32,
) (bool, error) {
	registered, ok := c.objects[sender]
	if !ok {
		return false, fmt.Errorf("%w: event for unknown object %d", ErrProtocol, sender)
	}
	switch registered.kind {
	case objectDisplay:
		return false, c.dispatchDisplay(opcode, payload)
	case objectRegistry:
		return false, c.dispatchRegistry(opcode, payload)
	case objectCallback:
		if opcode != eventCallbackDone || len(payload) != 4 {
			return false, fmt.Errorf("%w: malformed callback event", ErrProtocol)
		}
		return sender == callbackID, nil
	case objectOutput:
		return false, dispatchOutput(registered.output, opcode, payload)
	case objectXDGOutput:
		return false, dispatchXDGOutput(registered.output, opcode, payload)
	default:
		return false, fmt.Errorf("%w: unexpected event for object %d", ErrProtocol, sender)
	}
}

func (c *wireClient) dispatchDisplay(opcode uint16, payload []byte) error {
	switch opcode {
	case eventDisplayError:
		reader := newPayloadReader(payload)
		objectID, ok := reader.uint32()
		if !ok {
			return reader.err("display error object")
		}
		code, ok := reader.uint32()
		if !ok {
			return reader.err("display error code")
		}
		_, ok = reader.string()
		if !ok || !reader.done() {
			return reader.err("display error message")
		}
		return fmt.Errorf(
			"%w: compositor error object=%d code=%d",
			ErrProtocol,
			objectID,
			code,
		)
	case eventDisplayDeleteID:
		if len(payload) != 4 {
			return fmt.Errorf("%w: malformed display delete-id event", ErrProtocol)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown wl_display event %d", ErrProtocol, opcode)
	}
}

func (c *wireClient) dispatchRegistry(opcode uint16, payload []byte) error {
	switch opcode {
	case eventRegistryGlobal:
		reader := newPayloadReader(payload)
		name, ok := reader.uint32()
		if !ok {
			return reader.err("registry global name")
		}
		iface, ok := reader.string()
		if !ok {
			return reader.err("registry global interface")
		}
		version, ok := reader.uint32()
		if !ok || !reader.done() {
			return reader.err("registry global version")
		}
		if len(c.globals) >= maxAdvertisedGlobals {
			return fmt.Errorf(
				"%w: compositor advertises too many globals",
				ErrProtocol,
			)
		}
		c.globals = append(c.globals, global{name: name, iface: iface, version: version})
		return nil
	case eventRegistryRemove:
		reader := newPayloadReader(payload)
		name, ok := reader.uint32()
		if !ok || !reader.done() {
			return reader.err("registry remove")
		}
		c.removed[name] = struct{}{}
		return nil
	default:
		return fmt.Errorf("%w: unknown wl_registry event %d", ErrProtocol, opcode)
	}
}

func dispatchOutput(state *outputState, opcode uint16, payload []byte) error {
	reader := newPayloadReader(payload)
	switch opcode {
	case eventOutputGeometry:
		x, ok := reader.int32()
		if !ok {
			return reader.err("output geometry x")
		}
		y, ok := reader.int32()
		if !ok {
			return reader.err("output geometry y")
		}
		for range 3 {
			if _, ok := reader.int32(); !ok {
				return reader.err("output geometry integer")
			}
		}
		if _, ok := reader.string(); !ok {
			return reader.err("output make")
		}
		if _, ok := reader.string(); !ok {
			return reader.err("output model")
		}
		transform, ok := reader.int32()
		if !ok || !reader.done() {
			return reader.err("output transform")
		}
		if transform < 0 || transform > 7 {
			return fmt.Errorf("%w: invalid output transform %d", ErrProtocol, transform)
		}
		state.coreX, state.coreY = x, y
		state.transform = transform
		state.haveGeometry = true
	case eventOutputMode:
		flags, ok := reader.uint32()
		if !ok {
			return reader.err("output mode flags")
		}
		width, ok := reader.int32()
		if !ok {
			return reader.err("output mode width")
		}
		height, ok := reader.int32()
		if !ok {
			return reader.err("output mode height")
		}
		if _, ok := reader.int32(); !ok || !reader.done() {
			return reader.err("output mode refresh")
		}
		if flags&outputModeCurrent != 0 {
			state.modeWidth, state.modeHeight = width, height
			state.haveCurrentMode = true
		}
	case eventOutputDone:
		if !reader.done() {
			return reader.err("output done")
		}
	case eventOutputScale:
		scale, ok := reader.int32()
		if !ok || !reader.done() {
			return reader.err("output scale")
		}
		state.scale = scale
	case eventOutputName:
		name, ok := reader.string()
		if !ok || !reader.done() {
			return reader.err("output name")
		}
		state.name = name
	case eventOutputDescription:
		if _, ok := reader.string(); !ok || !reader.done() {
			return reader.err("output description")
		}
	default:
		return fmt.Errorf("%w: unknown wl_output event %d", ErrProtocol, opcode)
	}
	return nil
}

func dispatchXDGOutput(state *outputState, opcode uint16, payload []byte) error {
	reader := newPayloadReader(payload)
	switch opcode {
	case eventXDGLogicalPosition:
		x, ok := reader.int32()
		if !ok {
			return reader.err("xdg-output logical x")
		}
		y, ok := reader.int32()
		if !ok || !reader.done() {
			return reader.err("xdg-output logical y")
		}
		state.logicalX, state.logicalY = x, y
		state.haveLogicalPosition = true
	case eventXDGLogicalSize:
		width, ok := reader.int32()
		if !ok {
			return reader.err("xdg-output logical width")
		}
		height, ok := reader.int32()
		if !ok || !reader.done() {
			return reader.err("xdg-output logical height")
		}
		state.logicalWidth, state.logicalHeight = width, height
		state.haveLogicalSize = true
	case eventXDGDone:
		if !reader.done() {
			return reader.err("xdg-output done")
		}
	case eventXDGName:
		name, ok := reader.string()
		if !ok || !reader.done() {
			return reader.err("xdg-output name")
		}
		if state.name == "" {
			state.name = name
		}
	case eventXDGDescription:
		if _, ok := reader.string(); !ok || !reader.done() {
			return reader.err("xdg-output description")
		}
	default:
		return fmt.Errorf("%w: unknown xdg-output event %d", ErrProtocol, opcode)
	}
	return nil
}
