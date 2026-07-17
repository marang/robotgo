//go:build linux

package x11input

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
	"golang.org/x/sys/unix"
)

const (
	guardianTestEnvironmentOnly = "ROBOTGO_TEST_X11_GUARDIAN_ENVIRONMENT_ONLY"
	guardianTestPIDFile         = "ROBOTGO_TEST_X11_GUARDIAN_PID_FILE"
	guardianTestFDFile          = "ROBOTGO_TEST_X11_GUARDIAN_FD_FILE"
	guardianTestBlockingProcess = "ROBOTGO_TEST_X11_GUARDIAN_BLOCKING_PROCESS"
)

type guardianTestEvent struct {
	open bool
	err  error
}

type guardianTestInput struct {
	eventType byte
	detail    byte
}

type guardianTestConnection struct {
	mu sync.Mutex

	setup                       Setup
	version                     XTestVersion
	mappings                    map[xproto.Keycode]KeyboardMapping
	modifiers                   []xproto.Keycode
	pressed                     []byte
	pointer                     PointerState
	inputs                      []guardianTestInput
	grabbed                     bool
	closed                      bool
	grabCount                   int
	ungrabCount                 int
	clearPressedOnUngrab        bool
	canonicalizeMapping         func(KeyboardMapping) KeyboardMapping
	blockOperation              string
	blockEntered                chan struct{}
	blockRelease                chan struct{}
	blockClose                  chan struct{}
	operationFailures           map[string][]error
	fakeInputFailures           map[guardianTestInput]int
	changeFailuresAfterMutation []error

	events           chan guardianTestEvent
	closeOnce        sync.Once
	blockEnteredOnce sync.Once
	blockReleaseOnce sync.Once
}

type guardianTestDialer struct {
	connection Connection
	display    string
	block      <-chan struct{}
}

func (dialer *guardianTestDialer) Dial(display string) (Connection, error) {
	dialer.display = display
	if dialer.block != nil {
		<-dialer.block
	}
	return dialer.connection, nil
}

func newGuardianTestConnection() *guardianTestConnection {
	return &guardianTestConnection{
		setup:   Setup{Root: 1, MinKeycode: 8, MaxKeycode: 255},
		version: XTestVersion{Major: 2, Minor: 2, Valid: true},
		mappings: map[xproto.Keycode]KeyboardMapping{
			10: {KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{0, 0}},
			11: {KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{'a', 'A'}},
		},
		pressed:           make([]byte, 32),
		pointer:           PointerState{RootX: 40, RootY: 50},
		events:            make(chan guardianTestEvent, 8),
		operationFailures: make(map[string][]error),
		fakeInputFailures: make(map[guardianTestInput]int),
	}
}

func (connection *guardianTestConnection) popOperationFailureLocked(operation string) error {
	failures := connection.operationFailures[operation]
	if len(failures) == 0 {
		return nil
	}
	failure := failures[0]
	if len(failures) == 1 {
		delete(connection.operationFailures, operation)
	} else {
		connection.operationFailures[operation] = failures[1:]
	}
	return failure
}

func (connection *guardianTestConnection) Close() error {
	if connection.blockRelease != nil {
		connection.blockReleaseOnce.Do(func() { close(connection.blockRelease) })
	}
	if connection.blockClose != nil {
		<-connection.blockClose
	}
	connection.closeOnce.Do(func() {
		connection.mu.Lock()
		connection.closed = true
		connection.mu.Unlock()
		close(connection.events)
	})
	return nil
}

func (connection *guardianTestConnection) waitIfBlocked(operation string) {
	if connection.blockOperation != operation || connection.blockRelease == nil {
		return
	}
	if connection.blockEntered != nil {
		connection.blockEnteredOnce.Do(func() { close(connection.blockEntered) })
	}
	<-connection.blockRelease
}

func (connection *guardianTestConnection) WaitForEvent() (bool, error) {
	event, open := <-connection.events
	if !open {
		return false, nil
	}
	return event.open, event.err
}

func (connection *guardianTestConnection) Setup() (Setup, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.setup, nil
}

func (*guardianTestConnection) InitXTest() error { return nil }

func (connection *guardianTestConnection) XTestVersion(byte, uint16) (XTestVersion, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.version, nil
}

func (connection *guardianTestConnection) GrabServer() error {
	connection.waitIfBlocked(guardianOperationGrabServer)
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.grabbed {
		return errors.New("already grabbed")
	}
	connection.grabbed = true
	connection.grabCount++
	return nil
}

func (connection *guardianTestConnection) UngrabServer() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if !connection.grabbed {
		return errors.New("not grabbed")
	}
	connection.grabbed = false
	connection.ungrabCount++
	if connection.clearPressedOnUngrab {
		clear(connection.pressed)
	}
	return nil
}

func (connection *guardianTestConnection) KeyboardMapping(first xproto.Keycode, count byte) (KeyboardMapping, error) {
	connection.waitIfBlocked(guardianOperationKeyboardMapping)
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.popOperationFailureLocked(guardianOperationKeyboardMapping); err != nil {
		return KeyboardMapping{}, err
	}
	if count != 1 {
		return KeyboardMapping{}, errors.New("test transport supports one keycode")
	}
	mapping, ok := connection.mappings[first]
	if !ok {
		return KeyboardMapping{}, errors.New("mapping is absent")
	}
	return cloneGuardianMapping(mapping), nil
}

func (connection *guardianTestConnection) ModifierMapping() ([]xproto.Keycode, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.popOperationFailureLocked(guardianOperationModifierMapping); err != nil {
		return nil, err
	}
	return append([]xproto.Keycode(nil), connection.modifiers...), nil
}

func (connection *guardianTestConnection) ChangeKeyboardMapping(first xproto.Keycode, perKeycode byte, keysyms []xproto.Keysym) error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.popOperationFailureLocked(guardianOperationChangeKeyboardMapping); err != nil {
		return err
	}
	mapping := KeyboardMapping{
		KeysymsPerKeycode: perKeycode,
		Keysyms:           append([]xproto.Keysym(nil), keysyms...),
	}
	if connection.canonicalizeMapping != nil {
		mapping = connection.canonicalizeMapping(mapping)
	}
	connection.mappings[first] = cloneGuardianMapping(mapping)
	if len(connection.changeFailuresAfterMutation) > 0 {
		err := connection.changeFailuresAfterMutation[0]
		connection.changeFailuresAfterMutation = connection.changeFailuresAfterMutation[1:]
		return err
	}
	return nil
}

func (connection *guardianTestConnection) PressedKeys() ([]byte, error) {
	connection.waitIfBlocked(guardianOperationPressedKeys)
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if err := connection.popOperationFailureLocked(guardianOperationPressedKeys); err != nil {
		return nil, err
	}
	return append([]byte(nil), connection.pressed...), nil
}

func (connection *guardianTestConnection) QueryPointer(xproto.Window) (PointerState, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.pointer, nil
}

func (connection *guardianTestConnection) FakeInput(eventType, detail byte, _ xproto.Window, _, _ int16) error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	input := guardianTestInput{eventType: eventType, detail: detail}
	connection.inputs = append(connection.inputs, input)
	switch eventType {
	case byte(xproto.KeyPress):
		connection.pressed[int(detail)/8] |= 1 << uint(detail%8)
	case byte(xproto.KeyRelease):
		connection.pressed[int(detail)/8] &^= 1 << uint(detail%8)
	case byte(xproto.ButtonPress):
		connection.pointer.Mask |= 1 << (7 + detail)
	case byte(xproto.ButtonRelease):
		connection.pointer.Mask &^= 1 << (7 + detail)
	}
	if remaining := connection.fakeInputFailures[input]; remaining > 0 {
		if remaining == 1 {
			delete(connection.fakeInputFailures, input)
		} else {
			connection.fakeInputFailures[input] = remaining - 1
		}
		return errors.New("injected mutate-then-error FakeInput failure")
	}
	return nil
}

