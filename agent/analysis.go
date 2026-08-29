package agent

import (
	"context"
	"errors"
	"image"
	"math"
	"time"

	robotgo "github.com/marang/robotgo"
)

const (
	// VisualAnalysisBackend identifies RobotGo's deterministic in-memory
	// visual component detector for target-evidence policy allowlists.
	VisualAnalysisBackend = "robotgo-visual"
	// VisualAnalysisModel identifies the fixed visual proposal algorithm.
	VisualAnalysisModel = "contrast-components-v1"
	// OCRAnalysisBackend identifies RobotGo's optional in-memory Tesseract
	// backend for target-evidence policy allowlists.
	OCRAnalysisBackend = "tesseract-memory"
	// OCRAnalysisModel identifies the fixed OCR word-box result contract.
	OCRAnalysisModel = "tesseract-word-boxes-v1"
)

// AnalysisSchemaVersion identifies the OCR and visual-proposal result contract.
const AnalysisSchemaVersion = "2"

var analysisWorkerGate = func() chan struct{} {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return gate
}()

type OCRRequest struct {
	ObservationID string        `json:"observation_id"`
	Region        CaptureRegion `json:"region"`
	Languages     []string      `json:"languages"`
	MinConfidence float64       `json:"min_confidence,omitempty"`
	Confirmed     bool          `json:"confirmed,omitempty"`
}

type VisualElementsRequest struct {
	ObservationID string        `json:"observation_id"`
	Region        CaptureRegion `json:"region"`
	MinConfidence float64       `json:"min_confidence,omitempty"`
	Confirmed     bool          `json:"confirmed,omitempty"`
}

type AnalysisMetadata struct {
	SchemaVersion  string        `json:"schema_version"`
	ObservationID  string        `json:"observation_id"`
	Region         CaptureRegion `json:"region"`
	Backend        string        `json:"backend"`
	Model          string        `json:"model"`
	DurationMillis int64         `json:"duration_ms"`
	Truncated      bool          `json:"truncated,omitempty"`
	Sanitized      bool          `json:"sanitized,omitempty"`
	Untrusted      bool          `json:"untrusted"`
	EvidenceID     string        `json:"evidence_id,omitempty"`
}

type OCRTextBox struct {
	Text       string        `json:"text"`
	Bounds     CaptureRegion `json:"bounds"`
	Confidence float64       `json:"confidence"`
}

type OCRResult struct {
	Metadata AnalysisMetadata `json:"metadata"`
	Boxes    []OCRTextBox     `json:"boxes"`
}

type VisualElementProposal struct {
	Kind       string        `json:"kind"`
	Bounds     CaptureRegion `json:"bounds"`
	Confidence float64       `json:"confidence"`
}

type VisualElementsResult struct {
	Metadata AnalysisMetadata        `json:"metadata"`
	Elements []VisualElementProposal `json:"elements"`
}

type rawOCRBox struct {
	text       []byte
	bounds     image.Rectangle
	confidence float64
	truncated  bool
}

type ocrAnalyzer interface {
	Analyze(context.Context, *image.RGBA, []string) ([]rawOCRBox, error)
}

