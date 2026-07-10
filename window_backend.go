package robotgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	envDesktop                     = "XDG_CURRENT_DESKTOP"
	envSessionDesktop              = "XDG_SESSION_DESKTOP"
	envSwaySock                    = "SWAYSOCK"
	envHyprlandSignature           = "HYPRLAND_INSTANCE_SIGNATURE"
	compositorWlroots              = "wlroots"
	compositorMutter               = "mutter"
	compositorKWin                 = "kwin"
	compositorHyprland             = "hyprland"
	compositorSway                 = "sway"
	compositorWayfire              = "wayfire"
	compositorRiver                = "river"
	compositorLabwc                = "labwc"
	compositorDwl                  = "dwl"
	compositorGamescope            = "gamescope"
	compositorUnknown              = "unknown"
	desktopTokenSway               = "sway"
	desktopTokenGNOME              = "gnome"
	desktopTokenKDE                = "kde"
	desktopTokenPlasma             = "plasma"
	desktopTokenWayfire            = "wayfire"
	desktopTokenHyprland           = "hyprland"
	desktopTokenRiver              = "river"
	desktopTokenLabwc              = "labwc"
	desktopTokenDwl                = "dwl"
	desktopTokenGamescope          = "gamescope"
	windowBackendSway              = "sway"
	windowBackendHypr              = "hyprland"
	windowBackendWlroots           = "wlroots-generic"
	windowBackendCore              = "wayland-core"
	reasonWaylandGlobalUnsupported = "global foreign-window operations are not universally available in Wayland core protocols"
	notesWlrootsBackend            = "wlroots generic backend selected; operation support to be implemented via wlroots-compatible paths"
	reasonCompositorSpecific       = "compositor-specific backend selected with partial operation support"
	notesSwayPartialSupport        = "supports active-window title retrieval and close; active minimize/maximize available when wlrctl is present"
	notesHyprPartialSupport        = "supports active-window title retrieval and close; active minimize/maximize available when wlrctl is present"
	cmdSwayMsg                     = "swaymsg"
	cmdHyprCtl                     = "hyprctl"
	cmdWlrCtl                      = "wlrctl"
	argJSON                        = "-j"
	argRawJSON                     = "-r"
	argType                        = "-t"
	argGetTree                     = "get_tree"
	argActiveWindow                = "activewindow"
	argWindow                      = "window"
	argMinimize                    = "minimize"
	argMaximize                    = "maximize"
	argStateActive                 = "state:active"
	argKill                        = "kill"
	argDispatch                    = "dispatch"
	argKillActive                  = "killactive"
	windowCommandTimeout           = 2 * time.Second
)

var (
	errWindowTitleUnavailable = errors.New("window title unavailable from compositor backend")
	errWindowOperationFailed  = errors.New("window operation failed for compositor backend")
	runWindowCommand          = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
)

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

type windowBackend interface {
	Name() string
	Capability() FeatureCapability
	SetActive(win Handle) error
	Minimize(pid int, state bool, isPid bool) error
	Maximize(pid int, state bool, isPid bool) error
	Close(args ...int) error
	Title(args ...int) (string, error)
}

type nativeWindowBackend struct{}

func (nativeWindowBackend) Name() string {
	if runtime.GOOS == "linux" && DetectDisplayServer() == DisplayServerX11 {
		return "x11"
	}
	return "native"
}

func (nativeWindowBackend) Capability() FeatureCapability {
	return FeatureCapability{
		Available: true,
		Fallback:  false,
		Backend:   "native",
		Reason:    "operation supported by current non-wayland-global backend",
		Notes:     "window operations use platform native backend",
	}
}

func (nativeWindowBackend) SetActive(win Handle) error {
	nativeSetActive(win)
	return nil
}

func (nativeWindowBackend) Minimize(pid int, state bool, isPid bool) error {
	nativeMinWindow(pid, state, isPid)
	return nil
}

