//go:build linux && !cgo && waylandoutputintegration

package robotgo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marang/robotgo/internal/waylandoutput"
)

const envRequireWaylandOutputIntegration = "ROBOTGO_REQUIRE_WAYLAND_OUTPUT_INTEGRATION"

const (
	envXDGConfigHome = "XDG_CONFIG_HOME"
	westonConfigName = "weston.ini"
)

type pureGoOutputWestonOptions struct {
	arguments       []string
	config          string
	display         string
	expectedOutputs int
}

func TestPureGoWaylandOutputEnumerationWeston(t *testing.T) {
	snapshot := startPureGoOutputWeston(t, pureGoOutputWestonOptions{
		arguments: []string{
			"--backend=headless",
			"--width=1024",
			"--height=768",
		},
		expectedOutputs: 1,
	})
	if len(snapshot.Outputs) != 1 {
		t.Fatalf("Weston outputs = %d, want 1", len(snapshot.Outputs))
	}

	x, y, width, height, err := GetDisplayBoundsE(0)
	if err != nil {
		t.Fatalf("GetDisplayBoundsE(0): %v", err)
	}
	if width <= 0 || height <= 0 {
		t.Fatalf("GetDisplayBoundsE(0) = %d,%d %dx%d", x, y, width, height)
	}
	if count, err := DisplaysNumE(); err != nil || count != 1 {
		t.Fatalf("DisplaysNumE() = %d, %v, want 1, nil", count, err)
	}
	rect, err := GetScreenRectE()
	if err != nil {
		t.Fatalf("GetScreenRectE(): %v", err)
	}
	if rect != (Rect{
		Point: Point{X: x, Y: y},
		Size:  Size{W: width, H: height},
	}) {
		t.Fatalf("GetScreenRectE() = %+v, want display bounds", rect)
	}
	capability := pureGoWaylandBoundsCapability()
	if !capability.Available || capability.Backend != featureBackendPureGoWaylandOutput {
		t.Fatalf("Pure-Go Wayland bounds capability = %+v", capability)
	}
}

func TestPureGoWaylandMultiOutputEnumerationWeston(t *testing.T) {
	x11Display := os.Getenv(envDisplay)
	if x11Display == "" {
		skipOrFailWaylandOutputIntegration(
			t,
			"DISPLAY is unavailable for Weston's multi-output X11 backend",
		)
	}

	snapshot := startPureGoOutputWeston(t, pureGoOutputWestonOptions{
		arguments: []string{
			"--backend=x11",
			"--output-count=2",
			"--width=800",
			"--height=600",
			"--renderer=pixman",
			"--no-input",
		},
		config: `[output]
name=screen0
mode=800x600
scale=2
transform=rotate-90

[output]
name=screen1
mode=800x600
scale=2
transform=rotate-90
`,
		display:         x11Display,
		expectedOutputs: 2,
	})
	want := []waylandoutput.Output{
		{X: 0, Y: 0, Width: 600, Height: 800, Scale: 2, Transform: 1, Logical: true},
		{X: 600, Y: 0, Width: 600, Height: 800, Scale: 2, Transform: 1, Logical: true},
	}
	assertPureGoWaylandMultiOutputGeometry(t, snapshot, want)
	assertPureGoWaylandMultiOutputPublicContract(t, want)
}

