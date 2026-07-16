//go:build linux

package x11input

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/jezek/xgb/xproto"
	"golang.org/x/sys/unix"
)

const (
	guardianEnvironmentToken = "ROBOTGO_INTERNAL_X11_GUARDIAN_TOKEN"
	guardianReexecArgument   = "--robotgo-internal-x11-guardian"
	guardianSocketPrefix     = "robotgo-x11-guardian-"

	guardianDefaultStartupTimeout = 5 * time.Second
	guardianDefaultRequestTimeout = 15 * time.Second
	guardianDefaultCleanupTimeout = 5 * time.Second
	guardianDefaultCrashSettle    = 2 * time.Second
	guardianMaximumDuration       = time.Minute
	guardianRequestCleanupMargin  = 250 * time.Millisecond
	guardianMinimumReapTimeout    = 250 * time.Millisecond
)

// GuardianOptions controls the bounded lifecycle of a Pure-Go X11 guardian.
// Zero values select conservative defaults. Executable is primarily a test
// seam; production callers should leave it empty to re-exec /proc/self/exe.
type GuardianOptions struct {
	Executable     string
	StartupTimeout time.Duration
	RequestTimeout time.Duration
	CleanupTimeout time.Duration
	CrashSettle    time.Duration
}

// GuardianDialer moves ownership of an X11 connection into a re-executed
// helper over a token-authenticated Linux abstract socket whose kernel peer
// credentials are verified by the parent. The helper survives a targeted
// SIGKILL of its parent and restores connection-owned input and keyboard
// mappings when the control socket closes.
type GuardianDialer struct {
	Options GuardianOptions
}

// NewGuardianDialer returns a Dialer whose X11 connections live in a
// re-executed guardian process. It never falls back to an in-process XGB
// connection when guardian startup or authentication fails.
func NewGuardianDialer(options GuardianOptions) Dialer {
	return GuardianDialer{Options: options}
}

type guardianResponse struct {
	envelope guardianEnvelope
	err      error
}

type guardianConnection struct {
	control io.ReadWriteCloser
	writer  guardianFramedWriter
	process *exec.Cmd

	requestTimeout time.Duration
	cleanupTimeout time.Duration

	mu                sync.Mutex
	nextID            uint64
	pending           map[uint64]chan guardianResponse
	readErr           error
	controlErr        error
	terminalEvent     guardianEvent
	terminalRecorded  bool
	terminalDelivered bool
	terminalSignal    chan struct{}
	terminalOnce      sync.Once

	events chan guardianEvent
	done   chan struct{}

	finishOnce  sync.Once
	closeOnce   sync.Once
	closeErr    error
	processDone chan error
}

// Dial starts a guardian and opens the requested display inside that helper.
func (dialer GuardianDialer) Dial(display string) (Connection, error) {
	if display == "" {
		return nil, errors.New("X11 guardian display is empty")
	}
	options, err := normalizeGuardianOptions(dialer.Options)
	if err != nil {
		return nil, err
	}
	token, err := newGuardianToken()
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", guardianAbstractSocketAddress(token))
	if err != nil {
		return nil, fmt.Errorf("listen on X11 guardian abstract socket: %w", err)
	}
	defer func() { _ = listener.Close() }()
	command := exec.Command(options.Executable, guardianReexecArgument)
	command.Env = guardianChildEnvironment(os.Environ(), token)
	if err := command.Start(); err != nil {
		if options.Executable == "/proc/self/exe" {
			return nil, fmt.Errorf(
				"start X11 guardian by re-executing %q (Linux procfs must remain accessible): %w",
				options.Executable, err,
			)
		}
		return nil, fmt.Errorf("start X11 guardian: %w", err)
	}
	control, err := acceptGuardianControl(listener, command, options.StartupTimeout)
	if err != nil {
		terminationErr := terminateGuardianCommand(command, options.StartupTimeout, "failed startup")
		return nil, errors.Join(fmt.Errorf("accept authenticated X11 guardian control connection: %w", err), terminationErr)
	}
	connection := newGuardianConnection(control, command, options)

	hello := guardianHelloRequest{
		Token:              token,
		Display:            display,
		RequestTimeoutNano: int64(options.RequestTimeout),
		CleanupTimeoutNano: int64(options.CleanupTimeout),
		CrashSettleNano:    int64(options.CrashSettle),
	}
	if err := connection.requestWithTimeout(guardianOperationHello, hello, nil, options.StartupTimeout); err != nil {
		connection.finish(err)
		terminationErr := connection.terminateStartupProcess(options.StartupTimeout)
		return nil, errors.Join(
			fmt.Errorf("initialize X11 guardian: %w", err),
			terminationErr,
		)
	}
	return connection, nil
}

