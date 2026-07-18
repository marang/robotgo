//go:build linux

// Package waylandoutput provides bounded, read-only Wayland output discovery.
package waylandoutput

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	envWaylandDisplay = "WAYLAND_DISPLAY"
	envXDGRuntimeDir  = "XDG_RUNTIME_DIR"

	interfaceOutput           = "wl_output"
	interfaceXDGOutputManager = "zxdg_output_manager_v1"

	maxOutputVersion           = uint32(4)
	maxXDGOutputManagerVersion = uint32(3)
	maxAdvertisedGlobals       = 4096
	maxAdvertisedOutputs       = 256
	displayObjectID            = uint32(1)
	firstClientID              = uint32(2)
	displaySync                = uint16(0)
	displayRegistry            = uint16(1)
	registryBind               = uint16(0)
	xdgManagerGetOutput        = uint16(1)

	eventDisplayError       = uint16(0)
	eventDisplayDeleteID    = uint16(1)
	eventRegistryGlobal     = uint16(0)
	eventRegistryRemove     = uint16(1)
	eventCallbackDone       = uint16(0)
	eventOutputGeometry     = uint16(0)
	eventOutputMode         = uint16(1)
	eventOutputDone         = uint16(2)
	eventOutputScale        = uint16(3)
	eventOutputName         = uint16(4)
	eventOutputDescription  = uint16(5)
	eventXDGLogicalPosition = uint16(0)
	eventXDGLogicalSize     = uint16(1)
	eventXDGDone            = uint16(2)
	eventXDGName            = uint16(3)
	eventXDGDescription     = uint16(4)

	outputModeCurrent = uint32(1)
)

var (
	// ErrUnavailable indicates that the Wayland display or output protocols
	// cannot provide a usable snapshot.
	ErrUnavailable = errors.New("wayland output enumeration unavailable")
	// ErrProtocol indicates a malformed or inconsistent compositor response.
	ErrProtocol = errors.New("invalid Wayland output protocol response")
)

type global struct {
	name    uint32
	iface   string
	version uint32
}

type objectKind uint8

const (
	objectDisplay objectKind = iota
	objectRegistry
	objectCallback
	objectOutput
	objectXDGManager
	objectXDGOutput
)

type object struct {
	kind   objectKind
	output *outputState
}

type wireClient struct {
	conn    net.Conn
	nextID  uint32
	objects map[uint32]object
	globals []global
	outputs []*outputState
	removed map[uint32]struct{}
}

// Enumerate returns the current logical output layout. The caller must provide
// a context with a deadline so dialing, roundtrips, and cleanup are bounded.
func Enumerate(ctx context.Context) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, fmt.Errorf("%w: nil context", ErrUnavailable)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: context deadline is required", ErrUnavailable)
	}
	path, err := socketPath()
	if err != nil {
		return Snapshot{}, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: connect to configured Wayland display", ErrUnavailable)
	}
	defer func() {
		_ = conn.Close()
	}()
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	defer stopCancellation()
	if err := conn.SetDeadline(deadline); err != nil {
		return Snapshot{}, fmt.Errorf("%w: set socket deadline: %v", ErrUnavailable, err)
	}

	client := &wireClient{
		conn:    conn,
		nextID:  firstClientID,
		objects: map[uint32]object{displayObjectID: {kind: objectDisplay}},
		removed: make(map[uint32]struct{}),
	}
	snapshot, err := client.enumerate()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Snapshot{}, fmt.Errorf("%w: %v", ErrUnavailable, ctxErr)
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return Snapshot{}, fmt.Errorf("%w: output query deadline exceeded", ErrUnavailable)
		}
		return Snapshot{}, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrUnavailable, ctxErr)
	}
	return snapshot, nil
}

func socketPath() (string, error) {
	name := strings.TrimSpace(os.Getenv(envWaylandDisplay))
	if name == "" {
		name = "wayland-0"
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name), nil
	}
	if name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("%w: invalid %s socket name", ErrUnavailable, envWaylandDisplay)
	}
	runtimeDir := strings.TrimSpace(os.Getenv(envXDGRuntimeDir))
	if runtimeDir == "" {
		return "", fmt.Errorf("%w: %s is not set", ErrUnavailable, envXDGRuntimeDir)
	}
	return filepath.Join(runtimeDir, name), nil
}

