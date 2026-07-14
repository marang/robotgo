//go:build !cgo

package robotgo

import "testing"

func TestRuntimeBackendInfoReportsPureGoBuild(t *testing.T) {
	info := GetRuntimeBackendInfo()
	if info.CGOEnabled || info.BuildImplementation != RuntimeImplementationPureGo {
		t.Fatalf("runtime backend info = %+v, want Pure-Go build", info)
	}
}
