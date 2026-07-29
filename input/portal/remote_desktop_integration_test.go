//go:build linux && integration

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

const envRemoteDesktopE2E = "ROBOTGO_REMOTE_DESKTOP_E2E"
const (
	envPortalConsentReadyFile = "ROBOTGO_PORTAL_CONSENT_READY_FILE"
	envPortalConsentStartGate = "ROBOTGO_PORTAL_CONSENT_START_GATE_FILE"
)

func TestRemoteDesktopPortalRuntime(t *testing.T) {
	if os.Getenv(envRemoteDesktopE2E) == "" {
		t.Skip("set ROBOTGO_REMOTE_DESKTOP_E2E=1 to allow a portal consent dialog and pointer motion")
	}
	stage := "probe"
	defer reportPortalStageOnFailure(t, &stage)

	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
	capability, err := Probe(probeCtx)
	cancelProbe()
	if err != nil {
		t.Fatalf("Probe error: %v", err)
	}
	devices := DeviceKeyboard | DevicePointer
	if capability.Supports(DeviceTouchscreen) {
		devices |= DeviceTouchscreen
	}
	if !capability.Supports(devices) {
		t.Fatalf("RemoteDesktop portal does not advertise keyboard and pointer input: %+v", capability)
	}
	if !capability.SupportsSources(SourceMonitor) {
		t.Fatalf("ScreenCast portal does not advertise monitor sources: %+v", capability)
	}
	if !capability.SupportsCursorMode(CursorHidden) {
		t.Fatalf("ScreenCast portal does not advertise hidden cursor mode: %+v", capability)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	options := OpenOptions{
		Devices: devices, Sources: SourceMonitor,
		CursorMode: CursorHidden,
	}
	expectedOutputs, multiOutput := expectedPortalOutputs(t)
	options.Multiple = multiOutput
	stage = "open"
	session, err := openWithOptionsBeforeStart(
		ctx,
		options,
		func() error {
			signalPortalConsentReady(t, "remote-desktop")
			waitForPortalConsentStart(t, ctx, "remote-desktop")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("OpenWithOptions error: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = session.Close()
		}
	}()
	stage = "validate-session"
	if granted := session.Devices(); granted&devices != devices {
		t.Fatalf("granted devices=%d, want all requested devices=%d", granted, devices)
	}
	runPortalEvent(t, "first PointerMotion", func(eventCtx context.Context) error {
		return session.PointerMotion(eventCtx, 1, 0)
	})
	runPortalEvent(t, "second PointerMotion", func(eventCtx context.Context) error {
		return session.PointerMotion(eventCtx, -1, 0)
	})
	streams := session.Streams()
	if len(streams) == 0 {
		t.Fatal("portal session returned no ScreenCast streams")
	}
	stage = "input-events"
	if multiOutput {
		validateRemoteDesktopStreams(
			t,
			&stage,
			streams,
			expectedOutputs,
			capability.ScreenCastVersion,
		)
		for index, stream := range streams {
			stage = fmt.Sprintf("input-output-%d", index+1)
			stream := stream
			x, y := remoteDesktopStreamCoordinates(stream)
			runPortalEvent(
				t,
				fmt.Sprintf("PointerMotionAbsolute output %d", index+1),
				func(eventCtx context.Context) error {
					return session.PointerMotionAbsolute(
						eventCtx,
						stream.NodeID,
						x,
						y,
					)
				},
			)
		}
	} else {
		stream := streams[0]
		runPortalEvent(t, "PointerMotionAbsolute", func(eventCtx context.Context) error {
			return session.PointerMotionAbsolute(eventCtx, stream.NodeID, 1, 1)
		})
	}
	if devices&DeviceTouchscreen != 0 {
		stream := streams[0]
		runPortalEvent(t, "TouchDown", func(eventCtx context.Context) error {
			return session.TouchDown(eventCtx, stream.NodeID, 0, 1, 1)
		})
		runPortalEvent(t, "TouchUp", func(eventCtx context.Context) error {
			return session.TouchUp(eventCtx, 0)
		})
	}
	// A modifier-only tap validates keyboard injection without typing text into
	// whichever application happens to own focus on the interactive runner.
	runPortalEvent(t, "modifier press", func(eventCtx context.Context) error {
		return session.KeyboardKeysym(eventCtx, 0xffe1, true)
	})
	runPortalEvent(t, "modifier release", func(eventCtx context.Context) error {
		return session.KeyboardKeysym(eventCtx, 0xffe1, false)
	})
	stage = "close"
	if err := session.Close(); err != nil {
		t.Fatalf("portal session Close error: %v", err)
	}
	closed = true
}

func expectedPortalOutputs(
	t *testing.T,
) (portalrunner.HostedDisplay, bool) {
	t.Helper()
	multiOutput := os.Getenv(portalrunner.PortalMultiOutputEnvKey)
	encoded := os.Getenv(portalrunner.PortalExpectedOutputsEnvKey)
	if multiOutput == "" {
		if encoded != "" {
			t.Fatal("portal expected outputs require multi-output mode")
		}
		return portalrunner.HostedDisplay{}, false
	}
	if multiOutput != "1" {
		t.Fatal("portal multi-output marker is invalid")
	}
	display, err := portalrunner.ParseHostedDisplay(encoded)
	if err != nil {
		t.Fatalf("parse expected portal outputs: %v", err)
	}
	return display, true
}

func validateRemoteDesktopStreams(
	t *testing.T,
	stage *string,
	streams []Stream,
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
			Monitor:        stream.SourceType == SourceMonitor,
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
		t.Fatalf("validate portal multi-output evidence: %v", err)
	}
}

func remoteDesktopStreamCoordinates(stream Stream) (float64, float64) {
	x, y := float64(1), float64(1)
	if stream.HasSize && stream.Size.Width > 0 {
		x = float64(stream.Size.Width) / 2
	}
	if stream.HasSize && stream.Size.Height > 0 {
		y = float64(stream.Size.Height) / 2
	}
	return x, y
}

func TestRemoteDesktopStreamCoordinates(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		stream Stream
		wantX  float64
		wantY  float64
	}{
		{
			name: "portal omitted optional size",
			stream: Stream{
				Size: Size{Width: 1280, Height: 720},
			},
			wantX: 1, wantY: 1,
		},
		{
			name: "logical center",
			stream: Stream{
				HasSize: true,
				Size:    Size{Width: 1280, Height: 720},
			},
			wantX: 640, wantY: 360,
		},
		{
			name: "minimum safe coordinate",
			stream: Stream{
				HasSize: true,
				Size:    Size{Width: 1, Height: 1},
			},
			wantX: 0.5, wantY: 0.5,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			x, y := remoteDesktopStreamCoordinates(test.stream)
			if x != test.wantX || y != test.wantY {
				t.Fatalf(
					"remoteDesktopStreamCoordinates() = (%g,%g), want (%g,%g)",
					x,
					y,
					test.wantX,
					test.wantY,
				)
			}
		})
	}
}

