package agent

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"math"
	"strings"
	"testing"
	"time"
)

func conditionPolicy() Policy {
	return Policy{
		AllowedOperations:  []Operation{OperationObserve, OperationFindColor, OperationWaitColor},
		AllowedDisplayIDs:  []int{0},
		MaxObservations:    8,
		MaxCapturePixels:   16,
		MaxQueries:         4,
		WaitAttempts:       3,
		WaitIntervalMillis: 0,
		WaitTimeoutMillis:  100,
	}
}

func TestFindColorUsesOnlyExplicitObservation(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 41)}}
	session := newTestSession(t, conditionPolicy(), driver)
	observation := observeCapture(t, session)

	result, err := session.FindColor(context.Background(), FindColorRequest{
		ObservationID: observation.ObservationID,
		Condition:     ColorCondition{Red: 41, Green: 42, Blue: 43},
	})
	if err != nil || result.Status != ConditionMatched || result.Match == nil {
		t.Fatalf("FindColor = %+v, %v", result, err)
	}
	if *result.Match != (VisualMatch{X: 1, Y: 2, DisplayID: 0}) {
		t.Fatalf("match = %+v", result.Match)
	}
	if driver.captureCount() != 1 {
		t.Fatalf("FindColor performed implicit capture; calls = %d", driver.captureCount())
	}
	if result.ObservationID != observation.ObservationID || result.ConditionID == "" {
		t.Fatalf("result lineage = %+v", result)
	}

	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sha256", "pixels", "tolerance", `"red"`, `"green"`, `"blue"`} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("result leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestFindColorNoMatchIsSuccessfulAndConsumesQueryQuota(t *testing.T) {
	policy := conditionPolicy()
	policy.MaxQueries = 1
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 5)}}
	session := newTestSession(t, policy, driver)
	observation := observeCapture(t, session)
	request := FindColorRequest{
		ObservationID: observation.ObservationID,
		Condition:     ColorCondition{Red: 200, Green: 201, Blue: 202},
	}
	result, err := session.FindColor(context.Background(), request)
	if err != nil || result.Status != ConditionNotMatched || result.Match != nil {
		t.Fatalf("FindColor = %+v, %v", result, err)
	}
	if _, err := session.FindColor(context.Background(), request); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("second FindColor = %v", err)
	}
	if driver.captureCount() != 1 {
		t.Fatalf("FindColor performed implicit capture; calls = %d", driver.captureCount())
	}
}

func TestFindColorRejectsInvalidOrClosedObservation(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 5)}}
	session := newTestSession(t, conditionPolicy(), driver)
	result, err := session.FindColor(context.Background(), FindColorRequest{
		ObservationID: "private-caller-value",
	})
	if !hasErrorCode(err, ErrorInvalidInput) || result.ObservationID != "" {
		t.Fatalf("invalid ID error = %v", err)
	}
	observation := observeCapture(t, session)
	if err := observation.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.FindColor(context.Background(), FindColorRequest{
		ObservationID: observation.ObservationID,
	}); !errors.Is(err, ErrStaleTarget) || !hasErrorCode(err, ErrorStaleTarget) {
		t.Fatalf("closed observation error = %v", err)
	}
}

func TestVisualConditionsReportCanceledAndClosedSessions(t *testing.T) {
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 5)}}
	session := newTestSession(t, conditionPolicy(), driver)
	observation := observeCapture(t, session)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.FindColor(canceled, FindColorRequest{
		ObservationID: observation.ObservationID,
	}); !hasErrorCode(err, ErrorCanceled) {
		t.Fatalf("canceled FindColor = %v", err)
	}
	if _, err := session.WaitColor(canceled, WaitColorRequest{
		Region: CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
	}); !hasErrorCode(err, ErrorCanceled) {
		t.Fatalf("canceled WaitColor = %v", err)
	}
	if session.usedQueries != 0 || driver.captureCount() != 1 {
		t.Fatalf("pre-canceled conditions consumed quota or capture: queries=%d captures=%d",
			session.usedQueries, driver.captureCount())
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.FindColor(context.Background(), FindColorRequest{
		ObservationID: observation.ObservationID,
	}); !errors.Is(err, ErrSessionClosed) || !hasErrorCode(err, ErrorSessionClosed) {
		t.Fatalf("closed FindColor = %v", err)
	}
	if _, err := session.WaitColor(context.Background(), WaitColorRequest{
		Region: CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
	}); !errors.Is(err, ErrSessionClosed) || !hasErrorCode(err, ErrorSessionClosed) {
		t.Fatalf("closed WaitColor = %v", err)
	}
}

