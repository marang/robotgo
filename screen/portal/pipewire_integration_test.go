//go:build linux && cgo && pipewire && integration

package portal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const envScreenCastRequireMonitor = "ROBOTGO_SCREENCAST_REQUIRE_MONITOR"

func TestPipeWireCapturePersistentSessionIntegration(t *testing.T) {
	if os.Getenv("ROBOTGO_SCREENCAST_E2E") == "" {
		t.Skip("set ROBOTGO_SCREENCAST_E2E=1 in a graphical Wayland session")
	}
	stage := "preflight"
	defer reportScreenCastStageOnFailure(t, &stage)
	signalScreenCastConsentReady(t)
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
	)
	cancelOpen()
	if err != nil {
		if classified := screenCastPortalFailureStage(err); classified != "" {
			stage = classified
		}
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
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)
	t.Setenv("ROBOTGO_PORTAL_CONSENT_READY_FILE", marker)
	t.Run("lifecycle", func(t *testing.T) {
		signalScreenCastConsentReady(t)
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
