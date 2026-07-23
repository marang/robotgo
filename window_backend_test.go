//go:build cgo

package robotgo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeStubCommand(t *testing.T, dir, name string) {
	t.Helper()
	contents := []byte("#!/bin/sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		name += ".bat"
		contents = []byte("@exit /b 0\r\n")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		t.Fatalf("write stub command %q: %v", name, err)
	}
}

func TestDetectWaylandCompositor(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	type tc struct {
		name           string
		wayland        string
		display        string
		desktop        string
		session        string
		swaySock       string
		hyprSig        string
		wantCompositor string
	}

	tests := []tc{
		{
			name:           "wlroots via sway sock",
			wayland:        testWaylandDisplay,
			desktop:        "GNOME",
			swaySock:       "/tmp/sway.sock",
			wantCompositor: compositorSway,
		},
		{
			name:           "wlroots via sway desktop",
			wayland:        testWaylandDisplay,
			desktop:        "sway",
			wantCompositor: compositorSway,
		},
		{
			name:           "mutter via gnome",
			wayland:        testWaylandDisplay,
			desktop:        "GNOME",
			wantCompositor: compositorMutter,
		},
		{
			name:           "kwin via kde",
			wayland:        testWaylandDisplay,
			desktop:        "KDE",
			wantCompositor: compositorKWin,
		},
		{
			name:           "kwin via plasma session",
			wayland:        testWaylandDisplay,
			session:        "plasma",
			wantCompositor: compositorKWin,
		},
		{
			name:           "hyprland via env signature",
			wayland:        testWaylandDisplay,
			hyprSig:        "hyprland-test",
			wantCompositor: compositorHyprland,
		},
		{
			name:           "wayfire compositor",
			wayland:        testWaylandDisplay,
			desktop:        "wayfire",
			wantCompositor: compositorWayfire,
		},
		{
			name:           "river compositor",
			wayland:        testWaylandDisplay,
			desktop:        "river",
			wantCompositor: compositorRiver,
		},
		{
			name:           "labwc compositor",
			wayland:        testWaylandDisplay,
			desktop:        "labwc",
			wantCompositor: compositorLabwc,
		},
		{
			name:           "dwl compositor",
			wayland:        testWaylandDisplay,
			desktop:        "dwl",
			wantCompositor: compositorDwl,
		},
		{
			name:           "gamescope compositor",
			wayland:        testWaylandDisplay,
			desktop:        "gamescope",
			wantCompositor: compositorGamescope,
		},
		{
			name:           "unknown wayland compositor",
			wayland:        testWaylandDisplay,
			desktop:        "foo",
			session:        "bar",
			wantCompositor: compositorUnknown,
		},
		{
			name:           "not wayland session",
			display:        ":0",
			wantCompositor: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envWaylandDisplay, tt.wayland)
			t.Setenv(envDisplay, tt.display)
			t.Setenv(envDesktop, tt.desktop)
			t.Setenv(envSessionDesktop, tt.session)
			t.Setenv(envSwaySock, tt.swaySock)
			t.Setenv(envHyprlandSignature, tt.hyprSig)

			got := detectWaylandCompositor()
			if got != tt.wantCompositor {
				t.Fatalf("detectWaylandCompositor() = %q, want %q", got, tt.wantCompositor)
			}
		})
	}
}

func TestResolveWindowBackendWaylandNameIncludesCompositor(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	t.Setenv(envWaylandDisplay, testWaylandDisplay)
	t.Setenv(envDisplay, "")
	t.Setenv(envDesktop, "GNOME")
	t.Setenv(envSessionDesktop, "")
	t.Setenv(envSwaySock, "")
	t.Setenv(envHyprlandSignature, "")

	b := resolveWindowBackend()
	if b.Name() != windowBackendCore+"/"+compositorMutter {
		t.Fatalf("resolveWindowBackend().Name() = %q, want %q", b.Name(), windowBackendCore+"/"+compositorMutter)
	}
}