// OCR extracts bounded, sanitized word boxes from one explicit subregion of a
// live desktop.view observation.
func (s *Session) OCR(ctx context.Context, request OCRRequest) (OCRResult, error) {
	var result OCRResult
	result.Metadata = analysisMetadata(request.ObservationID, request.Region, s.ocrBackend, s.ocrModel)
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.acquire(ctx); err != nil {
		return result, analysisOperationError(OperationOCR, err)
	}
	defer s.release()
	if err := s.ensureOpen(); err != nil {
		return result, analysisOperationError(OperationOCR, err)
	}
	if err := s.authorizeAnalysis(OperationOCR, request.ObservationID, request.Region, request.Confirmed, request.MinConfidence); err != nil {
		return result, err
	}
	if err := s.authorizeOCRLanguages(request.Languages); err != nil {
		return result, err
	}
	capability, ok := s.operationCapability(OperationOCR)
	if !ok || !capability.Available {
		return result, analysisError(OperationOCR, ErrorUnsupported, "in-memory OCR is unavailable", robotgo.ErrNotSupported)
	}
	if !capability.PolicyAllowed {
		return result, analysisError(OperationOCR, ErrorPolicyDenied, "agent policy denied the available OCR backend", ErrPolicyDenied)
	}
	started := s.now()
	analysisCtx, cancel, err := s.beginAnalysis(ctx, OperationOCR, request.ObservationID)
	if err != nil {
		return result, err
	}
	defer cancel()
	img, redacted, err := s.analysisImage(analysisCtx, OperationOCR, request.ObservationID, request.Region)
	if err != nil {
		return result, s.finishAnalysisFailure(ctx, OperationOCR, request.ObservationID, err)
	}
	boxes, err := runOCRAnalyzer(analysisCtx, s.ocrAnalyzer, img, append([]string(nil), request.Languages...))
	if err != nil {
		clearRawOCRBoxes(boxes)
		return result, s.finishAnalysisFailure(ctx, OperationOCR, request.ObservationID, analysisOperationError(OperationOCR, err))
	}
	result.Boxes, result.Metadata.Truncated, result.Metadata.Sanitized = s.projectOCRBoxes(
		boxes, request.Region, request.MinConfidence, redacted,
	)
	clearRawOCRBoxes(boxes)
	result.Metadata.DurationMillis = max(int64(0), s.now().Sub(started).Milliseconds())
	evidence := retainedTargetEvidence{
		source: TargetEvidenceSourceOCR, region: request.Region,
		backend: result.Metadata.Backend, model: result.Metadata.Model,
		languages: append([]string(nil), request.Languages...), createdAt: s.now().UTC(),
		truncated: result.Metadata.Truncated, sanitized: result.Metadata.Sanitized,
		items: make([]retainedTargetEvidenceItem, len(result.Boxes)),
	}
	for index := range result.Boxes {
		evidence.items[index] = retainedTargetEvidenceItem{
			bounds: result.Boxes[index].Bounds, confidence: result.Boxes[index].Confidence,
		}
	}
	evidenceID, err := s.publishTargetEvidence(request.ObservationID, evidence)
	if err != nil {
		return result, s.finishAnalysisFailure(ctx, OperationOCR, request.ObservationID,
			analysisError(OperationOCR, ErrorStaleTarget, "image observation expired before OCR evidence publication", err))
	}
	result.Metadata.EvidenceID = evidenceID
	if err := s.finishAnalysisSuccess(ctx, OperationOCR, request.ObservationID); err != nil {
		s.removeTargetEvidence(request.ObservationID, evidenceID)
		result.Metadata.EvidenceID = ""
		return result, err
	}
	return result, nil
}

