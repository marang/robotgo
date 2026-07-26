//go:build linux

package portalrunner

import (
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

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
