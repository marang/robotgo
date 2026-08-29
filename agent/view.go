package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	robotgo "github.com/marang/robotgo"
)

const (
	ViewSchemaVersion = "1"
	ViewMIMEType      = "image/png"
)

var errViewEncodedLimit = errors.New("agent: encoded view exceeds policy limit")

// ViewRequest selects either one explicitly allow-listed region or one whole
// display that has a separate full-display policy grant. Exactly one selector
// must be present.
type ViewRequest struct {
	Region        *CaptureRegion `json:"region,omitempty"`
	FullDisplayID *int           `json:"full_display_id,omitempty"`
	Confirmed     bool           `json:"confirmed,omitempty"`
}

// ViewMetadata is the sanitized structured portion of an image observation.
// It deliberately excludes pixels, encoded bytes, and the private digest.
type ViewMetadata struct {
	SchemaVersion         string        `json:"schema_version"`
	ObservationID         string        `json:"observation_id"`
	CreatedAt             time.Time     `json:"created_at"`
	Region                CaptureRegion `json:"region"`
	Width                 int           `json:"width"`
	Height                int           `json:"height"`
	Backend               string        `json:"backend"`
	MIMEType              string        `json:"mime_type"`
	CaptureDurationMillis int64         `json:"capture_duration_ms"`
	Downscaled            bool          `json:"downscaled,omitempty"`
	Redacted              bool          `json:"redacted,omitempty"`
}

// View owns one encoded, metadata-free image until TakePNG transfers that
// ownership to the caller. The retained observation capture remains separately
// session-owned until ReleaseObservation or Session.Close.
type View struct {
	Metadata ViewMetadata `json:"metadata"`

	mu     sync.Mutex
	data   []byte
	closed bool
	done   func(*View)
}

// NewImageView adopts one metadata-free PNG produced by a custom Session
// implementation. The caller relinquishes the byte slice on success. Normal
// RobotGo callers should use Session.View instead.
func NewImageView(metadata ViewMetadata, encodedPNG []byte) (*View, error) {
	if metadata.SchemaVersion != ViewSchemaVersion ||
		!validObservationID(metadata.ObservationID) || metadata.Backend == "" ||
		len(metadata.Backend) > 128 || sanitizeUIText(metadata.Backend) != metadata.Backend ||
		metadata.MIMEType != ViewMIMEType || metadata.Width <= 0 || metadata.Height <= 0 ||
		metadata.Width > maxAgentViewDimension || metadata.Height > maxAgentViewDimension ||
		metadata.Width > metadata.Region.Width || metadata.Height > metadata.Region.Height ||
		metadata.Downscaled != (metadata.Width != metadata.Region.Width || metadata.Height != metadata.Region.Height) ||
		metadata.CreatedAt.IsZero() || metadata.CaptureDurationMillis < 0 ||
		metadata.CaptureDurationMillis > maxAgentViewTimeoutMS ||
		len(encodedPNG) == 0 || len(encodedPNG) > maxAgentViewEncodedBytes {
		return nil, errors.New("agent: invalid image view envelope")
	}
	if err := validateCaptureRegion(metadata.Region, maxAgentCapturePixels); err != nil {
		return nil, errors.New("agent: invalid image view region")
	}
	if err := validateMetadataFreeViewPNG(encodedPNG); err != nil {
		return nil, errors.New("agent: image view PNG contains invalid or unsupported chunks")
	}
	configuration, err := png.DecodeConfig(bytes.NewReader(encodedPNG))
	if err != nil || configuration.Width != metadata.Width || configuration.Height != metadata.Height {
		return nil, errors.New("agent: image view PNG does not match its metadata")
	}
	decoded, err := png.Decode(bytes.NewReader(encodedPNG))
	if err != nil {
		return nil, errors.New("agent: image view PNG does not match its metadata")
	}
	defer wipeMutableImage(decoded)
	if decoded.Bounds().Dx() != metadata.Width || decoded.Bounds().Dy() != metadata.Height {
		return nil, errors.New("agent: image view PNG does not match its metadata")
	}
	return &View{Metadata: metadata, data: encodedPNG}, nil
}