func TestResolveWindowBackendPriority(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}

	t.Run("sway overrides family", func(t *testing.T) {
		t.Setenv(envWaylandDisplay, testWaylandDisplay)
		t.Setenv(envDisplay, "")
		t.Setenv(envDesktop, "GNOME")
		t.Setenv(envSessionDesktop, "")
		t.Setenv(envSwaySock, "/tmp/sway.sock")
		t.Setenv(envHyprlandSignature, "")

		b := resolveWindowBackend()
		if b.Name() != windowBackendSway {
			t.Fatalf("resolveWindowBackend().Name() = %q, want %q", b.Name(), windowBackendSway)
		}
	})

	t.Run("hyprland overrides wlroots family", func(t *testing.T) {
		t.Setenv(envWaylandDisplay, testWaylandDisplay)
		t.Setenv(envDisplay, "")
		t.Setenv(envDesktop, "wayfire")
		t.Setenv(envSessionDesktop, "")
		t.Setenv(envSwaySock, "")
		t.Setenv(envHyprlandSignature, "hyprland-test")

		b := resolveWindowBackend()
		if b.Name() != windowBackendHypr {
			t.Fatalf("resolveWindowBackend().Name() = %q, want %q", b.Name(), windowBackendHypr)
		}
	})

	t.Run("wlroots family backend selected", func(t *testing.T) {
		t.Setenv(envWaylandDisplay, testWaylandDisplay)
		t.Setenv(envDisplay, "")
		t.Setenv(envDesktop, "wayfire")
		t.Setenv(envSessionDesktop, "")
		t.Setenv(envSwaySock, "")
		t.Setenv(envHyprlandSignature, "")

		b := resolveWindowBackend()
		if b.Name() != windowBackendWlroots {
			t.Fatalf("resolveWindowBackend().Name() = %q, want %q", b.Name(), windowBackendWlroots)
		}
	})
}

func TestWaylandCompositorFamily(t *testing.T) {
	tests := []struct {
		name       string
		compositor string
		wantFamily string
	}{
		{name: compositorSway, compositor: compositorSway, wantFamily: compositorWlroots},
		{name: compositorHyprland, compositor: compositorHyprland, wantFamily: compositorWlroots},
		{name: compositorWayfire, compositor: compositorWayfire, wantFamily: compositorWlroots},
		{name: compositorRiver, compositor: compositorRiver, wantFamily: compositorWlroots},
		{name: compositorLabwc, compositor: compositorLabwc, wantFamily: compositorWlroots},
		{name: compositorDwl, compositor: compositorDwl, wantFamily: compositorWlroots},
		{name: compositorGamescope, compositor: compositorGamescope, wantFamily: compositorWlroots},
		{name: compositorMutter, compositor: compositorMutter, wantFamily: compositorMutter},
		{name: compositorKWin, compositor: compositorKWin, wantFamily: compositorKWin},
		{name: compositorUnknown, compositor: compositorUnknown, wantFamily: compositorUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := waylandCompositorFamily(tt.compositor)
			if got != tt.wantFamily {
				t.Fatalf("waylandCompositorFamily(%q) = %q, want %q", tt.compositor, got, tt.wantFamily)
			}
		})
	}
}

func TestSpecificWindowBackendForCompositor(t *testing.T) {
	cases := []struct {
		compositor string
		wantName   string
		wantOK     bool
	}{
		{compositor: compositorSway, wantName: windowBackendSway, wantOK: true},
		{compositor: compositorHyprland, wantName: windowBackendHypr, wantOK: true},
		{compositor: compositorWayfire, wantName: "", wantOK: false},
	}

	for _, tt := range cases {
		b, ok := specificWindowBackendForCompositor(tt.compositor)
		if ok != tt.wantOK {
			t.Fatalf("specificWindowBackendForCompositor(%q) ok=%v want %v", tt.compositor, ok, tt.wantOK)
		}
		if ok && b.Name() != tt.wantName {
			t.Fatalf("specificWindowBackendForCompositor(%q).Name()=%q want %q", tt.compositor, b.Name(), tt.wantName)
		}
	}
}

