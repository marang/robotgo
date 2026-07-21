package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
)

const (
	testWaylandDisplayEnv = "WAYLAND_DISPLAY"
	testX11DisplayEnv     = "DISPLAY"
)

func observationPolicy() Policy {
	return Policy{
		AllowedOperations: []Operation{OperationObserve, OperationClick},
		AllowedDisplayIDs: []int{0},
		MaxActions:        3, MaxObservations: 8, MaxCapturePixels: 16,
		VerificationAttempts: 2, VerificationIntervalMillis: 0,
		VerificationTimeoutMillis: 100,
	}
}

func syntheticCapture(width, height int, value byte) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: value, G: value + 1, B: value + 2, A: 255})
		}
	}
	return img
}

func observeCapture(t *testing.T, session *Session) *Observation {
	t.Helper()
	observation, err := session.Observe(context.Background(), ObserveRequest{
		Capture: &CaptureRegion{X: 1, Y: 2, Width: 2, Height: 2, DisplayID: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func TestObserveOwnsPixelsAndSerializesOnlyMetadata(t *testing.T) {
	source := syntheticCapture(2, 2, 41)
	rawPixelEncoding := base64.StdEncoding.EncodeToString(append([]byte(nil), source.Pix...))
	driver := &fakeDriver{captureImages: []image.Image{source}}
	session := newTestSession(t, observationPolicy(), driver)

	observation := observeCapture(t, session)
	for index, value := range source.Pix {
		if value != 0 {
			t.Fatalf("source pixel %d was not zeroed: %d", index, value)
		}
	}
	first, err := observation.Image()
	if err != nil {
		t.Fatal(err)
	}
	first.Pix[0] = 255
	second, err := observation.Image()
	if err != nil {
		t.Fatal(err)
	}
	if second.Pix[0] == 255 {
		t.Fatal("caller mutation changed the observation-owned pixels")
	}
	serialized, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "Pix") || strings.Contains(string(serialized), "pixels") ||
		strings.Contains(string(serialized), rawPixelEncoding) {
		t.Fatalf("serialized observation leaked a pixel field: %s", serialized)
	}
	if observation.Capture == nil || observation.Capture.SHA256 == "" {
		t.Fatalf("capture metadata = %+v", observation.Capture)
	}
}

func TestObservationErrorsNeverSerializeBackendDetails(t *testing.T) {
	const backendDetail = "private-backend-diagnostic"
	driver := &fakeDriver{captureErr: errors.New(backendDetail)}
	session := newTestSession(t, observationPolicy(), driver)
	_, observeErr := session.Observe(context.Background(), ObserveRequest{
		Capture: &CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
	})
	serialized, err := json.Marshal(observeErr)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), backendDetail) {
		t.Fatalf("serialized observation error leaked backend detail: %s", serialized)
	}
}

func TestObservationPolicyDeniesBeforeDesktopIO(t *testing.T) {
	tests := []Policy{
		testPolicy(),
		{
			AllowedOperations: []Operation{OperationObserve},
			ConfirmOperations: []Operation{OperationObserve},
			AllowedDisplayIDs: []int{0}, MaxObservations: 1, MaxCapturePixels: 4,
		},
		{
			AllowedOperations: []Operation{OperationObserve},
			AllowedDisplayIDs: []int{0}, MaxObservations: 1,
		},
	}
	for _, policy := range tests {
		driver := &fakeDriver{}
		session := newTestSession(t, policy, driver)
		_, err := session.Observe(context.Background(), ObserveRequest{
			Capture: &CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
		})
		if !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("Observe with policy %+v = %v", policy, err)
		}
		if driver.captureCount() != 0 {
			t.Fatalf("denied observation reached capture backend %d times", driver.captureCount())
		}
		_ = session.Close()
	}
}

