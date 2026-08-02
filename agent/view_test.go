package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	robotgo "github.com/marang/robotgo"
)

func TestNewImageViewRejectsMismatchedOrMetadataBearingPNGWithoutTakingOwnership(t *testing.T) {
	metadata := ViewMetadata{
		SchemaVersion: ViewSchemaVersion, ObservationID: "observation-94",
		CreatedAt: time.Unix(1, 0).UTC(),
		Region:    CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
		Width:     1, Height: 1, Backend: "custom", MIMEType: ViewMIMEType,
	}
	wrongSize := encodeTestPNG(t, image.NewRGBA(image.Rect(0, 0, 2, 1)))
	expected := append([]byte(nil), wrongSize...)
	if view, err := NewImageView(metadata, wrongSize); err == nil || view != nil {
		t.Fatal("accepted PNG dimensions that disagree with view metadata")
	}
	if !bytes.Equal(wrongSize, expected) {
		t.Fatal("failed constructor modified caller-owned PNG bytes")
	}

	metadataChunk := []byte("sensitive comment")
	chunk := make([]byte, 12+len(metadataChunk))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(metadataChunk)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:], metadataChunk)
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write(chunk[4 : 8+len(metadataChunk)])
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], checksum.Sum32())
	plain := encodeTestPNG(t, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	withMetadata := append(append([]byte(nil), plain[:len(plain)-12]...), chunk...)
	withMetadata = append(withMetadata, plain[len(plain)-12:]...)
	if view, err := NewImageView(metadata, withMetadata); err == nil || view != nil {
		t.Fatal("accepted PNG ancillary metadata")
	}

	metadata.Region.Width = 2
	metadata.Downscaled = false
	plain = encodeTestPNG(t, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	expected = append(expected[:0], plain...)
	if view, err := NewImageView(metadata, plain); err == nil || view != nil {
		t.Fatal("accepted inconsistent downscaled metadata")
	}
	if !bytes.Equal(plain, expected) {
		t.Fatal("failed constructor modified caller-owned PNG after metadata rejection")
	}
}

func TestCaptureImageWithContextBoundsSynchronousBackendAndWipesLateResult(t *testing.T) {
	lateImage := syntheticCapture(2, 2, 91)
	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	begin := time.Now()
	img, err := captureImageWithContext(ctx, func() (image.Image, error) {
		close(started)
		<-release
		return lateImage, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || img != nil {
		t.Fatalf("bounded capture = (%v, %v), want deadline error", img, err)
	}
	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		t.Fatalf("bounded capture returned after %s", elapsed)
	}
	<-started
	close(release)
	deadline := time.Now().Add(time.Second)
	for !allZeroBytes(lateImage.Pix) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !allZeroBytes(lateImage.Pix) {
		t.Fatal("late native capture retained sensitive pixels")
	}
}

func TestViewDeadlineBoundsDisplayLookupAndAllowsClose(t *testing.T) {
	for _, fullDisplay := range []bool{false, true} {
		name := "region"
		if fullDisplay {
			name = "full-display"
		}
		t.Run(name, func(t *testing.T) {
			policy := viewPolicy()
			policy.ViewTimeoutMillis = 20
			policy.AllowFullDisplayView = fullDisplay
			if fullDisplay {
				policy.AllowedViewRegions = nil
			}
			boundsGo := make(chan struct{})
			boundsDone := make(chan struct{}, 1)
			driver := &fakeDriver{
				boundsHit:  make(chan struct{}, 1),
				boundsGo:   boundsGo,
				boundsDone: boundsDone,
			}
			session := newTestSession(t, policy, driver)
			region := CaptureRegion{Width: 4, Height: 2, DisplayID: 0}
			displayID := 0
			request := ViewRequest{Region: &region}
			if fullDisplay {
				request = ViewRequest{FullDisplayID: &displayID}
			}

			started := time.Now()
			view, err := session.View(context.Background(), request)
			if view != nil || !hasErrorCode(err, ErrorTimedOut) {
				t.Fatalf("View = (%v, %v), want timed-out bounds lookup", view, err)
			}
			if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
				t.Fatalf("bounded view returned after %s", elapsed)
			}
			select {
			case <-driver.boundsHit:
			default:
				t.Fatal("view did not reach display-bounds lookup")
			}
			if driver.captureCount() != 0 {
				t.Fatal("timed-out bounds lookup reached capture")
			}

			closed := make(chan error, 1)
			go func() { closed <- session.Close() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Session.Close blocked behind timed-out display-bounds lookup")
			}
			close(boundsGo)
			select {
			case <-boundsDone:
			case <-time.After(time.Second):
				t.Fatal("display-bounds worker did not finish after release")
			}
		})
	}
}

func TestViewLineageRecaptureUsesViewDeadlineAndSessionCancellation(t *testing.T) {
	for _, closeSession := range []bool{false, true} {
		name := "view-deadline"
		if closeSession {
			name = "session-close"
		}
		t.Run(name, func(t *testing.T) {
			policy := viewPolicy()
			policy.AllowedOperations = append(policy.AllowedOperations, OperationClick)
			policy.AllowedMouseButtons = []MouseButton{MouseButtonLeft}
			policy.MaxActions = 1
			policy.MaxViewWidth = 4
			policy.ViewTimeoutMillis = 20
			if closeSession {
				policy.ViewTimeoutMillis = 5000
			}
			driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(4, 2, 17)}}
			session := newTestSession(t, policy, driver)
			region := CaptureRegion{Width: 4, Height: 2, DisplayID: 0}
			view, err := session.View(context.Background(), ViewRequest{Region: &region})
			if err != nil {
				t.Fatal(err)
			}
			data, err := view.TakePNG()
			if err != nil {
				t.Fatal(err)
			}
			clear(data)

			boundsGo := make(chan struct{})
			boundsDone := make(chan struct{}, 1)
			driver.boundsHit = make(chan struct{}, 1)
			driver.boundsGo = boundsGo
			driver.boundsDone = boundsDone
			executed := make(chan error, 1)
			started := time.Now()
			go func() {
				_, executeErr := session.Execute(context.Background(), ActionRequest{
					Operation: OperationClick,
					Click:     &ClickAction{Button: MouseButtonLeft},
					Precondition: &ObservationPrecondition{
						ObservationID: view.Metadata.ObservationID,
					},
				})
				executed <- executeErr
			}()
			select {
			case <-driver.boundsHit:
			case <-time.After(time.Second):
				t.Fatal("lineage recapture did not reach display-bounds lookup")
			}

			if closeSession {
				closed := make(chan error, 1)
				go func() { closed <- session.Close() }()
				select {
				case err := <-closed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(500 * time.Millisecond):
					t.Fatal("Session.Close blocked behind view-lineage recapture")
				}
			}
			select {
			case err := <-executed:
				wantCode := ErrorTimedOut
				if closeSession {
					wantCode = ErrorCanceled
				}
				if !hasErrorCode(err, wantCode) {
					t.Fatalf("lineage Execute error = %v, want %s", err, wantCode)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("view-lineage action did not honor its bounded context")
			}
			if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
				t.Fatalf("bounded lineage action returned after %s", elapsed)
			}
			if driver.callCount() != 0 {
				t.Fatal("timed-out lineage recapture reached input dispatch")
			}

			close(boundsGo)
			select {
			case <-boundsDone:
			case <-time.After(time.Second):
				t.Fatal("lineage bounds worker did not finish after release")
			}
		})
	}
}

