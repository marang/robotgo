package robotgo

import "runtime"

// RuntimeImplementation identifies how the current RobotGo binary was built.
type RuntimeImplementation string

const (
	// RuntimeImplementationNativeCGO identifies a build with native CGO backends.
	RuntimeImplementationNativeCGO RuntimeImplementation = "native-cgo"
	// RuntimeImplementationPureGo identifies a build without CGO.
	RuntimeImplementationPureGo RuntimeImplementation = "pure-go"

	featureBackendNativeCGO          = "native-cgo"
	featureBackendPureGoPrefix       = "pure-go-"
	featureBackendPureGoCoreGraphics = "pure-go-coregraphics"
	featureBackendPureGoWindows      = "pure-go-windows"
	featureBackendPureGoX11          = "pure-go-x11"
	featureBackendGoProcess          = "go-process"
	featureBackendGoClipboard        = "go-clipboard"
	featureBackendPureGoProcess      = "pure-go-process"
	featureBackendPureGoClipboard    = "pure-go-clipboard"
)

// RuntimeBackendInfo describes the implementation compiled into the current
// binary. Use GetLinuxCapabilities for feature-specific Linux backend status.
type RuntimeBackendInfo struct {
	GOOS                string
	GOARCH              string
	CGOEnabled          bool
	BuildImplementation RuntimeImplementation
	DisplayServer       DisplayServer
}

// RuntimeCapabilities reports feature-level backend availability for the
// current platform and build. Availability may include bounded runtime probes;
// inspecting RuntimeBackendInfo never performs those probes.
type RuntimeCapabilities struct {
	Runtime       RuntimeBackendInfo
	Capture       FeatureCapability
	Bounds        FeatureCapability
	Keyboard      FeatureCapability
	Mouse         FeatureCapability
	RemoteDesktop FeatureCapability
	Window        FeatureCapability
	Process       FeatureCapability
	Clipboard     FeatureCapability
	Hook          FeatureCapability
	Events        FeatureCapability
}

// GetRuntimeBackendInfo reports build-time backend information without probing
// portals, compositors, permissions, or other external services.
func GetRuntimeBackendInfo() RuntimeBackendInfo {
	displayServer := DisplayServerUnknown
	if runtime.GOOS == "linux" {
		displayServer = DetectDisplayServer()
	}
	return RuntimeBackendInfo{
		GOOS:                runtime.GOOS,
		GOARCH:              runtime.GOARCH,
		CGOEnabled:          runtimeCGOEnabled,
		BuildImplementation: runtimeBuildImplementation,
		DisplayServer:       displayServer,
	}
}

// GetRuntimeCapabilities reports the feature backends available to the current
// binary. Unlike GetRuntimeBackendInfo, this function may perform bounded
// platform probes, but it never opens a consent dialog.
func GetRuntimeCapabilities() RuntimeCapabilities {
	return runtimeCapabilities()
}
