//go:build cgo && linux

package robotgo

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	inputportal "github.com/marang/robotgo/input/portal"
)

func TestHighLevelInputFallsBackToActiveRemoteDesktopSession(t *testing.T) {
	t.Setenv(envWaylandDisplay, "robotgo-missing-wayland")
	t.Setenv(envDisplay, "")
	stubCaptureCapabilityProbes(t, false, false)
	oldMouseSleep, oldKeySleep := MouseSleep, KeySleep
	MouseSleep, KeySleep = 23, 0
	t.Cleanup(func() { MouseSleep, KeySleep = oldMouseSleep, oldKeySleep })
	delays := installRemoteDesktopMouseDelayRecorder(t)

	session := installFakeHighLevelPortalSession(t, inputportal.DeviceKeyboard|inputportal.DevicePointer)
	session.streams = []inputportal.Stream{{
		NodeID: 77, Position: inputportal.Point{X: -1920, Y: 0}, HasPosition: true,
		Size: inputportal.Size{Width: 1920, Height: 1080}, HasSize: true,
	}}
	if err := MoveE(-1900, 100); err != nil {
		t.Fatalf("MoveE absolute portal fallback error: %v", err)
	}
	if err := MoveRelativeE(4, -3); err != nil {
		t.Fatalf("MoveRelativeE error: %v", err)
	}
	if err := ClickE("left"); err != nil {
		t.Fatalf("ClickE error: %v", err)
	}
	if err := Toggle("right"); err != nil {
		t.Fatalf("Toggle error: %v", err)
	}
	if err := Toggle("right", "up"); err != nil {
		t.Fatalf("Toggle release error: %v", err)
	}
	if err := ScrollE(2, -3, 7); err != nil {
		t.Fatalf("ScrollE error: %v", err)
	}
	if err := KeyTap("a"); err != nil {
		t.Fatalf("KeyTap error: %v", err)
	}
	if err := KeyTap("A", []string{"ctrl"}); err != nil {
		t.Fatalf("modified uppercase KeyTap error: %v", err)
	}
	if err := TypeStrE("A"); err != nil {
		t.Fatalf("TypeStrE error: %v", err)
	}
	if err := UnicodeTypeE('€'); err != nil {
		t.Fatalf("UnicodeTypeE error: %v", err)
	}
	capabilities := GetLinuxCapabilities()
	if capabilities.Keyboard.Backend != "portal-remote-desktop" || !capabilities.Keyboard.Available {
		t.Fatalf("keyboard capability did not select active portal session: %+v", capabilities.Keyboard)
	}
	if capabilities.Mouse.Backend != "portal-remote-desktop" || !capabilities.Mouse.Available {
		t.Fatalf("mouse capability did not select active portal session: %+v", capabilities.Mouse)
	}
	assertRemoteDesktopMouseDelays(t, *delays, []int{23, 23, 23, 30})

	events, _ := session.snapshot()
	wantPrefixes := []string{
		"absolute:77:20:100",
		"motion:4:-3",
		"button:272:true",
		"button:272:false",
		"button:273:true",
		"button:273:false",
		"axis:1:2",
		"axis:0:3",
		"keysym:97:true",
		"keysym:97:false",
		"keysym:65507:true",
		"keysym:65505:true",
		"keysym:97:true",
		"keysym:97:false",
		"keysym:65505:false",
		"keysym:65507:false",
		"keysym:65:true",
		"keysym:65:false",
		"keysym:16785580:true",
		"keysym:16785580:false",
	}
	if len(events) != len(wantPrefixes) {
		t.Fatalf("events = %#v, want %d events", events, len(wantPrefixes))
	}
	for i, want := range wantPrefixes {
		if events[i] != want {
			t.Fatalf("event %d = %q, want %q (all=%#v)", i, events[i], want, events)
		}
	}
}

