//go:build cgo

package robotgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	commandpkg "github.com/marang/robotgo/internal/command"
)

const (
	windowBackendSway              = "sway"
	windowBackendHypr              = "hyprland"
	windowBackendWlroots           = "wlroots-generic"
	windowBackendCore              = "wayland-core"
	reasonWaylandGlobalUnsupported = "global foreign-window operations are not universally available in Wayland core protocols"
	notesWlrootsBackend            = "wlroots generic backend selected; operation support to be implemented via wlroots-compatible paths"
	reasonCompositorSpecific       = "compositor-specific backend selected with partial operation support"
	notesSwayPartialSupport        = "supports active-window title retrieval and close; active minimize/maximize available when wlrctl is present"
	notesHyprPartialSupport        = "supports active-window title, close, and reliable maximize query/set/restore across Hyprland hyprlang/Lua config providers; active minimize requires wlrctl"
	cmdSwayMsg                     = "swaymsg"
	cmdHyprCtl                     = "hyprctl"
	cmdWlrCtl                      = "wlrctl"
	argJSON                        = "-j"
	argRawJSON                     = "-r"
	argType                        = "-t"
	argGetTree                     = "get_tree"
	argStatus                      = "status"
	argActiveWindow                = "activewindow"
	argWindow                      = "window"
	argMinimize                    = "minimize"
	argMaximize                    = "maximize"
	argStateActive                 = "state:active"
	argKill                        = "kill"
	argDispatch                    = "dispatch"
	argKillActive                  = "killactive"
	argFullscreenState             = "fullscreenstate"
	argHyprlandNone                = "0"
	argHyprlandMaximized           = "1"
	hyprlandConfigProviderLua      = "lua"
	hyprlandConfigProviderLegacy   = "hyprlang"
	hyprlandStatusUnsupported      = "unknown request"
	hyprlandLuaCloseActive         = `hl.dsp.window.close({})`
	hyprlandLuaMaximizeActive      = `hl.dsp.window.fullscreen_state({ internal = 1, client = 1, action = "set" })`
	hyprlandLuaRestoreActive       = `hl.dsp.window.fullscreen_state({ internal = 0, client = 0, action = "set" })`
	windowCommandTimeout           = 2 * time.Second
	hyprlandFullscreenNone         = 0
	hyprlandFullscreenMaximized    = 1
	hyprlandFullscreenFull         = 2
	hyprlandFullscreenMaxAndFull   = 3
)

var (
	errWindowTitleUnavailable    = errors.New("window title unavailable from compositor backend")
	errWindowStateUnavailable    = errors.New("window state unavailable from compositor backend")
	errWindowOperationFailed     = errors.New("window operation failed for compositor backend")
	errHyprlandStatusUnavailable = errors.New("hyprland status request unavailable")
	runWindowCommand             = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return commandpkg.Output(ctx, name, args...)
	}
)

type windowBackend interface {
	Name() string
	Capability() FeatureCapability
	SetActive(win Handle) error
	Minimize(pid int, state bool, isPid bool) error
	Maximize(pid int, state bool, isPid bool) error
	Maximized() (bool, error)
	Close(args ...int) error
	Title(args ...int) (string, error)
}

type nativeWindowBackend struct{}

func (nativeWindowBackend) Name() string {
	if runtime.GOOS == "linux" && selectedDisplayServer() == DisplayServerX11 {
		return "x11"
	}
	return "native"
}

func (nativeWindowBackend) Capability() FeatureCapability {
	reason := "operation supported by current non-wayland-global backend"
	notes := "window operations use platform native backend"
	available := true
	backend := "native"
	switch runtime.GOOS {
	case "linux":
		backend = "x11"
		reason = "X11 window backend is available with explicit per-operation support"
		notes = "activation, title, and close use X11; minimize/maximize return ErrNotSupported because the legacy native path has no implementation"
		if err := nativeX11WindowReady(); err != nil {
			available = false
			reason = err.Error()
			notes = "build the native X11 backend and verify the configured X11 display"
		}
	case "darwin":
		notes = "native window operations use Accessibility APIs; maximize returns ErrNotSupported"
	}
	return FeatureCapability{
		Available: available,
		Fallback:  false,
		Backend:   backend,
		Reason:    reason,
		Notes:     notes,
	}
}

