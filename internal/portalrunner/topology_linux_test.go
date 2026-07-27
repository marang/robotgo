//go:build linux

package portalrunner

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestInitialKScreenStateRetriesTransientCommandFailure(t *testing.T) {
	t.Parallel()
	want := kscreenState{Outputs: []kscreenOutput{{Name: "Virtual-1"}}}
	calls := 0
	state, err := waitForInitialKScreenState(
		context.Background(),
		0,
		func(context.Context) (kscreenState, error) {
			calls++
			if calls < 3 {
				return kscreenState{}, newHostedDisplayFailure(
					hostedDisplayStageKDEStateRun,
				)
			}
			return want, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(state.Outputs) != 1 ||
		state.Outputs[0].Name != want.Outputs[0].Name {
		t.Fatalf("initial KScreen state = %#v after %d calls", state, calls)
	}
}

func TestInitialKScreenStatePreservesFinalFailureStage(t *testing.T) {
	t.Parallel()
	calls := 0
	state, err := waitForInitialKScreenState(
		context.Background(),
		0,
		func(context.Context) (kscreenState, error) {
			calls++
			return kscreenState{}, newHostedDisplayFailure(
				hostedDisplayStageKDEStateJSON,
			)
		},
	)
	if len(state.Outputs) != 0 {
		t.Fatalf("failed initial KScreen state = %#v", state)
	}
	if stage := HostedDisplayFailureStage(err); stage !=
		hostedDisplayStageKDEStateJSON {
		t.Fatalf("initial KScreen failure stage = %q", stage)
	}
	if calls != 1 {
		t.Fatalf("deterministic KScreen failure was retried %d times", calls)
	}
}

func TestKScreenApplyRetriesTransientCommandFailure(t *testing.T) {
	t.Parallel()
	calls := 0
	err := waitForKScreenApply(
		context.Background(),
		0,
		[]string{"output.Virtual-1.enable"},
		func(_ context.Context, arguments []string) error {
			calls++
			if len(arguments) != 1 ||
				arguments[0] != "output.Virtual-1.enable" {
				t.Fatalf("KScreen apply arguments = %v", arguments)
			}
			if calls < 3 {
				return newHostedDisplayFailure(
					hostedDisplayStageKDEApply,
				)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("KScreen apply calls = %d", calls)
	}
}

func TestKScreenApplyPreservesNonApplyFailure(t *testing.T) {
	t.Parallel()
	calls := 0
	err := waitForKScreenApply(
		context.Background(),
		0,
		nil,
		func(context.Context, []string) error {
			calls++
			return newHostedDisplayFailure(hostedDisplayStageKDEPlan)
		},
	)
	if stage := HostedDisplayFailureStage(err); stage !=
		hostedDisplayStageKDEPlan {
		t.Fatalf("KScreen apply failure stage = %q", stage)
	}
	if calls != 1 {
		t.Fatalf("non-apply KScreen failure was retried %d times", calls)
	}
}

func TestKScreenSettleRetriesTransientCommandFailure(t *testing.T) {
	t.Parallel()
	selection := topologySelection{
		Connector: "Virtual-1",
		ModeID:    "1280x720@60",
		Output: HostedOutput{
			Width: 1280, Height: 720,
		},
	}
	calls := 0
	err := waitForKScreenSettle(
		context.Background(),
		0,
		[]topologySelection{selection},
		func(context.Context) (kscreenState, error) {
			calls++
			if calls < 3 {
				return kscreenState{}, newHostedDisplayFailure(
					hostedDisplayStageKDEStateRun,
				)
			}
			return kscreenState{Outputs: []kscreenOutput{{
				Name:          selection.Connector,
				Scale:         1,
				Size:          kscreenSize{Width: 1280, Height: 720},
				CurrentModeID: selection.ModeID,
				Connected:     true,
				Enabled:       true,
			}}}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("KScreen settle calls = %d", calls)
	}
}

func TestKScreenSettlePreservesDeterministicStateFailure(t *testing.T) {
	t.Parallel()
	calls := 0
	err := waitForKScreenSettle(
		context.Background(),
		0,
		nil,
		func(context.Context) (kscreenState, error) {
			calls++
			return kscreenState{}, newHostedDisplayFailure(
				hostedDisplayStageKDEStateJSON,
			)
		},
	)
	if stage := HostedDisplayFailureStage(err); stage !=
		hostedDisplayStageKDEStateJSON {
		t.Fatalf("KScreen settle failure stage = %q", stage)
	}
	if calls != 1 {
		t.Fatalf("deterministic KScreen settle failure was retried %d times", calls)
	}
}

func TestHostedDisplayFailureMarkerIsPrivacySafe(t *testing.T) {
	t.Parallel()
	failure := errors.Join(
		newHostedDisplayFailure(hostedDisplayStageGNOMEApply),
		errors.New("private connector and mode"),
	)
	if stage := HostedDisplayFailureStage(failure); stage !=
		hostedDisplayStageGNOMEApply {
		t.Fatalf("hosted display failure stage = %q", stage)
	}
	var output bytes.Buffer
	if err := WriteHostedDisplayFailureMarker(
		&output,
		failure,
	); err != nil {
		t.Fatal(err)
	}
	if output.String() !=
		hostedDisplayFailureMarker+hostedDisplayStageGNOMEApply+"\n" {
		t.Fatalf("hosted display failure marker = %q", output.String())
	}
	if strings.Contains(output.String(), "private") {
		t.Fatal("hosted display failure marker leaked the wrapped cause")
	}
	if err := WriteHostedDisplayFailureMarker(
		&output,
		errors.New("unclassified"),
	); err == nil {
		t.Fatal("unclassified display failure emitted a marker")
	}
	if err := WriteHostedDisplayFailureMarker(
		nil,
		failure,
	); err == nil {
		t.Fatal("nil display failure output was accepted")
	}
}

func TestSelectHostedTopologyIsDeterministicAndExact(t *testing.T) {
	t.Parallel()
	outputs := validManifest().HostedDisplay.Outputs
	connectors := []topologyConnector{
		{
			Name: "Virtual-2", Connected: true,
			Modes: []topologyMode{
				{
					ID: "4", Width: 1024, Height: 768,
					RefreshHz: 60, Safe: true,
				},
				{
					ID: "3", Width: 1024, Height: 768,
					Preferred: true, RefreshHz: 60, Safe: true,
				},
			},
		},
		{
			Name: "Virtual-1", Connected: true,
			Modes: []topologyMode{
				{
					ID: "2", Width: 1280, Height: 720,
					Preferred: true, RefreshHz: 75, Safe: true,
				},
				{
					ID: "1", Width: 1280, Height: 720,
					Current: true, RefreshHz: 60, Safe: true,
				},
			},
		},
	}
	selections, err := selectHostedTopology(connectors, outputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(selections) != 2 {
		t.Fatalf("selections = %#v", selections)
	}
	if selections[0] != (topologySelection{
		Connector: "Virtual-1",
		ModeID:    "1",
		Output:    outputs[0],
		Primary:   true,
	}) {
		t.Fatalf("primary selection = %#v", selections[0])
	}
	if selections[1] != (topologySelection{
		Connector: "Virtual-2",
		ModeID:    "3",
		Output:    outputs[1],
	}) {
		t.Fatalf("secondary selection = %#v", selections[1])
	}
}

func TestSelectHostedTopologyRejectsAmbiguousRuntimeState(t *testing.T) {
	t.Parallel()
	outputs := validManifest().HostedDisplay.Outputs
	validMode := func(width, height int) topologyMode {
		return topologyMode{
			ID: "1", Width: width, Height: height, Safe: true,
		}
	}
	tests := []struct {
		name       string
		connectors []topologyConnector
		want       string
	}{
		{
			name: "only one output",
			connectors: []topologyConnector{{
				Name: "Virtual-1", Connected: true,
				Modes: []topologyMode{validMode(1280, 720)},
			}},
			want: "connected outputs=1",
		},
		{
			name: "disconnected output",
			connectors: []topologyConnector{
				{
					Name: "Virtual-1", Connected: true,
					Modes: []topologyMode{validMode(1280, 720)},
				},
				{
					Name: "Virtual-2", Connected: false,
					Modes: []topologyMode{validMode(1024, 768)},
				},
			},
			want: "connector is invalid",
		},
		{
			name: "duplicate connector",
			connectors: []topologyConnector{
				{
					Name: "Virtual-1", Connected: true,
					Modes: []topologyMode{validMode(1280, 720)},
				},
				{
					Name: "Virtual-1", Connected: true,
					Modes: []topologyMode{validMode(1024, 768)},
				},
			},
			want: "duplicated",
		},
		{
			name: "unsafe connector",
			connectors: []topologyConnector{
				{
					Name: "Invalid.Name", Connected: true,
					Modes: []topologyMode{validMode(1280, 720)},
				},
				{
					Name: "Virtual-2", Connected: true,
					Modes: []topologyMode{validMode(1024, 768)},
				},
			},
			want: "connector is invalid",
		},
		{
			name: "required mode unavailable",
			connectors: []topologyConnector{
				{
					Name: "Virtual-1", Connected: true,
					Modes: []topologyMode{validMode(1920, 1080)},
				},
				{
					Name: "Virtual-2", Connected: true,
					Modes: []topologyMode{validMode(1024, 768)},
				},
			},
			want: "required mode 1280x720 is unavailable",
		},
		{
			name: "unsafe mode identifier",
			connectors: []topologyConnector{
				{
					Name: "Virtual-1", Connected: true,
					Modes: []topologyMode{{
						ID: "1.2", Width: 1280, Height: 720,
					}},
				},
				{
					Name: "Virtual-2", Connected: true,
					Modes: []topologyMode{validMode(1024, 768)},
				},
			},
			want: "required mode 1280x720 is unavailable",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := selectHostedTopology(test.connectors, outputs)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf(
					"selectHostedTopology() error = %v, want %q",
					err,
					test.want,
				)
			}
		})
	}
}

func TestMutterTopologyMatchesExactTwoOutputs(t *testing.T) {
	t.Parallel()
	display := validManifest().HostedDisplay
	selections := []topologySelection{
		{
			Connector: "Virtual-1", ModeID: "1280x720@60.000",
			Output: display.Outputs[0], Primary: true,
		},
		{
			Connector: "Virtual-2", ModeID: "1024x768@60.000",
			Output: display.Outputs[1],
		},
	}
	current := func(id string, width, height int32) mutterMode {
		return mutterMode{
			ID: id, Width: width, Height: height,
			Properties: map[string]dbus.Variant{
				"is-current": dbus.MakeVariant(true),
			},
		}
	}
	state := mutterDisplayState{
		Monitors: []mutterMonitor{
			{
				Identity: mutterMonitorIdentity{Connector: "Virtual-1"},
				Modes: []mutterMode{
					current("1280x720@60.000", 1280, 720),
				},
			},
			{
				Identity: mutterMonitorIdentity{Connector: "Virtual-2"},
				Modes: []mutterMode{
					current("1024x768@60.000", 1024, 768),
				},
			},
		},
		LogicalMonitors: []mutterLogicalMonitor{
			{
				X: 0, Y: 0, Scale: 1, Primary: true,
				Monitors: []mutterMonitorIdentity{{
					Connector: "Virtual-1",
				}},
			},
			{
				X: 1280, Y: 0, Scale: 1,
				Monitors: []mutterMonitorIdentity{{
					Connector: "Virtual-2",
				}},
			},
		},
	}
	if !mutterTopologyMatches(state, selections) {
		t.Fatal("exact Mutter topology was rejected")
	}
	state.LogicalMonitors[1].X = 0
	if mutterTopologyMatches(state, selections) {
		t.Fatal("overlapping Mutter topology was accepted")
	}
}

func TestMutterApplyPropertiesOnlyChangesSupportedLayoutMode(t *testing.T) {
	t.Parallel()
	properties, err := mutterApplyProperties(map[string]dbus.Variant{
		"layout-mode": dbus.MakeVariant(uint32(2)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(properties) != 0 {
		t.Fatalf("unsupported Mutter apply properties = %#v", properties)
	}

	properties, err = mutterApplyProperties(map[string]dbus.Variant{
		"supports-changing-layout-mode": dbus.MakeVariant(true),
		"layout-mode":                   dbus.MakeVariant(uint32(2)),
	})
	if err != nil {
		t.Fatal(err)
	}
	var mode uint32
	if err := properties["layout-mode"].Store(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != 2 {
		t.Fatalf("Mutter apply layout mode = %d, want 2", mode)
	}

	for _, invalid := range []map[string]dbus.Variant{
		{
			"supports-changing-layout-mode": dbus.MakeVariant(true),
		},
		{
			"supports-changing-layout-mode": dbus.MakeVariant(true),
			"layout-mode":                   dbus.MakeVariant(uint32(3)),
		},
	} {
		if _, err := mutterApplyProperties(invalid); err == nil {
			t.Fatalf("invalid Mutter apply properties accepted: %#v", invalid)
		}
	}
}

func TestKScreenTopologyMatchesExactTwoOutputs(t *testing.T) {
	t.Parallel()
	display := validManifest().HostedDisplay
	selections := []topologySelection{
		{
			Connector: "Virtual-1", ModeID: "1",
			Output: display.Outputs[0], Primary: true,
		},
		{
			Connector: "Virtual-2", ModeID: "2",
			Output: display.Outputs[1],
		},
	}
	outputs := []kscreenOutput{
		{
			Name: "Virtual-1", Connected: true, Enabled: true,
			CurrentModeID: "1", Scale: 1,
			Position: kscreenPoint{X: 0, Y: 0},
			Size:     kscreenSize{Width: 1280, Height: 720},
		},
		{
			Name: "Virtual-2", Connected: true, Enabled: true,
			CurrentModeID: "2", Scale: 1,
			Position: kscreenPoint{X: 1280, Y: 0},
			Size:     kscreenSize{Width: 1024, Height: 768},
		},
	}
	if !kscreenTopologyMatches(outputs, selections) {
		t.Fatal("exact KScreen topology was rejected")
	}
	outputs[1].Size.Width = 1280
	if kscreenTopologyMatches(outputs, selections) {
		t.Fatal("wrong KScreen geometry was accepted")
	}
}

func TestSafeMutterModeIDAllowsDBusOnlyModeSyntax(t *testing.T) {
	t.Parallel()
	if !safeMutterModeID("1280x720@60.000") {
		t.Fatal("normal Mutter mode ID was rejected")
	}
	for _, value := range []string{"", "private\nvalue", strings.Repeat("x", 129)} {
		if safeMutterModeID(value) {
			t.Fatalf("unsafe Mutter mode ID %q was accepted", value)
		}
	}
}
