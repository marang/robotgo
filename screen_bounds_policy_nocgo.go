//go:build !cgo

package robotgo

import (
	"fmt"
	"runtime"
)

func pureGoWaylandBoundsError() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	if selectedDisplayServer() == DisplayServerWayland {
		return fmt.Errorf(
			"%w: Pure-Go Wayland display bounds require a non-prompting native protocol backend",
			ErrNotSupported,
		)
	}
	if pureGoX11EnvironmentConflict() {
		return fmt.Errorf(
			"%w: %s selects Wayland; refusing X11 display bounds",
			ErrNotSupported,
			envXDGSessionType,
		)
	}
	return nil
}
