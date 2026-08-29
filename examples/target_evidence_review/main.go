//go:build darwin || linux || windows

// Command target_evidence_review demonstrates a non-executable TargetSpec v2
// review using one explicitly selected visual proposal from a self-owned GUI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/marang/robotgo/agent"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	pid := flag.Int("pid", 0, "PID of a self-owned accessible application")
	title := flag.String("title", "", "exact top-level accessible window title")
	name := flag.String("name", "", "expected or former semantic target name")
	roleName := flag.String("role", "button", "semantic target role: button, checkbox, switch, or textbox")
	x := flag.Int("x", 0, "explicit self-owned view region x")
	y := flag.Int("y", 0, "explicit self-owned view region y")
	width := flag.Int("width", 320, "explicit self-owned view region width")
	height := flag.Int("height", 200, "explicit self-owned view region height")
	displayID := flag.Int("display", 0, "explicit display ID")
	item := flag.Uint("item", 0, "reviewed visual proposal index")
	confirm := flag.Bool("confirm", false, "confirm the explicit image and accessibility reads")
	flag.Parse()
	if *pid <= 0 || *title == "" || *name == "" || !*confirm {
		return errors.New("positive -pid, non-empty -title and -name, plus -confirm are required")
	}
	role, err := parseRole(*roleName)
	if err != nil {
		return err
	}
	if *width < 3 || *height < 3 {
		return errors.New("width and height must both be at least 3")
	}
	viewRegion := agent.CaptureRegion{X: *x, Y: *y, Width: *width, Height: *height, DisplayID: *displayID}
	analysisRegion := agent.CaptureRegion{
		X: *x + 1, Y: *y + 1, Width: *width - 2, Height: *height - 2, DisplayID: *displayID,
	}
	policy := agent.Policy{
		AllowedOperations: []agent.Operation{
			agent.OperationView, agent.OperationDetectElements,
			agent.OperationInspectUI, agent.OperationResolveUI,
		},
		ConfirmOperations: []agent.Operation{
			agent.OperationView, agent.OperationDetectElements, agent.OperationInspectUI,
		},
		AllowedDisplayIDs:  []int{*displayID},
		AllowedViewRegions: []agent.CaptureRegion{viewRegion},
		AllowedWindows: []agent.WindowTarget{{
			Target: *pid, Kind: agent.WindowTargetProcess, ExpectedTitle: *title,
		}},
		AllowedUIRoles: []agent.UIRole{
			agent.UIRoleWindow, agent.UIRoleDialog, agent.UIRoleGroup, role,
		},
		AllowedUIProperties: []agent.UIProperty{
			agent.UIPropertyRole, agent.UIPropertyName, agent.UIPropertyState,
			agent.UIPropertyBounds, agent.UIPropertyActions,
		},
		AllowedTargetModes: []agent.TargetResolutionMode{
			agent.TargetResolutionModeStrict, agent.TargetResolutionModeReview,
		},
		AllowedTargetEvidenceSources: []agent.TargetEvidenceSource{agent.TargetEvidenceSourceVisual},
		AllowedTargetEvidenceProviders: []agent.TargetEvidenceProvider{{
			Source:  agent.TargetEvidenceSourceVisual,
			Backend: agent.VisualAnalysisBackend, Model: agent.VisualAnalysisModel,
		}},
		AllowedTargetEvidenceRegions: []agent.CaptureRegion{analysisRegion},
		AdaptiveTargetThreshold:      90, MaxTargetEvidenceAgeMillis: 5_000,
		MinTargetVisualConfidence: 0.5,
		MaxObservations:           2, MaxQueries: 1, MaxUIElements: 256,
		MaxUITreeDepth: 16, MaxUIStringBytes: 64 * 1024, MinUIQueryIntervalMillis: 1,
		MaxViewSourcePixels: uint64(*width) * uint64(*height), MaxViewEncodedBytes: 4 << 20,
		MaxViewWidth: *width, MaxViewHeight: *height, MaxViews: 1,
		MaxConcurrentViews: 1, MinViewIntervalMillis: 1, ViewTimeoutMillis: 5_000,
		MaxAnalysisPixels: uint64(analysisRegion.Width) * uint64(analysisRegion.Height),
		MaxVisualElements: 128, MaxAnalyses: 1, MaxConcurrentAnalyses: 1,
		MinAnalysisIntervalMillis: 1, AnalysisTimeoutMillis: 5_000,
		SessionTimeoutMillis: 15_000,
	}
	session, err := agent.NewSession(agent.Config{Policy: policy})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	view, err := session.View(ctx, agent.ViewRequest{Region: &viewRegion, Confirmed: true})
	if err != nil {
		return err
	}
	defer func() { _ = session.ReleaseObservation(view.Metadata.ObservationID) }()
	visual, err := session.DetectVisualElements(ctx, agent.VisualElementsRequest{
		ObservationID: view.Metadata.ObservationID, Region: analysisRegion,
		MinConfidence: 0.5, Confirmed: true,
	})
	if err != nil {
		return err
	}
	if int(*item) >= len(visual.Elements) {
		return fmt.Errorf("visual proposal index %d is unavailable; detector returned %d proposals", *item, len(visual.Elements))
	}
	ui, err := session.InspectUI(ctx, agent.InspectUIRequest{
		Target: *pid, Kind: agent.WindowTargetProcess, Confirmed: true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = session.ReleaseObservation(ui.ObservationID) }()
	result, err := session.ResolveUITarget(ctx, agent.ResolveUIRequest{
		ObservationID: ui.ObservationID, Mode: agent.TargetResolutionModeReview,
		Target: agent.TargetSpec{
			SchemaVersion: agent.TargetSpecSchemaVersion,
			Window: agent.TargetWindowSpec{
				Target: *pid, Kind: agent.WindowTargetProcess, ExpectedTitle: *title,
			},
			Role: role, Name: *name,
			Evidence: []agent.TargetEvidenceClause{{
				SchemaVersion: agent.TargetEvidenceClauseSchemaVersion,
				ObservationID: view.Metadata.ObservationID,
				EvidenceID:    visual.Metadata.EvidenceID,
				Source:        agent.TargetEvidenceSourceVisual, ItemIndex: uint32(*item),
			}},
		},
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func parseRole(value string) (agent.UIRole, error) {
	switch agent.UIRole(value) {
	case agent.UIRoleButton, agent.UIRoleCheckbox, agent.UIRoleSwitch, agent.UIRoleTextBox:
		return agent.UIRole(value), nil
	default:
		return "", fmt.Errorf("unsupported -role %q", value)
	}
}