func nativeX11WindowReady() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	if selectedDisplayServer() != DisplayServerX11 {
		return fmt.Errorf("%w: no X11 display server is selected", ErrNotSupported)
	}
	if !nativeX11BackendCompiled() {
		return fmt.Errorf("%w: native X11 window backend is not compiled", ErrNotSupported)
	}
	unlock := lockNativeX11Display()
	defer unlock()
	return nativeX11DisplayReadyLocked()
}

func (nativeWindowBackend) SetActive(win Handle) error {
	var zero Handle
	if win == zero {
		return fmt.Errorf("%w: active window handle is zero", errWindowOperationFailed)
	}
	if err := nativeX11WindowReady(); err != nil {
		return err
	}
	if !nativeSetActive(win) {
		return fmt.Errorf("%w: native backend could not activate target window", errWindowOperationFailed)
	}
	return nil
}

func (nativeWindowBackend) Minimize(pid int, state bool, isPid bool) error {
	if pid <= 0 {
		return fmt.Errorf("%w: invalid window target %d", errWindowOperationFailed, pid)
	}
	if err := nativeX11WindowReady(); err != nil {
		return err
	}
	if !nativeMinWindow(pid, state, isPid) {
		if runtime.GOOS == "linux" {
			return fmt.Errorf("%w: native X11 minimize is not implemented", ErrNotSupported)
		}
		return fmt.Errorf("%w: native backend could not change minimized state", errWindowOperationFailed)
	}
	return nil
}

func (nativeWindowBackend) Maximize(pid int, state bool, isPid bool) error {
	if pid <= 0 {
		return fmt.Errorf("%w: invalid window target %d", errWindowOperationFailed, pid)
	}
	if err := nativeX11WindowReady(); err != nil {
		return err
	}
	if !nativeMaxWindow(pid, state, isPid) {
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			return fmt.Errorf("%w: native maximize is not implemented on %s", ErrNotSupported, runtime.GOOS)
		}
		return fmt.Errorf("%w: native backend could not change maximized state", errWindowOperationFailed)
	}
	return nil
}

func (nativeWindowBackend) Maximized() (bool, error) {
	return false, linuxWindowStateNotSupported("query maximized state")
}

func (nativeWindowBackend) Close(args ...int) error {
	if err := nativeX11WindowReady(); err != nil {
		return err
	}
	if len(args) <= 0 {
		if !nativeCloseMainWindow() {
			return fmt.Errorf("%w: native backend could not close active window", errWindowOperationFailed)
		}
		return nil
	}

	pid := args[0]
	if pid <= 0 {
		return fmt.Errorf("%w: invalid window target %d", errWindowOperationFailed, pid)
	}
	isPid := len(args) > 1 || currentTreatAsHandle()
	if !nativeCloseWindowByPid(pid, isPid) {
		return fmt.Errorf("%w: native backend could not close target window", errWindowOperationFailed)
	}
	return nil
}

func (nativeWindowBackend) Title(args ...int) (string, error) {
	if err := nativeX11WindowReady(); err != nil {
		return "", err
	}
	var title string
	if len(args) <= 0 {
		title = nativeGetMainTitle()
	} else if args[0] <= 0 {
		return "", fmt.Errorf("%w: invalid window target %d", errWindowTitleUnavailable, args[0])
	} else if len(args) > 1 {
		title = nativeGetInternalTitle(args[0], args[1])
	} else {
		title = nativeGetInternalTitle(args[0], 0)
	}
	if title == "" || title == "is_valid failed." {
		return "", errWindowTitleUnavailable
	}
	return title, nil
}

type waylandCoreWindowBackend struct {
	compositor string
}

func (b waylandCoreWindowBackend) Name() string {
	if b.compositor == "" {
		return windowBackendCore
	}
	return windowBackendCore + "/" + b.compositor
}

