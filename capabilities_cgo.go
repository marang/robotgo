//go:build cgo

package robotgo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	inputportal "github.com/marang/robotgo/input/portal"
	portalpkg "github.com/marang/robotgo/screen/portal"
)

func nativeWaylandProtocolVersions() nativeWaylandProtocolInfo {
	if selectedDisplayServer() != DisplayServerWayland {
		return nativeWaylandProtocolInfo{}
	}
	info := nativeWaylandProtocolInfo{
		Screencopy: nativeScreencopyProtocolVersion(),
	}
	info.VirtualKeyboard = nativeWaylandKeyboardProtocolVersion()
	unlockMouse := lockLinuxMouse()
	info.VirtualPointer = nativeMouseProtocolVersion()
	unlockMouse()
	return info
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
		return nativeScreencopyReady()
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
		Accessibility:  accessibilityCapability(),
	}

	switch ds {
	case DisplayServerWayland:
		c.Compositor = detectWaylandCompositor()
		c.RemoteDesktop = probeRemoteDesktopCapability()
		nativeCapture := waylandCaptureAvailabilityProbe()
		portalAllowed := os.Getenv(envDisablePortal) == ""
		portalAvailable := portalAllowed && portalAvailabilityProbe()
		persistentCapture := portalAllowed && ScreenCastCaptureReady() == nil
		switch {
		case nativeCapture:
			c.Capture = FeatureCapability{
				Available: true,
				Fallback:  persistentCapture || portalAvailable,
				Backend:   FeatureBackendWaylandScreencopy,
				Reason:    "screencopy manager and compatible buffer backend detected",
				Notes:     "native screencopy path (dmabuf/wl_shm)",
			}
			if persistentCapture {
				c.Capture.Notes += "; active ScreenCast fallback: " + screenCastCapabilityNotes()
			} else if portalAvailable {
				c.Capture.Notes += "; desktop portal fallback detected"
				if screenCastCaptureCompiled() {
					c.Capture.Notes += "; " + screenCastCapabilityNotes()
				}
			}
		case persistentCapture:
			c.Capture = FeatureCapability{
				Available: true,
				Fallback:  portalAvailable,
				Backend:   FeatureBackendScreenCast,
				Reason:    "an active ScreenCast session provides reusable PipeWire frames",
				Notes:     screenCastCapabilityNotes(),
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
		size := nativeMainDisplaySize()
		unlockDisplay()
		if size.W > 0 && size.H > 0 {
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
				Reason:    "zwp_virtual_keyboard_manager_v1 and a Wayland seat are available",
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
		displayErr, inputErr := runtimeX11CapabilityErrors()
		nativeBackendReason := "native X11 backend is not compiled for this build"
		if displayErr != nil {
			nativeBackendReason = displayErr.Error()
		}
		if capabilityProbeSucceeded(displayErr) {
			captureNotes := "native X11 capture path"
			boundsNotes := "native X11 bounds path"
			if !nativeX11BackendCompiled() {
				captureNotes = "XGB/Xinerama compatibility capture path"
				boundsNotes = "XGB/Xinerama compatibility bounds path"
			}
			c.Capture = FeatureCapability{Available: true, Backend: "x11", Reason: "X11 display connection is available", Notes: captureNotes}
			c.Bounds = FeatureCapability{Available: true, Backend: "x11", Reason: "X11 display connection is available", Notes: boundsNotes}
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
			c.Window = FeatureCapability{Available: false, Backend: "x11", Reason: nativeBackendReason, Notes: "build the native X11 backend and verify the configured X11 display"}
		}
		if capabilityProbeSucceeded(displayErr) && nativeX11BackendCompiled() {
			c.Hook = FeatureCapability{Available: true, Backend: "x11", Reason: "X11 display connection is available", Notes: "native X11 hook/event path"}
		} else {
			c.Hook = FeatureCapability{Available: false, Backend: "x11", Reason: nativeBackendReason, Notes: "build the native X11 backend and verify the configured X11 display"}
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
