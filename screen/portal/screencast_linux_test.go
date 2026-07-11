//go:build linux

package portal

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

type fakeScreenCastPortal struct {
	mu sync.Mutex

	name            string
	signals         chan<- *dbus.Signal
	requestMatch    bool
	sessionMatch    bool
	connection      chan struct{}
	connectionOne   sync.Once
	selectCode      uint32
	startCode       uint32
	holdCreate      bool
	createEntered   chan struct{}
	holdSelect      bool
	selectEntered   chan struct{}
	malformedCreate bool
	selectOptions   map[string]dbus.Variant
	closeRequests   []dbus.ObjectPath
	closeSessions   []dbus.ObjectPath
	closed          bool
	readFD          int
	writeFD         int
}

func newFakeScreenCastPortal(t *testing.T) *fakeScreenCastPortal {
	t.Helper()
	fds := []int{0, 0}
	if err := unix.Pipe2(fds, unix.O_CLOEXEC); err != nil {
		t.Fatalf("create fake PipeWire pipe: %v", err)
	}
	portal := &fakeScreenCastPortal{
		name: ":1.42", connection: make(chan struct{}), readFD: fds[0], writeFD: fds[1],
	}
	t.Cleanup(func() {
		portal.mu.Lock()
		writeFD := portal.writeFD
		portal.writeFD = -1
		readFD := portal.readFD
		portal.readFD = -1
		portal.mu.Unlock()
		if writeFD >= 0 {
			_ = unix.Close(writeFD)
		}
		if readFD >= 0 {
			_ = unix.Close(readFD)
		}
	})
	return portal
}

func (p *fakeScreenCastPortal) uniqueName() string { return p.name }
func (p *fakeScreenCastPortal) addRequestMatch() error {
	p.mu.Lock()
	p.requestMatch = true
	p.mu.Unlock()
	return nil
}
func (p *fakeScreenCastPortal) removeRequestMatch() error {
	p.mu.Lock()
	p.requestMatch = false
	p.mu.Unlock()
	return nil
}
func (p *fakeScreenCastPortal) addSessionMatch() error {
	p.mu.Lock()
	p.sessionMatch = true
	p.mu.Unlock()
	return nil
}
func (p *fakeScreenCastPortal) removeSessionMatch() error {
	p.mu.Lock()
	p.sessionMatch = false
	p.mu.Unlock()
	return nil
}
func (p *fakeScreenCastPortal) registerSignals(ch chan<- *dbus.Signal) {
	p.mu.Lock()
	p.signals = ch
	p.mu.Unlock()
}
func (p *fakeScreenCastPortal) removeSignals(ch chan<- *dbus.Signal) {
	p.mu.Lock()
	if p.signals == ch {
		p.signals = nil
	}
	p.mu.Unlock()
}
func (p *fakeScreenCastPortal) connectionDone() <-chan struct{} { return p.connection }

func (p *fakeScreenCastPortal) createSession(_ context.Context, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	request := screenCastRequestPath(p.name, screenCastVariantString(options, screenCastOptionHandle))
	session := screenCastSessionPath(p.name, screenCastVariantString(options, screenCastOptionSessionHandle))
	if p.holdCreate {
		if p.createEntered != nil {
			close(p.createEntered)
		}
		return request, nil
	}
	if p.malformedCreate {
		return request, p.emitResponse(request, 0, map[string]dbus.Variant{})
	}
	return request, p.emitResponse(request, 0, map[string]dbus.Variant{screenCastResultSession: dbus.MakeVariant(string(session))})
}

func (p *fakeScreenCastPortal) selectSources(_ context.Context, _ dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	p.mu.Lock()
	p.selectOptions = options
	p.mu.Unlock()
	request := screenCastRequestPath(p.name, screenCastVariantString(options, screenCastOptionHandle))
	if p.holdSelect {
		if p.selectEntered != nil {
			close(p.selectEntered)
		}
		return request, nil
	}
	return request, p.emitResponse(request, p.selectCode, map[string]dbus.Variant{})
}

