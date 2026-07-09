//go:build !linux || wayland
// +build !linux wayland

package robotgo

func x11MainDisplayAvailable() bool {
	return false
}