func (b waylandCoreWindowBackend) Capability() FeatureCapability {
	note := "wayland core does not provide universal global foreign-window control"
	if b.compositor != "" {
		note += "; compositor=" + b.compositor
	}
	return FeatureCapability{
		Available: false,
		Fallback:  false,
		Backend:   b.Name(),
		Reason:    reasonWaylandGlobalUnsupported,
		Notes:     note,
	}
}

func (b waylandCoreWindowBackend) unsupported(op string) error {
	if b.compositor == "" {
		return waylandWindowNotSupported(op)
	}
	return fmt.Errorf("%w (compositor=%s)", waylandWindowNotSupported(op), b.compositor)
}

func (b waylandCoreWindowBackend) SetActive(win Handle) error {
	_ = win
	return b.unsupported("set active window")
}

func (b waylandCoreWindowBackend) Minimize(pid int, state bool, isPid bool) error {
	_, _, _ = pid, state, isPid
	return b.unsupported("minimize window")
}

func (b waylandCoreWindowBackend) Maximize(pid int, state bool, isPid bool) error {
	_, _, _ = pid, state, isPid
	return b.unsupported("maximize window")
}

func (b waylandCoreWindowBackend) Maximized() (bool, error) {
	return false, b.unsupported("query maximized state")
}

func (b waylandCoreWindowBackend) Close(args ...int) error {
	_ = args
	return b.unsupported("close window")
}

func (b waylandCoreWindowBackend) Title(args ...int) (string, error) {
	_ = args
	return "", b.unsupported("get window title")
}

func newSwayWindowBackend() windowBackend {
	return swayWindowBackend{}
}

func newHyprlandWindowBackend() windowBackend {
	return hyprlandWindowBackend{}
}

func newWlrootsGenericWindowBackend() windowBackend {
	return wlrootsGenericWindowBackend{}
}

type swayWindowBackend struct{}