func (nativeWindowBackend) Maximize(pid int, state bool, isPid bool) error {
	nativeMaxWindow(pid, state, isPid)
	return nil
}

func (nativeWindowBackend) Close(args ...int) error {
	if len(args) <= 0 {
		nativeCloseMainWindow()
		return nil
	}

	pid := args[0]
	isPid := len(args) > 1 || NotPid
	nativeCloseWindowByPid(pid, isPid)
	return nil
}

func (nativeWindowBackend) Title(args ...int) (string, error) {
	if len(args) <= 0 {
		return nativeGetMainTitle(), nil
	}
	if len(args) > 1 {
		return nativeGetInternalTitle(args[0], args[1]), nil
	}
	return nativeGetInternalTitle(args[0], 0), nil
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
	if !state {
		return waylandWindowNotSupported("restore maximized window")
	}
	return runWlrctlActiveWindowAction("maximize window", argMaximize)
}
func (hyprlandWindowBackend) Close(args ...int) error {
	if len(args) > 0 {
		return waylandWindowNotSupported("close window by pid/handle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	if _, err := runWindowCommand(ctx, cmdHyprCtl, argDispatch, argKillActive); err != nil {
		return fmt.Errorf("%w: %v", errWindowOperationFailed, err)
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
	Title string `json:"title"`
}

func getHyprlandActiveWindowTitle() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), windowCommandTimeout)
	defer cancel()
	out, err := runWindowCommand(ctx, cmdHyprCtl, argActiveWindow, argJSON)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errWindowTitleUnavailable, err)
	}
	var info hyprlandActiveWindow
	if err := json.Unmarshal(out, &info); err != nil {
		return "", fmt.Errorf("%w: invalid hyprland json: %v", errWindowTitleUnavailable, err)
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

func detectWaylandCompositor() string {
	if runtime.GOOS != "linux" || DetectDisplayServer() != DisplayServerWayland {
		return ""
	}

	desktop := strings.ToLower(os.Getenv(envDesktop))
	session := strings.ToLower(os.Getenv(envSessionDesktop))

	if isSwaySession(desktop, session) {
		return compositorSway
	}
	if isHyprlandSession(desktop, session) {
		return compositorHyprland
	}
	if containsAny(desktop, desktopTokenWayfire) || containsAny(session, desktopTokenWayfire) {
		return compositorWayfire
	}
	if containsAny(desktop, desktopTokenRiver) || containsAny(session, desktopTokenRiver) {
		return compositorRiver
	}
	if containsAny(desktop, desktopTokenLabwc) || containsAny(session, desktopTokenLabwc) {
		return compositorLabwc
	}
	if containsAny(desktop, desktopTokenDwl) || containsAny(session, desktopTokenDwl) {
		return compositorDwl
	}
	if containsAny(desktop, desktopTokenGamescope) || containsAny(session, desktopTokenGamescope) {
		return compositorGamescope
	}
	if containsAny(desktop, desktopTokenGNOME) || containsAny(session, desktopTokenGNOME) {
		return compositorMutter
	}
	if containsAny(desktop, desktopTokenKDE, desktopTokenPlasma) || containsAny(session, desktopTokenKDE, desktopTokenPlasma) {
		return compositorKWin
	}

	return compositorUnknown
}

func waylandCompositorFamily(compositor string) string {
	switch compositor {
	case compositorSway, compositorHyprland, compositorWayfire, compositorRiver, compositorLabwc, compositorDwl, compositorGamescope:
		return compositorWlroots
	default:
		return compositor
	}
}

func isSwaySession(desktop, session string) bool {
	return os.Getenv(envSwaySock) != "" || containsAny(desktop, desktopTokenSway) || containsAny(session, desktopTokenSway)
}

func isHyprlandSession(desktop, session string) bool {
	return os.Getenv(envHyprlandSignature) != "" || containsAny(desktop, desktopTokenHyprland) || containsAny(session, desktopTokenHyprland)
}
