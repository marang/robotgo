//go:build cgo && linux && wayland && swayintegration

package robotgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	envRequireSwayE2E  = "ROBOTGO_REQUIRE_SWAY_E2E"
	envSwayIsolated    = "ROBOTGO_SWAY_ISOLATED"
	envWLRBackends     = "WLR_BACKENDS"
	envWLRRenderer     = "WLR_RENDERER"
	envWLRNoInput      = "WLR_LIBINPUT_NO_DEVICES"
	envSwaySocket      = "SWAYSOCK"
	envSwayDesktop     = "XDG_CURRENT_DESKTOP"
	envSwaySessionType = "XDG_SESSION_TYPE"
	envSwayRuntimeDir  = "XDG_RUNTIME_DIR"
	swayFixtureAppID   = "wev"
	swayFixtureTitle   = "wev"
	swayFixtureX       = 120
	swayFixtureY       = 80
	swayFixtureWidth   = 480
	swayFixtureHeight  = 320
	swayConfigureEvent = "xdg_surface] configure:"
	swayOutputWidth    = 1280
	swayOutputHeight   = 720
	swaySecondOutputX  = -600
	swaySecondOutputY  = 0
	swaySecondWidth    = 400
	swaySecondHeight   = 600
	swayCommandTimeout = 3 * time.Second
	swayFixtureTimeout = 5 * time.Second
	maxFixtureLogBytes = 256 * 1024
)

