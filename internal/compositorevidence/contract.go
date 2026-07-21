// Package compositorevidence creates privacy-safe evidence for protected
// real-compositor integration jobs.
package compositorevidence

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// SchemaVersion identifies the compositor-evidence JSON contract.
	SchemaVersion = "1"

	preflightSchemaVersion = "1"
	evidenceProvider       = "github-actions"
	maxTextLength          = 256
	maxLogBytes            = 1024 * 1024
	maxEvidenceBytes       = 1024 * 1024
	defaultProbeTimeout    = 5 * time.Second
)

// Lane identifies a protected desktop image family.
type Lane string

const (
	LaneGNOME   Lane = "gnome"
	LaneKDE     Lane = "kde"
	LaneWlroots Lane = "wlroots"
)

// Cell identifies the runtime behavior proven by one matrix cell.
type Cell string

const (
	CellRemoteDesktop      Cell = "remote-desktop"
	CellScreenCast         Cell = "screencast"
	CellNativeInput        Cell = "native-input"
	CellNativeCapture      Cell = "native-capture"
	CellNativeWindow       Cell = "native-window"
	CellNativeOutput       Cell = "native-output"
	CellPortalAvailability Cell = "portal-availability"
)

const (
	remoteDesktopTestName = "TestRemoteDesktopPortalRuntime"
	remoteDesktopPackage  = "github.com/marang/robotgo/input/portal"
	remoteDesktopCommand  = "go test -count=1 -timeout=3m -tags=integration ./input/portal -run ^TestRemoteDesktopPortalRuntime$ -v"
	screenCastTestName    = "TestPipeWireCapturePersistentSessionIntegration"
	screenCastPackage     = "github.com/marang/robotgo/screen/portal"
	screenCastCommand     = "go test -count=1 -timeout=3m -tags=pipewire,integration ./screen/portal -run ^TestPipeWireCapturePersistentSessionIntegration$ -v"
	swayPackage           = "github.com/marang/robotgo"
	swayCommandPrefix     = "go test -count=1 -timeout=2m -tags=wayland,swayintegration . -run ^"
	swayCommandSuffix     = "$ -v"
	swayInputTestName     = "TestSwayNativeInputRuntime"
	swayCaptureTestName   = "TestSwayNativeCaptureRuntime"
	swayWindowTestName    = "TestSwayNativeWindowRuntime"
	swayOutputTestName    = "TestSwayNativeOutputRuntime"
	swayPortalTestName    = "TestSwayPortalAvailabilityRuntime"
)

var (
	gitObjectPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	labelPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	versionPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+:~_-]{0,127}$`)
	kernelPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}$`)
	digestPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// TestSpec describes the single integration test represented by an evidence
// cell.
type TestSpec struct {
	Package string
	Name    string
	Command string
}

// ParseLane validates a desktop lane name.
func ParseLane(value string) (Lane, error) {
	lane := Lane(strings.ToLower(strings.TrimSpace(value)))
	switch lane {
	case LaneGNOME, LaneKDE, LaneWlroots:
		return lane, nil
	default:
		return "", fmt.Errorf("unsupported compositor lane %q", value)
	}
}

// ParseCell validates an evidence cell name.
func ParseCell(value string) (Cell, error) {
	cell := Cell(strings.ToLower(strings.TrimSpace(value)))
	switch cell {
	case CellRemoteDesktop, CellScreenCast, CellNativeInput,
		CellNativeCapture, CellNativeWindow, CellNativeOutput,
		CellPortalAvailability:
		return cell, nil
	default:
		return "", fmt.Errorf("unsupported compositor evidence cell %q", value)
	}
}

// PortalRequired reports whether the cell must expose a real desktop portal
// interface.
func (cell Cell) PortalRequired() bool {
	return cell == CellRemoteDesktop || cell == CellScreenCast
}

// PortalObserved reports whether the cell records portal capability even when
// the selected wlroots image explicitly proves that it is unavailable.
func (cell Cell) PortalObserved() bool {
	return cell.PortalRequired() || cell == CellPortalAvailability
}

// PipeWireRequired reports whether the cell must expose a persistent PipeWire
// capture runtime.
func (cell Cell) PipeWireRequired() bool {
	return cell == CellScreenCast
}