func (c *wireClient) enumerate() (Snapshot, error) {
	registryID := c.allocate(object{kind: objectRegistry})
	if err := c.writeRequest(displayObjectID, displayRegistry, uint32Value(registryID)); err != nil {
		return Snapshot{}, fmt.Errorf("%w: request registry: %v", ErrUnavailable, err)
	}
	if err := c.roundtrip(); err != nil {
		return Snapshot{}, err
	}

	var outputGlobals []global
	var xdgManager global
	for _, advertised := range c.globals {
		if _, removed := c.removed[advertised.name]; removed {
			continue
		}
		switch advertised.iface {
		case interfaceOutput:
			outputGlobals = append(outputGlobals, advertised)
		case interfaceXDGOutputManager:
			if xdgManager.name == 0 {
				xdgManager = advertised
			}
		}
	}
	if len(outputGlobals) == 0 {
		return Snapshot{}, fmt.Errorf("%w: compositor advertises no %s globals", ErrUnavailable, interfaceOutput)
	}
	if len(outputGlobals) > maxAdvertisedOutputs {
		return Snapshot{}, fmt.Errorf(
			"%w: compositor advertises too many outputs (%d)",
			ErrProtocol,
			len(outputGlobals),
		)
	}
	sort.Slice(outputGlobals, func(i, j int) bool {
		return outputGlobals[i].name < outputGlobals[j].name
	})

	var outputVersion uint32
	for _, advertised := range outputGlobals {
		version := min(advertised.version, maxOutputVersion)
		if version == 0 {
			return Snapshot{}, fmt.Errorf("%w: %s version is zero", ErrProtocol, interfaceOutput)
		}
		state := &outputState{
			globalName: advertised.name,
			scale:      1,
		}
		outputID := c.allocate(object{kind: objectOutput, output: state})
		if err := c.bind(registryID, advertised.name, interfaceOutput, version, outputID); err != nil {
			return Snapshot{}, fmt.Errorf("%w: bind %s global %d: %v", ErrUnavailable, interfaceOutput, advertised.name, err)
		}
		c.outputs = append(c.outputs, state)
		outputVersion = max(outputVersion, version)
	}

	var xdgVersion uint32
	if xdgManager.name != 0 {
		xdgVersion = min(xdgManager.version, maxXDGOutputManagerVersion)
		if xdgVersion == 0 {
			return Snapshot{}, fmt.Errorf("%w: %s version is zero", ErrProtocol, interfaceXDGOutputManager)
		}
		managerID := c.allocate(object{kind: objectXDGManager})
		if err := c.bind(registryID, xdgManager.name, interfaceXDGOutputManager, xdgVersion, managerID); err != nil {
			return Snapshot{}, fmt.Errorf("%w: bind %s: %v", ErrUnavailable, interfaceXDGOutputManager, err)
		}
		outputIDs := c.outputObjectIDs()
		for index, state := range c.outputs {
			xdgOutputID := c.allocate(object{kind: objectXDGOutput, output: state})
			payload := append(uint32Value(xdgOutputID), uint32Value(outputIDs[index])...)
			if err := c.writeRequest(managerID, xdgManagerGetOutput, payload); err != nil {
				return Snapshot{}, fmt.Errorf("%w: create xdg-output for global %d: %v", ErrUnavailable, state.globalName, err)
			}
		}
	}

	if err := c.roundtrip(); err != nil {
		return Snapshot{}, err
	}
	for _, state := range c.outputs {
		if _, removed := c.removed[state.globalName]; removed {
			return Snapshot{}, fmt.Errorf(
				"%w: output global %d was removed during enumeration",
				ErrUnavailable,
				state.globalName,
			)
		}
	}
	outputs, err := resolveOutputs(c.outputs, xdgVersion != 0)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Outputs:          outputs,
		OutputVersion:    outputVersion,
		XDGOutputVersion: xdgVersion,
	}, nil
}

func (c *wireClient) outputObjectIDs() []uint32 {
	ids := make([]uint32, 0, len(c.outputs))
	for id, registered := range c.objects {
		if registered.kind == objectOutput {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return c.objects[ids[i]].output.globalName < c.objects[ids[j]].output.globalName
	})
	return ids
}

func (c *wireClient) bind(
	registryID, globalName uint32,
	iface string,
	version, objectID uint32,
) error {
	payload := uint32Value(globalName)
	payload = append(payload, stringValue(iface)...)
	payload = append(payload, uint32Value(version)...)
	payload = append(payload, uint32Value(objectID)...)
	return c.writeRequest(registryID, registryBind, payload)
}

func (c *wireClient) roundtrip() error {
	callbackID := c.allocate(object{kind: objectCallback})
	if err := c.writeRequest(displayObjectID, displaySync, uint32Value(callbackID)); err != nil {
		return fmt.Errorf("%w: request synchronization: %v", ErrUnavailable, err)
	}
	for {
		sender, opcode, payload, err := c.readMessage()
		if err != nil {
			return fmt.Errorf("%w: read compositor response: %v", ErrUnavailable, err)
		}
		done, err := c.dispatch(sender, opcode, payload, callbackID)
		if err != nil {
			return err
		}
		if done {
			delete(c.objects, callbackID)
			return nil
		}
	}
}

func (c *wireClient) allocate(value object) uint32 {
	id := c.nextID
	c.nextID++
	c.objects[id] = value
	return id
}
