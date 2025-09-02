//go:build !linux
// +build !linux

package screen

// openXDisplay always reports that an X display is unavailable on non-Linux systems.
func openXDisplay(name string) bool {
	return false
}