func TestVisualConditionValidationAndConfirmationPrecedeDesktopIO(t *testing.T) {
	policy := conditionPolicy()
	policy.ConfirmOperations = []Operation{OperationWaitColor}
	driver := &fakeDriver{}
	session := newTestSession(t, policy, driver)
	request := WaitColorRequest{
		Region:    CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
		Condition: ColorCondition{Tolerance: math.NaN()},
		Confirmed: true,
	}
	if _, err := session.WaitColor(context.Background(), request); !hasErrorCode(err, ErrorInvalidInput) {
		t.Fatalf("invalid wait error = %v", err)
	}
	request.Condition.Tolerance = 0
	request.Confirmed = false
	if _, err := session.WaitColor(context.Background(), request); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("unconfirmed wait error = %v", err)
	}
	if driver.captureCount() != 0 {
		t.Fatalf("denied waits reached capture backend %d times", driver.captureCount())
	}
}

func TestWaitColorDistinguishesInvalidShapeFromPolicySize(t *testing.T) {
	policy := conditionPolicy()
	policy.MaxCapturePixels = 1
	driver := &fakeDriver{}
	session := newTestSession(t, policy, driver)
	if _, err := session.WaitColor(context.Background(), WaitColorRequest{
		Region: CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
	}); !errors.Is(err, ErrPolicyDenied) || !hasErrorCode(err, ErrorPolicyDenied) {
		t.Fatalf("policy-sized WaitColor = %v", err)
	}
	if _, err := session.WaitColor(context.Background(), WaitColorRequest{
		Region: CaptureRegion{Width: maxAgentCapturePixels + 1, Height: 1, DisplayID: 0},
	}); !hasErrorCode(err, ErrorInvalidInput) {
		t.Fatalf("hard-limit WaitColor = %v", err)
	}
	if driver.captureCount() != 0 {
		t.Fatalf("rejected wait reached capture backend %d times", driver.captureCount())
	}
}

func TestWaitColorRetainsOnlyMatchedObservation(t *testing.T) {
	first := syntheticCapture(2, 2, 1)
	second := syntheticCapture(2, 2, 9)
	driver := &fakeDriver{captureImages: []image.Image{first, second}}
	session := newTestSession(t, conditionPolicy(), driver)
	result, err := session.WaitColor(context.Background(), WaitColorRequest{
		Region:    CaptureRegion{X: 10, Y: 20, Width: 2, Height: 2, DisplayID: 0},
		Condition: ColorCondition{Red: 9, Green: 10, Blue: 11},
	})
	if err != nil || result.Status != ConditionMatched ||
		result.Attempts != 2 || result.Match == nil || result.ObservationID == "" {
		t.Fatalf("WaitColor = (%+v, %v)", result, err)
	}
	if *result.Match != (VisualMatch{X: 10, Y: 20, DisplayID: 0}) {
		t.Fatalf("match = %+v", result.Match)
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sha256", "pixels", "tolerance", `"red"`, `"green"`, `"blue"`} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("wait result leaked %q: %s", forbidden, serialized)
		}
	}
	if len(session.observations) != 1 {
		t.Fatalf("retained observations = %d, want matched frame only", len(session.observations))
	}
	for name, source := range map[string]*image.RGBA{"first": first, "second": second} {
		for index, value := range source.Pix {
			if value != 0 {
				t.Fatalf("%s source pixel %d was not zeroed: %d", name, index, value)
			}
		}
	}
	record, ok := session.observation(result.ObservationID)
	if !ok || !record.capture.usable() {
		t.Fatal("matched observation was not retained")
	}
	if err := session.ReleaseObservation(result.ObservationID); err != nil {
		t.Fatal(err)
	}
	if record.capture.usable() || len(session.observations) != 0 {
		t.Fatal("released matched observation retained sensitive pixels or lineage")
	}
	if err := session.ReleaseObservation(result.ObservationID); err != nil {
		t.Fatalf("idempotent ReleaseObservation = %v", err)
	}
}

