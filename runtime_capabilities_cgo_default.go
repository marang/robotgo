//go:build cgo && !darwin

package robotgo

func nativePlatformCaptureCapabilities() (FeatureCapability, FeatureCapability) {
	native := FeatureCapability{
		Available: true,
		Backend:   featureBackendNativeCGO,
		Reason:    "native CGO backend is compiled for this platform",
		Notes:     "runtime permissions are validated when an operation starts",
	}
	return native, native
}