func validateMetadataFreeViewPNG(data []byte) error {
	const signature = "\x89PNG\r\n\x1a\n"
	if len(data) < len(signature) || string(data[:len(signature)]) != signature {
		return errors.New("agent: invalid PNG signature")
	}
	seenHeader, seenImage, seenEnd := false, false, false
	for offset := len(signature); offset < len(data); {
		if seenEnd || len(data)-offset < 12 {
			return errors.New("agent: invalid PNG chunk framing")
		}
		length := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		remaining := uint64(len(data) - offset)
		if length+12 > remaining {
			return errors.New("agent: invalid PNG chunk length")
		}
		chunkEnd := offset + 12 + int(length)
		chunkType := data[offset+4 : offset+8]
		payload := data[offset+8 : offset+8+int(length)]
		checksum := crc32.NewIEEE()
		_, _ = checksum.Write(chunkType)
		_, _ = checksum.Write(payload)
		if checksum.Sum32() != binary.BigEndian.Uint32(data[chunkEnd-4:chunkEnd]) {
			return errors.New("agent: invalid PNG chunk checksum")
		}
		switch string(chunkType) {
		case "IHDR":
			if seenHeader || seenImage || length != 13 {
				return errors.New("agent: invalid PNG header")
			}
			if colorType := payload[9]; colorType != 0 && colorType != 2 && colorType != 4 && colorType != 6 {
				return errors.New("agent: indexed PNG views are not accepted")
			}
			seenHeader = true
		case "IDAT":
			if !seenHeader || seenEnd {
				return errors.New("agent: invalid PNG image data")
			}
			seenImage = true
		case "IEND":
			if !seenHeader || !seenImage || seenEnd || length != 0 || chunkEnd != len(data) {
				return errors.New("agent: invalid PNG end")
			}
			seenEnd = true
		default:
			return errors.New("agent: PNG ancillary chunks are not accepted")
		}
		offset = chunkEnd
	}
	if !seenEnd {
		return errors.New("agent: missing PNG end")
	}
	return nil
}

// TakePNG transfers the encoded image to the caller exactly once. The caller
// must clear the returned bytes after its transport or consumer is finished.
func (view *View) TakePNG() ([]byte, error) {
	if view == nil {
		return nil, ErrObservationClosed
	}
	view.mu.Lock()
	if view.closed || len(view.data) == 0 {
		view.mu.Unlock()
		return nil, ErrObservationClosed
	}
	data := view.data
	view.data = nil
	view.closed = true
	done := view.done
	view.done = nil
	view.mu.Unlock()
	if done != nil {
		done(view)
	}
	return data, nil
}

// Close zeroes encoded image bytes that have not been transferred.
func (view *View) Close() error {
	if view == nil {
		return nil
	}
	view.mu.Lock()
	clear(view.data)
	view.data = nil
	view.closed = true
	done := view.done
	view.done = nil
	view.mu.Unlock()
	if done != nil {
		done(view)
	}
	return nil
}

type viewCaptureDriver interface {
	CaptureView(context.Context, CaptureRegion, bool) (image.Image, string, error)
}

func (robotGoDriver) CaptureView(
	ctx context.Context,
	region CaptureRegion,
	allowPortal bool,
) (image.Image, string, error) {
	if runtime.GOOS == goOSLinux && robotgo.DetectDisplayServer() == robotgo.DisplayServerWayland {
		return captureWaylandAgentWithBackend(
			ctx,
			region,
			!allowPortal || os.Getenv(disablePortalEnv) != "",
			robotgo.CaptureImgNativeContext,
			robotgo.ScreenCastCaptureReady,
			robotgo.CaptureScreenCastDisplay,
		)
	}
	img, err := (robotGoDriver{}).Capture(ctx, region)
	return img, string(robotgo.LastBackend()), err
}