func TestPortalDevicePreflightPreservesOtherGrants(t *testing.T) {
	t.Setenv(envWaylandDisplay, "robotgo-device-preflight")
	t.Setenv(envDisplay, "")

	t.Run("missing keyboard grant preserves pointer", func(t *testing.T) {
		session := installFakeHighLevelPortalSession(t, inputportal.DevicePointer)
		if err := KeyDown("a"); !errors.Is(err, inputportal.ErrDeviceNotGranted) {
			t.Fatalf("KeyDown without keyboard grant error = %v, want ErrDeviceNotGranted", err)
		}
		if _, closed := session.snapshot(); closed != 0 {
			t.Fatalf("keyboard preflight closed pointer-only session %d times", closed)
		}
		if err := MoveRelativeE(3, -2); err != nil {
			t.Fatalf("pointer grant after keyboard preflight: %v", err)
		}
		if events, _ := session.snapshot(); !reflect.DeepEqual(events, []string{"motion:3:-2"}) {
			t.Fatalf("pointer events after keyboard preflight = %v", events)
		}
	})

	t.Run("missing pointer grant preserves keyboard", func(t *testing.T) {
		session := installFakeHighLevelPortalSession(t, inputportal.DeviceKeyboard)
		if err := MouseDown("left"); !errors.Is(err, inputportal.ErrDeviceNotGranted) {
			t.Fatalf("MouseDown without pointer grant error = %v, want ErrDeviceNotGranted", err)
		}
		if _, closed := session.snapshot(); closed != 0 {
			t.Fatalf("pointer preflight closed keyboard-only session %d times", closed)
		}
		if err := KeyTap("a"); err != nil {
			t.Fatalf("keyboard grant after pointer preflight: %v", err)
		}
		if events, _ := session.snapshot(); !reflect.DeepEqual(events, []string{
			"keysym:97:true", "keysym:97:false",
		}) {
			t.Fatalf("keyboard events after pointer preflight = %v", events)
		}
	})
}

func TestRemoteDesktopFallbackRequiresZeroNativeMutation(t *testing.T) {
	injectionErr := errors.New("native input failed after mutation")
	tests := []struct {
		name   string
		server DisplayServer
		ready  bool
		err    error
		want   bool
	}{
		{name: "readiness failure", server: DisplayServerWayland, ready: false, err: injectionErr, want: true},
		{name: "zero-mutation unsupported", server: DisplayServerWayland, ready: true, err: ErrNotSupported, want: true},
		{name: "post-readiness injection failure", server: DisplayServerWayland, ready: true, err: injectionErr, want: false},
		{name: "ownership conflict", server: DisplayServerWayland, ready: true, err: ErrInputOwnership, want: false},
		{name: "X11 never uses portal", server: DisplayServerX11, ready: false, err: ErrNotSupported, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldTryRemoteDesktopAfterNative(test.server, test.ready, test.err); got != test.want {
				t.Fatalf("fallback decision = %t, want %t", got, test.want)
			}
		})
	}
}

func TestAbsolutePortalFallbackPreservesRequestedCoordinates(t *testing.T) {
	const requestX, requestY = -1900, 100
	nativeX, nativeY := requestX, requestY
	var portalX, portalY int

	usedPortal, err := moveAbsoluteWithFallback(
		requestX, requestY, []int{1}, DisplayServerWayland,
		func() (bool, error) {
			// Model legacy native scaling before a zero-mutation backend error.
			nativeX, nativeY = requestX/2, requestY/2
			return true, ErrNotSupported
		},
		func(x, y int, displayID []int) (bool, error) {
			portalX, portalY = x, y
			if !reflect.DeepEqual(displayID, []int{1}) {
				t.Fatalf("portal display IDs = %v, want [1]", displayID)
			}
			return true, nil
		},
	)
	if err != nil || !usedPortal {
		t.Fatalf("fallback = (used=%t err=%v), want portal success", usedPortal, err)
	}
	if nativeX != -950 || nativeY != 50 {
		t.Fatalf("native coordinates = (%d,%d), want modeled scale", nativeX, nativeY)
	}
	if portalX != requestX || portalY != requestY {
		t.Fatalf("portal coordinates = (%d,%d), want request (%d,%d)", portalX, portalY, requestX, requestY)
	}
}

