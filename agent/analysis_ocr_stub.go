//go:build !ocr || !cgo

package agent

import (
	"context"
	"image"

	robotgo "github.com/marang/robotgo"
)

const (
	ocrBackendAvailable = false
	ocrBackendName      = ""
	ocrModelName        = ""
)

type unavailableOCRAnalyzer struct{}

func defaultOCRAnalyzer() ocrAnalyzer { return unavailableOCRAnalyzer{} }

func (unavailableOCRAnalyzer) Analyze(context.Context, *image.RGBA, []string) ([]rawOCRBox, error) {
	return nil, robotgo.ErrNotSupported
}
