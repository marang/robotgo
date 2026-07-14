package robotgo

import "runtime"

// RuntimeImplementation identifies how the current RobotGo binary was built.
type RuntimeImplementation string

const (
	// RuntimeImplementationNativeCGO identifies a build with native CGO backends.
	RuntimeImplementationNativeCGO RuntimeImplementation = "native-cgo"
	// RuntimeImplementationPureGo identifies a build without CGO.
	RuntimeImplementationPureGo RuntimeImplementation = "pure-go"
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
