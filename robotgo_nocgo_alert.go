//go:build !cgo && !linux

package robotgo

// Alert reports failure when no native dialog backend is selected.
func Alert(string, string, ...string) bool { return false }
