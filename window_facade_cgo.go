//go:build cgo

package robotgo

import (
	"fmt"
	"runtime"
	"time"
)

func waylandWindowNotSupported(op string) error {
	return fmt.Errorf("%w: %s on Wayland", ErrNotSupported, op)
}

func linuxWindowStateNotSupported(op string) error {
	if isWaylandSession() {
		return waylandWindowNotSupported(op)
	}
	return fmt.Errorf("%w: %s on Linux", ErrNotSupported, op)
}

func isWaylandSession() bool {
	return runtime.GOOS == "linux" && DetectDisplayServer() == DisplayServerWayland
}

func alertArgs(args ...string) (string, string) {
	defaultBtn := "Ok"
	cancelBtn := ""
	if len(args) > 0 && args[0] != "" {
		defaultBtn = args[0]
	}
	if len(args) > 1 {
		cancelBtn = args[1]
	}
	return defaultBtn, cancelBtn
}

// IsTopMostE reports whether the current active window is topmost and returns
// an explicit unsupported error on Linux backends without reliable state
// query support.
func IsTopMostE() (bool, error) {
	if runtime.GOOS == "linux" {
		return false, linuxWindowStateNotSupported("query topmost state")
	}
	return nativeIsTopMost(), nil
}

// IsMinimizedE reports whether the current active window is minimized and
// returns an explicit unsupported error on Linux backends without reliable
// state query support.
func IsMinimizedE() (bool, error) {
	if runtime.GOOS == "linux" {
		return false, linuxWindowStateNotSupported("query minimized state")
	}
	return nativeIsMinimized(), nil
}

// IsMaximizedE reports whether the current active window is maximized.
// Hyprland uses its compositor state; Linux backends without a trustworthy
// query return an explicit unsupported error.
func IsMaximizedE() (bool, error) {
	if runtime.GOOS == "linux" {
		return resolveWindowBackend().Maximized()
	}
	return nativeIsMaximized(), nil
}

// SetTopMostE updates topmost state and returns an explicit unsupported error
// on Linux backends without reliable topmost support.
func SetTopMostE(state bool) error {
	if runtime.GOOS == "linux" {
		return linuxWindowStateNotSupported("set topmost state")
	}
	nativeSetTopMost(state)
	return nil
}

// IsTopMost reports whether the current active window is topmost.
func IsTopMost() bool {
	ok, _ := IsTopMostE()
	return ok
}

// IsMinimized reports whether the current active window is minimized.
func IsMinimized() bool {
	ok, _ := IsMinimizedE()
	return ok
}

// IsMaximized reports whether the current active window is maximized.
func IsMaximized() bool {
	ok, _ := IsMaximizedE()
	return ok
}

// SetTopMost updates topmost state for platforms that support it.
func SetTopMost(state bool) {
	_ = SetTopMostE(state)
}

// SetActive set the window active
func SetActive(win Handle) {
	_ = SetActiveE(win)
}

// SetActiveE sets the active window and returns an explicit unsupported error
// for Wayland sessions where global window activation is not available.
func SetActiveE(win Handle) error {
	return resolveWindowBackend().SetActive(win)
}

func nativeGetInternalTitle(pid int, isPid int) string {
	return internalGetTitle(pid, isPid)
}

// GetActive get the active window
func GetActive() Handle {
	handle, _ := GetActiveE()
	return handle
}

// GetActiveE gets the active window or returns an explicit backend error.
// Wayland backends return ErrNotSupported unless the compositor exposes a
// stable foreign-window handle contract.
func GetActiveE() (Handle, error) {
	return resolveWindowBackend().Active()
}

// MinWindow set the window min
func MinWindow(pid int, args ...interface{}) {
	_ = MinWindowE(pid, args...)
}

// MinWindowE sets the window min state and returns an explicit unsupported
// error on Wayland sessions.
func MinWindowE(pid int, args ...interface{}) error {
	state, err := parseWindowStateArguments(args)
	if err != nil {
		return err
	}
	var isPid int
	if len(args) > 1 || currentTreatAsHandle() {
		isPid = 1
	}
	return resolveWindowBackend().Minimize(pid, state, isPid == 1)
}

// MaxWindow set the window max
func MaxWindow(pid int, args ...interface{}) {
	_ = MaxWindowE(pid, args...)
}

// MaxWindowE sets or restores the window max state. Wayland backends without
// trustworthy compositor support return an explicit unsupported error.
func MaxWindowE(pid int, args ...interface{}) error {
	state, err := parseWindowStateArguments(args)
	if err != nil {
		return err
	}
	var isPid int
	if len(args) > 1 || currentTreatAsHandle() {
		isPid = 1
	}
	return resolveWindowBackend().Maximize(pid, state, isPid == 1)
}

// CloseWindow close the window
func CloseWindow(args ...int) {
	_ = CloseWindowE(args...)
}

