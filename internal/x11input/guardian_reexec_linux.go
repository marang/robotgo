//go:build linux

package x11input

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jezek/xgb/xproto"
)

const guardianCleanupRetryDelay = 10 * time.Millisecond

type guardianMappingClaim struct {
	before     KeyboardMapping
	after      KeyboardMapping
	unresolved bool
}

type guardianOwnedInput struct {
	eventType byte
	detail    byte
	root      xproto.Window
}

type guardianServer struct {
	connection Connection
	writer     *guardianFramedWriter
	eventsMu   sync.RWMutex
	closing    bool

	connectionCloseOnce sync.Once
	connectionCloseDone chan struct{}
	connectionCloseErr  error
	cleanupWorkDone     <-chan struct{}
	cleanupDeadline     time.Time

	cleanupTimeout time.Duration
	requestTimeout time.Duration
	crashSettle    time.Duration
	grabbed        bool
	closed         bool

	mappings     map[xproto.Keycode]*guardianMappingClaim
	mappingOrder []xproto.Keycode
	ownedKeys    map[byte]guardianOwnedInput
	ownedButtons map[byte]guardianOwnedInput
	inputOrder   []guardianOwnedInput
	lastKeyEvent time.Time
}

func init() {
	if !guardianReexecSelected(os.Args) {
		return
	}
	status := runGuardianReexecChild()
	os.Exit(status)
}

func guardianReexecSelected(arguments []string) bool {
	return len(arguments) == 2 && arguments[1] == guardianReexecArgument
}

func runGuardianReexecChild() int {
	expectedToken, ok := guardianTokenFromEnvironment()
	if !ok {
		return 125
	}
	control, err := net.DialUnix("unix", nil, guardianAbstractSocketAddress(expectedToken))
	if err != nil {
		return 125
	}
	_ = os.Unsetenv(guardianEnvironmentToken)
	err = serveGuardian(control, expectedToken, xgbDialer{})
	_ = control.Close()
	if err != nil {
		return 1
	}
	return 0
}

func serveGuardian(control io.ReadWriteCloser, expectedToken string, dialer Dialer) error {
	writer := &guardianFramedWriter{writer: control}
	reader := guardianFramedReader{reader: control}
	helloEnvelope, err := reader.read()
	if err != nil {
		return err
	}
	if helloEnvelope.Kind != guardianFrameRequest || helloEnvelope.ID == 0 || helloEnvelope.Operation != guardianOperationHello {
		return errors.New("X11 guardian expected hello as its first request")
	}
	var hello guardianHelloRequest
	if err := decodeGuardianPayload(helloEnvelope.Payload, &hello); err != nil {
		_ = writeGuardianResponse(writer, helloEnvelope, nil, err)
		return nil
	}
	if err := validateGuardianHello(expectedToken, hello); err != nil {
		_ = writeGuardianResponse(writer, helloEnvelope, nil, err)
		return nil
	}
	connection, err := dialGuardianConnection(dialer, hello.Display, time.Duration(hello.RequestTimeoutNano))
	if err != nil {
		_ = writeGuardianResponse(writer, helloEnvelope, nil, fmt.Errorf("dial X11 display %q: %w", hello.Display, err))
		return nil
	}
	server := &guardianServer{
		connection:     connection,
		writer:         writer,
		requestTimeout: time.Duration(hello.RequestTimeoutNano),
		cleanupTimeout: time.Duration(hello.CleanupTimeoutNano),
		crashSettle:    time.Duration(hello.CrashSettleNano),
		mappings:       make(map[xproto.Keycode]*guardianMappingClaim),
		ownedKeys:      make(map[byte]guardianOwnedInput),
		ownedButtons:   make(map[byte]guardianOwnedInput),
	}
	defer func() {
		if !server.closed {
			_ = server.closeConnection()
		}
	}()
	if err := writeGuardianResponse(writer, helloEnvelope, nil, nil); err != nil {
		return errors.Join(err, server.cleanup(true), server.closeConnection())
	}
	go server.forwardEvents()

	for {
		envelope, readErr := reader.read()
		if readErr != nil {
			server.stopForwardingEvents()
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
				return errors.Join(server.cleanup(true), server.closeConnection())
			}
			return errors.Join(readErr, server.cleanup(true), server.closeConnection())
		}
		if envelope.Kind != guardianFrameRequest || envelope.ID == 0 || envelope.Operation == guardianOperationHello {
			protocolErr := errors.New("invalid X11 guardian request envelope")
			_ = writeGuardianResponse(writer, envelope, nil, protocolErr)
			server.stopForwardingEvents()
			return errors.Join(protocolErr, server.cleanup(true), server.closeConnection())
		}
		payload, operationErr, terminalErr := server.dispatchBounded(envelope)
		if writeErr := writeGuardianResponse(writer, envelope, payload, operationErr); writeErr != nil {
			server.stopForwardingEvents()
			if terminalErr != nil {
				return errors.Join(writeErr, terminalErr)
			}
			return errors.Join(writeErr, server.cleanup(true), server.closeConnection())
		}
		if terminalErr != nil {
			server.stopForwardingEvents()
			return terminalErr
		}
		if envelope.Operation == guardianOperationClose {
			return nil
		}
	}
}

