//go:build !cgo && !linux

package robotgo

func pureGoWaylandBoundsCapability() FeatureCapability {
	return FeatureCapability{
		Reason: ErrNotSupported.Error(),
		Notes:  "Pure-Go Wayland output enumeration is Linux-specific",
	}
}