type swayOutputIdentity struct {
	Name      string  `json:"name"`
	Active    bool    `json:"active"`
	Scale     float64 `json:"scale"`
	Transform string  `json:"transform"`
	Rect      struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"rect"`
}

type swayOutputExpectation struct {
	x, y, width, height int
	scale               float64
	transform           string
}

var (
	swaySingleOutput = []swayOutputExpectation{
		{0, 0, swayOutputWidth, swayOutputHeight, 1, "normal"},
	}
	swayMultiOutput = []swayOutputExpectation{
		{0, 0, swayOutputWidth, swayOutputHeight, 1, "normal"},
		{swaySecondOutputX, swaySecondOutputY, swaySecondWidth, swaySecondHeight, 2, "90"},
	}
)

type swayInputIdentity struct {
	Identifier string `json:"identifier"`
}

type swayFixtureNode struct {
	AppID         string            `json:"app_id"`
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Focused       bool              `json:"focused"`
	Rect          swayFixtureRect   `json:"rect"`
	WindowRect    swayFixtureRect   `json:"window_rect"`
	Geometry      swayFixtureRect   `json:"geometry"`
	Nodes         []swayFixtureNode `json:"nodes"`
	FloatingNodes []swayFixtureNode `json:"floating_nodes"`
}

type swayFixtureRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type lockedBoundedBuffer struct {
	mu       sync.Mutex
	data     bytes.Buffer
	maximum  int
	overflow bool
}

func (buffer *lockedBoundedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.overflow || buffer.data.Len()+len(data) > buffer.maximum {
		buffer.overflow = true
		return 0, errors.New("fixture event log exceeded its in-memory limit")
	}
	return buffer.data.Write(data)
}

func (buffer *lockedBoundedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.data.String()
}

func (buffer *lockedBoundedBuffer) Overflowed() bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.overflow
}

type swayFixture struct {
	command *exec.Cmd
	cancel  context.CancelFunc
	done    chan error
	log     *lockedBoundedBuffer
	once    sync.Once
}

func TestSwayNativeInputRuntime(t *testing.T) {
	requireIsolatedSway(t, swaySingleOutput)
	fixture := startSwayFixture(t)
	t.Cleanup(CloseWaylandInput)

	if err := MouseReady(); err != nil {
		t.Fatalf("native pointer readiness failed: %v", err)
	}
	if err := KeyboardReady(); err != nil {
		t.Fatalf("native keyboard readiness failed: %v", err)
	}
	waitForFixtureEvents(t, fixture.log, []string{"wl_keyboard] enter:"})
	if err := MoveE(swayOutputWidth/2, swayOutputHeight/2); err != nil {
		t.Fatalf("native absolute pointer motion failed: %v", err)
	}
	if err := ClickE("left"); err != nil {
		t.Fatalf("native pointer button failed: %v", err)
	}
	if err := ScrollE(0, -2); err != nil {
		t.Fatalf("native pointer scroll failed: %v", err)
	}
	if err := KeyTap("a"); err != nil {
		t.Fatalf("native keyboard tap failed: %v", err)
	}

	wantEvents := []string{
		"wl_pointer] motion:",
		"button: 272 (left), state: 1 (pressed)",
		"button: 272 (left), state: 0 (released)",
		"axis: 0 (vertical)",
		"wl_keyboard] key:",
		"utf8: 'a'",
	}
	waitForFixtureEvents(t, fixture.log, wantEvents)
	if count := strings.Count(fixture.log.String(), "wl_keyboard] key:"); count < 2 {
		t.Fatalf("native keyboard tap produced %d key events, want press and release", count)
	}
	CloseWaylandInput()
	waitForNoSwayInputs(t)
	fixture.close(t, false)
	if fixture.log.Overflowed() {
		t.Fatal("fixture event log exceeded its in-memory limit")
	}
}

func TestSwayNativeCaptureRuntime(t *testing.T) {
	requireIsolatedSway(t, swaySingleOutput)
	if os.Getenv(envDisablePortal) == "" {
		t.Fatal("native capture evidence requires portal fallback to be disabled")
	}
	background := startSwayBackground(t, "#214365")
	t.Cleanup(func() { stopFixtureProcess(background, swayFixtureTimeout) })

	SetWaylandBackend(WaylandBackendWlShm)
	t.Cleanup(func() { SetWaylandBackend(WaylandBackendAuto) })
	want := color.RGBA{R: 0x21, G: 0x43, B: 0x65, A: 0xff}
	deadline := time.Now().Add(swayFixtureTimeout)
	var last color.RGBA
	for time.Now().Before(deadline) {
		bitmap, err := CaptureScreen()
		if err != nil {
			t.Fatalf("native Sway screencopy failed: %v", err)
		}
		image, imageErr := ToRGBAE(bitmap)
		FreeBitmap(bitmap)
		if imageErr != nil {
			t.Fatalf("convert native Sway capture: %v", imageErr)
		}
		if image.Bounds().Dx() != swayOutputWidth || image.Bounds().Dy() != swayOutputHeight {
			t.Fatalf("native Sway capture dimensions = %v", image.Bounds())
		}
		last = color.RGBAModel.Convert(image.At(swayOutputWidth/2, swayOutputHeight/2)).(color.RGBA)
		if last == want {
			if LastBackend() != BackendScreencopy {
				t.Fatalf("capture backend = %q, want %q", LastBackend(), BackendScreencopy)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("synthetic Sway background was not captured in memory: got %v", last)
}

func TestSwayNativeWindowRuntime(t *testing.T) {
	requireIsolatedSway(t, swaySingleOutput)
	fixture := startSwayFixture(t)
	configureSwayFixtureGeometry(t, fixture.log)
	waitForSwayFixtureGeometry(t)
	title, err := GetTitleE()
	if err != nil {
		t.Fatalf("query Sway fixture title: %v", err)
	}
	if title != swayFixtureTitle {
		t.Fatalf("active Sway title = %q, want %q", title, swayFixtureTitle)
	}
	capability := GetLinuxCapabilities().Window
	if !capability.Available || capability.Backend != windowBackendSway {
		t.Fatalf("Sway window capability = %+v", capability)
	}
	if _, _, _, _, err := GetBoundsE(os.Getpid()); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("pid-specific Sway bounds error = %v, want ErrNotSupported", err)
	}
	if _, _, _, _, err := GetClientE(1, 1); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("handle-specific Sway client error = %v, want ErrNotSupported", err)
	}
	if err := CloseWindowE(); err != nil {
		t.Fatalf("close self-owned Sway fixture: %v", err)
	}
	fixture.close(t, true)
}

func configureSwayFixtureGeometry(t *testing.T, log *lockedBoundedBuffer) {
	t.Helper()
	criterion := `[app_id="` + swayFixtureAppID + `"]`
	configureCount := waitForSwayConfigureSettled(t, log, 0)
	runSwayFixtureCommand(t, criterion, "border", "none")
	configureCount = waitForSwayConfigureSettled(t, log, configureCount)
	runSwayFixtureCommand(t, criterion, "floating", "enable")
	configureCount = waitForSwayConfigureSettled(t, log, configureCount)
	waitForSwayFixtureState(t, "confirmed floating mode", func(node swayFixtureNode) bool {
		return node.Type == swayNodeTypeFloatingContainer &&
			node.Geometry.Width > 0 &&
			node.Geometry.Height > 0 &&
			node.Rect.Width == node.Geometry.Width &&
			node.Rect.Height == node.Geometry.Height &&
			node.WindowRect.Width == node.Geometry.Width &&
			node.WindowRect.Height == node.Geometry.Height
	})
	runSwayFixtureCommand(
		t,
		criterion,
		"resize", "set",
		"width", strconv.Itoa(swayFixtureWidth), "px",
		"height", strconv.Itoa(swayFixtureHeight), "px",
	)
	_ = waitForSwayConfigureSettled(t, log, configureCount)
	waitForSwayFixtureState(t, "confirmed size", func(node swayFixtureNode) bool {
		return node.Rect.Width == swayFixtureWidth &&
			node.Rect.Height == swayFixtureHeight &&
			node.WindowRect.Width == swayFixtureWidth &&
			node.WindowRect.Height == swayFixtureHeight
	})
	runSwayFixtureCommand(
		t,
		criterion,
		"move", "position",
		strconv.Itoa(swayFixtureX), "px",
		strconv.Itoa(swayFixtureY), "px",
	)
}

func waitForSwayConfigureSettled(
	t *testing.T,
	log *lockedBoundedBuffer,
	previous int,
) int {
	t.Helper()
	deadline := time.Now().Add(swayFixtureTimeout)
	last := previous
	stable := 0
	for time.Now().Before(deadline) {
		count := strings.Count(log.String(), swayConfigureEvent)
		if count > previous && count == last {
			stable++
			if stable >= 3 {
				return count
			}
		} else {
			stable = 0
		}
		last = count
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf(
		"self-owned Sway fixture configure count did not advance past %d (last %d)",
		previous,
		last,
	)
	return 0
}

func runSwayFixtureCommand(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), swayCommandTimeout)
	output, err := exec.CommandContext(ctx, cmdSwayMsg, args...).CombinedOutput()
	cancel()
	if err != nil {
		t.Fatalf("configure self-owned Sway fixture geometry: %v: %s", err, output)
	}
}

func waitForSwayFixtureState(
	t *testing.T,
	description string,
	ready func(swayFixtureNode) bool,
) {
	t.Helper()
	deadline := time.Now().Add(swayFixtureTimeout)
	var last *swayFixtureNode
	for time.Now().Before(deadline) {
		var tree swayFixtureNode
		runSwayJSON(t, &tree, "get_tree")
		if node := findSwayFixture(tree); node != nil {
			snapshot := *node
			last = &snapshot
			if ready(snapshot) {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if last == nil {
		t.Fatalf("self-owned Sway fixture disappeared before reaching %s", description)
	}
	t.Fatalf(
		"self-owned Sway fixture did not reach %s: type=%q rect=%+v window_rect=%+v geometry=%+v",
		description,
		last.Type,
		last.Rect,
		last.WindowRect,
		last.Geometry,
	)
}

func waitForSwayFixtureGeometry(t *testing.T) {
	t.Helper()
	assertGeometry := func(name string, client bool) bool {
		t.Helper()
		var x, y, width, height int
		var err error
		if client {
			x, y, width, height, err = GetClientE(0)
		} else {
			x, y, width, height, err = GetBoundsE(0)
		}
		if err != nil {
			return false
		}
		if x != swayFixtureX || y != swayFixtureY ||
			width != swayFixtureWidth || height != swayFixtureHeight {
			return false
		}
		var legacyX, legacyY, legacyWidth, legacyHeight int
		if client {
			legacyX, legacyY, legacyWidth, legacyHeight = GetClient(0)
		} else {
			legacyX, legacyY, legacyWidth, legacyHeight = GetBounds(0)
		}
		if legacyX != x || legacyY != y ||
			legacyWidth != width || legacyHeight != height {
			t.Fatalf(
				"legacy %s geometry = %d,%d %dx%d, error API = %d,%d %dx%d",
				name,
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
		return true
	}

	deadline := time.Now().Add(swayFixtureTimeout)
	for time.Now().Before(deadline) {
		if assertGeometry("outer", false) && assertGeometry("client", true) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf(
		"self-owned Sway fixture did not reach exact geometry %d,%d %dx%d",
		swayFixtureX,
		swayFixtureY,
		swayFixtureWidth,
		swayFixtureHeight,
	)
}

func TestSwayNativeOutputRuntime(t *testing.T) {
	requireIsolatedSway(t, swaySingleOutput)
	count, err := DisplaysNumE()
	if err != nil {
		t.Fatalf("enumerate Sway outputs: %v", err)
	}
	if count != 1 {
		t.Fatalf("Sway output count = %d, want 1", count)
	}
	x, y, width, height, err := GetDisplayBoundsE(0)
	if err != nil {
		t.Fatalf("query Sway output bounds: %v", err)
	}
	if x != 0 || y != 0 || width != swayOutputWidth || height != swayOutputHeight {
		t.Fatalf("Sway output bounds = %d,%d %dx%d", x, y, width, height)
	}
	rect, err := GetScreenRectE(-1)
	if err != nil {
		t.Fatalf("query aggregate Sway bounds: %v", err)
	}
	if rect != (Rect{Point: Point{}, Size: Size{W: swayOutputWidth, H: swayOutputHeight}}) {
		t.Fatalf("aggregate Sway bounds = %+v", rect)
	}
	width, height, err = GetScreenSizeE()
	if err != nil || width != swayOutputWidth || height != swayOutputHeight {
		t.Fatalf("Sway screen size = %dx%d, %v", width, height, err)
	}
}

func TestSwayNativeOutputMultiRuntime(t *testing.T) {
	requireIsolatedSway(t, swayMultiOutput)
	count, err := DisplaysNumE()
	if err != nil {
		t.Fatalf("enumerate multi-output Sway topology: %v", err)
	}
	if count != len(swayMultiOutput) {
		t.Fatalf("Sway output count = %d, want %d", count, len(swayMultiOutput))
	}
	assertSwayPublicBounds(t, 0, swayMultiOutput[0])
	assertSwayPublicBounds(t, 1, swayMultiOutput[1])
	if _, _, _, _, err := GetDisplayBoundsE(count); err == nil {
		t.Fatalf("GetDisplayBoundsE accepted inactive output index %d", count)
	}

	wantAggregate := Rect{
		Point: Point{X: swaySecondOutputX, Y: 0},
		Size:  Size{W: swayOutputWidth - swaySecondOutputX, H: swayOutputHeight},
	}
	for _, displayID := range [][]int{nil, {-1}} {
		got, err := GetScreenRectE(displayID...)
		if err != nil {
			t.Fatalf("query aggregate Sway bounds %v: %v", displayID, err)
		}
		if got != wantAggregate {
			t.Fatalf("aggregate Sway bounds %v = %+v, want %+v", displayID, got, wantAggregate)
		}
	}
}

func TestSwayPortalAvailabilityRuntime(t *testing.T) {
	requireIsolatedSway(t, swaySingleOutput)
	ctx, cancel := context.WithTimeout(context.Background(), swayCommandTimeout)
	defer cancel()
	status, err := GetRemoteDesktopInputStatus(ctx)
	if status.SessionActive || status.RestoreTokenAvailable || len(status.Streams) != 0 {
		t.Fatalf("portal availability probe created or retained a session: %+v", status)
	}
	if status.PortalAvailable && status.PortalVersion == 0 {
		t.Fatal("available RemoteDesktop portal has no interface version")
	}
	if !status.PortalAvailable && err == nil {
		t.Fatal("unavailable RemoteDesktop portal returned no actionable error")
	}
}

func requireIsolatedSway(t *testing.T, expectedOutputs []swayOutputExpectation) {
	t.Helper()
	checks := map[string]string{
		envRequireSwayE2E:  "1",
		envSwayIsolated:    "1",
		envWLRBackends:     "headless",
		envWLRRenderer:     "pixman",
		envWLRNoInput:      "1",
		envSwaySessionType: "wayland",
	}
	for name, want := range checks {
		if got := os.Getenv(name); got != want {
			t.Fatalf("isolated Sway contract requires %s=%q, got %q", name, want, got)
		}
	}
	if os.Getenv(envDisplay) != "" {
		t.Fatal("isolated Sway contract requires DISPLAY to be unset")
	}
	if !strings.EqualFold(os.Getenv(envSwayDesktop), "sway") {
		t.Fatal("isolated Sway contract requires the Sway desktop identity")
	}
	runtimeDirectory := filepath.Clean(os.Getenv(envSwayRuntimeDir))
	if !filepath.IsAbs(runtimeDirectory) || runtimeDirectory == string(filepath.Separator) {
		t.Fatal("isolated Sway runtime directory is invalid")
	}
	assertSocketInRuntime(t, runtimeDirectory, os.Getenv(envWaylandDisplay))
	assertSocketInRuntime(t, runtimeDirectory, os.Getenv(envSwaySocket))

	var outputs []swayOutputIdentity
	runSwayJSON(t, &outputs, "get_outputs")
	sort.Slice(outputs, func(left, right int) bool {
		return outputs[left].Name < outputs[right].Name
	})
	if len(outputs) != len(expectedOutputs) {
		t.Fatalf("isolated Sway output count = %d, want %d", len(outputs), len(expectedOutputs))
	}
	for index, expected := range expectedOutputs {
		output := outputs[index]
		if !output.Active || !strings.HasPrefix(output.Name, "HEADLESS-") ||
			output.Rect.X != expected.x || output.Rect.Y != expected.y ||
			output.Rect.Width != expected.width || output.Rect.Height != expected.height ||
			output.Scale != expected.scale || output.Transform != expected.transform {
			t.Fatalf("isolated Sway output %d identity is invalid: %+v", index, output)
		}
	}
	var inputs []swayInputIdentity
	runSwayJSON(t, &inputs, "get_inputs")
	if len(inputs) != 0 {
		t.Fatalf("isolated Sway exposed input devices before the test: %d", len(inputs))
	}
	if DetectDisplayServer() != DisplayServerWayland {
		t.Fatal("isolated Sway did not select the Wayland backend")
	}
}

func assertSwayPublicBounds(t *testing.T, displayID int, expected swayOutputExpectation) {
	t.Helper()
	x, y, width, height, err := GetDisplayBoundsE(displayID)
	if err != nil {
		t.Fatalf("GetDisplayBoundsE(%d): %v", displayID, err)
	}
	if x != expected.x || y != expected.y || width != expected.width || height != expected.height {
		t.Fatalf(
			"GetDisplayBoundsE(%d) = %d,%d %dx%d, want %d,%d %dx%d",
			displayID, x, y, width, height,
			expected.x, expected.y, expected.width, expected.height,
		)
	}
	rect, err := GetScreenRectE(displayID)
	if err != nil {
		t.Fatalf("GetScreenRectE(%d): %v", displayID, err)
	}
	want := Rect{
		Point: Point{X: expected.x, Y: expected.y},
		Size:  Size{W: expected.width, H: expected.height},
	}
	if rect != want {
		t.Fatalf("GetScreenRectE(%d) = %+v, want %+v", displayID, rect, want)
	}
}

func assertSocketInRuntime(t *testing.T, runtimeDirectory, value string) {
	t.Helper()
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(runtimeDirectory, value)
	}
	clean := filepath.Clean(path)
	relative, err := filepath.Rel(runtimeDirectory, clean)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("isolated Sway socket escapes its private runtime directory")
	}
	info, err := os.Lstat(clean)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatal("isolated Sway socket is unavailable")
	}
}

func runSwayJSON(t *testing.T, destination any, request string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), swayCommandTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "swaymsg", "-t", request, "-r").Output()
	if err != nil {
		t.Fatalf("bounded Sway %s query failed: %v", request, err)
	}
	if err := json.Unmarshal(output, destination); err != nil {
		t.Fatalf("decode sanitized Sway %s response: %v", request, err)
	}
}

func waitForNoSwayInputs(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(swayFixtureTimeout)
	for time.Now().Before(deadline) {
		var inputs []swayInputIdentity
		runSwayJSON(t, &inputs, "get_inputs")
		if len(inputs) == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("native input objects survived explicit cleanup")
}

func startSwayFixture(t *testing.T) *swayFixture {
	t.Helper()
	for _, executable := range []string{"stdbuf", "wev"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Fatalf("%s fixture dependency is unavailable: %v", executable, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	log := &lockedBoundedBuffer{maximum: maxFixtureLogBytes}
	command := exec.CommandContext(
		ctx,
		"stdbuf", "-oL", "-eL",
		"wev",
		"-f", "wl_pointer",
		"-f", "wl_keyboard",
		"-f", "xdg_surface",
	)
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start self-owned Sway fixture: %v", err)
	}
	fixture := &swayFixture{
		command: command,
		cancel:  cancel,
		done:    make(chan error, 1),
		log:     log,
	}
	go func() { fixture.done <- command.Wait() }()
	t.Cleanup(func() { fixture.close(t, false) })
	waitForSwayFixture(t)
	return fixture
}

func waitForSwayFixture(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(swayFixtureTimeout)
	for time.Now().Before(deadline) {
		var tree swayFixtureNode
		runSwayJSON(t, &tree, "get_tree")
		if node := findSwayFixture(tree); node != nil && node.Focused && node.Name == swayFixtureTitle {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("self-owned Sway fixture did not become focused")
}

func findSwayFixture(node swayFixtureNode) *swayFixtureNode {
	if node.AppID == swayFixtureAppID {
		return &node
	}
	for _, children := range [][]swayFixtureNode{node.Nodes, node.FloatingNodes} {
		for _, child := range children {
			if match := findSwayFixture(child); match != nil {
				return match
			}
		}
	}
	return nil
}

func waitForFixtureEvents(t *testing.T, log *lockedBoundedBuffer, patterns []string) {
	t.Helper()
	deadline := time.Now().Add(swayFixtureTimeout)
	missing := make([]string, 0, len(patterns))
	for time.Now().Before(deadline) {
		output := log.String()
		missing = missing[:0]
		for _, pattern := range patterns {
			if !strings.Contains(output, pattern) {
				missing = append(missing, pattern)
			}
		}
		if len(missing) == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("self-owned fixture is missing fixed event classes: %q", missing)
}

func (fixture *swayFixture) close(t *testing.T, alreadyClosed bool) {
	t.Helper()
	fixture.once.Do(func() {
		if !alreadyClosed {
			select {
			case err := <-fixture.done:
				fixture.cancel()
				if err != nil {
					t.Errorf("self-owned Sway fixture exited before cleanup: %v", err)
				}
				return
			default:
			}
			ctx, cancel := context.WithTimeout(context.Background(), swayCommandTimeout)
			command := exec.CommandContext(ctx, "swaymsg", `[app_id="`+swayFixtureAppID+`"]`, "kill")
			_ = command.Run()
			cancel()
		}
		select {
		case <-fixture.done:
			fixture.cancel()
		case <-time.After(swayFixtureTimeout):
			fixture.cancel()
			_ = fixture.command.Process.Kill()
			<-fixture.done
			t.Error("self-owned Sway fixture did not exit before timeout")
		}
	})
}

func startSwayBackground(t *testing.T, value string) *exec.Cmd {
	t.Helper()
	if _, err := exec.LookPath("swaybg"); err != nil {
		t.Fatalf("swaybg fixture is unavailable: %v", err)
	}
	command := exec.Command("swaybg", "-c", value)
	if err := command.Start(); err != nil {
		t.Fatalf("start synthetic Sway background: %v", err)
	}
	return command
}

func stopFixtureProcess(command *exec.Cmd, timeout time.Duration) {
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-done
	}
}