func TestAbsoluteMoveFallbackDecision(t *testing.T) {
	nativeFailure := errors.New("native failure")
	portalFailure := errors.New("portal failure")
	tests := []struct {
		name            string
		ready           bool
		nativeErr       error
		portalUsed      bool
		portalErr       error
		wantUsedPortal  bool
		wantErr         error
		wantPortalCalls int
	}{
		{name: "native success", ready: true},
		{name: "post-mutation native failure", ready: true, nativeErr: nativeFailure, wantErr: nativeFailure},
		{name: "portal unavailable", nativeErr: nativeFailure, wantErr: nativeFailure, wantPortalCalls: 1},
		{name: "portal success", nativeErr: nativeFailure, portalUsed: true, wantUsedPortal: true, wantPortalCalls: 1},
		{name: "portal failure", nativeErr: nativeFailure, portalUsed: true, portalErr: portalFailure, wantUsedPortal: true, wantErr: portalFailure, wantPortalCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			portalCalls := 0
			usedPortal, err := moveAbsoluteWithFallback(
				10, 20, nil, DisplayServerWayland,
				func() (bool, error) { return test.ready, test.nativeErr },
				func(int, int, []int) (bool, error) {
					portalCalls++
					return test.portalUsed, test.portalErr
				},
			)
			if usedPortal != test.wantUsedPortal || !errors.Is(err, test.wantErr) || portalCalls != test.wantPortalCalls {
				t.Fatalf(
					"fallback = (used=%t err=%v portalCalls=%d), want (%t,%v,%d)",
					usedPortal, err, portalCalls,
					test.wantUsedPortal, test.wantErr, test.wantPortalCalls,
				)
			}
		})
	}
}

type blockingHighLevelInputSession struct {
	*fakeHighLevelPortalSession
	once    sync.Once
	started chan struct{}
	device  inputportal.DeviceType
}

func (session *blockingHighLevelInputSession) block(ctx context.Context) error {
	session.once.Do(func() { close(session.started) })
	<-ctx.Done()
	return ctx.Err()
}

func (session *blockingHighLevelInputSession) KeyboardKeysym(
	ctx context.Context, _ int32, _ bool,
) error {
	if session.device == inputportal.DeviceKeyboard {
		return session.block(ctx)
	}
	return nil
}

func (session *blockingHighLevelInputSession) PointerMotion(
	ctx context.Context, _, _ float64,
) error {
	if session.device == inputportal.DevicePointer {
		return session.block(ctx)
	}
	return nil
}

