// Program verified_flow_planner demonstrates advisory capability planning for
// one semantic resolve-act-verify transaction. It performs no desktop read or
// mutation and intentionally omits a live observation so the report explains
// which runtime evidence must be supplied before execution.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/marang/robotgo/agent"
)

func main() {
	policy := agent.Policy{
		AllowedOperations: []agent.Operation{
			agent.OperationInspectUI, agent.OperationResolveUI, agent.OperationElementAct,
		},
		AllowedWindows: []agent.WindowTarget{{
			Target: 1, Kind: agent.WindowTargetProcess, ExpectedTitle: "self-owned fixture",
		}},
		AllowedUIRoles: []agent.UIRole{agent.UIRoleWindow, agent.UIRoleButton},
		AllowedUIProperties: []agent.UIProperty{
			agent.UIPropertyRole, agent.UIPropertyName, agent.UIPropertyState,
			agent.UIPropertyBounds, agent.UIPropertyActions,
		},
		AllowedUIActions:            []agent.UIAction{agent.UIActionPress},
		AllowedTargetModes:          []agent.TargetResolutionMode{agent.TargetResolutionModeStrict},
		MaxActions:                  1,
		MinActionIntervalMillis:     1,
		SessionTimeoutMillis:        5_000,
		MaxObservations:             8,
		MaxQueries:                  8,
		MaxUIElements:               32,
		MaxUITreeDepth:              8,
		MaxUIStringBytes:            1_024,
		MinUIQueryIntervalMillis:    1,
		UIActionTimeoutMillis:       1_000,
		UIVerificationAttempts:      1,
		UIVerificationTimeoutMillis: 1_000,
		RequireCapabilityLease:      true,
		MaxCapabilityLeases:         1,
		MaxCapabilityLeaseMillis:    1_000,
	}
	session, err := agent.NewSession(agent.Config{Policy: policy})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := session.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, closeErr)
		}
	}()

	target := agent.TargetSpec{
		SchemaVersion: agent.TargetSpecSchemaVersion,
		Window: agent.TargetWindowSpec{
			Target: 1, Kind: agent.WindowTargetProcess, ExpectedTitle: "self-owned fixture",
		},
		Role: agent.UIRoleButton, Name: "Save",
		RequiredStates:  []agent.UIState{agent.UIStateEnabled},
		RequiredActions: []agent.UIAction{agent.UIActionPress},
	}
	report, err := session.PlanVerifiedFlow(context.Background(), agent.VerifiedFlowPlanRequest{
		SchemaVersion: agent.VerifiedFlowPlanSchemaVersion, CatalogVersion: agent.CatalogSchemaVersion,
		TargetSpecVersion: agent.TargetSpecSchemaVersion, CapabilityLeaseVersion: agent.CapabilityLeaseSchemaVersion,
		ActionProofVersion: agent.ActionProofSchemaVersion, TraceVersion: agent.RobotGoTraceSchemaVersion,
		Steps: []agent.VerifiedFlowPlanStep{{
			Target: target, Mode: agent.TargetResolutionModeStrict, Action: agent.UIActionPress,
			Postcondition: &agent.UIElementCondition{
				Kind: agent.UIElementConditionStatePresent, State: agent.UIStateChecked,
			},
			CapabilityLeaseDurationMillis: 1_000,
		}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