func TestWaitColorExhaustionLeavesNoObservation(t *testing.T) {
	source := syntheticCapture(2, 2, 3)
	driver := &fakeDriver{captureImages: []image.Image{source}}
	session := newTestSession(t, conditionPolicy(), driver)
	result, err := session.WaitColor(context.Background(), WaitColorRequest{
		Region:    CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
		Condition: ColorCondition{Red: 90, Green: 91, Blue: 92},
	})
	if result.Status != ConditionNotMatched || result.Attempts != 3 ||
		!errors.Is(err, ErrConditionNotMet) || !hasErrorCode(err, ErrorConditionNotMet) {
		t.Fatalf("WaitColor = (%+v, %v)", result, err)
	}
	if len(session.observations) != 0 || driver.captureCount() != 3 {
		t.Fatalf("retained=%d captures=%d", len(session.observations), driver.captureCount())
	}
	for index, value := range source.Pix {
		if value != 0 {
			t.Fatalf("source pixel %d was not zeroed: %d", index, value)
		}
	}
}

func TestWaitColorBackendFailureZeroesPartialCaptureAndSanitizesError(t *testing.T) {
	const privateDetail = "private-capture-backend-detail"
	source := syntheticCapture(2, 2, 3)
	driver := &fakeDriver{
		captureImages: []image.Image{source},
		captureErr:    errors.New(privateDetail),
	}
	session := newTestSession(t, conditionPolicy(), driver)
	result, waitErr := session.WaitColor(context.Background(), WaitColorRequest{
		Region: CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
	})
	if result.Attempts != 1 || !hasErrorCode(waitErr, ErrorBackendFailure) {
		t.Fatalf("WaitColor backend failure = (%+v, %v)", result, waitErr)
	}
	serialized, err := json.Marshal(waitErr)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), privateDetail) {
		t.Fatalf("WaitColor error leaked backend detail: %s", serialized)
	}
	for index, value := range source.Pix {
		if value != 0 {
			t.Fatalf("failed source pixel %d was not zeroed: %d", index, value)
		}
	}
	if len(session.observations) != 0 {
		t.Fatalf("backend failure retained observations = %d", len(session.observations))
	}
}

func TestWaitColorTimeoutStopsPollingAndRetainsNothing(t *testing.T) {
	policy := conditionPolicy()
	policy.WaitAttempts = 8
	policy.WaitIntervalMillis = 50
	policy.WaitTimeoutMillis = 5
	policy.MaxObservations = 8
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 3)}}
	session := newTestSession(t, policy, driver)
	result, err := session.WaitColor(context.Background(), WaitColorRequest{
		Region:    CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
		Condition: ColorCondition{Red: 90, Green: 91, Blue: 92},
	})
	if result.Attempts != 1 || !errors.Is(err, context.DeadlineExceeded) ||
		!hasErrorCode(err, ErrorTimedOut) {
		t.Fatalf("WaitColor = (%+v, %v)", result, err)
	}
	if len(session.observations) != 0 || driver.captureCount() != 1 {
		t.Fatalf("retained=%d captures=%d", len(session.observations), driver.captureCount())
	}
}

func TestWaitColorRequiresCompleteObservationBudgetBeforeCapture(t *testing.T) {
	policy := conditionPolicy()
	policy.MaxObservations = 3
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 3)}}
	session := newTestSession(t, policy, driver)
	_ = observeCapture(t, session)
	if _, err := session.WaitColor(context.Background(), WaitColorRequest{
		Region:    CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
		Condition: ColorCondition{Red: 90, Green: 91, Blue: 92},
	}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("WaitColor observation budget = %v", err)
	}
	if driver.captureCount() != 1 {
		t.Fatalf("budget-denied wait reached capture backend; calls = %d", driver.captureCount())
	}
}

