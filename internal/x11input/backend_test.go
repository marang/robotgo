//go:build linux

package x11input

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
)

type fakeX11Server struct {
	mu           sync.Mutex
	setup        Setup
	perKeycode   byte
	keysyms      []xproto.Keysym
	modifiers    []xproto.Keycode
	pressed      []byte
	pointer      PointerState
	buttons      map[byte]bool
	connections  []*fakeX11Connection
	setupErr     error
	initErr      error
	versionErr   error
	grabErr      error
	ungrabErr    error
	mappingErr   error
	modifierErr  error
	changeErr    error
	pressedErr   error
	pointerErr   error
	fakeInputErr error
	closeErr     error
}

func newFakeX11Server() *fakeX11Server {
	return &fakeX11Server{
		setup:      Setup{Root: 1, MinKeycode: 8, MaxKeycode: 15},
		perKeycode: 2,
		keysyms: []xproto.Keysym{
			0xff0d, 0, // 8: Enter
			0xffe1, 0, // 9: Shift_L
			0, 0, // 10-15: scratch candidates
			0, 0,
			0, 0,
			0, 0,
			0, 0,
			0, 0,
		},
		modifiers: []xproto.Keycode{9},
		pressed:   make([]byte, 32),
		buttons:   make(map[byte]bool),
	}
}

type fakeX11Dialer struct{ server *fakeX11Server }

func (dialer fakeX11Dialer) Dial(string) (Connection, error) {
	connection := &fakeX11Connection{server: dialer.server, closed: make(chan struct{})}
	dialer.server.mu.Lock()
	dialer.server.connections = append(dialer.server.connections, connection)
	dialer.server.mu.Unlock()
	return connection, nil
}

type fakeX11Connection struct {
	server    *fakeX11Server
	closeOnce sync.Once
	closed    chan struct{}
}

func (connection *fakeX11Connection) Close() error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	return connection.server.closeErr
}

func (connection *fakeX11Connection) WaitForEvent() (bool, error) {
	<-connection.closed
	return false, nil
}

func (connection *fakeX11Connection) Setup() (Setup, error) {
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	return connection.server.setup, connection.server.setupErr
}

func (connection *fakeX11Connection) InitXTest() error {
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	return connection.server.initErr
}

func (connection *fakeX11Connection) XTestVersion(byte, uint16) (XTestVersion, error) {
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	return XTestVersion{Major: 2, Minor: 2, Valid: true}, connection.server.versionErr
}

func (connection *fakeX11Connection) GrabServer() error {
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	return connection.server.grabErr
}

func (connection *fakeX11Connection) UngrabServer() error {
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	return connection.server.ungrabErr
}

func (connection *fakeX11Connection) KeyboardMapping(first xproto.Keycode, count byte) (KeyboardMapping, error) {
	server := connection.server
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.mappingErr != nil {
		return KeyboardMapping{}, server.mappingErr
	}
	start := (int(first) - int(server.setup.MinKeycode)) * int(server.perKeycode)
	end := start + int(count)*int(server.perKeycode)
	if start < 0 || end > len(server.keysyms) {
		return KeyboardMapping{}, errors.New("fake keyboard-map range is invalid")
	}
	return KeyboardMapping{
		KeysymsPerKeycode: server.perKeycode,
		Keysyms:           append([]xproto.Keysym(nil), server.keysyms[start:end]...),
	}, nil
}

func (connection *fakeX11Connection) ModifierMapping() ([]xproto.Keycode, error) {
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	if connection.server.modifierErr != nil {
		return nil, connection.server.modifierErr
	}
	return append([]xproto.Keycode(nil), connection.server.modifiers...), nil
}

func (connection *fakeX11Connection) ChangeKeyboardMapping(first xproto.Keycode, per byte, keysyms []xproto.Keysym) error {
	server := connection.server
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.changeErr != nil {
		return server.changeErr
	}
	if per != server.perKeycode || len(keysyms) != int(per) {
		return errors.New("fake keyboard-map width mismatch")
	}
	start := (int(first) - int(server.setup.MinKeycode)) * int(per)
	if start < 0 || start+len(keysyms) > len(server.keysyms) {
		return errors.New("fake keyboard-map change is out of range")
	}
	copy(server.keysyms[start:], keysyms)
	return nil
}

