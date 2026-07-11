package robotgo

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	inputportal "github.com/marang/robotgo/input/portal"
)

const remoteDesktopEventTimeout = 5 * time.Second

var remoteDesktopMouseSleeper = MilliSleep

// RemoteDesktopDevice is a device mask for consent-aware portal input.
type RemoteDesktopDevice = inputportal.DeviceType

const (
	// RemoteDesktopKeyboard requests keyboard injection permission.
	RemoteDesktopKeyboard RemoteDesktopDevice = inputportal.DeviceKeyboard
	// RemoteDesktopPointer requests pointer injection permission.
	RemoteDesktopPointer RemoteDesktopDevice = inputportal.DevicePointer
	// RemoteDesktopTouchscreen requests touch injection and requires ScreenCast sources.
	RemoteDesktopTouchscreen RemoteDesktopDevice = inputportal.DeviceTouchscreen
)

// RemoteDesktopSource is a ScreenCast source mask used for absolute input.
type RemoteDesktopSource = inputportal.SourceType

// RemoteDesktopCursorMode controls cursor representation in selected streams.
type RemoteDesktopCursorMode = inputportal.CursorMode

// RemoteDesktopPersistMode controls portal permission persistence.
type RemoteDesktopPersistMode = inputportal.PersistMode

// RemoteDesktopInputOptions configures devices and optional ScreenCast sources.
type RemoteDesktopInputOptions = inputportal.OpenOptions

// RemoteDesktopStream describes a selected stream's logical coordinate space.
type RemoteDesktopStream = inputportal.Stream

const (
	RemoteDesktopSourceMonitor   = inputportal.SourceMonitor
	RemoteDesktopSourceWindow    = inputportal.SourceWindow
	RemoteDesktopSourceVirtual   = inputportal.SourceVirtual
	RemoteDesktopCursorHidden    = inputportal.CursorHidden
	RemoteDesktopCursorEmbedded  = inputportal.CursorEmbedded
	RemoteDesktopCursorMetadata  = inputportal.CursorMetadata
	RemoteDesktopPersistNone     = inputportal.PersistNone
	RemoteDesktopPersistApp      = inputportal.PersistApplication
	RemoteDesktopPersistExplicit = inputportal.PersistExplicit
)

type remoteDesktopInputSession interface {
	Devices() inputportal.DeviceType
	Closed() <-chan struct{}
	Close() error
	PointerMotion(context.Context, float64, float64) error
	PointerMotionAbsolute(context.Context, uint32, float64, float64) error
	PointerButton(context.Context, int32, bool) error
	PointerAxisDiscrete(context.Context, inputportal.PointerAxis, int32) error
	KeyboardKeysym(context.Context, int32, bool) error
	Streams() []inputportal.Stream
	RestoreToken() string
	TouchDown(context.Context, uint32, uint32, float64, float64) error
	TouchMotion(context.Context, uint32, uint32, float64, float64) error
	TouchUp(context.Context, uint32) error
}

type remoteDesktopPendingStart struct {
	cancel context.CancelFunc
}

var (
	remoteDesktopStatusProbe = inputportal.Probe
	remoteDesktopInputOpen   = func(ctx context.Context, options inputportal.OpenOptions) (remoteDesktopInputSession, error) {
		return inputportal.OpenWithOptions(ctx, options)
	}
	remoteDesktopInputOperation sync.Mutex
	remoteDesktopInputPending   struct {
		sync.Mutex
		start   *remoteDesktopPendingStart
		closing int
	}
	remoteDesktopInputState struct {
		sync.RWMutex
		session    remoteDesktopInputSession
		permission RemoteDesktopPermissionStatus
		reason     string
	}
)

// StartRemoteDesktopInput opens a consent-aware portal session and makes it
// available to supported high-level input APIs when native Wayland input is
// unavailable. Replacing an existing session closes the old one.
func StartRemoteDesktopInput(ctx context.Context, devices RemoteDesktopDevice) error {
	return StartRemoteDesktopInputWithOptions(ctx, RemoteDesktopInputOptions{Devices: devices})
}

