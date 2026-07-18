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
		return nil
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