func (connection *fakeX11Connection) PressedKeys() ([]byte, error) {
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	if connection.server.pressedErr != nil {
		return nil, connection.server.pressedErr
	}
	return append([]byte(nil), connection.server.pressed...), nil
}

func (connection *fakeX11Connection) QueryPointer(xproto.Window) (PointerState, error) {
	connection.server.mu.Lock()
	defer connection.server.mu.Unlock()
	if connection.server.pointerErr != nil {
		return PointerState{}, connection.server.pointerErr
	}
	state := connection.server.pointer
	for button, down := range connection.server.buttons {
		if down {
			state.Mask |= x11ButtonMask(button)
		}
	}
	return state, nil
}

func (connection *fakeX11Connection) FakeInput(eventType, detail byte, _ xproto.Window, x, y int16) error {
	server := connection.server
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.fakeInputErr != nil {
		return server.fakeInputErr
	}
	switch eventType {
	case byte(xproto.KeyPress), byte(xproto.KeyRelease):
		index := int(detail) / 8
		mask := byte(1 << uint(detail%8))
		if eventType == byte(xproto.KeyPress) {
			server.pressed[index] |= mask
		} else {
			server.pressed[index] &^= mask
		}
	case byte(xproto.ButtonPress):
		server.buttons[detail] = true
	case byte(xproto.ButtonRelease):
		delete(server.buttons, detail)
	case byte(xproto.MotionNotify):
		if detail == x11RelativeMotion {
			server.pointer.RootX += x
			server.pointer.RootY += y
		} else {
			server.pointer.RootX = x
			server.pointer.RootY = y
		}
	}
	return nil
}

func newFakeBackend(server *fakeX11Server, display func() string) *Backend {
	return New(Config{
		ResolveDisplay: func() (string, error) { return display(), nil },
		Dialer:         fakeX11Dialer{server: server},
		KeyHoldDelay:   time.Nanosecond,
		Sleep:          func(time.Duration) {},
	})
}

func TestKeyboardMapAndScratchContracts(t *testing.T) {
	keyboard := x11KeyboardMap{
		minimum:    8,
		perKeycode: 6,
		modifiers:  map[xproto.Keycode]struct{}{9: {}},
		keysyms: []xproto.Keysym{
			'a', 'A', 0x010003b1, 0x01000391, '@', '#',
			0xffe1, 0, 0xffe1, 0, 0, 0,
			0, 0, 0, 0, 0, 0,
		},
	}
	if resolved, ok := keyboard.resolve(0xffe1); !ok || resolved.code != 9 {
		t.Fatalf("resolve Shift_L = (%+v,%t), want keycode 9", resolved, ok)
	}
	for _, ambiguous := range []uint32{'a', 'A', 0x010003b1, '@'} {
		if resolved, ok := keyboard.resolve(ambiguous); ok {
			t.Fatalf("resolve(%#x) = %+v, want no unsafe core-map guess", ambiguous, resolved)
		}
	}
	backend := New(Config{})
	if err := backend.updateScratchStateLocked(&keyboard); err != nil {
		t.Fatalf("initialize scratch state: %v", err)
	}
	if len(backend.scratchSlots) != 1 || backend.scratchSlots[0].code != 10 {
		t.Fatalf("scratch slots = %+v, want only keycode 10", backend.scratchSlots)
	}
}

func TestModifierResolutionRequiresModifierMapMembership(t *testing.T) {
	keyboard := x11KeyboardMap{
		minimum:    8,
		perKeycode: 2,
		keysyms: []xproto.Keysym{
			0xffe3, 0,
			0xffe3, 0xffe7,
		},
		modifiers: map[xproto.Keycode]struct{}{9: {}},
	}
	resolved, ok := keyboard.resolveModifier(0xffe3)
	if !ok || resolved.code != 9 {
		t.Fatalf("resolveModifier(Control_L) = (%+v,%t), want keycode 9", resolved, ok)
	}
	delete(keyboard.modifiers, 9)
	if resolved, ok := keyboard.resolveModifier(0xffe3); ok {
		t.Fatalf("resolveModifier without modifier-map membership = %+v, want no match", resolved)
	}
}

