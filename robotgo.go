// Copyright 2016 The go-vgo Project Developers. See the COPYRIGHT
// file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

/*
Package robotgo Go native cross-platform system automation.

Please make sure Golang, GCC is installed correctly before installing RobotGo;

See Requirements:

	https://github.com/marang/robotgo#requirements

Installation:

With Go module support (Go 1.11+), just import:

	import "github.com/marang/robotgo"

Otherwise, to install the robotgo package, run the command:

	go get -u github.com/marang/robotgo
*/
package robotgo

/*
#cgo darwin CFLAGS: -x objective-c -Wno-deprecated-declarations
#cgo darwin LDFLAGS: -framework Cocoa -framework CoreFoundation -framework IOKit
#cgo darwin LDFLAGS: -framework Carbon -framework OpenGL
//
#if __ENVIRONMENT_MAC_OS_X_VERSION_MIN_REQUIRED__ > 140400
#cgo darwin LDFLAGS: -framework ScreenCaptureKit
#endif

#cgo linux CFLAGS: -I/usr/src
#cgo linux,!wayland LDFLAGS: -L/usr/src -lm -lX11 -lXtst
#cgo linux,wayland LDFLAGS: -L/usr/src -lm
#cgo windows LDFLAGS: -lgdi32 -luser32
//
#include "screen/goScreen.h"
#include "mouse/mouse_c.h"
#ifdef DISPLAY_SERVER_WAYLAND
#include "window/goWindow_wayland_stub.h"
#else
#include "window/goWindow.h"
#endif
*/
import "C"

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	inputportal "github.com/marang/robotgo/input/portal"
	portalpkg "github.com/marang/robotgo/screen/portal"
	"github.com/vcaesar/tt"
)

const (
	// Version get the robotgo version
	Version                  = "v1.00.0.1189, MT. Baker!"
	envWaylandDisplay        = "WAYLAND_DISPLAY"
	envDisplay               = "DISPLAY"
	envCaptureDebug          = "ROBOTGO_CAPTURE_DEBUG"
	envWaylandBackend        = "ROBOTGO_WAYLAND_BACKEND"
	envPortalStubGreen       = "ROBOTGO_PORTAL_STUB_GREEN"
	envForcePortal           = "ROBOTGO_FORCE_PORTAL"
	envDisablePortal         = "ROBOTGO_DISABLE_PORTAL"
	envPath                  = "PATH"
	waylandBackendPortalName = "portal"
	waylandBackendScreenCast = "screencast"
	cmdWaylandInfo           = "wayland-info"
)

// GetVersion get the robotgo version
func GetVersion() string {
	return Version
}

var (
	// MouseSleep set the mouse default millisecond sleep time
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	MouseSleep = 0
	// KeySleep set the key default millisecond sleep time
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	KeySleep = 10

	// DisplayID set the screen display id
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	DisplayID = -1

	// NotPid used the hwnd not pid in windows
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	NotPid bool
	// Scale option the os screen scale
	// Deprecated: use SetRuntimeConfig for runtime changes in concurrent programs.
	Scale bool
)

// DisplayServer identifies the active Linux display server.
type DisplayServer string

const (
	// DisplayServerX11 represents an X11 display server.
	DisplayServerX11 DisplayServer = "x11"
	// DisplayServerWayland represents a Wayland display server.
	DisplayServerWayland DisplayServer = "wayland"
	// DisplayServerUnknown indicates no known display server was detected.
	DisplayServerUnknown DisplayServer = "unknown"
)

// FeatureCapability describes runtime availability for a feature backend.
type FeatureCapability struct {
	Available bool
	Fallback  bool
	Backend   string
	Reason    string
	Notes     string
}

// LinuxCapabilities summarizes runtime backend availability on Linux.
type LinuxCapabilities struct {
	DisplayServer  DisplayServer
	Compositor     string
	WaylandSession bool
	X11Session     bool
	Capture        FeatureCapability
	Bounds         FeatureCapability
	Keyboard       FeatureCapability
	Mouse          FeatureCapability
	RemoteDesktop  FeatureCapability
	Window         FeatureCapability
	Hook           FeatureCapability
	Events         FeatureCapability
}

