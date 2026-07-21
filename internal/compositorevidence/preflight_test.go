package compositorevidence

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testCommit = "0123456789abcdef0123456789abcdef01234567"
	testRef    = "refs/heads/feature/lab-16-evidence"
)

type fakeProbe struct {
	mu       sync.Mutex
	calls    []string
	failName string
	block    bool
}

func (probe *fakeProbe) output(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	probe.mu.Lock()
	probe.calls = append(probe.calls, name+" "+strings.Join(args, " "))
	probe.mu.Unlock()
	if probe.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if name == probe.failName {
		return nil, errors.New("private backend failure")
	}
	switch name {
	case "uname":
		if len(args) == 1 && args[0] == "-s" {
			return []byte("Linux\n"), nil
		}
		return []byte("6.12.4-test\n"), nil
	case "dpkg-query":
		return []byte("1.2.3-1ubuntu1\n"), nil
	case "rpm":
		return []byte("1.2.3-1\n"), nil
	case "busctl":
		if len(args) >= 2 && args[len(args)-2] == "list" {
			return []byte(args[len(args)-1] + " 123 portal-user :1.42 session - -\n"), nil
		}
		if args[len(args)-1] == availableSourcesProperty {
			return []byte("u 3\n"), nil
		}
		return []byte("u 6\n"), nil
	case "pkg-config":
		return []byte("1.2.3\n"), nil
	case "systemctl":
		return []byte("active\n"), nil
	case "swaymsg":
		return []byte(`{"human_readable":"sway version 1.10"}`), nil
	default:
		return nil, errors.New("unexpected command")
	}
}

func (probe *fakeProbe) called(name string) bool {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	for _, call := range probe.calls {
		if strings.HasPrefix(call, name+" ") {
			return true
		}
	}
	return false
}

func validPreflightConfig(lane Lane, cell Cell) PreflightConfig {
	desktop := "GNOME"
	workflow := "RemoteDesktop E2E"
	if lane == LaneKDE {
		desktop = "KDE:Plasma"
	}
	if lane == LaneWlroots {
		desktop = "sway"
	}
	if cell == CellScreenCast {
		workflow = "ScreenCast E2E"
	}
	return PreflightConfig{
		Lane:               lane,
		Cell:               cell,
		CheckoutCommit:     testCommit,
		ExpectedCommit:     testCommit,
		Ref:                testRef,
		Workflow:           workflow,
		RunID:              "12345",
		RunAttempt:         2,
		CurrentDesktop:     desktop,
		WaylandDisplay:     "wayland-1",
		RuntimeDir:         "/run/user/1000",
		SessionBusAddress:  "unix:path=/run/private-bus",
		OperatorReadyPath:  "/run/robotgo-evidence/operator-ready",
		OutputCount:        2,
		MinimumOutputCount: 2,
		ProbeTimeout:       time.Second,
	}
}

func validDependencies(probe *fakeProbe) preflightDependencies {
	return preflightDependencies{
		output: probe.output,
		socketReady: func(runtimeDir, display string) error {
			if runtimeDir != "/run/user/1000" || display != "wayland-1" {
				return errors.New("unexpected private socket identity")
			}
			return nil
		},
		operatorReady: func(path, expected string) error {
			if path != "/run/robotgo-evidence/operator-ready" {
				return errors.New("unexpected readiness path")
			}
			if !strings.HasPrefix(expected, operatorReadyContent+" commit="+testCommit+" run=12345 attempt=2 lane=") {
				return errors.New("unexpected readiness attestation")
			}
			return nil
		},
		readFile: func(path string) ([]byte, error) {
			if path != defaultOSReleasePath {
				return nil, errors.New("unexpected OS identity path")
			}
			return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
		},
	}
}

