package robotgo

import (
	"context"
	"strings"
	"time"
)

// RuntimeDiagnosticsSchemaVersion identifies the stable JSON/data contract
// returned by GetRuntimeDiagnostics.
const RuntimeDiagnosticsSchemaVersion = "1"

const (
	runtimeFeatureCapture       = "capture"
	runtimeFeatureBounds        = "bounds"
	runtimeFeatureKeyboard      = "keyboard"
	runtimeFeatureMouse         = "mouse"
	runtimeFeatureRemoteDesktop = "remote-desktop"
	runtimeFeatureWindow        = "window"
	runtimeFeatureProcess       = "process"
	runtimeFeatureClipboard     = "clipboard"
	runtimeFeatureHook          = "hook"
	runtimeFeatureEvents        = "events"
	runtimeFeatureDesktop       = "desktop"
)

// RuntimePermissionState describes a permission or consent state without
// opening a system dialog.
type RuntimePermissionState string

const (
	RuntimePermissionUnknown      RuntimePermissionState = "unknown"
	RuntimePermissionNotRequired  RuntimePermissionState = "not-required"
	RuntimePermissionNotRequested RuntimePermissionState = "not-requested"
	RuntimePermissionGranted      RuntimePermissionState = "granted"
	RuntimePermissionDenied       RuntimePermissionState = "denied"
	RuntimePermissionCancelled    RuntimePermissionState = "cancelled"
	RuntimePermissionTimedOut     RuntimePermissionState = "timed-out"
	RuntimePermissionClosed       RuntimePermissionState = "closed"
	RuntimePermissionFailed       RuntimePermissionState = "failed"
	RuntimePermissionUnavailable  RuntimePermissionState = "unavailable"
)