func (swayWindowBackend) Name() string { return windowBackendSway }
func (swayWindowBackend) Capability() FeatureCapability {
	available := hasCommand(cmdSwayMsg)
	reason := reasonCompositorSpecific
	notes := notesSwayPartialSupport
	if !available {
		reason = "sway backend selected but swaymsg command not found"
		notes = "install swaymsg to enable compositor-specific operations"
	}
	return FeatureCapability{
		Available: available,
		Fallback:  false,
		Backend:   windowBackendSway,
		Reason:    reason,
		Notes:     notes,
	}
}
func (swayWindowBackend) SetActive(win Handle) error {
	_ = win
	return waylandWindowNotSupported("set active window")
}
func (swayWindowBackend) Minimize(pid int, state bool, isPid bool) error {
	if pid > 0 || isPid {
		return waylandWindowNotSupported("minimize window by pid/handle")
	}
	if !state {
		return waylandWindowNotSupported("restore minimized window")
	}
	return runWlrctlActiveWindowAction("minimize window", argMinimize)
}
func (swayWindowBackend) Maximize(pid int, state bool, isPid bool) error {
	if pid > 0 || isPid {
		return waylandWindowNotSupported("maximize window by pid/handle")
	}
	if !state {
		return waylandWindowNotSupported("restore maximized window")
	}
	return runWlrctlActiveWindowAction("maximize window", argMaximize)
}
func (swayWindowBackend) Maximized() (bool, error) {
	return false, waylandWindowNotSupported("query maximized state")
}
func (swayWindowBackend) Close(args ...int) error {
	if len(args) > 0 {
		return waylandWindowNotSupported("close window by pid/handle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	if _, err := runWindowCommand(ctx, cmdSwayMsg, argKill); err != nil {
		return fmt.Errorf("%w: %v", errWindowOperationFailed, err)
	}
	return nil
}
func (swayWindowBackend) Title(args ...int) (string, error) {
	if len(args) > 0 {
		return "", waylandWindowNotSupported("get window title by pid/handle")
	}
	return getSwayActiveWindowTitle()
}

type hyprlandWindowBackend struct{}

func (hyprlandWindowBackend) Name() string { return windowBackendHypr }
func (hyprlandWindowBackend) Capability() FeatureCapability {
	available := hasCommand(cmdHyprCtl)
	reason := reasonCompositorSpecific
	notes := notesHyprPartialSupport
	if !available {
		reason = "hyprland backend selected but hyprctl command not found"
		notes = "install hyprctl to enable compositor-specific operations"
	}
	return FeatureCapability{
		Available: available,
		Fallback:  false,
		Backend:   windowBackendHypr,
		Reason:    reason,
		Notes:     notes,
	}
}
func (hyprlandWindowBackend) SetActive(win Handle) error {
	_ = win
	return waylandWindowNotSupported("set active window")
}
func (hyprlandWindowBackend) Minimize(pid int, state bool, isPid bool) error {
	if pid > 0 || isPid {
		return waylandWindowNotSupported("minimize window by pid/handle")
	}
	if !state {
		return waylandWindowNotSupported("restore minimized window")
	}
	return runWlrctlActiveWindowAction("minimize window", argMinimize)
}
func (hyprlandWindowBackend) Maximize(pid int, state bool, isPid bool) error {
	if pid > 0 || isPid {
		return waylandWindowNotSupported("maximize window by pid/handle")
	}
	if !hasCommand(cmdHyprCtl) {
		return waylandWindowNotSupported("maximize window (hyprctl unavailable)")
	}
	info, err := getHyprlandActiveWindow()
	if err != nil {
		return fmt.Errorf("%w: %w", errWindowStateUnavailable, err)
	}
	if info.Fullscreen == nil || info.FullscreenClient == nil {
		return fmt.Errorf(
			"%w: hyprland response omitted fullscreen state",
			errWindowStateUnavailable,
		)
	}
	if !validHyprlandFullscreenMode(*info.Fullscreen) ||
		!validHyprlandFullscreenMode(*info.FullscreenClient) {
		return fmt.Errorf(
			"%w: invalid hyprland fullscreen state internal=%d client=%d",
			errWindowStateUnavailable,
			*info.Fullscreen,
			*info.FullscreenClient,
		)
	}

	if state {
		if *info.Fullscreen == hyprlandFullscreenMaximized &&
			*info.FullscreenClient == hyprlandFullscreenMaximized {
			return nil
		}
	} else {
		if !hyprlandFullscreenModeIsMaximized(*info.Fullscreen) &&
			!hyprlandFullscreenModeIsMaximized(*info.FullscreenClient) {
			return nil
		}
	}

	mode := argHyprlandNone
	if state {
		mode = argHyprlandMaximized
	}
	luaExpression := hyprlandLuaRestoreActive
	if state {
		luaExpression = hyprlandLuaMaximizeActive
	}
	args, err := resolveHyprlandDispatchArgs(
		[]string{argFullscreenState, mode, mode},
		luaExpression,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", errWindowOperationFailed, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	if _, err := runWindowCommand(
		ctx,
		cmdHyprCtl,
		args...,
	); err != nil {
		return fmt.Errorf("%w: %w", errWindowOperationFailed, err)
	}
	return nil
}
func (hyprlandWindowBackend) Maximized() (bool, error) {
	if !hasCommand(cmdHyprCtl) {
		return false, waylandWindowNotSupported("query maximized state (hyprctl unavailable)")
	}
	info, err := getHyprlandActiveWindow()
	if err != nil {
		return false, fmt.Errorf("%w: %w", errWindowStateUnavailable, err)
	}
	if info.Fullscreen == nil {
		return false, fmt.Errorf("%w: hyprland response omitted fullscreen state", errWindowStateUnavailable)
	}
	if !validHyprlandFullscreenMode(*info.Fullscreen) {
		return false, fmt.Errorf(
			"%w: invalid hyprland fullscreen state %d",
			errWindowStateUnavailable,
			*info.Fullscreen,
		)
	}
	return hyprlandFullscreenModeIsMaximized(*info.Fullscreen), nil
}
func (hyprlandWindowBackend) Close(args ...int) error {
	if len(args) > 0 {
		return waylandWindowNotSupported("close window by pid/handle")
	}
	dispatchArgs, err := resolveHyprlandDispatchArgs(
		[]string{argKillActive},
		hyprlandLuaCloseActive,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", errWindowOperationFailed, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	if _, err := runWindowCommand(ctx, cmdHyprCtl, dispatchArgs...); err != nil {
		return fmt.Errorf("%w: %w", errWindowOperationFailed, err)
	}
	return nil
}
func (hyprlandWindowBackend) Title(args ...int) (string, error) {
	if len(args) > 0 {
		return "", waylandWindowNotSupported("get window title by pid/handle")
	}
	return getHyprlandActiveWindowTitle()
}

type wlrootsGenericWindowBackend struct{}

func (wlrootsGenericWindowBackend) Name() string { return windowBackendWlroots }

func (wlrootsGenericWindowBackend) Capability() FeatureCapability {
	available := hasCommand(cmdWlrCtl)
	reason := reasonWaylandGlobalUnsupported
	notes := notesWlrootsBackend
	if available {
		reason = "wlroots generic backend can minimize/maximize active window via wlrctl"
		notes = "supports active-window minimize/maximize (state=true only); close/title and pid/handle-specific operations remain unsupported"
	} else {
		notes += "; install wlrctl to enable active-window minimize/maximize operations"
	}
	return FeatureCapability{
		Available: available,
		Fallback:  false,
		Backend:   windowBackendWlroots,
		Reason:    reason,
		Notes:     notes,
	}
}

func (wlrootsGenericWindowBackend) SetActive(win Handle) error {
	_ = win
	return waylandWindowNotSupported("set active window")
}

func (wlrootsGenericWindowBackend) Minimize(pid int, state bool, isPid bool) error {
	if pid > 0 || isPid {
		return waylandWindowNotSupported("minimize window by pid/handle")
	}
	if !state {
		return waylandWindowNotSupported("restore minimized window")
	}
	return runWlrctlActiveWindowAction("minimize window", argMinimize)
}

func (wlrootsGenericWindowBackend) Maximize(pid int, state bool, isPid bool) error {
	if pid > 0 || isPid {
		return waylandWindowNotSupported("maximize window by pid/handle")
	}
	if !state {
		return waylandWindowNotSupported("restore maximized window")
	}
	return runWlrctlActiveWindowAction("maximize window", argMaximize)
}

func (wlrootsGenericWindowBackend) Maximized() (bool, error) {
	return false, waylandWindowNotSupported("query maximized state")
}

func runWlrctlActiveWindowAction(op, action string) error {
	if !hasCommand(cmdWlrCtl) {
		return waylandWindowNotSupported(op + " (wlrctl unavailable)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	if _, err := runWindowCommand(ctx, cmdWlrCtl, argWindow, action, argStateActive); err != nil {
		return fmt.Errorf("%w: %v", errWindowOperationFailed, err)
	}
	return nil
}

func (wlrootsGenericWindowBackend) Close(args ...int) error {
	_ = args
	return waylandWindowNotSupported("close window")
}

func (wlrootsGenericWindowBackend) Title(args ...int) (string, error) {
	_ = args
	return "", waylandWindowNotSupported("get window title")
}

type swayTreeNode struct {
	Name          string         `json:"name"`
	Focused       bool           `json:"focused"`
	Nodes         []swayTreeNode `json:"nodes"`
	FloatingNodes []swayTreeNode `json:"floating_nodes"`
}

func findFocusedSwayTitle(n swayTreeNode) (string, bool) {
	if n.Focused && n.Name != "" {
		return n.Name, true
	}
	for _, c := range n.Nodes {
		if title, ok := findFocusedSwayTitle(c); ok {
			return title, true
		}
	}
	for _, c := range n.FloatingNodes {
		if title, ok := findFocusedSwayTitle(c); ok {
			return title, true
		}
	}
	return "", false
}

func getSwayActiveWindowTitle() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	out, err := runWindowCommand(ctx, cmdSwayMsg, argType, argGetTree, argRawJSON)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errWindowTitleUnavailable, err)
	}
	var root swayTreeNode
	if err := json.Unmarshal(out, &root); err != nil {
		return "", fmt.Errorf("%w: invalid sway tree json: %v", errWindowTitleUnavailable, err)
	}
	title, ok := findFocusedSwayTitle(root)
	if !ok || strings.TrimSpace(title) == "" {
		return "", errWindowTitleUnavailable
	}
	return title, nil
}

