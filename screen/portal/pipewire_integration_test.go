//go:build linux && cgo && pipewire && integration

package portal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marang/robotgo/internal/portalrunner"
)

const envScreenCastRequireMonitor = "ROBOTGO_SCREENCAST_REQUIRE_MONITOR"

func TestPipeWireCapturePersistentSessionIntegration(t *testing.T) {
	if os.Getenv("ROBOTGO_SCREENCAST_E2E") == "" {
		t.Skip("set ROBOTGO_SCREENCAST_E2E=1 in a graphical Wayland session")
	}
	stage := "preflight"
	defer reportScreenCastStageOnFailure(t, &stage)
	expectedOutputs, multiOutput := expectedScreenCastOutputs(t)
	if multiOutput {
		probeCtx, cancelProbe := context.WithTimeout(
			context.Background(),
			2*time.Second,
		)
		capability, err := ProbeScreenCast(probeCtx)
		cancelProbe()
		if err != nil {
			t.Fatalf("ProbeScreenCast error: %v", err)
		}
		testMultiplePipeWireStreams(
			t,
			&stage,
			expectedOutputs,
			capability.Version,
		)
		return
	}
	openCtx, cancelOpen := context.WithTimeout(context.Background(), 2*time.Minute)
	capture, err := openPipeWireCapture(
		openCtx,
		ScreenCastOptions{
			Sources: ScreenCastSourceMonitor,
			Cursor:  ScreenCastCursorEmbedded,
			Persist: ScreenCastPersistApplication,
		},
		0,
		func(current string) {
			stage = current
		},
		func(
			ctx context.Context,
			options ScreenCastOptions,
		) (ScreenCast, error) {
			return openScreenCastBeforeStart(
				ctx,
				options,
				func() error {
					signalScreenCastConsentReady(t)
					waitForScreenCastConsentStart(t, ctx)
					return nil
				},
			)
		},
	)
	cancelOpen()
	if err != nil {
		stage = screenCastOpenFailureStage(stage, err)
		t.Fatalf("OpenPipeWireCapture error: %v", err)
	}
	defer func() {
		if err := capture.Close(); err != nil {
			stage = "close"
			t.Errorf("Close error: %v", err)
		}
	}()
	stage = "stream-metadata"
	if os.Getenv(envScreenCastRequireMonitor) != "" &&
		capture.SelectedStream().SourceType != ScreenCastSourceMonitor {
		t.Fatalf(
			"selected source type = %d, want physical monitor",
			capture.SelectedStream().SourceType,
		)
	}

	for frameNumber := 1; frameNumber <= 2; frameNumber++ {
		stage = []string{"capture-1", "capture-2"}[frameNumber-1]
		frameCtx, cancelFrame := context.WithTimeout(context.Background(), 10*time.Second)
		frame, err := capture.Capture(frameCtx, 0, 0, 0, 0)
		cancelFrame()
		if err != nil {
			t.Fatalf("capture frame %d: %v", frameNumber, err)
		}
		if frame.Bounds().Empty() {
			t.Fatalf("frame %d is empty", frameNumber)
		}
	}
}

func testMultiplePipeWireStreams(
	t *testing.T,
	stage *string,
	expected portalrunner.HostedDisplay,
	screenCastVersion uint32,
) {
	t.Helper()
	if !pipeWireCaptureCompiled() {
		t.Fatal("PipeWire capture backend is not compiled")
	}
	*stage = "portal-open"
	openCtx, cancelOpen := context.WithTimeout(
		context.Background(),
		2*time.Minute,
	)
	session, err := openScreenCastBeforeStart(
		openCtx,
		ScreenCastOptions{
			Sources:  ScreenCastSourceMonitor,
			Multiple: true,
			Cursor:   ScreenCastCursorEmbedded,
			Persist:  ScreenCastPersistApplication,
		},
		func() error {
			signalScreenCastConsentReady(t)
			waitForScreenCastConsentStart(t, openCtx)
			return nil
		},
	)
	cancelOpen()
	if err != nil {
		if classified := screenCastPortalFailureStage(err); classified != "" {
			*stage = classified
		}
		t.Fatalf("OpenScreenCast multi-output error: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			*stage = "close"
			if err := session.Close(); err != nil {
				t.Errorf("multi-output session Close error: %v", err)
			}
		}
	}()
	*stage = "stream-metadata"
	streams := session.Streams()
	validateScreenCastStreams(
		t,
		stage,
		streams,
		expected,
		screenCastVersion,
	)
	for index, stream := range streams {
		*stage = fmt.Sprintf("pipewire-open-%d", index+1)
		backendCtx, cancelBackend := context.WithTimeout(
			context.Background(),
			15*time.Second,
		)
		backend, err := newPipeWireFrameSource(
			backendCtx,
			session,
			stream,
		)
		cancelBackend()
		if err != nil {
			t.Fatalf("open PipeWire output %d: %v", index+1, err)
		}
		*stage = fmt.Sprintf("capture-output-%d", index+1)
		frameCtx, cancelFrame := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		frame, frameErr := backend.frame(frameCtx)
		cancelFrame()
		closeErr := backend.close()
		if frameErr != nil || closeErr != nil {
			t.Fatalf(
				"capture PipeWire output %d: %v",
				index+1,
				errors.Join(frameErr, closeErr),
			)
		}
		if frame == nil || frame.Bounds().Empty() {
			t.Fatalf("PipeWire output %d returned an empty frame", index+1)
		}
		frame = nil
	}
	*stage = "close"
	if err := session.Close(); err != nil {
		t.Fatalf("multi-output session Close error: %v", err)
	}
	closed = true
}