func normalizeGuardianOptions(options GuardianOptions) (GuardianOptions, error) {
	if options.Executable == "" {
		options.Executable = "/proc/self/exe"
		if _, err := os.Stat(options.Executable); err != nil {
			return GuardianOptions{}, fmt.Errorf(
				"resolve X11 guardian executable %q (Linux procfs must expose /proc/self/exe): %w",
				options.Executable, err,
			)
		}
	}
	if options.StartupTimeout == 0 {
		options.StartupTimeout = guardianDefaultStartupTimeout
	}
	if options.RequestTimeout == 0 {
		options.RequestTimeout = guardianDefaultRequestTimeout
	}
	if options.CleanupTimeout == 0 {
		options.CleanupTimeout = guardianDefaultCleanupTimeout
	}
	if options.CrashSettle == 0 {
		options.CrashSettle = guardianDefaultCrashSettle
	}
	for name, duration := range map[string]time.Duration{
		"startup timeout": options.StartupTimeout,
		"request timeout": options.RequestTimeout,
		"cleanup timeout": options.CleanupTimeout,
		"crash settle":    options.CrashSettle,
	} {
		if duration < 0 || duration > guardianMaximumDuration {
			return GuardianOptions{}, fmt.Errorf("X11 guardian %s %s is outside 0..%s", name, duration, guardianMaximumDuration)
		}
	}
	return options, nil
}