func TestSwayWindowBackendTitle(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdSwayMsg {
			t.Fatalf("expected %q, got %q", cmdSwayMsg, name)
		}
		return []byte(`{"focused":false,"name":"","nodes":[{"type":"con","focused":true,"name":"Terminal","nodes":[],"floating_nodes":[]}],"floating_nodes":[]}`), nil
	}

	b := newSwayWindowBackend()
	title, err := b.Title()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Terminal" {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestHyprlandWindowBackendTitle(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		return []byte(`{"title":"Editor"}`), nil
	}

	b := newHyprlandWindowBackend()
	title, err := b.Title()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Editor" {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestHyprlandWindowBackendMaximized(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	tests := []struct {
		name string
		json string
		want bool
	}{
		{name: "normal", json: `{"fullscreen":0}`, want: false},
		{name: "maximized", json: `{"fullscreen":1}`, want: true},
		{name: "fullscreen", json: `{"fullscreen":2}`, want: false},
		{name: "legacy combined state", json: `{"fullscreen":3}`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				_ = ctx
				if name != cmdHyprCtl {
					t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
				}
				if len(args) != 2 || args[0] != argActiveWindow || args[1] != argJSON {
					t.Fatalf("unexpected args: %#v", args)
				}
				return []byte(tt.json), nil
			}

			got, err := newHyprlandWindowBackend().Maximized()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Maximized() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHyprlandWindowBackendMaximizedRejectsUnreliableState(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	tests := []struct {
		name string
		json string
	}{
		{name: "missing state", json: `{"title":"Editor"}`},
		{name: "invalid state", json: `{"fullscreen":4}`},
		{name: "malformed response", json: `{"fullscreen":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runWindowCommand = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(tt.json), nil
			}

			_, err := newHyprlandWindowBackend().Maximized()
			if !errors.Is(err, errWindowStateUnavailable) {
				t.Fatalf("Maximized() error = %v, want errWindowStateUnavailable", err)
			}
		})
	}
}

func TestHyprlandWindowBackendMaximizedRequiresHyprctl(t *testing.T) {
	t.Setenv(envPath, t.TempDir())

	_, err := newHyprlandWindowBackend().Maximized()
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Maximized() error = %v, want ErrNotSupported", err)
	}
}

func TestCompositorSpecificBackendTitleByPIDUnsupported(t *testing.T) {
	for _, b := range []windowBackend{newSwayWindowBackend(), newHyprlandWindowBackend()} {
		_, err := b.Title(1234)
		if !errors.Is(err, ErrNotSupported) {
			t.Fatalf("%s backend expected ErrNotSupported for pid title lookup, got: %v", b.Name(), err)
		}
	}
}

func TestSwayWindowBackendCloseActive(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdSwayMsg {
			t.Fatalf("expected %q, got %q", cmdSwayMsg, name)
		}
		if len(args) != 1 || args[0] != argKill {
			t.Fatalf("unexpected args: %#v", args)
		}
		return []byte("ok"), nil
	}

	if err := newSwayWindowBackend().Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSwayWindowBackendMinMaxActiveViaWlrctl(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdWlrCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	var calls [][]string
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdWlrCtl {
			t.Fatalf("expected %q, got %q", cmdWlrCtl, name)
		}
		calls = append(calls, append([]string(nil), args...))
		return []byte("ok"), nil
	}

	b := newSwayWindowBackend()
	if err := b.Minimize(0, true, false); err != nil {
		t.Fatalf("unexpected minimize error: %v", err)
	}
	if err := b.Maximize(0, true, false); err != nil {
		t.Fatalf("unexpected maximize error: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if len(calls[0]) != 3 || calls[0][0] != argWindow || calls[0][1] != argMinimize || calls[0][2] != argStateActive {
		t.Fatalf("unexpected minimize args: %#v", calls[0])
	}
	if len(calls[1]) != 3 || calls[1][0] != argWindow || calls[1][1] != argMaximize || calls[1][2] != argStateActive {
		t.Fatalf("unexpected maximize args: %#v", calls[1])
	}
}

func TestHyprlandWindowBackendCloseActive(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	var calls [][]string
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		calls = append(calls, append([]string(nil), args...))
		if len(args) == 2 && args[0] == argStatus && args[1] == argJSON {
			return []byte(`{"configProvider":"hyprlang"}`), nil
		}
		return []byte("ok"), nil
	}

	if err := newHyprlandWindowBackend().Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{
		{argStatus, argJSON},
		{argDispatch, argKillActive},
	}
	assertWindowCommandCalls(t, calls, want)
}

func TestHyprlandWindowBackendCloseActiveLua(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	var calls [][]string
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		calls = append(calls, append([]string(nil), args...))
		if len(args) == 2 && args[0] == argStatus && args[1] == argJSON {
			return []byte(`{"configProvider":"lua"}`), nil
		}
		return []byte("ok"), nil
	}

	if err := newHyprlandWindowBackend().Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{
		{argStatus, argJSON},
		{argDispatch, hyprlandLuaCloseActive},
	}
	assertWindowCommandCalls(t, calls, want)
}

func TestHyprlandWindowBackendCloseFailsClosedOnStatusError(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	transportErr := errors.New("status transport failed")
	tests := []struct {
		name       string
		output     string
		commandErr error
		wantCause  error
	}{
		{
			name:       "transport failure",
			commandErr: transportErr,
			wantCause:  transportErr,
		},
		{
			name:   "malformed response",
			output: `{"configProvider":`,
		},
		{
			name:   "unknown provider",
			output: `{"configProvider":"future"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				_ = ctx
				calls++
				if name != cmdHyprCtl {
					t.Fatalf("command = %q, want %q", name, cmdHyprCtl)
				}
				if len(args) != 2 || args[0] != argStatus || args[1] != argJSON {
					t.Fatalf("status args = %#v", args)
				}
				return []byte(tt.output), tt.commandErr
			}

			err := newHyprlandWindowBackend().Close()
			if !errors.Is(err, errWindowOperationFailed) {
				t.Fatalf("Close() error = %v, want errWindowOperationFailed", err)
			}
			if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
				t.Fatalf("Close() error = %v, want cause %v", err, tt.wantCause)
			}
			if calls != 1 {
				t.Fatalf("Close() issued %d commands after status failure, want 1", calls)
			}
		})
	}
}