// View captures and encodes one policy-approved image entirely in memory.
// It never opens a portal consent prompt and consumes view and observation
// quota on an authorized attempt.
func (s *Session) View(ctx context.Context, request ViewRequest) (*View, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.acquire(ctx); err != nil {
		return nil, viewOperationError(err)
	}
	defer s.release()
	if err := s.ensureOpen(); err != nil {
		return nil, viewOperationError(err)
	}
	if err := s.authorizeView(request); err != nil {
		return nil, err
	}
	capability, ok := s.operationCapability(OperationView)
	if !ok || !capability.Available || capability.Backend == "" {
		code := capability.UnavailableCode
		if code == "" {
			code = ErrorUnsupported
		}
		return nil, viewError(code, "desktop image view is unavailable", robotgo.ErrNotSupported)
	}
	if !capability.PolicyAllowed {
		return nil, viewError(ErrorPolicyDenied, "agent policy denied the available image view backend", ErrPolicyDenied)
	}
	if s.usedViews >= s.policy.MaxViews || s.usedObservations >= s.policy.MaxObservations {
		return nil, viewError(ErrorPolicyDenied, "agent policy image view quota reached", ErrPolicyDenied)
	}
	if err := s.emitAudit(ctx, AuditEvent{Kind: AuditObservationStarted, Operation: OperationView}); err != nil {
		return nil, viewError(ErrorAuditDelivery, "audit sink rejected image view intent", err)
	}
	if err := s.beginView(); err != nil {
		return s.finishViewFailure(ctx, nil,
			viewError(ErrorPolicyDenied, "agent policy image view rate limit reached", err))
	}

	viewCtx, cancel := context.WithTimeout(ctx, time.Duration(s.policy.ViewTimeoutMillis)*time.Millisecond)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	defer func() {
		stopSessionCancel()
		cancel()
	}()
	if err := s.viewExecutionError(viewCtx); err != nil {
		return s.finishViewFailure(ctx, nil, err)
	}
	region, err := s.resolveViewRegion(viewCtx, request)
	if err != nil {
		return s.finishViewFailure(ctx, nil, err)
	}
	started := s.now()
	frame, backend, err := s.captureViewFrame(viewCtx, region)
	if err != nil {
		return s.finishViewFailure(ctx, nil, err)
	}
	retained := false
	defer func() {
		if !retained {
			_ = frame.buffer.close()
		}
	}()
	redacted, err := s.redactViewFrame(viewCtx, frame)
	if err != nil {
		return s.finishViewFailure(ctx, nil, viewOperationError(err))
	}
	encoded, width, height, downscaled, err := encodeViewPNG(
		viewCtx,
		frame.buffer,
		s.policy.MaxViewWidth,
		s.policy.MaxViewHeight,
		s.policy.MaxViewEncodedBytes,
	)
	if err != nil {
		if errors.Is(err, errViewEncodedLimit) {
			return s.finishViewFailure(ctx, nil,
				viewError(ErrorPolicyDenied, "encoded image view exceeds the policy byte limit", ErrPolicyDenied))
		}
		return s.finishViewFailure(ctx, nil, viewOperationError(err))
	}
	metadata := ViewMetadata{
		SchemaVersion: ViewSchemaVersion, ObservationID: newObservationID(),
		CreatedAt: s.now().UTC(), Region: region, Width: width, Height: height,
		Backend: backend, MIMEType: ViewMIMEType,
		CaptureDurationMillis: max(int64(0), s.now().Sub(started).Milliseconds()),
		Downscaled:            downscaled, Redacted: redacted,
	}
	if metadata.Backend == "" {
		metadata.Backend = capability.Backend
	}
	view, err := NewImageView(metadata, encoded)
	if err != nil {
		clear(encoded)
		return s.finishViewFailure(ctx, nil, viewOperationError(err))
	}
	if err := s.viewExecutionError(viewCtx); err != nil {
		_ = view.Close()
		return s.finishViewFailure(ctx, nil, err)
	}
	s.storeViewObservation(view.Metadata, frame, redacted)
	s.trackView(view)
	retained = true
	if err := s.emitAudit(ctx, AuditEvent{
		Kind: AuditObservationFinished, Operation: OperationView,
		ObservationID: view.Metadata.ObservationID,
	}); err != nil {
		return view, viewError(ErrorAuditDelivery, "image view completed but audit delivery failed", err)
	}
	return view, nil
}

