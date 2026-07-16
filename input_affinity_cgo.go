//go:build cgo

package robotgo

import (
	"errors"

	inputportal "github.com/marang/robotgo/input/portal"
)

type persistentInputBackend uint8

const (
	persistentInputBackendNative persistentInputBackend = iota + 1
	persistentInputBackendPortal
)

func portalInputFailureInvalidatesSession(err error) bool {
	return err != nil &&
		!errors.Is(err, ErrInputOwnership) &&
		!errors.Is(err, ErrNotSupported) &&
		!errors.Is(err, inputportal.ErrDeviceNotGranted)
}

func remoteDesktopInputGeneration() uint64 {
	remoteDesktopInputState.RLock()
	defer remoteDesktopInputState.RUnlock()
	return remoteDesktopInputState.generation
}
