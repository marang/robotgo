//go:build linux

package portalrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	hostedTopologyTimeout = 30 * time.Second
	hostedTopologyPoll    = 250 * time.Millisecond
	hostedTopologyOutput  = 64 * 1024

	mutterDisplayDestination = "org.gnome.Mutter.DisplayConfig"
	mutterDisplayPath        = dbus.ObjectPath("/org/gnome/Mutter/DisplayConfig")
	mutterDisplayInterface   = "org.gnome.Mutter.DisplayConfig"
	mutterApplyTemporary     = uint32(1)

	hostedDisplayStageManifest    = "manifest"
	hostedDisplayStageLane        = "lane"
	hostedDisplayStageStatus      = "status"
	hostedDisplayStageGNOMEBus    = "gnome-bus"
	hostedDisplayStageGNOMEState  = "gnome-state"
	hostedDisplayStageGNOMEPlan   = "gnome-plan"
	hostedDisplayStageGNOMEApply  = "gnome-apply"
	hostedDisplayStageGNOMESettle = "gnome-settle"
	hostedDisplayStageKDEState    = "kde-state"
	hostedDisplayStageKDEPlan     = "kde-plan"
	hostedDisplayStageKDEApply    = "kde-apply"
	hostedDisplayStageKDESettle   = "kde-settle"
	hostedDisplayFailureMarker    = "ROBOTGO_HOSTED_DISPLAY_STAGE="
)

var topologyIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type topologyMode struct {
	ID        string
	Width     int
	Height    int
	Current   bool
	Preferred bool
	RefreshHz float64
	Safe      bool
}

type topologyConnector struct {
	Name      string
	Connected bool
	Enabled   bool
	Modes     []topologyMode
	CurrentID string
}

type topologySelection struct {
	Connector string
	ModeID    string
	Output    HostedOutput
	Primary   bool
}

type mutterMonitorIdentity struct {
	Connector string
	Vendor    string
	Product   string
	Serial    string
}

type mutterMode struct {
	ID              string
	Width           int32
	Height          int32
	RefreshRate     float64
	PreferredScale  float64
	SupportedScales []float64
	Properties      map[string]dbus.Variant
}

type mutterMonitor struct {
	Identity   mutterMonitorIdentity
	Modes      []mutterMode
	Properties map[string]dbus.Variant
}

type mutterLogicalMonitor struct {
	X          int32
	Y          int32
	Scale      float64
	Transform  uint32
	Primary    bool
	Monitors   []mutterMonitorIdentity
	Properties map[string]dbus.Variant
}

type mutterRequestedMonitor struct {
	Connector  string
	ModeID     string
	Properties map[string]dbus.Variant
}

type mutterRequestedLogicalMonitor struct {
	X         int32
	Y         int32
	Scale     float64
	Transform uint32
	Primary   bool
	Monitors  []mutterRequestedMonitor
}

type mutterDisplayState struct {
	Serial          uint32
	Monitors        []mutterMonitor
	LogicalMonitors []mutterLogicalMonitor
	Properties      map[string]dbus.Variant
}

type kscreenSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type kscreenPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type kscreenMode struct {
	ID          string      `json:"id"`
	Size        kscreenSize `json:"size"`
	RefreshRate float64     `json:"refreshRate"`
}

type kscreenOutput struct {
	Name          string        `json:"name"`
	Position      kscreenPoint  `json:"pos"`
	Scale         float64       `json:"scale"`
	Size          kscreenSize   `json:"size"`
	CurrentModeID string        `json:"currentModeId"`
	Connected     bool          `json:"connected"`
	Enabled       bool          `json:"enabled"`
	Modes         []kscreenMode `json:"modes"`
}

type kscreenState struct {
	Outputs []kscreenOutput `json:"outputs"`
}

type hostedDisplayFailure struct {
	stage string
}

func (failure *hostedDisplayFailure) Error() string {
	return "configure hosted display"
}