func (s *Session) authorizeView(request ViewRequest) error {
	if _, allowed := s.policy.allowOperation[OperationView]; !allowed {
		return viewError(ErrorPolicyDenied, "agent policy denied desktop image view", ErrPolicyDenied)
	}
	if _, required := s.policy.requireConfirmation[OperationView]; required && !request.Confirmed {
		return viewError(ErrorPolicyDenied, "agent policy requires image view confirmation", ErrPolicyDenied)
	}
	if (request.Region == nil) == (request.FullDisplayID == nil) {
		return viewError(ErrorInvalidInput, "image view requires exactly one region or full display selector", nil)
	}
	if request.FullDisplayID != nil {
		if *request.FullDisplayID < 0 {
			return viewError(ErrorInvalidInput, "full display ID must be non-negative", nil)
		}
		if !s.policy.AllowFullDisplayView {
			return viewError(ErrorPolicyDenied, "agent policy denied full-display image view", ErrPolicyDenied)
		}
		if _, allowed := s.policy.allowDisplay[*request.FullDisplayID]; !allowed {
			return viewError(ErrorPolicyDenied, "agent policy denied the image view display", ErrPolicyDenied)
		}
		return nil
	}
	if err := validateCaptureRegion(*request.Region, s.policy.MaxViewSourcePixels); err != nil {
		return viewError(ErrorInvalidInput, "invalid image view region", err)
	}
	if _, allowed := s.policy.allowDisplay[request.Region.DisplayID]; !allowed {
		return viewError(ErrorPolicyDenied, "agent policy denied the image view display", ErrPolicyDenied)
	}
	for _, allowed := range s.policy.AllowedViewRegions {
		if captureRegionContains(allowed, *request.Region) {
			return nil
		}
	}
	return viewError(ErrorPolicyDenied, "agent policy denied the image view region", ErrPolicyDenied)
}

func (s *Session) beginView() error {
	now := s.now()
	if !s.lastView.IsZero() {
		elapsed := now.Sub(s.lastView)
		minimum := time.Duration(s.policy.MinViewIntervalMillis) * time.Millisecond
		if elapsed < 0 || elapsed < minimum {
			return ErrPolicyDenied
		}
	}
	s.usedViews++
	s.lastView = now
	return nil
}

func (s *Session) resolveViewRegion(ctx context.Context, request ViewRequest) (CaptureRegion, error) {
	if request.Region != nil {
		return *request.Region, nil
	}
	bounds, err := displayBoundsWithContext(ctx, func() (displayBounds, error) {
		return s.driver.DisplayBounds(*request.FullDisplayID)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CaptureRegion{}, viewContextError(ctx)
		}
		code, message := classifyBackendError(err)
		return CaptureRegion{}, viewError(code, message, err)
	}
	region := CaptureRegion{
		X: bounds.x, Y: bounds.y, Width: bounds.width, Height: bounds.height,
		DisplayID: *request.FullDisplayID,
	}
	if err := validateCaptureRegion(region, s.policy.MaxViewSourcePixels); err != nil {
		return CaptureRegion{}, viewError(ErrorPolicyDenied, "full display exceeds the image view source-pixel limit", ErrPolicyDenied)
	}
	return region, nil
}

func (s *Session) captureViewFrame(ctx context.Context, region CaptureRegion) (*capturedFrame, string, error) {
	backend := ""
	capture := s.driver.Capture
	if driver, ok := s.driver.(viewCaptureDriver); ok {
		capture = func(ctx context.Context, region CaptureRegion) (image.Image, error) {
			var img image.Image
			var err error
			img, backend, err = driver.CaptureView(ctx, region, s.policy.AllowPortalView)
			return img, err
		}
	}
	frame, err := s.captureWith(ctx, region, s.policy.MaxViewSourcePixels, true, capture)
	if err != nil {
		return nil, "", viewOperationError(err)
	}
	return frame, backend, nil
}