// DetectVisualElements returns bounded deterministic geometry proposals from
// one explicit subregion of a live desktop.view observation.
func (s *Session) DetectVisualElements(ctx context.Context, request VisualElementsRequest) (VisualElementsResult, error) {
	var result VisualElementsResult
	result.Metadata = analysisMetadata(request.ObservationID, request.Region, VisualAnalysisBackend, VisualAnalysisModel)
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.acquire(ctx); err != nil {
		return result, analysisOperationError(OperationDetectElements, err)
	}
	defer s.release()
	if err := s.ensureOpen(); err != nil {
		return result, analysisOperationError(OperationDetectElements, err)
	}
	if err := s.authorizeAnalysis(OperationDetectElements, request.ObservationID, request.Region, request.Confirmed, request.MinConfidence); err != nil {
		return result, err
	}
	capability, ok := s.operationCapability(OperationDetectElements)
	if !ok || !capability.Available || !capability.PolicyAllowed {
		return result, analysisError(OperationDetectElements, ErrorPolicyDenied, "agent policy denied visual element detection", ErrPolicyDenied)
	}
	started := s.now()
	analysisCtx, cancel, err := s.beginAnalysis(ctx, OperationDetectElements, request.ObservationID)
	if err != nil {
		return result, err
	}
	defer cancel()
	if err := acquireAnalysisWorker(analysisCtx); err != nil {
		return result, s.finishAnalysisFailure(ctx, OperationDetectElements, request.ObservationID, analysisOperationError(OperationDetectElements, err))
	}
	defer releaseAnalysisWorker()
	img, redacted, err := s.analysisImage(analysisCtx, OperationDetectElements, request.ObservationID, request.Region)
	if err != nil {
		return result, s.finishAnalysisFailure(ctx, OperationDetectElements, request.ObservationID, err)
	}
	defer wipeMutableImage(img)
	elements, truncated, err := detectVisualComponents(
		analysisCtx, img, request.Region, request.MinConfidence, int(s.policy.MaxVisualElements),
	)
	if err != nil {
		return result, s.finishAnalysisFailure(ctx, OperationDetectElements, request.ObservationID, analysisOperationError(OperationDetectElements, err))
	}
	result.Elements = elements
	result.Metadata.Truncated = truncated
	result.Metadata.Sanitized = redacted
	result.Metadata.DurationMillis = max(int64(0), s.now().Sub(started).Milliseconds())
	evidence := retainedTargetEvidence{
		source: TargetEvidenceSourceVisual, region: request.Region,
		backend: result.Metadata.Backend, model: result.Metadata.Model, createdAt: s.now().UTC(),
		truncated: result.Metadata.Truncated, sanitized: result.Metadata.Sanitized,
		items: make([]retainedTargetEvidenceItem, len(result.Elements)),
	}
	for index := range result.Elements {
		evidence.items[index] = retainedTargetEvidenceItem{
			bounds: result.Elements[index].Bounds, confidence: result.Elements[index].Confidence,
			kind: result.Elements[index].Kind,
		}
	}
	evidenceID, err := s.publishTargetEvidence(request.ObservationID, evidence)
	if err != nil {
		return result, s.finishAnalysisFailure(ctx, OperationDetectElements, request.ObservationID,
			analysisError(OperationDetectElements, ErrorStaleTarget, "image observation expired before visual evidence publication", err))
	}
	result.Metadata.EvidenceID = evidenceID
	if err := s.finishAnalysisSuccess(ctx, OperationDetectElements, request.ObservationID); err != nil {
		s.removeTargetEvidence(request.ObservationID, evidenceID)
		result.Metadata.EvidenceID = ""
		return result, err
	}
	return result, nil
}

type ocrWorkerResult struct {
	boxes []rawOCRBox
	err   error
}

