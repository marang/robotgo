//go:build !cgo && !linux && !windows

package robotgo

func platformPureGoInputBackend() pureGoInputBackend { return nil }

func closePureGoPlatformInput() error { return nil }
