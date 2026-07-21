//go:build integration

package agent_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/marang/robotgo/agent"
)

func TestAgentSessionMoveRuntime(t *testing.T) {
	if os.Getenv("ROBOTGO_AGENT_INPUT_E2E") != "1" {
		t.Skip("set ROBOTGO_AGENT_INPUT_E2E=1 to permit real pointer movement")
	}
	x := requiredCoordinate(t, "ROBOTGO_AGENT_INPUT_X")
	y := requiredCoordinate(t, "ROBOTGO_AGENT_INPUT_Y")
	displayID := requiredCoordinate(t, "ROBOTGO_AGENT_INPUT_DISPLAY")
	session, err := agent.NewSession(agent.Config{Policy: agent.Policy{
		AllowedOperations: []agent.Operation{agent.OperationMove},
		AllowedDisplayIDs: []int{displayID},
		MaxActions:        1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	request := agent.ActionRequest{
		Operation: agent.OperationMove,
		Move:      &agent.MoveAction{X: x, Y: y, DisplayID: displayID},
	}
	if _, err := session.DryRun(context.Background(), request); err != nil {
		t.Fatalf("dry-run preflight: %v", err)
	}
	if _, err := session.Execute(context.Background(), request); err != nil {
		t.Fatalf("runtime move: %v", err)
	}
}

func requiredCoordinate(t *testing.T, name string) int {
	t.Helper()
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 0 {
		t.Fatalf("%s must be a non-negative integer", name)
	}
	return value
}