// DetectDisplayServer inspects the environment and reports the active display server.
// It checks the standard DISPLAY and WAYLAND_DISPLAY variables.
// If neither is present, DisplayServerUnknown is returned.
func DetectDisplayServer() DisplayServer {
	if os.Getenv(envWaylandDisplay) != "" {
		return DisplayServerWayland
	}
	if os.Getenv(envDisplay) != "" {
		return DisplayServerX11
	}
	return DisplayServerUnknown
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

var (
	portalAvailabilityProbe = func() bool {
		if os.Getenv(envDisablePortal) != "" {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		available, err := portalpkg.Available(ctx)
		return err == nil && available
	}
	waylandCaptureAvailabilityProbe = func() bool {
		return int(C.robotgo_wayland_screencopy_ready()) != 0
	}
	remoteDesktopCapabilityProbe = func() (inputportal.Capability, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		return inputportal.Probe(ctx)
	}
	screenCastCapabilityProbe = func() (portalpkg.ScreenCastCapability, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		return portalpkg.ProbeScreenCast(ctx)
	}
)

func screenCastCapabilityNotes() string {
	if os.Getenv(envDisablePortal) != "" {
		return "persistent ScreenCast capture is disabled by " + envDisablePortal
	}
	if !screenCastCaptureCompiled() {
		return "persistent ScreenCast frame capture is not compiled; build with -tags pipewire"
	}
	capability, err := screenCastCapabilityProbe()
	if err != nil {
		return "persistent ScreenCast/PipeWire probe failed: " + err.Error()
	}
	if !capability.PipeWireReady {
		return "ScreenCast portal detected, but the session bus cannot pass the PipeWire file descriptor"
	}
	if capability.Sources == 0 {
		return fmt.Sprintf("ScreenCast interface version=%d advertises no capturable sources", capability.Version)
	}
	return fmt.Sprintf(
		"persistent ScreenCast/PipeWire available (interface version=%d source-mask=%d cursor-mask=%d)",
		capability.Version, capability.Sources, capability.CursorModes,
	)
}

func probeRemoteDesktopCapability() FeatureCapability {
	capability, err := remoteDesktopCapabilityProbe()
	if err != nil && capability.ScreenCastIssue == "" {
		return FeatureCapability{
			Available: false,
			Backend:   "portal-remote-desktop",
			Reason:    err.Error(),
			Notes:     "install xdg-desktop-portal with a backend that implements RemoteDesktop",
		}
	}
	available := capability.AvailableDevices != 0
	reason := "RemoteDesktop portal advertises input devices"
	if !available {
		reason = "RemoteDesktop portal advertises no input devices"
	}
	notes := fmt.Sprintf(
		"interface version=%d device-mask=%d; screencast version=%d source-mask=%d cursor-mask=%d; explicit consent session available through input/portal",
		capability.Version,
		capability.AvailableDevices,
		capability.ScreenCastVersion,
		capability.AvailableSources,
		capability.AvailableCursorModes,
	)
	if capability.ScreenCastIssue != "" {
		notes += "; ScreenCast capability degraded: " + capability.ScreenCastIssue
	}
	return FeatureCapability{
		Available: available,
		Fallback:  false,
		Backend:   "portal-remote-desktop",
		Reason:    reason,
		Notes:     notes,
	}
}

func nativeWaylandProtocolVersions() nativeWaylandProtocolInfo {
	if selectedDisplayServer() != DisplayServerWayland {
		return nativeWaylandProtocolInfo{}
	}
	info := nativeWaylandProtocolInfo{
		Screencopy: uint32(C.robotgo_wayland_screencopy_version()),
	}
	info.VirtualKeyboard = nativeWaylandKeyboardProtocolVersion()
	unlockMouse := lockLinuxMouse()
	info.VirtualPointer = uint32(C.robotgo_wayland_mouse_protocol_version())
	unlockMouse()
	return info
}

// GetLinuxCapabilities reports runtime feature availability for Linux sessions.
// On non-Linux platforms it returns a zero-value capability set.
func GetLinuxCapabilities() LinuxCapabilities {
	if runtime.GOOS != "linux" {
		return LinuxCapabilities{}
	}

	ds := selectedDisplayServer()
	c := LinuxCapabilities{
		DisplayServer:  ds,
		Compositor:     "",
		WaylandSession: ds == DisplayServerWayland,
		X11Session:     ds == DisplayServerX11,
	}

	switch ds {
	case DisplayServerWayland:
		c.Compositor = detectWaylandCompositor()
		c.RemoteDesktop = probeRemoteDesktopCapability()
		nativeCapture := waylandCaptureAvailabilityProbe()
		portalAvailable := portalAvailabilityProbe()
		persistentCapture := ScreenCastCaptureReady() == nil
		switch {
		case persistentCapture:
			c.Capture = FeatureCapability{
				Available: true,
				Fallback:  nativeCapture || portalAvailable,
				Backend:   "portal-screencast+pipewire",
				Reason:    "an active ScreenCast session provides reusable PipeWire frames",
				Notes:     screenCastCapabilityNotes(),
			}
		case nativeCapture:
			c.Capture = FeatureCapability{
				Available: true,
				Fallback:  portalAvailable,
				Backend:   "wayland+screencopy",
				Reason:    "screencopy manager and compatible buffer backend detected",
				Notes:     "native screencopy path (dmabuf/wl_shm)",
			}
			if portalAvailable {
				c.Capture.Notes += "; desktop portal fallback detected"
				if screenCastCaptureCompiled() {
					c.Capture.Notes += "; " + screenCastCapabilityNotes()
				}
			}
		case portalAvailable:
			c.Capture = FeatureCapability{
				Available: true,
				Fallback:  false,
				Backend:   "portal",
				Reason:    "native screencopy unavailable; desktop portal service detected",
				Notes:     "capture requires portal approval and may prompt the user; " + screenCastCapabilityNotes(),
			}
		default:
			c.Capture = FeatureCapability{
				Available: false,
				Backend:   "",
				Reason:    "no native screencopy protocol or desktop portal service detected",
				Notes:     "build with -tags wayland for native wlroots capture or install a desktop portal",
			}
		}

		unlockDisplay := lockNativeX11Display()
		size := C.getMainDisplaySize()
		unlockDisplay()
		if int(size.w) > 0 && int(size.h) > 0 {
			c.Bounds = FeatureCapability{
				Available: true,
				Fallback:  false,
				Backend:   "wayland-native",
				Reason:    "native wayland bounds path reports non-zero dimensions",
				Notes:     "native wayland bounds path available",
			}
		} else if rect, ok := waylandScreenBoundsFallback(); ok {
			c.Bounds = FeatureCapability{
				Available: true,
				Fallback:  true,
				Backend:   cmdWaylandInfo,
				Reason:    "native wayland bounds unavailable; wayland-info fallback returned valid bounds",
				Notes:     fmt.Sprintf("wayland-info fallback available with bounds %dx%d at (%d,%d)", rect.W, rect.H, rect.X, rect.Y),
			}
		} else if hasCommand(cmdWaylandInfo) {
			c.Bounds = FeatureCapability{
				Available: false,
				Fallback:  false,
				Backend:   cmdWaylandInfo,
				Reason:    "native wayland bounds unavailable and wayland-info returned no valid bounds",
				Notes:     "wayland-info command detected but fallback probe did not produce non-zero bounds",
			}
		} else {
			c.Bounds = FeatureCapability{
				Available: false,
				Fallback:  false,
				Backend:   "",
				Reason:    "native wayland bounds unavailable and wayland-info command missing",
				Notes:     "no native bounds and no wayland-info command detected",
			}
		}

		if err := nativeWaylandKeyboardReady(); err == nil {
			c.Keyboard = FeatureCapability{
				Available: true,
				Fallback:  false,
				Backend:   "wayland-virtual-keyboard",
				Reason:    "zwp_virtual_keyboard_manager_v1 and a keyboard seat are available",
				Notes:     "keyboard injection targets the focused Wayland surface",
			}
		} else if portalErr := RemoteDesktopInputReady(RemoteDesktopKeyboard); portalErr == nil {
			c.Keyboard = FeatureCapability{
				Available: true,
				Fallback:  true,
				Backend:   "portal-remote-desktop",
				Reason:    "active RemoteDesktop portal session grants keyboard input",
				Notes:     "consent-aware portal keyboard session is active",
			}
		} else {
			c.Keyboard = FeatureCapability{
				Available: false,
				Fallback:  c.RemoteDesktop.Available,
				Backend:   "wayland-virtual-keyboard",
				Reason:    err.Error(),
				Notes:     "use native zwp_virtual_keyboard_manager_v1 or call StartRemoteDesktopInput for a consent-aware portal session",
			}
		}
		if err := nativeWaylandMouseReady(); err == nil {
			c.Mouse = FeatureCapability{
				Available: true,
				Fallback:  false,
				Backend:   "wayland-virtual-pointer",
				Reason:    "zwlr_virtual_pointer_v1 is available",
				Notes:     "mouse injection available; Wayland does not expose the real global cursor position",
			}
		} else if portalErr := RemoteDesktopInputReady(RemoteDesktopPointer); portalErr == nil {
			c.Mouse = FeatureCapability{
				Available: true,
				Fallback:  true,
				Backend:   "portal-remote-desktop",
				Reason:    "active RemoteDesktop portal session grants pointer input",
				Notes:     "relative motion, button, and scroll fallback is active; global position remains unavailable",
			}
		} else {
			c.Mouse = FeatureCapability{
				Available: false,
				Fallback:  c.RemoteDesktop.Available,
				Backend:   "wayland-virtual-pointer",
				Reason:    err.Error(),
				Notes:     "use native zwlr_virtual_pointer_v1 or call StartRemoteDesktopInput for a consent-aware portal session",
			}
		}
		c.Window = resolveWindowBackend().Capability()
		if c.Window.Backend == "native" {
			c.Window.Backend = "wayland-core"
		}
		c.Hook = FeatureCapability{
			Available: false,
			Fallback:  false,
			Backend:   "wayland",
			Reason:    "global hooks are restricted by Wayland compositor policy",
			Notes:     "hook/event APIs must report unsupported unless a compositor-specific protocol is implemented",
		}
		c.Events = c.Hook

	case DisplayServerX11:
		displayErr, inputErr := nativeX11CapabilityErrors()
		if capabilityProbeSucceeded(displayErr) {
			c.Capture = FeatureCapability{Available: true, Backend: "x11", Reason: "X11 display connection is available", Notes: "native X11 capture path"}
			c.Bounds = FeatureCapability{Available: true, Backend: "x11", Reason: "X11 display connection is available", Notes: "native X11 bounds path"}
		} else {
			c.Capture = FeatureCapability{Available: false, Backend: "x11", Reason: displayErr.Error(), Notes: "check DISPLAY and X11 server access"}
			c.Bounds = c.Capture
		}
		if capabilityProbeSucceeded(inputErr) {
			c.Keyboard = FeatureCapability{Available: true, Backend: "x11", Reason: "XTEST 2.2 or newer is available", Notes: "native X11 keyboard path"}
			c.Mouse = FeatureCapability{Available: true, Backend: "x11", Reason: "XTEST 2.2 or newer is available", Notes: "native X11 mouse path"}
		} else {
			c.Keyboard = FeatureCapability{Available: false, Backend: "x11", Reason: inputErr.Error(), Notes: "enable XTEST 2.2 or newer on the selected X11 server"}
			c.Mouse = c.Keyboard
		}
		if capabilityProbeSucceeded(displayErr) && nativeX11BackendCompiled() {
			c.Window = FeatureCapability{Available: true, Backend: "x11", Reason: "X11 display connection is available", Notes: "native X11 activation, title, and close path"}
		} else {
			c.Window = FeatureCapability{Available: false, Backend: "x11", Reason: displayErr.Error(), Notes: "build the native X11 backend and verify the configured X11 display"}
		}
		if capabilityProbeSucceeded(displayErr) && nativeX11BackendCompiled() {
			c.Hook = FeatureCapability{Available: true, Backend: "x11", Reason: "X11 display connection is available", Notes: "native X11 hook/event path"}
		} else {
			c.Hook = FeatureCapability{Available: false, Backend: "x11", Reason: displayErr.Error(), Notes: "build the native X11 backend and verify the configured X11 display"}
		}
		c.Events = c.Hook

	default:
		c.Capture = FeatureCapability{Available: false, Backend: "", Reason: "no display server detected", Notes: "no detected display server"}
		c.Bounds = FeatureCapability{Available: false, Backend: "", Reason: "no display server detected", Notes: "no detected display server"}
		c.Keyboard = FeatureCapability{Available: false, Backend: "", Reason: "no display server detected", Notes: "no detected display server"}
		c.Mouse = FeatureCapability{Available: false, Backend: "", Reason: "no display server detected", Notes: "no detected display server"}
		c.Window = FeatureCapability{Available: false, Backend: "", Reason: "no display server detected", Notes: "no detected display server"}
		c.Hook = FeatureCapability{Available: false, Backend: "", Reason: "no display server detected", Notes: "no detected display server"}
		c.Events = c.Hook
	}

	return c
}

func capabilityProbeSucceeded(err error) bool {
	return err == nil
}

// CaptureBackend reports which backend handled the most recent screen capture.
type CaptureBackend string

const (
	BackendNone       CaptureBackend = ""
	BackendScreencopy CaptureBackend = "screencopy"
	BackendPortal     CaptureBackend = "portal"
	BackendScreenCast CaptureBackend = "screencast"
	BackendX11        CaptureBackend = "x11"
	BackendPureGo     CaptureBackend = "pure-go"
)

var (
	captureStateMu sync.RWMutex
	lastBackend    CaptureBackend
	waylandBackend = WaylandBackendAuto
)

// LastBackend returns the backend used for the last CaptureScreen call.
func LastBackend() CaptureBackend {
	captureStateMu.RLock()
	defer captureStateMu.RUnlock()
	return lastBackend
}

func setLastBackend(backend CaptureBackend) {
	captureStateMu.Lock()
	lastBackend = backend
	captureStateMu.Unlock()
}

// WaylandBackend selects which Wayland backend to use at runtime.
type WaylandBackend int

const (
	WaylandBackendAuto   WaylandBackend = -1
	WaylandBackendDmabuf WaylandBackend = 0
	WaylandBackendWlShm  WaylandBackend = 1
)

// SetWaylandBackend allows tests and callers to force a specific Wayland
// capture backend.
func SetWaylandBackend(b WaylandBackend) {
	captureStateMu.Lock()
	waylandBackend = b
	captureStateMu.Unlock()
}

func selectedWaylandBackend() WaylandBackend {
	captureStateMu.RLock()
	defer captureStateMu.RUnlock()
	return waylandBackend
}

var (
	ErrWaylandDisplay   = errors.New("wayland connect failed")
	ErrNoScreencopy     = errors.New("screencopy manager not available")
	ErrNoOutputs        = errors.New("no outputs")
	ErrDmabufDevice     = errors.New("screencopy dmabuf device unsupported")
	ErrDmabufModifiers  = errors.New("screencopy dmabuf modifiers unsupported")
	ErrDmabufImport     = errors.New("screencopy dmabuf import failed")
	ErrDmabufMap        = errors.New("screencopy dmabuf map failed")
	ErrWaylandFailed    = errors.New("wayland capture failed")
	ErrPortalFailed     = errors.New("portal capture failed")
	ErrNotSupported     = errors.New("operation not supported on current platform/backend")
	ErrPermissionDenied = errors.New("permission denied by desktop security policy")
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

var (
	reWaylandOutputID  = regexp.MustCompile(`name:\s*([0-9]+)\s*$`)
	rePosXY            = regexp.MustCompile(`x:\s*(-?[0-9]+),\s*y:\s*(-?[0-9]+)`)
	reLogicalXY        = regexp.MustCompile(`logical_x:\s*(-?[0-9]+),\s*logical_y:\s*(-?[0-9]+)`)
	reLogicalWH        = regexp.MustCompile(`logical_width:\s*([0-9]+),\s*logical_height:\s*([0-9]+)`)
	reModeWH           = regexp.MustCompile(`width:\s*([0-9]+)\s*px,\s*height:\s*([0-9]+)\s*px`)
	reXDGOutputID      = regexp.MustCompile(`output:\s*([0-9]+)\s*$`)
	reWaylandScale     = regexp.MustCompile(`scale:\s*([0-9]+)`)
	reWaylandTransform = regexp.MustCompile(
		`transform:\s*([[:alnum:]_-]+)`,
	)
)

type waylandWLModeState struct {
	w, h int
}

var (
	waylandBoundsProbeMu sync.Mutex
	waylandBoundsMu      sync.Mutex
	waylandBoundsCached  Rect
	waylandBoundsValid   bool
	waylandBoundsProbed  bool
	waylandBoundsAt      time.Time
	waylandBoundsNow     = time.Now
)

const (
	waylandBoundsSuccessTTL = 2 * time.Second
	waylandBoundsFailureTTL = 250 * time.Millisecond
)

// InvalidateScreenBoundsCache forces the next Wayland fallback bounds query to
// re-read compositor output geometry.
func InvalidateScreenBoundsCache() {
	waylandBoundsProbeMu.Lock()
	defer waylandBoundsProbeMu.Unlock()
	waylandBoundsMu.Lock()
	waylandBoundsCached = Rect{}
	waylandBoundsValid = false
	waylandBoundsProbed = false
	waylandBoundsAt = time.Time{}
	waylandBoundsMu.Unlock()
}

func waylandScreenBoundsFallback() (Rect, bool) {
	if !isWaylandSession() {
		return Rect{}, false
	}
	waylandBoundsProbeMu.Lock()
	defer waylandBoundsProbeMu.Unlock()

	waylandBoundsMu.Lock()
	if waylandBoundsProbed {
		ttl := waylandBoundsFailureTTL
		if waylandBoundsValid {
			ttl = waylandBoundsSuccessTTL
		}
		if waylandBoundsNow().Sub(waylandBoundsAt) < ttl {
			r, ok := waylandBoundsCached, waylandBoundsValid
			waylandBoundsMu.Unlock()
			return r, ok
		}
	}
	waylandBoundsMu.Unlock()

	path, err := exec.LookPath(cmdWaylandInfo)
	if err != nil {
		waylandBoundsMu.Lock()
		waylandBoundsProbed = true
		waylandBoundsValid = false
		waylandBoundsAt = waylandBoundsNow()
		waylandBoundsMu.Unlock()
		return Rect{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, path).Output()
	if err != nil {
		waylandBoundsMu.Lock()
		waylandBoundsProbed = true
		waylandBoundsValid = false
		waylandBoundsAt = waylandBoundsNow()
		waylandBoundsMu.Unlock()
		return Rect{}, false
	}

	rect, ok := parseWaylandInfoBounds(string(out))
	waylandBoundsMu.Lock()
	waylandBoundsProbed = true
	waylandBoundsValid = ok
	waylandBoundsAt = waylandBoundsNow()
	if ok {
		waylandBoundsCached = rect
	}
	waylandBoundsMu.Unlock()

	return rect, ok
}

func parseWaylandInfoBounds(raw string) (Rect, bool) {
	type xdgState struct {
		outputID      int
		hasID         bool
		logicalX      int
		logicalY      int
		logicalW      int
		logicalH      int
		hasLogicalPos bool
		hasLogicalWH  bool
	}
	type wlState struct {
		outputID      int
		hasID         bool
		x             int
		y             int
		hasPos        bool
		currentMode   waylandWLModeState
		hasCurrent    bool
		firstMode     waylandWLModeState
		hasFirst      bool
		pendingMode   waylandWLModeState
		hasPending    bool
		expectModeVal bool
		scale         int
		transform     int
	}

	xdgBounds := make(map[int]waylandOutputBounds)
	var logicalBounds []waylandOutputBounds
	var wlBounds []waylandOutputBounds

	inXDG := false
	inWL := false
	var xs xdgState
	var ws wlState

	commitXDG := func() {
		if !xs.hasLogicalWH {
			return
		}
		x := 0
		y := 0
		if xs.hasLogicalPos {
			x = xs.logicalX
			y = xs.logicalY
		}
		bounds := waylandOutputBounds{
			x: x,
			y: y,
			w: xs.logicalW,
			h: xs.logicalH,
		}
		logicalBounds = append(logicalBounds, bounds)
		if xs.hasID {
			xdgBounds[xs.outputID] = bounds
		}
	}

	commitWL := func() {
		if !ws.hasPos {
			return
		}
		w := 0
		h := 0
		if ws.hasCurrent {
			w, h = ws.currentMode.w, ws.currentMode.h
		} else if ws.hasFirst {
			w, h = ws.firstMode.w, ws.firstMode.h
		}
		if w <= 0 || h <= 0 {
			return
		}
		if ws.hasID {
			if b, ok := xdgBounds[ws.outputID]; ok && b.w > 0 && b.h > 0 {
				wlBounds = append(wlBounds, b)
				return
			}
		}
		scale := ws.scale
		if scale <= 0 {
			scale = 1
		}
		w /= scale
		h /= scale
		if waylandTransformRotatesDimensions(ws.transform) {
			w, h = h, w
		}
		if w <= 0 || h <= 0 {
			return
		}
		wlBounds = append(wlBounds, waylandOutputBounds{
			x: ws.x,
			y: ws.y,
			w: w,
			h: h,
		})
	}

	newWLState := func() wlState {
		return wlState{scale: 1, transform: waylandTransformNormal}
	}

	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "interface: '") {
			if inXDG {
				commitXDG()
				inXDG = false
				xs = xdgState{}
			}
			if inWL {
				commitWL()
				inWL = false
				ws = newWLState()
			}
			if strings.HasPrefix(line, "interface: 'wl_output'") {
				inWL = true
				ws = newWLState()
				if m := reWaylandOutputID.FindStringSubmatch(line); len(m) == 2 {
					if id, err := strconv.Atoi(m[1]); err == nil {
						ws.outputID = id
						ws.hasID = true
					}
				}
			}
			continue
		}

		if line == "xdg_output_v1" {
			if inXDG {
				commitXDG()
			}
			inXDG = true
			xs = xdgState{}
			continue
		}

		if inXDG {
			if m := reXDGOutputID.FindStringSubmatch(line); len(m) == 2 {
				if id, err := strconv.Atoi(m[1]); err == nil {
					xs.outputID = id
					xs.hasID = true
				}
				continue
			}
			if m := reLogicalXY.FindStringSubmatch(line); len(m) == 3 {
				x, errX := strconv.Atoi(m[1])
				y, errY := strconv.Atoi(m[2])
				if errX == nil && errY == nil {
					xs.logicalX = x
					xs.logicalY = y
					xs.hasLogicalPos = true
				}
				continue
			}
			if m := reLogicalWH.FindStringSubmatch(line); len(m) == 3 {
				w, errW := strconv.Atoi(m[1])
				h, errH := strconv.Atoi(m[2])
				if errW == nil && errH == nil {
					xs.logicalW = w
					xs.logicalH = h
					xs.hasLogicalWH = true
				}
				continue
			}
		}

		if inWL {
			if m := reWaylandScale.FindStringSubmatch(line); len(m) == 2 {
				if scale, err := strconv.Atoi(m[1]); err == nil && scale > 0 {
					ws.scale = scale
				}
			}
			if m := reWaylandTransform.FindStringSubmatch(line); len(m) == 2 {
				if transform, ok := parseWaylandTransform(m[1]); ok {
					ws.transform = transform
				}
			}
			if m := rePosXY.FindStringSubmatch(line); len(m) == 3 {
				x, errX := strconv.Atoi(m[1])
				y, errY := strconv.Atoi(m[2])
				if errX == nil && errY == nil {
					ws.x = x
					ws.y = y
					ws.hasPos = true
				}
				continue
			}
			if line == "mode:" {
				ws.expectModeVal = true
				ws.hasPending = false
				continue
			}
			if ws.expectModeVal {
				if m := reModeWH.FindStringSubmatch(line); len(m) == 3 {
					w, errW := strconv.Atoi(m[1])
					h, errH := strconv.Atoi(m[2])
					if errW == nil && errH == nil {
						ws.pendingMode = waylandWLModeState{w: w, h: h}
						ws.hasPending = true
						if !ws.hasFirst {
							ws.firstMode = ws.pendingMode
							ws.hasFirst = true
						}
					}
					continue
				}
				if strings.HasPrefix(line, "flags:") {
					if ws.hasPending && strings.Contains(line, "current") {
						ws.currentMode = ws.pendingMode
						ws.hasCurrent = true
					}
					ws.expectModeVal = false
					ws.hasPending = false
					continue
				}
			}
		}
	}
	if sc.Err() != nil {
		return Rect{}, false
	}

	if inXDG {
		commitXDG()
	}
	if inWL {
		commitWL()
	}

	if len(logicalBounds) > 0 &&
		(len(wlBounds) == 0 || len(logicalBounds) == len(wlBounds)) {
		return aggregateWaylandOutputBounds(logicalBounds)
	}
	return aggregateWaylandOutputBounds(wlBounds)
}

func captureDebugf(format string, args ...interface{}) {
	if os.Getenv(envCaptureDebug) != "" {
		log.Printf("robotgo capture: "+format, args...)
	}
}

func waylandBackendFromEnv() (WaylandBackend, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envWaylandBackend))) {
	case "":
		return WaylandBackendAuto, false
	case "auto":
		return WaylandBackendAuto, true
	case "dmabuf":
		return WaylandBackendDmabuf, true
	case "wl_shm", "wlshm":
		return WaylandBackendWlShm, true
	default:
		return WaylandBackendAuto, false
	}
}