func TestPublicMetadataCannotChangeInternalLineage(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 8), syntheticCapture(2, 2, 8),
	}}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)
	observation.Capture.Region = CaptureRegion{X: 99, Y: 99, Width: 1, Height: 1, DisplayID: 0}
	observation.Capture.SHA256 = "caller-controlled"

	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
	})
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if driver.callCount() != 1 || driver.captureCount() != 2 {
		t.Fatalf("input calls = %d, capture calls = %d", driver.callCount(), driver.captureCount())
	}
}

func TestStalePreconditionPreventsMutation(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 1), syntheticCapture(2, 2, 2),
	}}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)

	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
	})
	if !errors.Is(err, ErrStaleTarget) || result.Error == nil || result.Error.Code != ErrorStaleTarget {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if driver.callCount() != 0 {
		t.Fatalf("stale target reached input driver %d times", driver.callCount())
	}
}

func TestChangedVerificationProducesBoundedProof(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 3), syntheticCapture(2, 2, 3), syntheticCapture(2, 2, 4),
	}}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)

	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
		Verification: &VerificationRequest{Condition: VerificationCaptureChanged},
	})
	if err != nil || result.Status != ActionSucceeded || result.Verification == nil ||
		result.Verification.Status != VerificationPassed || result.Verification.Attempts != 1 || result.PostObservationID == "" {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	record, ok := session.observation(result.PostObservationID)
	if !ok || !record.hasCapture || !record.capture.usable() {
		t.Fatalf("post observation %q was not retained safely", result.PostObservationID)
	}
}

func TestUnchangedVerificationPasses(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 10), syntheticCapture(2, 2, 10), syntheticCapture(2, 2, 10),
	}}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)
	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
		Verification: &VerificationRequest{Condition: VerificationCaptureUnchanged},
	})
	if err != nil || result.Status != ActionSucceeded || result.Verification == nil ||
		result.Verification.Status != VerificationPassed || result.Verification.Attempts != 1 {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
}

func TestFailedVerificationIsExplicitAfterMutation(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 5), syntheticCapture(2, 2, 5),
		syntheticCapture(2, 2, 5), syntheticCapture(2, 2, 5),
	}}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)

	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
		Verification: &VerificationRequest{Condition: VerificationCaptureChanged},
	})
	if !errors.Is(err, ErrVerification) || result.Status != ActionUnverified || result.Error == nil ||
		result.Error.Code != ErrorVerification || result.Verification == nil || result.Verification.Attempts != 2 {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if driver.callCount() != 1 || driver.captureCount() != 4 {
		t.Fatalf("input calls = %d, capture calls = %d", driver.callCount(), driver.captureCount())
	}
}

func TestVerificationTimeoutStopsPolling(t *testing.T) {
	policy := observationPolicy()
	policy.VerificationAttempts = 10
	policy.VerificationIntervalMillis = 50
	policy.VerificationTimeoutMillis = 5
	policy.MaxObservations = 20
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 6), syntheticCapture(2, 2, 6), syntheticCapture(2, 2, 6),
	}}
	session := newTestSession(t, policy, driver)
	observation := observeCapture(t, session)

	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
		Verification: &VerificationRequest{Condition: VerificationCaptureChanged},
	})
	if result.Status != ActionUnverified || result.Error == nil || result.Error.Code != ErrorTimedOut ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if driver.captureCount() != 3 {
		t.Fatalf("bounded capture calls = %d, want 3", driver.captureCount())
	}
}

func TestSessionCloseCancelsVerificationPolling(t *testing.T) {
	policy := observationPolicy()
	policy.VerificationAttempts = 10
	policy.VerificationIntervalMillis = 1000
	policy.VerificationTimeoutMillis = 5000
	policy.MaxObservations = 12
	driver := &fakeDriver{
		captureHit: make(chan struct{}, 3),
		captureImages: []image.Image{
			syntheticCapture(2, 2, 12), syntheticCapture(2, 2, 12), syntheticCapture(2, 2, 12),
		},
	}
	session := newTestSession(t, policy, driver)
	observation := observeCapture(t, session)
	<-driver.captureHit
	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := session.Execute(context.Background(), ActionRequest{
			Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
			Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
			Verification: &VerificationRequest{Condition: VerificationCaptureChanged},
		})
		resultCh <- result
		errCh <- err
	}()
	<-driver.captureHit
	<-driver.captureHit
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := <-resultCh, <-errCh
	if !errors.Is(err, ErrSessionClosed) || result.Status != ActionUnverified ||
		result.Error == nil || result.Error.Code != ErrorSessionClosed {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
}