func TestHyprlandWindowBackendMinimizeActiveViaWlrctl(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdWlrCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	var calls [][]string
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdWlrCtl {
			t.Fatalf("expected %q, got %q", cmdWlrCtl, name)
		}
		calls = append(calls, append([]string(nil), args...))
		return []byte("ok"), nil
	}

	b := newHyprlandWindowBackend()
	if err := b.Minimize(0, true, false); err != nil {
		t.Fatalf("unexpected minimize error: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if len(calls[0]) != 3 || calls[0][0] != argWindow || calls[0][1] != argMinimize || calls[0][2] != argStateActive {
		t.Fatalf("unexpected minimize args: %#v", calls[0])
	}
}

func TestHyprlandWindowBackendMaximizeSetAndRestore(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	var calls [][]string
	internal := hyprlandFullscreenNone
	client := hyprlandFullscreenNone
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		if len(args) == 2 && args[0] == argActiveWindow && args[1] == argJSON {
			switch {
			case internal == hyprlandFullscreenNone && client == hyprlandFullscreenNone:
				return []byte(`{"fullscreen":0,"fullscreenClient":0}`), nil
			case internal == hyprlandFullscreenMaximized && client == hyprlandFullscreenMaximized:
				return []byte(`{"fullscreen":1,"fullscreenClient":1}`), nil
			default:
				t.Fatalf("unexpected test state internal=%d client=%d", internal, client)
			}
		}
		if len(args) == 2 && args[0] == argStatus && args[1] == argJSON {
			return []byte(`{"configProvider":"hyprlang"}`), nil
		}
		calls = append(calls, append([]string(nil), args...))
		if len(args) == 4 && args[0] == argDispatch && args[1] == argFullscreenState {
			switch args[2] {
			case argHyprlandNone:
				internal, client = hyprlandFullscreenNone, hyprlandFullscreenNone
			case argHyprlandMaximized:
				internal, client = hyprlandFullscreenMaximized, hyprlandFullscreenMaximized
			}
		}
		return []byte("ok"), nil
	}

	b := newHyprlandWindowBackend()
	if err := b.Maximize(0, true, false); err != nil {
		t.Fatalf("maximize failed: %v", err)
	}
	if err := b.Maximize(0, false, false); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	want := [][]string{
		{argDispatch, argFullscreenState, argHyprlandMaximized, argHyprlandMaximized},
		{argDispatch, argFullscreenState, argHyprlandNone, argHyprlandNone},
	}
	assertWindowCommandCalls(t, calls, want)
}