func reportPortalStageOnFailure(t *testing.T, stage *string) {
	t.Helper()
	if t.Failed() {
		t.Logf("ROBOTGO_PORTAL_STAGE=%s", *stage)
	}
}

func TestRemoteDesktopConsentMarkerCleanup(t *testing.T) {
	runtimeDirectory := t.TempDir()
	marker := filepath.Join(
		runtimeDirectory,
		"robotgo-portal-consent-remote-desktop.ready",
	)
	startGate := filepath.Join(
		runtimeDirectory,
		"robotgo-portal-consent-remote-desktop.start",
	)
	if err := os.WriteFile(startGate, []byte("start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
	t.Setenv(envPortalConsentReadyFile, marker)
	t.Setenv(envPortalConsentStartGate, startGate)
	t.Run("lifecycle", func(t *testing.T) {
		signalPortalConsentReady(t, "remote-desktop")
		waitForPortalConsentStart(
			t,
			context.Background(),
			"remote-desktop",
		)
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
		if string(content) != "remote-desktop\n" {
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

func signalPortalConsentReady(t *testing.T, cell string) {
	t.Helper()
	path := os.Getenv(envPortalConsentReadyFile)
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

func waitForPortalConsentStart(
	t *testing.T,
	ctx context.Context,
	cell string,
) {
	t.Helper()
	path := os.Getenv(envPortalConsentStartGate)
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

func runPortalEvent(t *testing.T, action string, event func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := event(ctx); err != nil {
		t.Fatalf("portal %s error: %v", action, err)
	}
}