func expectedScreenCastOutputs(
	t *testing.T,
) (portalrunner.HostedDisplay, bool) {
	t.Helper()
	multiOutput := os.Getenv(portalrunner.PortalMultiOutputEnvKey)
	encoded := os.Getenv(portalrunner.PortalExpectedOutputsEnvKey)
	if multiOutput == "" {
		if encoded != "" {
			t.Fatal("ScreenCast expected outputs require multi-output mode")
		}
		return portalrunner.HostedDisplay{}, false
	}
	if multiOutput != "1" {
		t.Fatal("ScreenCast multi-output marker is invalid")
	}
	display, err := portalrunner.ParseHostedDisplay(encoded)
	if err != nil {
		t.Fatalf("parse expected ScreenCast outputs: %v", err)
	}
	return display, true
}

func validateScreenCastStreams(
	t *testing.T,
	stage *string,
	streams []ScreenCastStream,
	expected portalrunner.HostedDisplay,
	screenCastVersion uint32,
) {
	t.Helper()
	evidence := make(
		[]portalrunner.HostedStreamEvidence,
		0,
		len(streams),
	)
	for _, stream := range streams {
		evidence = append(evidence, portalrunner.HostedStreamEvidence{
			NodeID: stream.NodeID, ID: stream.ID,
			MappingID:      stream.MappingID,
			PipeWireSerial: stream.PipeWireSerial,
			Monitor:        stream.SourceType == ScreenCastSourceMonitor,
			HasPosition:    stream.HasPosition,
			HasSize:        stream.HasSize,
			X:              int(stream.Position.X),
			Y:              int(stream.Position.Y),
			Width:          int(stream.Size.Width),
			Height:         int(stream.Size.Height),
		})
	}
	if err := portalrunner.ValidateHostedStreamEvidence(
		expected,
		screenCastVersion,
		evidence,
	); err != nil {
		if category := portalrunner.HostedStreamEvidenceFailureStage(err); category != "" {
			*stage = "streams-" + category
		}
		t.Fatalf("validate ScreenCast multi-output evidence: %v", err)
	}
}

func screenCastPortalFailureStage(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "portal-timeout"
	case errors.Is(err, ErrScreenCastCancelled):
		return "portal-cancelled"
	case errors.Is(err, ErrScreenCastRejected):
		return "portal-rejected"
	case errors.Is(err, ErrScreenCastUnavailable):
		return "portal-unavailable"
	default:
		return ""
	}
}

func screenCastOpenFailureStage(current string, err error) string {
	if current != "portal-open" {
		return current
	}
	if classified := screenCastPortalFailureStage(err); classified != "" {
		return classified
	}
	return current
}

