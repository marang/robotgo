//go:build !linux

package robotgo

func accessibilityCapability() FeatureCapability {
	return FeatureCapability{
		Reason: ErrNotSupported.Error(),
		Notes:  "native semantic accessibility inspection is not implemented on this platform",
	}
}