func waylandErr(code C.int32_t) error {
	switch code {
	case C.ScreengrabErrDisplay:
		return ErrWaylandDisplay
	case C.ScreengrabErrNoManager:
		return ErrNoScreencopy
	case C.ScreengrabErrNoOutputs:
		return ErrNoOutputs
	case C.ScreengrabErrDmabufDevice:
		return ErrDmabufDevice
	case C.ScreengrabErrDmabufModifiers:
		return ErrDmabufModifiers
	case C.ScreengrabErrDmabufImport:
		return ErrDmabufImport
	case C.ScreengrabErrDmabufMap:
		return ErrDmabufMap
	default:
		return ErrWaylandFailed
	}
}

func portalStubEnabled() bool {
	return os.Getenv(envPortalStubGreen) != ""
}

func captureViaPortalScreenshot(x, y, w, h C.int32_t) (CBitmap, error) {
	if os.Getenv(envDisablePortal) != "" {
		return nil, ErrPortalFailed
	}
	img, err := portalpkg.CaptureRegionImage(context.Background(), int(x), int(y), int(w), int(h))
	if err != nil {
		captureDebugf("portal screenshot failed: %v", err)
		return nil, err
	}
	if img == nil {
		return nil, errors.New("portal screenshot returned nil image")
	}
	cb := ImgToCBitmap(img)
	if cb == nil {
		return nil, errors.New("portal screenshot conversion failed")
	}
	setLastBackend(BackendPortal)
	return cb, nil
}

func captureViaPersistentScreenCast(x, y, w, h C.int32_t) (CBitmap, error) {
	img, err := captureViaScreenCast(context.Background(), int(x), int(y), int(w), int(h))
	if err != nil {
		captureDebugf("persistent ScreenCast failed: %v", err)
		return nil, err
	}
	bitmap := ImgToCBitmap(img)
	if bitmap == nil {
		return nil, errors.New("ScreenCast frame conversion failed")
	}
	setLastBackend(BackendScreenCast)
	return bitmap, nil
}

func captureViaPortalStub(x, y, w, h C.int32_t, displayId int, isPid int) (CBitmap, error) {
	var perr C.int32_t
	pbit := C.capture_screen_portal(x, y, w, h, C.int32_t(displayId), C.int8_t(isPid), &perr)
	if pbit == nil {
		return nil, fmt.Errorf("portal capture failed: %d", int(perr))
	}
	setLastBackend(BackendPortal)
	return CBitmap(pbit), nil
}

