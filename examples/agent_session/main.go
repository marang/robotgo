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
	operation := flag.String("operation", "move", "move, click, or type")
	x := flag.Int("x", 0, "move destination x")
	y := flag.Int("y", 0, "move destination y")
	displayID := flag.Int("display", 0, "explicit display ID")
	text := flag.String("text", "RobotGo agent session", "text used only with -operation type")
	flag.Parse()

	session, err := agent.NewSession(agent.Config{Policy: agent.Policy{
		AllowedOperations: []agent.Operation{
			agent.OperationMove, agent.OperationClick, agent.OperationTypeText,
		},
		ConfirmOperations: []agent.Operation{
			agent.OperationMove, agent.OperationClick, agent.OperationTypeText,
		},
		AllowedDisplayIDs: []int{*displayID},
		MaxActions:        1, MaxTextRunes: 256,
	}})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	request, err := requestFor(*operation, *x, *y, *displayID, *text)
	if err != nil {
		return err
	}
	var result agent.ActionResult
	if *act {
		request.Confirmed = true
		result, err = session.Execute(context.Background(), request)
	} else {
		// DryRun applies the same validation, policy, quota, and capability
		// preflight but never injects desktop input.
		request.Confirmed = true
		result, err = session.DryRun(context.Background(), request)
	}
	output := struct {
		Catalog agent.OperationCatalog `json:"catalog"`
		Result  agent.ActionResult     `json:"result"`
	}{Catalog: session.Catalog(), Result: result}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(output); encodeErr != nil {
		return encodeErr
	}
	return err
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
