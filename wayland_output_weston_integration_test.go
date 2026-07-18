//go:build linux && !cgo && waylandoutputintegration

package robotgo

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marang/robotgo/internal/waylandoutput"
)

func TestPureGoWaylandOutputEnumerationWeston(t *testing.T) {
	snapshot := startPureGoOutputWeston(t)
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

func startPureGoOutputWeston(t *testing.T) waylandoutput.Snapshot {
	t.Helper()
	if _, err := exec.LookPath("weston"); err != nil {
		t.Skipf("weston is unavailable: %v", err)
	}

	runtimeDir := t.TempDir()
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	socketName := "robotgo-purego-output-test"
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(
		ctx,
		"weston",
		"--backend=headless",
		"--socket="+socketName,
		"--width=1024",
		"--height=768",
		"--idle-time=0",
	)
	cmd.Env = append(
		os.Environ(),
		envXDGRuntimeDir+"="+runtimeDir,
		"WAYLAND_DISPLAY="+socketName,
		"DISPLAY=",
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
			if queryErr == nil {
				return snapshot
			}
			lastErr = queryErr
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