// StartRemoteDesktopInputWithOptions opens a consent-aware input session and
// optionally selects ScreenCast sources for absolute pointer and touch input.
func StartRemoteDesktopInputWithOptions(ctx context.Context, options RemoteDesktopInputOptions) error {
	if runtime.GOOS != "linux" || DetectDisplayServer() != DisplayServerWayland {
		return fmt.Errorf("%w: RemoteDesktop portal input requires a Linux Wayland session", ErrNotSupported)
	}
	remoteDesktopInputOperation.Lock()
	defer remoteDesktopInputOperation.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	openCtx, cancel := context.WithCancel(ctx)
	pending := &remoteDesktopPendingStart{cancel: cancel}
	remoteDesktopInputPending.Lock()
	if remoteDesktopInputPending.closing > 0 {
		remoteDesktopInputPending.Unlock()
		cancel()
		return context.Canceled
	}
	remoteDesktopInputPending.start = pending
	remoteDesktopInputPending.Unlock()
	defer func() {
		cancel()
		remoteDesktopInputPending.Lock()
		if remoteDesktopInputPending.start == pending {
			remoteDesktopInputPending.start = nil
		}
		remoteDesktopInputPending.Unlock()
	}()

	session, err := remoteDesktopInputOpen(openCtx, inputportal.OpenOptions(options))
	if err != nil {
		remoteDesktopInputState.Lock()
		if remoteDesktopInputState.session == nil || remoteDesktopSessionClosed(remoteDesktopInputState.session) {
			remoteDesktopInputState.session = nil
			remoteDesktopInputState.permission = permissionStatusForError(err)
			remoteDesktopInputState.reason = err.Error()
		}
		remoteDesktopInputState.Unlock()
		return err
	}

	remoteDesktopInputState.RLock()
	previous := remoteDesktopInputState.session
	remoteDesktopInputState.RUnlock()
	if previous != nil {
		if err := previous.Close(); err != nil {
			newCloseErr := session.Close()
			remoteDesktopInputState.Lock()
			if remoteDesktopInputState.session == previous {
				remoteDesktopInputState.session = nil
				remoteDesktopInputState.permission = RemoteDesktopPermissionUnavailable
				remoteDesktopInputState.reason = err.Error()
			}
			remoteDesktopInputState.Unlock()
			return errors.Join(
				fmt.Errorf("remote desktop portal: close previous session: %w", err),
				wrapRemoteDesktopCloseError("close replacement session", newCloseErr),
			)
		}
	}
	remoteDesktopInputState.Lock()
	remoteDesktopInputState.session = session
	remoteDesktopInputState.permission = RemoteDesktopPermissionGranted
	remoteDesktopInputState.reason = "portal consent session is active"
	remoteDesktopInputState.Unlock()
	return nil
}

func remoteDesktopSessionClosed(session remoteDesktopInputSession) bool {
	if session == nil {
		return true
	}
	select {
	case <-session.Closed():
		return true
	default:
		return false
	}
}

func wrapRemoteDesktopCloseError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("remote desktop portal: %s: %w", action, err)
}

// RemoteDesktopInputReady reports whether an active portal session grants all
// requested devices. It never opens a session or presents a consent dialog.
func RemoteDesktopInputReady(devices RemoteDesktopDevice) error {
	remoteDesktopInputState.RLock()
	defer remoteDesktopInputState.RUnlock()
	if remoteDesktopInputState.session == nil {
		return fmt.Errorf("%w: call StartRemoteDesktopInput to request consent", ErrNotSupported)
	}
	select {
	case <-remoteDesktopInputState.session.Closed():
		return inputportal.ErrClosed
	default:
	}
	granted := remoteDesktopInputState.session.Devices()
	requested := inputportal.DeviceType(devices)
	if requested == 0 || granted&requested != requested {
		return fmt.Errorf("%w: requested=%d granted=%d", inputportal.ErrDeviceNotGranted, requested, granted)
	}
	return nil
}