func allZeroBytes(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func encodeTestPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func viewPolicy() Policy {
	return Policy{
		AllowedOperations:  []Operation{OperationView},
		AllowedDisplayIDs:  []int{0},
		AllowedViewRegions: []CaptureRegion{{X: 0, Y: 0, Width: 4, Height: 2, DisplayID: 0}},
		MaxObservations:    8, SessionTimeoutMillis: 10_000,
		MaxViewSourcePixels: 8, MaxViewEncodedBytes: 4096,
		MaxViewWidth: 2, MaxViewHeight: 2, MaxViews: 4,
		MaxConcurrentViews: 1, MinViewIntervalMillis: 1, ViewTimeoutMillis: 1000,
	}
}

func TestViewIsDenyByDefaultBeforeDesktopIO(t *testing.T) {
	driver := &fakeDriver{}
	session := newTestSession(t, Policy{AllowedOperations: []Operation{OperationObserve}, MaxObservations: 1}, driver)
	_, err := session.View(context.Background(), ViewRequest{Region: &CaptureRegion{
		Width: 1, Height: 1, DisplayID: 0,
	}})
	if !hasErrorCode(err, ErrorPolicyDenied) {
		t.Fatalf("View error = %v", err)
	}
	if driver.captureCount() != 0 {
		t.Fatal("denied image view reached the desktop capture backend")
	}
}

func TestViewRedactsDownscalesEncodesInMemoryAndRetainsLineage(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(20 + x), G: uint8(40 + y), B: 60, A: 255})
		}
	}
	policy := viewPolicy()
	policy.ViewRedactionMasks = []CaptureRegion{{X: 2, Y: 0, Width: 2, Height: 2, DisplayID: 0}}
	driver := &fakeDriver{captureImages: []image.Image{source}}
	session := newTestSession(t, policy, driver)

	view, err := session.View(context.Background(), ViewRequest{Region: &CaptureRegion{
		X: 0, Y: 0, Width: 4, Height: 2, DisplayID: 0,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if view.Metadata.Width != 2 || view.Metadata.Height != 1 ||
		!view.Metadata.Downscaled || !view.Metadata.Redacted ||
		view.Metadata.Backend != "fake-capture" || view.Metadata.MIMEType != ViewMIMEType {
		t.Fatalf("view metadata = %+v", view.Metadata)
	}
	for index, value := range source.Pix {
		if value != 0 {
			t.Fatalf("desktop source byte %d was not zeroed: %d", index, value)
		}
	}
	encoded, err := view.TakePNG()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(encoded)
	if _, err := view.TakePNG(); !errors.Is(err, ErrObservationClosed) {
		t.Fatalf("second TakePNG error = %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	if decoded.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Fatalf("decoded bounds = %v", decoded.Bounds())
	}
	if got := color.RGBAModel.Convert(decoded.At(1, 0)).(color.RGBA); got != (color.RGBA{A: 255}) {
		t.Fatalf("redacted output pixel = %#v", got)
	}
	for _, marker := range [][]byte{[]byte("tEXt"), []byte("iTXt"), []byte("eXIf"), []byte("tIME")} {
		if bytes.Contains(encoded, marker) {
			t.Fatalf("PNG contains metadata chunk %q", marker)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("image view created files: %v", entries)
	}
	record, ok := session.observation(view.Metadata.ObservationID)
	if !ok || record.source != OperationView || !record.capture.usable() {
		t.Fatalf("retained view lineage = %+v, ok=%v", record, ok)
	}
	if err := session.ReleaseObservation(view.Metadata.ObservationID); err != nil {
		t.Fatal(err)
	}
	if record.capture.usable() {
		t.Fatal("released view retained raw capture pixels")
	}
}

func TestViewRequiresDistinctFullDisplayGrantBeforeBoundsIO(t *testing.T) {
	policy := viewPolicy()
	driver := &fakeDriver{boundsHit: make(chan struct{}, 1)}
	session := newTestSession(t, policy, driver)
	displayID := 0
	_, err := session.View(context.Background(), ViewRequest{FullDisplayID: &displayID})
	if !hasErrorCode(err, ErrorPolicyDenied) {
		t.Fatalf("full-display error = %v", err)
	}
	select {
	case <-driver.boundsHit:
		t.Fatal("denied full-display view queried desktop bounds")
	default:
	}
}

func TestViewAuthorizationRejectsUnconfirmedAmbiguousAndOutOfScopeRequestsBeforeDesktopIO(t *testing.T) {
	for name, test := range map[string]struct {
		mutate  func(*Policy)
		request ViewRequest
	}{
		"confirmation": {
			mutate: func(policy *Policy) { policy.ConfirmOperations = []Operation{OperationView} },
			request: ViewRequest{Region: &CaptureRegion{
				Width: 1, Height: 1, DisplayID: 0,
			}},
		},
		"both selectors": {
			mutate: func(*Policy) {},
			request: func() ViewRequest {
				displayID := 0
				return ViewRequest{
					Region:        &CaptureRegion{Width: 1, Height: 1, DisplayID: 0},
					FullDisplayID: &displayID,
				}
			}(),
		},
		"outside allowlist": {
			mutate: func(*Policy) {},
			request: ViewRequest{Region: &CaptureRegion{
				X: 3, Width: 2, Height: 1, DisplayID: 0,
			}},
		},
		"source pixel limit": {
			mutate: func(policy *Policy) { policy.MaxViewSourcePixels = 4 },
			request: ViewRequest{Region: &CaptureRegion{
				Width: 4, Height: 2, DisplayID: 0,
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := viewPolicy()
			test.mutate(&policy)
			driver := &fakeDriver{}
			session := newTestSession(t, policy, driver)
			if view, err := session.View(context.Background(), test.request); err == nil || view != nil {
				t.Fatalf("View = (%v, %v)", view, err)
			}
			if driver.captureCount() != 0 {
				t.Fatalf("denied view reached capture %d times", driver.captureCount())
			}
		})
	}
}

func TestViewFullDisplayGrantUsesLiveBoundsAndStillEnforcesSourceLimit(t *testing.T) {
	policy := viewPolicy()
	policy.AllowedViewRegions = nil
	policy.AllowFullDisplayView = true
	policy.MaxViewSourcePixels = 8
	policy.MaxViewWidth = 4
	driver := &fakeDriver{
		bounds:        map[int]displayBounds{0: {x: 10, y: 20, width: 4, height: 2}},
		captureImages: []image.Image{syntheticCapture(4, 2, 11)},
	}
	session := newTestSession(t, policy, driver)
	displayID := 0
	view, err := session.View(context.Background(), ViewRequest{FullDisplayID: &displayID})
	if err != nil {
		t.Fatal(err)
	}
	if view.Metadata.Region != (CaptureRegion{X: 10, Y: 20, Width: 4, Height: 2, DisplayID: 0}) {
		t.Fatalf("full-display region = %+v", view.Metadata.Region)
	}
	data, err := view.TakePNG()
	if err != nil {
		t.Fatal(err)
	}
	clear(data)
	if err := session.ReleaseObservation(view.Metadata.ObservationID); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}

	policy.MaxViewSourcePixels = 7
	driver = &fakeDriver{bounds: map[int]displayBounds{0: {x: 10, y: 20, width: 4, height: 2}}}
	session = newTestSession(t, policy, driver)
	if view, err := session.View(context.Background(), ViewRequest{FullDisplayID: &displayID}); view != nil || !hasErrorCode(err, ErrorPolicyDenied) {
		t.Fatalf("oversized full display = (%v, %v)", view, err)
	}
	if driver.captureCount() != 0 {
		t.Fatal("oversized full display reached capture")
	}
}

func TestViewEnforcesRateCountAndEncodedByteLimits(t *testing.T) {
	policy := viewPolicy()
	policy.MaxViews = 1
	policy.MaxViewEncodedBytes = 8
	source := syntheticCapture(4, 2, 10)
	driver := &fakeDriver{captureImages: []image.Image{source}}
	session := newTestSession(t, policy, driver)
	now := time.Unix(100, 0)
	session.now = func() time.Time { return now }
	request := ViewRequest{Region: &CaptureRegion{Width: 4, Height: 2, DisplayID: 0}}
	view, err := session.View(context.Background(), request)
	if view != nil || !hasErrorCode(err, ErrorPolicyDenied) {
		t.Fatalf("encoded limit result = %v, %v", view, err)
	}
	for index, value := range source.Pix {
		if value != 0 {
			t.Fatalf("failed view source byte %d was not zeroed", index)
		}
	}
	if len(session.observations) != 0 {
		t.Fatal("failed encoded view retained an observation")
	}
	secondSource := syntheticCapture(4, 2, 20)
	driver.captureImages = append(driver.captureImages, secondSource)
	now = now.Add(time.Second)
	if _, err := session.View(context.Background(), request); !hasErrorCode(err, ErrorPolicyDenied) {
		t.Fatalf("view count error = %v", err)
	}
	if driver.captureCount() != 1 {
		t.Fatalf("capture calls after count denial = %d", driver.captureCount())
	}
}

func TestViewPortalGrantIsPassedOnlyFromImmutablePolicy(t *testing.T) {
	policy := viewPolicy()
	policy.AllowPortalView = true
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(4, 2, 30)}}
	session := newTestSession(t, policy, driver)
	view, err := session.View(context.Background(), ViewRequest{Region: &CaptureRegion{
		Width: 4, Height: 2, DisplayID: 0,
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := view.TakePNG()
	if err != nil {
		t.Fatal(err)
	}
	clear(data)
	if len(driver.viewPortalGrants) != 1 || !driver.viewPortalGrants[0] {
		t.Fatalf("portal grants = %v", driver.viewPortalGrants)
	}
}

func TestViewCapabilitySeparatesActivePortalAvailabilityFromPolicyGrant(t *testing.T) {
	policy, err := preparePolicy(viewPolicy())
	if err != nil {
		t.Fatal(err)
	}
	capabilities := availableCapabilities()
	capabilities.Runtime.GOOS = goOSLinux
	capabilities.Runtime.DisplayServer = robotgo.DisplayServerWayland
	capabilities.Capture = robotgo.FeatureCapability{
		Available: true, Backend: robotgo.FeatureBackendScreenCast,
	}
	capability := buildCatalog(policy, capabilities).Operations[1]
	if !capability.Available || capability.PolicyAllowed || capability.UnavailableCode != "" ||
		!strings.Contains(capability.Remediation, "allow_portal_view") {
		t.Fatalf("portal view capability without grant = %+v", capability)
	}
	driver := &fakeDriver{captureImages: []image.Image{syntheticCapture(4, 2, 30)}}
	session, err := newSession(policy, driver, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.View(context.Background(), ViewRequest{Region: &CaptureRegion{
		Width: 4, Height: 2, DisplayID: 0,
	}}); !hasErrorCode(err, ErrorPolicyDenied) {
		t.Fatalf("portal view without policy grant = %v", err)
	}
	if driver.captureCount() != 0 {
		t.Fatal("portal-policy denial reached desktop capture")
	}

	input := viewPolicy()
	input.AllowPortalView = true
	policy, err = preparePolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	capability = buildCatalog(policy, capabilities).Operations[1]
	if !capability.Available || !capability.PolicyAllowed {
		t.Fatalf("portal view capability with grant = %+v", capability)
	}
}

func TestViewObservationCanAuthorizeOnlyUnchangedRedactedLineage(t *testing.T) {
	policy := viewPolicy()
	policy.AllowedOperations = append(policy.AllowedOperations, OperationClick)
	policy.AllowedMouseButtons = []MouseButton{MouseButtonLeft}
	policy.MaxActions = 1
	policy.MaxViewWidth = 4
	policy.ViewRedactionMasks = []CaptureRegion{{X: 2, Y: 0, Width: 2, Height: 2, DisplayID: 0}}
	initial := syntheticCapture(4, 2, 10)
	recapture := syntheticCapture(4, 2, 10)
	for y := 0; y < 2; y++ {
		for x := 2; x < 4; x++ {
			recapture.SetRGBA(x, y, color.RGBA{R: 250, A: 255})
		}
	}
	driver := &fakeDriver{captureImages: []image.Image{initial, recapture}}
	session := newTestSession(t, policy, driver)
	view, err := session.View(context.Background(), ViewRequest{Region: &CaptureRegion{
		Width: 4, Height: 2, DisplayID: 0,
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := view.TakePNG()
	if err != nil {
		t.Fatal(err)
	}
	clear(data)
	result, err := session.Execute(context.Background(), ActionRequest{
		Operation: OperationClick, Click: &ClickAction{Button: MouseButtonLeft},
		Precondition: &ObservationPrecondition{ObservationID: view.Metadata.ObservationID},
	})
	if err != nil || result.Status != ActionSucceeded {
		t.Fatalf("redacted lineage action = %+v, %v", result, err)
	}
}

func TestPreparePolicyRejectsIncompleteViewAndExcessConcurrency(t *testing.T) {
	policy := viewPolicy()
	policy.MaxConcurrentViews = 2
	if _, err := preparePolicy(policy); err == nil {
		t.Fatal("view policy accepted concurrent desktop image reads")
	}
	policy = viewPolicy()
	policy.AllowedViewRegions = nil
	if _, err := preparePolicy(policy); err == nil {
		t.Fatal("view policy accepted no region and no full-display grant")
	}
}