func newInProcessGuardian(t *testing.T, transport *guardianTestConnection) (*guardianConnection, <-chan error) {
	return newInProcessGuardianWithOptions(t, transport, GuardianOptions{
		RequestTimeout: time.Second,
		CleanupTimeout: 100 * time.Millisecond,
	})
}

func newInProcessGuardianWithOptions(
	t *testing.T,
	transport *guardianTestConnection,
	options GuardianOptions,
) (*guardianConnection, <-chan error) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("create guardian test socket: %v", err)
	}
	parent := os.NewFile(uintptr(fds[0]), "guardian-test-parent")
	child := os.NewFile(uintptr(fds[1]), "guardian-test-child")
	connection := newGuardianConnection(parent, nil, options)
	done := make(chan error, 1)
	token := strings.Repeat("ab", 32)
	dialer := &guardianTestDialer{connection: transport}
	go func() {
		done <- serveGuardian(child, token, dialer)
		_ = child.Close()
		close(done)
	}()
	hello := guardianHelloRequest{
		Token:              token,
		Display:            ":guardian-test",
		RequestTimeoutNano: int64(options.RequestTimeout),
		CleanupTimeoutNano: int64(options.CleanupTimeout),
		CrashSettleNano:    0,
	}
	if err := connection.request(guardianOperationHello, hello, nil); err != nil {
		t.Fatalf("guardian hello: %v", err)
	}
	return connection, done
}

func TestGuardianConnectionProxiesAndMultiplexesEvents(t *testing.T) {
	transport := newGuardianTestConnection()
	connection, done := newInProcessGuardian(t, transport)

	setup, err := connection.Setup()
	if err != nil || setup != transport.setup {
		t.Fatalf("Setup = %+v, %v; want %+v", setup, err, transport.setup)
	}
	if err := connection.InitXTest(); err != nil {
		t.Fatalf("InitXTest: %v", err)
	}
	version, err := connection.XTestVersion(2, 2)
	if err != nil || version != transport.version {
		t.Fatalf("XTestVersion = %+v, %v; want %+v", version, err, transport.version)
	}
	mapping, err := connection.KeyboardMapping(11, 1)
	if err != nil || !guardianMappingsEqual(mapping, transport.mappings[11]) {
		t.Fatalf("KeyboardMapping = %+v, %v", mapping, err)
	}
	state, err := connection.QueryPointer(1)
	if err != nil || state != transport.pointer {
		t.Fatalf("QueryPointer = %+v, %v; want %+v", state, err, transport.pointer)
	}

	transport.events <- guardianTestEvent{open: true}
	open, err := connection.WaitForEvent()
	if err != nil || !open {
		t.Fatalf("WaitForEvent = %t, %v; want true, nil", open, err)
	}
	if err := connection.GrabServer(); err != nil {
		t.Fatalf("GrabServer: %v", err)
	}
	if err := connection.ChangeKeyboardMapping(10, 2, []xproto.Keysym{0x0101f600, 0x0101f600}); err != nil {
		t.Fatalf("ChangeKeyboardMapping: %v", err)
	}
	if err := connection.FakeInput(byte(xproto.KeyPress), 10, 1, 0, 0); err != nil {
		t.Fatalf("FakeInput press: %v", err)
	}
	if err := connection.FakeInput(byte(xproto.KeyRelease), 10, 1, 0, 0); err != nil {
		t.Fatalf("FakeInput release: %v", err)
	}
	if err := connection.UngrabServer(); err != nil {
		t.Fatalf("UngrabServer: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server: %v", err)
	}

	transport.mu.Lock()
	restored := cloneGuardianMapping(transport.mappings[10])
	closed := transport.closed
	grabbed := transport.grabbed
	transport.mu.Unlock()
	if !guardianMappingsEqual(restored, KeyboardMapping{KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{0, 0}}) {
		t.Fatalf("mapping after verified close = %+v", restored)
	}
	if !closed || grabbed {
		t.Fatalf("transport close state: closed=%t grabbed=%t", closed, grabbed)
	}
}

func TestGuardianConcurrentRequestsRemainCorrelated(t *testing.T) {
	transport := newGuardianTestConnection()
	connection, done := newInProcessGuardian(t, transport)

	const workers = 64
	start := make(chan struct{})
	failures := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := range workers {
		go func() {
			defer group.Done()
			<-start
			if worker%2 == 0 {
				setup, err := connection.Setup()
				if err != nil {
					failures <- fmt.Errorf("worker %d Setup: %w", worker, err)
				} else if setup != transport.setup {
					failures <- fmt.Errorf("worker %d Setup = %+v, want %+v", worker, setup, transport.setup)
				}
				return
			}
			state, err := connection.QueryPointer(transport.setup.Root)
			if err != nil {
				failures <- fmt.Errorf("worker %d QueryPointer: %w", worker, err)
			} else if state != transport.pointer {
				failures <- fmt.Errorf("worker %d QueryPointer = %+v, want %+v", worker, state, transport.pointer)
			}
		}()
	}
	close(start)
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close after concurrent requests: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server after concurrent requests: %v", err)
	}
}

func TestGuardianFramedWriterPayloadRoundTrips(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload any
		check   func(*testing.T, guardianEnvelope)
	}{
		{
			name: "omitted payload",
			check: func(t *testing.T, envelope guardianEnvelope) {
				t.Helper()
				if len(envelope.Payload) != 0 {
					t.Fatalf("nil payload encoded as %q, want omitted", envelope.Payload)
				}
			},
		},
		{
			name: "typed payload",
			payload: guardianFakeInputRequest{
				EventType: byte(xproto.MotionNotify),
				Detail:    x11RelativeMotion,
				Root:      42,
				X:         -17,
				Y:         23,
			},
			check: func(t *testing.T, envelope guardianEnvelope) {
				t.Helper()
				var got guardianFakeInputRequest
				if err := decodeGuardianPayload(envelope.Payload, &got); err != nil {
					t.Fatalf("decode typed payload: %v", err)
				}
				want := guardianFakeInputRequest{
					EventType: byte(xproto.MotionNotify),
					Detail:    x11RelativeMotion,
					Root:      42,
					X:         -17,
					Y:         23,
				}
				if got != want {
					t.Fatalf("typed payload = %+v, want %+v", got, want)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var framed strings.Builder
			writer := guardianFramedWriter{writer: &framed}
			wantEnvelope := guardianEnvelope{
				Version:   guardianProtocolVersion,
				Kind:      guardianFrameRequest,
				ID:        27,
				Operation: guardianOperationFakeInput,
			}
			if err := writer.writePayload(wantEnvelope, test.payload); err != nil {
				t.Fatalf("write payload frame: %v", err)
			}
			gotEnvelope, err := readGuardianEnvelope(strings.NewReader(framed.String()))
			if err != nil {
				t.Fatalf("read payload frame: %v", err)
			}
			if gotEnvelope.Version != wantEnvelope.Version ||
				gotEnvelope.Kind != wantEnvelope.Kind ||
				gotEnvelope.ID != wantEnvelope.ID ||
				gotEnvelope.Operation != wantEnvelope.Operation {
				t.Fatalf("frame envelope = %+v, want metadata %+v", gotEnvelope, wantEnvelope)
			}
			test.check(t, gotEnvelope)
		})
	}
}

