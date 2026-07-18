//go:build linux

package waylandoutput

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestEnumerateCoreOutputScaleAndTransformFallback(t *testing.T) {
	runtimeDir := t.TempDir()
	socketName := "wayland-core-output-test"
	listener, err := net.Listen("unix", filepath.Join(runtimeDir, socketName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	t.Setenv(envXDGRuntimeDir, runtimeDir)
	t.Setenv(envWaylandDisplay, socketName)

	serverDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		serverDone <- serveCoreOutput(conn)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snapshot, err := Enumerate(ctx)
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	want := Snapshot{
		Outputs: []Output{
			{
				GlobalName: 30,
				X:          -540,
				Y:          100,
				Width:      540,
				Height:     960,
				Scale:      2,
				Transform:  1,
			},
		},
		OutputVersion: 2,
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("Enumerate() = %+v, want %+v", snapshot, want)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("fake compositor: %v", err)
	}
}

func serveCoreOutput(conn net.Conn) error {
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	sender, opcode, payload, err := readTestMessage(conn)
	if err != nil {
		return err
	}
	if sender != displayObjectID || opcode != displayRegistry || len(payload) != 4 {
		return fmt.Errorf("unexpected get-registry request")
	}
	registryID := binary.NativeEndian.Uint32(payload)
	callbackID, err := readSyncRequest(conn)
	if err != nil {
		return err
	}
	events := testEvent(
		registryID,
		eventRegistryGlobal,
		registryGlobalPayload(30, interfaceOutput, 2),
	)
	events = append(events, testEvent(callbackID, eventCallbackDone, uint32Value(0))...)
	if err := writeFragmented(conn, events); err != nil {
		return err
	}

	sender, opcode, payload, err = readTestMessage(conn)
	if err != nil {
		return err
	}
	if sender != registryID || opcode != registryBind {
		return fmt.Errorf("unexpected output bind request")
	}
	reader := newPayloadReader(payload)
	globalName, ok := reader.uint32()
	if !ok || globalName != 30 {
		return fmt.Errorf("bound global = %d, want 30", globalName)
	}
	iface, ok := reader.string()
	if !ok || iface != interfaceOutput {
		return fmt.Errorf("bound interface = %q, want %s", iface, interfaceOutput)
	}
	version, ok := reader.uint32()
	if !ok || version != 2 {
		return fmt.Errorf("bound version = %d, want 2", version)
	}
	outputID, ok := reader.uint32()
	if !ok || !reader.done() {
		return errors.New("malformed output bind")
	}
	secondCallbackID, err := readSyncRequest(conn)
	if err != nil {
		return err
	}

	geometry := int32Values(-540, 100, 0, 0, 0)
	geometry = append(geometry, stringValue("RobotGo")...)
	geometry = append(geometry, stringValue("Virtual")...)
	geometry = append(geometry, int32Values(1)...)
	events = testEvent(outputID, eventOutputGeometry, geometry)
	mode := append(uint32Value(outputModeCurrent), int32Values(1920, 1080, 60000)...)
	events = append(events, testEvent(outputID, eventOutputMode, mode)...)
	events = append(events, testEvent(outputID, eventOutputScale, int32Values(2))...)
	events = append(events, testEvent(outputID, eventOutputDone, nil)...)
	events = append(events, testEvent(secondCallbackID, eventCallbackDone, uint32Value(0))...)
	if err := writeFragmented(conn, events); err != nil {
		return err
	}

	var one [1]byte
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(one[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return fmt.Errorf("client connection remained open: n=%d err=%v", n, err)
	}
	return nil
}
