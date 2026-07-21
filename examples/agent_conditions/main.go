package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
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
	allowCapture := flag.Bool("allow-capture", false, "explicitly permit in-memory capture; no image is written to disk")
	mode := flag.String("mode", "find", "condition mode: find or wait")
	x := flag.Int("x", 0, "global logical capture x")
	y := flag.Int("y", 0, "global logical capture y")
	width := flag.Int("width", 320, "capture width")
	height := flag.Int("height", 200, "capture height")
	displayID := flag.Int("display", 0, "explicit display ID")
	red := flag.Uint("red", 0, "target red channel (0-255)")
	green := flag.Uint("green", 0, "target green channel (0-255)")
	blue := flag.Uint("blue", 0, "target blue channel (0-255)")
	tolerance := flag.Float64("tolerance", 0, "normalized RGB tolerance (0-1)")
	flag.Parse()

	condition, err := colorCondition(*red, *green, *blue, *tolerance)
	if err != nil {
		return err
	}
	region := agent.CaptureRegion{
		X: *x, Y: *y, Width: *width, Height: *height, DisplayID: *displayID,
	}
	session, err := agent.NewSession(agent.Config{Policy: agent.Policy{
		AllowedOperations: []agent.Operation{
			agent.OperationObserve, agent.OperationFindColor, agent.OperationWaitColor,
		},
		ConfirmOperations: []agent.Operation{
			agent.OperationObserve, agent.OperationFindColor, agent.OperationWaitColor,
		},
		AllowedDisplayIDs:  []int{*displayID},
		MaxObservations:    11,
		MaxCapturePixels:   4 * 1024 * 1024,
		MaxQueries:         2,
		WaitAttempts:       10,
		WaitIntervalMillis: 100,
		WaitTimeoutMillis:  5000,
	}})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	output := struct {
		Catalog agent.OperationCatalog `json:"catalog"`
		Note    string                 `json:"note,omitempty"`
		Find    *agent.FindColorResult `json:"find,omitempty"`
		Wait    *agent.WaitColorResult `json:"wait,omitempty"`
	}{Catalog: session.Catalog()}
	if !*allowCapture {
		output.Note = "inspection only; pass -allow-capture to evaluate the explicit region in memory"
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	switch *mode {
	case "find":
		observation, observeErr := session.Observe(context.Background(), agent.ObserveRequest{
			Confirmed: true,
			Capture:   &region,
		})
		if observeErr != nil {
			return observeErr
		}
		defer func() { _ = observation.Close() }()
		result, findErr := session.FindColor(context.Background(), agent.FindColorRequest{
			ObservationID: observation.ObservationID,
			Condition:     condition,
			Confirmed:     true,
		})
		output.Find = &result
		err = findErr
	case "wait":
		result, waitErr := session.WaitColor(context.Background(), agent.WaitColorRequest{
			Region: region, Condition: condition, Confirmed: true,
		})
		if result.ObservationID != "" {
			defer func() { _ = session.ReleaseObservation(result.ObservationID) }()
		}
		output.Wait = &result
		err = waitErr
	default:
		return fmt.Errorf("unknown mode %q", *mode)
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(output); encodeErr != nil {
		return encodeErr
	}
	return err
}

func colorCondition(red, green, blue uint, tolerance float64) (agent.ColorCondition, error) {
	if red > 255 || green > 255 || blue > 255 {
		return agent.ColorCondition{}, fmt.Errorf("RGB channels must be between 0 and 255")
	}
	if math.IsNaN(tolerance) || math.IsInf(tolerance, 0) || tolerance < 0 || tolerance > 1 {
		return agent.ColorCondition{}, fmt.Errorf("tolerance must be between 0 and 1")
	}
	return agent.ColorCondition{
		Red: uint8(red), Green: uint8(green), Blue: uint8(blue), Tolerance: tolerance,
	}, nil
}
