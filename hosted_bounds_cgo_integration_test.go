//go:build cgo && linux && wayland && hostedboundsintegration

package robotgo

import "testing"

func TestHostedWaylandBoundsCGORuntime(t *testing.T) {
	assertHostedWaylandBoundsRuntime(t)
}