func runOCRAnalyzer(ctx context.Context, analyzer ocrAnalyzer, source *image.RGBA, languages []string) ([]rawOCRBox, error) {
	if err := acquireAnalysisWorker(ctx); err != nil {
		wipeMutableImage(source)
		return nil, err
	}
	completed := make(chan ocrWorkerResult)
	go func() {
		defer releaseAnalysisWorker()
		var boxes []rawOCRBox
		var err error
		func() {
			defer wipeMutableImage(source)
			boxes, err = analyzer.Analyze(ctx, source, languages)
		}()
		result := ocrWorkerResult{boxes: boxes, err: err}
		select {
		case completed <- result:
		case <-ctx.Done():
			clearRawOCRBoxes(boxes)
		}
	}()
	select {
	case result := <-completed:
		return result.boxes, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func acquireAnalysisWorker(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-analysisWorkerGate:
		return nil
	}
}

func releaseAnalysisWorker() { analysisWorkerGate <- struct{}{} }

func analysisMetadata(id string, region CaptureRegion, backend, model string) AnalysisMetadata {
	return AnalysisMetadata{
		SchemaVersion: AnalysisSchemaVersion, ObservationID: id, Region: region,
		Backend: backend, Model: model, Untrusted: true,
	}
}

func (s *Session) authorizeAnalysis(operation Operation, id string, region CaptureRegion, confirmed bool, confidence float64) error {
	if _, allowed := s.policy.allowOperation[operation]; !allowed {
		return analysisError(operation, ErrorPolicyDenied, "agent policy denied image analysis", ErrPolicyDenied)
	}
	if _, required := s.policy.requireConfirmation[operation]; required && !confirmed {
		return analysisError(operation, ErrorPolicyDenied, "agent policy requires image analysis confirmation", ErrPolicyDenied)
	}
	if !validObservationID(id) {
		return analysisError(operation, ErrorInvalidInput, "invalid RobotGo observation ID", nil)
	}
	if err := validateCaptureRegion(region, s.policy.MaxAnalysisPixels); err != nil {
		return analysisError(operation, ErrorInvalidInput, "invalid image analysis region", err)
	}
	if math.IsNaN(confidence) || math.IsInf(confidence, 0) || confidence < 0 || confidence > 1 {
		return analysisError(operation, ErrorInvalidInput, "analysis confidence must be between 0 and 1", nil)
	}
	record, ok := s.observation(id)
	if !ok || record.source != OperationView || !record.hasCapture || record.capture == nil || !record.capture.usable() {
		return analysisError(operation, ErrorStaleTarget, "image analysis requires a live desktop.view observation", ErrObservationClosed)
	}
	if !captureRegionContains(record.region, region) {
		return analysisError(operation, ErrorPolicyDenied, "analysis region is outside the authorized image observation", ErrPolicyDenied)
	}
	if region == record.region && !s.policy.AllowFullViewAnalysis {
		return analysisError(operation, ErrorPolicyDenied, "agent policy denied full-view image analysis", ErrPolicyDenied)
	}
	return nil
}

func (s *Session) authorizeOCRLanguages(languages []string) error {
	if len(languages) == 0 || len(languages) > maxAgentAnalysisLanguages {
		return analysisError(OperationOCR, ErrorInvalidInput, "OCR requires a bounded non-empty language list", nil)
	}
	seen := make(map[string]struct{}, len(languages))
	for _, language := range languages {
		if !validOCRLanguage(language) {
			return analysisError(OperationOCR, ErrorInvalidInput, "OCR language is invalid", nil)
		}
		if _, duplicate := seen[language]; duplicate {
			return analysisError(OperationOCR, ErrorInvalidInput, "OCR language is duplicated", nil)
		}
		seen[language] = struct{}{}
		if _, allowed := s.policy.allowOCRLanguage[language]; !allowed {
			return analysisError(OperationOCR, ErrorPolicyDenied, "agent policy denied the OCR language", ErrPolicyDenied)
		}
	}
	return nil
}

func (s *Session) beginAnalysis(ctx context.Context, operation Operation, observationID string) (context.Context, context.CancelFunc, error) {
	if s.usedAnalyses >= s.policy.MaxAnalyses {
		return nil, nil, analysisError(operation, ErrorPolicyDenied, "agent policy image analysis quota reached", ErrPolicyDenied)
	}
	now := s.now()
	if !s.lastAnalysis.IsZero() {
		minimum := time.Duration(s.policy.MinAnalysisIntervalMillis) * time.Millisecond
		elapsed := now.Sub(s.lastAnalysis)
		if elapsed < 0 || elapsed < minimum {
			return nil, nil, analysisError(operation, ErrorPolicyDenied, "agent policy image analysis rate limit reached", ErrPolicyDenied)
		}
	}
	if err := s.emitAudit(ctx, AuditEvent{Kind: AuditObservationStarted, Operation: operation, ObservationID: observationID}); err != nil {
		return nil, nil, analysisError(operation, ErrorAuditDelivery, "audit sink rejected image analysis intent", err)
	}
	s.usedAnalyses++
	s.lastAnalysis = now
	analysisCtx, cancel := context.WithTimeout(ctx, time.Duration(s.policy.AnalysisTimeoutMillis)*time.Millisecond)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	return analysisCtx, func() { stopSessionCancel(); cancel() }, nil
}

func (s *Session) analysisImage(ctx context.Context, operation Operation, id string, region CaptureRegion) (*image.RGBA, bool, error) {
	record, ok := s.observation(id)
	if !ok || record.capture == nil || !record.capture.acquireUse() {
		return nil, false, analysisError(operation, ErrorStaleTarget, "image observation is no longer live", ErrObservationClosed)
	}
	defer record.capture.releaseUse()
	offsetX, offsetY := region.X-record.region.X, region.Y-record.region.Y
	sourceRect := image.Rect(offsetX, offsetY, offsetX+region.Width, offsetY+region.Height)
	if !sourceRect.In(record.capture.pixels.Bounds()) {
		return nil, record.redacted, errors.New("agent: retained image observation bounds are invalid")
	}
	output := image.NewRGBA(image.Rect(0, 0, region.Width, region.Height))
	for y := 0; y < region.Height; y++ {
		if y&63 == 0 {
			if err := ctx.Err(); err != nil {
				wipeMutableImage(output)
				return nil, record.redacted, analysisOperationError(operation, err)
			}
		}
		copy(output.Pix[y*output.Stride:y*output.Stride+region.Width*4],
			record.capture.pixels.Pix[(offsetY+y)*record.capture.pixels.Stride+offsetX*4:(offsetY+y)*record.capture.pixels.Stride+(offsetX+region.Width)*4])
	}
	return output, record.redacted, nil
}

func (s *Session) projectOCRBoxes(boxes []rawOCRBox, region CaptureRegion, minimum float64, redacted bool) ([]OCRTextBox, bool, bool) {
	result := make([]OCRTextBox, 0, min(len(boxes), int(s.policy.MaxOCRBoxes)))
	remaining := int(s.policy.MaxOCRTextBytes)
	truncated, sanitized := false, redacted
	localBounds := image.Rect(0, 0, region.Width, region.Height)
	for _, box := range boxes {
		if box.truncated {
			truncated = true
		}
		if box.confidence < minimum {
			continue
		}
		if math.IsNaN(box.confidence) || math.IsInf(box.confidence, 0) ||
			box.confidence < 0 || box.confidence > 1 || box.bounds.Empty() || !box.bounds.In(localBounds) {
			sanitized = true
			continue
		}
		original := string(box.text)
		text := sanitizeUIText(original)
		if text != original {
			sanitized = true
		}
		if text == "" {
			continue
		}
		if len(result) >= int(s.policy.MaxOCRBoxes) || remaining == 0 {
			truncated = true
			break
		}
		if len(text) > remaining {
			text = truncateUTF8Bytes(text, remaining)
			truncated = true
		}
		remaining -= len(text)
		if text == "" {
			break
		}
		result = append(result, OCRTextBox{
			Text: text,
			Bounds: CaptureRegion{X: region.X + box.bounds.Min.X, Y: region.Y + box.bounds.Min.Y,
				Width: box.bounds.Dx(), Height: box.bounds.Dy(), DisplayID: region.DisplayID},
			Confidence: box.confidence,
		})
	}
	return result, truncated, sanitized
}

func (s *Session) finishAnalysisSuccess(ctx context.Context, operation Operation, observationID string) error {
	if err := s.emitAudit(ctx, AuditEvent{Kind: AuditObservationFinished, Operation: operation, ObservationID: observationID}); err != nil {
		return analysisError(operation, ErrorAuditDelivery, "image analysis completed but audit delivery failed", err)
	}
	return nil
}

func (s *Session) finishAnalysisFailure(ctx context.Context, operation Operation, observationID string, operationErr error) error {
	auditCtx := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		auditCtx, cancel = context.WithTimeout(context.Background(), uiCompletionAuditTimeout)
	}
	defer cancel()
	if auditErr := s.emitAudit(auditCtx, AuditEvent{
		Kind: AuditObservationFinished, Operation: operation, ObservationID: observationID,
		ErrorCode: classifyUIInspectionError(operationErr),
	}); auditErr != nil {
		return analysisError(operation, ErrorAuditDelivery, "image analysis failed and audit delivery failed", errors.Join(operationErr, auditErr))
	}
	return operationErr
}

func analysisError(operation Operation, code ErrorCode, message string, cause error) *ActionError {
	return newActionError(code, operation, message, cause)
}

func analysisOperationError(operation Operation, err error) error {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return &ActionError{Code: actionErr.Code, Operation: operation, Message: actionErr.Message, cause: err}
	}
	code, message := classifyBackendError(err)
	return analysisError(operation, code, message, err)
}

func clearRawOCRBoxes(boxes []rawOCRBox) {
	for index := range boxes {
		clear(boxes[index].text)
		boxes[index].text = nil
	}
	clear(boxes)
}