func TestGuardianEOFReleasesOwnedInputAndRestoresMapping(t *testing.T) {
	transport := newGuardianTestConnection()
	connection, done := newInProcessGuardian(t, transport)
	if err := connection.GrabServer(); err != nil {
		t.Fatalf("GrabServer: %v", err)
	}
	if err := connection.ChangeKeyboardMapping(10, 2, []xproto.Keysym{0x0101f600, 0x0101f600}); err != nil {
		t.Fatalf("ChangeKeyboardMapping: %v", err)
	}
	if err := connection.FakeInput(byte(xproto.KeyPress), 10, 1, 0, 0); err != nil {
		t.Fatalf("FakeInput key press: %v", err)
	}
	if err := connection.FakeInput(byte(xproto.ButtonPress), 1, 1, 0, 0); err != nil {
		t.Fatalf("FakeInput button press: %v", err)
	}
	connection.finish(errors.New("simulated parent crash"))
	if err := <-done; err != nil {
		t.Fatalf("guardian EOF cleanup: %v", err)
	}

	transport.mu.Lock()
	mapping := cloneGuardianMapping(transport.mappings[10])
	pressed := append([]byte(nil), transport.pressed...)
	mask := transport.pointer.Mask
	grabbed := transport.grabbed
	inputs := append([]guardianTestInput(nil), transport.inputs...)
	transport.mu.Unlock()
	if !guardianMappingsEqual(mapping, KeyboardMapping{KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{0, 0}}) {
		t.Fatalf("mapping after parent EOF = %+v", mapping)
	}
	if guardianKeycodePressed(pressed, 10) || mask != 0 || grabbed {
		t.Fatalf("state after parent EOF: pressed=%t mask=%#x grabbed=%t", guardianKeycodePressed(pressed, 10), mask, grabbed)
	}
	wantSuffix := []guardianTestInput{
		{eventType: byte(xproto.ButtonRelease), detail: 1},
		{eventType: byte(xproto.KeyRelease), detail: 10},
	}
	if len(inputs) < len(wantSuffix) {
		t.Fatalf("input log too short: %+v", inputs)
	}
	gotSuffix := inputs[len(inputs)-len(wantSuffix):]
	for index := range wantSuffix {
		if gotSuffix[index] != wantSuffix[index] {
			t.Fatalf("cleanup input suffix = %+v, want %+v", gotSuffix, wantSuffix)
		}
	}
}