func TestVerificationCancellationCoversFinalDiagnostics(t *testing.T) {
	policy := observationPolicy()
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 13), syntheticCapture(2, 2, 13), syntheticCapture(2, 2, 14),
	}}
	session := newTestSession(t, policy, driver)
	observation := observeCapture(t, session)
	driver.capabilityHit = make(chan struct{}, 1)
	driver.capabilityGo = make(chan struct{})
	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		result, err := session.Execute(ctx, ActionRequest{
			Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
			Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
			Verification: &VerificationRequest{Condition: VerificationCaptureChanged},
		})
		resultCh <- result
		errCh <- err
	}()
	<-driver.capabilityHit
	cancel()
	close(driver.capabilityGo)
	result, err := <-resultCh, <-errCh
	if !errors.Is(err, context.Canceled) || result.Status != ActionUnverified ||
		result.Error == nil || result.Error.Code != ErrorCanceled || result.PostObservationID != "" {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if len(session.observations) != 1 {
		t.Fatalf("timed-out proof retained a post observation: %d", len(session.observations))
	}
}

func TestSessionCloseZeroesAllOwnedCaptures(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 7)}}
	policy, err := preparePolicy(observationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	session, err := newSession(policy, driver, availableCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	observation := observeCapture(t, session)
	buffer := observation.capture
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if buffer.usable() {
		t.Fatal("session close retained sensitive capture pixels")
	}
	if _, err := observation.Image(); !errors.Is(err, ErrObservationClosed) {
		t.Fatalf("Image after Close = %v", err)
	}
	if _, err := session.Observe(context.Background(), ObserveRequest{}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Observe after Close = %v", err)
	}
}

func TestClosedObservationCannotAuthorizeMutation(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 19)}}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)
	if err := observation.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
	})
	if !errors.Is(err, ErrStaleTarget) || result.Error == nil || result.Error.Code != ErrorStaleTarget {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if driver.callCount() != 0 || driver.captureCount() != 1 {
		t.Fatalf("input calls = %d, capture calls = %d", driver.callCount(), driver.captureCount())
	}
}

func TestObservationCloseDuringLineageCapturePreventsMutation(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 20), syntheticCapture(2, 2, 20),
	}}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)
	driver.captureHit = make(chan struct{}, 1)
	driver.captureGo = make(chan struct{})

	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := session.Execute(context.Background(), ActionRequest{
			Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
			Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
		})
		resultCh <- result
		errCh <- err
	}()
	<-driver.captureHit
	if err := observation.Close(); err != nil {
		t.Fatal(err)
	}
	close(driver.captureGo)

	result, err := <-resultCh, <-errCh
	if !errors.Is(err, ErrStaleTarget) || result.Error == nil || result.Error.Code != ErrorStaleTarget {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if driver.callCount() != 0 {
		t.Fatalf("closed lineage reached input driver %d times", driver.callCount())
	}
}

func TestObservationCloseWaitsForAuthorizedDispatch(t *testing.T) {
	driver := &fakeDriver{
		captureImages: []image.Image{
			syntheticCapture(2, 2, 22), syntheticCapture(2, 2, 22),
		},
		started: make(chan struct{}, 1), release: make(chan struct{}),
	}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)

	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := session.Execute(context.Background(), ActionRequest{
			Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
			Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
		})
		resultCh <- result
		errCh <- err
	}()
	<-driver.started
	closeDone := make(chan error, 1)
	go func() { closeDone <- observation.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Observation.Close returned during authorized dispatch: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(driver.release)

	result, err := <-resultCh, <-errCh
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
}