func (s *Session) recaptureLineage(ctx context.Context, record observationRecord) (*capturedFrame, error) {
	if record.source != OperationView {
		return s.capture(ctx, record.region, true)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	viewCtx, cancel := context.WithTimeout(ctx, time.Duration(s.policy.ViewTimeoutMillis)*time.Millisecond)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	defer func() {
		stopSessionCancel()
		cancel()
	}()
	if err := s.viewExecutionError(viewCtx); err != nil {
		return nil, err
	}
	frame, _, err := s.captureViewFrame(viewCtx, record.region)
	if err != nil {
		return nil, err
	}
	if _, err := s.redactViewFrame(viewCtx, frame); err != nil {
		_ = frame.buffer.close()
		return nil, viewOperationError(err)
	}
	return frame, nil
}

func (s *Session) redactViewFrame(ctx context.Context, frame *capturedFrame) (bool, error) {
	if frame == nil || frame.buffer == nil || !frame.buffer.acquireUse() {
		return false, viewError(ErrorBackendFailure, "image view capture is unavailable", ErrObservationClosed)
	}
	defer frame.buffer.releaseUse()
	redacted := false
	for _, mask := range s.policy.ViewRedactionMasks {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		intersection, ok := intersectCaptureRegions(frame.metadata.Region, mask)
		if !ok {
			continue
		}
		redacted = true
		rect := image.Rect(
			intersection.X-frame.metadata.Region.X,
			intersection.Y-frame.metadata.Region.Y,
			intersection.X-frame.metadata.Region.X+intersection.Width,
			intersection.Y-frame.metadata.Region.Y+intersection.Height,
		)
		if err := fillRGBA(ctx, frame.buffer.pixels, rect, color.RGBA{A: 255}); err != nil {
			return false, err
		}
	}
	if redacted {
		digest := sha256.Sum256(frame.buffer.pixels.Pix)
		frame.metadata.SHA256 = hex.EncodeToString(digest[:])
	}
	return redacted, nil
}

func captureRegionContains(outer, inner CaptureRegion) bool {
	return outer.DisplayID == inner.DisplayID &&
		containsSpan(inner.X, inner.Width, outer.X, outer.Width) &&
		containsSpan(inner.Y, inner.Height, outer.Y, outer.Height)
}

func intersectCaptureRegions(left, right CaptureRegion) (CaptureRegion, bool) {
	if left.DisplayID != right.DisplayID {
		return CaptureRegion{}, false
	}
	x1, y1 := max(left.X, right.X), max(left.Y, right.Y)
	x2 := min(left.X+left.Width, right.X+right.Width)
	y2 := min(left.Y+left.Height, right.Y+right.Height)
	if x2 <= x1 || y2 <= y1 {
		return CaptureRegion{}, false
	}
	return CaptureRegion{X: x1, Y: y1, Width: x2 - x1, Height: y2 - y1, DisplayID: left.DisplayID}, true
}

func fillRGBA(ctx context.Context, target *image.RGBA, rect image.Rectangle, value color.RGBA) error {
	rect = rect.Intersect(target.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		if y&63 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		for x := rect.Min.X; x < rect.Max.X; x++ {
			target.SetRGBA(x, y, value)
		}
	}
	return nil
}

func encodeViewPNG(
	ctx context.Context,
	source *captureBuffer,
	maxWidth, maxHeight int,
	maxBytes uint64,
) ([]byte, int, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, false, err
	}
	if source == nil || !source.acquireUse() {
		return nil, 0, 0, false, ErrObservationClosed
	}
	defer source.releaseUse()
	width, height := boundedViewDimensions(source.pixels.Bounds().Dx(), source.pixels.Bounds().Dy(), maxWidth, maxHeight)
	if width <= 0 || height <= 0 {
		return nil, 0, 0, false, errors.New("agent: invalid bounded view dimensions")
	}
	output := image.NewRGBA(image.Rect(0, 0, width, height))
	defer clear(output.Pix)
	downscaled := width != source.pixels.Bounds().Dx() || height != source.pixels.Bounds().Dy()
	if downscaled {
		if err := scaleNearestRGBA(ctx, output, source.pixels); err != nil {
			return nil, 0, 0, false, err
		}
	} else {
		copy(output.Pix, source.pixels.Pix)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, false, err
	}
	buffer := &boundedViewBuffer{ctx: ctx, maximum: maxBytes}
	encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := encoder.Encode(buffer, output); err != nil {
		buffer.close()
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, 0, 0, false, contextErr
		}
		if errors.Is(err, errViewEncodedLimit) {
			return nil, 0, 0, false, errViewEncodedLimit
		}
		return nil, 0, 0, false, errors.New("agent: encode image view")
	}
	if err := ctx.Err(); err != nil {
		buffer.close()
		return nil, 0, 0, false, err
	}
	return buffer.take(), width, height, downscaled, nil
}

func boundedViewDimensions(width, height, maxWidth, maxHeight int) (int, int) {
	if width <= 0 || height <= 0 || maxWidth <= 0 || maxHeight <= 0 {
		return 0, 0
	}
	resultWidth, resultHeight := width, height
	if resultWidth > maxWidth {
		resultHeight = max(1, int(uint64(resultHeight)*uint64(maxWidth)/uint64(resultWidth)))
		resultWidth = maxWidth
	}
	if resultHeight > maxHeight {
		resultWidth = max(1, int(uint64(resultWidth)*uint64(maxHeight)/uint64(resultHeight)))
		resultHeight = maxHeight
	}
	return resultWidth, resultHeight
}