type hyprlandActiveWindow struct {
	Title            string `json:"title"`
	Fullscreen       *int   `json:"fullscreen"`
	FullscreenClient *int   `json:"fullscreenClient"`
}

type hyprlandStatus struct {
	ConfigProvider string `json:"configProvider"`
}

func resolveHyprlandDispatchArgs(legacy []string, luaExpression string) ([]string, error) {
	provider, err := getHyprlandConfigProvider()
	if errors.Is(err, errHyprlandStatusUnavailable) {
		// Hyprland before 0.55 has no status request and only accepts the
		// historical dispatcher syntax. Only its exact "unknown request"
		// response reaches this fallback; transport failures remain fail-closed.
		return append([]string{argDispatch}, legacy...), nil
	}
	if err != nil {
		return nil, err
	}
	switch provider {
	case hyprlandConfigProviderLegacy:
		return append([]string{argDispatch}, legacy...), nil
	case hyprlandConfigProviderLua:
		return []string{argDispatch, luaExpression}, nil
	default:
		return nil, fmt.Errorf(
			"unknown hyprland config provider %q",
			provider,
		)
	}
}

func getHyprlandConfigProvider() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	out, err := runWindowCommand(ctx, cmdHyprCtl, argStatus, argJSON)
	if err != nil {
		return "", fmt.Errorf("query hyprland status: %w", err)
	}
	if strings.TrimSpace(string(out)) == hyprlandStatusUnsupported {
		return "", fmt.Errorf(
			"%w: %s",
			errHyprlandStatusUnavailable,
			hyprlandStatusUnsupported,
		)
	}
	var status hyprlandStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("invalid hyprland status json: %w", err)
	}
	provider := strings.ToLower(strings.TrimSpace(status.ConfigProvider))
	if provider == "" {
		return "", errors.New("hyprland status omitted config provider")
	}
	return provider, nil
}

