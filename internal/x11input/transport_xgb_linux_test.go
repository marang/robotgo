//go:build linux

package x11input

import (
	"sync"
	"testing"
	"time"

	"github.com/jezek/xgb"
)

type fakeXGBEvent struct{}

func (fakeXGBEvent) Bytes() []byte  { return nil }
func (fakeXGBEvent) String() string { return "fake XGB event" }

type fakeXGBEventSource struct {
	closeOnce   sync.Once
	closeCalled chan struct{}
	events      chan xgb.Event
}

func (source *fakeXGBEventSource) Close() {
	source.closeOnce.Do(func() { close(source.closeCalled) })
}

func (source *fakeXGBEventSource) WaitForEvent() (xgb.Event, xgb.Error) {
	event, open := <-source.events
	if !open {
		return nil, nil
	}
	return event, nil
}

func TestXGBConnectionCloseWaitsForEventTransportTermination(t *testing.T) {
	source := &fakeXGBEventSource{
		closeCalled: make(chan struct{}),
		events:      make(chan xgb.Event, 1),
	}
	source.events <- fakeXGBEvent{}
	connection := &xgbConnection{events: source}
	closed := make(chan struct{})
	go func() {
		_ = connection.Close()
		close(closed)
	}()
	select {
	case <-source.closeCalled:
	case <-time.After(time.Second):
		t.Fatal("transport Close was not called")
	}
	select {
	case <-closed:
		t.Fatal("xgbConnection.Close returned before the event transport terminated")
	case <-time.After(20 * time.Millisecond):
	}
	close(source.events)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("xgbConnection.Close did not return after event transport termination")
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
