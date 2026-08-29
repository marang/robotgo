package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	robotgo "github.com/marang/robotgo"
)

const (
	// ObservationSchemaVersion identifies the serialized observation contract.
	ObservationSchemaVersion  = "1"
	runtimeDiagnosticsBackend = "runtime-diagnostics"
	disablePortalEnv          = "ROBOTGO_DISABLE_PORTAL"
	observationIDPrefix       = "observation-"
	goOSLinux                 = "linux"
)

var (
	observationSerial atomic.Uint64
	// ErrObservationClosed reports access to a zeroed observation buffer.
	ErrObservationClosed = errors.New("agent observation is closed")
)

// CaptureRegion identifies one global logical rectangle on an explicit
// display. Width and Height are positive and bounded by session policy.
type CaptureRegion struct {
	X         int `json:"x"`
	Y         int `json:"y"`
	Width     int `json:"width"`
	Height    int `json:"height"`
	DisplayID int `json:"display_id"`
}

// ObserveRequest asks for sanitized diagnostics and, optionally, an in-memory
// capture. Capture pixels are never included in JSON.
type ObserveRequest struct {
	Confirmed bool           `json:"confirmed,omitempty"`
	Capture   *CaptureRegion `json:"capture,omitempty"`
}

// DiagnosticFeature is a stable, sanitized feature snapshot.
type DiagnosticFeature struct {
	Name        string `json:"name"`
	Available   bool   `json:"available"`
	Fallback    bool   `json:"fallback"`
	Backend     string `json:"backend,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// RuntimeDiagnostics is safe to serialize and never opens desktop consent.
type RuntimeDiagnostics struct {
	GOOS           string              `json:"goos"`
	GOARCH         string              `json:"goarch"`
	CGOEnabled     bool                `json:"cgo_enabled"`
	Implementation string              `json:"implementation"`
	DisplayServer  string              `json:"display_server"`
	Compositor     string              `json:"compositor,omitempty"`
	Features       []DiagnosticFeature `json:"features"`
}

// CaptureMetadata contains bounded, serializable capture lineage. Pixels are
// owned privately by Observation and never form part of this value.
type CaptureMetadata struct {
	Region CaptureRegion `json:"region"`
	SHA256 string        `json:"sha256"`
	Width  int           `json:"width"`
	Height int           `json:"height"`
}

type captureBuffer struct {
	mu     sync.Mutex
	pixels *image.RGBA
	closed bool
}

type capturedFrame struct {
	metadata CaptureMetadata
	buffer   *captureBuffer
}

func (capture *captureBuffer) image() (*image.RGBA, error) {
	if capture == nil {
		return nil, ErrObservationClosed
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.closed || capture.pixels == nil {
		return nil, ErrObservationClosed
	}
	return cloneRGBA(capture.pixels), nil
}

func (capture *captureBuffer) close() error {
	if capture == nil {
		return nil
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.pixels != nil {
		clear(capture.pixels.Pix)
		capture.pixels = nil
	}
	capture.closed = true
	return nil
}

func (capture *captureBuffer) usable() bool {
	if capture == nil {
		return false
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return !capture.closed && capture.pixels != nil
}

func (capture *captureBuffer) acquireUse() bool {
	if capture == nil {
		return false
	}
	capture.mu.Lock()
	if capture.closed || capture.pixels == nil {
		capture.mu.Unlock()
		return false
	}
	return true
}

func (capture *captureBuffer) releaseUse() {
	if capture != nil {
		capture.mu.Unlock()
	}
}

// Observation owns optional sensitive pixels and sanitized diagnostics.
type Observation struct {
	SchemaVersion string             `json:"schema_version"`
	ObservationID string             `json:"observation_id"`
	CreatedAt     time.Time          `json:"created_at"`
	Diagnostics   RuntimeDiagnostics `json:"diagnostics"`
	Capture       *CaptureMetadata   `json:"capture,omitempty"`

	capture *captureBuffer
	source  Operation
}

type observationRecord struct {
	capture                *captureBuffer
	region                 CaptureRegion
	digest                 string
	hasCapture             bool
	source                 Operation
	uiElements             map[string][]byte
	uiExpected             map[string]UIElementExpectation
	uiTree                 []retainedUITarget
	uiTarget               *uiBackendTarget
	uiBackend              string
	uiActionable           bool
	uiResolutionIncomplete bool
	redacted               bool
}

func (record *observationRecord) close() error {
	if record == nil {
		return nil
	}
	closeUIReferences(record.uiElements)
	clear(record.uiExpected)
	record.uiExpected = nil
	clearRetainedUITargets(record.uiTree)
	record.uiTree = nil
	record.uiTarget = nil
	record.uiBackend = ""
	record.uiActionable = false
	record.uiResolutionIncomplete = false
	return record.capture.close()
}

// Close zeroes the optional capture. Sanitized metadata remains readable so
// audit and stale-lineage errors can still identify the closed observation.
func (observation *Observation) Close() error {
	if observation == nil || observation.capture == nil {
		return nil
	}
	return observation.capture.close()
}

// Image returns a defensive copy of the optional sensitive capture.
func (observation *Observation) Image() (*image.RGBA, error) {
	if observation == nil || observation.capture == nil {
		return nil, ErrObservationClosed
	}
	return observation.capture.image()
}

// ObservationPrecondition requires the target capture to remain byte-identical
// immediately before mutation.
type ObservationPrecondition struct {
	ObservationID string `json:"observation_id"`
}

// VerificationCondition identifies a bounded post-action capture predicate.
type VerificationCondition string

const (
	VerificationCaptureChanged   VerificationCondition = "capture-changed"
	VerificationCaptureUnchanged VerificationCondition = "capture-unchanged"
)

// VerificationRequest compares post-action captures with the precondition
// observation. Attempt count and interval come from immutable session policy.
type VerificationRequest struct {
	Condition VerificationCondition `json:"condition"`
}

// VerificationStatus is the machine-readable post-action outcome.
type VerificationStatus string

const (
	VerificationPassed VerificationStatus = "passed"
	VerificationFailed VerificationStatus = "failed"
)

// VerificationResult never includes capture pixels or digests.
type VerificationResult struct {
	Status    VerificationStatus    `json:"status"`
	Condition VerificationCondition `json:"condition"`
	Attempts  uint32                `json:"attempts"`
}

func (robotGoDriver) RuntimeCapabilities() robotgo.RuntimeCapabilities {
	return robotgo.GetRuntimeCapabilities()
}

func (robotGoDriver) Capture(ctx context.Context, region CaptureRegion) (image.Image, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runtime.GOOS == "linux" && robotgo.DetectDisplayServer() == robotgo.DisplayServerWayland {
		return captureWaylandAgent(
			ctx,
			region,
			os.Getenv(disablePortalEnv) != "",
			robotgo.CaptureImgNativeContext,
			robotgo.ScreenCastCaptureReady,
			robotgo.CaptureScreenCastDisplay,
		)
	}
	return captureImageWithContext(ctx, func() (image.Image, error) {
		return robotgo.CaptureImg(region.X, region.Y, region.Width, region.Height, region.DisplayID)
	})
}

type captureImageResult struct {
	image image.Image
	err   error
}

type displayBoundsResult struct {
	bounds displayBounds
	err    error
}

var synchronousDesktopReadSlot = make(chan struct{}, 1)

// captureImageWithContext bounds synchronous native capture APIs that cannot
// accept a context themselves. A late result remains owned by the worker and
// is wiped instead of crossing the canceled operation boundary.
func captureImageWithContext(
	ctx context.Context,
	capture func() (image.Image, error),
) (image.Image, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case synchronousDesktopReadSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if ctx.Done() == nil {
		defer func() { <-synchronousDesktopReadSlot }()
		img, err := capture()
		if err != nil {
			wipeMutableImage(img)
			return nil, err
		}
		return img, nil
	}

	result := make(chan captureImageResult)
	go func() {
		defer func() { <-synchronousDesktopReadSlot }()
		if ctx.Err() != nil {
			return
		}
		img, err := capture()
		select {
		case result <- captureImageResult{image: img, err: err}:
		case <-ctx.Done():
			wipeMutableImage(img)
		}
	}()

	select {
	case captured := <-result:
		if err := ctx.Err(); err != nil {
			wipeMutableImage(captured.image)
			return nil, err
		}
		if captured.err != nil {
			wipeMutableImage(captured.image)
			return nil, captured.err
		}
		return captured.image, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// displayBoundsWithContext applies the same single-flight cancellation
// boundary as synchronous capture. The native lookup may finish after the
// caller returns, but it cannot retain sensitive pixels or start additional
// synchronous desktop reads while it remains blocked.
func displayBoundsWithContext(
	ctx context.Context,
	lookup func() (displayBounds, error),
) (displayBounds, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return displayBounds{}, err
	}
	select {
	case synchronousDesktopReadSlot <- struct{}{}:
	case <-ctx.Done():
		return displayBounds{}, ctx.Err()
	}
	if ctx.Done() == nil {
		defer func() { <-synchronousDesktopReadSlot }()
		return lookup()
	}

	result := make(chan displayBoundsResult)
	go func() {
		defer func() { <-synchronousDesktopReadSlot }()
		if ctx.Err() != nil {
			return
		}
		bounds, err := lookup()
		select {
		case result <- displayBoundsResult{bounds: bounds, err: err}:
		case <-ctx.Done():
		}
	}()

	select {
	case resolved := <-result:
		if err := ctx.Err(); err != nil {
			return displayBounds{}, err
		}
		return resolved.bounds, resolved.err
	case <-ctx.Done():
		return displayBounds{}, ctx.Err()
	}
}

type nativeCaptureFunc func(context.Context, ...int) (image.Image, error)
type screenCastReadyFunc func() error
type screenCastDisplayCaptureFunc func(context.Context, int, ...int) (image.Image, error)

func captureWaylandAgent(
	ctx context.Context,
	region CaptureRegion,
	portalDisabled bool,
	nativeCapture nativeCaptureFunc,
	screenCastReady screenCastReadyFunc,
	screenCastCapture screenCastDisplayCaptureFunc,
) (image.Image, error) {
	img, _, err := captureWaylandAgentWithBackend(
		ctx, region, portalDisabled, nativeCapture, screenCastReady, screenCastCapture,
	)
	return img, err
}

func captureWaylandAgentWithBackend(
	ctx context.Context,
	region CaptureRegion,
	portalDisabled bool,
	nativeCapture nativeCaptureFunc,
	screenCastReady screenCastReadyFunc,
	screenCastCapture screenCastDisplayCaptureFunc,
) (image.Image, string, error) {
	img, nativeErr := nativeCapture(ctx, region.X, region.Y, region.Width, region.Height, region.DisplayID)
	if nativeErr == nil && img != nil && !img.Bounds().Empty() {
		if err := ctx.Err(); err != nil {
			wipeMutableImage(img)
			return nil, "", err
		}
		return img, string(robotgo.BackendScreencopy), nil
	}
	if nativeErr == nil {
		nativeErr = errors.New("native Wayland capture returned an empty image")
	}
	if img != nil {
		wipeMutableImage(img)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	if !portalDisabled && screenCastReady() == nil {
		img, screenCastErr := screenCastCapture(
			ctx,
			region.DisplayID,
			region.X,
			region.Y,
			region.Width,
			region.Height,
		)
		if screenCastErr == nil && img != nil && !img.Bounds().Empty() {
			if err := ctx.Err(); err != nil {
				wipeMutableImage(img)
				return nil, "", err
			}
			return img, string(robotgo.BackendScreenCast), nil
		}
		if screenCastErr == nil {
			screenCastErr = errors.New("ScreenCast capture returned an empty image")
		}
		if img != nil {
			wipeMutableImage(img)
		}
		return nil, "", errors.Join(nativeErr, screenCastErr)
	}

	return nil, "", errors.Join(
		nativeErr,
		fmt.Errorf(
			"%w: native Wayland capture failed and agent capture will not open portal consent implicitly; start ScreenCast explicitly for an authorized fallback",
			robotgo.ErrNotSupported,
		),
	)
}

// Observe returns a diagnostics snapshot and optional bounded capture. It is
// serialized with actions so an observation cannot race a session mutation.
func (s *Session) Observe(ctx context.Context, request ObserveRequest) (*Observation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.release()
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}
	if err := s.authorizeObservation(request); err != nil {
		return nil, err
	}
	if s.usedObservations >= s.policy.MaxObservations {
		return nil, observationActionError(ErrorPolicyDenied, "agent policy observation limit reached", ErrPolicyDenied)
	}
	if err := s.emitAudit(ctx, AuditEvent{Kind: AuditObservationStarted, Operation: OperationObserve}); err != nil {
		return nil, observationActionError(ErrorAuditDelivery, "audit sink rejected observation intent", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, observationContextError(ctx)
	}
	if err := s.ensureOpen(); err != nil {
		return nil, err
	}

	diagnostics, err := s.runtimeDiagnostics(ctx)
	if err != nil {
		_ = s.emitAudit(ctx, AuditEvent{Kind: AuditObservationFinished, Operation: OperationObserve, ErrorCode: classifyObservationError(err)})
		return nil, err
	}
	observation := &Observation{
		SchemaVersion: ObservationSchemaVersion,
		ObservationID: newObservationID(),
		CreatedAt:     time.Now().UTC(),
		Diagnostics:   diagnostics,
	}
	if request.Capture != nil {
		capture, err := s.capture(ctx, *request.Capture, true)
		if err != nil {
			_ = s.emitAudit(ctx, AuditEvent{Kind: AuditObservationFinished, Operation: OperationObserve, ErrorCode: classifyObservationError(err)})
			return nil, err
		}
		observation.Capture = &capture.metadata
		observation.capture = capture.buffer
	} else {
		s.usedObservations++
	}
	s.storeObservation(observation)
	if err := s.emitAudit(ctx, AuditEvent{
		Kind: AuditObservationFinished, Operation: OperationObserve,
		ObservationID: observation.ObservationID,
	}); err != nil {
		return observation, observationActionError(ErrorAuditDelivery, "observation completed but audit delivery failed", err)
	}
	return observation, nil
}

func (s *Session) runtimeDiagnostics(ctx context.Context) (RuntimeDiagnostics, error) {
	diagnostics := diagnosticsFromCapabilities(s.driver.RuntimeCapabilities())
	if err := ctx.Err(); err != nil {
		return RuntimeDiagnostics{}, observationContextError(ctx)
	}
	if err := s.ensureOpen(); err != nil {
		return RuntimeDiagnostics{}, err
	}
	return diagnostics, nil
}

func newObservationID() string {
	return observationIDPrefix + strconv.FormatUint(observationSerial.Add(1), 10)
}

func validObservationID(id string) bool {
	digits := strings.TrimPrefix(id, observationIDPrefix)
	if digits == id || digits == "" || len(digits) > 20 || digits[0] == '0' {
		return false
	}
	value, err := strconv.ParseUint(digits, 10, 64)
	return err == nil && value != 0 && strconv.FormatUint(value, 10) == digits
}

func (s *Session) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return observationContextError(ctx)
	case <-s.ctx.Done():
		return observationActionError(ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
	case <-s.actionGate:
		return nil
	}
}

func (s *Session) release() { s.actionGate <- struct{}{} }

func (s *Session) ensureOpen() error {
	select {
	case <-s.ctx.Done():
		return observationActionError(ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
	default:
		return nil
	}
}

func (s *Session) authorizeObservation(request ObserveRequest) error {
	if _, allowed := s.policy.allowOperation[OperationObserve]; !allowed {
		return observationActionError(ErrorPolicyDenied, "agent policy denied desktop observation", ErrPolicyDenied)
	}
	if _, required := s.policy.requireConfirmation[OperationObserve]; required && !request.Confirmed {
		return observationActionError(ErrorPolicyDenied, "agent policy requires observation confirmation", ErrPolicyDenied)
	}
	if request.Capture == nil {
		return nil
	}
	if s.policy.MaxCapturePixels == 0 {
		return observationActionError(ErrorPolicyDenied, "agent policy denies desktop capture", ErrPolicyDenied)
	}
	if _, allowed := s.policy.allowDisplay[request.Capture.DisplayID]; !allowed {
		return observationActionError(ErrorPolicyDenied, "agent policy denied the capture display", ErrPolicyDenied)
	}
	return nil
}

func (s *Session) capture(ctx context.Context, region CaptureRegion, count bool) (*capturedFrame, error) {
	return s.captureWith(ctx, region, s.policy.MaxCapturePixels, count, s.driver.Capture)
}

func (s *Session) captureWith(
	ctx context.Context,
	region CaptureRegion,
	maxPixels uint64,
	count bool,
	capture func(context.Context, CaptureRegion) (image.Image, error),
) (*capturedFrame, error) {
	if err := validateCaptureRegion(region, maxPixels); err != nil {
		return nil, observationActionError(ErrorInvalidInput, "invalid capture region", err)
	}
	if _, allowed := s.policy.allowDisplay[region.DisplayID]; !allowed {
		return nil, observationActionError(ErrorPolicyDenied, "agent policy denied the capture display", ErrPolicyDenied)
	}
	if err := ctx.Err(); err != nil {
		return nil, observationContextError(ctx)
	}
	bounds, err := displayBoundsWithContext(ctx, func() (displayBounds, error) {
		return s.driver.DisplayBounds(region.DisplayID)
	})
	if err != nil {
		code, message := classifyBackendError(err)
		return nil, observationActionError(code, message, err)
	}
	if !bounds.containsRegion(region) {
		return nil, observationActionError(ErrorPolicyDenied, "capture region is outside the allowed display", ErrPolicyDenied)
	}
	if err := ctx.Err(); err != nil {
		return nil, observationContextError(ctx)
	}
	if count {
		if s.usedObservations >= s.policy.MaxObservations {
			return nil, observationActionError(ErrorPolicyDenied, "agent policy observation limit reached", ErrPolicyDenied)
		}
		s.usedObservations++
	}
	img, err := capture(ctx, region)
	if err != nil {
		wipeMutableImage(img)
		code, message := classifyBackendError(err)
		return nil, observationActionError(code, message, err)
	}
	defer wipeMutableImage(img)
	frame, err := newCaptureObservation(region, img, maxPixels)
	if err != nil {
		return nil, observationActionError(ErrorBackendFailure, "desktop backend returned an invalid capture", err)
	}
	if err := ctx.Err(); err != nil {
		_ = frame.buffer.close()
		return nil, observationContextError(ctx)
	}
	return frame, nil
}

func validateCaptureRegion(region CaptureRegion, maxPixels uint64) error {
	if region.DisplayID < 0 {
		return errors.New("capture requires a non-negative display ID")
	}
	if region.Width <= 0 || region.Height <= 0 {
		return errors.New("capture width and height must be positive")
	}
	maximum := int(^uint(0) >> 1)
	if region.X > maximum-region.Width || region.Y > maximum-region.Height {
		return errors.New("capture coordinates overflow")
	}
	width, height := uint64(region.Width), uint64(region.Height)
	if maxPixels == 0 || width > maxPixels/height {
		return fmt.Errorf("capture exceeds the %d pixel policy limit", maxPixels)
	}
	return nil
}

func (b displayBounds) containsRegion(region CaptureRegion) bool {
	return containsSpan(region.X, region.Width, b.x, b.width) &&
		containsSpan(region.Y, region.Height, b.y, b.height)
}

func containsSpan(start, size, minimum, extent int) bool {
	if size <= 0 || extent <= 0 || start < minimum {
		return false
	}
	offset := uint(start) - uint(minimum)
	return offset <= uint(extent) && uint(size) <= uint(extent)-offset
}

func newCaptureObservation(region CaptureRegion, source image.Image, maxPixels uint64) (*capturedFrame, error) {
	if source == nil {
		return nil, errors.New("capture image is nil")
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width != region.Width || height != region.Height {
		return nil, fmt.Errorf("capture size %dx%d does not match requested %dx%d", width, height, region.Width, region.Height)
	}
	if err := validateCaptureRegion(CaptureRegion{Width: width, Height: height, DisplayID: region.DisplayID}, maxPixels); err != nil {
		return nil, err
	}
	pixels := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(pixels, pixels.Bounds(), source, bounds.Min, draw.Src)
	digest := sha256.Sum256(pixels.Pix)
	return &capturedFrame{
		metadata: CaptureMetadata{
			Region: region, SHA256: hex.EncodeToString(digest[:]),
			Width: width, Height: height,
		},
		buffer: &captureBuffer{pixels: pixels},
	}, nil
}

func cloneRGBA(source *image.RGBA) *image.RGBA {
	clone := image.NewRGBA(source.Bounds())
	copy(clone.Pix, source.Pix)
	return clone
}

func wipeMutableImage(img image.Image) {
	switch typed := img.(type) {
	case *image.RGBA:
		clear(typed.Pix)
	case *image.NRGBA:
		clear(typed.Pix)
	case *image.RGBA64:
		clear(typed.Pix)
	case *image.NRGBA64:
		clear(typed.Pix)
	case *image.Gray:
		clear(typed.Pix)
	case *image.Gray16:
		clear(typed.Pix)
	case *image.Alpha:
		clear(typed.Pix)
	case *image.Alpha16:
		clear(typed.Pix)
	case *image.CMYK:
		clear(typed.Pix)
	case *image.Paletted:
		clear(typed.Pix)
	case *image.YCbCr:
		clear(typed.Y)
		clear(typed.Cb)
		clear(typed.Cr)
	case *image.NYCbCrA:
		clear(typed.Y)
		clear(typed.Cb)
		clear(typed.Cr)
		clear(typed.A)
	}
}

func (s *Session) storeObservation(observation *Observation) {
	record := observationRecord{capture: observation.capture, source: observation.source}
	if record.source == "" {
		record.source = OperationObserve
	}
	if observation.Capture != nil {
		record.region = observation.Capture.Region
		record.digest = observation.Capture.SHA256
		record.hasCapture = true
	}
	s.observationMu.Lock()
	s.observations[observation.ObservationID] = record
	s.observationMu.Unlock()
}

func (s *Session) observation(id string) (observationRecord, bool) {
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	record, ok := s.observations[id]
	return record, ok
}

// ReleaseObservation idempotently removes one valid observation from the
// session and zeroes its optional capture. It can release observations returned
// directly by Observe or referenced by wait/verification results.
func (s *Session) ReleaseObservation(id string) error {
	if !validObservationID(id) {
		return observationActionError(ErrorInvalidInput, "invalid RobotGo observation ID", nil)
	}
	s.releaseView(id)
	s.invalidateObservationLeases(id)
	s.observationMu.Lock()
	record, ok := s.observations[id]
	if ok {
		delete(s.observations, id)
	}
	s.observationMu.Unlock()
	if !ok {
		return nil
	}
	return record.close()
}

func (s *Session) closeObservations() {
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	for _, record := range s.observations {
		_ = record.close()
	}
	clear(s.observations)
}

func diagnosticsFromCapabilities(capabilities robotgo.RuntimeCapabilities) RuntimeDiagnostics {
	features := []struct {
		name  string
		value robotgo.FeatureCapability
	}{
		{"capture", capabilities.Capture}, {"bounds", capabilities.Bounds},
		{"keyboard", capabilities.Keyboard}, {"mouse", capabilities.Mouse},
		{"remote-desktop", capabilities.RemoteDesktop}, {"window", capabilities.Window},
		{"accessibility", capabilities.Accessibility},
		{"process", capabilities.Process}, {"clipboard", capabilities.Clipboard},
		{"hook", capabilities.Hook}, {"events", capabilities.Events},
	}
	diagnostics := RuntimeDiagnostics{
		GOOS: capabilities.Runtime.GOOS, GOARCH: capabilities.Runtime.GOARCH,
		CGOEnabled:     capabilities.Runtime.CGOEnabled,
		Implementation: string(capabilities.Runtime.BuildImplementation),
		DisplayServer:  string(capabilities.Runtime.DisplayServer),
		Compositor:     capabilities.Compositor,
		Features:       make([]DiagnosticFeature, 0, len(features)),
	}
	for _, feature := range features {
		remediation := feature.value.Notes
		if remediation == "" {
			remediation = feature.value.Reason
		}
		diagnostics.Features = append(diagnostics.Features, DiagnosticFeature{
			Name: feature.name, Available: feature.value.Available,
			Fallback: feature.value.Fallback, Backend: feature.value.Backend,
			Reason: feature.value.Reason, Remediation: remediation,
		})
	}
	return diagnostics
}

func observationActionError(code ErrorCode, message string, cause error) *ActionError {
	return newActionError(code, OperationObserve, message, cause)
}

func observationContextError(ctx context.Context) *ActionError {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return observationActionError(ErrorTimedOut, "observation deadline exceeded", ctx.Err())
	}
	return observationActionError(ErrorCanceled, "observation canceled", ctx.Err())
}

func classifyObservationError(err error) ErrorCode {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return actionErr.Code
	}
	code, _ := classifyBackendError(err)
	return code
}
