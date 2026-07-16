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

func TestNativeWindowBackendReportsUnavailableDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	backend := nativeWindowBackend{}
	if nativeX11BackendCompiled() {
		original := GetXDisplayName()
		if err := SetXDisplayName(":65535"); err != nil {
			t.Fatalf("SetXDisplayName: %v", err)
		}
		t.Cleanup(func() {
			_ = SetXDisplayName(original)
			_ = CloseMainDisplayE()
		})
	}
	if capability := backend.Capability(); capability.Available {
		t.Fatalf("native window capability = %+v, want unavailable", capability)
	}
	if err := backend.Close(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Close error = %v, want ErrNotSupported", err)
	}
	if err := backend.Close(1); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Close(1) error = %v, want ErrNotSupported", err)
	}
}

func TestWaylandBuildCannotReportX11WindowStubSuccess(t *testing.T) {
	if nativeX11BackendCompiled() {
		t.Skip("native X11 backend is compiled")
	}
	t.Setenv("DISPLAY", ":123")
	t.Setenv("WAYLAND_DISPLAY", "")
	backend := nativeWindowBackend{}
	if capability := backend.Capability(); capability.Available {
		t.Fatalf("wayland-build X11 window capability = %+v, want unavailable", capability)
	}
	capabilities := GetLinuxCapabilities()
	if capability := capabilities.Window; capability.Available {
		t.Fatalf("public X11 window capability from wayland build = %+v, want unavailable", capability)
	}
	if capability := capabilities.Hook; capability.Available {
		t.Fatalf("public X11 hook capability from wayland build = %+v, want unavailable", capability)
	}
	handle := GetHandByPid(1, 1)
	for name, err := range map[string]error{
		"set active":    backend.SetActive(handle),
		"public active": SetActiveE(handle),
		"minimize":      backend.Minimize(1, true, true),
		"maximize":      backend.Maximize(1, true, true),
		"close":         backend.Close(1, 1),
		"public close":  CloseWindowE(1, 1),
		"active pid":    ActivePid(1, 1),
		"active pid C":  ActivePidC(1, 1),
	} {
		if !errors.Is(err, ErrNotSupported) {
			t.Errorf("%s error = %v, want ErrNotSupported", name, err)
		}
	}
	if _, err := backend.Title(1, 1); !errors.Is(err, ErrNotSupported) {
		t.Errorf("title error = %v, want ErrNotSupported", err)
	}
	if _, err := GetTitleE(1, 1); !errors.Is(err, ErrNotSupported) {
		t.Errorf("public title error = %v, want ErrNotSupported", err)
	}
	if x, y, width, height := GetBounds(1, 1); x != 0 || y != 0 || width != 0 || height != 0 {
		t.Errorf("wayland-build X11 GetBounds = (%d,%d %dx%d), want zero unsupported result", x, y, width, height)
	}
}