func TestHyprlandWindowBackendMaximizeSetAndRestoreLua(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	var calls [][]string
	internal := hyprlandFullscreenNone
	client := hyprlandFullscreenNone
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		if len(args) == 2 && args[0] == argActiveWindow && args[1] == argJSON {
			return []byte(fmt.Sprintf(
				`{"fullscreen":%d,"fullscreenClient":%d}`,
				internal,
				client,
			)), nil
		}
		if len(args) == 2 && args[0] == argStatus && args[1] == argJSON {
			return []byte(`{"configProvider":"lua"}`), nil
		}
		calls = append(calls, append([]string(nil), args...))
		switch {
		case len(args) == 2 && args[0] == argDispatch && args[1] == hyprlandLuaMaximizeActive:
			internal, client = hyprlandFullscreenMaximized, hyprlandFullscreenMaximized
		case len(args) == 2 && args[0] == argDispatch && args[1] == hyprlandLuaRestoreActive:
			internal, client = hyprlandFullscreenNone, hyprlandFullscreenNone
		default:
			t.Fatalf("unexpected hyprctl args: %#v", args)
		}
		return []byte("ok"), nil
	}

	b := newHyprlandWindowBackend()
	if err := b.Maximize(0, true, false); err != nil {
		t.Fatalf("maximize failed: %v", err)
	}
	if err := b.Maximize(0, false, false); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	want := [][]string{
		{argDispatch, hyprlandLuaMaximizeActive},
		{argDispatch, hyprlandLuaRestoreActive},
	}
	assertWindowCommandCalls(t, calls, want)
}

func TestHyprlandWindowBackendMaximizeAvoidsLegacyToggle(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	dispatches := 0
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		if len(args) == 2 && args[0] == argActiveWindow && args[1] == argJSON {
			return []byte(`{"fullscreen":1,"fullscreenClient":1}`), nil
		}
		dispatches++
		return []byte("ok"), nil
	}

	b := newHyprlandWindowBackend()
	if err := b.Maximize(0, true, false); err != nil {
		t.Fatalf("maximize already-maximized window: %v", err)
	}
	if dispatches != 0 {
		t.Fatalf("already-maximized state dispatched %d commands; older Hyprland would toggle it off", dispatches)
	}
}