// ConsentRequired reports whether the cell starts an interactive portal
// request that requires the protected operator handoff.
func (cell Cell) ConsentRequired() bool {
	return cell == CellRemoteDesktop || cell == CellScreenCast
}

// NativeRequired reports whether the cell proves wlroots-native behavior.
func (cell Cell) NativeRequired() bool {
	switch cell {
	case CellNativeInput, CellNativeCapture, CellNativeWindow, CellNativeOutput:
		return true
	default:
		return false
	}
}

// TestSpec returns the fixed test represented by a runnable evidence cell.
func (cell Cell) TestSpec() (TestSpec, error) {
	switch cell {
	case CellRemoteDesktop:
		return TestSpec{
			Package: remoteDesktopPackage,
			Name:    remoteDesktopTestName,
			Command: remoteDesktopCommand,
		}, nil
	case CellScreenCast:
		return TestSpec{
			Package: screenCastPackage,
			Name:    screenCastTestName,
			Command: screenCastCommand,
		}, nil
	case CellNativeInput:
		return swayTestSpec(swayInputTestName), nil
	case CellNativeCapture:
		return swayTestSpec(swayCaptureTestName), nil
	case CellNativeWindow:
		return swayTestSpec(swayWindowTestName), nil
	case CellNativeOutput:
		return swayTestSpec(swayOutputTestName), nil
	case CellPortalAvailability:
		return swayTestSpec(swayPortalTestName), nil
	default:
		return TestSpec{}, fmt.Errorf("cell %q has no integration test", cell)
	}
}

func swayTestSpec(name string) TestSpec {
	return TestSpec{
		Package: swayPackage,
		Name:    name,
		Command: swayCommandPrefix + name + swayCommandSuffix,
	}
}

func (cell Cell) expectedWorkflow() string {
	switch cell {
	case CellRemoteDesktop:
		return "RemoteDesktop E2E"
	case CellScreenCast:
		return "ScreenCast E2E"
	case CellNativeInput, CellNativeCapture, CellNativeWindow,
		CellNativeOutput, CellPortalAvailability:
		return "Sway E2E"
	default:
		return ""
	}
}

func validateWorkflow(cell Cell, workflow string) error {
	if err := validateText("workflow", workflow); err != nil {
		return err
	}
	if expected := cell.expectedWorkflow(); expected != "" && workflow != expected {
		return errors.New("workflow does not match compositor evidence cell")
	}
	return nil
}

func validateLaneCell(lane Lane, cell Cell) error {
	if _, err := ParseLane(string(lane)); err != nil {
		return err
	}
	if _, err := ParseCell(string(cell)); err != nil {
		return err
	}
	if cell.NativeRequired() && lane != LaneWlroots {
		return fmt.Errorf("native cell %q requires the wlroots lane", cell)
	}
	if cell == CellPortalAvailability && lane != LaneWlroots {
		return errors.New("portal-availability cell requires the wlroots lane")
	}
	if lane == LaneWlroots && cell.ConsentRequired() {
		return errors.New("wlroots portal behavior must use the portal-availability cell until a protected consent backend is promoted")
	}
	return nil
}

func validateGitIdentity(commit, ref string) error {
	if !gitObjectPattern.MatchString(commit) {
		return errors.New("commit must be a lowercase 40- or 64-character Git object ID")
	}
	if err := validateText("ref", ref); err != nil {
		return err
	}
	if !strings.HasPrefix(ref, "refs/") {
		return errors.New("ref must start with refs/")
	}
	return nil
}

func validateText(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > maxTextLength {
		return fmt.Errorf("%s exceeds %d bytes", name, maxTextLength)
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return fmt.Errorf("%s contains control characters", name)
		}
	}
	return nil
}

func validateLabel(name, value string) error {
	if len(value) > 128 || !labelPattern.MatchString(value) {
		return fmt.Errorf("%s must contain only letters, digits, dot, underscore, or hyphen", name)
	}
	return nil
}

func validateVersion(name, value string, required bool) error {
	if value == "" && !required {
		return nil
	}
	if !versionPattern.MatchString(value) {
		return fmt.Errorf("%s is not a sanitized version", name)
	}
	return nil
}