func TestSessionCloseCancelsWaitAndLeavesNoObservation(t *testing.T) {
	policy := conditionPolicy()
	policy.WaitIntervalMillis = 1000
	policy.WaitTimeoutMillis = 5000
	source := syntheticCapture(2, 2, 3)
	driver := &fakeDriver{
		captureImages: []image.Image{source},
		captureHit:    make(chan struct{}, 1),
		captureGo:     make(chan struct{}),
	}
	session := newTestSession(t, policy, driver)
	type waitOutcome struct {
		result WaitColorResult
		err    error
	}
	waitDone := make(chan waitOutcome, 1)
	go func() {
		result, err := session.WaitColor(context.Background(), WaitColorRequest{
			Region:    CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
			Condition: ColorCondition{Red: 90, Green: 91, Blue: 92},
		})
		waitDone <- waitOutcome{result: result, err: err}
	}()
	<-driver.captureHit
	closeDone := make(chan struct{})
	go func() {
		_ = session.Close()
		close(closeDone)
	}()
	<-session.ctx.Done()
	close(driver.captureGo)
	outcome := <-waitDone
	if !errors.Is(outcome.err, ErrSessionClosed) ||
		!hasErrorCode(outcome.err, ErrorSessionClosed) {
		t.Fatalf("WaitColor during close = (%+v, %v)", outcome.result, outcome.err)
	}
	<-closeDone
	if len(session.observations) != 0 {
		t.Fatalf("closed wait retained observations = %d", len(session.observations))
	}
	for index, value := range source.Pix {
		if value != 0 {
			t.Fatalf("canceled source pixel %d was not zeroed: %d", index, value)
		}
	}
}

func TestWaitColorAuditIsPayloadFreeAndIntentFailurePreventsCapture(t *testing.T) {
	policy, err := preparePolicy(conditionPolicy())
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
	request := WaitColorRequest{
		Region:    CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
		Condition: ColorCondition{Red: 9, Green: 10, Blue: 11},
	}
	if _, err := session.WaitColor(context.Background(), request); !errors.Is(err, ErrAuditDelivery) {
		t.Fatalf("WaitColor audit failure = %v", err)
	}
	if driver.captureCount() != 0 || session.usedQueries != 0 || session.usedObservations != 0 {
		t.Fatalf("intent failure reached IO/quota: captures=%d queries=%d observations=%d",
			driver.captureCount(), session.usedQueries, session.usedObservations)
	}
	serialized, err := json.Marshal(sink.events)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sha256", "pixels", "tolerance", `"red"`, `"green"`, `"blue"`} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("audit leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestFindColorAuditRecordsOnlySanitizedLifecycle(t *testing.T) {
	policy, err := preparePolicy(conditionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingAuditSink{}
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 9)}}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	observation := observeCapture(t, session)
	result, err := session.FindColor(context.Background(), FindColorRequest{
		ObservationID: observation.ObservationID,
		Condition:     ColorCondition{Red: 9, Green: 10, Blue: 11},
	})
	if err != nil || result.Status != ConditionMatched {
		t.Fatalf("FindColor = %+v, %v", result, err)
	}
	if len(sink.events) != 4 {
		t.Fatalf("audit events = %+v", sink.events)
	}
	started, finished := sink.events[2], sink.events[3]
	if started.SchemaVersion != AuditSchemaVersion || started.Kind != AuditConditionStarted ||
		started.ConditionID != result.ConditionID || started.ObservationID != observation.ObservationID ||
		started.ConditionStatus != "" || started.ConditionAttempts != 0 {
		t.Fatalf("condition started audit = %+v", started)
	}
	if finished.Kind != AuditConditionFinished || finished.ConditionStatus != ConditionMatched ||
		finished.ConditionAttempts != 1 || finished.ErrorCode != "" {
		t.Fatalf("condition finished audit = %+v", finished)
	}
	serialized, err := json.Marshal(sink.events[2:])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sha256", "pixels", "tolerance", `"red"`, `"green"`, `"blue"`} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatalf("condition audit leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestWaitColorCompletionAuditFailureReturnsOwnedObservation(t *testing.T) {
	policy, err := preparePolicy(conditionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(2, 2, 9)}}
	sink := &recordingAuditSink{failAt: 2}
	session, err := newSessionWithAudit(policy, driver, availableCapabilities(), sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	result, waitErr := session.WaitColor(context.Background(), WaitColorRequest{
		Region:    CaptureRegion{Width: 2, Height: 2, DisplayID: 0},
		Condition: ColorCondition{Red: 9, Green: 10, Blue: 11},
	})
	if result.ObservationID == "" || result.Status != ConditionMatched || !errors.Is(waitErr, ErrAuditDelivery) {
		t.Fatalf("WaitColor = (%+v, %v)", result, waitErr)
	}
	if err := session.ReleaseObservation(result.ObservationID); err != nil {
		t.Fatal(err)
	}
}