// HostedDisplayFailureStage returns only an allowlisted, privacy-safe stage
// name for a topology failure. It never exposes connector or mode identifiers.
func HostedDisplayFailureStage(err error) string {
	var failure *hostedDisplayFailure
	if errors.As(err, &failure) {
		return failure.stage
	}
	return ""
}

// WriteHostedDisplayFailureMarker emits only the allowlisted stage marker used
// by the independent host controller.
func WriteHostedDisplayFailureMarker(output io.Writer, err error) error {
	if output == nil {
		return errors.New("hosted display failure output is nil")
	}
	stage := HostedDisplayFailureStage(err)
	if stage == "" {
		return errors.New("hosted display failure stage is unavailable")
	}
	if _, writeErr := fmt.Fprintln(
		output,
		hostedDisplayFailureMarker+stage,
	); writeErr != nil {
		return errors.New("write hosted display failure stage")
	}
	return nil
}

func newHostedDisplayFailure(stage string) error {
	return &hostedDisplayFailure{stage: stage}
}

// ConfigureHostedDisplay applies and verifies the manifest-bound topology
// inside a disposable hosted guest. Callers must independently ensure this is
// never invoked against a developer desktop.
func ConfigureHostedDisplay(
	ctx context.Context,
	manifest Manifest,
	output io.Writer,
) error {
	if output == nil {
		return newHostedDisplayFailure(hostedDisplayStageStatus)
	}
	if err := manifest.HostedDisplay.Validate(); err != nil {
		return newHostedDisplayFailure(hostedDisplayStageManifest)
	}
	topologyContext, cancel := context.WithTimeout(ctx, hostedTopologyTimeout)
	defer cancel()
	switch manifest.Lane {
	case portalLaneGNOME:
		if err := configureMutterDisplay(
			topologyContext,
			manifest.HostedDisplay,
		); err != nil {
			return err
		}
	case portalLaneKDE:
		if err := configureKScreenDisplay(
			topologyContext,
			manifest.HostedDisplay,
		); err != nil {
			return err
		}
	default:
		return newHostedDisplayFailure(hostedDisplayStageLane)
	}
	minX, minY, maxX, maxY := hostedDisplayBounds(
		manifest.HostedDisplay.Outputs,
	)
	if err := writeStatus(
		output,
		"hosted display topology configured outputs=2 bounds=%d,%d,%dx%d\n",
		minX,
		minY,
		maxX-minX,
		maxY-minY,
	); err != nil {
		return newHostedDisplayFailure(hostedDisplayStageStatus)
	}
	return nil
}

func configureMutterDisplay(
	ctx context.Context,
	display HostedDisplay,
) (returnError error) {
	connection, err := dbus.ConnectSessionBus()
	if err != nil {
		return newHostedDisplayFailure(hostedDisplayStageGNOMEBus)
	}
	defer func() {
		if err := connection.Close(); err != nil {
			returnError = errors.Join(
				returnError,
				newHostedDisplayFailure(hostedDisplayStageGNOMEBus),
			)
		}
	}()
	object := connection.Object(mutterDisplayDestination, mutterDisplayPath)
	state, err := readMutterDisplayState(ctx, object)
	if err != nil {
		return newHostedDisplayFailure(hostedDisplayStageGNOMEState)
	}
	selections, err := selectHostedTopology(
		mutterTopologyConnectors(state.Monitors),
		display.Outputs,
	)
	if err != nil {
		return newHostedDisplayFailure(hostedDisplayStageGNOMEPlan)
	}
	requested := make([]mutterRequestedLogicalMonitor, 0, len(selections))
	for _, selection := range selections {
		requested = append(requested, mutterRequestedLogicalMonitor{
			X:         int32(selection.Output.X),
			Y:         int32(selection.Output.Y),
			Scale:     1,
			Transform: 0,
			Primary:   selection.Primary,
			Monitors: []mutterRequestedMonitor{{
				Connector:  selection.Connector,
				ModeID:     selection.ModeID,
				Properties: map[string]dbus.Variant{},
			}},
		})
	}
	properties := map[string]dbus.Variant{}
	if layoutMode, ok := state.Properties["layout-mode"]; ok {
		var mode uint32
		if err := layoutMode.Store(&mode); err != nil ||
			mode < 1 || mode > 2 {
			return newHostedDisplayFailure(hostedDisplayStageGNOMEState)
		}
		properties["layout-mode"] = dbus.MakeVariant(mode)
	}
	call := object.CallWithContext(
		ctx,
		mutterDisplayInterface+".ApplyMonitorsConfig",
		0,
		state.Serial,
		mutterApplyTemporary,
		requested,
		properties,
	)
	if call.Err != nil {
		return newHostedDisplayFailure(hostedDisplayStageGNOMEApply)
	}
	return waitForMutterTopology(ctx, object, selections)
}

