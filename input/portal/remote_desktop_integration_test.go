//go:build linux && integration

package portal_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	portalinput "github.com/marang/robotgo/input/portal"
	"github.com/marang/robotgo/internal/portalrunner"
)

const envRemoteDesktopE2E = "ROBOTGO_REMOTE_DESKTOP_E2E"
const envPortalConsentReadyFile = "ROBOTGO_PORTAL_CONSENT_READY_FILE"
const envPortalMultiOutput = "ROBOTGO_PORTAL_MULTI_OUTPUT"
const envPortalExpectedOutputs = "ROBOTGO_PORTAL_EXPECTED_OUTPUTS"

func TestRemoteDesktopPortalRuntime(t *testing.T) {
	if os.Getenv(envRemoteDesktopE2E) == "" {
		t.Skip("set ROBOTGO_REMOTE_DESKTOP_E2E=1 to allow a portal consent dialog and pointer motion")
	}
	stage := "probe"
	defer reportPortalStageOnFailure(t, &stage)

	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
	capability, err := portalinput.Probe(probeCtx)
	cancelProbe()
	if err != nil {
		t.Fatalf("Probe error: %v", err)
	}
	devices := portalinput.DeviceKeyboard | portalinput.DevicePointer
	if capability.Supports(portalinput.DeviceTouchscreen) {
		devices |= portalinput.DeviceTouchscreen
	}
	if !capability.Supports(devices) {
		t.Fatalf("RemoteDesktop portal does not advertise keyboard and pointer input: %+v", capability)
	}
	if !capability.SupportsSources(portalinput.SourceMonitor) {
		t.Fatalf("ScreenCast portal does not advertise monitor sources: %+v", capability)
	}
	if !capability.SupportsCursorMode(portalinput.CursorHidden) {
		t.Fatalf("ScreenCast portal does not advertise hidden cursor mode: %+v", capability)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	options := portalinput.OpenOptions{
		Devices: devices, Sources: portalinput.SourceMonitor,
		CursorMode: portalinput.CursorHidden,
	}
	expectedOutputs, multiOutput := expectedPortalOutputs(t)
	options.Multiple = multiOutput
	stage = "open"
	signalPortalConsentReady(t, "remote-desktop")
	session, err := portalinput.OpenWithOptions(ctx, options)
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
		validateRemoteDesktopStreams(t, &stage, streams, expectedOutputs)
		for index, stream := range streams {
			stage = fmt.Sprintf("input-output-%d", index+1)
			stream := stream
			runPortalEvent(
				t,
				fmt.Sprintf("PointerMotionAbsolute output %d", index+1),
				func(eventCtx context.Context) error {
					return session.PointerMotionAbsolute(
						eventCtx,
						stream.NodeID,
						float64(stream.Size.Width)/2,
						float64(stream.Size.Height)/2,
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
	if devices&portalinput.DeviceTouchscreen != 0 {
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
	multiOutput := os.Getenv(envPortalMultiOutput)
	encoded := os.Getenv(envPortalExpectedOutputs)
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
	streams []portalinput.Stream,
	expected portalrunner.HostedDisplay,
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
			Monitor:        stream.SourceType == portalinput.SourceMonitor,
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
		evidence,
	); err != nil {
		if category := portalrunner.HostedStreamEvidenceFailureStage(err); category != "" {
			*stage = "streams-" + category
		}
		t.Fatalf("validate portal multi-output evidence: %v", err)
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
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
	t.Setenv(envPortalConsentReadyFile, marker)
	t.Run("lifecycle", func(t *testing.T) {
		signalPortalConsentReady(t, "remote-desktop")
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

func runPortalEvent(t *testing.T, action string, event func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := event(ctx); err != nil {
		t.Fatalf("portal %s error: %v", action, err)
	}
}
