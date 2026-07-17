//go:build linux && !cgo

package robotgo

import (
	"errors"
	"testing"

	"github.com/marang/robotgo/internal/windowbackend"
)

func TestPureGoX11WindowCapabilitySelection(t *testing.T) {
	t.Setenv(envDisplay, ":robotgo-test")
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envXDGSessionType, "")

	capability := pureGoWindowCapability()
	if !capability.Available || capability.Backend != featureBackendPureGoX11 {
		t.Fatalf("Pure-Go X11 window capability = %+v", capability)
	}
	if platformPureGoWindowBackend() == nil {
		t.Fatal("Pure-Go X11 window backend was not selected")
	}
}

func TestPureGoX11WindowCapabilityRejectsWaylandConflict(t *testing.T) {
	t.Setenv(envDisplay, ":robotgo-test")
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envXDGSessionType, "wayland")

	capability := pureGoWindowCapability()
	if capability.Available || capability.Backend != featureBackendPureGoX11 {
		t.Fatalf("conflicting Pure-Go X11 window capability = %+v", capability)
	}
	if platformPureGoWindowBackend() != nil {
		t.Fatal("Pure-Go X11 window backend was selected during a Wayland conflict")
	}
	if err := ActivePid(1); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("ActivePid() conflict error = %v, want ErrNotSupported", err)
	}
}

func TestTranslatePureGoWindowUnsupportedError(t *testing.T) {
	cause := errors.Join(
		windowbackend.ErrOperation,
		windowbackend.ErrUnsupported,
	)
	err := translatePureGoWindowError(cause)
	if !errors.Is(err, ErrNotSupported) ||
		!errors.Is(err, windowbackend.ErrOperation) ||
		!errors.Is(err, windowbackend.ErrUnsupported) {
		t.Fatalf("translated error = %v, want all public and internal causes", err)
	}
}