type (
	// Map a map[string]interface{}
	Map map[string]interface{}
	// CHex define CHex as c rgb Hex type (C.MMRGBHex)
	CHex C.MMRGBHex
	// CBitmap define CBitmap as C.MMBitmapRef type
	CBitmap = C.MMBitmapRef
	// Handle define window Handle as C.MData type
	Handle C.MData
)

// Bitmap define the go Bitmap struct
//
// The common type conversion of bitmap:
//
//	https://github.com/marang/robotgo/blob/master/docs/keys.md#type-conversion
type Bitmap struct {
	ImgBuf        *uint8
	Width, Height int

	Bytewidth     int
	BitsPixel     uint8
	BytesPerPixel uint8

	buf     []uint8 // keep Go memory alive for ImgBuf
	trusted bool    // ImgBuf was produced by a RobotGo-owned native bitmap
}

// Point is point struct
type Point struct {
	X int
	Y int
}

// Size is size structure
type Size struct {
	W, H int
}

// Rect is rect structure
type Rect struct {
	Point
	Size
}

// Try handler(err)
func Try(fun func(), handler func(interface{})) {
	defer func() {
		if err := recover(); err != nil {
			handler(err)
		}
	}()
	fun()
}

// MilliSleep sleep tm milli second
func MilliSleep(tm int) {
	time.Sleep(time.Duration(tm) * time.Millisecond)
}

// Sleep time.Sleep tm second
func Sleep(tm int) {
	time.Sleep(time.Duration(tm) * time.Second)
}

// Deprecated: use the MilliSleep(),
//
// MicroSleep time C.microsleep(tm)
func MicroSleep(tm float64) {
	C.microsleep(C.double(tm))
}

// GoString trans C.char to string
func GoString(char *C.char) string {
	return C.GoString(char)
}

/*
      _______.  ______ .______       _______  _______ .__   __.
    /       | /      ||   _  \     |   ____||   ____||  \ |  |
   |   (----`|  ,----'|  |_)  |    |  |__   |  |__   |   \|  |
    \   \    |  |     |      /     |   __|  |   __|  |  . `  |
.----)   |   |  `----.|  |\  \----.|  |____ |  |____ |  |\   |
|_______/     \______|| _| `._____||_______||_______||__| \__|
*/

// ToMMRGBHex trans CHex to C.MMRGBHex
func ToMMRGBHex(hex CHex) C.MMRGBHex {
	return C.MMRGBHex(hex)
}

// UintToHex trans uint32 to robotgo.CHex
func UintToHex(u uint32) CHex {
	hex := U32ToHex(C.uint32_t(u))
	return CHex(hex)
}

// U32ToHex trans C.uint32_t to C.MMRGBHex
func U32ToHex(hex C.uint32_t) C.MMRGBHex {
	return C.MMRGBHex(hex)
}

// U8ToHex trans *C.uint8_t to C.MMRGBHex
func U8ToHex(hex *C.uint8_t) C.MMRGBHex {
	return C.MMRGBHex(*hex)
}

// PadHex trans C.MMRGBHex to string
func PadHex(hex C.MMRGBHex) string {
	color := C.pad_hex(hex)
	gcolor := C.GoString(color)
	C.free(unsafe.Pointer(color))

	return gcolor
}

// PadHexs trans CHex to string
func PadHexs(hex CHex) string {
	return PadHex(C.MMRGBHex(hex))
}

// HexToRgb trans hex to rgb
func HexToRgb(hex uint32) *C.uint8_t {
	return C.color_hex_to_rgb(C.uint32_t(hex))
}

// RgbToHex trans rgb to hex
func RgbToHex(r, g, b uint8) C.uint32_t {
	return C.color_rgb_to_hex(C.uint8_t(r), C.uint8_t(g), C.uint8_t(b))
}

// GetPxColor returns the pixel color at (x,y). On Linux it captures a 1x1
// region through the selected capture backend so invalid coordinates and
// backend failures are returned as errors instead of looking like black.
func GetPxColor(x, y int, displayId ...int) (C.MMRGBHex, error) {
	display := displayIdx(displayId...)

	if runtime.GOOS == "linux" {
		bit, err := CaptureScreen(x, y, 1, 1, display)
		if err != nil {
			return 0, err
		}
		defer FreeBitmap(bit)
		return C.mmrgb_hex_at(C.MMBitmapRef(bit), 0, 0), nil
	}

	color := C.get_px_color(C.int32_t(x), C.int32_t(y), C.int32_t(display))
	return color, nil
}

// GetPixelColor returns the pixel color as a hex string.
func GetPixelColor(x, y int, displayId ...int) (string, error) {
	c, err := GetPxColor(x, y, displayId...)
	if err != nil {
		return "", err
	}
	return PadHex(c), nil
}

// GetLocationColor gets the color of the current mouse location.
func GetLocationColor(displayId ...int) (string, error) {
	x, y, err := LocationE()
	if err != nil {
		return "", err
	}
	return GetPixelColor(x, y, displayId...)
}

// IsMain is main display
func IsMain(displayId int) bool {
	return displayId == GetMainId()
}

func displayIdx(id ...int) int {
	display := -1
	if configured := currentDisplayID(); configured != -1 {
		display = configured
	}
	if len(id) > 0 {
		display = id[0]
	}

	return display
}

// GetHWNDByPid get the hwnd by pid
func GetHWNDByPid(pid int) int {
	return int(C.get_hwnd_by_pid(C.uintptr(pid)))
}

// SysScale get the sys scale
func SysScale(displayId ...int) float64 {
	display := displayIdx(displayId...)
	unlock := lockNativeX11Display()
	defer unlock()
	return sysScaleLocked(display)
}

func sysScaleLocked(display int) float64 {
	return float64(C.sys_scale(C.int32_t(display)))
}

func scaleFLocked(displayId ...int) float64 {
	f := sysScaleLocked(displayIdx(displayId...))
	if f == 0.0 {
		return 1.0
	}
	return f
}

// Scaled get the screen scaled return scale size
func Scaled(x int, displayId ...int) int {
	f := ScaleF(displayId...)
	return Scaled0(x, f)
}

// Scaled0 return int(x * f)
func Scaled0(x int, f float64) int {
	return int(float64(x) * f)
}

// Scaled1 return int(x / f)
func Scaled1(x int, f float64) int {
	return int(float64(x) / f)
}

// GetScreenSize get the screen size
func GetScreenSize() (int, int) {
	unlock := lockNativeX11Display()
	size := C.getMainDisplaySize()
	unlock()
	w := int(size.w)
	h := int(size.h)
	if w > 0 && h > 0 {
		return w, h
	}
	if rect, ok := waylandScreenBoundsFallback(); ok {
		return rect.W, rect.H
	}
	return w, h
}

// GetScreenRect get the screen rect (x, y, w, h)
func GetScreenRect(displayId ...int) Rect {
	display := -1
	if len(displayId) > 0 {
		display = displayId[0]
	}

	unlock := lockNativeX11Display()
	rect := getScreenRectLocked(display, false)
	unlock()
	if display < 0 && (rect.W <= 0 || rect.H <= 0) && isWaylandSession() {
		if wlRect, ok := waylandScreenBoundsFallback(); ok {
			return wlRect
		}
	}
	return rect
}

// getScreenRectLocked returns the native screen rectangle while the caller
// holds the native X11 display lease. The lease is a no-op on platforms that
// do not compile the native X11 backend.
func getScreenRectLocked(display int, waylandSession bool) Rect {
	rect := C.getScreenRect(C.int32_t(display))
	x, y, w, h := int(rect.origin.x), int(rect.origin.y),
		int(rect.size.w), int(rect.size.h)
	if (w <= 0 || h <= 0) && waylandSession {
		if wlRect, ok := waylandScreenBoundsFallback(); ok {
			x, y, w, h = wlRect.X, wlRect.Y, wlRect.W, wlRect.H
		}
	}

	if runtime.GOOS == "windows" {
		// f := ScaleF(displayId...)
		f := ScaleF()
		x, y, w, h = Scaled0(x, f), Scaled0(y, f), Scaled0(w, f), Scaled0(h, f)
	}
	return Rect{
		Point{X: x, Y: y},
		Size{W: w, H: h},
	}
}

// GetScaleSize get the screen scale size
func GetScaleSize(displayId ...int) (int, int) {
	x, y := GetScreenSize()
	f := ScaleF(displayId...)
	return int(float64(x) * f), int(float64(y) * f)
}