func TestSessionCloseDuringLineageCapturePreventsMutation(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 21), syntheticCapture(2, 2, 21),
	}}
	session := newTestSession(t, observationPolicy(), driver)
	observation := observeCapture(t, session)
	driver.captureHit = make(chan struct{}, 1)
	driver.captureGo = make(chan struct{})

	resultCh := make(chan ActionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := session.Execute(context.Background(), ActionRequest{
			Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
			Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
		})
		resultCh <- result
		errCh <- err
	}()
	<-driver.captureHit
	closeDone := make(chan struct{})
	go func() {
		_ = session.Close()
		close(closeDone)
	}()
	<-session.ctx.Done()
	close(driver.captureGo)

	result, err := <-resultCh, <-errCh
	if !errors.Is(err, ErrSessionClosed) || result.Error == nil || result.Error.Code != ErrorSessionClosed {
		t.Fatalf("Execute = %+v, %v", result, err)
	}
	if driver.callCount() != 0 {
		t.Fatalf("closed session reached input driver %d times", driver.callCount())
	}
	<-closeDone
}

type recordingAuditSink struct {
	mu     sync.Mutex
	events []AuditEvent
	failAt int
}

func (sink *recordingAuditSink) Record(_ context.Context, event AuditEvent) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.events = append(sink.events, event)
	if sink.failAt > 0 && len(sink.events) == sink.failAt {
		return errors.New("audit unavailable")
	}
	return nil
}

func TestAuditIntentFailurePreventsDesktopIO(t *testing.T) {
	policy, err := preparePolicy(observationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 9)}}
	sink := &recordingAuditSink{failAt: 1}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if _, err := session.Observe(context.Background(), ObserveRequest{
		Capture: &CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
	}); err == nil {
		t.Fatal("Observe succeeded after audit intent failure")
	}
	if driver.captureCount() != 0 || driver.callCount() != 0 {
		t.Fatal("audit intent failure reached desktop I/O")
	}
}

func TestActionAuditIntentFailurePreventsBoundsAndInputIO(t *testing.T) {
	policy := observationPolicy()
	policy.AllowedOperations = append(policy.AllowedOperations, OperationMove)
	prepared, err := preparePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeDriver{boundsHit: make(chan struct{}, 1)}
	sink := &recordingAuditSink{failAt: 1}
	session, err := newSessionWithAudit(prepared, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, executeErr := session.Execute(context.Background(), ActionRequest{
		Operation: OperationMove, Move: &MoveAction{X: 1, Y: 1, DisplayID: 0},
	})
	if result.Error == nil || result.Error.Code != ErrorAuditDelivery || !errors.Is(executeErr, ErrAuditDelivery) {
		t.Fatalf("Execute = %+v, %v", result, executeErr)
	}
	select {
	case <-driver.boundsHit:
		t.Fatal("audit intent failure reached display bounds I/O")
	default:
	}
	if driver.callCount() != 0 {
		t.Fatal("audit intent failure reached input I/O")
	}
}

func TestActionCompletionAuditFailurePreservesOutcome(t *testing.T) {
	policy, err := preparePolicy(observationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeDriver{}
	sink := &recordingAuditSink{failAt: 2}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	result, executeErr := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
	})
	var actionErr *ActionError
	if !errors.As(executeErr, &actionErr) || actionErr.Code != ErrorAuditDelivery || !errors.Is(executeErr, ErrAuditDelivery) ||
		result.Status != ActionSucceeded || result.Error != nil || driver.callCount() != 1 {
		t.Fatalf("Execute = %+v, %v", result, executeErr)
	}
	serialized, err := json.Marshal(sink.events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "coordinates") || strings.Contains(string(serialized), "text") ||
		strings.Contains(string(serialized), "sha256") || strings.Contains(string(serialized), "pixels") {
		t.Fatalf("audit leaked sensitive payload fields: %s", serialized)
	}
}

