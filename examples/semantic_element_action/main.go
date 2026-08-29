//go:build darwin || linux || windows

// Command semantic_element_action inspects one self-owned accessible window,
// resolves and toggles one uniquely named checkbox or switch, verifies the
// opposite checked state, records the semantic workflow, and returns both the
// privacy-tiered Trace and deterministic Go/MCP flow artifacts. It never falls
// back to pointer or keyboard input.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
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
	toggleName := flag.String("toggle", "", "exact accessible name of the checkbox or switch to toggle")
	toggleRole := flag.String("role", "", "exact semantic role: checkbox or switch")
	confirm := flag.Bool("confirm", false, "confirm the observation-bound semantic toggle")
	traceTier := flag.String("trace-tier", string(agent.TracePrivacyMetadataOnly),
		"trace privacy tier: metadata-only, semantic-redacted, visual-redacted, or full-explicit")
	flag.Parse()
	if *pid <= 0 || *title == "" || *toggleName == "" || *toggleRole == "" {
		return errors.New("positive -pid plus exact non-empty -title, -toggle, and -role are required")
	}
	if !*confirm {
		return errors.New("pass -confirm after verifying the self-owned target and toggle")
	}
	role := agent.UIRole(*toggleRole)
	if role != agent.UIRoleCheckbox && role != agent.UIRoleSwitch {
		return errors.New("-role must be checkbox or switch")
	}
	tier := agent.TracePrivacyTier(*traceTier)
	switch tier {
	case agent.TracePrivacyMetadataOnly, agent.TracePrivacySemanticRedacted,
		agent.TracePrivacyVisualRedacted, agent.TracePrivacyFullExplicit:
	default:
		return errors.New("invalid -trace-tier")
	}
	policy := agent.Policy{
		AllowedOperations: []agent.Operation{
			agent.OperationInspectUI, agent.OperationResolveUI, agent.OperationElementAct,
		},
		ConfirmOperations: []agent.Operation{agent.OperationElementAct},
		AllowedWindows: []agent.WindowTarget{{
			Target: *pid, Kind: agent.WindowTargetProcess, ExpectedTitle: *title,
		}},
		AllowedUIRoles: []agent.UIRole{
			agent.UIRoleWindow, agent.UIRoleDialog, agent.UIRoleGroup,
			agent.UIRoleCheckbox, agent.UIRoleSwitch,
		},
		AllowedUIProperties: []agent.UIProperty{
			agent.UIPropertyRole, agent.UIPropertyName, agent.UIPropertyState,
			agent.UIPropertyBounds, agent.UIPropertyActions, agent.UIPropertyHierarchy,
		},
		AllowedUIActions:             []agent.UIAction{agent.UIActionToggle},
		AllowedTargetModes:           []agent.TargetResolutionMode{agent.TargetResolutionModeStrict},
		RequireCapabilityLease:       true,
		MaxCapabilityLeases:          1,
		MaxCapabilityLeaseMillis:     5_000,
		MaxActions:                   1,
		MaxObservations:              8,
		MaxQueries:                   8,
		MaxUIElements:                256,
		MaxUITreeDepth:               16,
		MaxUIStringBytes:             64 * 1024,
		MinActionIntervalMillis:      50,
		MinUIQueryIntervalMillis:     50,
		UIActionTimeoutMillis:        5_000,
		UIVerificationAttempts:       3,
		UIVerificationIntervalMillis: 50,
		UIVerificationTimeoutMillis:  3_000,
		AllowedTraceTiers:            []agent.TracePrivacyTier{tier},
		MaxTraceEvents:               16,
		MaxTraceBytes:                16 * 1024,
		TraceLifetimeMillis:          5_000,
		AllowRecorder:                true,
		MaxRecorderEvents:            32,
		MaxRecorderBytes:             64 * 1024,
		RecorderLifetimeMillis:       15_000,
		SessionTimeoutMillis:         15_000,
	}
	session, err := agent.NewSession(agent.Config{Policy: policy})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	recorder, err := session.StartRecorder(ctx, agent.RecorderRequest{
		SchemaVersion: agent.SemanticRecorderSchemaVersion,
	})
	if err != nil {
		return err
	}
	defer func() { _ = recorder.Close() }()
	observation, err := session.InspectUI(ctx, agent.InspectUIRequest{
		Target: *pid, Kind: agent.WindowTargetProcess,
	})
	if err != nil {
		return err
	}
	defer func() { _ = session.ReleaseObservation(observation.ObservationID) }()
	matchingStates := []agent.UIState(nil)
	for _, element := range observation.Elements {
		if element.Role == role && element.Name == *toggleName {
			matchingStates = append([]agent.UIState(nil), element.States...)
		}
	}
	desired := agent.UIElementConditionStatePresent
	if slices.Contains(matchingStates, agent.UIStateChecked) {
		desired = agent.UIElementConditionStateAbsent
	}
	postcondition := &agent.UIElementCondition{Kind: desired, State: agent.UIStateChecked}
	resolution, err := session.ResolveUITarget(ctx, agent.ResolveUIRequest{
		ObservationID: observation.ObservationID,
		Target: agent.TargetSpec{
			SchemaVersion: agent.TargetSpecSchemaVersion,
			Window: agent.TargetWindowSpec{
				Target: *pid, Kind: agent.WindowTargetProcess, ExpectedTitle: *title,
			},
			Role: role, Name: *toggleName,
			RequiredStates:  []agent.UIState{agent.UIStateEnabled},
			RequiredActions: []agent.UIAction{agent.UIActionToggle},
		},
		Mode: agent.TargetResolutionModeStrict,
		Lease: &agent.CapabilityLeaseRequest{
			SchemaVersion: agent.CapabilityLeaseSchemaVersion,
			Action:        agent.UIActionToggle, Postcondition: postcondition,
			DurationMillis: 5_000,
		},
	})
	if err != nil {
		return err
	}
	if resolution.Lease == nil {
		return errors.New("semantic target resolution returned no capability lease")
	}
	result, err := session.ActUIElement(ctx, agent.ElementActionRequest{
		CapabilityLeaseID: resolution.Lease.ID,
		Action:            agent.UIActionToggle, Confirmed: true, Postcondition: postcondition,
		Hint:  &agent.RecorderActionHint{Impact: agent.RecorderActionReversible},
		Trace: &agent.TraceRequest{SchemaVersion: agent.TraceRequestSchemaVersion, Tier: tier},
	})
	if err != nil {
		return err
	}
	flow, err := recorder.Stop(ctx)
	if err != nil {
		return err
	}
	artifacts, err := flow.Generate(agent.FlowGenerationRequest{
		PackageName: "recordedflow", FunctionName: "RunVerifiedFlow",
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Result    agent.ActionResult           `json:"result"`
		Flow      agent.RecordedFlow           `json:"flow"`
		Artifacts agent.GeneratedFlowArtifacts `json:"artifacts"`
	}{Result: result, Flow: flow, Artifacts: artifacts})
}
