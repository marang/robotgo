//go:build linux && !cgo

package robotgo

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

const (
	x11XTestMajor = 2
	x11XTestMinor = 2
)

var (
	errX11Connection  = errors.New("X11 connection failure")
	errX11KeyboardMap = errors.New("X11 keyboard mapping failure")
)

// x11InputBackend owns one lazily opened X connection. Its mutex covers whole
// high-level transactions, including the stable scratch-keymap pool.
type x11InputBackend struct {
	mu             sync.Mutex
	conn           *xgb.Conn
	display        string
	root           xproto.Window
	keyboard       x11KeyboardMap
	events         <-chan struct{}
	heldKeys       map[uint32]x11HeldKey
	keyRefs        map[xproto.Keycode]int
	keyOrder       []xproto.Keycode
	buttons        map[byte]struct{}
	buttonOrder    []byte
	cleanupPending bool

	scratchInitialized bool
	scratchPerKeycode  byte
	scratchSlots       []x11ScratchSlot
	scratchByKeysym    map[uint32]xproto.Keycode

	eventMu         sync.Mutex
	eventGeneration uint64
	eventErr        error
}

var linuxX11Input = &x11InputBackend{}

func platformPureGoInputBackend() pureGoInputBackend {
	if DetectDisplayServer() != DisplayServerX11 {
		return nil
	}
	return linuxX11Input
}

func closePureGoPlatformInput() error { return linuxX11Input.Close() }

func (*x11InputBackend) Name() string { return featureBackendPureGoX11 }

func (backend *x11InputBackend) sessionReady() error {
	if DetectDisplayServer() != DisplayServerX11 {
		return fmt.Errorf("%w: Pure-Go X11 input requires DISPLAY without an active Wayland session", ErrNotSupported)
	}
	if pureGoX11EnvironmentConflict() {
		return fmt.Errorf("%w: %s selects Wayland while DISPLAY selects X11", ErrNotSupported, envXDGSessionType)
	}
	return nil
}

func (backend *x11InputBackend) openLocked() error {
	if err := backend.sessionReady(); err != nil {
		if backend.conn != nil {
			err = errors.Join(err, backend.closeLocked())
		}
		return err
	}
	display := os.Getenv(envDisplay)
	if display == "" {
		return fmt.Errorf("%w: %s is unset", ErrNotSupported, envDisplay)
	}
	if backend.conn != nil {
		if backend.cleanupPending {
			return errors.New("robotgo: previous X11 cleanup is incomplete; release conflicting input state and retry CloseMainDisplayE")
		}
		if backend.display != display {
			if err := backend.closeLocked(); err != nil {
				return fmt.Errorf("robotgo: clean up previous X11 display: %w", err)
			}
		} else {
			if err := backend.eventDrainErrorLocked(); err != nil {
				return errors.Join(
					fmt.Errorf("robotgo: X11 connection is unhealthy: %w", err),
					backend.closeLocked(),
				)
			}
			return nil
		}
	}
	connection, err := xgb.NewConnDisplay(display)
	if err != nil {
		return fmt.Errorf("robotgo: connect to X11 display %q: %w", display, err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			connection.Close()
		}
	}()
	if err := xtest.Init(connection); err != nil {
		return fmt.Errorf("%w: XTEST extension is unavailable: %v", ErrNotSupported, err)
	}
	version, err := xtest.GetVersion(connection, x11XTestMajor, x11XTestMinor).Reply()
	if err != nil {
		return fmt.Errorf("robotgo: query XTEST version: %w", err)
	}
	if !x11XTestVersionSupported(version) {
		if version == nil {
			return fmt.Errorf("%w: XTEST returned no version", ErrNotSupported)
		}
		return fmt.Errorf("%w: XTEST %d.%d is older than required %d.%d",
			ErrNotSupported, version.MajorVersion, version.MinorVersion, x11XTestMajor, x11XTestMinor)
	}
	setup := xproto.Setup(connection)
	if setup == nil {
		return errors.New("robotgo: X11 returned no setup information")
	}
	screen := setup.DefaultScreen(connection)
	if screen == nil || screen.Root == 0 {
		return errors.New("robotgo: X11 returned no default root window")
	}
	backend.conn = connection
	backend.display = display
	backend.root = screen.Root
	backend.keyboard = x11KeyboardMap{}
	backend.startEventDrainLocked(connection)
	closeOnError = false
	return nil
}