func TestGuardianCleanupPreservesForeignMapping(t *testing.T) {
	transport := newGuardianTestConnection()
	connection, done := newInProcessGuardian(t, transport)
	if err := connection.ChangeKeyboardMapping(10, 2, []xproto.Keysym{0x0101f600, 0x0101f600}); err != nil {
		t.Fatalf("ChangeKeyboardMapping: %v", err)
	}
	foreign := KeyboardMapping{KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{'z', 'z'}}
	transport.mu.Lock()
	transport.mappings[10] = cloneGuardianMapping(foreign)
	transport.mu.Unlock()
	if err := connection.Close(); err != nil {
		t.Fatalf("Close preserving foreign mapping: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	transport.mu.Unlock()
	if !guardianMappingsEqual(got, foreign) {
		t.Fatalf("foreign mapping after cleanup = %+v, want %+v", got, foreign)
	}
}

func TestGuardianAcceptsServerCanonicalizedScratchMapping(t *testing.T) {
	transport := newGuardianTestConnection()
	transport.canonicalizeMapping = func(mapping KeyboardMapping) KeyboardMapping {
		canonical := cloneGuardianMapping(mapping)
		if len(canonical.Keysyms) > 2 && canonical.Keysyms[0] != 0 {
			canonical.Keysyms[len(canonical.Keysyms)-1] = 0
		}
		return canonical
	}
	transport.mappings[10] = KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	connection, done := newInProcessGuardian(t, transport)
	keysym := xproto.Keysym(0x010020ac)
	if err := connection.ChangeKeyboardMapping(10, 3, []xproto.Keysym{keysym, keysym, keysym}); err != nil {
		t.Fatalf("ChangeKeyboardMapping with canonicalized readback: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close after canonicalized mapping: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	transport.mu.Unlock()
	want := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	if !guardianMappingsEqual(got, want) {
		t.Fatalf("mapping after canonicalized cleanup = %+v, want %+v", got, want)
	}
}

func TestGuardianRestoresCanonicalizedMappingAfterLostChangeReply(t *testing.T) {
	transport := newGuardianTestConnection()
	transport.canonicalizeMapping = func(mapping KeyboardMapping) KeyboardMapping {
		canonical := cloneGuardianMapping(mapping)
		if len(canonical.Keysyms) > 2 && canonical.Keysyms[0] != 0 {
			canonical.Keysyms[len(canonical.Keysyms)-1] = 0
		}
		return canonical
	}
	before := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	transport.mappings[10] = cloneGuardianMapping(before)
	lostReply := errors.New("lost mapping-change reply")
	transport.changeFailuresAfterMutation = []error{lostReply}
	connection, done := newInProcessGuardian(t, transport)
	if err := connection.GrabServer(); err != nil {
		t.Fatalf("GrabServer before ambiguous mapping change: %v", err)
	}
	keysym := xproto.Keysym(0x010020ac)
	err := connection.ChangeKeyboardMapping(10, 3, []xproto.Keysym{keysym, keysym, keysym})
	if err == nil || !strings.Contains(err.Error(), lostReply.Error()) {
		t.Fatalf("canonicalized mapping change error = %v; want lost reply", err)
	}
	if err := connection.UngrabServer(); err != nil {
		t.Fatalf("UngrabServer after ambiguous mapping change: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close after ambiguous canonicalized mapping change: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server after ambiguous canonicalized mapping change: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	transport.mu.Unlock()
	if !guardianMappingsEqual(got, before) {
		t.Fatalf("mapping after ambiguous canonicalized cleanup = %+v, want %+v", got, before)
	}
}

func TestGuardianPreservesCanonicalizedMappingWhenChangeAndReadbackAreLost(t *testing.T) {
	transport := newGuardianTestConnection()
	transport.canonicalizeMapping = func(mapping KeyboardMapping) KeyboardMapping {
		canonical := cloneGuardianMapping(mapping)
		if len(canonical.Keysyms) > 2 && canonical.Keysyms[0] != 0 {
			canonical.Keysyms[len(canonical.Keysyms)-1] = 0
		}
		return canonical
	}
	before := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	transport.mappings[10] = cloneGuardianMapping(before)
	transport.operationFailures[guardianOperationKeyboardMapping] = []error{
		nil,
		errors.New("lost mapping readback"),
	}
	transport.changeFailuresAfterMutation = []error{errors.New("lost mapping-change reply")}
	connection, done := newInProcessGuardian(t, transport)
	if err := connection.GrabServer(); err != nil {
		t.Fatalf("GrabServer before doubly ambiguous mapping change: %v", err)
	}
	keysym := xproto.Keysym(0x010020ac)
	err := connection.ChangeKeyboardMapping(10, 3, []xproto.Keysym{keysym, keysym, keysym})
	if err == nil || !strings.Contains(err.Error(), "lost mapping-change reply") ||
		!strings.Contains(err.Error(), "lost mapping readback") {
		t.Fatalf("doubly ambiguous mapping change error = %v; want both causes", err)
	}
	if err := connection.UngrabServer(); err != nil {
		t.Fatalf("UngrabServer after doubly ambiguous mapping change: %v", err)
	}
	if err := connection.Close(); err == nil ||
		(!strings.Contains(err.Error(), "ownership remains unresolved") && !strings.Contains(err.Error(), "cleanup timed out")) {
		t.Fatalf("Close after doubly ambiguous mapping change = %v; want explicit cleanup failure", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server after doubly ambiguous mapping change: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	transport.mu.Unlock()
	want := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{keysym, keysym, 0}}
	if !guardianMappingsEqual(got, want) {
		t.Fatalf("mapping after doubly ambiguous cleanup = %+v, want preserved %+v", got, want)
	}
}

func TestGuardianPreservesCanonicalizedMappingWhenVerificationReadbackIsLost(t *testing.T) {
	transport := newGuardianTestConnection()
	transport.canonicalizeMapping = func(mapping KeyboardMapping) KeyboardMapping {
		canonical := cloneGuardianMapping(mapping)
		if len(canonical.Keysyms) > 2 && canonical.Keysyms[0] != 0 {
			canonical.Keysyms[len(canonical.Keysyms)-1] = 0
		}
		return canonical
	}
	before := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	transport.mappings[10] = cloneGuardianMapping(before)
	transport.operationFailures[guardianOperationKeyboardMapping] = []error{
		nil,
		errors.New("lost verification readback"),
	}
	connection, done := newInProcessGuardian(t, transport)
	if err := connection.GrabServer(); err != nil {
		t.Fatalf("GrabServer before lost verification readback: %v", err)
	}
	keysym := xproto.Keysym(0x010020ac)
	err := connection.ChangeKeyboardMapping(10, 3, []xproto.Keysym{keysym, keysym, keysym})
	if err == nil || !strings.Contains(err.Error(), "lost verification readback") {
		t.Fatalf("mapping verification error = %v; want lost readback", err)
	}
	if err := connection.UngrabServer(); err != nil {
		t.Fatalf("UngrabServer after lost verification readback: %v", err)
	}
	if err := connection.Close(); err == nil ||
		(!strings.Contains(err.Error(), "ownership remains unresolved") && !strings.Contains(err.Error(), "cleanup timed out")) {
		t.Fatalf("Close after lost verification readback = %v; want explicit cleanup failure", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server after lost verification readback: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	transport.mu.Unlock()
	want := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{keysym, keysym, 0}}
	if !guardianMappingsEqual(got, want) {
		t.Fatalf("mapping after lost verification readback = %+v, want preserved %+v", got, want)
	}
}

func TestGuardianReportsUnresolvedFailedMappingWithoutOverwrite(t *testing.T) {
	transport := newGuardianTestConnection()
	before := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	foreign := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{'z', 0, 0}}
	transport.mappings[10] = cloneGuardianMapping(before)
	transport.canonicalizeMapping = func(mapping KeyboardMapping) KeyboardMapping {
		if len(mapping.Keysyms) > 0 && mapping.Keysyms[0] != 0 {
			return cloneGuardianMapping(foreign)
		}
		return cloneGuardianMapping(mapping)
	}
	transport.changeFailuresAfterMutation = []error{errors.New("ambiguous mapping failure")}
	connection, done := newInProcessGuardianWithOptions(t, transport, GuardianOptions{
		RequestTimeout: 100 * time.Millisecond,
		CleanupTimeout: 25 * time.Millisecond,
	})
	keysym := xproto.Keysym(0x010020ac)
	err := connection.ChangeKeyboardMapping(10, 3, []xproto.Keysym{keysym, keysym, keysym})
	if err == nil || !strings.Contains(err.Error(), "ownership is unresolved") {
		t.Fatalf("unresolved mapping change error = %v; want explicit ownership error", err)
	}
	err = connection.ChangeKeyboardMapping(10, before.KeysymsPerKeycode, before.Keysyms)
	if err == nil || !strings.Contains(err.Error(), "refuses to restore unresolved mapping") {
		t.Fatalf("unresolved mapping restore error = %v; want explicit refusal", err)
	}
	if err := connection.Close(); err == nil ||
		(!strings.Contains(err.Error(), "ownership remains unresolved") && !strings.Contains(err.Error(), "cleanup timed out")) {
		t.Fatalf("Close with unresolved mapping = %v; want explicit ownership or cleanup-timeout error", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server after unresolved mapping close: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	transport.mu.Unlock()
	if !guardianMappingsEqual(got, foreign) {
		t.Fatalf("unresolved mapping after cleanup = %+v, want preserved %+v", got, foreign)
	}
}

func TestGuardianPreservesSemanticallyEqualForeignMapping(t *testing.T) {
	transport := newGuardianTestConnection()
	transport.canonicalizeMapping = func(mapping KeyboardMapping) KeyboardMapping {
		canonical := cloneGuardianMapping(mapping)
		if len(canonical.Keysyms) > 2 && canonical.Keysyms[0] != 0 {
			canonical.Keysyms[len(canonical.Keysyms)-1] = 0
		}
		return canonical
	}
	transport.mappings[10] = KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	connection, done := newInProcessGuardian(t, transport)
	keysym := xproto.Keysym(0x010020ac)
	if err := connection.ChangeKeyboardMapping(10, 3, []xproto.Keysym{keysym, keysym, keysym}); err != nil {
		t.Fatalf("ChangeKeyboardMapping with canonicalized readback: %v", err)
	}
	foreign := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{keysym, 0, 0}}
	transport.mu.Lock()
	transport.mappings[10] = cloneGuardianMapping(foreign)
	transport.mu.Unlock()
	if err := connection.Close(); err != nil {
		t.Fatalf("Close preserving semantically equal foreign mapping: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	transport.mu.Unlock()
	if !guardianMappingsEqual(got, foreign) {
		t.Fatalf("semantically equal foreign mapping after cleanup = %+v, want %+v", got, foreign)
	}
}

func TestGuardianCleanupReleasesAmbiguousFailedPresses(t *testing.T) {
	tests := []struct {
		name        string
		pressType   byte
		releaseType byte
		detail      byte
		held        func(*guardianTestConnection) bool
		owned       func(*guardianServer) bool
	}{
		{
			name:        "key",
			pressType:   byte(xproto.KeyPress),
			releaseType: byte(xproto.KeyRelease),
			detail:      10,
			held: func(transport *guardianTestConnection) bool {
				return guardianKeycodePressed(transport.pressed, 10)
			},
			owned: func(server *guardianServer) bool {
				_, ok := server.ownedKeys[10]
				return ok
			},
		},
		{
			name:        "button",
			pressType:   byte(xproto.ButtonPress),
			releaseType: byte(xproto.ButtonRelease),
			detail:      1,
			held: func(transport *guardianTestConnection) bool {
				return transport.pointer.Mask&(1<<(7+1)) != 0
			},
			owned: func(server *guardianServer) bool {
				_, ok := server.ownedButtons[1]
				return ok
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newGuardianTestConnection()
			server := &guardianServer{
				connection:     transport,
				cleanupTimeout: 100 * time.Millisecond,
				mappings:       make(map[xproto.Keycode]*guardianMappingClaim),
				ownedKeys:      make(map[byte]guardianOwnedInput),
				ownedButtons:   make(map[byte]guardianOwnedInput),
			}
			press := guardianTestInput{eventType: test.pressType, detail: test.detail}
			transport.fakeInputFailures[press] = 1
			request := guardianFakeInputRequest{EventType: test.pressType, Detail: test.detail, Root: 1}
			if err := server.fakeInput(request); err == nil {
				t.Fatal("ambiguous press unexpectedly succeeded")
			}
			transport.mu.Lock()
			heldAfterError := test.held(transport)
			transport.mu.Unlock()
			if !heldAfterError || !test.owned(server) || len(server.inputOrder) != 1 {
				t.Fatalf("state after ambiguous press: held=%t owned=%t order=%d", heldAfterError, test.owned(server), len(server.inputOrder))
			}
			if err := server.cleanup(false); err != nil {
				t.Fatalf("cleanup ambiguous press: %v", err)
			}
			transport.mu.Lock()
			heldAfterCleanup := test.held(transport)
			inputs := append([]guardianTestInput(nil), transport.inputs...)
			transport.mu.Unlock()
			if heldAfterCleanup || test.owned(server) || len(server.inputOrder) != 0 {
				t.Fatalf("state after cleanup: held=%t owned=%t order=%d", heldAfterCleanup, test.owned(server), len(server.inputOrder))
			}
			want := []guardianTestInput{
				{eventType: test.pressType, detail: test.detail},
				{eventType: test.releaseType, detail: test.detail},
			}
			if len(inputs) != len(want) || inputs[0] != want[0] || inputs[1] != want[1] {
				t.Fatalf("input log = %+v, want %+v", inputs, want)
			}
		})
	}
}

func TestGuardianCleanupRetainsAmbiguousFailedReleases(t *testing.T) {
	tests := []struct {
		name        string
		pressType   byte
		releaseType byte
		detail      byte
		owned       func(*guardianServer) bool
	}{
		{
			name:        "key",
			pressType:   byte(xproto.KeyPress),
			releaseType: byte(xproto.KeyRelease),
			detail:      10,
			owned: func(server *guardianServer) bool {
				_, ok := server.ownedKeys[10]
				return ok
			},
		},
		{
			name:        "button",
			pressType:   byte(xproto.ButtonPress),
			releaseType: byte(xproto.ButtonRelease),
			detail:      1,
			owned: func(server *guardianServer) bool {
				_, ok := server.ownedButtons[1]
				return ok
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newGuardianTestConnection()
			server := &guardianServer{
				connection:     transport,
				cleanupTimeout: 100 * time.Millisecond,
				mappings:       make(map[xproto.Keycode]*guardianMappingClaim),
				ownedKeys:      make(map[byte]guardianOwnedInput),
				ownedButtons:   make(map[byte]guardianOwnedInput),
			}
			if err := server.fakeInput(guardianFakeInputRequest{EventType: test.pressType, Detail: test.detail, Root: 1}); err != nil {
				t.Fatalf("press: %v", err)
			}
			release := guardianTestInput{eventType: test.releaseType, detail: test.detail}
			transport.fakeInputFailures[release] = 1
			if err := server.fakeInput(guardianFakeInputRequest{EventType: test.releaseType, Detail: test.detail, Root: 1}); err == nil {
				t.Fatal("ambiguous release unexpectedly succeeded")
			}
			if !test.owned(server) || len(server.inputOrder) != 1 {
				t.Fatalf("ownership after ambiguous release: owned=%t order=%d", test.owned(server), len(server.inputOrder))
			}
			if err := server.cleanup(false); err != nil {
				t.Fatalf("cleanup ambiguous release: %v", err)
			}
			if test.owned(server) || len(server.inputOrder) != 0 {
				t.Fatalf("ownership after cleanup: owned=%t order=%d", test.owned(server), len(server.inputOrder))
			}
			transport.mu.Lock()
			inputs := append([]guardianTestInput(nil), transport.inputs...)
			transport.mu.Unlock()
			want := []guardianTestInput{
				{eventType: test.pressType, detail: test.detail},
				{eventType: test.releaseType, detail: test.detail},
				{eventType: test.releaseType, detail: test.detail},
			}
			if len(inputs) != len(want) || inputs[0] != want[0] || inputs[1] != want[1] || inputs[2] != want[2] {
				t.Fatalf("input log = %+v, want %+v", inputs, want)
			}
		})
	}
}

func TestGuardianCleanupRetriesTransientMappingFailures(t *testing.T) {
	injected := errors.New("injected transient cleanup failure")
	tests := []struct {
		name     string
		failures map[string][]error
	}{
		{name: "pressed keys", failures: map[string][]error{guardianOperationPressedKeys: {injected}}},
		{name: "modifier mapping", failures: map[string][]error{guardianOperationModifierMapping: {injected}}},
		{name: "keyboard mapping", failures: map[string][]error{guardianOperationKeyboardMapping: {injected}}},
		{name: "change mapping", failures: map[string][]error{guardianOperationChangeKeyboardMapping: {injected}}},
		{name: "verify mapping", failures: map[string][]error{guardianOperationKeyboardMapping: {nil, injected}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newGuardianTestConnection()
			before := cloneGuardianMapping(transport.mappings[10])
			after := KeyboardMapping{KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{'x', 'x'}}
			transport.mappings[10] = cloneGuardianMapping(after)
			for operation, failures := range test.failures {
				transport.operationFailures[operation] = append([]error(nil), failures...)
			}
			server := &guardianServer{
				connection:     transport,
				cleanupTimeout: 200 * time.Millisecond,
				mappings: map[xproto.Keycode]*guardianMappingClaim{
					10: {before: before, after: after},
				},
				mappingOrder: []xproto.Keycode{10},
				ownedKeys:    make(map[byte]guardianOwnedInput),
				ownedButtons: make(map[byte]guardianOwnedInput),
			}
			if err := server.cleanup(false); err != nil {
				t.Fatalf("cleanup transient %s failure: %v", test.name, err)
			}
			transport.mu.Lock()
			got := cloneGuardianMapping(transport.mappings[10])
			grabCount := transport.grabCount
			ungrabCount := transport.ungrabCount
			remainingFailures := len(transport.operationFailures)
			transport.mu.Unlock()
			if !guardianMappingsEqual(got, before) || len(server.mappings) != 0 {
				t.Fatalf("mapping after retry = %+v, claims=%d; want before and no claims", got, len(server.mappings))
			}
			if grabCount < 2 || ungrabCount < 2 {
				t.Fatalf("cleanup retry grab transitions = %d/%d; want at least two", grabCount, ungrabCount)
			}
			if remainingFailures != 0 {
				t.Fatalf("cleanup left %d injected failure queues", remainingFailures)
			}
		})
	}
}

func TestGuardianCleanupPreservesSemanticForeignImageAfterRestoreMismatch(t *testing.T) {
	transport := newGuardianTestConnection()
	keysym := xproto.Keysym(0x010020ac)
	before := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	after := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{keysym, keysym, 0}}
	foreign := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{keysym, 0, 0}}
	transport.mappings[10] = cloneGuardianMapping(before)
	transport.canonicalizeMapping = func(mapping KeyboardMapping) KeyboardMapping {
		if guardianMappingsEqual(mapping, before) {
			return cloneGuardianMapping(foreign)
		}
		return cloneGuardianMapping(mapping)
	}
	if guardianMappingsEqual(foreign, after) || !guardianMappingMatchesWrittenImage(foreign, after) {
		t.Fatal("test foreign image must be semantically owned but not exact-after")
	}
	connection, done := newInProcessGuardian(t, transport)
	if err := connection.ChangeKeyboardMapping(10, after.KeysymsPerKeycode, after.Keysyms); err != nil {
		t.Fatalf("assign mapping before restore mismatch: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close preserving semantic foreign restore mismatch: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server preserving semantic foreign restore mismatch: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	grabCount := transport.grabCount
	ungrabCount := transport.ungrabCount
	transport.mu.Unlock()
	if !guardianMappingsEqual(got, foreign) {
		t.Fatalf("mapping after restore mismatch = %+v, want preserved foreign %+v", got, foreign)
	}
	if grabCount < 2 || ungrabCount < 2 {
		t.Fatalf("restore mismatch grab transitions = %d/%d; want retry before relinquish", grabCount, ungrabCount)
	}
}

func TestGuardianRestoreRequestRelinquishesSemanticForeignImageWithoutError(t *testing.T) {
	transport := newGuardianTestConnection()
	keysym := xproto.Keysym(0x010020ac)
	before := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{0, 0, 0}}
	after := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{keysym, keysym, 0}}
	foreign := KeyboardMapping{KeysymsPerKeycode: 3, Keysyms: []xproto.Keysym{keysym, 0, 0}}
	transport.mappings[10] = cloneGuardianMapping(before)
	connection, done := newInProcessGuardian(t, transport)
	if err := connection.ChangeKeyboardMapping(10, after.KeysymsPerKeycode, after.Keysyms); err != nil {
		t.Fatalf("assign guardian mapping: %v", err)
	}
	transport.mu.Lock()
	transport.mappings[10] = cloneGuardianMapping(foreign)
	transport.mu.Unlock()
	if err := connection.ChangeKeyboardMapping(10, before.KeysymsPerKeycode, before.Keysyms); err != nil {
		t.Fatalf("restore request preserving semantic foreign image: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	transport.mu.Unlock()
	if !guardianMappingsEqual(got, foreign) {
		t.Fatalf("mapping after relinquished restore request = %+v, want foreign %+v", got, foreign)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close after relinquishing semantic foreign image: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("guardian server after relinquished restore request: %v", err)
	}
}

func TestGuardianCleanupYieldsServerGrabBetweenPressedRetries(t *testing.T) {
	transport := newGuardianTestConnection()
	transport.clearPressedOnUngrab = true
	transport.pressed[10/8] |= 1 << uint(10%8)
	before := cloneGuardianMapping(transport.mappings[10])
	after := KeyboardMapping{KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{'x', 'x'}}
	transport.mappings[10] = cloneGuardianMapping(after)
	server := &guardianServer{
		connection:     transport,
		cleanupTimeout: 100 * time.Millisecond,
		mappings: map[xproto.Keycode]*guardianMappingClaim{
			10: {before: before, after: after},
		},
		mappingOrder: []xproto.Keycode{10},
		ownedKeys:    make(map[byte]guardianOwnedInput),
		ownedButtons: make(map[byte]guardianOwnedInput),
	}
	if err := server.cleanup(false); err != nil {
		t.Fatalf("cleanup after pressed retry: %v", err)
	}
	transport.mu.Lock()
	got := cloneGuardianMapping(transport.mappings[10])
	grabCount := transport.grabCount
	ungrabCount := transport.ungrabCount
	transport.mu.Unlock()
	if !guardianMappingsEqual(got, before) {
		t.Fatalf("mapping after retry cleanup = %+v, want %+v", got, before)
	}
	if grabCount < 2 || ungrabCount < 2 {
		t.Fatalf("cleanup grab transitions = %d grabs/%d ungrabs, want retry yield", grabCount, ungrabCount)
	}
}

func TestGuardianRequestTimeoutTerminatesAmbiguousConnection(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("create socketpair: %v", err)
	}
	parent := os.NewFile(uintptr(fds[0]), "guardian-timeout-parent")
	peer := os.NewFile(uintptr(fds[1]), "guardian-timeout-peer")
	connection := newGuardianConnection(parent, nil, GuardianOptions{RequestTimeout: 5 * time.Millisecond})
	peerDone := make(chan error, 1)
	go func() {
		if _, err := readGuardianEnvelope(peer); err != nil {
			peerDone <- err
			return
		}
		var one [1]byte
		_, err := peer.Read(one[:])
		peerDone <- err
	}()
	if _, err := connection.Setup(); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Setup timeout error = %v", err)
	}
	if err := <-peerDone; !errors.Is(err, io.EOF) {
		t.Fatalf("guardian peer after request timeout = %v, want EOF", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("close timeout peer: %v", err)
	}
}

func TestGuardianUnexpectedResponseIDTerminatesConnection(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("create socketpair: %v", err)
	}
	parent := os.NewFile(uintptr(fds[0]), "guardian-response-id-parent")
	peer := os.NewFile(uintptr(fds[1]), "guardian-response-id-peer")
	connection := newGuardianConnection(parent, nil, GuardianOptions{RequestTimeout: time.Second})
	peerDone := make(chan error, 1)
	go func() {
		request, readErr := readGuardianEnvelope(peer)
		if readErr != nil {
			peerDone <- readErr
			return
		}
		writer := guardianFramedWriter{writer: peer}
		writeErr := writer.writePayload(guardianEnvelope{
			Version:   guardianProtocolVersion,
			Kind:      guardianFrameResponse,
			ID:        request.ID + 1,
			Operation: request.Operation,
		}, guardianSetupResponse{Setup: Setup{Root: 99}})
		peerDone <- writeErr
	}()
	if _, err := connection.Setup(); err == nil || !strings.Contains(err.Error(), "unexpected X11 guardian response ID") {
		t.Fatalf("Setup with unexpected response ID = %v, want protocol error", err)
	}
	if err := <-peerDone; err != nil {
		t.Fatalf("write unexpected response: %v", err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("close unexpected-response peer: %v", err)
	}
}

func TestGuardianDisplayDialIsDeadlineBounded(t *testing.T) {
	release := make(chan struct{})
	dialer := &guardianTestDialer{block: release}
	started := time.Now()
	connection, err := dialGuardianConnection(dialer, ":blocked", 10*time.Millisecond)
	close(release)
	if connection != nil || err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("blocked guardian dial = %T, %v; want nil timeout", connection, err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("blocked guardian dial took %s; want bounded completion", elapsed)
	}
}

func TestGuardianDispatchTimeoutClosesTransportAndTerminatesServer(t *testing.T) {
	transport := newGuardianTestConnection()
	transport.blockOperation = guardianOperationGrabServer
	transport.blockEntered = make(chan struct{})
	transport.blockRelease = make(chan struct{})
	connection, done := newInProcessGuardianWithOptions(t, transport, GuardianOptions{
		RequestTimeout: 20 * time.Millisecond,
		CleanupTimeout: 20 * time.Millisecond,
	})

	started := time.Now()
	err := connection.GrabServer()
	if err == nil || !strings.Contains(err.Error(), "dispatch timed out") {
		t.Fatalf("blocked guardian dispatch error = %v; want dispatch timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("blocked guardian dispatch took %s; want bounded completion", elapsed)
	}
	select {
	case serveErr := <-done:
		if serveErr == nil || !strings.Contains(serveErr.Error(), "dispatch timed out") {
			t.Fatalf("guardian server terminal error = %v; want dispatch timeout", serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("guardian server did not terminate after dispatch timeout")
	}
	transport.mu.Lock()
	closed := transport.closed
	transport.mu.Unlock()
	if !closed {
		t.Fatal("dispatch watchdog did not close the blocked X11 transport")
	}
}

func TestGuardianCloseKillsAndReapsHelperAfterExitTimeout(t *testing.T) {
	if os.Getenv(guardianTestBlockingProcess) == "1" {
		time.Sleep(time.Minute)
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestGuardianCloseKillsAndReapsHelperAfterExitTimeout$")
	command.Env = append(os.Environ(), guardianTestBlockingProcess+"=1")
	if err := command.Start(); err != nil {
		t.Fatalf("start blocking helper process: %v", err)
	}
	pid := command.Process.Pid
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("create guardian close-timeout socketpair: %v", err)
	}
	parent := os.NewFile(uintptr(fds[0]), "guardian-close-timeout-parent")
	peer := os.NewFile(uintptr(fds[1]), "guardian-close-timeout-peer")
	defer func() { _ = peer.Close() }()
	connection := newGuardianConnection(parent, command, GuardianOptions{
		RequestTimeout: 5 * time.Millisecond,
		CleanupTimeout: 5 * time.Millisecond,
	})
	if err := connection.Close(); err == nil || !strings.Contains(err.Error(), "did not exit") {
		t.Fatalf("Close with stuck helper error = %v; want process exit timeout", err)
	}
	if signalErr := unix.Kill(pid, 0); !errors.Is(signalErr, unix.ESRCH) {
		t.Fatalf("stuck helper PID %d remains after Close fallback: %v", pid, signalErr)
	}
}

func TestTerminateGuardianCommandPreservesNaturalFailure(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exit 7")
	if err := command.Start(); err != nil {
		t.Fatalf("start failing helper process: %v", err)
	}
	pid := command.Process.Pid
	deadline := time.Now().Add(time.Second)
	for {
		state, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err == nil && strings.Contains(string(state), ") Z ") {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatalf("helper PID %d did not reach zombie state: %v", pid, err)
		}
		time.Sleep(time.Millisecond)
	}

	err := terminateGuardianCommand(command, time.Second, "test natural failure")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("natural helper failure = %v; want exit status 7", err)
	}
}

func TestGuardianWaitForEventReportsTerminalErrorOnce(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("create socketpair: %v", err)
	}
	parent := os.NewFile(uintptr(fds[0]), "guardian-terminal-parent")
	peer := os.NewFile(uintptr(fds[1]), "guardian-terminal-peer")
	connection := newGuardianConnection(parent, nil, GuardianOptions{RequestTimeout: time.Second})
	if err := peer.Close(); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	if open, err := connection.WaitForEvent(); open || err == nil {
		t.Fatalf("first terminal WaitForEvent = %t, %v; want false, error", open, err)
	}
	if open, err := connection.WaitForEvent(); open || err != nil {
		t.Fatalf("second terminal WaitForEvent = %t, %v; want false, nil", open, err)
	}
}

func TestGuardianWaitForEventPreservesTerminalWhenOrdinaryBufferIsFull(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("create socketpair: %v", err)
	}
	parent := os.NewFile(uintptr(fds[0]), "guardian-full-events-parent")
	peer := os.NewFile(uintptr(fds[1]), "guardian-full-events-peer")
	connection := newGuardianConnection(parent, nil, GuardianOptions{RequestTimeout: time.Second})
	for index := 0; index < cap(connection.events); index++ {
		connection.events <- guardianEvent{Open: true}
	}
	payload, err := guardianPayload(guardianEvent{Open: false, Error: "terminal transport failure"})
	if err != nil {
		t.Fatalf("encode terminal event: %v", err)
	}
	writer := guardianFramedWriter{writer: peer}
	if err := writer.write(guardianEnvelope{
		Version: guardianProtocolVersion,
		Kind:    guardianFrameEvent,
		Payload: payload,
	}); err != nil {
		t.Fatalf("write terminal event: %v", err)
	}
	select {
	case <-connection.done:
	case <-time.After(time.Second):
		t.Fatal("terminal event did not finish guardian connection")
	}
	if open, err := connection.WaitForEvent(); open || err == nil || !strings.Contains(err.Error(), "terminal transport failure") {
		t.Fatalf("first terminal WaitForEvent = %t, %v; want terminal transport failure", open, err)
	}
	if open, err := connection.WaitForEvent(); open || err != nil {
		t.Fatalf("second terminal WaitForEvent = %t, %v; want false, nil", open, err)
	}
	if err := peer.Close(); err != nil {
		t.Fatalf("close terminal peer: %v", err)
	}
}

func TestGuardianCleanupTimeoutInterruptsBlockingRoundTrips(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		configure func(*guardianTestConnection, *guardianServer)
	}{
		{
			name:      "grab",
			operation: guardianOperationGrabServer,
			configure: func(*guardianTestConnection, *guardianServer) {},
		},
		{
			name:      "pressed keys",
			operation: guardianOperationPressedKeys,
			configure: func(transport *guardianTestConnection, server *guardianServer) {
				server.mappings[10] = &guardianMappingClaim{
					before: cloneGuardianMapping(transport.mappings[10]),
					after:  KeyboardMapping{KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{'x', 'x'}},
				}
				server.mappingOrder = []xproto.Keycode{10}
				transport.mappings[10] = cloneGuardianMapping(server.mappings[10].after)
			},
		},
		{
			name:      "mapping",
			operation: guardianOperationKeyboardMapping,
			configure: func(transport *guardianTestConnection, server *guardianServer) {
				server.mappings[10] = &guardianMappingClaim{
					before: cloneGuardianMapping(transport.mappings[10]),
					after:  KeyboardMapping{KeysymsPerKeycode: 2, Keysyms: []xproto.Keysym{'x', 'x'}},
				}
				server.mappingOrder = []xproto.Keycode{10}
				transport.mappings[10] = cloneGuardianMapping(server.mappings[10].after)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newGuardianTestConnection()
			transport.blockOperation = test.operation
			transport.blockEntered = make(chan struct{})
			transport.blockRelease = make(chan struct{})
			transport.blockClose = make(chan struct{})
			server := &guardianServer{
				connection:     transport,
				cleanupTimeout: 20 * time.Millisecond,
				mappings:       make(map[xproto.Keycode]*guardianMappingClaim),
				ownedKeys:      make(map[byte]guardianOwnedInput),
				ownedButtons:   make(map[byte]guardianOwnedInput),
			}
			test.configure(transport, server)

			started := time.Now()
			err := server.cleanup(false)
			if err == nil || !strings.Contains(err.Error(), "cleanup timed out") {
				t.Fatalf("cleanup error = %v; want timeout", err)
			}
			if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
				t.Fatalf("cleanup took %s; want bounded completion", elapsed)
			}
			select {
			case <-transport.blockEntered:
			default:
				t.Fatalf("cleanup never entered blocking %s round trip", test.operation)
			}
			select {
			case <-transport.blockRelease:
			case <-time.After(time.Second):
				t.Fatal("cleanup watchdog did not initiate transport Close")
			}

			// Keep Close blocked until cleanup has already returned. This proves
			// the watchdog itself never waits synchronously for Connection.Close.
			close(transport.blockClose)
			select {
			case <-server.cleanupWorkDone:
			case <-time.After(time.Second):
				t.Fatal("cleanup worker did not stop after transport Close unblocked its round trip")
			}
			select {
			case <-server.connectionCloseDone:
			case <-time.After(time.Second):
				t.Fatal("transport Close did not finish after test released it")
			}
		})
	}
}

func TestGuardianCloseReturnsWhenCleanupAndTransportCloseBlock(t *testing.T) {
	transport := newGuardianTestConnection()
	connection, done := newInProcessGuardian(t, transport)
	transport.blockOperation = guardianOperationGrabServer
	transport.blockEntered = make(chan struct{})
	transport.blockRelease = make(chan struct{})
	transport.blockClose = make(chan struct{})

	started := time.Now()
	err := connection.Close()
	if err == nil || !strings.Contains(err.Error(), "cleanup timed out") {
		t.Fatalf("Close error = %v; want cleanup timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Close took %s; want bounded completion", elapsed)
	}
	select {
	case serveErr := <-done:
		if serveErr != nil {
			t.Fatalf("guardian server after bounded Close: %v", serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("guardian request loop did not terminate after bounded Close")
	}

	close(transport.blockClose)
	deadline := time.Now().Add(time.Second)
	for {
		transport.mu.Lock()
		closed := transport.closed
		transport.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test transport Close did not finish after release")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestGuardianProtocolRejectsOversizedFrame(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], guardianMaximumFrame+1)
	if _, err := readGuardianEnvelope(strings.NewReader(string(header[:]))); err == nil {
		t.Fatal("oversized guardian frame unexpectedly succeeded")
	}
}

func TestGuardianAbstractSocketRequiresExpectedKernelPeer(t *testing.T) {
	for _, test := range []struct {
		name        string
		expectedPID int
		wantSuccess bool
	}{
		{name: "exact helper", expectedPID: os.Getpid(), wantSuccess: true},
		{name: "different process", expectedPID: os.Getpid() + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			token, err := newGuardianToken()
			if err != nil {
				t.Fatalf("create guardian test token: %v", err)
			}
			listener, err := net.ListenUnix("unix", guardianAbstractSocketAddress(token))
			if err != nil {
				t.Fatalf("listen on guardian test socket: %v", err)
			}
			defer func() { _ = listener.Close() }()
			dialDone := make(chan error, 1)
			go func() {
				connection, dialErr := net.DialUnix("unix", nil, guardianAbstractSocketAddress(token))
				if connection != nil {
					_ = connection.Close()
				}
				dialDone <- dialErr
			}()
			command := &exec.Cmd{Process: &os.Process{Pid: test.expectedPID}}
			connection, acceptErr := acceptGuardianControl(listener, command, 30*time.Millisecond)
			if connection != nil {
				_ = connection.Close()
			}
			if dialErr := <-dialDone; dialErr != nil {
				t.Fatalf("dial guardian test socket: %v", dialErr)
			}
			if test.wantSuccess && acceptErr != nil {
				t.Fatalf("accept exact guardian peer: %v", acceptErr)
			}
			if !test.wantSuccess && acceptErr == nil {
				t.Fatal("accepted guardian peer with a different expected PID")
			}
		})
	}
}

func TestGuardianReexecSelectionRequiresExactMarker(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      bool
	}{
		{name: "marker", arguments: []string{"robotgo-test", guardianReexecArgument}, want: true},
		{name: "no marker", arguments: []string{"robotgo-test"}},
		{name: "different marker", arguments: []string{"robotgo-test", "--other"}},
		{name: "extra argument", arguments: []string{"robotgo-test", guardianReexecArgument, "extra"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := guardianReexecSelected(test.arguments); got != test.want {
				t.Fatalf("guardianReexecSelected(%q) = %t, want %t", test.arguments, got, test.want)
			}
		})
	}
}

func TestGuardianEnvironmentAloneDoesNotSelectReexec(t *testing.T) {
	if os.Getenv(guardianTestEnvironmentOnly) == "1" {
		return
	}
	token := strings.Repeat("ab", 32)
	command := exec.Command(os.Args[0], "-test.run=^TestGuardianEnvironmentAloneDoesNotSelectReexec$")
	command.Env = append(
		guardianChildEnvironment(os.Environ(), token),
		guardianTestEnvironmentOnly+"=1",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("test binary with guardian environment but no marker exited: %v\n%s", err, output)
	}
}

func TestGuardianMarkerFailsClosedWithoutValidToken(t *testing.T) {
	command := exec.Command(os.Args[0], guardianReexecArgument)
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if hasEnvironmentKey(entry, guardianEnvironmentToken) {
			continue
		}
		environment = append(environment, entry)
	}
	command.Env = append(environment, guardianEnvironmentToken+"=invalid")
	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 125 {
		t.Fatalf("guardian marker with invalid environment error = %v; want exit status 125", err)
	}
}

func TestGuardianStartupTimeoutKillsAndReapsHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "guardian.pid")
	fdFile := filepath.Join(t.TempDir(), "guardian-fd.txt")
	executable := filepath.Join(t.TempDir(), "blocking-guardian")
	script := "#!/bin/sh\n" +
		"printf '%s' \"$$\" > \"${" + guardianTestPIDFile + ":?}\"\n" +
		"if [ -e /proc/self/fd/3 ]; then printf inherited; else printf none; fi > \"${" + guardianTestFDFile + ":?}\"\n" +
		"exec sleep 60\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatalf("write blocking guardian helper: %v", err)
	}
	t.Setenv(guardianTestPIDFile, pidFile)
	t.Setenv(guardianTestFDFile, fdFile)
	dialer := NewGuardianDialer(GuardianOptions{
		Executable:     executable,
		StartupTimeout: 50 * time.Millisecond,
		RequestTimeout: time.Second,
		CleanupTimeout: 50 * time.Millisecond,
		CrashSettle:    time.Millisecond,
	})
	connection, err := dialer.Dial(":guardian-startup-timeout")
	if connection != nil || err == nil || (!os.IsTimeout(err) && !strings.Contains(err.Error(), "timeout")) {
		t.Fatalf("blocking guardian Dial = %T, %v; want nil timeout", connection, err)
	}
	pidData, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("read blocking guardian PID: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(string(pidData))
	if parseErr != nil {
		t.Fatalf("parse blocking guardian PID %q: %v", pidData, parseErr)
	}
	if signalErr := unix.Kill(pid, 0); !errors.Is(signalErr, unix.ESRCH) {
		t.Fatalf("guardian helper PID %d remains after startup failure: %v", pid, signalErr)
	}
	fdState, readErr := os.ReadFile(fdFile)
	if readErr != nil {
		t.Fatalf("read blocking guardian FD state: %v", readErr)
	}
	if string(fdState) != "none" {
		t.Fatalf("re-executed helper inherited control descriptor 3: %q", fdState)
	}
}

func TestGuardianReexecFailsClosedForInvalidDisplay(t *testing.T) {
	dialer := NewGuardianDialer(GuardianOptions{
		StartupTimeout: time.Second,
		RequestTimeout: time.Second,
		CleanupTimeout: 100 * time.Millisecond,
		CrashSettle:    time.Millisecond,
	})
	connection, err := dialer.Dial("not-a-valid-x11-display")
	if connection != nil || err == nil {
		if connection != nil {
			_ = connection.Close()
		}
		t.Fatalf("invalid display Dial = %T, %v; want nil, error", connection, err)
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("invalid display failed before authenticated response: %v", err)
	}
}