func assertPureGoWaylandMultiOutputGeometry(
	t *testing.T,
	snapshot waylandoutput.Snapshot,
	want []waylandoutput.Output,
) {
	t.Helper()
	if len(snapshot.Outputs) != len(want) {
		t.Fatalf("Weston outputs = %d, want %d", len(snapshot.Outputs), len(want))
	}
	for index, expected := range want {
		got := snapshot.Outputs[index]
		if got.X != expected.X ||
			got.Y != expected.Y ||
			got.Width != expected.Width ||
			got.Height != expected.Height ||
			got.Scale != expected.Scale ||
			got.Transform != expected.Transform ||
			got.Logical != expected.Logical {
			t.Fatalf("Weston output %d = %+v, want geometry %+v", index, got, expected)
		}

		x, y, width, height, err := GetDisplayBoundsE(index)
		if err != nil {
			t.Fatalf("GetDisplayBoundsE(%d): %v", index, err)
		}
		if x != expected.X || y != expected.Y ||
			width != expected.Width || height != expected.Height {
			t.Fatalf(
				"GetDisplayBoundsE(%d) = %d,%d %dx%d, want %d,%d %dx%d",
				index,
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
	}
}

func assertPureGoWaylandMultiOutputPublicContract(
	t *testing.T,
	want []waylandoutput.Output,
) {
	t.Helper()
	if count, err := DisplaysNumE(); err != nil || count != len(want) {
		t.Fatalf("DisplaysNumE() = %d, %v, want %d, nil", count, err, len(want))
	}
	if _, _, _, _, err := GetDisplayBoundsE(len(want)); err == nil {
		t.Fatal("GetDisplayBoundsE accepted an inactive output index")
	}
	width, height, err := GetScreenSizeE()
	if err != nil {
		t.Fatalf("GetScreenSizeE(): %v", err)
	}
	if width != want[0].Width || height != want[0].Height {
		t.Fatalf(
			"GetScreenSizeE() = %dx%d, want primary %dx%d",
			width,
			height,
			want[0].Width,
			want[0].Height,
		)
	}
	aggregate, err := GetScreenRectE(-1)
	if err != nil {
		t.Fatalf("GetScreenRectE(-1): %v", err)
	}
	if aggregate != (Rect{
		Point: Point{X: 0, Y: 0},
		Size:  Size{W: 1200, H: 800},
	}) {
		t.Fatalf("GetScreenRectE(-1) = %+v, want 0,0 1200x800", aggregate)
	}
	capability := pureGoWaylandBoundsCapability()
	if !capability.Available ||
		capability.Backend != featureBackendPureGoWaylandOutput ||
		!strings.HasPrefix(capability.Notes, "outputs=2 ") {
		t.Fatalf("Pure-Go Wayland multi-output capability = %+v", capability)
	}
}

func startPureGoOutputWeston(
	t *testing.T,
	options pureGoOutputWestonOptions,
) waylandoutput.Snapshot {
	t.Helper()
	if _, err := exec.LookPath("weston"); err != nil {
		skipOrFailWaylandOutputIntegration(t, "weston is unavailable: "+err.Error())
	}
	if options.expectedOutputs <= 0 {
		t.Fatal("Weston integration requires a positive expected output count")
	}

	runtimeDir := t.TempDir()
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if options.config != "" {
		configPath := filepath.Join(runtimeDir, westonConfigName)
		if err := os.WriteFile(configPath, []byte(options.config), 0o600); err != nil {
			t.Fatalf("write Weston configuration: %v", err)
		}
		options.arguments = append(
			[]string{"--config=" + westonConfigName},
			options.arguments...,
		)
	}
	socketName := "robotgo-purego-output-test"
	ctx, cancel := context.WithCancel(context.Background())
	arguments := append([]string{
		"--socket=" + socketName,
		"--idle-time=0",
		"--shell=kiosk-shell.so",
	}, options.arguments...)
	cmd := exec.CommandContext(
		ctx,
		"weston",
		arguments...,
	)
	cmd.Env = pureGoOutputWestonEnvironment(
		os.Environ(),
		runtimeDir,
		socketName,
		options.display,
	)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start Weston: %v", err)
	}

	processDone := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(processDone)
	}()
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			cancel()
			select {
			case <-processDone:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-processDone
			}
		})
	}
	t.Cleanup(cleanup)

	t.Setenv(envXDGRuntimeDir, runtimeDir)
	t.Setenv(envWaylandDisplay, socketName)
	t.Setenv(envDisplay, "")
	t.Setenv(envXDGSessionType, sessionTypeWayland)
	setTestRuntimeDisplayID(t, -1)

	socketPath := filepath.Join(runtimeDir, socketName)
	startupDeadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(startupDeadline) {
		select {
		case <-processDone:
			t.Fatalf("Weston stopped during startup: %v\n%s", waitErr, logs.String())
		default:
		}
		if _, err := os.Stat(socketPath); err == nil {
			queryCtx, queryCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			snapshot, queryErr := waylandoutput.Enumerate(queryCtx)
			queryCancel()
			if queryErr == nil && len(snapshot.Outputs) == options.expectedOutputs {
				return snapshot
			}
			if queryErr != nil {
				lastErr = queryErr
			} else {
				lastErr = fmt.Errorf(
					"Weston exposed %d outputs, want %d",
					len(snapshot.Outputs),
					options.expectedOutputs,
				)
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf(
		"Weston did not expose bounded output enumeration: %v\n%s",
		lastErr,
		logs.String(),
	)
	return waylandoutput.Snapshot{}
}

func pureGoOutputWestonEnvironment(
	environment []string,
	runtimeDir, socketName, display string,
) []string {
	filtered := make([]string, 0, len(environment)+4)
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && (name == envXDGRuntimeDir ||
			name == envWaylandDisplay ||
			name == envDisplay ||
			name == envXDGConfigHome) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(
		filtered,
		envXDGRuntimeDir+"="+runtimeDir,
		envWaylandDisplay+"="+socketName,
		envDisplay+"="+display,
		envXDGConfigHome+"="+runtimeDir,
	)
}

func skipOrFailWaylandOutputIntegration(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv(envRequireWaylandOutputIntegration) == "1" {
		t.Fatal(reason)
	}
	t.Skip(reason)
}
