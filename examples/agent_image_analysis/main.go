// Command agent_image_analysis demonstrates explicit, observation-bound local
// OCR or visual element proposals for a self-owned desktop region.
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
	mode := flag.String("mode", "detect", "analysis mode: detect or ocr (ocr requires an ocr+CGO build)")
	x := flag.Int("x", 0, "self-owned view region x")
	y := flag.Int("y", 0, "self-owned view region y")
	width := flag.Int("width", 320, "self-owned view region width")
	height := flag.Int("height", 200, "self-owned view region height")
	displayID := flag.Int("display", 0, "explicit display ID")
	language := flag.String("language", "eng", "allow-listed Tesseract language for OCR")
	confirm := flag.Bool("confirm", false, "confirm this explicit sensitive desktop read")
	flag.Parse()
	if !*confirm {
		return fmt.Errorf("-confirm is required for this sensitive desktop read")
	}
	if *width < 3 || *height < 3 {
		return fmt.Errorf("width and height must both be at least 3")
	}
	viewRegion := agent.CaptureRegion{X: *x, Y: *y, Width: *width, Height: *height, DisplayID: *displayID}
	analysisRegion := agent.CaptureRegion{
		X: *x + 1, Y: *y + 1, Width: *width - 2, Height: *height - 2, DisplayID: *displayID,
	}
	operation := agent.OperationDetectElements
	if *mode == "ocr" {
		operation = agent.OperationOCR
	} else if *mode != "detect" {
		return fmt.Errorf("unknown mode %q", *mode)
	}
	policy := agent.Policy{
		AllowedOperations: []agent.Operation{agent.OperationView, operation},
		ConfirmOperations: []agent.Operation{agent.OperationView, operation},
		AllowedDisplayIDs: []int{*displayID}, AllowedViewRegions: []agent.CaptureRegion{viewRegion},
		MaxObservations: 2, SessionTimeoutMillis: 30_000,
		MaxViewSourcePixels: uint64(*width) * uint64(*height), MaxViewEncodedBytes: 4 << 20,
		MaxViewWidth: *width, MaxViewHeight: *height, MaxViews: 1,
		MaxConcurrentViews: 1, MinViewIntervalMillis: 1, ViewTimeoutMillis: 10_000,
		MaxAnalysisPixels: uint64(analysisRegion.Width) * uint64(analysisRegion.Height),
		MaxAnalyses:       1, MaxConcurrentAnalyses: 1,
		MinAnalysisIntervalMillis: 1, AnalysisTimeoutMillis: 10_000,
		MaxVisualElements: 128,
	}
	if operation == agent.OperationOCR {
		policy.AllowedOCRLanguages = []string{*language}
		policy.MaxOCRBoxes = 128
		policy.MaxOCRTextBytes = 16 << 10
	}
	session, err := agent.NewSession(agent.Config{Policy: policy})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	view, err := session.View(context.Background(), agent.ViewRequest{Region: &viewRegion, Confirmed: true})
	if err != nil {
		return err
	}
	encoded, err := view.TakePNG()
	if err != nil {
		return err
	}
	clear(encoded)
	defer func() { _ = session.ReleaseObservation(view.Metadata.ObservationID) }()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if operation == agent.OperationOCR {
		result, analysisErr := session.OCR(context.Background(), agent.OCRRequest{
			ObservationID: view.Metadata.ObservationID, Region: analysisRegion,
			Languages: []string{*language}, MinConfidence: 0.5, Confirmed: true,
		})
		if analysisErr != nil {
			return analysisErr
		}
		return encoder.Encode(result)
	}
	result, err := session.DetectVisualElements(context.Background(), agent.VisualElementsRequest{
		ObservationID: view.Metadata.ObservationID, Region: analysisRegion,
		MinConfidence: 0.5, Confirmed: true,
	})
	if err != nil {
		return err
	}
	return encoder.Encode(result)
}
