package portalrunner

import (
	"strings"
	"testing"
)

func TestValidateHostedStreamEvidence(t *testing.T) {
	t.Parallel()
	display := validManifest().HostedDisplay
	valid := []HostedStreamEvidence{
		{
			NodeID: 1, ID: "stream-a", MappingID: "mapping-a",
			PipeWireSerial: 11, Monitor: true,
			HasPosition: true, HasSize: true,
			X: 0, Y: 0, Width: 1280, Height: 720,
		},
		{
			NodeID: 2, ID: "stream-b", MappingID: "mapping-b",
			PipeWireSerial: 12, Monitor: true,
			HasPosition: true, HasSize: true,
			X: 1280, Y: 0, Width: 1024, Height: 768,
		},
	}
	if err := ValidateHostedStreamEvidence(display, valid); err != nil {
		t.Fatalf("valid stream evidence: %v", err)
	}
	tests := []struct {
		name   string
		change func([]HostedStreamEvidence) []HostedStreamEvidence
		want   string
	}{
		{
			name: "one stream",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				return streams[:1]
			},
			want: "stream count=1",
		},
		{
			name: "ambiguous geometry",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].HasPosition = false
				return streams
			},
			want: "metadata is ambiguous",
		},
		{
			name: "unexpected geometry",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].X = 0
				return streams
			},
			want: "geometry is unexpected",
		},
		{
			name: "duplicate geometry",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].X = streams[0].X
				streams[1].Y = streams[0].Y
				streams[1].Width = streams[0].Width
				streams[1].Height = streams[0].Height
				return streams
			},
			want: "geometry is unexpected",
		},
		{
			name: "missing mapping",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].MappingID = ""
				return streams
			},
			want: "mapping ID is unavailable",
		},
		{
			name: "duplicate mapping",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].MappingID = streams[0].MappingID
				return streams
			},
			want: "mapping ID is duplicated",
		},
		{
			name: "duplicate node",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].NodeID = streams[0].NodeID
				return streams
			},
			want: "node ID is duplicated",
		},
		{
			name: "duplicate serial",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].PipeWireSerial = streams[0].PipeWireSerial
				return streams
			},
			want: "PipeWire serial is duplicated",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			streams := append([]HostedStreamEvidence(nil), valid...)
			err := ValidateHostedStreamEvidence(
				display,
				test.change(streams),
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf(
					"ValidateHostedStreamEvidence() error = %v, want %q",
					err,
					test.want,
				)
			}
			for _, sensitive := range []string{
				"stream-a",
				"stream-b",
				"mapping-a",
				"mapping-b",
			} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatalf(
						"validation error leaked identifier %q: %v",
						sensitive,
						err,
					)
				}
			}
		})
	}
}
