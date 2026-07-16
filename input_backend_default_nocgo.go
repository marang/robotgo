//go:build !cgo && !linux

package robotgo

func platformPureGoInputBackend() pureGoInputBackend { return nil }

func closePureGoPlatformInput() error { return nil }