// CaptureScreen capture the screen and return a bitmap (C struct).
// Use `defer robotgo.FreeBitmap(bitmap)` to free the bitmap.
//
// robotgo.CaptureScreen(x, y, w, h int)
func CaptureScreen(args ...int) (CBitmap, error) {
	argX, argY, argW, argH, argErr := captureRegionFromArgs(args)
	if argErr != nil {
		return nil, argErr
	}
	var x, y, w, h C.int32_t
	displayId := -1
	if configured := currentDisplayID(); configured != -1 {
		displayId = configured
	}

	if len(args) > 4 {
		displayId = args[4]
	}

	ds := selectedDisplayServer()

	if len(args) > 3 {
		x = C.int32_t(argX)
		y = C.int32_t(argY)
		w = C.int32_t(argW)
		h = C.int32_t(argH)
	} else if runtime.GOOS != "linux" {
		// Get the main screen rect on non-Linux platforms. Linux resolves X11
		// bounds while holding the native display lease in the X11 branch below;
		// Wayland and portal backends accept an empty rectangle as full-screen.
		rect := getScreenRectLocked(displayId, false)
		if err := validateCaptureRegionRequest(rect.X, rect.Y, rect.W, rect.H); err != nil {
			return nil, err
		}
		if runtime.GOOS == "windows" {
			x = C.int32_t(rect.X)
			y = C.int32_t(rect.Y)
		}

		w = C.int32_t(rect.W)
		h = C.int32_t(rect.H)
	}

	isPid := 0
	if currentTreatAsHandle() || len(args) > 5 {
		isPid = 1
	}

	if runtime.GOOS == "linux" {
		// Allow tests or environments to force the portal backend regardless
		// of the detected display server.
		backendOverride := strings.ToLower(strings.TrimSpace(os.Getenv(envWaylandBackend)))
		forcePortal := os.Getenv(envForcePortal) != "" || backendOverride == waylandBackendPortalName
		if len(args) <= 3 && ds == DisplayServerX11 &&
			(forcePortal || backendOverride == waylandBackendScreenCast) {
			// Preserve argumentless X11 crop semantics without holding the native
			// display lease during portal or PipeWire I/O.
			unlockDisplay := lockNativeX11Display()
			if x11MainDisplayAvailableLocked() {
				rect := getScreenRectLocked(displayId, false)
				if validateCaptureRegionRequest(rect.X, rect.Y, rect.W, rect.H) == nil {
					x = C.int32_t(rect.X)
					y = C.int32_t(rect.Y)
					w = C.int32_t(rect.W)
					h = C.int32_t(rect.H)
				}
			}
			unlockDisplay()
		}
		if backendOverride == waylandBackendScreenCast {
			bitmap, err := captureViaPersistentScreenCast(x, y, w, h)
			if err != nil {
				return nil, errors.Join(ErrPortalFailed, err)
			}
			captureDebugf("forced persistent ScreenCast backend (rect=%d,%d %dx%d)", int(x), int(y), int(w), int(h))
			return bitmap, nil
		}
		if forcePortal {
			if cb, pErr := captureViaPortalScreenshot(x, y, w, h); pErr == nil {
				captureDebugf("forced portal screenshot backend (display=%d, rect=%d,%d %dx%d)", displayId, int(x), int(y), int(w), int(h))
				return cb, nil
			} else if portalStubEnabled() {
				cb, sErr := captureViaPortalStub(x, y, w, h, displayId, isPid)
				if sErr != nil {
					return nil, fmt.Errorf("%w: %v", ErrPortalFailed, sErr)
				}
				captureDebugf("forced portal stub backend (display=%d, rect=%d,%d %dx%d)", displayId, int(x), int(y), int(w), int(h))
				return cb, nil
			}
			return nil, ErrPortalFailed
		}
		switch ds {
		case DisplayServerWayland:
			backend := selectedWaylandBackend()
			if envBackend, ok := waylandBackendFromEnv(); ok {
				backend = envBackend
			}
			var cerr C.int32_t
			bit := C.capture_screen_wayland(x, y, w, h, C.int32_t(displayId), C.int8_t(isPid), C.int32_t(backend), &cerr)
			if bit == nil {
				err := waylandErr(cerr)
				captureDebugf("wayland screencopy failed: %v", err)
				if cb, streamErr := captureViaPersistentScreenCast(x, y, w, h); streamErr == nil {
					captureDebugf("fallback to persistent ScreenCast backend")
					return cb, nil
				}
				// Try portal screenshot (real pixels) when available.
				if cb, pErr := captureViaPortalScreenshot(x, y, w, h); pErr == nil {
					captureDebugf("fallback to portal screenshot backend")
					return cb, nil
				}
				// Optional fallback to C portal stub for tests only.
				var sErr error
				if portalStubEnabled() {
					var cb CBitmap
					cb, sErr = captureViaPortalStub(x, y, w, h, displayId, isPid)
					if sErr == nil {
						captureDebugf("fallback to C portal stub backend")
						return cb, nil
					}
				}
				if errors.Is(err, ErrNoScreencopy) {
					if sErr != nil {
						return nil, fmt.Errorf("%w; %v", err, sErr)
					}
					return nil, fmt.Errorf("%w; %w", err, ErrPortalFailed)
				}
				return nil, err
			}
			setLastBackend(BackendScreencopy)
			return CBitmap(bit), nil
		case DisplayServerX11:
			// The shared X11 connection is borrowed by bounds and capture. Keep
			// the lease scoped to those native operations; portal, ScreenCast,
			// and Wayland I/O above never run while this mutex is held.
			unlockDisplay := lockNativeX11Display()
			defer unlockDisplay()
			if !x11MainDisplayAvailableLocked() {
				return nil, errors.New("no display server found")
			}
			if len(args) <= 3 {
				rect := getScreenRectLocked(displayId, false)
				if err := validateCaptureRegionRequest(rect.X, rect.Y, rect.W, rect.H); err != nil {
					return nil, err
				}
				x = C.int32_t(rect.X)
				y = C.int32_t(rect.Y)
				w = C.int32_t(rect.W)
				h = C.int32_t(rect.H)
			}
			bit := C.capture_screen(x, y, w, h, C.int32_t(displayId), C.int8_t(isPid))
			if bit == nil {
				return nil, errors.New("screen capture failed")
			}
			setLastBackend(BackendX11)
			return CBitmap(bit), nil
		default:
			return nil, errors.New("no display server found")
		}
	}

	bit := C.capture_screen(x, y, w, h, C.int32_t(displayId), C.int8_t(isPid))
	if bit == nil {
		return nil, errors.New("screen capture failed")
	}
	setLastBackend(BackendNone)
	return CBitmap(bit), nil
}

// CaptureGo capture the screen and return a Go bitmap.
func CaptureGo(args ...int) (Bitmap, error) {
	bit, err := CaptureScreen(args...)
	if err != nil {
		return Bitmap{}, err
	}
	defer FreeBitmap(bit)

	return ownedBitmapFromC(bit)
}

// CaptureImg capture the screen and return image.Image, error
func CaptureImg(args ...int) (image.Image, error) {
	bit, err := CaptureScreen(args...)
	if err != nil {
		return nil, err
	}
	defer FreeBitmap(bit)

	return ToImage(bit), nil
}

// FreeBitmap free and dealloc the C bitmap
func FreeBitmap(bitmap CBitmap) {
	if bitmap == nil {
		return
	}
	// C.destroyMMBitmap(bitmap)
	C.bitmap_dealloc(C.MMBitmapRef(bitmap))
}

// FreeBitmapArr free and dealloc the C bitmap array
func FreeBitmapArr(bit ...CBitmap) {
	for i := 0; i < len(bit); i++ {
		FreeBitmap(bit[i])
	}
}

// ToMMBitmapRef trans CBitmap to C.MMBitmapRef
func ToMMBitmapRef(bit CBitmap) C.MMBitmapRef {
	return C.MMBitmapRef(bit)
}

// ToBitmap trans C.MMBitmapRef to Bitmap
func ToBitmap(bit CBitmap) Bitmap {
	if bit == nil {
		return Bitmap{}
	}
	bitmap := Bitmap{
		ImgBuf:        (*uint8)(bit.imageBuffer),
		Width:         int(bit.width),
		Height:        int(bit.height),
		Bytewidth:     int(bit.bytewidth),
		BitsPixel:     uint8(bit.bitsPerPixel),
		BytesPerPixel: uint8(bit.bytesPerPixel),
		buf:           nil,
		trusted:       true,
	}

	return bitmap
}

func ownedBitmapFromC(bit CBitmap) (Bitmap, error) {
	bitmap := ToBitmap(bit)
	data, err := bitmapBytes(bitmap)
	if err != nil {
		return Bitmap{}, err
	}
	bitmap.buf = data
	if len(bitmap.buf) > 0 {
		bitmap.ImgBuf = &bitmap.buf[0]
	}
	return bitmap, nil
}

// ToCBitmap trans Bitmap to C.MMBitmapRef. Invalid input returns nil; callers
// that need the validation error should use ToCBitmapE.
func ToCBitmap(bit Bitmap) CBitmap {
	bitmap, _ := ToCBitmapE(bit)
	return bitmap
}

// ToCBitmapE validates and copies a Go Bitmap into C-owned memory.
func ToCBitmapE(bit Bitmap) (CBitmap, error) {
	data, err := bitmapBytes(bit)
	if err != nil {
		return nil, err
	}
	cptr := C.CBytes(data)
	if cptr == nil {
		return nil, errors.New("allocate C bitmap buffer")
	}
	cbitmap := C.createMMBitmap_c(
		(*C.uint8_t)(cptr),
		C.int32_t(bit.Width),
		C.int32_t(bit.Height),
		C.int32_t(bit.Bytewidth),
		C.uint8_t(bit.BitsPixel),
		C.uint8_t(bit.BytesPerPixel),
	)
	if cbitmap == nil {
		C.free(cptr)
		return nil, errors.New("create C bitmap")
	}
	return CBitmap(cbitmap), nil
}

// ToImage convert C.MMBitmapRef to standard image.Image
func ToImage(bit CBitmap) image.Image {
	img, err := ToRGBAE(bit)
	if err != nil {
		return nil
	}
	return img
}

// ToRGBA convert C.MMBitmapRef to standard image.RGBA
func ToRGBA(bit CBitmap) *image.RGBA {
	img, _ := ToRGBAE(bit)
	return img
}

// ToRGBAE validates a C bitmap before converting it to image.RGBA.
func ToRGBAE(bit CBitmap) (*image.RGBA, error) {
	if bit == nil {
		return nil, errors.New("bitmap is nil")
	}
	return ToRGBAGoE(ToBitmap(bit))
}

// ImgToCBitmap trans image.Image to CBitmap
func ImgToCBitmap(img image.Image) CBitmap {
	bitmap, _ := ImgToCBitmapE(img)
	return bitmap
}

// ImgToCBitmapE converts an image to a validated C bitmap.
func ImgToCBitmapE(img image.Image) (CBitmap, error) {
	bitmap, err := ImgToBitmapE(img)
	if err != nil {
		return nil, err
	}
	return ToCBitmapE(bitmap)
}

// ByteToCBitmap trans []byte to CBitmap
func ByteToCBitmap(by []byte) CBitmap {
	bitmap, _ := ByteToCBitmapE(by)
	return bitmap
}

// ByteToCBitmapE decodes image bytes and returns any decode or bitmap error.
func ByteToCBitmapE(by []byte) (CBitmap, error) {
	img, err := ByteToImg(by)
	if err != nil {
		return nil, err
	}
	return ImgToCBitmapE(img)
}

// SetXDisplayName set XDisplay name (Linux)
func SetXDisplayName(name string) error {
	if strings.IndexByte(name, 0) >= 0 {
		return errors.New("robotgo: X11 display name contains NUL")
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	unlockDisplay := lockNativeX11Display()
	defer unlockDisplay()
	mouseReleaseErr := releaseNativeMouseHoldsLocked(DisplayServerX11)
	releaseErr := releaseNativeX11KeyboardOwnershipLocked()
	clearNativeKeyboardStateLocked(DisplayServerX11)
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	str := C.set_XDisplay_name(cname)
	if err := toErr(str); err != nil {
		return errors.Join(mouseReleaseErr, releaseErr, err)
	}
	return errors.Join(mouseReleaseErr, releaseErr)
}

// GetXDisplayName get XDisplay name (Linux)
func GetXDisplayName() string {
	unlock := lockNativeX11Display()
	defer unlock()
	name := C.get_XDisplay_name()
	if name == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(name))
	return C.GoString(name)
}

func getXDisplayNameLocked() string {
	name := C.get_XDisplay_name_borrowed()
	if name == nil {
		return ""
	}
	return C.GoString(name)
}

// CloseMainDisplay closes the main display and ignores cleanup errors for
// compatibility. Prefer CloseMainDisplayE in new code.
func CloseMainDisplay() { _ = CloseMainDisplayE() }