func newGuardianToken() (string, error) {
	var token [32]byte
	if _, err := io.ReadFull(rand.Reader, token[:]); err != nil {
		return "", fmt.Errorf("create X11 guardian token: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

func guardianChildEnvironment(environment []string, token string) []string {
	clean := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if hasEnvironmentKey(entry, guardianEnvironmentToken) {
			continue
		}
		clean = append(clean, entry)
	}
	return append(clean, guardianEnvironmentToken+"="+token)
}

func hasEnvironmentKey(entry, key string) bool {
	return len(entry) > len(key) && entry[:len(key)] == key && entry[len(key)] == '='
}

func guardianAbstractSocketAddress(token string) *net.UnixAddr {
	digest := sha256.Sum256([]byte(guardianSocketPrefix + token))
	return &net.UnixAddr{
		Name: "\x00" + guardianSocketPrefix + hex.EncodeToString(digest[:]),
		Net:  "unix",
	}
}

func acceptGuardianControl(listener *net.UnixListener, command *exec.Cmd, timeout time.Duration) (*net.UnixConn, error) {
	if timeout <= 0 {
		timeout = guardianDefaultStartupTimeout
	}
	if err := listener.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("set guardian accept deadline: %w", err)
	}
	for {
		control, err := listener.AcceptUnix()
		if err != nil {
			return nil, err
		}
		pid, uid, credentialErr := guardianPeerCredentials(control)
		if credentialErr == nil && command.Process != nil &&
			pid == command.Process.Pid && uid == uint32(os.Geteuid()) {
			return control, nil
		}
		_ = control.Close()
		if credentialErr != nil {
			return nil, fmt.Errorf("inspect guardian peer credentials: %w", credentialErr)
		}
	}
}

func guardianPeerCredentials(control *net.UnixConn) (int, uint32, error) {
	raw, err := control.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var (
		credentials *unix.Ucred
		controlErr  error
	)
	if err := raw.Control(func(fd uintptr) {
		credentials, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if controlErr != nil {
		return 0, 0, controlErr
	}
	if credentials == nil || credentials.Pid <= 0 {
		return 0, 0, errors.New("guardian peer returned invalid credentials")
	}
	return int(credentials.Pid), credentials.Uid, nil
}

func newGuardianConnection(control io.ReadWriteCloser, process *exec.Cmd, options GuardianOptions) *guardianConnection {
	connection := &guardianConnection{
		control:        control,
		writer:         guardianFramedWriter{writer: control},
		process:        process,
		requestTimeout: options.RequestTimeout,
		cleanupTimeout: options.CleanupTimeout,
		pending:        make(map[uint64]chan guardianResponse),
		events:         make(chan guardianEvent, 256),
		done:           make(chan struct{}),
		terminalSignal: make(chan struct{}),
		processDone:    make(chan error, 1),
	}
	go connection.readFrames()
	if process != nil {
		go func() {
			connection.processDone <- process.Wait()
			close(connection.processDone)
		}()
	} else {
		close(connection.processDone)
	}
	return connection
}

func (connection *guardianConnection) readFrames() {
	for {
		envelope, err := readGuardianEnvelope(connection.control)
		if err != nil {
			connection.finish(err)
			return
		}
		switch envelope.Kind {
		case guardianFrameResponse:
			connection.mu.Lock()
			response := connection.pending[envelope.ID]
			delete(connection.pending, envelope.ID)
			connection.mu.Unlock()
			if response != nil {
				response <- guardianResponse{envelope: envelope}
			}
		case guardianFrameEvent:
			var event guardianEvent
			if err := decodeGuardianPayload(envelope.Payload, &event); err != nil {
				connection.finish(err)
				return
			}
			if !event.Open || event.Error != "" {
				connection.recordTerminalEvent(event)
				terminalErr := error(io.EOF)
				if event.Error != "" {
					terminalErr = errors.New(event.Error)
				}
				connection.finish(terminalErr)
				return
			}
			select {
			case connection.events <- event:
			default:
				// Backend consumes events only as a connection-health signal. Dropping
				// a redundant event cannot lose input or protocol state.
			}
		default:
			connection.finish(fmt.Errorf("unexpected guardian frame kind %q", envelope.Kind))
			return
		}
	}
}

func (connection *guardianConnection) finish(err error) {
	connection.finishOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}
		terminal := guardianEvent{Open: false, Error: err.Error()}
		connection.recordTerminalEvent(terminal)
		connection.mu.Lock()
		connection.readErr = err
		pending := connection.pending
		connection.pending = make(map[uint64]chan guardianResponse)
		connection.mu.Unlock()
		for _, response := range pending {
			response <- guardianResponse{err: err}
		}
		// Shutdown wakes both the local reader and the guardian peer before Close.
		var controlErr error
		if shutdownErr := shutdownGuardianControl(connection.control); shutdownErr != nil {
			controlErr = shutdownErr
		}
		closeErr := connection.control.Close()
		if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) && !errors.Is(closeErr, net.ErrClosed) {
			controlErr = errors.Join(controlErr, closeErr)
		}
		connection.mu.Lock()
		connection.controlErr = errors.Join(connection.controlErr, controlErr)
		connection.mu.Unlock()
		close(connection.done)
	})
}

type guardianSyscallControl interface {
	SyscallConn() (syscall.RawConn, error)
}

func shutdownGuardianControl(control io.ReadWriteCloser) error {
	socket, ok := control.(guardianSyscallControl)
	if !ok {
		return nil
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return fmt.Errorf("access X11 guardian control socket: %w", err)
	}
	var shutdownErr error
	if err := raw.Control(func(fd uintptr) {
		shutdownErr = unix.Shutdown(int(fd), unix.SHUT_RDWR)
	}); err != nil {
		return fmt.Errorf("access X11 guardian control descriptor: %w", err)
	}
	if shutdownErr != nil && !errors.Is(shutdownErr, unix.ENOTCONN) &&
		!errors.Is(shutdownErr, unix.EINVAL) && !errors.Is(shutdownErr, unix.EBADF) {
		return fmt.Errorf("shutdown X11 guardian control socket: %w", shutdownErr)
	}
	return nil
}

func (connection *guardianConnection) recordTerminalEvent(event guardianEvent) {
	connection.terminalOnce.Do(func() {
		connection.mu.Lock()
		connection.terminalEvent = event
		connection.terminalRecorded = true
		connection.mu.Unlock()
		close(connection.terminalSignal)
	})
}

func (connection *guardianConnection) takeTerminalEvent() (guardianEvent, bool, bool, <-chan struct{}) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.terminalRecorded && !connection.terminalDelivered {
		connection.terminalDelivered = true
		return connection.terminalEvent, true, false, nil
	}
	if connection.terminalDelivered {
		return guardianEvent{}, false, true, nil
	}
	return guardianEvent{}, false, false, connection.terminalSignal
}