func scaleNearestRGBA(ctx context.Context, destination, source *image.RGBA) error {
	sourceWidth, sourceHeight := source.Bounds().Dx(), source.Bounds().Dy()
	for y := 0; y < destination.Bounds().Dy(); y++ {
		if y&63 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		sourceY := source.Bounds().Min.Y + y*sourceHeight/destination.Bounds().Dy()
		for x := 0; x < destination.Bounds().Dx(); x++ {
			sourceX := source.Bounds().Min.X + x*sourceWidth/destination.Bounds().Dx()
			destination.SetRGBA(x, y, source.RGBAAt(sourceX, sourceY))
		}
	}
	return nil
}

type boundedViewBuffer struct {
	ctx     context.Context
	data    []byte
	maximum uint64
}

func (buffer *boundedViewBuffer) Write(value []byte) (int, error) {
	if err := buffer.ctx.Err(); err != nil {
		return 0, err
	}
	if uint64(len(buffer.data)) > buffer.maximum ||
		uint64(len(value)) > buffer.maximum-uint64(len(buffer.data)) {
		return 0, errViewEncodedLimit
	}
	buffer.data = append(buffer.data, value...)
	return len(value), nil
}

func (buffer *boundedViewBuffer) take() []byte {
	data := buffer.data
	buffer.data = nil
	return data
}

func (buffer *boundedViewBuffer) close() {
	clear(buffer.data)
	buffer.data = nil
}

var _ io.Writer = (*boundedViewBuffer)(nil)

func (s *Session) storeViewObservation(metadata ViewMetadata, frame *capturedFrame, redacted bool) {
	s.observationMu.Lock()
	s.observations[metadata.ObservationID] = observationRecord{
		capture: frame.buffer, region: frame.metadata.Region,
		digest: frame.metadata.SHA256, hasCapture: true, source: OperationView, redacted: redacted,
		createdAt: metadata.CreatedAt, viewDownscaled: metadata.Downscaled,
		targetEvidence: make(map[string]retainedTargetEvidence),
	}
	s.observationMu.Unlock()
}

func (s *Session) trackView(view *View) {
	if view == nil {
		return
	}
	id := view.Metadata.ObservationID
	view.mu.Lock()
	view.done = func(completed *View) {
		s.viewMu.Lock()
		if s.views[id] == completed {
			delete(s.views, id)
		}
		s.viewMu.Unlock()
	}
	view.mu.Unlock()
	s.viewMu.Lock()
	s.views[id] = view
	s.viewMu.Unlock()
}

func (s *Session) releaseView(id string) {
	s.viewMu.Lock()
	view := s.views[id]
	delete(s.views, id)
	s.viewMu.Unlock()
	_ = view.Close()
}

func (s *Session) closeViews() {
	s.viewMu.Lock()
	views := make([]*View, 0, len(s.views))
	for _, view := range s.views {
		views = append(views, view)
	}
	clear(s.views)
	s.viewMu.Unlock()
	for _, view := range views {
		_ = view.Close()
	}
}

func (s *Session) operationCapability(operation Operation) (OperationCapability, bool) {
	for _, capability := range s.catalog.Operations {
		if capability.Operation == operation {
			return capability, true
		}
	}
	return OperationCapability{}, false
}

func (s *Session) viewExecutionError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return viewContextError(ctx)
	}
	if err := s.ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return viewError(ErrorTimedOut, "agent session lifetime expired", context.DeadlineExceeded)
		}
		return viewError(ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
	}
	return nil
}

func (s *Session) finishViewFailure(ctx context.Context, view *View, operationErr error) (*View, error) {
	auditCtx := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		auditCtx, cancel = context.WithTimeout(context.Background(), uiCompletionAuditTimeout)
	}
	defer cancel()
	if auditErr := s.emitAudit(auditCtx, AuditEvent{
		Kind: AuditObservationFinished, Operation: OperationView,
		ErrorCode: classifyUIInspectionError(operationErr),
	}); auditErr != nil {
		return view, viewError(ErrorAuditDelivery, "image view failed and audit delivery failed", errors.Join(operationErr, auditErr))
	}
	return view, operationErr
}

func viewError(code ErrorCode, message string, cause error) *ActionError {
	return newActionError(code, OperationView, message, cause)
}

func viewOperationError(err error) error {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return &ActionError{Code: actionErr.Code, Operation: OperationView, Message: actionErr.Message, cause: err}
	}
	code, message := classifyBackendError(err)
	return viewError(code, message, err)
}

func viewContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return viewError(ErrorTimedOut, "image view deadline exceeded", ctx.Err())
	}
	return viewError(ErrorCanceled, "image view canceled", ctx.Err())
}
