//go:build linux && hostedboundsintegration

package robotgo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marang/robotgo/internal/portalrunner"
)

const (
	envHostedBoundsE2E         = "ROBOTGO_HOSTED_BOUNDS_E2E"
	envHostedBoundsSessionType = "XDG_SESSION_TYPE"
	hostedBoundsWaylandSession = "wayland"

	hostedBoundsStageEnvironment  = "environment"
	hostedBoundsStageTopology     = "topology"
	hostedBoundsStageDisplayCount = "display-count"
	hostedBoundsStageMainDisplay  = "main-display"
	hostedBoundsStageDisplayZero  = "display-zero"
	hostedBoundsStageDisplayOne   = "display-one"
	hostedBoundsStageInvalidIndex = "invalid-index"
	hostedBoundsStageAggregate    = "aggregate"
	hostedBoundsStagePrimarySize  = "primary-size"
	hostedBoundsStageComplete     = "complete"
)

func assertHostedWaylandBoundsRuntime(t *testing.T) {
	t.Helper()
	variant := os.Getenv(portalrunner.HostedBoundsVariantEnvKey)
	if variant != portalrunner.HostedBoundsVariantNativeCGO &&
		variant != portalrunner.HostedBoundsVariantPureGo {
		t.Fatal("hosted bounds implementation variant is invalid")
	}
	reportHostedBoundsStage(t, variant, hostedBoundsStageEnvironment)
	if os.Getenv(envHostedBoundsE2E) != "1" {
		t.Fatalf("hosted bounds contract requires %s=1", envHostedBoundsE2E)
	}
	if _, exists := os.LookupEnv(envDisplay); exists {
		t.Fatal("hosted bounds contract requires DISPLAY to be unset")
	}
	if os.Getenv(envWaylandDisplay) == "" {
		t.Fatal("hosted bounds contract requires WAYLAND_DISPLAY")
	}
	if os.Getenv(envHostedBoundsSessionType) != hostedBoundsWaylandSession {
		t.Fatalf(
			"hosted bounds contract requires %s=%q",
			envHostedBoundsSessionType,
			hostedBoundsWaylandSession,
		)
	}
	for _, forbidden := range []string{
		"ROBOTGO_PORTAL_CONSENT_READY_FILE",
		"ROBOTGO_PORTAL_MULTI_OUTPUT",
		"ROBOTGO_PORTAL_EXPECTED_OUTPUTS",
		"ROBOTGO_REMOTE_DESKTOP_E2E",
		"ROBOTGO_SCREENCAST_E2E",
		"ROBOTGO_FORCE_PORTAL",
	} {
		if _, exists := os.LookupEnv(forbidden); exists {
			t.Fatalf("consent-free hosted bounds contract exposes %s", forbidden)
		}
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Fatalf(
			"hosted bounds selected %q, want %q",
			DetectDisplayServer(),
			DisplayServerWayland,
		)
	}
	assertHostedWaylandSocket(t)

	reportHostedBoundsStage(t, variant, hostedBoundsStageTopology)
	expected, err := portalrunner.ParseHostedDisplay(
		os.Getenv(portalrunner.HostedExpectedOutputsEnvKey),
	)
	if err != nil {
		t.Fatalf("parse hosted output contract: %v", err)
	}
	if len(expected.Outputs) != 2 {
		t.Fatalf(
			"hosted bounds evidence requires exactly two outputs, got %d",
			len(expected.Outputs),
		)
	}

	reportHostedBoundsStage(t, variant, hostedBoundsStageDisplayCount)
	count, err := DisplaysNumE()
	if err != nil {
		t.Fatalf("DisplaysNumE(): %v", err)
	}
	if count != len(expected.Outputs) {
		t.Fatalf("DisplaysNumE() = %d, want %d", count, len(expected.Outputs))
	}
	if legacy := DisplaysNum(); legacy != count {
		t.Fatalf("DisplaysNum() = %d, error API returned %d", legacy, count)
	}
	reportHostedBoundsStage(t, variant, hostedBoundsStageMainDisplay)
	if mainID := GetMainId(); mainID != 0 {
		t.Fatalf("GetMainId() = %d, want primary output index 0", mainID)
	}

	for displayID, output := range expected.Outputs {
		stage := hostedBoundsStageDisplayZero
		if displayID == 1 {
			stage = hostedBoundsStageDisplayOne
		}
		reportHostedBoundsStage(t, variant, stage)
		assertHostedDisplayBounds(t, displayID, output)
	}
	reportHostedBoundsStage(t, variant, hostedBoundsStageInvalidIndex)
	if _, _, _, _, err := GetDisplayBoundsE(count); err == nil {
		t.Fatalf("GetDisplayBoundsE accepted inactive output index %d", count)
	}
	if x, y, width, height := GetDisplayBounds(count); x != 0 ||
		y != 0 || width != 0 || height != 0 {
		t.Fatalf(
			"legacy invalid display bounds = %d,%d %dx%d, want empty",
			x,
			y,
			width,
			height,
		)
	}

	reportHostedBoundsStage(t, variant, hostedBoundsStageAggregate)
	aggregate := hostedAggregateRect(expected.Outputs)
	for _, displayID := range [][]int{nil, {-1}} {
		rect, err := GetScreenRectE(displayID...)
		if err != nil {
			t.Fatalf("GetScreenRectE(%v): %v", displayID, err)
		}
		if rect != aggregate {
			t.Fatalf(
				"GetScreenRectE(%v) = %+v, want %+v",
				displayID,
				rect,
				aggregate,
			)
		}
		if legacy := GetScreenRect(displayID...); legacy != rect {
			t.Fatalf(
				"GetScreenRect(%v) = %+v, error API returned %+v",
				displayID,
				legacy,
				rect,
			)
		}
	}

	reportHostedBoundsStage(t, variant, hostedBoundsStagePrimarySize)
	width, height, err := GetScreenSizeE()
	if err != nil {
		t.Fatalf("GetScreenSizeE(): %v", err)
	}
	primary := expected.Outputs[0]
	if width != primary.Width || height != primary.Height {
		t.Fatalf(
			"GetScreenSizeE() = %dx%d, want primary %dx%d",
			width,
			height,
			primary.Width,
			primary.Height,
		)
	}
	legacyWidth, legacyHeight := GetScreenSize()
	if legacyWidth != width || legacyHeight != height {
		t.Fatalf(
			"GetScreenSize() = %dx%d, error API returned %dx%d",
			legacyWidth,
			legacyHeight,
			width,
			height,
		)
	}
	reportHostedBoundsStage(t, variant, hostedBoundsStageComplete)
}