// RuntimeFeatureDiagnostic is a stable, named view of one feature capability.
type RuntimeFeatureDiagnostic struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Fallback  bool   `json:"fallback"`
	Backend   string `json:"backend,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Notes     string `json:"notes,omitempty"`
}

// RuntimeProtocolDiagnostic reports a protocol version negotiated with the
// active compositor, display server, or desktop portal.
type RuntimeProtocolDiagnostic struct {
	Feature    string `json:"feature"`
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	Negotiated bool   `json:"negotiated"`
	Reason     string `json:"reason,omitempty"`
}

// RuntimePermissionDiagnostic reports permission state without requesting
// consent or exposing tokens and other sensitive session data.
type RuntimePermissionDiagnostic struct {
	Feature string                 `json:"feature"`
	Name    string                 `json:"name"`
	State   RuntimePermissionState `json:"state"`
	Reason  string                 `json:"reason,omitempty"`
}

// RuntimeIdentityDiagnostic is the sanitized build identity included in a
// versioned diagnostic report.
type RuntimeIdentityDiagnostic struct {
	RobotGoVersion      string                `json:"robotgo_version"`
	GOOS                string                `json:"goos"`
	GOARCH              string                `json:"goarch"`
	CGOEnabled          bool                  `json:"cgo_enabled"`
	BuildImplementation RuntimeImplementation `json:"build_implementation"`
}

// RuntimeRemediation describes one actionable step for an unavailable feature.
type RuntimeRemediation struct {
	Feature string `json:"feature"`
	Action  string `json:"action"`
}

// RuntimeDiagnostics is the versioned, machine-readable runtime support
// report. Feature and protocol ordering is stable within a schema version.
type RuntimeDiagnostics struct {
	SchemaVersion string                        `json:"schema_version"`
	Runtime       RuntimeIdentityDiagnostic     `json:"runtime"`
	DisplayServer DisplayServer                 `json:"display_server,omitempty"`
	Compositor    string                        `json:"compositor,omitempty"`
	Features      []RuntimeFeatureDiagnostic    `json:"features"`
	Protocols     []RuntimeProtocolDiagnostic   `json:"protocols,omitempty"`
	Permissions   []RuntimePermissionDiagnostic `json:"permissions,omitempty"`
	Remediation   []RuntimeRemediation          `json:"remediation,omitempty"`
}

type nativeWaylandProtocolInfo struct {
	Screencopy      uint32
	VirtualKeyboard uint32
	VirtualPointer  uint32
}

// GetRuntimeDiagnostics reports backend, protocol, permission, and remediation
// information without opening a consent dialog. Feature discovery retains its
// existing bounded probes; the additional structured portal probes use the
// supplied deadline or a bounded default.
func GetRuntimeDiagnostics(ctx context.Context) RuntimeDiagnostics {
	capabilities := GetRuntimeCapabilities()
	report := RuntimeDiagnostics{
		SchemaVersion: RuntimeDiagnosticsSchemaVersion,
		Runtime:       newRuntimeIdentityDiagnostic(capabilities.Runtime),
		DisplayServer: capabilities.Runtime.DisplayServer,
		Features:      runtimeFeatureDiagnostics(capabilities),
	}
	if capabilities.Runtime.GOOS == "linux" {
		report.Compositor = capabilities.Compositor
	}

	probeCtx, cancel := boundedDiagnosticContext(ctx)
	defer cancel()
	report.Protocols, report.Permissions = platformRuntimeDiagnosticDetails(
		probeCtx,
		capabilities,
	)
	report.Remediation = runtimeRemediation(report.Features)
	return report
}

func newRuntimeIdentityDiagnostic(info RuntimeBackendInfo) RuntimeIdentityDiagnostic {
	return RuntimeIdentityDiagnostic{
		RobotGoVersion:      GetVersion(),
		GOOS:                info.GOOS,
		GOARCH:              info.GOARCH,
		CGOEnabled:          info.CGOEnabled,
		BuildImplementation: info.BuildImplementation,
	}
}

func boundedDiagnosticContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, 750*time.Millisecond)
}

func runtimeFeatureDiagnostics(c RuntimeCapabilities) []RuntimeFeatureDiagnostic {
	return []RuntimeFeatureDiagnostic{
		newRuntimeFeatureDiagnostic(runtimeFeatureCapture, c.Capture),
		newRuntimeFeatureDiagnostic(runtimeFeatureBounds, c.Bounds),
		newRuntimeFeatureDiagnostic(runtimeFeatureKeyboard, c.Keyboard),
		newRuntimeFeatureDiagnostic(runtimeFeatureMouse, c.Mouse),
		newRuntimeFeatureDiagnostic(runtimeFeatureRemoteDesktop, c.RemoteDesktop),
		newRuntimeFeatureDiagnostic(runtimeFeatureWindow, c.Window),
		newRuntimeFeatureDiagnostic(runtimeFeatureProcess, c.Process),
		newRuntimeFeatureDiagnostic(runtimeFeatureClipboard, c.Clipboard),
		newRuntimeFeatureDiagnostic(runtimeFeatureHook, c.Hook),
		newRuntimeFeatureDiagnostic(runtimeFeatureEvents, c.Events),
	}
}

func newRuntimeFeatureDiagnostic(name string, capability FeatureCapability) RuntimeFeatureDiagnostic {
	return RuntimeFeatureDiagnostic{
		Name:      name,
		Available: capability.Available,
		Fallback:  capability.Fallback,
		Backend:   capability.Backend,
		Reason:    capability.Reason,
		Notes:     capability.Notes,
	}
}

func runtimeRemediation(features []RuntimeFeatureDiagnostic) []RuntimeRemediation {
	seen := make(map[string]struct{})
	result := make([]RuntimeRemediation, 0)
	for _, feature := range features {
		if feature.Available {
			continue
		}
		action := feature.Notes
		if action == "" {
			action = feature.Reason
		}
		if action == "" {
			continue
		}
		if !actionableRuntimeAdvice(action) {
			continue
		}
		key := feature.Name + "\x00" + action
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, RuntimeRemediation{
			Feature: feature.Name,
			Action:  action,
		})
	}
	return result
}

func actionableRuntimeAdvice(action string) bool {
	lower := strings.ToLower(action)
	for _, verb := range []string{
		"build", "call", "check", "configure", "enable", "grant", "install",
		"provide", "remove", "run", "select", "set", "start", "use", "verify",
	} {
		if strings.HasPrefix(lower, verb+" ") ||
			strings.Contains(lower, "; "+verb+" ") ||
			strings.Contains(lower, ". "+verb+" ") {
			return true
		}
	}
	return false
}