func (connection *guardianConnection) request(operation string, request, response any) error {
	return connection.requestWithTimeout(
		operation,
		request,
		response,
		connection.requestTimeout+connection.cleanupTimeout+guardianRequestCleanupMargin,
	)
}

func (connection *guardianConnection) requestWithTimeout(operation string, request, response any, timeout time.Duration) error {
	payload, err := guardianPayload(request)
	if err != nil {
		return err
	}
	connection.mu.Lock()
	if connection.readErr != nil {
		err := connection.readErr
		connection.mu.Unlock()
		return err
	}
	connection.nextID++
	id := connection.nextID
	result := make(chan guardianResponse, 1)
	connection.pending[id] = result
	connection.mu.Unlock()

	envelope := guardianEnvelope{
		Version:   guardianProtocolVersion,
		Kind:      guardianFrameRequest,
		ID:        id,
		Operation: operation,
		Payload:   payload,
	}
	if err := connection.writer.write(envelope); err != nil {
		connection.removePending(id)
		connection.finish(err)
		return err
	}
	if timeout <= 0 {
		timeout = guardianDefaultRequestTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case received := <-result:
		if received.err != nil {
			return received.err
		}
		if received.envelope.Error != "" {
			return errors.New(received.envelope.Error)
		}
		return decodeGuardianPayload(received.envelope.Payload, response)
	case <-timer.C:
		connection.removePending(id)
		timeoutErr := fmt.Errorf("X11 guardian %s timed out after %s", operation, timeout)
		// A timed-out state-changing request has an unknowable outcome. Tear down
		// the control channel so the helper performs its crash-safe cleanup rather
		// than allowing later requests to continue from ambiguous state.
		connection.finish(timeoutErr)
		return timeoutErr
	}
}

func (connection *guardianConnection) removePending(id uint64) {
	connection.mu.Lock()
	delete(connection.pending, id)
	connection.mu.Unlock()
}

func (connection *guardianConnection) WaitForEvent() (bool, error) {
	for {
		event, ok, delivered, terminalSignal := connection.takeTerminalEvent()
		if ok {
			if event.Error != "" {
				return event.Open, errors.New(event.Error)
			}
			return event.Open, nil
		} else if delivered {
			return false, nil
		}
		select {
		case event := <-connection.events:
			// A terminal frame may have arrived concurrently with this ordinary
			// event. It takes precedence; ordinary events are health hints and may
			// be coalesced without losing protocol or input state.
			if terminal, ok, _, _ := connection.takeTerminalEvent(); ok {
				if terminal.Error != "" {
					return terminal.Open, errors.New(terminal.Error)
				}
				return terminal.Open, nil
			}
			return event.Open, nil
		case <-terminalSignal:
			continue
		case <-connection.done:
			if terminal, ok, _, _ := connection.takeTerminalEvent(); ok {
				if terminal.Error != "" {
					return terminal.Open, errors.New(terminal.Error)
				}
				return terminal.Open, nil
			}
			return false, nil
		}
	}
}

func (connection *guardianConnection) terminateStartupProcess(timeout time.Duration) error {
	return connection.terminateProcess(timeout, "failed startup")
}

func (connection *guardianConnection) terminateProcess(timeout time.Duration, reason string) error {
	if connection.process == nil || connection.process.Process == nil {
		return nil
	}
	var killErr error
	killed := false
	if err := connection.process.Process.Kill(); err == nil {
		killed = true
	} else if !errors.Is(err, os.ErrProcessDone) {
		killErr = fmt.Errorf("kill X11 guardian after %s: %w", reason, err)
	}
	reapErr := connection.waitForProcess(timeout)
	if killed && guardianKilledExit(reapErr) {
		// A non-success wait status is the expected result of our SIGKILL. Wait
		// still completed, so the helper was reaped and did not become orphaned.
		reapErr = nil
	}
	if reapErr != nil {
		reapErr = fmt.Errorf("reap X11 guardian after %s: %w", reason, reapErr)
	}
	return errors.Join(killErr, reapErr)
}

