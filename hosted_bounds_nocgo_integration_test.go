//go:build !cgo && linux && hostedboundsintegration

package robotgo

import "testing"

func TestHostedWaylandBoundsPureGoRuntime(t *testing.T) {
	assertHostedWaylandBoundsRuntime(t)
}
