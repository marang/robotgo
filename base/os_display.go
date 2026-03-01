//go:build linux

package base

/*
#include "os.h"
*/
import "C"

// DisplayServer mirrors the values from the C enum in os.h.
type DisplayServer int

const (
	Wayland DisplayServer = C.Wayland
	X11     DisplayServer = C.X11
	Unknown DisplayServer = C.Unknown
)

// DetectDisplayServer calls the C detectDisplayServer function.
func DetectDisplayServer() DisplayServer {
	return DisplayServer(C.detectDisplayServer())
}
