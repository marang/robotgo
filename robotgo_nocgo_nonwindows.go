//go:build !cgo && !windows

package robotgo

// FindWindow is a Windows-only operation.
func FindWindow(string) uintptr { return 0 }

// GetMainId returns the default display index when no native backend is selected.
func GetMainId() int { return 0 }
