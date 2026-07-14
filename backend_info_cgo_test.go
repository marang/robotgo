//go:build cgo

package robotgo

import "testing"

func TestRuntimeBackendInfoReportsCGOBuild(t *testing.T) {
	info := GetRuntimeBackendInfo()
	if !info.CGOEnabled || info.BuildImplementation != RuntimeImplementationNativeCGO {
		t.Fatalf("runtime backend info = %+v, want native CGO build", info)
	}
}