func readMutterDisplayState(
	ctx context.Context,
	object dbus.BusObject,
) (mutterDisplayState, error) {
	call := object.CallWithContext(
		ctx,
		mutterDisplayInterface+".GetCurrentState",
		0,
	)
	if call.Err != nil {
		return mutterDisplayState{}, errors.New(
			"read hosted GNOME display state",
		)
	}
	var state mutterDisplayState
	if err := call.Store(
		&state.Serial,
		&state.Monitors,
		&state.LogicalMonitors,
		&state.Properties,
	); err != nil {
		return mutterDisplayState{}, errors.New(
			"decode hosted GNOME display state",
		)
	}
	return state, nil
}

func mutterTopologyConnectors(
	monitors []mutterMonitor,
) []topologyConnector {
	connectors := make([]topologyConnector, 0, len(monitors))
	for _, monitor := range monitors {
		connector := topologyConnector{
			Name:      monitor.Identity.Connector,
			Connected: true,
			Modes:     make([]topologyMode, 0, len(monitor.Modes)),
		}
		for _, mode := range monitor.Modes {
			current := mutterBoolProperty(mode.Properties, "is-current")
			connector.Modes = append(connector.Modes, topologyMode{
				ID: mode.ID, Width: int(mode.Width), Height: int(mode.Height),
				Current: current,
				Preferred: mutterBoolProperty(
					mode.Properties,
					"is-preferred",
				),
				RefreshHz: mode.RefreshRate,
				Safe:      safeMutterModeID(mode.ID),
			})
			if current {
				connector.Enabled = true
				connector.CurrentID = mode.ID
			}
		}
		connectors = append(connectors, connector)
	}
	return connectors
}

func mutterBoolProperty(
	properties map[string]dbus.Variant,
	name string,
) bool {
	value, ok := properties[name]
	if !ok {
		return false
	}
	var result bool
	return value.Store(&result) == nil && result
}

func waitForMutterTopology(
	ctx context.Context,
	object dbus.BusObject,
	selections []topologySelection,
) error {
	for {
		state, err := readMutterDisplayState(ctx, object)
		if err == nil &&
			mutterTopologyMatches(state, selections) {
			return nil
		}
		select {
		case <-ctx.Done():
			return newHostedDisplayFailure(hostedDisplayStageGNOMESettle)
		case <-time.After(hostedTopologyPoll):
		}
	}
}

func mutterTopologyMatches(
	state mutterDisplayState,
	selections []topologySelection,
) bool {
	if len(state.LogicalMonitors) != len(selections) {
		return false
	}
	byConnector := make(map[string]mutterLogicalMonitor, len(selections))
	for _, logical := range state.LogicalMonitors {
		if len(logical.Monitors) != 1 ||
			logical.Scale != 1 ||
			logical.Transform != 0 {
			return false
		}
		connector := logical.Monitors[0].Connector
		if _, duplicate := byConnector[connector]; duplicate {
			return false
		}
		byConnector[connector] = logical
	}
	connectors := mutterTopologyConnectors(state.Monitors)
	currentModes := make(map[string]string, len(connectors))
	for _, connector := range connectors {
		currentModes[connector.Name] = connector.CurrentID
	}
	for _, selection := range selections {
		logical, ok := byConnector[selection.Connector]
		if !ok ||
			int(logical.X) != selection.Output.X ||
			int(logical.Y) != selection.Output.Y ||
			logical.Primary != selection.Primary ||
			currentModes[selection.Connector] != selection.ModeID {
			return false
		}
	}
	return true
}

