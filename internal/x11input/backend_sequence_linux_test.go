//go:build linux

package x11input

import (
	"sync"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
)

type sequencingFakeX11Dialer struct {
	server *fakeX11Server
	mu     sync.Mutex
	steps  [][]fakeInputStep
}

func (dialer *sequencingFakeX11Dialer) Dial(string) (Connection, error) {
	connection := &fakeX11Connection{server: dialer.server, closed: make(chan struct{})}
	dialer.server.mu.Lock()
	dialer.server.connections = append(dialer.server.connections, connection)
	dialer.server.mu.Unlock()
	return &sequencingFakeX11Connection{fakeX11Connection: connection, dialer: dialer}, nil
}

type sequencingFakeX11Connection struct {
	*fakeX11Connection
	dialer *sequencingFakeX11Dialer
}

func (connection *sequencingFakeX11Connection) FakeInputSequence(steps []fakeInputStep) error {
	copied := append([]fakeInputStep(nil), steps...)
	connection.dialer.mu.Lock()
	connection.dialer.steps = append(connection.dialer.steps, copied)
	connection.dialer.mu.Unlock()
	for _, step := range steps {
		if err := connection.FakeInput(step.eventType, step.detail, step.root, step.x, step.y); err != nil {
			return err
		}
	}
	return nil
}

func TestBackendSequencesOnlyBalancedTransientInput(t *testing.T) {
	server := newFakeX11Server()
	dialer := &sequencingFakeX11Dialer{server: server}
	backend := New(Config{
		ResolveDisplay: func() (string, error) { return ":fake", nil },
		Dialer:         dialer,
		KeyHoldDelay:   time.Nanosecond,
		Sleep:          func(time.Duration) {},
	})

	if err := backend.Click(ButtonLeft, false); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if err := backend.Scroll(0, 2); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	if err := backend.Key(KeyEvent{Keysym: 0xff0d, Tap: true}); err != nil {
		t.Fatalf("unmodified Key tap: %v", err)
	}
	if err := backend.Text(TextEvent{Keysyms: []uint32{0x0101f600}}); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if err := backend.Key(KeyEvent{Keysym: 0xff0d, Tap: true, Modifiers: []uint32{0xffe1}}); err != nil {
		t.Fatalf("modified Key tap: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dialer.mu.Lock()
	sequences := append([][]fakeInputStep(nil), dialer.steps...)
	dialer.mu.Unlock()
	if len(sequences) != 5 {
		t.Fatalf("balanced sequence count = %d, want 5 (click, two scroll pulses, key, text)", len(sequences))
	}
	wantDelays := []time.Duration{
		time.Nanosecond,
		0,
		0,
		time.Nanosecond,
		time.Nanosecond,
	}
	for index, steps := range sequences {
		if len(steps) != 2 {
			t.Fatalf("sequence %d has %d steps, want 2", index, len(steps))
		}
		if steps[0].eventType != byte(xproto.KeyPress) &&
			steps[0].eventType != byte(xproto.ButtonPress) {
			t.Fatalf("sequence %d starts with event type %d", index, steps[0].eventType)
		}
		if steps[1].eventType != byte(xproto.KeyRelease) &&
			steps[1].eventType != byte(xproto.ButtonRelease) {
			t.Fatalf("sequence %d ends with event type %d", index, steps[1].eventType)
		}
		if steps[0].delayAfter != wantDelays[index] || steps[1].delayAfter != 0 {
			t.Fatalf(
				"sequence %d delays = [%s, %s], want [%s, 0s]",
				index, steps[0].delayAfter, steps[1].delayAfter, wantDelays[index],
			)
		}
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.buttons) != 0 {
		t.Fatalf("sequenced input left held buttons: %v", server.buttons)
	}
	for _, pressed := range server.pressed {
		if pressed != 0 {
			t.Fatalf("sequenced input left held keys: %v", server.pressed)
		}
	}
}