func TestScratchCapacityAndOwnershipAreMutationFreeOnFailure(t *testing.T) {
	backend := New(Config{})
	backend.scratchInitialized = true
	backend.scratchPerKeycode = 2
	backend.scratchSlots = []x11ScratchSlot{{code: 10}}
	backend.scratchByKeysym = make(map[uint32]xproto.Keycode)
	if err := backend.validateScratchCapacityLocked([]uint32{'a', 'b'}, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("capacity error = %v, want ErrUnsupported", err)
	}
	if backend.scratchSlots[0].keysym != 0 || len(backend.scratchByKeysym) != 0 {
		t.Fatalf("capacity failure mutated state: slots=%+v map=%v", backend.scratchSlots, backend.scratchByKeysym)
	}
	pressed := make([]byte, 32)
	pressed[10/8] |= 1 << uint(10%8)
	if err := backend.validateScratchCapacityLocked([]uint32{'a'}, pressed); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("pressed capacity error = %v, want ErrUnsupported", err)
	}
}

func TestScratchRestoreUsesCurrentWidthAndOwnership(t *testing.T) {
	const keysym = uint32(0x010020ac)
	current := KeyboardMapping{
		KeysymsPerKeycode: 3,
		Keysyms: []xproto.Keysym{
			xproto.Keysym(keysym), xproto.Keysym(keysym), xproto.Keysym(keysym),
		},
	}
	width, owned, err := x11ScratchMappingCanRestore(current, keysym)
	if err != nil || !owned || width != 3 {
		t.Fatalf("restore plan = (%d,%t,%v), want (3,true,nil)", width, owned, err)
	}
	current.Keysyms[2] = 'x'
	if _, owned, err := x11ScratchMappingCanRestore(current, keysym); err != nil || owned {
		t.Fatalf("foreign restore plan = (owned=%t,err=%v), want false,nil", owned, err)
	}
}

func TestScratchOwnershipRecognizesServerCanonicalization(t *testing.T) {
	const keysym = uint32(0x010020ac)
	for _, test := range []struct {
		mapping []xproto.Keysym
		want    bool
	}{
		{mapping: []xproto.Keysym{xproto.Keysym(keysym), xproto.Keysym(keysym), 0}, want: true},
		{mapping: []xproto.Keysym{xproto.Keysym(keysym)}, want: true},
		{mapping: []xproto.Keysym{0, 0}},
		{mapping: []xproto.Keysym{xproto.Keysym(keysym), 'x'}},
	} {
		if got := x11MappingOwnedBy(test.mapping, keysym); got != test.want {
			t.Fatalf("x11MappingOwnedBy(%v,%#x) = %t, want %t", test.mapping, keysym, got, test.want)
		}
	}
}

func TestScratchOwnershipRejectsModifierReassignment(t *testing.T) {
	const keysym = uint32(0x010020ac)
	backend := New(Config{})
	backend.scratchInitialized = true
	backend.scratchPerKeycode = 2
	backend.scratchSlots = []x11ScratchSlot{{code: 10, keysym: keysym}}
	backend.scratchByKeysym = map[uint32]xproto.Keycode{keysym: 10}
	keyboard := x11KeyboardMap{
		minimum:    10,
		perKeycode: 2,
		keysyms:    []xproto.Keysym{xproto.Keysym(keysym), xproto.Keysym(keysym)},
		modifiers:  map[xproto.Keycode]struct{}{10: {}},
	}
	if err := backend.updateScratchStateLocked(&keyboard); err == nil {
		t.Fatal("owned scratch keycode becoming a modifier unexpectedly succeeded")
	}
}