func configureKScreenDisplay(
	ctx context.Context,
	display HostedDisplay,
) error {
	state, err := readKScreenState(ctx)
	if err != nil {
		return newHostedDisplayFailure(hostedDisplayStageKDEState)
	}
	selections, err := selectHostedTopology(
		kscreenTopologyConnectors(state.Outputs),
		display.Outputs,
	)
	if err != nil {
		return newHostedDisplayFailure(hostedDisplayStageKDEPlan)
	}
	arguments := make([]string, 0, len(selections)*6)
	for index, selection := range selections {
		name := selection.Connector
		priority := index + 1
		arguments = append(
			arguments,
			"output."+name+".enable",
			"output."+name+".mode."+selection.ModeID,
			fmt.Sprintf(
				"output.%s.position.%d,%d",
				name,
				selection.Output.X,
				selection.Output.Y,
			),
			"output."+name+".scale.1",
			"output."+name+".rotation.normal",
			fmt.Sprintf("output.%s.priority.%d", name, priority),
		)
	}
	command := exec.CommandContext(ctx, "kscreen-doctor", arguments...)
	var commandOutput bytes.Buffer
	command.Stdout = &boundedWriter{
		destination: &commandOutput,
		remaining:   hostedTopologyOutput,
	}
	command.Stderr = command.Stdout
	configureHostedProcess(command)
	if err := command.Run(); err != nil {
		return newHostedDisplayFailure(hostedDisplayStageKDEApply)
	}
	for {
		state, err = readKScreenState(ctx)
		if err == nil &&
			kscreenTopologyMatches(state.Outputs, selections) {
			return nil
		}
		select {
		case <-ctx.Done():
			return newHostedDisplayFailure(hostedDisplayStageKDESettle)
		case <-time.After(hostedTopologyPoll):
		}
	}
}

func readKScreenState(ctx context.Context) (kscreenState, error) {
	command := exec.CommandContext(ctx, "kscreen-doctor", "-j")
	var data bytes.Buffer
	command.Stdout = &boundedWriter{
		destination: &data,
		remaining:   hostedTopologyOutput,
	}
	command.Stderr = io.Discard
	configureHostedProcess(command)
	if err := command.Run(); err != nil {
		return kscreenState{}, errors.New("read hosted KDE display state")
	}
	var state kscreenState
	if err := json.Unmarshal(data.Bytes(), &state); err != nil {
		return kscreenState{}, errors.New("decode hosted KDE display state")
	}
	return state, nil
}

func kscreenTopologyConnectors(
	outputs []kscreenOutput,
) []topologyConnector {
	connectors := make([]topologyConnector, 0, len(outputs))
	for _, output := range outputs {
		connector := topologyConnector{
			Name: output.Name, Connected: output.Connected,
			Enabled: output.Enabled, CurrentID: output.CurrentModeID,
			Modes: make([]topologyMode, 0, len(output.Modes)),
		}
		for _, mode := range output.Modes {
			connector.Modes = append(connector.Modes, topologyMode{
				ID: mode.ID, Width: mode.Size.Width, Height: mode.Size.Height,
				Current:   mode.ID == output.CurrentModeID,
				RefreshHz: mode.RefreshRate,
				Safe:      topologyIdentifierPattern.MatchString(mode.ID),
			})
		}
		connectors = append(connectors, connector)
	}
	return connectors
}

