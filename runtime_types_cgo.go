//go:build cgo

package robotgo

import (
	"os"
)

const (
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
	Accessibility  FeatureCapability
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