func TestPreflightPortalCellsSelectApplicableRequirements(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		lane         Lane
		cell         Cell
		wantPipeWire bool
	}{
		{name: "remote desktop", lane: LaneGNOME, cell: CellRemoteDesktop},
		{name: "screen cast", lane: LaneKDE, cell: CellScreenCast, wantPipeWire: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe := &fakeProbe{}
			report, err := preflight(
				context.Background(),
				validPreflightConfig(tc.lane, tc.cell),
				validDependencies(probe),
			)
			if err != nil {
				t.Fatalf("preflight failed: %v", err)
			}
			if !report.Desktop.Portal.Available ||
				report.Desktop.Portal.RemoteDesktopVersion != 6 ||
				report.Desktop.Portal.ScreenCastVersion != 6 {
				t.Fatalf("portal report = %+v", report.Desktop.Portal)
			}
			if report.Desktop.PipeWire.Required != tc.wantPipeWire {
				t.Fatalf("PipeWire required = %t, want %t", report.Desktop.PipeWire.Required, tc.wantPipeWire)
			}
			if probe.called("pkg-config") != tc.wantPipeWire {
				t.Fatalf("pkg-config called = %t, want %t", probe.called("pkg-config"), tc.wantPipeWire)
			}
			if !report.Desktop.OperatorReady {
				t.Fatal("interactive portal cell lacks operator readiness")
			}
			if report.Workflow != validPreflightConfig(tc.lane, tc.cell).Workflow ||
				report.RunID != "12345" || report.RunAttempt != 2 {
				t.Fatalf("protected run identity = %q/%q/%d", report.Workflow, report.RunID, report.RunAttempt)
			}
		})
	}
}

func TestPreflightNativeSwayDoesNotRequirePortalOrPipeWire(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	report, err := preflight(
		context.Background(),
		validPreflightConfig(LaneWlroots, CellNativeInput),
		validDependencies(probe),
	)
	if err != nil {
		t.Fatalf("preflight failed: %v", err)
	}
	if report.Desktop.Portal.Observed || report.Desktop.PipeWire.Required ||
		report.Desktop.OperatorReady {
		t.Fatalf("native report contains inapplicable requirements: %+v", report.Desktop)
	}
	if !probe.called("swaymsg") {
		t.Fatal("native Sway capability was not probed")
	}
	calls := strings.Join(probe.calls, "\n")
	if strings.Contains(calls, portalBusName) || probe.called("pkg-config") || probe.called("systemctl") {
		t.Fatalf("native cell ran portal/PipeWire probes: %v", probe.calls)
	}
}

func TestPreflightWlrootsPortalAvailabilityRecordsUnavailable(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	dependencies := validDependencies(probe)
	dependencies.output = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if (name == "dpkg-query" || name == "rpm") &&
			strings.HasPrefix(args[len(args)-1], "xdg-desktop-portal") {
			return nil, &probeError{failure: probeFailureUnavailable}
		}
		return probe.output(ctx, name, args...)
	}
	report, err := preflight(
		context.Background(),
		validPreflightConfig(LaneWlroots, CellPortalAvailability),
		dependencies,
	)
	if err != nil {
		t.Fatalf("preflight failed: %v", err)
	}
	if !report.Desktop.Portal.Observed || report.Desktop.Portal.Available {
		t.Fatalf("portal availability report = %+v", report.Desktop.Portal)
	}
}

func TestPreflightWlrootsRecordsPortalWithoutNameOwnerAsUnavailable(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	dependencies := validDependencies(probe)
	dependencies.output = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "busctl" && args[len(args)-2] == "list" && args[len(args)-1] == portalBusName {
			return nil, nil
		}
		if name == "busctl" && args[len(args)-2] != "list" {
			return nil, &probeError{failure: probeFailureUnavailable}
		}
		return probe.output(ctx, name, args...)
	}
	report, err := preflight(
		context.Background(),
		validPreflightConfig(LaneWlroots, CellPortalAvailability),
		dependencies,
	)
	if err != nil {
		t.Fatalf("preflight failed: %v", err)
	}
	if report.Desktop.Portal.Available {
		t.Fatalf("portal availability report = %+v", report.Desktop.Portal)
	}
}