func TestScratchRefreshFailureLeavesStateExactlyUnchanged(t *testing.T) {
	const (
		firstKeysym  = uint32(0x010020ac)
		secondKeysym = uint32(0x0101f600)
	)
	backend := New(Config{})
	backend.scratchInitialized = true
	backend.scratchPerKeycode = 2
	backend.scratchSlots = []x11ScratchSlot{
		{code: 10},
		{code: 11, keysym: firstKeysym},
		{code: 12, keysym: secondKeysym},
	}
	backend.scratchByKeysym = map[uint32]xproto.Keycode{
		firstKeysym:  11,
		secondKeysym: 12,
	}
	originalSlots := append([]x11ScratchSlot(nil), backend.scratchSlots...)
	originalAssignments := map[uint32]xproto.Keycode{
		firstKeysym:  11,
		secondKeysym: 12,
	}
	keyboard := x11KeyboardMap{
		minimum:    10,
		perKeycode: 2,
		keysyms: []xproto.Keysym{
			'x', 'x',
			xproto.Keysym(firstKeysym), 0,
			xproto.Keysym(secondKeysym), 'x',
		},
		modifiers: make(map[xproto.Keycode]struct{}),
	}
	if err := backend.updateScratchStateLocked(&keyboard); err == nil {
		t.Fatal("foreign scratch replacement unexpectedly succeeded")
	}
	if !reflect.DeepEqual(backend.scratchSlots, originalSlots) {
		t.Fatalf("failed refresh mutated scratch slots: got %+v, want %+v", backend.scratchSlots, originalSlots)
	}
	if !reflect.DeepEqual(backend.scratchByKeysym, originalAssignments) {
		t.Fatalf("failed refresh mutated scratch assignments: got %v, want %v", backend.scratchByKeysym, originalAssignments)
	}
}

func TestScratchWidthChangeReinitializesOnlyWithoutClaims(t *testing.T) {
	backend := New(Config{})
	backend.scratchInitialized = true
	backend.scratchPerKeycode = 2
	backend.scratchSlots = []x11ScratchSlot{{code: 10}}
	backend.scratchByKeysym = make(map[uint32]xproto.Keycode)
	keyboard := x11KeyboardMap{
		minimum:    10,
		perKeycode: 3,
		keysyms:    make([]xproto.Keysym, 6),
		modifiers:  make(map[xproto.Keycode]struct{}),
	}
	if err := backend.updateScratchStateLocked(&keyboard); err != nil {
		t.Fatalf("refresh unclaimed scratch pool after width change: %v", err)
	}
	if backend.scratchPerKeycode != 3 || !reflect.DeepEqual(backend.scratchSlots, []x11ScratchSlot{{code: 10}, {code: 11}}) {
		t.Fatalf("reinitialized scratch state = width %d slots %+v", backend.scratchPerKeycode, backend.scratchSlots)
	}

	const keysym = uint32(0x010020ac)
	backend.scratchPerKeycode = 2
	backend.scratchSlots = []x11ScratchSlot{{code: 10, keysym: keysym}}
	backend.scratchByKeysym = map[uint32]xproto.Keycode{keysym: 10}
	originalSlots := append([]x11ScratchSlot(nil), backend.scratchSlots...)
	if err := backend.updateScratchStateLocked(&keyboard); err == nil {
		t.Fatal("scratch width change with an owned mapping unexpectedly succeeded")
	}
	if backend.scratchPerKeycode != 2 || !reflect.DeepEqual(backend.scratchSlots, originalSlots) || backend.scratchByKeysym[keysym] != 10 {
		t.Fatalf("rejected width change mutated state: width %d slots %+v map %v", backend.scratchPerKeycode, backend.scratchSlots, backend.scratchByKeysym)
	}
}

func TestXTestVersionContract(t *testing.T) {
	for _, test := range []struct {
		version XTestVersion
		want    bool
	}{
		{version: XTestVersion{}},
		{version: XTestVersion{Major: 2, Minor: 1, Valid: true}},
		{version: XTestVersion{Major: 2, Minor: 2, Valid: true}, want: true},
		{version: XTestVersion{Major: 3, Valid: true}, want: true},
	} {
		if got := x11XTestVersionSupported(test.version); got != test.want {
			t.Fatalf("x11XTestVersionSupported(%+v) = %t, want %t", test.version, got, test.want)
		}
	}
}

