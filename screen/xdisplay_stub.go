//go:build !linux && cgo
// +build !linux,cgo

package screen

// openXDisplay always reports that an X display is unavailable on non-Linux systems.
func openXDisplay(name string) bool {
	return false
}
