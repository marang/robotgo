//go:build cgo && linux

package robotgo

import (
	"errors"
	"testing"
)

func TestNativeLinuxWindowBackendDoesNotReportNoOpStateChangesAsSuccess(t *testing.T) {
	backend := nativeWindowBackend{}
	if err := backend.Minimize(1, true, false); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Minimize error = %v, want ErrNotSupported", err)
	}
	if err := backend.Maximize(1, true, false); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Maximize error = %v, want ErrNotSupported", err)
	}
}

func TestNativeWindowBackendRejectsInvalidTargetsBeforeCGO(t *testing.T) {
	backend := nativeWindowBackend{}
	var zero Handle
	if err := backend.SetActive(zero); err == nil {
		t.Fatal("SetActive accepted zero handle")
	}
	if err := backend.Close(0); err == nil {
		t.Fatal("Close accepted zero target")
	}
	if _, err := backend.Title(0); err == nil {
		t.Fatal("Title accepted zero target")
	}
}

func TestNativeWindowBackendReportsCloseFailure(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	backend := nativeWindowBackend{}
	if err := backend.Close(); !errors.Is(err, errWindowOperationFailed) {
		t.Fatalf("Close error = %v, want window operation failure", err)
	}
	if err := backend.Close(1); !errors.Is(err, errWindowOperationFailed) {
		t.Fatalf("Close(1) error = %v, want window operation failure", err)
	}
}
