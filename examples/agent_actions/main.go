// Command agent_actions demonstrates the extended, policy-gated RobotGo agent
// actions. It is dry-run-only unless -act is supplied.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/marang/robotgo/agent"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (returnErr error) {
	act := flag.Bool("act", false, "execute the action; default is dry-run")
	confirm := flag.Bool("confirm", false, "confirm the selected action")
	operationName := flag.String("operation", "scroll", "scroll, drag, chord, or activate")
	displayID := flag.Int("display", 0, "explicit display ID")
	x := flag.Int("x", 0, "scroll target or drag start x")
	y := flag.Int("y", 0, "scroll target or drag start y")
	endX := flag.Int("end-x", 10, "drag end x")
	endY := flag.Int("end-y", 10, "drag end y")
	deltaX := flag.Int("delta-x", 0, "horizontal scroll delta per event")
	deltaY := flag.Int("delta-y", 1, "vertical scroll delta per event")
	events := flag.Uint("events", 1, "scroll event count")
	duration := flag.Int("duration-ms", 100, "drag duration in milliseconds")
	key := flag.String("key", "c", "canonical chord key")
	modifiers := flag.String("modifiers", "control", "comma-separated alt,control,meta,shift modifiers")
	windowTarget := flag.Int("window-target", 0, "positive process ID or native handle")
	windowKind := flag.String("window-kind", "process", "process or handle")
	windowTitle := flag.String("window-title", "", "exact expected live title for activation")
	flag.Parse()

	operation, request, policy, err := actionConfiguration(actionFlags{
		operationName: *operationName,
		displayID:     *displayID,
		x:             *x,
		y:             *y,
		endX:          *endX,
		endY:          *endY,
		deltaX:        *deltaX,
		deltaY:        *deltaY,
		events:        *events,
		duration:      *duration,
		key:           *key,
		modifiers:     *modifiers,
		windowTarget:  *windowTarget,
		windowKind:    *windowKind,
		windowTitle:   *windowTitle,
	})
	if err != nil {
		return err
	}
	request.Operation = operation
	request.Confirmed = *confirm
	policy.ConfirmOperations = []agent.Operation{operation}

	session, err := agent.NewSession(agent.Config{Policy: policy})
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, session.Close())
	}()

	var result agent.ActionResult
	if *act {
		result, err = session.Execute(context.Background(), request)
	} else {
		result, err = session.DryRun(context.Background(), request)
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(struct {
		Catalog agent.OperationCatalog `json:"catalog"`
		Result  agent.ActionResult     `json:"result"`
	}{Catalog: session.Catalog(), Result: result}); encodeErr != nil {
		return errors.Join(err, encodeErr)
	}
	return err
}

type actionFlags struct {
	operationName string
	displayID     int
	x             int
	y             int
	endX          int
	endY          int
	deltaX        int
	deltaY        int
	events        uint
	duration      int
	key           string
	modifiers     string
	windowTarget  int
	windowKind    string
	windowTitle   string
}

func actionConfiguration(flags actionFlags) (
	agent.Operation,
	agent.ActionRequest,
	agent.Policy,
	error,
) {
	if flags.events > uint(^uint32(0)) {
		return "", agent.ActionRequest{}, agent.Policy{},
			fmt.Errorf("scroll event count exceeds uint32")
	}
	policy := agent.Policy{
		MaxActions:              1,
		MinActionIntervalMillis: 100,
		SessionTimeoutMillis:    60_000,
	}
	switch flags.operationName {
	case "scroll":
		policy.AllowedOperations = []agent.Operation{agent.OperationScroll}
		policy.AllowedDisplayIDs = []int{flags.displayID}
		policy.MaxScrollEvents = uint32(flags.events)
		policy.MaxScrollDistance = 10_000
		return agent.OperationScroll, agent.ActionRequest{
			Scroll: &agent.ScrollAction{
				TargetX: flags.x, TargetY: flags.y,
				DeltaX: flags.deltaX, DeltaY: flags.deltaY,
				Events: uint32(flags.events), DisplayID: flags.displayID,
			},
		}, policy, nil
	case "drag":
		policy.AllowedOperations = []agent.Operation{agent.OperationDrag}
		policy.AllowedDisplayIDs = []int{flags.displayID}
		policy.AllowedMouseButtons = []agent.MouseButton{agent.MouseButtonLeft}
		policy.MaxDragDistance = 10_000
		policy.MaxDragDurationMillis = flags.duration
		return agent.OperationDrag, agent.ActionRequest{
			Drag: &agent.DragAction{
				StartX: flags.x, StartY: flags.y,
				EndX: flags.endX, EndY: flags.endY,
				DisplayID: flags.displayID, Button: agent.MouseButtonLeft,
				DurationMillis: flags.duration,
			},
		}, policy, nil
	case "chord":
		chordModifiers, err := parseModifiers(flags.modifiers)
		if err != nil {
			return "", agent.ActionRequest{}, agent.Policy{}, err
		}
		policy.AllowedOperations = []agent.Operation{agent.OperationKeyChord}
		policy.AllowedKeys = []string{flags.key}
		policy.AllowedModifiers = append([]agent.KeyModifier(nil), chordModifiers...)
		policy.AllowedWindows = []agent.WindowTarget{{
			Target: flags.windowTarget, Kind: agent.WindowTargetProcess,
			ExpectedTitle: flags.windowTitle,
		}}
		policy.MaxChordKeys = uint32(len(chordModifiers) + 1)
		return agent.OperationKeyChord, agent.ActionRequest{
			KeyChord: &agent.KeyChordAction{
				Key: flags.key, Modifiers: chordModifiers, TargetPID: flags.windowTarget,
			},
		}, policy, nil
	case "activate":
		kind, err := parseWindowKind(flags.windowKind)
		if err != nil {
			return "", agent.ActionRequest{}, agent.Policy{}, err
		}
		policy.AllowedOperations = []agent.Operation{agent.OperationActivate}
		policy.AllowedWindows = []agent.WindowTarget{{
			Target: flags.windowTarget, Kind: kind, ExpectedTitle: flags.windowTitle,
		}}
		return agent.OperationActivate, agent.ActionRequest{
			Activate: &agent.ActivateWindowAction{Target: flags.windowTarget, Kind: kind},
		}, policy, nil
	default:
		return "", agent.ActionRequest{}, agent.Policy{},
			fmt.Errorf("unknown operation %q", flags.operationName)
	}
}

func parseModifiers(value string) ([]agent.KeyModifier, error) {
	if value == "" {
		return nil, nil
	}
	fields := strings.Split(value, ",")
	result := make([]agent.KeyModifier, 0, len(fields))
	for _, field := range fields {
		modifier := agent.KeyModifier(field)
		switch modifier {
		case agent.KeyModifierAlt, agent.KeyModifierControl,
			agent.KeyModifierMeta, agent.KeyModifierShift:
			result = append(result, modifier)
		default:
			return nil, fmt.Errorf("unsupported canonical modifier %q", field)
		}
	}
	return result, nil
}

func parseWindowKind(value string) (agent.WindowTargetKind, error) {
	kind := agent.WindowTargetKind(value)
	switch kind {
	case agent.WindowTargetProcess, agent.WindowTargetHandle:
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported window target kind %q", value)
	}
}