func TestPointerArgumentContracts(t *testing.T) {
	for _, value := range []int{math.MinInt16, 0, math.MaxInt16} {
		if got, err := x11Coordinate(value); err != nil || int(got) != value {
			t.Fatalf("x11Coordinate(%d) = (%d,%v)", value, got, err)
		}
	}
	for _, value := range []int{math.MinInt16 - 1, math.MaxInt16 + 1} {
		if _, err := x11Coordinate(value); err == nil {
			t.Fatalf("x11Coordinate(%d) unexpectedly succeeded", value)
		}
	}
	for value, want := range map[int]uint64{-1000: 1000, 0: 0, 1000: 1000} {
		if got, err := x11ScrollSteps(value); err != nil || got != want {
			t.Fatalf("x11ScrollSteps(%d) = (%d,%v), want %d,nil", value, got, err, want)
		}
	}
	for _, value := range []int{-1001, 1001, math.MinInt, math.MaxInt} {
		if _, err := x11ScrollSteps(value); err == nil {
			t.Fatalf("x11ScrollSteps(%d) unexpectedly succeeded", value)
		}
	}
	for _, button := range []byte{ButtonLeft, ButtonMiddle, ButtonRight, ButtonWheelUp, ButtonWheelDown} {
		if !x11ButtonStateObservable(button) {
			t.Fatalf("button %d state unexpectedly unobservable", button)
		}
	}
	for _, button := range []byte{ButtonWheelLeft, ButtonWheelRight} {
		if x11ButtonStateObservable(button) {
			t.Fatalf("button %d state unexpectedly observable", button)
		}
	}
}

func TestUnobservableWheelOperationsFailBeforeConnectionOrMutation(t *testing.T) {
	server := newFakeX11Server()
	backend := newFakeBackend(server, func() string { return ":fake" })
	for _, button := range []Button{ButtonWheelLeft, ButtonWheelRight} {
		if err := backend.Click(button, false); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("Click(%d) error = %v, want ErrUnsupported", button, err)
		}
	}
	for _, horizontal := range []int{-1, 1} {
		if err := backend.Scroll(horizontal, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("Scroll(%d,1) error = %v, want ErrUnsupported", horizontal, err)
		}
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.connections) != 0 {
		t.Fatalf("rejected wheel operations opened %d X11 connections", len(server.connections))
	}
	if len(server.buttons) != 0 {
		t.Fatalf("rejected wheel operations changed button state: %v", server.buttons)
	}
}

func TestSmoothMovePlanningAndOwnershipOrderHelpers(t *testing.T) {
	if err := validateX11SmoothMove(1, 2, math.NaN(), 2); err == nil {
		t.Fatal("NaN smooth-move delay unexpectedly succeeded")
	}
	if err := validateX11SmoothMove(1, 2, 3, 2); err == nil {
		t.Fatal("reversed smooth-move delay unexpectedly succeeded")
	}
	if err := validateX11SmoothMove(1, 2, 0, x11MaximumSmoothDelay+1); err == nil {
		t.Fatal("oversized smooth-move delay unexpectedly succeeded")
	}
	keys := []xproto.Keycode{8, 9, 10}
	keys = removeX11Keycode(keys, 9)
	if len(keys) != 2 || keys[0] != 8 || keys[1] != 10 {
		t.Fatalf("removeX11Keycode = %v, want [8 10]", keys)
	}
	buttons := []byte{ButtonLeft, ButtonMiddle, ButtonRight}
	buttons = removeX11Button(buttons, ButtonMiddle)
	if len(buttons) != 2 || buttons[0] != ButtonLeft || buttons[1] != ButtonRight {
		t.Fatalf("removeX11Button = %v, want [1 3]", buttons)
	}
}

