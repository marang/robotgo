//go:build !cgo && !windows

package robotgo

// FindWindow is a Windows-only operation.
func FindWindow(string) uintptr { return 0 }
