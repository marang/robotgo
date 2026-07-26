package portalrunner

import (
	"errors"
	"fmt"
)

// HostedStreamEvidence contains only the transient metadata required to prove
// one selected monitor stream. Callers must not log or persist the values.
type HostedStreamEvidence struct {
	NodeID         uint32
	ID             string
	MappingID      string
	PipeWireSerial uint64
	Monitor        bool
	HasPosition    bool
	HasSize        bool
	X              int
	Y              int
	Width          int
	Height         int
}

type hostedStreamEvidenceFailure struct {
	stage string
	err   error
}

func (failure *hostedStreamEvidenceFailure) Error() string {
	return failure.err.Error()
}

func (failure *hostedStreamEvidenceFailure) Unwrap() error {
	return failure.err
}

func newHostedStreamEvidenceFailure(stage string, err error) error {
	return &hostedStreamEvidenceFailure{stage: stage, err: err}
}

// HostedStreamEvidenceFailureStage returns a bounded, privacy-safe validation
// category without exposing transient portal identifiers.
func HostedStreamEvidenceFailureStage(err error) string {
	var failure *hostedStreamEvidenceFailure
	if errors.As(err, &failure) {
		return failure.stage
	}
	return ""
}

// ValidateHostedStreamEvidence proves an exact one-to-one mapping between the
// manifest topology and transient portal streams without returning identifiers
// in errors.
func ValidateHostedStreamEvidence(
	display HostedDisplay,
	streams []HostedStreamEvidence,
) error {
	if err := display.Validate(); err != nil {
		return err
	}
	if len(streams) != len(display.Outputs) {
		return newHostedStreamEvidenceFailure(
			"count",
			fmt.Errorf(
				"portal stream count=%d, want %d",
				len(streams),
				len(display.Outputs),
			),
		)
	}
	expectedGeometry := make(map[HostedOutput]struct{}, len(display.Outputs))
	for _, output := range display.Outputs {
		expectedGeometry[output] = struct{}{}
	}
	nodes := make(map[uint32]struct{}, len(streams))
	identifiers := make(map[string]struct{}, len(streams))
	mappings := make(map[string]struct{}, len(streams))
	serials := make(map[uint64]struct{}, len(streams))
	for _, stream := range streams {
		if !stream.Monitor || !stream.HasPosition || !stream.HasSize {
			return newHostedStreamEvidenceFailure(
				"metadata",
				errors.New("portal monitor stream metadata is ambiguous"),
			)
		}
		geometry := HostedOutput{
			X: stream.X, Y: stream.Y,
			Width: stream.Width, Height: stream.Height,
		}
		if _, ok := expectedGeometry[geometry]; !ok {
			return newHostedStreamEvidenceFailure(
				"geometry",
				errors.New("portal monitor stream geometry is unexpected"),
			)
		}
		delete(expectedGeometry, geometry)
		if stream.NodeID == 0 {
			return newHostedStreamEvidenceFailure(
				"node",
				errors.New("portal monitor stream node ID is unavailable"),
			)
		}
		if _, duplicate := nodes[stream.NodeID]; duplicate {
			return newHostedStreamEvidenceFailure(
				"node",
				errors.New("portal monitor stream node ID is duplicated"),
			)
		}
		nodes[stream.NodeID] = struct{}{}
		if stream.ID == "" {
			return newHostedStreamEvidenceFailure(
				"stable-id",
				errors.New("portal monitor stream stable ID is unavailable"),
			)
		}
		if _, duplicate := identifiers[stream.ID]; duplicate {
			return newHostedStreamEvidenceFailure(
				"stable-id",
				errors.New("portal monitor stream stable ID is duplicated"),
			)
		}
		identifiers[stream.ID] = struct{}{}
		if stream.MappingID == "" {
			return newHostedStreamEvidenceFailure(
				"mapping-id",
				errors.New("portal monitor stream mapping ID is unavailable"),
			)
		}
		if _, duplicate := mappings[stream.MappingID]; duplicate {
			return newHostedStreamEvidenceFailure(
				"mapping-id",
				errors.New("portal monitor stream mapping ID is duplicated"),
			)
		}
		mappings[stream.MappingID] = struct{}{}
		if stream.PipeWireSerial == 0 {
			return newHostedStreamEvidenceFailure(
				"pipewire-serial",
				errors.New(
					"portal monitor stream PipeWire serial is unavailable",
				),
			)
		}
		if _, duplicate := serials[stream.PipeWireSerial]; duplicate {
			return newHostedStreamEvidenceFailure(
				"pipewire-serial",
				errors.New(
					"portal monitor stream PipeWire serial is duplicated",
				),
			)
		}
		serials[stream.PipeWireSerial] = struct{}{}
	}
	if len(expectedGeometry) != 0 {
		return newHostedStreamEvidenceFailure(
			"geometry",
			errors.New("portal monitor stream geometry is incomplete"),
		)
	}
	return nil
}