func TestVerificationAuditFailurePreservesPassedOutcome(t *testing.T) {
	policy, err := preparePolicy(observationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeDriver{captureImages: []image.Image{
		syntheticCapture(2, 2, 20), syntheticCapture(2, 2, 20), syntheticCapture(2, 2, 21),
	}}
	sink := &recordingAuditSink{failAt: 4}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	observation := observeCapture(t, session)
	result, executeErr := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: observation.ObservationID},
		Verification: &VerificationRequest{Condition: VerificationCaptureChanged},
	})
	var actionErr *ActionError
	if !errors.As(executeErr, &actionErr) || actionErr.Code != ErrorAuditDelivery || !errors.Is(executeErr, ErrAuditDelivery) ||
		result.Status != ActionSucceeded || result.Error != nil || result.Verification == nil ||
		result.Verification.Status != VerificationPassed || driver.callCount() != 1 {
		t.Fatalf("Execute = %+v, %v", result, executeErr)
	}
}

func TestCaptureRegionValidationIsOverflowSafe(t *testing.T) {
	if err := validateCaptureRegion(CaptureRegion{Width: int(^uint(0) >> 1), Height: 2, DisplayID: 0}, 16); err == nil {
		t.Fatal("overflow-sized region was accepted")
	}
	if containsSpan(99, 2, 0, 100) {
		t.Fatal("out-of-bounds span was accepted")
	}
}

func TestInvalidCaptureRegionNeverReachesDesktopBackend(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, observationPolicy(), driver)
	for _, region := range []CaptureRegion{
		{Width: 0, Height: 1, DisplayID: 0},
		{Width: 5, Height: 5, DisplayID: 0},
		{X: 99, Y: 99, Width: 2, Height: 2, DisplayID: 0},
		{Width: 1, Height: 1, DisplayID: -1},
	} {
		if _, err := session.Observe(context.Background(), ObserveRequest{Capture: &region}); err == nil {
			t.Fatalf("invalid capture region was accepted: %+v", region)
		}
	}
	if driver.captureCount() != 0 {
		t.Fatalf("invalid regions reached capture backend %d times", driver.captureCount())
	}
}

func TestObservationAndActionAreSerialized(t *testing.T) {
	driver := &fakeDriver{
		captureHit: make(chan struct{}, 1), captureGo: make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	session := newTestSession(t, observationPolicy(), driver)
	observeErr := make(chan error, 1)
	go func() {
		observation, err := session.Observe(context.Background(), ObserveRequest{
			Capture: &CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
		})
		if observation != nil {
			_ = observation.Close()
		}
		observeErr <- err
	}()
	<-driver.captureHit
	actionErr := make(chan error, 1)
	go func() {
		_, err := session.Execute(context.Background(), ActionRequest{
			Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		})
		actionErr <- err
	}()
	select {
	case <-driver.started:
		t.Fatal("action reached input while observation still owned the session gate")
	case <-time.After(20 * time.Millisecond):
	}
	close(driver.captureGo)
	if err := <-observeErr; err != nil {
		t.Fatalf("Observe = %v", err)
	}
	if err := <-actionErr; err != nil {
		t.Fatalf("Execute = %v", err)
	}
}

func TestFailedCaptureConsumesBoundedObservationAttempt(t *testing.T) {
	policy := observationPolicy()
	policy.MaxObservations = 1
	policy.VerificationAttempts = 0
	policy.VerificationIntervalMillis = 0
	policy.VerificationTimeoutMillis = 0
	driver := &fakeDriver{captureErr: errors.New("capture failed")}
	session := newTestSession(t, policy, driver)
	request := ObserveRequest{Capture: &CaptureRegion{Width: 2, Height: 2, DisplayID: 0}}
	if _, err := session.Observe(context.Background(), request); err == nil {
		t.Fatal("first capture unexpectedly succeeded")
	}
	if _, err := session.Observe(context.Background(), request); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("second capture error = %v", err)
	}
	if driver.captureCount() != 1 {
		t.Fatalf("capture attempts = %d, want 1", driver.captureCount())
	}
}