func validHyprlandFullscreenMode(mode int) bool {
	return mode >= hyprlandFullscreenNone && mode <= hyprlandFullscreenMaxAndFull
}

func hyprlandFullscreenModeIsMaximized(mode int) bool {
	return mode == hyprlandFullscreenMaximized || mode == hyprlandFullscreenMaxAndFull
}

func getHyprlandActiveWindow() (hyprlandActiveWindow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	out, err := runWindowCommand(ctx, cmdHyprCtl, argActiveWindow, argJSON)
	if err != nil {
		return hyprlandActiveWindow{}, err
	}
	var info hyprlandActiveWindow
	if err := json.Unmarshal(out, &info); err != nil {
		return hyprlandActiveWindow{}, fmt.Errorf("invalid hyprland json: %w", err)
	}
	return info, nil
}

func getHyprlandActiveWindowTitle() (string, error) {
	info, err := getHyprlandActiveWindow()
	if err != nil {
		return "", fmt.Errorf("%w: %v", errWindowTitleUnavailable, err)
	}
	if strings.TrimSpace(info.Title) == "" {
		return "", errWindowTitleUnavailable
	}
	return info.Title, nil
}

var specificWindowBackends = map[string]func() windowBackend{
	compositorSway:     newSwayWindowBackend,
	compositorHyprland: newHyprlandWindowBackend,
}

func specificWindowBackendForCompositor(compositor string) (windowBackend, bool) {
	factory, ok := specificWindowBackends[compositor]
	if !ok {
		return nil, false
	}
	return factory(), true
}

func resolveWindowBackend() windowBackend {
	if runtime.GOOS != "linux" || DetectDisplayServer() != DisplayServerWayland {
		return nativeWindowBackend{}
	}

	compositor := detectWaylandCompositor()
	if b, ok := specificWindowBackendForCompositor(compositor); ok {
		return b
	}
	if waylandCompositorFamily(compositor) == compositorWlroots {
		return newWlrootsGenericWindowBackend()
	}
	return waylandCoreWindowBackend{compositor: compositor}
}

func waylandCompositorFamily(compositor string) string {
	switch compositor {
	case compositorSway, compositorHyprland, compositorWayfire, compositorRiver,
		compositorLabwc, compositorDwl, compositorGamescope:
		return compositorWlroots
	default:
		return compositor
	}
}
