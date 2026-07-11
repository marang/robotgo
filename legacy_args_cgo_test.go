//go:build cgo

package robotgo

import "testing"

func TestErrorReturningWindowAPIsRejectInvalidStateType(t *testing.T) {
	if err := MinWindowE(0, "minimize"); err == nil {
		t.Fatal("MinWindowE accepted a non-bool state")
	}
	if err := MaxWindowE(0, "maximize"); err == nil {
		t.Fatal("MaxWindowE accepted a non-bool state")
	}
}