func TestHyprlandWindowBackendMaximizeRejectsUnreliableState(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	tests := []struct {
		name string
		json string
	}{
		{name: "missing client state", json: `{"fullscreen":0}`},
		{name: "invalid internal state", json: `{"fullscreen":4,"fullscreenClient":0}`},
		{name: "invalid client state", json: `{"fullscreen":0,"fullscreenClient":-2}`},
		{name: "malformed response", json: `{"fullscreen":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runWindowCommand = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(tt.json), nil
			}

			err := newHyprlandWindowBackend().Maximize(0, true, false)
			if !errors.Is(err, errWindowStateUnavailable) {
				t.Fatalf("Maximize() error = %v, want errWindowStateUnavailable", err)
			}
		})
	}
}

func TestHyprlandWindowBackendMaximizePreservesCommandError(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	wantErr := errors.New("dispatch failed")
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		if len(args) == 2 && args[0] == argActiveWindow && args[1] == argJSON {
			return []byte(`{"fullscreen":0,"fullscreenClient":0}`), nil
		}
		if len(args) == 2 && args[0] == argStatus && args[1] == argJSON {
			return []byte(`{"configProvider":"hyprlang"}`), nil
		}
		return nil, wantErr
	}

	err := newHyprlandWindowBackend().Maximize(0, true, false)
	if !errors.Is(err, errWindowOperationFailed) {
		t.Fatalf("Maximize() error = %v, want errWindowOperationFailed", err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Maximize() error = %v, want wrapped command error", err)
	}
}

func TestHyprlandDispatchArgsCompatibility(t *testing.T) {
	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	statusErr := errors.New("status unsupported")
	tests := []struct {
		name       string
		output     string
		commandErr error
		want       []string
		wantErr    bool
	}{
		{
			name:   "lua",
			output: `{"configProvider":"lua"}`,
			want:   []string{argDispatch, hyprlandLuaCloseActive},
		},
		{
			name:   "hyprlang",
			output: `{"configProvider":"hyprlang"}`,
			want:   []string{argDispatch, argKillActive},
		},
		{
			name:   "pre-status hyprland",
			output: hyprlandStatusUnsupported,
			want:   []string{argDispatch, argKillActive},
		},
		{
			name:       "status command failure",
			commandErr: statusErr,
			wantErr:    true,
		},
		{
			name:    "malformed successful status",
			output:  `{"configProvider":`,
			wantErr: true,
		},
		{
			name:    "missing provider",
			output:  `{}`,
			wantErr: true,
		},
		{
			name:    "unknown provider",
			output:  `{"configProvider":"future"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				_ = ctx
				if name != cmdHyprCtl {
					t.Fatalf("command = %q, want %q", name, cmdHyprCtl)
				}
				if len(args) != 2 || args[0] != argStatus || args[1] != argJSON {
					t.Fatalf("status args = %#v", args)
				}
				return []byte(tt.output), tt.commandErr
			}

			got, err := resolveHyprlandDispatchArgs(
				[]string{argKillActive},
				hyprlandLuaCloseActive,
			)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveHyprlandDispatchArgs() = %#v, nil; want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveHyprlandDispatchArgs() error = %v", err)
			}
			assertWindowCommandCalls(t, [][]string{got}, [][]string{tt.want})
		})
	}
}

func assertWindowCommandCalls(t *testing.T, got, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("call %d = %#v, want %#v", i, got[i], want[i])
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("call %d = %#v, want %#v", i, got[i], want[i])
			}
		}
	}
}

func TestHyprlandWindowBackendRestorePreservesFullscreen(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdHyprCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	dispatches := 0
	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdHyprCtl {
			t.Fatalf("expected %q, got %q", cmdHyprCtl, name)
		}
		if len(args) == 2 && args[0] == argActiveWindow && args[1] == argJSON {
			return []byte(`{"fullscreen":2,"fullscreenClient":2}`), nil
		}
		dispatches++
		return []byte("ok"), nil
	}

	if err := newHyprlandWindowBackend().Maximize(0, false, false); err != nil {
		t.Fatalf("restore fullscreen window: %v", err)
	}
	if dispatches != 0 {
		t.Fatalf("restore on fullscreen state dispatched %d commands; want no-op", dispatches)
	}
}