// CloseWindowE closes the target window and returns an explicit unsupported
// error on Wayland sessions.
func CloseWindowE(args ...int) error {
	return resolveWindowBackend().Close(args...)
}

// CloseWindowKill closes the target window and ensures the owning process
// terminates. If no arguments are provided, it targets the currently selected
// window (same as CloseWindow()). If a PID (or handle when NotPid is set) is
// provided, it targets that window. After issuing a normal close, it waits a
// short time for graceful shutdown and, if the process is still alive, it will
// force-kill it.
//
// Usage:
//
//	CloseWindowKill()           // close current window and kill if needed
//	CloseWindowKill(pid)        // close by pid and kill if needed
//	CloseWindowKill(pid, 1)     // on Windows, treat first arg as handle
func CloseWindowKill(args ...int) error {
	// Determine target pid and whether argument represents pid or handle.
	var (
		pid   int
		isPid int
	)

	if len(args) <= 0 {
		if isWaylandSession() {
			return CloseWindowE()
		}
		// Capture pid of the currently selected window before closing it.
		pid = GetPid()
		if err := CloseWindowE(); err != nil {
			return err
		}
	} else {
		pid = args[0]
		if len(args) > 1 || currentTreatAsHandle() {
			isPid = 1
		}
		// If the argument represents a handle (Windows/X11 path), resolve its PID
		// before closing so we can verify/kill the correct process afterward.
		if isPid == 1 {
			SetHandle(pid)
			pid = GetPid()
		}
		if err := CloseWindowE(args...); err != nil {
			return err
		}
	}

	if pid <= 0 {
		// Nothing more we can do.
		return nil
	}

	// Give the process a short opportunity to exit cleanly.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		exist, _ := PidExists(pid)
		if !exist {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force terminate if still running.
	return Kill(pid)
}

// GetHandById get handle mdata by id
func GetHandById(id int, args ...int) Handle {
	isPid := 1
	if len(args) > 0 {
		isPid = args[0]
	}
	return GetHandByPid(id, isPid)
}

// GetHandByPid get handle mdata by pid
func GetHandByPid(pid int, args ...int) Handle {
	return Handle(GetHandByPidC(pid, args...))
}

// Deprecated: use the GetHandByPid(),
//
// GetHandPid get handle mdata by pid
func GetHandPid(pid int, args ...int) Handle {
	return GetHandByPid(pid, args...)
}

func cgetTitle(pid, isPid int) string {
	unlock := lockNativeX11Display()
	defer unlock()
	return cgetTitleLocked(pid, isPid)
}

// GetTitle get the window title return string
//
// Examples:
//
//	fmt.Println(robotgo.GetTitle())
//
//	ids, _ := robotgo.FindIds()
//	robotgo.GetTitle(ids[0])
func GetTitle(args ...int) string {
	title, _ := GetTitleE(args...)
	return title
}

// GetTitleE gets the window title and returns an explicit unsupported error
// on Wayland sessions.
func GetTitleE(args ...int) (string, error) {
	return resolveWindowBackend().Title(args...)
}

// GetTitleTargetE gets the title of an explicit process or window handle.
// Unlike GetTitleE, isHandle does not inherit the legacy TreatAsHandle setting.
func GetTitleTargetE(target int, isHandle bool) (string, error) {
	if target <= 0 {
		return "", fmt.Errorf(
			"%w: invalid window target %d",
			errWindowTitleUnavailable,
			target,
		)
	}
	if err := nativeX11WindowReady(); err != nil {
		return "", err
	}
	title := strictInternalGetTitle(target, isHandle)
	if title == "" || title == "is_valid failed." {
		return "", errWindowTitleUnavailable
	}
	return title, nil
}

// GetPid get the process id return int32
func GetPid() int {
	pid, _ := GetPidE()
	return pid
}

// GetPidE gets the active window process ID or returns an explicit backend
// error.
func GetPidE() (int, error) {
	return resolveWindowBackend().PID()
}

// internalGetBounds get the window bounds
func internalGetBounds(pid, isPid int) (int, int, int, int) {
	unlock := lockNativeX11Display()
	defer unlock()
	return internalGetBoundsLocked(pid, isPid)
}

// internalGetClient get the window client bounds
func internalGetClient(pid, isPid int) (int, int, int, int) {
	unlock := lockNativeX11Display()
	defer unlock()
	return internalGetClientLocked(pid, isPid)
}

func internalActive(pid, isPid int) bool {
	unlock := lockNativeX11Display()
	defer unlock()
	return internalActiveLocked(pid, isPid)
}

// ActiveName active the window by name
//
// Examples:
//
//	robotgo.ActiveName("chrome")
func ActiveName(name string) error {
	pids, err := FindIds(name)
	if err == nil && len(pids) > 0 {
		return ActivePid(pids[0])
	}

	return err
}
