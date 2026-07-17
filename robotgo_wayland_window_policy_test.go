//go:build cgo

package robotgo

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

func TestWaylandWindowOpsReturnNotSupported(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envSwaySock, "")
	t.Setenv(envHyprlandSignature, "")

	if err := SetActiveE(Handle{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("SetActiveE expected ErrNotSupported, got: %v", err)
	}
	if err := MinWindowE(1234); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("MinWindowE expected ErrNotSupported, got: %v", err)
	}
	if err := MaxWindowE(1234); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("MaxWindowE expected ErrNotSupported, got: %v", err)
	}
	if err := CloseWindowE(1234); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("CloseWindowE expected ErrNotSupported, got: %v", err)
	}
	if err := CloseWindowKill(1234); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("CloseWindowKill(pid) expected ErrNotSupported, got: %v", err)
	}
	if err := CloseWindowKill(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("CloseWindowKill() expected ErrNotSupported, got: %v", err)
	}
	if _, err := GetTitleE(1234); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("GetTitleE expected ErrNotSupported, got: %v", err)
	}
}

func TestWaylandWlrootsMinMaxWindowSupportedForActiveWindow(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdWlrCtl)
	t.Setenv(envPath, tmp)

	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "wayfire")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envSwaySock, "")
	t.Setenv(envHyprlandSignature, "")

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	var calls [][]string
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdWlrCtl {
			t.Fatalf("expected command %q, got %q", cmdWlrCtl, name)
		}
		calls = append(calls, append([]string(nil), args...))
		return []byte("ok"), nil
	}

	if err := MinWindowE(0); err != nil {
		t.Fatalf("MinWindowE(0) expected nil, got: %v", err)
	}
	if err := MaxWindowE(0); err != nil {
		t.Fatalf("MaxWindowE(0) expected nil, got: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 wlrctl calls, got %d", len(calls))
	}
	if len(calls[0]) != 3 || calls[0][0] != argWindow || calls[0][1] != argMinimize || calls[0][2] != argStateActive {
		t.Fatalf("unexpected minimize args: %#v", calls[0])
	}
	if len(calls[1]) != 3 || calls[1][0] != argWindow || calls[1][1] != argMaximize || calls[1][2] != argStateActive {
		t.Fatalf("unexpected maximize args: %#v", calls[1])
	}
}

func TestWaylandWlrootsCloseWindowStillUnsupported(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "wayfire")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envSwaySock, "")
	t.Setenv(envHyprlandSignature, "")

	if err := CloseWindowE(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("CloseWindowE expected ErrNotSupported, got: %v", err)
	}
	if err := CloseWindowKill(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("CloseWindowKill expected ErrNotSupported, got: %v", err)
	}
}

func TestWaylandHyprlandMaximizedPublicAPI(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envSwaySock, "")
	t.Setenv(envHyprlandSignature, "hyprland-test")

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	internal := hyprlandFullscreenNone
	client := hyprlandFullscreenNone
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected command %q, got %q", cmdHyprCtl, name)
		}
		if len(args) == 2 && args[0] == argActiveWindow && args[1] == argJSON {
			if internal == hyprlandFullscreenMaximized &&
				client == hyprlandFullscreenMaximized {
				return []byte(`{"fullscreen":1,"fullscreenClient":1}`), nil
			}
			if internal == hyprlandFullscreenNone && client == hyprlandFullscreenNone {
				return []byte(`{"fullscreen":0,"fullscreenClient":0}`), nil
			}
			t.Fatalf("unexpected test state internal=%d client=%d", internal, client)
		}
		if len(args) == 4 &&
			args[0] == argDispatch &&
			args[1] == argFullscreenState &&
			args[2] == args[3] {
			switch args[2] {
			case argHyprlandMaximized:
				internal, client = hyprlandFullscreenMaximized, hyprlandFullscreenMaximized
			case argHyprlandNone:
				internal, client = hyprlandFullscreenNone, hyprlandFullscreenNone
			default:
				t.Fatalf("unexpected maximize state: %q", args[2])
			}
			return []byte("ok"), nil
		}
		t.Fatalf("unexpected hyprctl args: %#v", args)
		return nil, nil
	}

	maximized, err := IsMaximizedE()
	if err != nil || maximized {
		t.Fatalf("IsMaximizedE() = %v, %v; want false, nil", maximized, err)
	}
	if err := MaxWindowE(0, true); err != nil {
		t.Fatalf("MaxWindowE(0, true) failed: %v", err)
	}
	maximized, err = IsMaximizedE()
	if err != nil || !maximized {
		t.Fatalf("IsMaximizedE() = %v, %v; want true, nil", maximized, err)
	}
	if err := MaxWindowE(0, false); err != nil {
		t.Fatalf("MaxWindowE(0, false) failed: %v", err)
	}
	maximized, err = IsMaximizedE()
	if err != nil || maximized {
		t.Fatalf("IsMaximizedE() = %v, %v after restore; want false, nil", maximized, err)
	}
}