func TestValidOperatorAttestationRequiresExactBoundLine(t *testing.T) {
	t.Parallel()
	expected := operatorAttestation(validPreflightConfig(LaneGNOME, CellRemoteDesktop))
	for _, data := range []string{expected, expected + "\n"} {
		if !validOperatorAttestation([]byte(data), expected) {
			t.Fatalf("validOperatorAttestation rejected %q", data)
		}
	}
	for _, data := range []string{"ready", " " + expected, expected + "\n\n", expected + " stale"} {
		if validOperatorAttestation([]byte(data), expected) {
			t.Fatalf("validOperatorAttestation accepted %q", data)
		}
	}
}

func TestPreflightWlrootsPortalAvailabilityRejectsProbeFailure(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	dependencies := validDependencies(probe)
	dependencies.output = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "busctl" && args[len(args)-2] != "list" {
			return nil, errors.New("probe transport failed")
		}
		return probe.output(ctx, name, args...)
	}
	_, err := preflight(
		context.Background(),
		validPreflightConfig(LaneWlroots, CellPortalAvailability),
		dependencies,
	)
	if err == nil || !strings.Contains(err.Error(), "infrastructure") {
		t.Fatalf("preflight error = %v, want infrastructure failure", err)
	}
}

func TestPreflightRejectsIdentityAndTopologyMismatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		configure func(*PreflightConfig)
		want      string
	}{
		{
			name: "commit",
			configure: func(config *PreflightConfig) {
				config.ExpectedCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			},
			want: "approved commit",
		},
		{
			name: "workflow",
			configure: func(config *PreflightConfig) {
				config.Workflow = "ScreenCast E2E"
			},
			want: "workflow",
		},
		{
			name: "desktop",
			configure: func(config *PreflightConfig) {
				config.CurrentDesktop = "KDE"
			},
			want: "desktop identity",
		},
		{
			name: "outputs",
			configure: func(config *PreflightConfig) {
				config.OutputCount = 1
			},
			want: "required minimum",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := validPreflightConfig(LaneGNOME, CellRemoteDesktop)
			tc.configure(&config)
			_, err := preflight(context.Background(), config, validDependencies(&fakeProbe{}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("preflight error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPreflightBoundsProbeTimeoutWithoutPrivateOutput(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{block: true}
	config := validPreflightConfig(LaneGNOME, CellRemoteDesktop)
	config.ProbeTimeout = 10 * time.Millisecond
	_, err := preflight(context.Background(), config, validDependencies(probe))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("preflight error = %v, want bounded timeout", err)
	}
	if strings.Contains(err.Error(), config.SessionBusAddress) {
		t.Fatalf("preflight error leaked private session identity: %v", err)
	}
}

func TestPreflightRejectsUnavailableSessionBus(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	dependencies := validDependencies(probe)
	dependencies.output = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "busctl" && args[len(args)-1] == "org.freedesktop.DBus" {
			return nil, errors.New("private bus failure")
		}
		return probe.output(ctx, name, args...)
	}
	_, err := preflight(
		context.Background(),
		validPreflightConfig(LaneWlroots, CellNativeInput),
		dependencies,
	)
	if err == nil || !strings.Contains(err.Error(), "session bus") {
		t.Fatalf("preflight error = %v, want live session-bus rejection", err)
	}
}

func TestProbePortalRejectsMalformedRequiredProperty(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	dependencies := validDependencies(probe)
	dependencies.output = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "busctl" && args[len(args)-2] != "list" {
			return []byte("not-a-variant\n"), nil
		}
		return probe.output(ctx, name, args...)
	}
	_, err := preflight(
		context.Background(),
		validPreflightConfig(LaneGNOME, CellRemoteDesktop),
		dependencies,
	)
	if err == nil || !strings.Contains(err.Error(), "portal") {
		t.Fatalf("preflight error = %v, want malformed portal rejection", err)
	}
}

func TestParseOSReleaseRejectsMissingOrUnsafeIdentity(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		"ID=ubuntu\n",
		"ID=../../private\nVERSION_ID=24.04\n",
	} {
		if _, _, err := parseOSRelease([]byte(data)); err == nil {
			t.Fatalf("parseOSRelease(%q) succeeded", data)
		}
	}
}
