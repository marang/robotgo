//go:build !cgo && !darwin && !linux && !windows

package robotgo

func platformPureGoInputBackend() pureGoInputBackend { return nil }

func closePureGoPlatformInput() error { return nil }
