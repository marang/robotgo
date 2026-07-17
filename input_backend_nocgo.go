//go:build !cgo

package robotgo

import "fmt"

// pureGoKeyEvent is the normalized keyboard contract used by non-CGO
// platform backends. A tap owns the complete press/release transaction; a
// toggle changes only the requested state.
type pureGoKeyEvent struct {
	Key string
	// Modifiers contains the portable, backend-normalized modifiers used by
	// X11 and portal-compatible backends. UserModifiers preserves only the
	// modifiers supplied by the caller so layout-aware platforms can derive
	// character modifiers from their target keyboard layout.
	Modifiers     []string
	UserModifiers []string
	PID           int
	Down          bool
	Tap           bool
}

// pureGoTextEvent describes one UTF-8 text transaction. Delay is applied
// between runes and PID zero targets the active application.
type pureGoTextEvent struct {
	Text  string
	PID   int
	Delay int
}

// pureGoInputBackend keeps platform-specific input below one explicit,
// error-returning boundary. Implementations must serialize composite events so
// modifiers, double clicks, and temporary keymap changes cannot interleave.
type pureGoInputBackend interface {
	Name() string
	KeyboardReady() error
	MouseReady() error
	Key(pureGoKeyEvent) error
	Text(pureGoTextEvent) error
	MoveAbsolute(x, y int, displayID []int) error
	MoveRelative(x, y int) error
	MoveSmooth(x, y int, relative bool, lowDelay, highDelay float64) error
	DragSmooth(x, y int, lowDelay, highDelay float64) error
	Location() (int, int, error)
	Click(button string, double bool) error
	Toggle(button string, down bool) error
	Scroll(x, y int) error
	Close() error
}

var resolvePureGoInputBackend = platformPureGoInputBackend

func withPureGoInputBackend(operation func(pureGoInputBackend) error) (bool, error) {
	backend := resolvePureGoInputBackend()
	if backend == nil {
		if err := closePureGoPlatformInput(); err != nil {
			return true, fmt.Errorf("robotgo: clean up deselected Pure-Go input backend: %w", err)
		}
		return false, nil
	}
	return true, operation(backend)
}

func pureGoInputCapabilities() (keyboard, mouse FeatureCapability) {
	backend := resolvePureGoInputBackend()
	if backend == nil {
		return FeatureCapability{}, FeatureCapability{}
	}
	backendName := backend.Name()
	keyboard = FeatureCapability{
		Available: true,
		Backend:   backendName,
		Reason:    "Pure-Go keyboard backend is selected; runtime access is not probed",
		Notes:     "call KeyboardReady for a live backend check; capability inspection does not open a display connection",
	}
	mouse = FeatureCapability{
		Available: true,
		Backend:   backendName,
		Reason:    "Pure-Go pointer backend is selected; runtime access is not probed",
		Notes:     "call MouseReady for a live backend check; capability inspection does not open a display connection",
	}
	if backendName == featureBackendPureGoX11 {
		keyboard.Notes += "; key taps and text may use server-global scratch keycodes: call CloseMainDisplayE after targets process all prior keyboard input; a separate Pure-Go guardian performs bounded crash cleanup and restores only exact unchanged, unpressed, non-modifier scratch claims"
		mouse.Notes += "; vertical scrolling is supported; horizontal scrolling returns ErrNotSupported because core XTEST button 6/7 state is not safely observable"
		if pureGoX11EnvironmentConflict() {
			reason := envXDGSessionType + " selects Wayland while DISPLAY selects X11"
			keyboard.Available, keyboard.Reason = false, reason
			mouse.Available, mouse.Reason = false, reason
		}
	}
	if backendName == featureBackendPureGoWindows {
		keyboard.Notes += "; SendInput follows the foreground target's keyboard layout and Windows UIPI; CloseMainDisplayE releases RobotGo-owned persistent holds"
		mouse.Notes += "; pointer coordinates use the Windows virtual screen; CloseMainDisplayE releases RobotGo-owned persistent holds"
	}
	if backendName == featureBackendPureGoQuartzInput {
		keyboard.Available = false
		keyboard.Reason = ErrNotSupported.Error()
		keyboard.Notes = "Pure-Go macOS keyboard injection is not implemented yet"
		mouse.Notes = "Accessibility is preflighted without opening a consent dialog; pointer coordinates use the CoreGraphics global display space; CloseMainDisplayE releases RobotGo-owned persistent holds"
		if err := backend.MouseReady(); err != nil {
			mouse.Available = false
			mouse.Reason = err.Error()
		} else {
			mouse.Reason = "Pure-Go Quartz pointer backend is ready"
		}
	}
	return keyboard, mouse
}