func TestNonHyprlandWindowBackendsRejectMaximizedQuery(t *testing.T) {
	backends := []windowBackend{
		newSwayWindowBackend(),
		newWlrootsGenericWindowBackend(),
		waylandCoreWindowBackend{compositor: compositorMutter},
	}
	for _, backend := range backends {
		_, err := backend.Maximized()
		if !errors.Is(err, ErrNotSupported) {
			t.Fatalf("%s Maximized() error = %v, want ErrNotSupported", backend.Name(), err)
		}
	}
}

func TestCompositorSpecificBackendCloseByPIDUnsupported(t *testing.T) {
	for _, b := range []windowBackend{newSwayWindowBackend(), newHyprlandWindowBackend()} {
		err := b.Close(1234)
		if !errors.Is(err, ErrNotSupported) {
			t.Fatalf("%s backend expected ErrNotSupported for pid close, got: %v", b.Name(), err)
		}
	}
}

func TestWlrootsGenericWindowBackendMinimizeActive(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdWlrCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdWlrCtl {
			t.Fatalf("expected %q, got %q", cmdWlrCtl, name)
		}
		if len(args) != 3 || args[0] != argWindow || args[1] != argMinimize || args[2] != argStateActive {
			t.Fatalf("unexpected args: %#v", args)
		}
		return []byte("ok"), nil
	}

	if err := newWlrootsGenericWindowBackend().Minimize(0, true, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWlrootsGenericWindowBackendMaximizeActive(t *testing.T) {
	tmp := t.TempDir()
	writeStubCommand(t, tmp, cmdWlrCtl)
	t.Setenv(envPath, tmp)

	old := runWindowCommand
	t.Cleanup(func() { runWindowCommand = old })

	runWindowCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if name != cmdWlrCtl {
			t.Fatalf("expected %q, got %q", cmdWlrCtl, name)
		}
		if len(args) != 3 || args[0] != argWindow || args[1] != argMaximize || args[2] != argStateActive {
			t.Fatalf("unexpected args: %#v", args)
		}
		return []byte("ok"), nil
	}

	if err := newWlrootsGenericWindowBackend().Maximize(0, true, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWlrootsGenericWindowBackendUnsupportedVariants(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(envPath, tmp)

	b := newWlrootsGenericWindowBackend()
	err := b.Close()
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("wlroots backend expected ErrNotSupported for close, got: %v", err)
	}

	err = b.Minimize(1234, true, false)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("wlroots backend expected ErrNotSupported for pid minimize, got: %v", err)
	}

	err = b.Minimize(0, true, false)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("wlroots backend expected ErrNotSupported when wlrctl missing, got: %v", err)
	}

	err = b.Maximize(1234, true, false)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("wlroots backend expected ErrNotSupported for pid maximize, got: %v", err)
	}

	err = b.Minimize(0, false, false)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("wlroots backend expected ErrNotSupported for unminimize, got: %v", err)
	}

	err = b.Maximize(0, false, false)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("wlroots backend expected ErrNotSupported for unmaximize, got: %v", err)
	}

	err = b.Close(1234)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("wlroots backend expected ErrNotSupported for pid close, got: %v", err)
	}
}

func TestCompositorSpecificBackendCapabilityCommandAvailability(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(envPath, tmp)

	swayCap := newSwayWindowBackend().Capability()
	if swayCap.Available {
		t.Fatalf("expected sway capability unavailable without swaymsg in PATH")
	}
	hyprCap := newHyprlandWindowBackend().Capability()
	if hyprCap.Available {
		t.Fatalf("expected hyprland capability unavailable without hyprctl in PATH")
	}

	wlrootsCap := newWlrootsGenericWindowBackend().Capability()
	if wlrootsCap.Available {
		t.Fatalf("expected wlroots capability unavailable without wlrctl in PATH")
	}
}
