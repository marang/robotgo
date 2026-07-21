package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/marang/robotgo/agent"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	act := flag.Bool("act", false, "perform the action; default is validation-only dry-run")
	operation := flag.String("operation", "move", "observe, move, click, or type")
	x := flag.Int("x", 0, "move destination x")
	y := flag.Int("y", 0, "move destination y")
	displayID := flag.Int("display", 0, "explicit display ID")
	text := flag.String("text", "RobotGo agent session", "text used only with -operation type")
	capture := flag.Bool("capture", false, "capture an in-memory region with -operation observe")
	verify := flag.String("verify", "", "post-action capture proof: changed or unchanged (requires -act)")
	width := flag.Int("width", 320, "capture width")
	height := flag.Int("height", 200, "capture height")
	flag.Parse()

	session, err := agent.NewSession(agent.Config{Policy: agent.Policy{
		AllowedOperations: []agent.Operation{
			agent.OperationObserve, agent.OperationMove, agent.OperationClick, agent.OperationTypeText,
		},
		ConfirmOperations: []agent.Operation{
			agent.OperationObserve, agent.OperationMove, agent.OperationClick, agent.OperationTypeText,
		},
		AllowedDisplayIDs: []int{*displayID},
		MaxActions:        1, MaxTextRunes: 256,
		MaxObservations: 4, MaxCapturePixels: 4 * 1024 * 1024,
		VerificationAttempts: 2, VerificationIntervalMillis: 50,
		VerificationTimeoutMillis: 1000,
	}})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	output := struct {
		Catalog     agent.OperationCatalog `json:"catalog"`
		Observation *agent.Observation     `json:"observation,omitempty"`
		Result      *agent.ActionResult    `json:"result,omitempty"`
	}{Catalog: session.Catalog()}
	if *operation == "observe" {
		if *verify != "" {
			return fmt.Errorf("-verify requires a mutating operation")
		}
		observeRequest := agent.ObserveRequest{Confirmed: true}
		if *capture {
			observeRequest.Capture = &agent.CaptureRegion{
				X: *x, Y: *y, Width: *width, Height: *height, DisplayID: *displayID,
			}
		}
		output.Observation, err = session.Observe(context.Background(), observeRequest)
		if output.Observation != nil {
			defer func() { _ = output.Observation.Close() }()
		}
	} else {
		if *capture {
			return fmt.Errorf("-capture is only valid with -operation observe; use -verify for an action proof")
		}
		request, requestErr := requestFor(*operation, *x, *y, *displayID, *text)
		if requestErr != nil {
			return requestErr
		}
		request.Confirmed = true
		if *verify != "" {
			if !*act {
				return fmt.Errorf("-verify requires -act because dry-run performs no post-action proof")
			}
			condition, conditionErr := verificationCondition(*verify)
			if conditionErr != nil {
				return conditionErr
			}
			output.Observation, err = session.Observe(context.Background(), agent.ObserveRequest{
				Confirmed: true,
				Capture: &agent.CaptureRegion{
					X: *x, Y: *y, Width: *width, Height: *height, DisplayID: *displayID,
				},
			})
			if err != nil {
				return err
			}
			defer func() { _ = output.Observation.Close() }()
			request.Precondition = &agent.ObservationPrecondition{ObservationID: output.Observation.ObservationID}
			request.Verification = &agent.VerificationRequest{Condition: condition}
		}
		var result agent.ActionResult
		if *act {
			result, err = session.Execute(context.Background(), request)
		} else {
			// DryRun applies the same validation, policy, quota, and capability
			// preflight but never injects desktop input.
			result, err = session.DryRun(context.Background(), request)
		}
		output.Result = &result
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(output); encodeErr != nil {
		return encodeErr
	}
	return err
}

func verificationCondition(value string) (agent.VerificationCondition, error) {
	switch value {
	case "changed":
		return agent.VerificationCaptureChanged, nil
	case "unchanged":
		return agent.VerificationCaptureUnchanged, nil
	default:
		return "", fmt.Errorf("unknown verification condition %q", value)
	}
}

func requestFor(operation string, x, y, displayID int, text string) (agent.ActionRequest, error) {
	switch operation {
	case "move":
		return agent.ActionRequest{Operation: agent.OperationMove, Move: &agent.MoveAction{X: x, Y: y, DisplayID: displayID}}, nil
	case "click":
		return agent.ActionRequest{Operation: agent.OperationClick, Click: &agent.ClickAction{Button: agent.MouseButtonLeft}}, nil
	case "type":
		return agent.ActionRequest{Operation: agent.OperationTypeText, TypeText: &agent.TypeTextAction{Text: text}}, nil
	default:
		return agent.ActionRequest{}, fmt.Errorf("unknown operation %q", operation)
	}
}