func (connection *guardianConnection) Close() error {
	connection.closeOnce.Do(func() {
		requestErr := connection.requestWithTimeout(
			guardianOperationClose, nil, nil, connection.cleanupTimeout+connection.requestTimeout,
		)
		connection.finish(io.EOF)
		processTimeout := connection.cleanupTimeout + connection.requestTimeout + guardianRequestCleanupMargin
		processErr := connection.waitForProcess(processTimeout)
		if processErr != nil {
			reapTimeout := connection.cleanupTimeout
			if reapTimeout < guardianMinimumReapTimeout {
				reapTimeout = guardianMinimumReapTimeout
			}
			processErr = errors.Join(processErr, connection.terminateProcess(reapTimeout, "exit timeout"))
		}
		connection.mu.Lock()
		controlErr := connection.controlErr
		connection.mu.Unlock()
		connection.closeErr = errors.Join(requestErr, controlErr, processErr)
	})
	return connection.closeErr
}

func terminateGuardianCommand(command *exec.Cmd, timeout time.Duration, reason string) error {
	if command == nil || command.Process == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	killErr := command.Process.Kill()
	killed := killErr == nil
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	if timeout <= 0 {
		timeout = guardianDefaultStartupTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case waitErr := <-done:
		if killed && guardianKilledExit(waitErr) {
			waitErr = nil
		}
		return errors.Join(killErr, waitErr)
	case <-timer.C:
		return errors.Join(killErr, fmt.Errorf("reap X11 guardian after %s timed out after %s", reason, timeout))
	}
}

func guardianKilledExit(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ProcessState == nil {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}

func (connection *guardianConnection) waitForProcess(timeout time.Duration) error {
	if connection.process == nil {
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err, open := <-connection.processDone:
		if !open {
			return nil
		}
		return err
	case <-timer.C:
		return fmt.Errorf("X11 guardian process did not exit within %s", timeout)
	}
}

func (connection *guardianConnection) Setup() (Setup, error) {
	var response guardianSetupResponse
	err := connection.request(guardianOperationSetup, nil, &response)
	return response.Setup, err
}

func (connection *guardianConnection) InitXTest() error {
	return connection.request(guardianOperationInitXTest, nil, nil)
}

func (connection *guardianConnection) XTestVersion(major byte, minor uint16) (XTestVersion, error) {
	var response guardianXTestVersionResponse
	err := connection.request(guardianOperationXTestVersion, guardianXTestVersionRequest{Major: major, Minor: minor}, &response)
	return response.Version, err
}

func (connection *guardianConnection) GrabServer() error {
	return connection.request(guardianOperationGrabServer, nil, nil)
}

func (connection *guardianConnection) UngrabServer() error {
	return connection.request(guardianOperationUngrabServer, nil, nil)
}

func (connection *guardianConnection) KeyboardMapping(first xproto.Keycode, count byte) (KeyboardMapping, error) {
	var response guardianKeyboardMappingResponse
	err := connection.request(guardianOperationKeyboardMapping, guardianKeyboardMappingRequest{First: first, Count: count}, &response)
	return response.Mapping, err
}

func (connection *guardianConnection) ModifierMapping() ([]xproto.Keycode, error) {
	var response guardianModifierMappingResponse
	err := connection.request(guardianOperationModifierMapping, nil, &response)
	return response.Keycodes, err
}

func (connection *guardianConnection) ChangeKeyboardMapping(first xproto.Keycode, perKeycode byte, keysyms []xproto.Keysym) error {
	return connection.request(guardianOperationChangeKeyboardMapping, guardianChangeKeyboardMappingRequest{
		First: first, PerKeycode: perKeycode, Keysyms: append([]xproto.Keysym(nil), keysyms...),
	}, nil)
}

func (connection *guardianConnection) PressedKeys() ([]byte, error) {
	var response guardianPressedKeysResponse
	err := connection.request(guardianOperationPressedKeys, nil, &response)
	return response.Keys, err
}

func (connection *guardianConnection) QueryPointer(root xproto.Window) (PointerState, error) {
	var response guardianQueryPointerResponse
	err := connection.request(guardianOperationQueryPointer, guardianQueryPointerRequest{Root: root}, &response)
	return response.State, err
}

func (connection *guardianConnection) FakeInput(eventType, detail byte, root xproto.Window, x, y int16) error {
	return connection.request(guardianOperationFakeInput, guardianFakeInputRequest{
		EventType: eventType, Detail: detail, Root: root, X: x, Y: y,
	}, nil)
}

func guardianTokenFromEnvironment() (string, bool) {
	token := os.Getenv(guardianEnvironmentToken)
	if len(token) != 64 {
		return "", false
	}
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return "", false
	}
	return token, true
}