func (p *fakeScreenCastPortal) start(_ context.Context, _ dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, error) {
	request := screenCastRequestPath(p.name, screenCastVariantString(options, screenCastOptionHandle))
	streams := []screenCastRawStream{{NodeID: 77, Properties: map[string]dbus.Variant{
		screenCastStreamID: dbus.MakeVariant("stream-1"), screenCastStreamPosition: dbus.MakeVariant(screenCastDBusPoint{X: -1920, Y: 0}),
		screenCastStreamSize:       dbus.MakeVariant(screenCastDBusPoint{X: 1920, Y: 1080}),
		screenCastStreamSourceType: dbus.MakeVariant(uint32(ScreenCastSourceMonitor)),
		screenCastStreamMappingID:  dbus.MakeVariant("mapping-1"), screenCastStreamPipeWireSerial: dbus.MakeVariant(uint64(9001)),
	}}}
	return request, p.emitResponse(request, p.startCode, map[string]dbus.Variant{
		screenCastResultStreams: dbus.MakeVariant(streams), screenCastOptionRestoreToken: dbus.MakeVariant("restore-next"),
	})
}

func (p *fakeScreenCastPortal) openPipeWire(context.Context, dbus.ObjectPath) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fd := p.readFD
	p.readFD = -1
	return fd, nil
}

func (p *fakeScreenCastPortal) closeRequest(_ context.Context, path dbus.ObjectPath) error {
	p.mu.Lock()
	p.closeRequests = append(p.closeRequests, path)
	p.mu.Unlock()
	return nil
}

func (p *fakeScreenCastPortal) closeSession(_ context.Context, path dbus.ObjectPath) error {
	p.mu.Lock()
	p.closeSessions = append(p.closeSessions, path)
	p.mu.Unlock()
	return nil
}

func (p *fakeScreenCastPortal) close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.connectionOne.Do(func() { close(p.connection) })
	return nil
}

func (p *fakeScreenCastPortal) emitResponse(path dbus.ObjectPath, code uint32, results map[string]dbus.Variant) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.requestMatch || p.signals == nil {
		return errors.New("fake screencast response emitted before subscription")
	}
	p.signals <- &dbus.Signal{Name: portalResponse, Path: path, Body: []interface{}{code, results}}
	return nil
}

func screenCastVariantString(options map[string]dbus.Variant, key string) string {
	value, ok := options[key]
	if !ok {
		return ""
	}
	text, _ := value.Value().(string)
	return text
}

func TestOpenScreenCastSessionReusesPipeWireRemoteAndCleansUp(t *testing.T) {
	portal := newFakeScreenCastPortal(t)
	options := ScreenCastOptions{
		Sources: ScreenCastSourceMonitor, Multiple: true, Cursor: ScreenCastCursorEmbedded,
		Persist: ScreenCastPersistApplication, RestoreToken: "restore-old",
	}
	opened, err := openScreenCastWithPortal(context.Background(), portal, options)
	if err != nil {
		t.Fatalf("openScreenCastWithPortal error: %v", err)
	}
	session := opened.(*screenCastSession)
	streams := session.Streams()
	if len(streams) != 1 || streams[0].NodeID != 77 || streams[0].PipeWireSerial != 9001 || !streams[0].HasPosition || !streams[0].HasSize {
		t.Fatalf("streams = %#v", streams)
	}
	if session.RestoreToken() != "restore-next" {
		t.Fatalf("restore token = %q", session.RestoreToken())
	}

	first, err := session.PipeWireFile()
	if err != nil {
		t.Fatalf("first PipeWireFile error: %v", err)
	}
	second, err := session.PipeWireFile()
	if err != nil {
		_ = first.Close()
		t.Fatalf("second PipeWireFile error: %v", err)
	}
	if first.Fd() == second.Fd() {
		t.Fatal("PipeWireFile returned the same owned descriptor twice")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}
	if _, err := session.PipeWireFile(); !errors.Is(err, ErrScreenCastClosed) {
		t.Fatalf("PipeWireFile after close = %v, want ErrScreenCastClosed", err)
	}

	portal.mu.Lock()
	writeFD := portal.writeFD
	portal.mu.Unlock()
	if _, err := unix.Write(writeFD, []byte{'x'}); err != nil {
		t.Fatalf("write to duplicated PipeWire pipe: %v", err)
	}
	buffer := []byte{0}
	if _, err := io.ReadFull(first, buffer); err != nil || buffer[0] != 'x' {
		t.Fatalf("duplicated descriptor after session close = (%q, %v)", buffer, err)
	}
	_ = first.Close()
	_ = second.Close()

	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 1 || !portal.closed {
		t.Fatalf("cleanup: sessions=%v closed=%v", portal.closeSessions, portal.closed)
	}
	if screenCastVariantString(portal.selectOptions, screenCastOptionRestoreToken) != "restore-old" {
		t.Fatalf("select options = %#v", portal.selectOptions)
	}
}