func TestFailedBackendCaptureIsZeroed(t *testing.T) {
	source := syntheticCapture(2, 2, 33)
	driver := &fakeDriver{
		captureImages: []image.Image{source}, captureErr: errors.New("partial capture failed"),
	}
	session := newTestSession(t, observationPolicy(), driver)
	if _, err := session.Observe(context.Background(), ObserveRequest{
		Capture: &CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
	}); err == nil {
		t.Fatal("partial failed capture unexpectedly succeeded")
	}
	for index, value := range source.Pix {
		if value != 0 {
			t.Fatalf("failed capture pixel %d was not zeroed: %d", index, value)
		}
	}
}

func TestVerificationPolicyMustBeBounded(t *testing.T) {
	tests := []Policy{
		{AllowedOperations: []Operation{OperationClick}, MaxActions: 1, VerificationAttempts: 1},
		{AllowedOperations: []Operation{OperationClick}, MaxActions: 1, VerificationTimeoutMillis: 1},
		{AllowedOperations: []Operation{OperationClick}, MaxActions: 1, VerificationAttempts: 1, VerificationTimeoutMillis: 1},
		{AllowedOperations: []Operation{OperationClick}, MaxActions: 1, MaxCapturePixels: maxAgentCapturePixels + 1},
	}
	for _, policy := range tests {
		if _, err := preparePolicy(policy); err == nil {
			t.Fatalf("unbounded policy was accepted: %+v", policy)
		}
	}
}

func TestObservationOnlyPolicyNeedsNoActionBudget(t *testing.T) {
	policy := Policy{
		AllowedOperations: []Operation{OperationObserve},
		MaxObservations:   1,
	}
	if _, err := preparePolicy(policy); err != nil {
		t.Fatalf("observation-only policy = %v", err)
	}
	policy.AllowedOperations = append(policy.AllowedOperations, OperationClick)
	if _, err := preparePolicy(policy); err == nil {
		t.Fatal("mutation policy without an action budget was accepted")
	}
}

