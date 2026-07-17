package robotgo

import (
	"context"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeFeatureDiagnosticsHaveStableOrder(t *testing.T) {
	capability := FeatureCapability{
		Available: true,
		Fallback:  true,
		Backend:   "test",
		Reason:    "ready",
		Notes:     "details",
	}
	features := runtimeFeatureDiagnostics(RuntimeCapabilities{
		Capture:       capability,
		Bounds:        capability,
		Keyboard:      capability,
		Mouse:         capability,
		RemoteDesktop: capability,
		Window:        capability,
		Process:       capability,
		Clipboard:     capability,
		Hook:          capability,
		Events:        capability,
	})
	got := make([]string, len(features))
	for index := range features {
		got[index] = features[index].Name
		if features[index].Backend != capability.Backend ||
			features[index].Reason != capability.Reason ||
			features[index].Notes != capability.Notes {
			t.Fatalf("feature %q lost capability detail: %+v", features[index].Name, features[index])
		}
	}
	want := []string{
		"capture", "bounds", "keyboard", "mouse", "remote-desktop",
		"window", "process", "clipboard", "hook", "events",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("feature order = %v, want %v", got, want)
	}
}

func TestRuntimeRemediationOnlyReportsUnavailableFeatures(t *testing.T) {
	features := []RuntimeFeatureDiagnostic{
		{Name: "capture", Available: true, Notes: "not an action"},
		{Name: "keyboard", Notes: "enable the input backend"},
		{Name: "keyboard", Notes: "enable the input backend"},
		{Name: "window", Reason: "install a compositor helper"},
		{Name: "events", Notes: "global events are unsupported"},
		{Name: "events"},
	}
	got := runtimeRemediation(features)
	want := []RuntimeRemediation{
		{Feature: "keyboard", Action: "enable the input backend"},
		{Feature: "window", Action: "install a compositor helper"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remediation = %#v, want %#v", got, want)
	}
}

func TestGetRuntimeDiagnosticsUsesVersionedSanitizedContract(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Cleanup(CloseWaylandInput)
	}
	t.Setenv("ROBOTGO_DIAGNOSTIC_SECRET_TEST", "do-not-expose")
	if runtime.GOOS == "linux" {
		t.Setenv(envWaylandDisplay, "secret-wayland-display")
	}
	report := GetRuntimeDiagnostics(context.Background())
	if report.SchemaVersion != RuntimeDiagnosticsSchemaVersion {
		t.Fatalf("schema version = %q, want %q", report.SchemaVersion, RuntimeDiagnosticsSchemaVersion)
	}
	if report.Runtime.GOOS == "" || report.Runtime.GOARCH == "" {
		t.Fatalf("runtime identity missing: %+v", report.Runtime)
	}
	if len(report.Features) != 10 {
		t.Fatalf("feature count = %d, want 10", len(report.Features))
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal diagnostics: %v", err)
	}
	if strings.Contains(string(data), "do-not-expose") {
		t.Fatalf("diagnostics exposed unrelated environment data: %s", data)
	}
	if strings.Contains(string(data), "secret-wayland-display") {
		t.Fatalf("diagnostics exposed the Wayland display address: %s", data)
	}
	if !strings.Contains(string(data), `"robotgo_version"`) ||
		!strings.Contains(string(data), `"build_implementation"`) {
		t.Fatalf("diagnostic identity does not use the v1 JSON contract: %s", data)
	}
}

func TestBoundedDiagnosticContext(t *testing.T) {
	ctx, cancel := boundedDiagnosticContext(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("diagnostic context has no bounded deadline")
	}

	parent, parentCancel := context.WithCancel(context.Background())
	parentCancel()
	ctx, cancel = boundedDiagnosticContext(parent)
	defer cancel()
	if ctx.Err() == nil {
		t.Fatal("cancelled parent context was not preserved")
	}
}