func TestOpenScreenCastRejectionClosesCreatedSession(t *testing.T) {
	portal := newFakeScreenCastPortal(t)
	portal.selectCode = 2
	_, err := openScreenCastWithPortal(context.Background(), portal, ScreenCastOptions{Sources: ScreenCastSourceMonitor})
	if !errors.Is(err, ErrScreenCastRejected) {
		t.Fatalf("openScreenCastWithPortal error = %v, want ErrScreenCastRejected", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 1 || !portal.closed {
		t.Fatalf("cleanup: sessions=%v closed=%v", portal.closeSessions, portal.closed)
	}
}

func TestOpenScreenCastTimeoutClosesRequestAndSession(t *testing.T) {
	portal := newFakeScreenCastPortal(t)
	portal.holdSelect = true
	portal.selectEntered = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := openScreenCastWithPortal(ctx, portal, ScreenCastOptions{Sources: ScreenCastSourceMonitor})
		result <- err
	}()
	<-portal.selectEntered
	cancel()
	err := <-result
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("openScreenCastWithPortal error = %v, want context.Canceled", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeRequests) == 0 || len(portal.closeSessions) != 1 || !portal.closed {
		t.Fatalf("cleanup: requests=%v sessions=%v closed=%v", portal.closeRequests, portal.closeSessions, portal.closed)
	}
}

func TestOpenScreenCastCancellationDuringCreateClosesPredictedSession(t *testing.T) {
	portal := newFakeScreenCastPortal(t)
	portal.holdCreate = true
	portal.createEntered = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := openScreenCastWithPortal(ctx, portal, ScreenCastOptions{Sources: ScreenCastSourceMonitor})
		result <- err
	}()
	<-portal.createEntered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("openScreenCastWithPortal error = %v, want context.Canceled", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeRequests) != 1 || len(portal.closeSessions) != 1 || !portal.closed {
		t.Fatalf("cleanup: requests=%v sessions=%v closed=%v", portal.closeRequests, portal.closeSessions, portal.closed)
	}
}

func TestOpenScreenCastMalformedCreateResponseClosesPredictedSession(t *testing.T) {
	portal := newFakeScreenCastPortal(t)
	portal.malformedCreate = true
	_, err := openScreenCastWithPortal(context.Background(), portal, ScreenCastOptions{Sources: ScreenCastSourceMonitor})
	if err == nil {
		t.Fatal("malformed CreateSession response unexpectedly accepted")
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 1 || !portal.closed {
		t.Fatalf("cleanup: sessions=%v closed=%v", portal.closeSessions, portal.closed)
	}
}

func TestScreenCastOptionsRejectInvalidMasks(t *testing.T) {
	tests := []ScreenCastOptions{
		{},
		{Sources: 8},
		{Sources: ScreenCastSourceMonitor, Cursor: ScreenCastCursorHidden | ScreenCastCursorEmbedded},
		{Sources: ScreenCastSourceMonitor, Persist: 3},
	}
	for _, options := range tests {
		if err := validateScreenCastOptions(options); err == nil {
			t.Fatalf("options %#v unexpectedly accepted", options)
		}
	}
}

func TestDecodeScreenCastStreamsRejectsDuplicateAndInvalidGeometry(t *testing.T) {
	tests := []struct {
		name    string
		streams []screenCastRawStream
	}{
		{"duplicate node", []screenCastRawStream{{NodeID: 7}, {NodeID: 7}}},
		{"zero size", []screenCastRawStream{{NodeID: 7, Properties: map[string]dbus.Variant{
			screenCastStreamSize: dbus.MakeVariant(screenCastDBusPoint{}),
		}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeScreenCastStreams(map[string]dbus.Variant{screenCastResultStreams: dbus.MakeVariant(test.streams)})
			if err == nil {
				t.Fatal("invalid streams unexpectedly accepted")
			}
		})
	}
}

func TestParseScreenCastResponseClassifiesConsentResult(t *testing.T) {
	for code, want := range map[uint32]error{1: ErrScreenCastCancelled, 2: ErrScreenCastRejected} {
		_, err := parseScreenCastResponse(&dbus.Signal{Body: []interface{}{code, map[string]dbus.Variant{}}})
		if !errors.Is(err, want) {
			t.Fatalf("response %d error = %v, want %v", code, err, want)
		}
	}
	if _, err := parseScreenCastResponse(&dbus.Signal{Body: []interface{}{"bad", map[string]dbus.Variant{}}}); err == nil {
		t.Fatal("malformed response code unexpectedly accepted")
	}
}
