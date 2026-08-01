//go:build linux

// Command semantic_ui prints one bounded AT-SPI semantic snapshot for an
// explicitly selected process and exact window title. It writes no files and
// never requests desktop portal consent.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	robotgo "github.com/marang/robotgo"
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
	confirm := flag.Bool("confirm", false, "confirm this bounded semantic read")
	includeValues := flag.Bool("include-values", false, "include non-sensitive control values")
	flag.Parse()

	if *pid <= 0 || *title == "" {
		return errors.New("-pid and an exact non-empty -title are required")
	}
	if !*confirm {
		return errors.New("pass -confirm after verifying the target PID and title")
	}
	capability := robotgo.GetRuntimeCapabilities().Accessibility
	if !capability.Available {
		if capability.Notes != "" {
			return fmt.Errorf("semantic inspection unavailable: %s (%s)", capability.Reason, capability.Notes)
		}
		return fmt.Errorf("semantic inspection unavailable: %s", capability.Reason)
	}

	properties := []agent.UIProperty{
		agent.UIPropertyRole,
		agent.UIPropertyName,
		agent.UIPropertyState,
		agent.UIPropertyBounds,
		agent.UIPropertyFocus,
		agent.UIPropertyActions,
		agent.UIPropertyHierarchy,
	}
	if *includeValues {
		properties = append(properties, agent.UIPropertyValue)
	}
	policy := agent.Policy{
		AllowedOperations:        []agent.Operation{agent.OperationInspectUI},
		ConfirmOperations:        []agent.Operation{agent.OperationInspectUI},
		AllowedWindows:           []agent.WindowTarget{{Target: *pid, Kind: agent.WindowTargetProcess, ExpectedTitle: *title}},
		AllowedUIRoles:           semanticRoles(),
		AllowedUIProperties:      properties,
		MaxQueries:               1,
		MaxObservations:          1,
		MaxUIElements:            256,
		MaxUITreeDepth:           16,
		MaxUIStringBytes:         64 * 1024,
		MinUIQueryIntervalMillis: 100,
		SessionTimeoutMillis:     10_000,
	}
	session, err := agent.NewSession(agent.Config{Policy: policy})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observation, err := session.InspectUI(ctx, agent.InspectUIRequest{
		Target: *pid, Kind: agent.WindowTargetProcess, Confirmed: true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = session.ReleaseObservation(observation.ObservationID) }()
	return json.NewEncoder(os.Stdout).Encode(observation)
}

func semanticRoles() []agent.UIRole {
	return []agent.UIRole{
		agent.UIRoleApplication,
		agent.UIRoleWindow,
		agent.UIRoleDialog,
		agent.UIRoleButton,
		agent.UIRoleCheckbox,
		agent.UIRoleComboBox,
		agent.UIRoleRadio,
		agent.UIRoleSwitch,
		agent.UIRoleTextBox,
		agent.UIRolePassword,
		agent.UIRoleLabel,
		agent.UIRoleLink,
		agent.UIRoleList,
		agent.UIRoleListItem,
		agent.UIRoleMenu,
		agent.UIRoleMenuItem,
		agent.UIRoleTab,
		agent.UIRoleTabPanel,
		agent.UIRoleSlider,
		agent.UIRoleProgress,
		agent.UIRoleImage,
		agent.UIRoleTable,
		agent.UIRoleRow,
		agent.UIRoleCell,
		agent.UIRoleGroup,
		agent.UIRoleGeneric,
	}
}