// CloseRemoteDesktopInput closes the active portal input session. It is safe to
// call when no session is active.
func CloseRemoteDesktopInput() error {
	remoteDesktopInputPending.Lock()
	remoteDesktopInputPending.closing++
	if remoteDesktopInputPending.start != nil {
		remoteDesktopInputPending.start.cancel()
	}
	remoteDesktopInputPending.Unlock()

	remoteDesktopInputOperation.Lock()
	defer func() {
		remoteDesktopInputPending.Lock()
		remoteDesktopInputPending.closing--
		remoteDesktopInputPending.Unlock()
		remoteDesktopInputOperation.Unlock()
	}()

	remoteDesktopInputState.Lock()
	session := remoteDesktopInputState.session
	alreadyClosed := remoteDesktopSessionClosed(session)
	remoteDesktopInputState.session = nil
	if session != nil {
		remoteDesktopInputState.permission = RemoteDesktopPermissionClosed
		if alreadyClosed {
			remoteDesktopInputState.reason = "portal session was already closed before application cleanup"
		} else {
			remoteDesktopInputState.reason = "portal session was closed by the application"
		}
	}
	remoteDesktopInputState.Unlock()
	if session == nil {
		return nil
	}
	return session.Close()
}

func withRemoteDesktopInput(device inputportal.DeviceType, fn func(remoteDesktopInputSession) error) (bool, error) {
	remoteDesktopInputState.RLock()
	session := remoteDesktopInputState.session
	remoteDesktopInputState.RUnlock()
	if session == nil {
		return false, nil
	}
	select {
	case <-session.Closed():
		return true, inputportal.ErrClosed
	default:
	}
	if session.Devices()&device == 0 {
		return true, fmt.Errorf("%w: required=%d granted=%d", inputportal.ErrDeviceNotGranted, device, session.Devices())
	}
	return true, fn(session)
}

func remoteDesktopEvent(fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteDesktopEventTimeout)
	defer cancel()
	return fn(ctx)
}

func finishRemoteDesktopMouseEvent(err error, extraDelay int) error {
	if err == nil {
		remoteDesktopMouseSleeper(currentMouseDelay() + extraDelay)
	}
	return err
}

func portalModifiedKey(session remoteDesktopInputSession, key int32, modifiers []int32, down bool, tap bool) error {
	if !tap && !down {
		eventErr := remoteDesktopEvent(func(ctx context.Context) error {
			return session.KeyboardKeysym(ctx, key, false)
		})
		for i := len(modifiers) - 1; i >= 0; i-- {
			eventErr = errors.Join(eventErr, remoteDesktopEvent(func(ctx context.Context) error {
				return session.KeyboardKeysym(ctx, modifiers[i], false)
			}))
		}
		return eventErr
	}

	pressed := 0
	releaseModifiers := func() error {
		var releaseErr error
		for i := pressed - 1; i >= 0; i-- {
			releaseErr = errors.Join(releaseErr, remoteDesktopEvent(func(ctx context.Context) error {
				return session.KeyboardKeysym(ctx, modifiers[i], false)
			}))
		}
		return releaseErr
	}
	for _, modifier := range modifiers {
		if err := remoteDesktopEvent(func(ctx context.Context) error {
			return session.KeyboardKeysym(ctx, modifier, true)
		}); err != nil {
			return errors.Join(err, releaseModifiers())
		}
		pressed++
	}
	eventErr := remoteDesktopEvent(func(ctx context.Context) error {
		return session.KeyboardKeysym(ctx, key, down)
	})
	if eventErr == nil && tap {
		eventErr = remoteDesktopEvent(func(ctx context.Context) error {
			return session.KeyboardKeysym(ctx, key, false)
		})
	}
	if !tap && eventErr == nil {
		return nil
	}
	return errors.Join(eventErr, releaseModifiers())
}

func portalKeysymForRune(value rune) (int32, error) {
	if value < 0 || value > 0x10ffff || value >= 0xd800 && value <= 0xdfff {
		return 0, fmt.Errorf("invalid Unicode code point U+%04X", value)
	}
	switch value {
	case '\b':
		return 0xff08, nil
	case '\t':
		return 0xff09, nil
	case '\n', '\r':
		return 0xff0d, nil
	case 0x1b:
		return 0xff1b, nil
	}
	if value >= 0x20 && value <= 0x7e || value >= 0xa0 && value <= 0xff {
		return int32(value), nil
	}
	if value < 0xa0 {
		return 0, fmt.Errorf("unsupported control character U+%04X", value)
	}
	return int32(0x01000000 | value), nil
}

