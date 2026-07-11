//go:build linux

package portal

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestOpenRemoteDesktopScreenCastRejectionCleansUp(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.selectSourcesCode = 2
	_, err := openTestSessionWithOptions(context.Background(), portal, OpenOptions{
		Devices: DevicePointer, Sources: SourceMonitor,
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("openRemoteDesktop error = %v, want ErrRejected", err)
	}
	portal.mu.Lock()
	defer portal.mu.Unlock()
	if len(portal.closeSessions) != 1 || !portal.closed {
		t.Fatalf("cleanup after ScreenCast rejection: sessions=%v closed=%v", portal.closeSessions, portal.closed)
	}
}

func TestOpenRemoteDesktopRequiresSelectedStreamResult(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	_, err := openTestSessionWithOptions(context.Background(), portal, OpenOptions{
		Devices: DevicePointer, Sources: SourceMonitor,
	})
	if !errors.Is(err, ErrScreenCastRequired) {
		t.Fatalf("openRemoteDesktop error = %v, want ErrScreenCastRequired", err)
	}
}

func TestRemoteDesktopScreenCastMappingAndAbsoluteInput(t *testing.T) {
	portal := newFakeRemoteDesktopPortal()
	portal.granted = DevicePointer | DeviceTouchscreen
	portal.restoreToken = "restore-next"
	portal.streams = []rawStream{{
		NodeID: 77,
		Properties: map[string]dbus.Variant{
			"id":              dbus.MakeVariant("monitor-1"),
			"position":        dbus.MakeVariant(dbusPoint{X: -1920, Y: 0}),
			"size":            dbus.MakeVariant(dbusPoint{X: 1920, Y: 1080}),
			"source_type":     dbus.MakeVariant(uint32(SourceMonitor)),
			"mapping_id":      dbus.MakeVariant("mapping-1"),
			"pipewire-serial": dbus.MakeVariant(uint64(9001)),
		},
	}}
	options := OpenOptions{
		Devices:      DevicePointer | DeviceTouchscreen,
		Sources:      SourceMonitor,
		Multiple:     true,
		CursorMode:   CursorMetadata,
		PersistMode:  PersistExplicit,
		RestoreToken: "restore-old",
	}
	session, err := openTestSessionWithOptions(context.Background(), portal, options)
	if err != nil {
		t.Fatalf("openRemoteDesktop error: %v", err)
	}
	defer func() { _ = session.Close() }()

	streams := session.Streams()
	if len(streams) != 1 || streams[0].NodeID != 77 || !streams[0].HasPosition || !streams[0].HasSize {
		t.Fatalf("streams = %#v", streams)
	}
	if streams[0].Position != (Point{X: -1920, Y: 0}) || streams[0].Size != (Size{Width: 1920, Height: 1080}) {
		t.Fatalf("stream geometry = position %#v size %#v", streams[0].Position, streams[0].Size)
	}
	if streams[0].MappingID != "mapping-1" || streams[0].PipeWireSerial != 9001 || session.RestoreToken() != "restore-next" {
		t.Fatalf("stream/session metadata = %#v restore=%q", streams[0], session.RestoreToken())
	}
	if err := session.PointerMotionAbsolute(context.Background(), 77, 100, 200); err != nil {
		t.Fatalf("PointerMotionAbsolute error: %v", err)
	}
	if err := session.TouchDown(context.Background(), 77, 1, 100, 200); err != nil {
		t.Fatalf("TouchDown error: %v", err)
	}
	if err := session.TouchMotion(context.Background(), 77, 1, 101, 201); err != nil {
		t.Fatalf("TouchMotion error: %v", err)
	}
	if err := session.TouchUp(context.Background(), 1); err != nil {
		t.Fatalf("TouchUp error: %v", err)
	}
	if err := session.PointerMotionAbsolute(context.Background(), 77, 1920, 0); err == nil {
		t.Fatal("out-of-range absolute coordinate unexpectedly accepted")
	}
	if err := session.PointerMotionAbsolute(context.Background(), 77, math.NaN(), 0); err == nil {
		t.Fatal("non-finite absolute coordinate unexpectedly accepted")
	}
	if err := session.PointerMotionAbsolute(context.Background(), 88, 1, 1); !errors.Is(err, ErrStreamNotFound) {
		t.Fatalf("unknown stream error = %v, want ErrStreamNotFound", err)
	}

	portal.mu.Lock()
	defer portal.mu.Unlock()
	if got := portal.selectDeviceOptions["persist_mode"].Value(); got != uint32(PersistExplicit) {
		t.Fatalf("persist_mode = %#v", got)
	}
	if got := portal.selectDeviceOptions["restore_token"].Value(); got != "restore-old" {
		t.Fatalf("restore_token = %#v", got)
	}
	if got := portal.selectSourceOptions["types"].Value(); got != uint32(SourceMonitor) {
		t.Fatalf("source types = %#v", got)
	}
	if got := portal.selectSourceOptions["cursor_mode"].Value(); got != uint32(CursorMetadata) {
		t.Fatalf("cursor_mode = %#v", got)
	}
	wantMethods := []string{notifyPointerAbsolute, notifyTouchDown, notifyTouchMotion, notifyTouchUp}
	if len(portal.notifications) != len(wantMethods) {
		t.Fatalf("notifications = %#v", portal.notifications)
	}
	for i, want := range wantMethods {
		if portal.notifications[i].method != want {
			t.Fatalf("notification %d method = %q, want %q", i, portal.notifications[i].method, want)
		}
	}
}
