//go:build linux

package x11input

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jezek/xgb/xproto"
)

const (
	x11XTestMajor = 2
	x11XTestMinor = 2
)

var (
	// ErrUnsupported identifies X11/XTEST capabilities the core cannot provide.
	// The public adapter translates it to robotgo.ErrNotSupported.
	ErrUnsupported    = errors.New("X11 input operation is not supported")
	errX11Connection  = errors.New("X11 connection failure")
	errX11KeyboardMap = errors.New("X11 keyboard mapping failure")
)

// Backend owns one lazily opened X connection. Its mutex covers whole
// high-level transactions, including the stable scratch-keymap pool.
type Backend struct {
	mu             sync.Mutex
	conn           Connection
	config         Config
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
	beforeTextTap   func()
}

// New creates an independently owned, lazily connected X11 input backend.
func New(config Config) *Backend {
	if config.Dialer == nil {
		config.Dialer = xgbDialer{}
	}
	if config.KeyHoldDelay == 0 {
		config.KeyHoldDelay = 5 * time.Millisecond
	}
	if config.Sleep == nil {
		config.Sleep = time.Sleep
	}
	return &Backend{config: config}
}

func (backend *Backend) openLocked() (err error) {
	if backend.config.ResolveDisplay == nil {
		return fmt.Errorf("%w: no X11 display resolver configured", ErrUnsupported)
	}
	display, err := backend.config.ResolveDisplay()
	if err != nil {
		if backend.conn != nil {
			err = errors.Join(err, backend.closeLocked())
		}
		return err
	}
	if display == "" {
		return fmt.Errorf("%w: X11 display is unset", ErrUnsupported)
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
	connection, err := backend.config.Dialer.Dial(display)
	if err != nil {
		return fmt.Errorf("robotgo: connect to X11 display %q: %w", display, err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			err = errors.Join(err, connection.Close())
		}
	}()
	if err := connection.InitXTest(); err != nil {
		return fmt.Errorf("%w: XTEST extension is unavailable: %w", ErrUnsupported, err)
	}
	version, err := connection.XTestVersion(x11XTestMajor, x11XTestMinor)
	if err != nil {
		return fmt.Errorf("robotgo: query XTEST version: %w", err)
	}
	if !x11XTestVersionSupported(version) {
		if !version.Valid {
			return fmt.Errorf("%w: XTEST returned no version", ErrUnsupported)
		}
		return fmt.Errorf("%w: XTEST %d.%d is older than required %d.%d",
			ErrUnsupported, version.Major, version.Minor, x11XTestMajor, x11XTestMinor)
	}
	setup, err := connection.Setup()
	if err != nil {
		return fmt.Errorf("robotgo: query X11 setup: %w", err)
	}
	backend.conn = connection
	backend.display = display
	backend.root = setup.Root
	backend.keyboard = x11KeyboardMap{}
	backend.startEventDrainLocked(connection)
	closeOnError = false
	return nil
}

func x11XTestVersionSupported(version XTestVersion) bool {
	return version.Valid && (version.Major > x11XTestMajor ||
		version.Major == x11XTestMajor && version.Minor >= x11XTestMinor)
}

func (backend *Backend) startEventDrainLocked(connection Connection) {
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
			open, eventErr := connection.WaitForEvent()
			if !open && eventErr == nil {
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

func (backend *Backend) eventDrainErrorLocked() error {
	stopped := false
	if backend.events != nil {
		select {
		case <-backend.events:
			stopped = true
		default:
		}
	}
	backend.eventMu.Lock()
	eventErr := backend.eventErr
	backend.eventMu.Unlock()
	if stopped {
		return errors.Join(errors.New("X11 connection event drain stopped"), eventErr)
	}
	return eventErr
}

func (backend *Backend) withServerGrabLocked(operation func() error) (err error) {
	if backend.conn == nil {
		return errors.Join(errX11Connection, errors.New("X11 connection is closed"))
	}
	if grabErr := backend.conn.GrabServer(); grabErr != nil {
		return errors.Join(errX11Connection, grabErr)
	}
	defer func() {
		if ungrabErr := backend.conn.UngrabServer(); ungrabErr != nil {
			// Connection health takes precedence over the operation result: the
			// caller must discard a connection whose server-grab state is unknown.
			err = errors.Join(err, errX11Connection, ungrabErr)
		}
	}()
	return operation()
}

func (backend *Backend) closeLocked() error {
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
		cleanupErr = errors.Join(cleanupErr, backend.conn.Close())
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

func (backend *Backend) releaseOwnedInputLocked() error {
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

func (backend *Backend) Close() error {
	backend.mu.Lock()
	err := backend.closeLocked()
	backend.mu.Unlock()
	return err
}

func (backend *Backend) failLocked(action string, err error) error {
	return errors.Join(
		fmt.Errorf("robotgo: X11 %s: %w", action, err),
		backend.closeLocked(),
	)
}
