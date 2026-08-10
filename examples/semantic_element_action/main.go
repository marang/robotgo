//go:build darwin || linux || windows

// Command semantic_element_action inspects one self-owned accessible window,
// toggles one uniquely named checkbox or switch, and verifies the opposite
// checked state. It never falls back to pointer or keyboard input.
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
	confirm := flag.Bool("confirm", false, "confirm the observation-bound semantic toggle")
	flag.Parse()
	if *pid <= 0 || *title == "" || *toggleName == "" {
		return errors.New("positive -pid plus exact non-empty -title and -toggle are required")
	}
	if !*confirm {
		return errors.New("pass -confirm after verifying the self-owned target and toggle")
	}
	policy := agent.Policy{
		AllowedOperations: []agent.Operation{agent.OperationInspectUI, agent.OperationElementAct},
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
		MaxActions:                   1,
		MaxObservations:              5,
		MaxQueries:                   5,
		MaxUIElements:                256,
		MaxUITreeDepth:               16,
		MaxUIStringBytes:             64 * 1024,
		MinActionIntervalMillis:      50,
		MinUIQueryIntervalMillis:     50,
		UIActionTimeoutMillis:        5_000,
		UIVerificationAttempts:       3,
		UIVerificationIntervalMillis: 50,
		UIVerificationTimeoutMillis:  3_000,
		SessionTimeoutMillis:         15_000,
	}
	session, err := agent.NewSession(agent.Config{Policy: policy})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	observation, err := session.InspectUI(ctx, agent.InspectUIRequest{
		Target: *pid, Kind: agent.WindowTargetProcess,
	})
	if err != nil {
		return err
	}
	defer func() { _ = session.ReleaseObservation(observation.ObservationID) }()
	var matches []agent.UIElement
	for _, element := range observation.Elements {
		if (element.Role == agent.UIRoleCheckbox || element.Role == agent.UIRoleSwitch) &&
			element.Name == *toggleName && element.Bounds != nil &&
			slices.Contains(element.States, agent.UIStateEnabled) &&
			slices.Contains(element.Actions, agent.UIActionToggle) {
			matches = append(matches, element)
		}
	}
	if len(matches) != 1 {
		return fmt.Errorf("expected exactly one matching checkbox or switch, found %d", len(matches))
	}
	element := matches[0]
	desired := agent.UIElementConditionStatePresent
	if slices.Contains(element.States, agent.UIStateChecked) {
		desired = agent.UIElementConditionStateAbsent
	}
	result, err := session.ActUIElement(ctx, agent.ElementActionRequest{
		ObservationID: observation.ObservationID, ElementID: element.ElementID,
		Action: agent.UIActionToggle, Confirmed: true,
		Expected: agent.UIElementExpectation{
			Role: element.Role, Name: element.Name, Sensitive: element.Sensitive,
			States: element.States, Bounds: element.Bounds, Actions: element.Actions,
		},
		Postcondition: &agent.UIElementCondition{
			Kind: desired, State: agent.UIStateChecked,
		},
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
