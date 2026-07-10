//go:build !cgo

package base

import "os"

// DisplayServer identifies the active Linux display server.
type DisplayServer string

const (
	Wayland DisplayServer = "wayland"
	X11     DisplayServer = "x11"
	Unknown DisplayServer = "unknown"
)

// DetectDisplayServer detects Wayland before X11, matching the CGO backend.
func DetectDisplayServer() DisplayServer {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return Wayland
	}
	if os.Getenv("DISPLAY") != "" {
		return X11
	}
	return Unknown
}