func TestBackendLifecycleRestoresOwnedState(t *testing.T) {
	server := newFakeX11Server()
	backend := newFakeBackend(server, func() string { return ":fake" })
	if err := backend.Key(KeyEvent{Keysym: 0xffe1, Down: true}); err != nil {
		t.Fatalf("hold key: %v", err)
	}
	if err := backend.Toggle(ButtonRight, true); err != nil {
		t.Fatalf("hold button: %v", err)
	}
	if err := backend.Text(TextEvent{Keysyms: []uint32{0x0101f600}}); err != nil {
		t.Fatalf("install scratch mapping: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.buttons) != 0 {
		t.Fatalf("buttons remained held: %v", server.buttons)
	}
	for _, pressed := range server.pressed {
		if pressed != 0 {
			t.Fatalf("pressed keys remained after Close: %v", server.pressed)
		}
	}
	for index := 4; index < len(server.keysyms); index++ {
		if server.keysyms[index] != 0 {
			t.Fatalf("scratch mapping remained at index %d: %#x", index, server.keysyms[index])
		}
	}
}

func TestBackendPropagatesSynchronousConnectionCloseErrors(t *testing.T) {
	closeErr := errors.New("verified transport close failed")
	server := newFakeX11Server()
	server.closeErr = closeErr
	backend := newFakeBackend(server, func() string { return ":fake" })
	if err := backend.KeyboardReady(); err != nil {
		t.Fatalf("KeyboardReady: %v", err)
	}
	if err := backend.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close error = %v, want transport close error", err)
	}
}

func TestBackendJoinsOpenFailureWithConnectionCloseError(t *testing.T) {
	initErr := errors.New("XTEST init failed")
	closeErr := errors.New("failed to close rejected connection")
	server := newFakeX11Server()
	server.initErr = initErr
	server.closeErr = closeErr
	backend := newFakeBackend(server, func() string { return ":fake" })
	err := backend.KeyboardReady()
	if !errors.Is(err, ErrUnsupported) || !errors.Is(err, initErr) || !errors.Is(err, closeErr) {
		t.Fatalf("KeyboardReady error = %v, want ErrUnsupported, init, and close causes", err)
	}
}

func TestBackendStoppedEventDrainPreservesTerminalCause(t *testing.T) {
	terminalErr := errors.New("terminal X11 transport failure")
	done := make(chan struct{})
	close(done)
	backend := New(Config{})
	backend.events = done
	backend.eventMu.Lock()
	backend.eventErr = terminalErr
	backend.eventMu.Unlock()

	err := backend.eventDrainErrorLocked()
	if !errors.Is(err, terminalErr) {
		t.Fatalf("stopped event-drain error = %v, want terminal cause", err)
	}
}