func reportHostedBoundsStage(t *testing.T, variant, stage string) {
	t.Helper()
	t.Logf("ROBOTGO_HOSTED_BOUNDS_STAGE=%s-%s", variant, stage)
}

func assertHostedWaylandSocket(t *testing.T) {
	t.Helper()
	runtimeDirectory := filepath.Clean(os.Getenv("XDG_RUNTIME_DIR"))
	if !filepath.IsAbs(runtimeDirectory) ||
		runtimeDirectory == string(filepath.Separator) {
		t.Fatal("hosted bounds runtime directory is invalid")
	}
	socket := os.Getenv(envWaylandDisplay)
	if !filepath.IsAbs(socket) {
		socket = filepath.Join(runtimeDirectory, socket)
	}
	socket = filepath.Clean(socket)
	relative, err := filepath.Rel(runtimeDirectory, socket)
	if err != nil || relative == "." || relative == ".." ||
		filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("hosted bounds Wayland socket escapes its runtime directory")
	}
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatal("hosted bounds Wayland socket is unavailable")
	}
}

func assertHostedDisplayBounds(
	t *testing.T,
	displayID int,
	expected portalrunner.HostedOutput,
) {
	t.Helper()
	x, y, width, height, err := GetDisplayBoundsE(displayID)
	if err != nil {
		t.Fatalf("GetDisplayBoundsE(%d): %v", displayID, err)
	}
	if x != expected.X || y != expected.Y ||
		width != expected.Width || height != expected.Height {
		t.Fatalf(
			"GetDisplayBoundsE(%d) = %d,%d %dx%d, want %d,%d %dx%d",
			displayID,
			x,
			y,
			width,
			height,
			expected.X,
			expected.Y,
			expected.Width,
			expected.Height,
		)
	}
	legacyX, legacyY, legacyWidth, legacyHeight := GetDisplayBounds(displayID)
	if legacyX != x || legacyY != y ||
		legacyWidth != width || legacyHeight != height {
		t.Fatalf(
			"GetDisplayBounds(%d) = %d,%d %dx%d, error API returned %d,%d %dx%d",
			displayID,
			legacyX,
			legacyY,
			legacyWidth,
			legacyHeight,
			x,
			y,
			width,
			height,
		)
	}
	want := Rect{
		Point: Point{X: expected.X, Y: expected.Y},
		Size:  Size{W: expected.Width, H: expected.Height},
	}
	rect, err := GetScreenRectE(displayID)
	if err != nil {
		t.Fatalf("GetScreenRectE(%d): %v", displayID, err)
	}
	if rect != want {
		t.Fatalf("GetScreenRectE(%d) = %+v, want %+v", displayID, rect, want)
	}
	if legacy := GetScreenRect(displayID); legacy != rect {
		t.Fatalf(
			"GetScreenRect(%d) = %+v, error API returned %+v",
			displayID,
			legacy,
			rect,
		)
	}
	if displayRect := GetDisplayRect(displayID); displayRect != rect {
		t.Fatalf(
			"GetDisplayRect(%d) = %+v, error API returned %+v",
			displayID,
			displayRect,
			rect,
		)
	}
}

func hostedAggregateRect(outputs []portalrunner.HostedOutput) Rect {
	minX, minY := outputs[0].X, outputs[0].Y
	maxX := outputs[0].X + outputs[0].Width
	maxY := outputs[0].Y + outputs[0].Height
	for _, output := range outputs[1:] {
		minX = min(minX, output.X)
		minY = min(minY, output.Y)
		maxX = max(maxX, output.X+output.Width)
		maxY = max(maxY, output.Y+output.Height)
	}
	return Rect{
		Point: Point{X: minX, Y: minY},
		Size:  Size{W: maxX - minX, H: maxY - minY},
	}
}