type guardianDialResult struct {
	connection Connection
	err        error
}

func dialGuardianConnection(dialer Dialer, display string, timeout time.Duration) (Connection, error) {
	if timeout <= 0 {
		timeout = guardianDefaultRequestTimeout
	}
	result := make(chan guardianDialResult, 1)
	go func() {
		connection, err := dialer.Dial(display)
		result <- guardianDialResult{connection: connection, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case completed := <-result:
		return completed.connection, completed.err
	case <-timer.C:
		go func() {
			completed := <-result
			if completed.connection != nil {
				_ = completed.connection.Close()
			}
		}()
		return nil, fmt.Errorf("X11 guardian display dial timed out after %s", timeout)
	}
}

func validateGuardianHello(expectedToken string, hello guardianHelloRequest) error {
	if len(expectedToken) != 64 || len(hello.Token) != len(expectedToken) {
		return errors.New("invalid X11 guardian authentication token")
	}
	if _, err := hex.DecodeString(expectedToken); err != nil {
		return errors.New("invalid X11 guardian authentication token")
	}
	if subtle.ConstantTimeCompare([]byte(expectedToken), []byte(hello.Token)) != 1 {
		return errors.New("X11 guardian authentication failed")
	}
	if hello.Display == "" {
		return errors.New("X11 guardian display is empty")
	}
	for name, duration := range map[string]int64{
		"request timeout": hello.RequestTimeoutNano,
		"cleanup timeout": hello.CleanupTimeoutNano,
		"crash settle":    hello.CrashSettleNano,
	} {
		if duration < 0 || time.Duration(duration) > guardianMaximumDuration {
			return fmt.Errorf("X11 guardian %s is outside the supported range", name)
		}
	}
	return nil
}

type guardianDispatchResult struct {
	payload any
	err     error
}

func (server *guardianServer) dispatchBounded(envelope guardianEnvelope) (any, error, error) {
	// Close already contains independently bounded cleanup and transport-close
	// phases. Running it through a second watchdog would race those state owners.
	if envelope.Operation == guardianOperationClose {
		payload, err := server.dispatch(envelope)
		return payload, err, nil
	}
	timeout := server.requestTimeout
	if timeout <= 0 {
		timeout = guardianDefaultRequestTimeout
	}
	result := make(chan guardianDispatchResult, 1)
	go func() {
		payload, err := server.dispatch(envelope)
		result <- guardianDispatchResult{payload: payload, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case completed := <-result:
		return completed.payload, completed.err, nil
	case <-timer.C:
		timeoutErr := fmt.Errorf("X11 guardian %s dispatch timed out after %s", envelope.Operation, timeout)
		server.stopForwardingEvents()
		closeErr := server.closeConnection()
		terminalErr := errors.Join(timeoutErr, closeErr)
		return nil, timeoutErr, terminalErr
	}
}

func (server *guardianServer) forwardEvents() {
	for {
		open, err := server.connection.WaitForEvent()
		event := guardianEvent{Open: open}
		if err != nil {
			event.Error = err.Error()
		}
		server.eventsMu.RLock()
		if server.closing {
			server.eventsMu.RUnlock()
			return
		}
		writeErr := server.writer.writePayload(guardianEnvelope{
			Version: guardianProtocolVersion,
			Kind:    guardianFrameEvent,
		}, event)
		server.eventsMu.RUnlock()
		if writeErr != nil || !open || err != nil {
			return
		}
	}
}

func (server *guardianServer) stopForwardingEvents() {
	server.eventsMu.Lock()
	server.closing = true
	server.eventsMu.Unlock()
}

func (server *guardianServer) dispatch(envelope guardianEnvelope) (any, error) {
	switch envelope.Operation {
	case guardianOperationSetup:
		setup, err := server.connection.Setup()
		return guardianSetupResponse{Setup: setup}, err
	case guardianOperationInitXTest:
		return nil, server.connection.InitXTest()
	case guardianOperationXTestVersion:
		var request guardianXTestVersionRequest
		if err := decodeGuardianPayload(envelope.Payload, &request); err != nil {
			return nil, err
		}
		version, err := server.connection.XTestVersion(request.Major, request.Minor)
		return guardianXTestVersionResponse{Version: version}, err
	case guardianOperationGrabServer:
		if server.grabbed {
			return nil, errors.New("X11 guardian server is already grabbed")
		}
		if err := server.connection.GrabServer(); err != nil {
			return nil, err
		}
		server.grabbed = true
		return nil, nil
	case guardianOperationUngrabServer:
		if !server.grabbed {
			return nil, errors.New("X11 guardian server is not grabbed")
		}
		if err := server.connection.UngrabServer(); err != nil {
			return nil, err
		}
		server.grabbed = false
		return nil, nil
	case guardianOperationKeyboardMapping:
		var request guardianKeyboardMappingRequest
		if err := decodeGuardianPayload(envelope.Payload, &request); err != nil {
			return nil, err
		}
		mapping, err := server.connection.KeyboardMapping(request.First, request.Count)
		return guardianKeyboardMappingResponse{Mapping: mapping}, err
	case guardianOperationModifierMapping:
		mapping, err := server.connection.ModifierMapping()
		return guardianModifierMappingResponse{Keycodes: mapping}, err
	case guardianOperationChangeKeyboardMapping:
		var request guardianChangeKeyboardMappingRequest
		if err := decodeGuardianPayload(envelope.Payload, &request); err != nil {
			return nil, err
		}
		return nil, server.changeKeyboardMapping(request)
	case guardianOperationPressedKeys:
		keys, err := server.connection.PressedKeys()
		return guardianPressedKeysResponse{Keys: keys}, err
	case guardianOperationQueryPointer:
		var request guardianQueryPointerRequest
		if err := decodeGuardianPayload(envelope.Payload, &request); err != nil {
			return nil, err
		}
		state, err := server.connection.QueryPointer(request.Root)
		return guardianQueryPointerResponse{State: state}, err
	case guardianOperationFakeInput:
		var request guardianFakeInputRequest
		if err := decodeGuardianPayload(envelope.Payload, &request); err != nil {
			return nil, err
		}
		return nil, server.fakeInput(request)
	case guardianOperationFakeInputSequence:
		var request guardianFakeInputSequenceRequest
		if err := decodeGuardianPayload(envelope.Payload, &request); err != nil {
			return nil, err
		}
		return nil, server.fakeInputSequence(request)
	case guardianOperationClose:
		server.stopForwardingEvents()
		cleanupErr := server.cleanup(false)
		return nil, errors.Join(cleanupErr, server.closeConnection())
	default:
		return nil, fmt.Errorf("unsupported X11 guardian operation %q", envelope.Operation)
	}
}

func writeGuardianResponse(writer *guardianFramedWriter, request guardianEnvelope, response any, responseErr error) error {
	envelope := guardianEnvelope{
		Version:   guardianProtocolVersion,
		Kind:      guardianFrameResponse,
		ID:        request.ID,
		Operation: request.Operation,
	}
	if responseErr != nil {
		envelope.Error = responseErr.Error()
	}
	return writer.writePayload(envelope, response)
}

func (server *guardianServer) changeKeyboardMapping(request guardianChangeKeyboardMappingRequest) error {
	if request.PerKeycode == 0 || len(request.Keysyms) != int(request.PerKeycode) {
		return fmt.Errorf("invalid keyboard mapping width %d with %d keysyms", request.PerKeycode, len(request.Keysyms))
	}
	current, err := server.connection.KeyboardMapping(request.First, 1)
	if err != nil {
		return fmt.Errorf("snapshot keyboard mapping %d: %w", request.First, err)
	}
	if err := validGuardianMapping(current); err != nil {
		return fmt.Errorf("snapshot keyboard mapping %d: %w", request.First, err)
	}
	after := KeyboardMapping{
		KeysymsPerKeycode: request.PerKeycode,
		Keysyms:           append([]xproto.Keysym(nil), request.Keysyms...),
	}
	claim := server.mappings[request.First]
	if claim != nil {
		if guardianMappingsEqual(after, claim.before) {
			if guardianMappingsEqual(current, claim.before) {
				server.removeMappingClaim(request.First)
				return nil
			}
			if !guardianMappingsEqual(current, claim.after) {
				if claim.unresolved {
					return fmt.Errorf(
						"X11 guardian refuses to restore unresolved mapping of keycode %d", request.First,
					)
				}
			}
			if !guardianMappingsEqual(current, claim.after) {
				// The parent may recognize a semantically owned X11 image that is
				// not the exact server image recorded by the guardian. Exact
				// mismatch is foreign ownership: preserve it, relinquish our claim,
				// and report successful cleanup instead of forcing a connection error.
				server.removeMappingClaim(request.First)
				return nil
			}
			if err := server.connection.ChangeKeyboardMapping(request.First, request.PerKeycode, request.Keysyms); err != nil {
				return err
			}
			if err := server.verifyMapping(request.First, claim.before); err != nil {
				return err
			}
			server.removeMappingClaim(request.First)
			return nil
		}
		if !guardianMappingsEqual(current, claim.after) {
			return fmt.Errorf("X11 guardian refuses to replace foreign mapping of keycode %d", request.First)
		}
		if guardianMappingsEqual(after, claim.after) {
			return nil
		}
		return fmt.Errorf("X11 guardian refuses to remap owned keycode %d before restoring it", request.First)
	} else if guardianMappingsEqual(current, after) {
		return nil
	} else {
		// Store both images before the checked request. If its round trip is
		// interrupted, cleanup treats the mutation as possibly applied.
		claim = &guardianMappingClaim{
			before: cloneGuardianMapping(current),
			after:  cloneGuardianMapping(after),
		}
		server.mappings[request.First] = claim
		server.mappingOrder = append(server.mappingOrder, request.First)
	}
	if err := server.connection.ChangeKeyboardMapping(request.First, request.PerKeycode, request.Keysyms); err != nil {
		return server.reconcileFailedMappingChange(request.First, claim, err)
	}
	actual, err := server.readBackAppliedMapping(request.First, claim.after)
	if err != nil {
		claim.unresolved = true
		return err
	}
	// X11 servers may canonicalize repeated scratch columns to NoSymbol.
	// Track the actual server image so cleanup can distinguish our state from
	// a later foreign replacement while preserving the exact before-image.
	claim.after = actual
	return nil
}

func (server *guardianServer) reconcileFailedMappingChange(
	code xproto.Keycode,
	claim *guardianMappingClaim,
	changeErr error,
) error {
	actual, readbackErr := server.connection.KeyboardMapping(code, 1)
	if readbackErr != nil {
		claim.unresolved = true
		return errors.Join(changeErr, fmt.Errorf(
			"read back possibly applied keyboard mapping %d: %w", code, readbackErr,
		))
	}
	if validErr := validGuardianMapping(actual); validErr != nil {
		claim.unresolved = true
		return errors.Join(changeErr, fmt.Errorf(
			"read back possibly applied keyboard mapping %d: %w", code, validErr,
		))
	}
	if guardianMappingsEqual(actual, claim.before) {
		server.removeMappingClaim(code)
		return changeErr
	}
	if server.grabbed {
		// A checked X11 request may mutate global state before its reply is lost.
		// While this client owns the server grab, any changed readback is the
		// request's actual (possibly canonicalized) image. Without that exclusion,
		// ownership remains ambiguous even when the image looks semantically equal.
		claim.after = cloneGuardianMapping(actual)
		claim.unresolved = false
		return changeErr
	}
	claim.unresolved = true
	return errors.Join(changeErr, fmt.Errorf(
		"keyboard mapping %d ownership is unresolved after a failed change", code,
	))
}

func validGuardianMapping(mapping KeyboardMapping) error {
	if mapping.KeysymsPerKeycode == 0 {
		return errors.New("keyboard mapping has zero width")
	}
	if len(mapping.Keysyms) != int(mapping.KeysymsPerKeycode) {
		return fmt.Errorf("keyboard mapping has %d keysyms for width %d", len(mapping.Keysyms), mapping.KeysymsPerKeycode)
	}
	return nil
}

func cloneGuardianMapping(mapping KeyboardMapping) KeyboardMapping {
	return KeyboardMapping{
		KeysymsPerKeycode: mapping.KeysymsPerKeycode,
		Keysyms:           append([]xproto.Keysym(nil), mapping.Keysyms...),
	}
}

func guardianMappingsEqual(left, right KeyboardMapping) bool {
	if left.KeysymsPerKeycode != right.KeysymsPerKeycode || len(left.Keysyms) != len(right.Keysyms) {
		return false
	}
	for index := range left.Keysyms {
		if left.Keysyms[index] != right.Keysyms[index] {
			return false
		}
	}
	return true
}

func guardianMappingMatchesWrittenImage(current, written KeyboardMapping) bool {
	if guardianMappingsEqual(current, written) {
		return true
	}
	if current.KeysymsPerKeycode != written.KeysymsPerKeycode ||
		len(current.Keysyms) != int(current.KeysymsPerKeycode) ||
		len(written.Keysyms) != int(written.KeysymsPerKeycode) || len(written.Keysyms) == 0 {
		return false
	}
	keysym := uint32(written.Keysyms[0])
	if keysym == 0 || !x11MappingOwnedBy(written.Keysyms, keysym) {
		return false
	}
	return x11MappingOwnedBy(current.Keysyms, keysym)
}

func (server *guardianServer) readBackAppliedMapping(code xproto.Keycode, expected KeyboardMapping) (KeyboardMapping, error) {
	current, err := server.connection.KeyboardMapping(code, 1)
	if err != nil {
		return KeyboardMapping{}, fmt.Errorf("verify keyboard mapping %d: %w", code, err)
	}
	if err := validGuardianMapping(current); err != nil {
		return KeyboardMapping{}, fmt.Errorf("verify keyboard mapping %d: %w", code, err)
	}
	if !guardianMappingMatchesWrittenImage(current, expected) {
		return KeyboardMapping{}, fmt.Errorf("keyboard mapping %d failed ownership readback verification", code)
	}
	return cloneGuardianMapping(current), nil
}

func (server *guardianServer) verifyMapping(code xproto.Keycode, expected KeyboardMapping) error {
	current, err := server.connection.KeyboardMapping(code, 1)
	if err != nil {
		return fmt.Errorf("verify keyboard mapping %d: %w", code, err)
	}
	if !guardianMappingsEqual(current, expected) {
		return fmt.Errorf("keyboard mapping %d failed readback verification", code)
	}
	return nil
}

func (server *guardianServer) removeMappingClaim(code xproto.Keycode) {
	delete(server.mappings, code)
	for index := len(server.mappingOrder) - 1; index >= 0; index-- {
		if server.mappingOrder[index] != code {
			continue
		}
		copy(server.mappingOrder[index:], server.mappingOrder[index+1:])
		server.mappingOrder[len(server.mappingOrder)-1] = 0
		server.mappingOrder = server.mappingOrder[:len(server.mappingOrder)-1]
		return
	}
}

func (server *guardianServer) fakeInput(request guardianFakeInputRequest) error {
	switch request.EventType {
	case byte(xproto.KeyPress):
		server.armOwnedInput(request, true)
		server.lastKeyEvent = time.Now()
	case byte(xproto.KeyRelease):
		server.lastKeyEvent = time.Now()
	case byte(xproto.ButtonPress):
		server.armOwnedInput(request, false)
	}
	// Press ownership is armed before the checked XTEST request because a
	// transport error cannot prove that the server rejected the mutation.
	// Conversely, release ownership is removed only after a verified success.
	if err := server.connection.FakeInput(request.EventType, request.Detail, request.Root, request.X, request.Y); err != nil {
		return err
	}
	switch request.EventType {
	case byte(xproto.KeyRelease):
		server.removeOwnedInput(request.Detail, true)
	case byte(xproto.ButtonRelease):
		server.removeOwnedInput(request.Detail, false)
	}
	return nil
}

func (server *guardianServer) fakeInputSequence(request guardianFakeInputSequenceRequest) error {
	if len(request.Steps) == 0 || len(request.Steps) > guardianMaximumInputSteps {
		return fmt.Errorf(
			"X11 guardian fake-input sequence has %d steps; want 1..%d",
			len(request.Steps), guardianMaximumInputSteps,
		)
	}
	timeout := server.requestTimeout
	if timeout <= 0 {
		timeout = guardianDefaultRequestTimeout
	}
	var totalDelay time.Duration
	for index, step := range request.Steps {
		delay := time.Duration(step.DelayAfterNano)
		if delay < 0 || delay > guardianMaximumDuration {
			return fmt.Errorf("X11 guardian fake-input sequence delay at step %d is outside 0..%s", index, guardianMaximumDuration)
		}
		if index == len(request.Steps)-1 && delay != 0 {
			return errors.New("X11 guardian fake-input sequence cannot delay after its final step")
		}
		if delay > timeout-totalDelay {
			return fmt.Errorf("X11 guardian fake-input sequence delays exceed request timeout %s", timeout)
		}
		totalDelay += delay
	}
	if totalDelay >= timeout {
		return fmt.Errorf("X11 guardian fake-input sequence delays must be shorter than request timeout %s", timeout)
	}
	for index, step := range request.Steps {
		if err := server.fakeInput(step.guardianFakeInputRequest); err != nil {
			return fmt.Errorf("X11 guardian fake-input sequence step %d: %w", index, err)
		}
		if step.DelayAfterNano > 0 {
			time.Sleep(time.Duration(step.DelayAfterNano))
		}
	}
	return nil
}

func (server *guardianServer) armOwnedInput(request guardianFakeInputRequest, key bool) {
	owned := guardianOwnedInput{eventType: request.EventType, detail: request.Detail, root: request.Root}
	if key {
		if _, exists := server.ownedKeys[request.Detail]; exists {
			return
		}
		server.ownedKeys[request.Detail] = owned
	} else {
		if _, exists := server.ownedButtons[request.Detail]; exists {
			return
		}
		server.ownedButtons[request.Detail] = owned
	}
	server.inputOrder = append(server.inputOrder, owned)
}

func (server *guardianServer) removeOwnedInput(detail byte, key bool) {
	if key {
		delete(server.ownedKeys, detail)
	} else {
		delete(server.ownedButtons, detail)
	}
	for index := len(server.inputOrder) - 1; index >= 0; index-- {
		input := server.inputOrder[index]
		if input.detail != detail || (input.eventType == byte(xproto.KeyPress)) != key {
			continue
		}
		copy(server.inputOrder[index:], server.inputOrder[index+1:])
		server.inputOrder[len(server.inputOrder)-1] = guardianOwnedInput{}
		server.inputOrder = server.inputOrder[:len(server.inputOrder)-1]
		return
	}
}

func (server *guardianServer) cleanup(crashed bool) error {
	if server.closed {
		return nil
	}
	timeout := server.cleanupTimeout
	if timeout <= 0 {
		timeout = guardianDefaultCleanupTimeout
	}
	server.cleanupDeadline = time.Now().Add(timeout)
	result := make(chan error, 1)
	done := make(chan struct{})
	server.cleanupWorkDone = done
	go func() {
		result <- server.cleanupTransaction(crashed, server.cleanupDeadline)
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		// Connection.Close is deliberately started in its own goroutine. XGB
		// Close normally interrupts blocked round trips, but if a transport's
		// Close itself blocks, neither this watchdog nor the helper's request
		// loop waits for it. The re-executed helper process then exits and the OS
		// reclaims that abandoned goroutine; in-process test transports must
		// release their Close seam explicitly.
		server.beginConnectionClose()
		return fmt.Errorf("X11 guardian cleanup timed out after %s; transport close initiated", timeout)
	}
}

func (server *guardianServer) cleanupTransaction(crashed bool, deadline time.Time) error {
	var cleanupErr error
	if !server.grabbed {
		if err := server.connection.GrabServer(); err != nil {
			return fmt.Errorf("X11 guardian grab server for cleanup: %w", err)
		}
		server.grabbed = true
	}
	lastReleaseErr := server.releaseOwnedInput()
	if crashed && len(server.inputOrder) == 0 && !server.lastKeyEvent.IsZero() && len(server.mappings) > 0 {
		settleUntil := server.lastKeyEvent.Add(server.crashSettle)
		if settleUntil.After(time.Now()) {
			if err := server.connection.UngrabServer(); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("X11 guardian ungrab before crash settle: %w", err))
			} else {
				server.grabbed = false
				remaining := time.Until(settleUntil)
				if deadlineRemaining := time.Until(deadline); remaining > deadlineRemaining {
					remaining = deadlineRemaining
				}
				if remaining > 0 {
					time.Sleep(remaining)
				}
				if err := server.connection.GrabServer(); err != nil {
					return errors.Join(cleanupErr, fmt.Errorf("X11 guardian re-grab after crash settle: %w", err))
				}
				server.grabbed = true
			}
		}
	}

	var lastRestoreErr error
	for {
		if len(server.inputOrder) > 0 {
			lastReleaseErr = server.releaseOwnedInput()
		}
		retry, restoreErr := server.restoreMappingsOnce()
		lastRestoreErr = restoreErr
		retry = retry || len(server.inputOrder) > 0
		if !retry || !time.Now().Before(deadline) {
			break
		}
		if err := server.yieldCleanupGrab(deadline); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			break
		}
	}
	cleanupErr = errors.Join(cleanupErr, lastReleaseErr, lastRestoreErr)
	if server.grabbed {
		if err := server.connection.UngrabServer(); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("X11 guardian ungrab after cleanup: %w", err))
		} else {
			server.grabbed = false
		}
	}
	return cleanupErr
}