func TestCloseWaylandInputCancelsPortalBeforeDeviceLocks(t *testing.T) {
	for _, test := range []struct {
		name    string
		device  inputportal.DeviceType
		operate func() error
	}{
		{name: "keyboard", device: inputportal.DeviceKeyboard, operate: func() error { return KeyTap("a") }},
		{name: "pointer", device: inputportal.DevicePointer, operate: func() error { return MoveRelativeE(1, 1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(envWaylandDisplay, "robotgo-close-cancel")
			t.Setenv(envDisplay, "")
			base := installFakeHighLevelPortalSession(t, test.device)
			session := &blockingHighLevelInputSession{
				fakeHighLevelPortalSession: base,
				started:                    make(chan struct{}),
				device:                     test.device,
			}
			remoteDesktopInputState.Lock()
			remoteDesktopInputState.session = session
			remoteDesktopInputState.generation++
			remoteDesktopInputState.Unlock()

			operationDone := make(chan error, 1)
			go func() { operationDone <- test.operate() }()
			select {
			case <-session.started:
			case <-time.After(time.Second):
				t.Fatal("portal input operation did not reach blocking callback")
			}

			closeDone := make(chan struct{})
			go func() {
				CloseWaylandInput()
				close(closeDone)
			}()
			select {
			case err := <-operationDone:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled portal operation error = %v, want context.Canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatal("portal operation remained blocked behind CloseWaylandInput")
			}
			select {
			case <-closeDone:
			case <-time.After(time.Second):
				t.Fatal("CloseWaylandInput deadlocked on device lock")
			}
		})
	}
}

func TestStatefulPortalInputKeepsBackendAndOwnership(t *testing.T) {
	t.Setenv(envWaylandDisplay, "robotgo-stateful-portal")
	t.Setenv(envDisplay, "")
	previous := GetRuntimeConfig()
	config := previous
	config.KeyDelay = 0
	config.MouseDelay = 0
	if err := SetRuntimeConfig(config); err != nil {
		t.Fatalf("disable input delays: %v", err)
	}
	t.Cleanup(func() { _ = SetRuntimeConfig(previous) })

	session := installFakeHighLevelPortalSession(
		t, inputportal.DeviceKeyboard|inputportal.DevicePointer,
	)
	t.Cleanup(CloseWaylandInput)

	if err := KeyDown("a", "ctrl"); err != nil {
		t.Fatalf("portal KeyDown before native recovery: %v", err)
	}
	if err := MouseDown("right"); err != nil {
		t.Fatalf("portal MouseDown before native recovery: %v", err)
	}
	// A fresh transaction now observes X11. Matching Up operations must still
	// use the portal generation that successfully handled their Down.
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, ":robotgo-native-recovered")
	if err := KeyUp("a", "ctrl"); err != nil {
		t.Fatalf("portal-affine KeyUp after native recovery: %v", err)
	}
	if err := MouseUp("right"); err != nil {
		t.Fatalf("portal-affine MouseUp after native recovery: %v", err)
	}
	events, _ := session.snapshot()
	wantAffinity := []string{
		"keysym:65507:true",
		"keysym:97:true",
		"button:273:true",
		"keysym:97:false",
		"keysym:65507:false",
		"button:273:false",
	}
	if !reflect.DeepEqual(events, wantAffinity) {
		t.Fatalf("backend-affine events = %#v, want %#v", events, wantAffinity)
	}

	t.Setenv(envWaylandDisplay, "robotgo-stateful-portal")
	t.Setenv(envDisplay, "")
	beforeShared := len(events)
	if err := KeyDown("a", "ctrl"); err != nil {
		t.Fatalf("first shared-modifier KeyDown: %v", err)
	}
	if err := KeyDown("b", "ctrl"); err != nil {
		t.Fatalf("second shared-modifier KeyDown: %v", err)
	}
	if err := KeyUp("a", "ctrl"); err != nil {
		t.Fatalf("first shared-modifier KeyUp: %v", err)
	}
	if err := KeyUp("b", "ctrl"); err != nil {
		t.Fatalf("second shared-modifier KeyUp: %v", err)
	}
	events, _ = session.snapshot()
	wantShared := []string{
		"keysym:65507:true",
		"keysym:97:true",
		"keysym:98:true",
		"keysym:97:false",
		"keysym:98:false",
		"keysym:65507:false",
	}
	if !reflect.DeepEqual(events[beforeShared:], wantShared) {
		t.Fatalf("shared-modifier events = %#v, want %#v", events[beforeShared:], wantShared)
	}

	beforeAlias := len(events)
	if err := KeyDown("esc"); err != nil {
		t.Fatalf("portal KeyDown with first alias: %v", err)
	}
	if err := KeyUp("escape"); err != nil {
		t.Fatalf("portal KeyUp with equivalent alias: %v", err)
	}
	events, _ = session.snapshot()
	wantAlias := []string{"keysym:65307:true", "keysym:65307:false"}
	if !reflect.DeepEqual(events[beforeAlias:], wantAlias) {
		t.Fatalf("alias-matched portal events = %#v, want %#v", events[beforeAlias:], wantAlias)
	}

	beforeOwnership := len(events)
	if err := KeyUp("z"); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("orphan KeyUp error = %v, want ErrInputOwnership", err)
	}
	if err := MouseUp("left"); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("orphan MouseUp error = %v, want ErrInputOwnership", err)
	}
	if err := KeyDown("x"); err != nil {
		t.Fatalf("owned KeyDown before duplicate: %v", err)
	}
	beforeDuplicate, _ := session.snapshot()
	if err := KeyDown("x"); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("duplicate KeyDown error = %v, want ErrInputOwnership", err)
	}
	afterDuplicate, _ := session.snapshot()
	if len(afterDuplicate) != len(beforeDuplicate) {
		t.Fatalf("duplicate KeyDown emitted events: before=%v after=%v", beforeDuplicate, afterDuplicate)
	}
	if err := KeyUp("x"); err != nil {
		t.Fatalf("owned KeyUp after duplicate: %v", err)
	}
	events, _ = session.snapshot()
	if len(events) != beforeOwnership+2 {
		t.Fatalf("ownership checks emitted unexpected events: before=%d after=%d events=%v", beforeOwnership, len(events), events)
	}

	if err := KeyDown("d"); err != nil {
		t.Fatalf("portal KeyDown before session close: %v", err)
	}
	beforeLostSession, _ := session.snapshot()
	if err := CloseRemoteDesktopInput(); err != nil {
		t.Fatalf("CloseRemoteDesktopInput after KeyDown: %v", err)
	}
	t.Setenv(envWaylandDisplay, "")
	t.Setenv(envDisplay, ":robotgo-native-after-close")
	if err := KeyUp("d"); !errors.Is(err, ErrInputOwnership) {
		t.Fatalf("KeyUp after portal session loss error = %v, want ErrInputOwnership", err)
	}
	afterLostSession, _ := session.snapshot()
	if len(afterLostSession) != len(beforeLostSession) {
		t.Fatalf("KeyUp after portal session loss emitted on old/new backend: before=%v after=%v", beforeLostSession, afterLostSession)
	}
}