func TestVisualConditionPolicyMustBeBounded(t *testing.T) {
	tests := []Policy{
		{AllowedOperations: []Operation{OperationFindColor}, MaxQueries: 1},
		{AllowedOperations: []Operation{OperationObserve, OperationFindColor}, MaxObservations: 1},
		{AllowedOperations: []Operation{OperationObserve, OperationFindColor}, MaxObservations: 1, MaxQueries: 1},
		{AllowedOperations: []Operation{OperationObserve, OperationFindColor}, MaxObservations: 1, MaxQueries: 1, MaxCapturePixels: 1},
		{AllowedOperations: []Operation{OperationWaitColor}, AllowedDisplayIDs: []int{0}, MaxQueries: 1, MaxObservations: 1, MaxCapturePixels: 1},
		{AllowedOperations: []Operation{OperationWaitColor}, AllowedDisplayIDs: []int{0}, MaxQueries: 1, MaxObservations: 1, MaxCapturePixels: 1, WaitAttempts: 1},
		{AllowedOperations: []Operation{OperationWaitColor}, AllowedDisplayIDs: []int{0}, MaxQueries: 1, MaxObservations: 1, MaxCapturePixels: 1, WaitTimeoutMillis: 1},
		{AllowedOperations: []Operation{OperationWaitColor}, MaxQueries: 1, MaxObservations: 1, MaxCapturePixels: 1, WaitAttempts: 1, WaitTimeoutMillis: 1},
	}
	for _, policy := range tests {
		if _, err := preparePolicy(policy); err == nil {
			t.Fatalf("unbounded visual-condition policy was accepted: %+v", policy)
		}
	}
}

func TestActionAPIRejectsVisualQueryOperations(t *testing.T) {
	session := newTestSession(t, conditionPolicy(), &fakeDriver{})
	for _, operation := range []Operation{OperationFindColor, OperationWaitColor} {
		result, err := session.Execute(context.Background(), ActionRequest{
			Operation: operation,
			Move:      &MoveAction{DisplayID: 0},
		})
		if !hasErrorCode(err, ErrorInvalidInput) || result.Error == nil {
			t.Fatalf("Execute(%s) = %+v, %v", operation, result, err)
		}
	}
}

func hasErrorCode(err error, code ErrorCode) bool {
	var actionErr *ActionError
	return errors.As(err, &actionErr) && actionErr.Code == code
}

func TestWaitForConditionHonorsCancellationWithoutSleeping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := waitForCondition(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForCondition = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("canceled wait took %v", elapsed)
	}
}

func TestColorMatchesUsesNormalizedEuclideanTolerance(t *testing.T) {
	if !colorMatches(1, 2, 3, ColorCondition{Red: 1, Green: 2, Blue: 3}) {
		t.Fatal("exact RGB match failed")
	}
	if colorMatches(1, 2, 4, ColorCondition{Red: 1, Green: 2, Blue: 3}) {
		t.Fatal("exact RGB mismatch passed")
	}
	condition := ColorCondition{Tolerance: 0.5}
	if !colorMatches(127, 127, 127, condition) {
		t.Fatal("color inside normalized tolerance did not match")
	}
	if colorMatches(128, 128, 128, condition) {
		t.Fatal("color outside normalized tolerance matched")
	}
}