func TestBackendTransportFailuresCloseAndRecover(t *testing.T) {
	server := newFakeX11Server()
	backend := newFakeBackend(server, func() string { return ":fake" })
	inputErr := errors.New("fake XTEST injection failed")
	server.fakeInputErr = inputErr
	if err := backend.MoveRelative(1, 1); !errors.Is(err, inputErr) {
		t.Fatalf("MoveRelative error = %v, want fake input failure", err)
	}
	server.mu.Lock()
	server.fakeInputErr = nil
	server.mu.Unlock()
	if err := backend.MoveRelative(1, 1); err != nil {
		t.Fatalf("MoveRelative after reconnect: %v", err)
	}

	grabErr := errors.New("fake server grab failed")
	server.mu.Lock()
	server.grabErr = grabErr
	server.mu.Unlock()
	if err := backend.Click(ButtonLeft, false); !errors.Is(err, grabErr) {
		t.Fatalf("Click grab error = %v, want fake grab failure", err)
	}
	server.mu.Lock()
	server.grabErr = nil
	server.mu.Unlock()
	if err := backend.Click(ButtonLeft, false); err != nil {
		t.Fatalf("Click after grab recovery: %v", err)
	}

	ungrabErr := errors.New("fake server ungrab failed")
	server.mu.Lock()
	server.ungrabErr = ungrabErr
	server.mu.Unlock()
	if err := backend.Click(ButtonLeft, false); !errors.Is(err, ungrabErr) {
		t.Fatalf("Click ungrab error = %v, want fake ungrab failure", err)
	}
	server.mu.Lock()
	server.ungrabErr = nil
	server.mu.Unlock()
	if err := backend.Click(ButtonLeft, false); err != nil {
		t.Fatalf("Click after ungrab recovery: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("final Close: %v", err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.buttons) != 0 {
		t.Fatalf("transport failures left held buttons: %v", server.buttons)
	}
	for _, pressed := range server.pressed {
		if pressed != 0 {
			t.Fatalf("transport failures left held keys: %v", server.pressed)
		}
	}
}

func TestBackendScratchCleanupFailureCanRetry(t *testing.T) {
	server := newFakeX11Server()
	backend := newFakeBackend(server, func() string { return ":fake" })
	const keysym = uint32(0x0101f600)
	if err := backend.Text(TextEvent{Keysyms: []uint32{keysym}}); err != nil {
		t.Fatalf("install scratch mapping: %v", err)
	}
	server.mu.Lock()
	var scratchCode xproto.Keycode
	for offset := 0; offset < len(server.keysyms); offset += int(server.perKeycode) {
		if uint32(server.keysyms[offset]) == keysym {
			scratchCode = server.setup.MinKeycode + xproto.Keycode(offset/int(server.perKeycode))
			break
		}
	}
	if scratchCode == 0 {
		server.mu.Unlock()
		t.Fatal("scratch mapping was not installed")
	}
	server.pressed[scratchCode/8] |= 1 << uint(scratchCode%8)
	server.mu.Unlock()
	if err := backend.Close(); err == nil {
		t.Fatal("Close unexpectedly restored a pressed scratch keycode")
	}
	server.mu.Lock()
	server.pressed[scratchCode/8] &^= 1 << uint(scratchCode%8)
	server.mu.Unlock()
	if err := backend.Close(); err != nil {
		t.Fatalf("retry Close after releasing scratch keycode: %v", err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	for _, value := range server.keysyms {
		if uint32(value) == keysym {
			t.Fatalf("scratch mapping remained after retry: %v", server.keysyms)
		}
	}
}

func TestBackendConcurrentLifecycleIsRaceSafe(t *testing.T) {
	server := newFakeX11Server()
	var selected atomic.Uint32
	backend := newFakeBackend(server, func() string {
		if selected.Load()%2 == 0 {
			return ":fake-a"
		}
		return ":fake-b"
	})
	const workers = 12
	const iterations = 60
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				selected.Store(uint32(worker + iteration))
				switch (worker + iteration) % 14 {
				case 0:
					_ = backend.KeyboardReady()
				case 1:
					_ = backend.MouseReady()
				case 2:
					_ = backend.Key(KeyEvent{Keysym: 0xff0d, Tap: true, Modifiers: []uint32{0xffe1}})
				case 3:
					_ = backend.Text(TextEvent{Keysyms: []uint32{0x0101f600}})
				case 4:
					_ = backend.MoveAbsolute(iteration, worker, nil)
				case 5:
					_ = backend.MoveRelative(1, -1)
				case 6:
					_ = backend.MoveSmooth(2, 1, true, 0, 0)
				case 7:
					_ = backend.DragSmooth(iteration, worker, 0, 0)
				case 8:
					_ = backend.Toggle(ButtonRight, true)
					_ = backend.Toggle(ButtonRight, false)
				case 9:
					_ = backend.Key(KeyEvent{Keysym: 0xff0d, Down: true})
					_ = backend.Key(KeyEvent{Keysym: 0xff0d})
				case 10:
					_ = backend.Scroll(0, 1)
				case 11:
					_ = backend.Click(ButtonLeft, false)
				case 12:
					_, _, _ = backend.Location()
				case 13:
					_ = backend.Close()
				}
			}
		}(worker)
	}
	wait.Wait()
	if err := backend.Close(); err != nil {
		t.Fatalf("final Close: %v", err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.buttons) != 0 {
		t.Fatalf("concurrent lifecycle left held buttons: %v", server.buttons)
	}
	for _, pressed := range server.pressed {
		if pressed != 0 {
			t.Fatalf("concurrent lifecycle left held keys: %v", server.pressed)
		}
	}
	for index := 4; index < len(server.keysyms); index++ {
		if server.keysyms[index] != 0 {
			t.Fatalf("concurrent lifecycle left scratch mapping at index %d: %#x", index, server.keysyms[index])
		}
	}
}