// CloseMainDisplayE releases RobotGo-owned native X11 keys and closes the
// native main display.
func CloseMainDisplayE() error {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	unlockKeyboard := lockLinuxKeyboard()
	defer unlockKeyboard()
	unlockDisplay := lockNativeX11Display()
	defer unlockDisplay()
	mouseReleaseErr := releaseNativeMouseHoldsLocked(DisplayServerX11)
	releaseErr := releaseNativeX11KeyboardOwnershipLocked()
	clearNativeKeyboardStateLocked(DisplayServerX11)
	C.close_main_display()
	return errors.Join(mouseReleaseErr, releaseErr)
}

// Deprecated: use the ScaledF(),
//
// ScaleX get the primary display horizontal DPI scale factor, drop
func ScaleX() int {
	return int(C.scaleX())
}

/*
.___  ___.   ______    __    __       _______. _______
|   \/   |  /  __  \  |  |  |  |     /       ||   ____|
|  \  /  | |  |  |  | |  |  |  |    |   (----`|  |__
|  |\/|  | |  |  |  | |  |  |  |     \   \    |   __|
|  |  |  | |  `--'  | |  `--'  | .----)   |   |  |____
|__|  |__|  \______/   \______/  |_______/    |_______|

*/

// CheckMouse check the mouse button
func CheckMouse(btn string) C.MMMouseButton {
	// button = args[0].(C.MMMouseButton)
	m1 := map[string]C.MMMouseButton{
		"left":       C.LEFT_BUTTON,
		"center":     C.CENTER_BUTTON,
		"middle":     C.CENTER_BUTTON,
		"right":      C.RIGHT_BUTTON,
		"wheelDown":  C.WheelDown,
		"wheelUp":    C.WheelUp,
		"wheelLeft":  C.WheelLeft,
		"wheelRight": C.WheelRight,
	}
	if v, ok := m1[btn]; ok {
		return v
	}

	return C.LEFT_BUTTON
}

// MoveScale calculate the os scale factor x, y
func MoveScale(x, y int, displayId ...int) (int, int) {
	if currentScale() || runtime.GOOS == "windows" {
		f := ScaleF()
		x, y = Scaled1(x, f), Scaled1(y, f)
	}

	return x, y
}

func moveScaleLocked(x, y int, displayId ...int) (int, int) {
	if currentScale() || runtime.GOOS == "windows" {
		f := scaleFLocked(displayId...)
		x, y = Scaled1(x, f), Scaled1(y, f)
	}
	return x, y
}

var waylandMouseMu sync.Mutex

type mouseHold struct {
	backend          persistentInputBackend
	server           DisplayServer
	portalGeneration uint64
	portalButton     int32
}

type portalMouseRefID struct {
	generation uint64
	button     int32
}

var (
	mouseHolds            = make(map[string]mouseHold)
	portalMouseButtonRefs = make(map[portalMouseRefID]uint)
)

func canonicalMouseHoldName(name string) string {
	switch name {
	case "", "left":
		return "left"
	case "middle":
		return "center"
	default:
		return name
	}
}

func nativeMouseStatusError(status C.int, operation string) error {
	switch status {
	case C.ROBOTGO_MOUSE_OK:
		return nil
	case C.ROBOTGO_MOUSE_NO_DISPLAY:
		return fmt.Errorf("%w: %s: native mouse display is unavailable", ErrNotSupported, operation)
	case C.ROBOTGO_MOUSE_UNSUPPORTED:
		return fmt.Errorf("%w: %s", ErrNotSupported, operation)
	case C.ROBOTGO_MOUSE_INJECTION_FAILED:
		return fmt.Errorf("robotgo: %s: native mouse injection failed", operation)
	case C.ROBOTGO_MOUSE_OWNERSHIP_CONFLICT:
		return fmt.Errorf("%w: %s", ErrInputOwnership, operation)
	case C.ROBOTGO_MOUSE_INVALID:
		return fmt.Errorf("robotgo: %s: invalid native mouse button", operation)
	default:
		return fmt.Errorf("robotgo: %s: unknown native mouse status %d", operation, int(status))
	}
}

func nativeWaylandMouseButtonCodeForTest(name string) (uint32, error) {
	var code C.uint32_t
	var index C.uint
	status := C.robotgo_wayland_mouse_button_code(
		CheckMouse(canonicalMouseHoldName(name)), &code, &index,
	)
	return uint32(code), nativeMouseStatusError(status, "map Wayland mouse button")
}

func nativeWaylandMouseBackendSelectedForTest() bool {
	return bool(C.robotgo_wayland_mouse_backend_selected())
}

func clearPortalMouseStateLocked() {
	clear(portalMouseButtonRefs)
	for name, hold := range mouseHolds {
		if hold.backend == persistentInputBackendPortal {
			delete(mouseHolds, name)
		}
	}
}

func clearPortalMouseGenerationLocked(generation uint64) {
	for id := range portalMouseButtonRefs {
		if id.generation == generation {
			delete(portalMouseButtonRefs, id)
		}
	}
	for name, hold := range mouseHolds {
		if hold.backend == persistentInputBackendPortal &&
			hold.portalGeneration == generation {
			delete(mouseHolds, name)
		}
	}
}

func releaseNativeMouseHoldsLocked(server DisplayServer) error {
	var releaseErr error
	if server == DisplayServerX11 {
		releaseErr = nativeMouseStatusError(
			C.robotgo_x11_release_owned_buttons(),
			"release owned X11 mouse buttons",
		)
	}
	for name, hold := range mouseHolds {
		matches := hold.server == server ||
			server == DisplayServerX11 && hold.server != DisplayServerWayland
		if hold.backend != persistentInputBackendNative || !matches {
			continue
		}
		if server == DisplayServerWayland {
			releaseErr = errors.Join(releaseErr, nativeMouseStatusError(
				C.toggleMouse(C.bool(false), CheckMouse(name)),
				"release owned Wayland mouse button",
			))
		}
		delete(mouseHolds, name)
	}
	return releaseErr
}

func tryPortalMouseDown(server DisplayServer, name string) (bool, mouseHold, error) {
	if runtime.GOOS != "linux" || server != DisplayServerWayland {
		return false, mouseHold{}, nil
	}
	var hold mouseHold
	used, generation, err := withRemoteDesktopInputLease(
		inputportal.DevicePointer, nil,
		func(session remoteDesktopInputSession, generation uint64) error {
			button, err := portalPointerButton(name)
			if err != nil {
				return err
			}
			id := portalMouseRefID{generation: generation, button: button}
			if portalMouseButtonRefs[id] != 0 {
				return ErrInputOwnership
			}
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.PointerButton(ctx, button, true)
			}); err != nil {
				return err
			}
			portalMouseButtonRefs[id] = 1
			hold = mouseHold{
				backend:          persistentInputBackendPortal,
				server:           DisplayServerWayland,
				portalGeneration: generation,
				portalButton:     button,
			}
			return nil
		},
	)
	if used && portalInputFailureInvalidatesSession(err) {
		closeErr := CloseRemoteDesktopInput()
		clearPortalMouseGenerationLocked(generation)
		return true, mouseHold{}, errors.Join(err, closeErr)
	}
	return used, hold, err
}

func tryPortalMouseUp(hold mouseHold) (bool, error) {
	expected := hold.portalGeneration
	used, currentGeneration, err := withRemoteDesktopInputLease(
		inputportal.DevicePointer, &expected,
		func(session remoteDesktopInputSession, generation uint64) error {
			id := portalMouseRefID{
				generation: generation,
				button:     hold.portalButton,
			}
			if portalMouseButtonRefs[id] != 1 {
				return ErrInputOwnership
			}
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.PointerButton(ctx, hold.portalButton, false)
			}); err != nil {
				return err
			}
			delete(portalMouseButtonRefs, id)
			return nil
		},
	)
	if errors.Is(err, ErrInputOwnership) || errors.Is(err, inputportal.ErrClosed) || !used {
		clearPortalMouseGenerationLocked(hold.portalGeneration)
		return used, errors.Join(ErrInputOwnership, err)
	}
	if err != nil {
		closeErr := CloseRemoteDesktopInput()
		clearPortalMouseGenerationLocked(currentGeneration)
		return true, errors.Join(err, closeErr)
	}
	return true, nil
}

func lockLinuxMouse() func() {
	if runtime.GOOS == "linux" {
		waylandMouseMu.Lock()
		return waylandMouseMu.Unlock
	}
	return func() {}
}

func lockNativeMouseDisplay(server DisplayServer) func() {
	if runtime.GOOS == "linux" && nativeX11BackendCompiled() &&
		server == DisplayServerX11 {
		return lockNativeX11Display()
	}
	return func() {}
}

// runNativeMouseOperation scopes the shared X11 display lease to the native
// probe and event only. Callers retain the Linux mouse transaction lock while
// selecting a portal fallback, but portal I/O never holds nativeX11DisplayMu.
func runNativeMouseOperation(server DisplayServer, operation func() error) (ready bool, err error) {
	unlockDisplay := lockNativeMouseDisplay(server)
	defer unlockDisplay()
	if err := ensureWaylandMouseReady(server); err != nil {
		return false, err
	}
	return true, operation()
}

func ensureWaylandMouseReady(server DisplayServer) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	switch server {
	case DisplayServerX11:
		return nativeX11InputReadyLocked()
	case DisplayServerWayland:
		if int(C.robotgo_wayland_mouse_backend_enabled()) == 0 {
			return fmt.Errorf("%w: robotgo was built without the Wayland mouse backend (build with -tags wayland)", ErrNotSupported)
		}
		if int(C.robotgo_wayland_mouse_ready()) == 0 {
			return fmt.Errorf("%w: zwlr_virtual_pointer_v1 is unavailable", ErrNotSupported)
		}
		return nil
	default:
		return fmt.Errorf("%w: no supported display server is selected", ErrNotSupported)
	}
}

// MouseReady reports whether the active display backend can inject mouse
// input. On Wayland it performs a real virtual-pointer protocol probe.
func MouseReady() error {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	_, nativeErr := runNativeMouseOperation(server, func() error { return nil })
	if nativeErr == nil {
		return nil
	}
	if server == DisplayServerWayland {
		if used, err := withRemoteDesktopInput(inputportal.DevicePointer, func(remoteDesktopInputSession) error { return nil }); used {
			return err
		}
	}
	return nativeErr
}

func nativeWaylandMouseReady() error {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	_, err := runNativeMouseOperation(server, func() error { return nil })
	return err
}