func TestWaylandCatalogDoesNotAdvertiseImplicitPortalCapture(t *testing.T) {
	t.Setenv(disablePortalEnv, "")
	policy, err := preparePolicy(Policy{
		AllowedOperations: []Operation{OperationObserve}, MaxObservations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := availableCapabilities()
	capabilities.Runtime.GOOS = "linux"
	capabilities.Runtime.DisplayServer = robotgo.DisplayServerWayland
	capabilities.Capture = robotgo.FeatureCapability{Available: true, Backend: string(robotgo.BackendPortal)}
	capability := buildCatalog(policy, capabilities).Operations[0]
	if capability.CaptureAvailable || !strings.Contains(capability.Remediation, "will not open portal consent implicitly") {
		t.Fatalf("portal-only capture capability = %+v", capability)
	}

	capabilities.Capture = robotgo.FeatureCapability{
		Available: true,
		Backend:   robotgo.FeatureBackendScreenCast,
	}
	if capability = buildCatalog(policy, capabilities).Operations[0]; !capability.CaptureAvailable {
		t.Fatalf("active ScreenCast capability = %+v", capability)
	}

	t.Setenv(disablePortalEnv, "1")
	capabilities.Capture = robotgo.FeatureCapability{
		Available: true,
		Backend:   robotgo.FeatureBackendWaylandScreencopy,
	}
	if capability = buildCatalog(policy, capabilities).Operations[0]; !capability.CaptureAvailable {
		t.Fatalf("explicit native-only capture capability = %+v", capability)
	}
}

func TestWaylandAgentCapturePrefersNativeBeforeActiveScreenCast(t *testing.T) {
	want := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var calls []string
	img, err := captureWaylandAgent(
		context.Background(),
		CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
		false,
		func(...int) (image.Image, error) {
			calls = append(calls, "native")
			return want, nil
		},
		func() error {
			calls = append(calls, "ready")
			return nil
		},
		func(context.Context, int, ...int) (image.Image, error) {
			calls = append(calls, "screencast")
			return nil, nil
		},
	)
	if err != nil || img != want {
		t.Fatalf("captureWaylandAgent = (%v, %v)", img, err)
	}
	if got := strings.Join(calls, ","); got != "native" {
		t.Fatalf("backend order = %q, want native only", got)
	}
}

func TestWaylandAgentCaptureUsesOnlyActiveScreenCastFallback(t *testing.T) {
	nativeErr := errors.New("native unavailable")
	want := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var calls []string
	img, err := captureWaylandAgent(
		context.Background(),
		CaptureRegion{X: -10, Y: 5, Width: 1, Height: 1, DisplayID: 2},
		false,
		func(args ...int) (image.Image, error) {
			calls = append(calls, "native")
			if got := fmt.Sprint(args); got != "[-10 5 1 1 2]" {
				t.Fatalf("native args = %s", got)
			}
			return nil, nativeErr
		},
		func() error {
			calls = append(calls, "ready")
			return nil
		},
		func(_ context.Context, displayID int, region ...int) (image.Image, error) {
			calls = append(calls, "screencast")
			if displayID != 2 || fmt.Sprint(region) != "[-10 5 1 1]" {
				t.Fatalf("ScreenCast target = display %d, region %v", displayID, region)
			}
			return want, nil
		},
	)
	if err != nil || img != want {
		t.Fatalf("captureWaylandAgent = (%v, %v)", img, err)
	}
	if got := strings.Join(calls, ","); got != "native,ready,screencast" {
		t.Fatalf("backend order = %q", got)
	}
}

func TestWaylandAgentCaptureDisabledPortalStopsAfterNative(t *testing.T) {
	nativeErr := errors.New("native unavailable")
	var fallbackCalled bool
	img, err := captureWaylandAgent(
		context.Background(),
		CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
		true,
		func(...int) (image.Image, error) { return nil, nativeErr },
		func() error {
			fallbackCalled = true
			return nil
		},
		func(context.Context, int, ...int) (image.Image, error) {
			fallbackCalled = true
			return nil, nil
		},
	)
	if img != nil || !errors.Is(err, nativeErr) || !errors.Is(err, robotgo.ErrNotSupported) {
		t.Fatalf("captureWaylandAgent = (%v, %v)", img, err)
	}
	if fallbackCalled {
		t.Fatal("portal fallback was consulted while disabled")
	}
}

func TestWaylandAgentCaptureNeverOpensPortalImplicitly(t *testing.T) {
	if runtime.GOOS != goOSLinux {
		t.Skip("Wayland environment selection is Linux-specific")
	}
	t.Setenv(testWaylandDisplayEnv, "robotgo-agent-test")
	t.Setenv(testX11DisplayEnv, "")
	t.Setenv(disablePortalEnv, "")
	img, err := (robotGoDriver{}).Capture(context.Background(), CaptureRegion{
		Width: 1, Height: 1, DisplayID: 0,
	})
	if img != nil || !errors.Is(err, robotgo.ErrNotSupported) ||
		!strings.Contains(err.Error(), "will not open portal consent implicitly") {
		t.Fatalf("Capture = (%v, %v)", img, err)
	}
}

func TestInvalidPreconditionIDCannotEnterAuditTrail(t *testing.T) {
	const secret = "customer-secret-as-observation-id"
	policy, err := preparePolicy(observationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingAuditSink{}
	driver := &fakeDriver{}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, executeErr := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: secret},
	})
	if executeErr == nil || result.Error == nil || result.Error.Code != ErrorInvalidInput {
		t.Fatalf("Execute = %+v, %v", result, executeErr)
	}
	serialized, err := json.Marshal(sink.events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), secret) || len(sink.events) != 0 {
		t.Fatalf("invalid precondition entered audit trail: %s", serialized)
	}
}
