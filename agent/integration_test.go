//go:build integration

package agent_test

import (
	"context"
	"image"
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

func TestAgentSessionCaptureRuntime(t *testing.T) {
	if os.Getenv("ROBOTGO_AGENT_CAPTURE_E2E") != "1" {
		t.Skip("set ROBOTGO_AGENT_CAPTURE_E2E=1 to permit a real in-memory desktop capture")
	}
	x := requiredInteger(t, "ROBOTGO_AGENT_CAPTURE_X")
	y := requiredInteger(t, "ROBOTGO_AGENT_CAPTURE_Y")
	width := requiredPositiveBoundedInteger(t, "ROBOTGO_AGENT_CAPTURE_WIDTH", 4096)
	height := requiredPositiveBoundedInteger(t, "ROBOTGO_AGENT_CAPTURE_HEIGHT", 4096)
	displayID := requiredCoordinate(t, "ROBOTGO_AGENT_CAPTURE_DISPLAY")
	session, err := agent.NewSession(agent.Config{Policy: agent.Policy{
		AllowedOperations: []agent.Operation{agent.OperationObserve},
		AllowedDisplayIDs: []int{displayID},
		MaxObservations:   1,
		MaxCapturePixels:  uint64(width) * uint64(height),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	observation, err := session.Observe(context.Background(), agent.ObserveRequest{
		Capture: &agent.CaptureRegion{X: x, Y: y, Width: width, Height: height, DisplayID: displayID},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observation.Close() })
	img, err := observation.Image()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clear(img.Pix) })
	if img.Bounds() != image.Rect(0, 0, width, height) {
		t.Fatalf("capture bounds = %v", img.Bounds())
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

func requiredInteger(t *testing.T, name string) int {
	t.Helper()
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		t.Fatalf("%s must be an integer", name)
	}
	return value
}

func requiredPositiveBoundedInteger(t *testing.T, name string, maximum int) int {
	t.Helper()
	value := requiredInteger(t, name)
	if value <= 0 || value > maximum {
		t.Fatalf("%s must be between 1 and %d", name, maximum)
	}
	return value
}