func kscreenTopologyMatches(
	outputs []kscreenOutput,
	selections []topologySelection,
) bool {
	if len(outputs) != len(selections) {
		return false
	}
	byName := make(map[string]kscreenOutput, len(outputs))
	for _, output := range outputs {
		if _, duplicate := byName[output.Name]; duplicate {
			return false
		}
		byName[output.Name] = output
	}
	for _, selection := range selections {
		output, ok := byName[selection.Connector]
		if !ok ||
			!output.Connected ||
			!output.Enabled ||
			output.CurrentModeID != selection.ModeID ||
			output.Position.X != selection.Output.X ||
			output.Position.Y != selection.Output.Y ||
			output.Size.Width != selection.Output.Width ||
			output.Size.Height != selection.Output.Height ||
			math.Abs(output.Scale-1) > 0.000001 {
			return false
		}
	}
	return true
}

func selectHostedTopology(
	connectors []topologyConnector,
	outputs []HostedOutput,
) ([]topologySelection, error) {
	if len(connectors) != len(outputs) {
		return nil, fmt.Errorf(
			"connected outputs=%d, want %d",
			len(connectors),
			len(outputs),
		)
	}
	slices.SortFunc(connectors, func(first, second topologyConnector) int {
		return strings.Compare(first.Name, second.Name)
	})
	selections := make([]topologySelection, 0, len(outputs))
	seen := make(map[string]struct{}, len(connectors))
	for index, connector := range connectors {
		if !connector.Connected ||
			!topologyIdentifierPattern.MatchString(connector.Name) {
			return nil, errors.New("hosted display connector is invalid")
		}
		if _, duplicate := seen[connector.Name]; duplicate {
			return nil, errors.New("hosted display connector is duplicated")
		}
		seen[connector.Name] = struct{}{}
		mode, err := selectHostedMode(connector.Modes, outputs[index])
		if err != nil {
			return nil, fmt.Errorf(
				"connector %q: %w",
				connector.Name,
				err,
			)
		}
		selections = append(selections, topologySelection{
			Connector: connector.Name,
			ModeID:    mode.ID,
			Output:    outputs[index],
			Primary:   index == 0,
		})
	}
	return selections, nil
}

func selectHostedMode(
	modes []topologyMode,
	output HostedOutput,
) (topologyMode, error) {
	candidates := make([]topologyMode, 0, len(modes))
	for _, mode := range modes {
		if mode.Width == output.Width &&
			mode.Height == output.Height &&
			mode.Safe {
			candidates = append(candidates, mode)
		}
	}
	if len(candidates) == 0 {
		return topologyMode{}, fmt.Errorf(
			"required mode %dx%d is unavailable",
			output.Width,
			output.Height,
		)
	}
	slices.SortFunc(candidates, func(first, second topologyMode) int {
		switch {
		case first.Current != second.Current:
			if first.Current {
				return -1
			}
			return 1
		case first.Preferred != second.Preferred:
			if first.Preferred {
				return -1
			}
			return 1
		case first.RefreshHz != second.RefreshHz:
			if first.RefreshHz > second.RefreshHz {
				return -1
			}
			return 1
		default:
			return strings.Compare(first.ID, second.ID)
		}
	})
	return candidates[0], nil
}

func safeMutterModeID(modeID string) bool {
	if len(modeID) == 0 || len(modeID) > 128 {
		return false
	}
	for _, character := range modeID {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func hostedDisplayBounds(
	outputs []HostedOutput,
) (minX, minY, maxX, maxY int) {
	minX, minY = outputs[0].X, outputs[0].Y
	maxX = outputs[0].X + outputs[0].Width
	maxY = outputs[0].Y + outputs[0].Height
	for _, output := range outputs[1:] {
		minX = min(minX, output.X)
		minY = min(minY, output.Y)
		maxX = max(maxX, output.X+output.Width)
		maxY = max(maxY, output.Y+output.Height)
	}
	return minX, minY, maxX, maxY
}