func TestScreenCastPortalFailureStage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "timeout", err: context.DeadlineExceeded, want: "portal-timeout"},
		{name: "cancelled", err: ErrScreenCastCancelled, want: "portal-cancelled"},
		{name: "rejected", err: ErrScreenCastRejected, want: "portal-rejected"},
		{name: "unavailable", err: ErrScreenCastUnavailable, want: "portal-unavailable"},
		{name: "detailed callback stage retained", err: errors.New("pipewire"), want: ""},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := screenCastPortalFailureStage(test.err); got != test.want {
				t.Fatalf("screenCastPortalFailureStage() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestScreenCastOpenFailureStagePreservesBackendPhase(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		current string
		err     error
		want    string
	}{
		{
			name:    "portal timeout",
			current: "portal-open",
			err:     context.DeadlineExceeded,
			want:    "portal-timeout",
		},
		{
			name:    "PipeWire startup timeout",
			current: "pipewire-open",
			err:     context.DeadlineExceeded,
			want:    "pipewire-open",
		},
		{
			name:    "stream selection cancellation",
			current: "stream-select",
			err:     ErrScreenCastCancelled,
			want:    "stream-select",
		},
		{
			name:    "unclassified portal failure",
			current: "portal-open",
			err:     errors.New("portal"),
			want:    "portal-open",
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := screenCastOpenFailureStage(
				test.current,
				test.err,
			); got != test.want {
				t.Fatalf(
					"screenCastOpenFailureStage() = %q, want %q",
					got,
					test.want,
				)
			}
		})
	}
}

func reportScreenCastStageOnFailure(t *testing.T, stage *string) {
	t.Helper()
	if t.Failed() {
		t.Logf("ROBOTGO_PORTAL_STAGE=%s", *stage)
	}
}

func TestScreenCastConsentMarkerCleanup(t *testing.T) {
	runtimeDirectory := t.TempDir()
	marker := filepath.Join(
		runtimeDirectory,
		"robotgo-portal-consent-screencast.ready",
	)
	startGate := filepath.Join(
		runtimeDirectory,
		"robotgo-portal-consent-screencast.start",
	)
	if err := os.WriteFile(startGate, []byte("start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
	t.Setenv("ROBOTGO_PORTAL_CONSENT_READY_FILE", marker)
	t.Setenv("ROBOTGO_PORTAL_CONSENT_START_GATE_FILE", startGate)
	t.Run("lifecycle", func(t *testing.T) {
		signalScreenCastConsentReady(t)
		waitForScreenCastConsentStart(t, context.Background())
		info, err := os.Lstat(marker)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("consent marker mode = %v", info.Mode())
		}
		content, err := os.ReadFile(marker)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "screencast\n" {
			t.Fatalf("consent marker content = %q", content)
		}
	})
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("consent marker survived test cleanup: %v", err)
	}
	if _, err := os.Lstat(startGate); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("consent start gate survived test cleanup: %v", err)
	}
}

func signalScreenCastConsentReady(t *testing.T) {
	t.Helper()
	const (
		cell    = "screencast"
		envName = "ROBOTGO_PORTAL_CONSENT_READY_FILE"
	)
	path := os.Getenv(envName)
	if path == "" {
		return
	}
	runtimeDirectory := filepath.Clean(os.Getenv("XDG_RUNTIME_DIR"))
	if !filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		filepath.Dir(path) != runtimeDirectory ||
		filepath.Base(path) != "robotgo-portal-consent-"+cell+".ready" {
		t.Fatal("portal consent readiness path is outside the private runtime directory")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create portal consent readiness marker: %v", err)
	}
	if _, err := file.WriteString(cell + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		t.Fatalf("write portal consent readiness marker: %v", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		t.Fatalf("close portal consent readiness marker: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("remove portal consent readiness marker: %v", err)
		}
	})
}

func waitForScreenCastConsentStart(t *testing.T, ctx context.Context) {
	t.Helper()
	const (
		cell    = "screencast"
		envName = "ROBOTGO_PORTAL_CONSENT_START_GATE_FILE"
	)
	path := os.Getenv(envName)
	if path == "" {
		return
	}
	runtimeDirectory := filepath.Clean(os.Getenv("XDG_RUNTIME_DIR"))
	if !filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		filepath.Dir(path) != runtimeDirectory ||
		filepath.Base(path) != "robotgo-portal-consent-"+cell+".start" {
		t.Fatal("portal consent start gate is outside the private runtime directory")
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("remove portal consent start gate: %v", err)
		}
	})
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
	for {
		content, err := os.ReadFile(path)
		if err == nil {
			info, statErr := os.Lstat(path)
			if statErr != nil {
				t.Fatalf("inspect portal consent start gate: %v", statErr)
			}
			if !info.Mode().IsRegular() ||
				info.Mode().Perm() != 0o600 ||
				string(content) != "start\n" {
				t.Fatal("portal consent start gate is invalid")
			}
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read portal consent start gate: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for portal consent start gate: %v", ctx.Err())
		case <-poll.C:
		}
	}
}