func (server *guardianServer) yieldCleanupGrab(deadline time.Time) error {
	if !server.grabbed {
		return errors.New("X11 guardian cannot yield an unowned cleanup grab")
	}
	if err := server.connection.UngrabServer(); err != nil {
		return fmt.Errorf("X11 guardian ungrab between cleanup retries: %w", err)
	}
	server.grabbed = false
	delay := guardianCleanupRetryDelay
	if remaining := time.Until(deadline); delay > remaining {
		delay = remaining
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if err := server.connection.GrabServer(); err != nil {
		return fmt.Errorf("X11 guardian re-grab between cleanup retries: %w", err)
	}
	server.grabbed = true
	return nil
}

func (server *guardianServer) releaseOwnedInput() error {
	var releaseErr error
	for index := len(server.inputOrder) - 1; index >= 0; index-- {
		owned := server.inputOrder[index]
		releaseType := owned.eventType + 1
		if err := server.connection.FakeInput(releaseType, owned.detail, owned.root, 0, 0); err != nil {
			releaseErr = errors.Join(releaseErr, fmt.Errorf("release X11 guardian input %d/%d: %w", owned.eventType, owned.detail, err))
			continue
		}
		if owned.eventType == byte(xproto.KeyPress) {
			delete(server.ownedKeys, owned.detail)
		} else {
			delete(server.ownedButtons, owned.detail)
		}
		server.inputOrder[index] = guardianOwnedInput{}
	}
	kept := server.inputOrder[:0]
	for _, owned := range server.inputOrder {
		if owned.eventType != 0 {
			kept = append(kept, owned)
		}
	}
	server.inputOrder = kept
	return releaseErr
}

func (server *guardianServer) restoreMappingsOnce() (bool, error) {
	if len(server.mappings) == 0 {
		return false, nil
	}
	pressed, err := server.connection.PressedKeys()
	if err != nil {
		return true, fmt.Errorf("query pressed keys during X11 guardian cleanup: %w", err)
	}
	modifierList, err := server.connection.ModifierMapping()
	if err != nil {
		return true, fmt.Errorf("query modifiers during X11 guardian cleanup: %w", err)
	}
	modifiers := make(map[xproto.Keycode]struct{}, len(modifierList))
	for _, code := range modifierList {
		if code != 0 {
			modifiers[code] = struct{}{}
		}
	}
	var restoreErr error
	retry := false
	for index := len(server.mappingOrder) - 1; index >= 0; index-- {
		code := server.mappingOrder[index]
		claim := server.mappings[code]
		if claim == nil {
			continue
		}
		current, mappingErr := server.connection.KeyboardMapping(code, 1)
		if mappingErr != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("query X11 guardian mapping %d: %w", code, mappingErr))
			retry = true
			continue
		}
		if guardianMappingsEqual(current, claim.before) {
			server.removeMappingClaim(code)
			continue
		}
		if !guardianMappingsEqual(current, claim.after) && claim.unresolved {
			restoreErr = errors.Join(restoreErr, fmt.Errorf(
				"X11 guardian mapping %d ownership remains unresolved after a failed change", code,
			))
			retry = true
			continue
		}
		if !guardianMappingsEqual(current, claim.after) {
			// Ownership is deliberately conservative: an exact-after mismatch is
			// another client's state. Relinquish the claim without overwriting or
			// turning a successful preserve-foreign close into an error.
			server.removeMappingClaim(code)
			continue
		}
		if guardianKeycodePressed(pressed, code) {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("X11 guardian keycode %d remains pressed", code))
			retry = true
			continue
		}
		if _, modifier := modifiers[code]; modifier {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("X11 guardian keycode %d became a modifier", code))
			retry = true
			continue
		}
		if err := server.connection.ChangeKeyboardMapping(code, claim.before.KeysymsPerKeycode, claim.before.Keysyms); err != nil {
			claim.unresolved = true
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore X11 guardian mapping %d: %w", code, err))
			retry = true
			continue
		}
		if err := server.verifyMapping(code, claim.before); err != nil {
			restoreErr = errors.Join(restoreErr, err)
			retry = true
			continue
		}
		server.removeMappingClaim(code)
	}
	return retry, restoreErr
}

func guardianKeycodePressed(keys []byte, code xproto.Keycode) bool {
	index := int(code) / 8
	return index < len(keys) && keys[index]&(1<<uint(code%8)) != 0
}

func (server *guardianServer) closeConnection() error {
	if server.closed {
		return nil
	}
	server.closed = true
	done := server.beginConnectionClose()
	timeout := server.cleanupTimeout
	if timeout <= 0 {
		timeout = guardianDefaultCleanupTimeout
	}
	if !server.cleanupDeadline.IsZero() {
		timeout = time.Until(server.cleanupDeadline)
	}
	if timeout <= 0 {
		select {
		case <-done:
			return server.connectionCloseErr
		default:
			return errors.New("X11 guardian transport close exceeded the cleanup deadline")
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return server.connectionCloseErr
	case <-timer.C:
		return fmt.Errorf("X11 guardian transport close did not finish within %s", timeout)
	}
}

func (server *guardianServer) beginConnectionClose() <-chan struct{} {
	server.connectionCloseOnce.Do(func() {
		server.connectionCloseDone = make(chan struct{})
		go func() {
			server.connectionCloseErr = server.connection.Close()
			close(server.connectionCloseDone)
		}()
	})
	return server.connectionCloseDone
}