func x11XTestVersionSupported(version *xtest.GetVersionReply) bool {
	if version == nil {
		return false
	}
	return version.MajorVersion > x11XTestMajor ||
		version.MajorVersion == x11XTestMajor && version.MinorVersion >= x11XTestMinor
}

func (backend *x11InputBackend) startEventDrainLocked(connection *xgb.Conn) {
	backend.eventMu.Lock()
	backend.eventGeneration++
	generation := backend.eventGeneration
	backend.eventErr = nil
	backend.eventMu.Unlock()
	done := make(chan struct{})
	backend.events = done
	go func() {
		defer close(done)
		for {
			event, eventErr := connection.WaitForEvent()
			if event == nil && eventErr == nil {
				return
			}
			if eventErr != nil {
				backend.eventMu.Lock()
				if backend.eventGeneration == generation {
					backend.eventErr = errors.Join(backend.eventErr, eventErr)
				}
				backend.eventMu.Unlock()
			}
		}
	}()
}

func (backend *x11InputBackend) eventDrainErrorLocked() error {
	if backend.events != nil {
		select {
		case <-backend.events:
			return errors.New("X11 connection event drain stopped")
		default:
		}
	}
	backend.eventMu.Lock()
	defer backend.eventMu.Unlock()
	return backend.eventErr
}

func (backend *x11InputBackend) withServerGrabLocked(operation func() error) (err error) {
	if backend.conn == nil {
		return errors.Join(errX11Connection, errors.New("X11 connection is closed"))
	}
	if grabErr := xproto.GrabServerChecked(backend.conn).Check(); grabErr != nil {
		return errors.Join(errX11Connection, grabErr)
	}
	defer func() {
		if ungrabErr := xproto.UngrabServerChecked(backend.conn).Check(); ungrabErr != nil {
			// Connection health takes precedence over the operation result: the
			// caller must discard a connection whose server-grab state is unknown.
			err = errors.Join(err, errX11Connection, ungrabErr)
		}
	}()
	return operation()
}

func (backend *x11InputBackend) closeLocked() error {
	var cleanupErr error
	if backend.conn != nil {
		cleanupErr = errors.Join(
			backend.withServerGrabLocked(backend.releaseOwnedInputLocked),
			backend.restoreScratchMappingsLocked(),
		)
		if cleanupErr != nil && !errors.Is(cleanupErr, errX11Connection) {
			backend.cleanupPending = true
			return cleanupErr
		}
		backend.conn.Close()
	}
	backend.conn = nil
	backend.display = ""
	backend.root = 0
	backend.keyboard = x11KeyboardMap{}
	backend.events = nil
	backend.heldKeys = nil
	backend.keyRefs = nil
	backend.keyOrder = nil
	backend.buttons = nil
	backend.buttonOrder = nil
	backend.cleanupPending = false
	backend.scratchInitialized = false
	backend.scratchPerKeycode = 0
	backend.scratchSlots = nil
	backend.scratchByKeysym = nil
	return cleanupErr
}

func (backend *x11InputBackend) releaseOwnedInputLocked() error {
	var releaseErr error
	for index := len(backend.keyOrder) - 1; index >= 0; index-- {
		code := backend.keyOrder[index]
		if backend.keyRefs[code] > 0 {
			if err := backend.sendKeyLocked(code, false); err != nil {
				releaseErr = errors.Join(releaseErr, err)
				continue
			}
			delete(backend.keyRefs, code)
			backend.keyOrder = removeX11Keycode(backend.keyOrder, code)
		}
	}
	for keysym, held := range backend.heldKeys {
		if backend.keyRefs[held.code] == 0 {
			delete(backend.heldKeys, keysym)
		}
	}
	for index := len(backend.buttonOrder) - 1; index >= 0; index-- {
		button := backend.buttonOrder[index]
		if _, held := backend.buttons[button]; held {
			if err := backend.sendButtonLocked(button, false); err != nil {
				releaseErr = errors.Join(releaseErr, err)
				continue
			}
			delete(backend.buttons, button)
			backend.buttonOrder = removeX11Button(backend.buttonOrder, button)
		}
	}
	return releaseErr
}

func (backend *x11InputBackend) Close() error {
	backend.mu.Lock()
	err := backend.closeLocked()
	backend.mu.Unlock()
	return err
}

func (backend *x11InputBackend) failLocked(action string, err error) error {
	return errors.Join(
		fmt.Errorf("robotgo: X11 %s: %w", action, err),
		backend.closeLocked(),
	)
}