func portalPointerButton(name string) (int32, error) {
	switch name {
	case "", "left":
		return 0x110, nil
	case "right":
		return 0x111, nil
	case "center":
		return 0x112, nil
	default:
		return 0, fmt.Errorf("%w: portal pointer button %q", ErrNotSupported, name)
	}
}

func tryRemoteDesktopMoveRelative(x, y int) (bool, error) {
	return withRemoteDesktopInput(inputportal.DevicePointer, func(session remoteDesktopInputSession) error {
		return remoteDesktopEvent(func(ctx context.Context) error {
			return session.PointerMotion(ctx, float64(x), float64(y))
		})
	})
}

func tryRemoteDesktopClick(name string, double bool) (bool, error) {
	return withRemoteDesktopInput(inputportal.DevicePointer, func(session remoteDesktopInputSession) error {
		button, err := portalPointerButton(name)
		if err != nil {
			return err
		}
		clicks := 1
		if double {
			clicks = 2
		}
		for i := 0; i < clicks; i++ {
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.PointerButton(ctx, button, true)
			}); err != nil {
				return err
			}
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.PointerButton(ctx, button, false)
			}); err != nil {
				return err
			}
			if double && i == 0 {
				MilliSleep(200)
			}
		}
		return nil
	})
}

func tryRemoteDesktopToggle(name string, down bool) (bool, error) {
	return withRemoteDesktopInput(inputportal.DevicePointer, func(session remoteDesktopInputSession) error {
		button, err := portalPointerButton(name)
		if err != nil {
			return err
		}
		return remoteDesktopEvent(func(ctx context.Context) error {
			return session.PointerButton(ctx, button, down)
		})
	})
}

func tryRemoteDesktopScroll(x, y int) (bool, error) {
	return withRemoteDesktopInput(inputportal.DevicePointer, func(session remoteDesktopInputSession) error {
		if x != 0 {
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.PointerAxisDiscrete(ctx, inputportal.PointerAxisHorizontal, int32(x))
			}); err != nil {
				return err
			}
		}
		if y != 0 {
			return remoteDesktopEvent(func(ctx context.Context) error {
				return session.PointerAxisDiscrete(ctx, inputportal.PointerAxisVertical, int32(-y))
			})
		}
		return nil
	})
}

func tryRemoteDesktopUnicode(value rune, args []int) (bool, error) {
	return withRemoteDesktopInput(inputportal.DeviceKeyboard, func(session remoteDesktopInputSession) error {
		if len(args) > 0 && args[0] != 0 {
			return fmt.Errorf("%w: RemoteDesktop portal input cannot target a process", ErrNotSupported)
		}
		keysym, err := portalKeysymForRune(value)
		if err != nil {
			return err
		}
		if err := remoteDesktopEvent(func(ctx context.Context) error {
			return session.KeyboardKeysym(ctx, keysym, true)
		}); err != nil {
			return err
		}
		return remoteDesktopEvent(func(ctx context.Context) error {
			return session.KeyboardKeysym(ctx, keysym, false)
		})
	})
}

func tryRemoteDesktopText(text string, args []int) (bool, error) {
	return withRemoteDesktopInput(inputportal.DeviceKeyboard, func(session remoteDesktopInputSession) error {
		if len(args) > 0 && args[0] != 0 {
			return fmt.Errorf("%w: RemoteDesktop portal input cannot target a process", ErrNotSupported)
		}
		delay := 0
		if len(args) > 1 {
			delay = args[1]
		}
		for _, value := range text {
			keysym, err := portalKeysymForRune(value)
			if err != nil {
				return err
			}
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.KeyboardKeysym(ctx, keysym, true)
			}); err != nil {
				return err
			}
			if err := remoteDesktopEvent(func(ctx context.Context) error {
				return session.KeyboardKeysym(ctx, keysym, false)
			}); err != nil {
				return err
			}
			MilliSleep(delay)
		}
		return nil
	})
}