// CloseWaylandInput releases persistent virtual-pointer and virtual-keyboard
// protocol objects. A later input call reconnects lazily.
func CloseWaylandInput() {
	// Cancel portal I/O before waiting for a device transaction lock. Portal
	// operations hold one of these locks while using their cancellable context.
	_ = CloseRemoteDesktopInput()
	waylandMouseMu.Lock()
	defer waylandMouseMu.Unlock()
	linuxKeyboardMu.Lock()
	defer linuxKeyboardMu.Unlock()
	_ = releaseNativeMouseHoldsLocked(DisplayServerWayland)
	C.robotgo_wayland_mouse_close()
	closeWaylandKeyboard()
	clearNativeKeyboardStateLocked(DisplayServerWayland)
	clearPortalMouseStateLocked()
	clearPortalKeyboardStateLocked()
}

// Move move the mouse to (x, y)
//
// Examples:
//
//	robotgo.MouseSleep = 100  // 100 millisecond
//	robotgo.Move(10, 10)
func Move(x, y int, displayId ...int) {
	_ = MoveE(x, y, displayId...)
}

// MoveE moves the mouse to (x, y) and reports backend availability errors.
// Prefer it over Move when the caller must know whether injection succeeded.
func MoveE(x, y int, displayId ...int) error {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		x, y = moveScaleLocked(x, y, displayId...)
		C.moveMouse(C.MMPointInt32Make(C.int32_t(x), C.int32_t(y)))
		return nil
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			used, err := tryRemoteDesktopMoveAbsolute(x, y, displayId)
			if used {
				return finishRemoteDesktopMouseEvent(err, 0)
			}
		}
		return nativeErr
	}

	MilliSleep(currentMouseDelay())
	return nil
}

// Deprecated: use the DragSmooth(),
//
// Drag drag the mouse to (x, y),
// It's not valid now, use the DragSmooth()
func Drag(x, y int, args ...string) {
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	_, err := runNativeMouseOperation(server, func() error {
		x, y = moveScaleLocked(x, y)
		button := C.MMMouseButton(C.LEFT_BUTTON)
		if len(args) > 0 {
			button = CheckMouse(args[0])
		}
		C.dragMouse(C.MMPointInt32Make(C.int32_t(x), C.int32_t(y)), button)
		return nil
	})
	if err != nil {
		return
	}
	MilliSleep(currentMouseDelay())
}

// DragSmooth drag the mouse like smooth to (x, y)
//
// Examples:
//
//	robotgo.DragSmooth(10, 10)
func DragSmooth(x, y int, args ...interface{}) {
	if _, _, _, ok := parseSmoothMoveArguments(args); !ok {
		return
	}
	if err := Toggle("left"); err != nil {
		return
	}
	MilliSleep(50)
	MoveSmooth(x, y, args...)
	if err := Toggle("left", "up"); err != nil {
		return
	}
}

// MoveSmooth move the mouse smooth,
// moves mouse to x, y human like, with the mouse button up.
//
// robotgo.MoveSmooth(x, y int, low, high float64, mouseDelay int)
//
// Examples:
//
//	robotgo.MoveSmooth(10, 10)
//	robotgo.MoveSmooth(10, 10, 1.0, 2.0)
func MoveSmooth(x, y int, args ...interface{}) bool {
	lowDelay, highDelay, mouseDelay, ok := parseSmoothMoveArguments(args)
	if !ok {
		return false
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	var moved bool
	_, err := runNativeMouseOperation(server, func() error {
		x, y = moveScaleLocked(x, y)
		moved = bool(C.smoothlyMoveMouse(
			C.MMPointInt32Make(C.int32_t(x), C.int32_t(y)),
			C.double(lowDelay), C.double(highDelay),
		))
		return nil
	})
	if err != nil {
		return false
	}
	MilliSleep(currentMouseDelay() + mouseDelay)
	return moved
}

// MoveArgs get the mouse relative args
func MoveArgs(x, y int) (int, int) {
	mx, my := Location()
	mx = mx + x
	my = my + y

	return mx, my
}

// MoveRelative move mouse with relative
func MoveRelative(x, y int) {
	_ = MoveRelativeE(x, y)
}

// MoveRelativeE moves the mouse by a relative delta and reports backend
// availability errors.
func MoveRelativeE(x, y int) error {
	if runtime.GOOS != "linux" {
		return MoveE(MoveArgs(x, y))
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		dx, dy := moveScaleLocked(x, y)
		C.moveMouseRelative(C.int32_t(dx), C.int32_t(dy))
		return nil
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			used, err := tryRemoteDesktopMoveRelative(x, y)
			if used {
				return finishRemoteDesktopMouseEvent(err, 0)
			}
		}
		return nativeErr
	}
	MilliSleep(currentMouseDelay())
	return nil
}

// MoveSmoothRelative move mouse smooth with relative
func MoveSmoothRelative(x, y int, args ...interface{}) {
	if _, _, _, ok := parseSmoothMoveArguments(args); !ok {
		return
	}
	mx, my := MoveArgs(x, y)
	MoveSmooth(mx, my, args...)
}

// Location get the mouse location position return x, y
func Location() (int, int) {
	x, y, _ := LocationE()
	return x, y
}

// LocationE returns the current pointer position. Native Wayland does not
// expose a trustworthy global pointer location, so it returns ErrNotSupported
// instead of presenting the last injected position as an observation.
func LocationE() (int, int, error) {
	unlock := func() {}
	if runtime.GOOS == "linux" {
		switch selectedDisplayServer() {
		case DisplayServerWayland:
			return 0, 0, fmt.Errorf("%w: global pointer location is not exposed by Wayland", ErrNotSupported)
		case DisplayServerX11:
		default:
			return 0, 0, fmt.Errorf("%w: no supported display server is selected", ErrNotSupported)
		}
		unlock = lockNativeX11Display()
		if err := nativeX11DisplayReadyLocked(); err != nil {
			unlock()
			return 0, 0, err
		}
	}
	defer unlock()
	pos := C.location()
	x := int(pos.x)
	y := int(pos.y)

	if currentScale() || runtime.GOOS == "windows" {
		f := scaleFLocked()
		x, y = Scaled0(x, f), Scaled0(y, f)
	}

	return x, y, nil
}

// Click click the mouse button
//
// robotgo.Click(button string, double bool)
//
// Examples:
//
//	robotgo.Click() // default is left button
//	robotgo.Click("right")
//	robotgo.Click("wheelLeft")
func Click(args ...interface{}) {
	_ = ClickE(args...)
}

// ClickE clicks a mouse button and reports backend availability errors.
func ClickE(args ...interface{}) error {
	name, double, err := parseClickArguments(args)
	if err != nil {
		return err
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	name = canonicalMouseHoldName(name)
	if runtime.GOOS == "linux" {
		if _, held := mouseHolds[name]; held {
			return ErrInputOwnership
		}
	}
	server := selectedDisplayServer()
	button := CheckMouse(name)
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		if !double {
			return nativeMouseStatusError(C.clickMouse(button), "click mouse button")
		}
		return nativeMouseStatusError(C.doubleClick(button), "double-click mouse button")
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			used, err := tryRemoteDesktopClick(name, double)
			if used {
				return finishRemoteDesktopMouseEvent(err, 0)
			}
		}
		return nativeErr
	}

	MilliSleep(currentMouseDelay())
	return nil
}

// MoveClick move and click the mouse
//
// robotgo.MoveClick(x, y int, button string, double bool)
//
// Examples:
//
//	robotgo.MouseSleep = 100
//	robotgo.MoveClick(10, 10)
func MoveClick(x, y int, args ...interface{}) {
	Move(x, y)
	MilliSleep(50)
	Click(args...)
}

// MovesClick move smooth and click the mouse
//
// use the `robotgo.MouseSleep = 100`
func MovesClick(x, y int, args ...interface{}) {
	MoveSmooth(x, y)
	MilliSleep(50)
	Click(args...)
}

// Toggle toggle the mouse, support button:
//
//		"left", "center", "right",
//	 "wheelDown", "wheelUp", "wheelLeft", "wheelRight"
//
// Examples:
//
//	robotgo.Toggle("left") // default is down
//	robotgo.Toggle("left", "up")
func Toggle(key ...interface{}) error {
	name, down, err := parseToggleArguments(key)
	if err != nil {
		return err
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	name = canonicalMouseHoldName(name)
	if runtime.GOOS != "linux" {
		if err := nativeMouseStatusError(
			C.toggleMouse(C.bool(down), CheckMouse(name)),
			"toggle mouse button",
		); err != nil {
			return err
		}
		if len(key) > 2 {
			MilliSleep(currentMouseDelay())
		}
		return nil
	}
	server := selectedDisplayServer()

	if !down {
		hold, ok := mouseHolds[name]
		if !ok {
			return ErrInputOwnership
		}
		if hold.backend == persistentInputBackendPortal {
			_, err := tryPortalMouseUp(hold)
			delete(mouseHolds, name)
			return err
		}
		button := CheckMouse(name)
		_, nativeErr := runNativeMouseOperation(hold.server, func() error {
			return nativeMouseStatusError(
				C.toggleMouse(C.bool(false), button),
				"release mouse button",
			)
		})
		if nativeErr == nil || errors.Is(nativeErr, ErrInputOwnership) {
			delete(mouseHolds, name)
		}
		return nativeErr
	}

	if existing, ok := mouseHolds[name]; ok {
		if existing.backend != persistentInputBackendPortal ||
			existing.portalGeneration == remoteDesktopInputGeneration() {
			return ErrInputOwnership
		}
		clearPortalMouseGenerationLocked(existing.portalGeneration)
	}
	button := CheckMouse(name)
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		return nativeMouseStatusError(
			C.toggleMouse(C.bool(true), button),
			"press mouse button",
		)
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			if used, hold, err := tryPortalMouseDown(server, name); used {
				if err == nil {
					mouseHolds[name] = hold
				}
				return err
			}
		}
		return nativeErr
	}
	mouseHolds[name] = mouseHold{
		backend: persistentInputBackendNative,
		server:  server,
	}
	if len(key) > 2 {
		MilliSleep(currentMouseDelay())
	}

	return nil
}

// MouseDown send mouse down event
func MouseDown(key ...interface{}) error {
	return Toggle(key...)
}

// MouseUp send mouse up event
func MouseUp(key ...interface{}) error {
	if len(key) <= 0 {
		key = append(key, "left")
	}
	return Toggle(append(key, "up")...)
}

// Scroll scroll the mouse to (x, y)
//
// robotgo.Scroll(x, y, msDelay int)
//
// Examples:
//
//	robotgo.Scroll(10, 10)
func Scroll(x, y int, args ...int) {
	_ = ScrollE(x, y, args...)
}

