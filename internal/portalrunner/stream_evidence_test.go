package portalrunner

import (
	"errors"
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
	if err := ValidateHostedStreamEvidence(display, 6, valid); err != nil {
		t.Fatalf("valid stream evidence: %v", err)
	}
	tests := []struct {
		name      string
		version   uint32
		change    func([]HostedStreamEvidence) []HostedStreamEvidence
		want      string
		wantStage string
	}{
		{
			name: "one stream",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				return streams[:1]
			},
			want: "stream count=1", wantStage: "count",
		},
		{
			name:    "non-monitor source",
			version: 3,
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].Monitor = false
				return streams
			},
			want: "not identified as a monitor", wantStage: "metadata",
		},
		{
			name: "unexpected geometry",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].X = 0
				return streams
			},
			want: "geometry is unexpected", wantStage: "geometry",
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
			want: "geometry is unexpected", wantStage: "geometry",
		},
		{
			name: "duplicate mapping",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].MappingID = streams[0].MappingID
				return streams
			},
			want: "mapping ID is duplicated", wantStage: "mapping-id",
		},
		{
			name: "duplicate node",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].NodeID = streams[0].NodeID
				return streams
			},
			want: "node ID is duplicated", wantStage: "node",
		},
		{
			name:    "missing serial in version 6",
			version: 6,
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].PipeWireSerial = 0
				return streams
			},
			want: "PipeWire serial is unavailable", wantStage: "pipewire-serial",
		},
		{
			name: "duplicate serial",
			change: func(streams []HostedStreamEvidence) []HostedStreamEvidence {
				streams[1].PipeWireSerial = streams[0].PipeWireSerial
				return streams
			},
			want: "PipeWire serial is duplicated", wantStage: "pipewire-serial",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			streams := append([]HostedStreamEvidence(nil), valid...)
			version := test.version
			if version == 0 {
				version = 6
			}
			err := ValidateHostedStreamEvidence(
				display,
				version,
				test.change(streams),
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf(
					"ValidateHostedStreamEvidence() error = %v, want %q",
					err,
					test.want,
				)
			}
			if stage := HostedStreamEvidenceFailureStage(err); stage != test.wantStage {
				t.Fatalf(
					"HostedStreamEvidenceFailureStage() = %q, want %q",
					stage,
					test.wantStage,
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
	for _, test := range []struct {
		name    string
		version uint32
		change  func([]HostedStreamEvidence)
	}{
		{
			name:    "version 5 without PipeWire serial",
			version: 5,
			change: func(streams []HostedStreamEvidence) {
				for index := range streams {
					streams[index].PipeWireSerial = 0
				}
			},
		},
		{
			name:    "optional identifiers omitted",
			version: 6,
			change: func(streams []HostedStreamEvidence) {
				for index := range streams {
					streams[index].ID = ""
					streams[index].MappingID = ""
				}
			},
		},
		{
			name:    "optional geometry omitted",
			version: 6,
			change: func(streams []HostedStreamEvidence) {
				for index := range streams {
					streams[index].HasPosition = false
					streams[index].HasSize = false
					streams[index].X = 0
					streams[index].Y = 0
					streams[index].Width = 0
					streams[index].Height = 0
				}
			},
		},
		{
			name:    "optional position omitted",
			version: 6,
			change: func(streams []HostedStreamEvidence) {
				streams[1].HasPosition = false
				streams[1].X = 0
				streams[1].Y = 0
			},
		},
		{
			name:    "optional size omitted",
			version: 6,
			change: func(streams []HostedStreamEvidence) {
				streams[1].HasSize = false
				streams[1].Width = 0
				streams[1].Height = 0
			},
		},
		{
			name:    "version 2 without source type",
			version: 2,
			change: func(streams []HostedStreamEvidence) {
				for index := range streams {
					streams[index].Monitor = false
				}
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			streams := append([]HostedStreamEvidence(nil), valid...)
			test.change(streams)
			if err := ValidateHostedStreamEvidence(
				display,
				test.version,
				streams,
			); err != nil {
				t.Fatalf("ValidateHostedStreamEvidence() error = %v", err)
			}
		})
	}
	if stage := HostedStreamEvidenceFailureStage(
		errors.New("unclassified"),
	); stage != "" {
		t.Fatalf("unclassified stream evidence stage = %q", stage)
	}
}