// ScrollE scrolls the mouse and reports backend availability errors.
func ScrollE(x, y int, args ...int) error {
	msDelay, validationErr := parseScrollDelay(args)
	if validationErr != nil {
		return validationErr
	}
	unlockMouse := lockLinuxMouse()
	defer unlockMouse()
	server := selectedDisplayServer()
	ready, nativeErr := runNativeMouseOperation(server, func() error {
		C.scrollMouseXY(C.int(x), C.int(y))
		return nil
	})
	if nativeErr != nil {
		if shouldTryRemoteDesktopAfterNative(server, ready, nativeErr) {
			used, err := tryRemoteDesktopScroll(x, y)
			if used {
				return finishRemoteDesktopMouseEvent(err, msDelay)
			}
		}
		return nativeErr
	}
	MilliSleep(currentMouseDelay() + msDelay)
	return nil
}

// ScrollDir scroll the mouse with direction to (x, "up")
// supported: "up", "down", "left", "right"
//
// Examples:
//
//	robotgo.ScrollDir(10, "down")
//	robotgo.ScrollDir(10, "up")
func ScrollDir(x int, direction ...interface{}) {
	d, err := parseScrollDirection(direction)
	if err != nil {
		return
	}

	if d == "down" {
		Scroll(0, -x)
	}
	if d == "up" {
		Scroll(0, x)
	}

	if d == "left" {
		Scroll(x, 0)
	}
	if d == "right" {
		Scroll(-x, 0)
	}
	// MilliSleep(MouseSleep)
}

// ScrollSmooth scroll the mouse smooth,
// default scroll 5 times and sleep 100 millisecond
//
// robotgo.ScrollSmooth(toy, num, sleep, tox)
//
// Examples:
//
//	robotgo.ScrollSmooth(-10)
//	robotgo.ScrollSmooth(-10, 6, 200, -10)
func ScrollSmooth(to int, args ...int) {
	i := 0
	num := 5
	if len(args) > 0 {
		num = args[0]
	}
	tm := 100
	if len(args) > 1 {
		tm = args[1]
	}
	tox := 0
	if len(args) > 2 {
		tox = args[2]
	}

	for {
		Scroll(tox, to)
		MilliSleep(tm)
		i++
		if i == num {
			break
		}
	}
	MilliSleep(currentMouseDelay())
}

// ScrollRelative scroll mouse with relative
//
// Examples:
//
//	robotgo.ScrollRelative(10, 10)
func ScrollRelative(x, y int, args ...int) {
	mx, my := MoveArgs(x, y)
	Scroll(mx, my, args...)
}

/*
____    __    ____  __  .__   __.  _______   ______   ____    __    ____
\   \  /  \  /   / |  | |  \ |  | |       \ /  __  \  \   \  /  \  /   /
 \   \/    \/   /  |  | |   \|  | |  .--.  |  |  |  |  \   \/    \/   /
  \            /   |  | |  . `  | |  |  |  |  |  |  |   \            /
   \    /\    /    |  | |  |\   | |  '--'  |  `--'  |    \    /\    /
    \__/  \__/     |__| |__| \__| |_______/ \______/      \__/  \__/

*/

func alertArgs(args ...string) (string, string) {
	var (
		defaultBtn = "Ok"
		cancelBtn  = "Cancel"
	)

	if len(args) > 0 {
		defaultBtn = args[0]
	}

	if len(args) > 1 {
		cancelBtn = args[1]
	}

	return defaultBtn, cancelBtn
}

// IsValid valid the window
func IsValid() bool {
	unlock := lockNativeX11Display()
	defer unlock()
	abool := C.is_valid()
	gbool := bool(abool)
	return gbool
}

// IsTopMost reports whether the current active window is topmost.
func IsTopMost() bool {
	ok, _ := IsTopMostE()
	return ok
}

// IsTopMostE reports whether the current active window is topmost and returns
// an explicit unsupported error on Linux backends without reliable state
// query support.
func IsTopMostE() (bool, error) {
	if runtime.GOOS == "linux" {
		return false, linuxWindowStateNotSupported("query topmost state")
	}
	return bool(C.IsTopMost()), nil
}

// IsMinimized reports whether the current active window is minimized.
func IsMinimized() bool {
	ok, _ := IsMinimizedE()
	return ok
}

// IsMinimizedE reports whether the current active window is minimized and
// returns an explicit unsupported error on Linux backends without reliable
// state query support.
func IsMinimizedE() (bool, error) {
	if runtime.GOOS == "linux" {
		return false, linuxWindowStateNotSupported("query minimized state")
	}
	return bool(C.IsMinimized()), nil
}

// IsMaximized reports whether the current active window is maximized.
func IsMaximized() bool {
	ok, _ := IsMaximizedE()
	return ok
}

// IsMaximizedE reports whether the current active window is maximized.
// Hyprland uses its compositor state; Linux backends without a trustworthy
// query return an explicit unsupported error.
func IsMaximizedE() (bool, error) {
	if runtime.GOOS == "linux" {
		return resolveWindowBackend().Maximized()
	}
	return bool(C.IsMaximized()), nil
}

// SetTopMost updates topmost state for platforms that support it.
func SetTopMost(state bool) {
	_ = SetTopMostE(state)
}

// SetTopMostE updates topmost state and returns an explicit unsupported error
// on Linux backends without reliable topmost support.
func SetTopMostE(state bool) error {
	if runtime.GOOS == "linux" {
		return linuxWindowStateNotSupported("set topmost state")
	}
	C.SetTopMost(C.bool(state))
	return nil
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

// SetActiveC set the window active
func SetActiveC(win C.MData) {
	unlock := lockNativeX11Display()
	defer unlock()
	_ = C.set_active(win)
}

func nativeSetActive(win Handle) bool {
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.set_active(C.MData(win)))
}

func nativeMinWindow(pid int, state bool, isPid bool) bool {
	flag := 0
	if isPid {
		flag = 1
	}
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.min_window(C.uintptr(pid), C.bool(state), C.int8_t(flag)))
}

func nativeMaxWindow(pid int, state bool, isPid bool) bool {
	flag := 0
	if isPid {
		flag = 1
	}
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.max_window(C.uintptr(pid), C.bool(state), C.int8_t(flag)))
}

func nativeCloseMainWindow() bool {
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.close_main_window())
}

func nativeCloseWindowByPid(pid int, isPid bool) bool {
	flag := 0
	if isPid {
		flag = 1
	}
	unlock := lockNativeX11Display()
	defer unlock()
	return bool(C.close_window_by_PId(C.uintptr(pid), C.int8_t(flag)))
}

func nativeGetMainTitle() string {
	unlock := lockNativeX11Display()
	defer unlock()
	title := C.get_main_title()
	if title == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(title))
	return C.GoString(title)
}

func nativeGetInternalTitle(pid int, isPid int) string {
	return internalGetTitle(pid, isPid)
}

// GetActive get the active window
func GetActive() Handle {
	return Handle(GetActiveC())
}

// GetActiveC get the active window
func GetActiveC() C.MData {
	unlock := lockNativeX11Display()
	defer unlock()
	mdata := C.get_active()
	// fmt.Println("active----", mdata)
	return mdata
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

// SetHandle set the window handle
func SetHandle(hwnd int) {
	chwnd := C.uintptr(hwnd)
	unlock := lockNativeX11Display()
	defer unlock()
	C.setHandle(chwnd)
}

// SetHandlePid set the window handle by pid
func SetHandlePid(pid int, args ...int) {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
	}

	unlock := lockNativeX11Display()
	defer unlock()
	C.set_handle_pid_mData(C.uintptr(pid), C.int8_t(isPid))
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

// GetHandByPidC get handle mdata by pid
func GetHandByPidC(pid int, args ...int) C.MData {
	var isPid int
	if len(args) > 0 || currentTreatAsHandle() {
		isPid = 1
	}

	unlock := lockNativeX11Display()
	defer unlock()
	return C.set_handle_pid(C.uintptr(pid), C.int8_t(isPid))
}

// GetHandle get the window handle
func GetHandle() int {
	unlock := lockNativeX11Display()
	defer unlock()
	hwnd := C.get_handle()
	ghwnd := int(hwnd)
	// fmt.Println("gethwnd---", ghwnd)
	return ghwnd
}

// Deprecated: use the GetHandle(),
//
// # GetBHandle get the window handle, Wno-deprecated
//
// This function will be removed in version v1.0.0
func GetBHandle() int {
	tt.Drop("GetBHandle", "GetHandle")
	unlock := lockNativeX11Display()
	defer unlock()
	hwnd := C.b_get_handle()
	ghwnd := int(hwnd)
	//fmt.Println("gethwnd---", ghwnd)
	return ghwnd
}

func cgetTitle(pid, isPid int) string {
	unlock := lockNativeX11Display()
	defer unlock()
	return cgetTitleLocked(pid, isPid)
}

func cgetTitleLocked(pid, isPid int) string {
	title := C.get_title_by_pid(C.uintptr(pid), C.int8_t(isPid))
	if title == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(title))
	return C.GoString(title)
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

// GetPid get the process id return int32
func GetPid() int {
	unlock := lockNativeX11Display()
	defer unlock()
	pid := C.get_PID()
	return int(pid)
}

// internalGetBounds get the window bounds
func internalGetBounds(pid, isPid int) (int, int, int, int) {
	unlock := lockNativeX11Display()
	defer unlock()
	return internalGetBoundsLocked(pid, isPid)
}

func internalGetBoundsLocked(pid, isPid int) (int, int, int, int) {
	bounds := C.get_bounds(C.uintptr(pid), C.int8_t(isPid))
	return int(bounds.X), int(bounds.Y), int(bounds.W), int(bounds.H)
}

// internalGetClient get the window client bounds
func internalGetClient(pid, isPid int) (int, int, int, int) {
	unlock := lockNativeX11Display()
	defer unlock()
	return internalGetClientLocked(pid, isPid)
}

func internalGetClientLocked(pid, isPid int) (int, int, int, int) {
	bounds := C.get_client(C.uintptr(pid), C.int8_t(isPid))
	return int(bounds.X), int(bounds.Y), int(bounds.W), int(bounds.H)
}

// Is64Bit determine whether the sys is 64bit
func Is64Bit() bool {
	b := C.Is64Bit()
	return bool(b)
}

func internalActive(pid, isPid int) bool {
	unlock := lockNativeX11Display()
	defer unlock()
	return internalActiveLocked(pid, isPid)
}

func internalActiveLocked(pid, isPid int) bool {
	return bool(C.active_PID(C.uintptr(pid), C.int8_t(isPid)))
}

// ActivePid active the window by Pid,
// If args[0] > 0 on the Windows platform via a window handle to active
// func ActivePid(pid int32, args ...int) {
// 	var isPid int
// 	if len(args) > 0 {
// 		isPid = args[0]
// 	}

// 	C.active_PID(C.uintptr(pid), C.uintptr(isPid))
// }

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
