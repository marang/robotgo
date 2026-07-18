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

func TestOutputStateResolveLogicalAndCoreFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		state          outputState
		requireLogical bool
		want           Output
		wantErr        error
	}{
		{
			name: "fractional logical geometry",
			state: outputState{
				globalName:          7,
				name:                "DP-1",
				scale:               2,
				transform:           1,
				logicalX:            -1280,
				logicalY:            120,
				logicalWidth:        1280,
				logicalHeight:       720,
				haveLogicalPosition: true,
				haveLogicalSize:     true,
			},
			requireLogical: true,
			want: Output{
				GlobalName: 7,
				Name:       "DP-1",
				X:          -1280,
				Y:          120,
				Width:      1280,
				Height:     720,
				Scale:      2,
				Transform:  1,
				Logical:    true,
			},
		},
		{
			name: "rotated scaled core fallback rounds outward",
			state: outputState{
				globalName:      8,
				coreX:           100,
				coreY:           -20,
				modeWidth:       1919,
				modeHeight:      1079,
				scale:           2,
				transform:       3,
				haveGeometry:    true,
				haveCurrentMode: true,
			},
			want: Output{
				GlobalName: 8,
				X:          100,
				Y:          -20,
				Width:      540,
				Height:     960,
				Scale:      2,
				Transform:  3,
			},
		},
		{
			name: "incomplete logical data",
			state: outputState{
				globalName:          9,
				scale:               1,
				haveLogicalPosition: true,
			},
			requireLogical: true,
			wantErr:        ErrProtocol,
		},
		{
			name: "invalid scale",
			state: outputState{
				globalName:      10,
				modeWidth:       1920,
				modeHeight:      1080,
				haveGeometry:    true,
				haveCurrentMode: true,
			},
			wantErr: ErrProtocol,
		},
		{
			name: "missing current mode",
			state: outputState{
				globalName:   11,
				scale:        1,
				haveGeometry: true,
			},
			wantErr: ErrProtocol,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := test.state.resolve(test.requireLogical)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("resolve() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && got != test.want {
				t.Fatalf("resolve() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestTransformRotationMatrix(t *testing.T) {
	t.Parallel()
	for transform := int32(0); transform <= 7; transform++ {
		want := transform == 1 || transform == 3 || transform == 5 || transform == 7
		if got := transformRotates(transform); got != want {
			t.Fatalf("transformRotates(%d) = %t, want %t", transform, got, want)
		}
	}
}

func TestCoreFallbackTransformMatrix(t *testing.T) {
	t.Parallel()
	for transform := int32(0); transform <= 7; transform++ {
		state := outputState{
			globalName:      uint32(transform + 1),
			modeWidth:       2000,
			modeHeight:      1000,
			scale:           2,
			transform:       transform,
			haveGeometry:    true,
			haveCurrentMode: true,
		}
		output, err := state.resolve(false)
		if err != nil {
			t.Fatalf("transform %d: %v", transform, err)
		}
		wantWidth, wantHeight := 1000, 500
		if transformRotates(transform) {
			wantWidth, wantHeight = 500, 1000
		}
		if output.Width != wantWidth || output.Height != wantHeight {
			t.Fatalf(
				"transform %d = %dx%d, want %dx%d",
				transform,
				output.Width,
				output.Height,
				wantWidth,
				wantHeight,
			)
		}
	}
}

func TestOutputDispatchRejectsMalformedGeometry(t *testing.T) {
	t.Parallel()
	state := outputState{scale: 1}
	if err := dispatchOutput(&state, eventOutputGeometry, []byte{0, 0, 0, 0}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("truncated geometry error = %v, want ErrProtocol", err)
	}

	geometry := int32Values(0, 0, 0, 0, 0)
	geometry = append(geometry, stringValue("RobotGo")...)
	geometry = append(geometry, stringValue("Virtual")...)
	geometry = append(geometry, int32Values(8)...)
	if err := dispatchOutput(&state, eventOutputGeometry, geometry); !errors.Is(err, ErrProtocol) {
		t.Fatalf("invalid transform error = %v, want ErrProtocol", err)
	}
}

func TestRegistryRemoveInvalidatesAdvertisedGlobal(t *testing.T) {
	t.Parallel()
	client := wireClient{removed: make(map[uint32]struct{})}
	if err := client.dispatchRegistry(
		eventRegistryGlobal,
		registryGlobalPayload(42, interfaceOutput, 4),
	); err != nil {
		t.Fatal(err)
	}
	if err := client.dispatchRegistry(eventRegistryRemove, uint32Value(42)); err != nil {
		t.Fatal(err)
	}
	if _, removed := client.removed[42]; !removed {
		t.Fatal("registry removal was not recorded")
	}
}

func TestEnumerateLogicalOutputsAndClampVersions(t *testing.T) {
	runtimeDir := t.TempDir()
	socketName := "wayland-enumeration-test"
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
		serverDone <- serveLogicalOutputs(conn)
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
				GlobalName: 50,
				Name:       "HDMI-A-1",
				X:          0,
				Y:          0,
				Width:      1920,
				Height:     1080,
				Scale:      1,
				Logical:    true,
			},
			{
				GlobalName: 10,
				Name:       "DP-1",
				X:          -1280,
				Y:          0,
				Width:      1280,
				Height:     720,
				Scale:      2,
				Transform:  1,
				Logical:    true,
			},
		},
		OutputVersion:    maxOutputVersion,
		XDGOutputVersion: maxXDGOutputManagerVersion,
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("Enumerate() = %+v, want %+v", snapshot, want)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("fake compositor: %v", err)
	}
}

func TestEnumerateDeadlineClosesStalledConnection(t *testing.T) {
	runtimeDir := t.TempDir()
	socketName := "wayland-timeout-test"
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
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_, copyErr := io.Copy(io.Discard, conn)
		serverDone <- copyErr
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = Enumerate(ctx)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Enumerate() error = %v, want ErrUnavailable", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Enumerate() exceeded bounded wait: %s", elapsed)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("stalled server did not observe clean connection close: %v", err)
	}
}

func TestEnumerateRejectsMissingDeadlineAndUnsafeSocketName(t *testing.T) {
	t.Setenv(envXDGRuntimeDir, t.TempDir())
	t.Setenv(envWaylandDisplay, "../outside")

	if _, err := Enumerate(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing deadline error = %v, want ErrUnavailable", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Enumerate(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unsafe socket error = %v, want ErrUnavailable", err)
	}
}

func TestReadMessageRejectsInvalidFrameSize(t *testing.T) {
	t.Parallel()
	clientConn, serverConn := net.Pipe()
	defer func() {
		_ = clientConn.Close()
	}()
	defer func() {
		_ = serverConn.Close()
	}()

	client := wireClient{conn: clientConn}
	go func() {
		var header [8]byte
		binary.NativeEndian.PutUint32(header[0:4], displayObjectID)
		binary.NativeEndian.PutUint32(header[4:8], uint32(7)<<16)
		_, _ = serverConn.Write(header[:])
	}()
	if _, _, _, err := client.readMessage(); !errors.Is(err, ErrProtocol) {
		t.Fatalf("readMessage() error = %v, want ErrProtocol", err)
	}
}

func serveLogicalOutputs(conn net.Conn) error {
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	sender, opcode, payload, err := readTestMessage(conn)
	if err != nil {
		return err
	}
	if sender != displayObjectID || opcode != displayRegistry || len(payload) != 4 {
		return fmt.Errorf("unexpected get-registry request sender=%d opcode=%d payload=%d", sender, opcode, len(payload))
	}
	registryID := binary.NativeEndian.Uint32(payload)

	callbackID, err := readSyncRequest(conn)
	if err != nil {
		return err
	}
	firstEvents := append(
		testEvent(registryID, eventRegistryGlobal, registryGlobalPayload(50, interfaceOutput, 9)),
		testEvent(registryID, eventRegistryGlobal, registryGlobalPayload(10, interfaceOutput, 4))...,
	)
	firstEvents = append(
		firstEvents,
		testEvent(registryID, eventRegistryGlobal, registryGlobalPayload(70, interfaceXDGOutputManager, 9))...,
	)
	firstEvents = append(firstEvents, testEvent(callbackID, eventCallbackDone, uint32Value(0))...)
	if err := writeFragmented(conn, firstEvents); err != nil {
		return err
	}

	outputIDs := make(map[uint32]uint32)
	outputGlobals := make(map[uint32]uint32)
	xdgOutputTargets := make(map[uint32]uint32)
	var managerID uint32
	var secondCallbackID uint32
	for secondCallbackID == 0 {
		sender, opcode, payload, err = readTestMessage(conn)
		if err != nil {
			return err
		}
		switch {
		case sender == registryID && opcode == registryBind:
			reader := newPayloadReader(payload)
			globalName, ok := reader.uint32()
			if !ok {
				return errors.New("malformed bind global")
			}
			iface, ok := reader.string()
			if !ok {
				return errors.New("malformed bind interface")
			}
			version, ok := reader.uint32()
			if !ok {
				return errors.New("malformed bind version")
			}
			objectID, ok := reader.uint32()
			if !ok || !reader.done() {
				return errors.New("malformed bind object")
			}
			switch iface {
			case interfaceOutput:
				if version != maxOutputVersion {
					return fmt.Errorf("wl_output version = %d, want %d", version, maxOutputVersion)
				}
				outputIDs[globalName] = objectID
				outputGlobals[objectID] = globalName
			case interfaceXDGOutputManager:
				if version != maxXDGOutputManagerVersion {
					return fmt.Errorf("xdg-output version = %d, want %d", version, maxXDGOutputManagerVersion)
				}
				managerID = objectID
			default:
				return fmt.Errorf("unexpected bind interface %q", iface)
			}
		case managerID != 0 && sender == managerID && opcode == xdgManagerGetOutput:
			if len(payload) != 8 {
				return errors.New("malformed get-xdg-output request")
			}
			xdgID := binary.NativeEndian.Uint32(payload[:4])
			outputID := binary.NativeEndian.Uint32(payload[4:])
			xdgOutputTargets[xdgID] = outputID
		case sender == displayObjectID && opcode == displaySync:
			if len(payload) != 4 {
				return errors.New("malformed second sync request")
			}
			secondCallbackID = binary.NativeEndian.Uint32(payload)
		default:
			return fmt.Errorf("unexpected second-stage request sender=%d opcode=%d", sender, opcode)
		}
	}
	if len(outputIDs) != 2 || managerID == 0 || len(xdgOutputTargets) != 2 {
		return fmt.Errorf(
			"incomplete bindings outputs=%d manager=%d xdg=%d",
			len(outputIDs),
			managerID,
			len(xdgOutputTargets),
		)
	}

	var events []byte
	for outputID, globalName := range outputGlobals {
		switch globalName {
		case 10:
			events = appendOutputEvents(events, outputID, -1280, 0, 2560, 1440, 2, 1, "DP-1")
		case 50:
			events = appendOutputEvents(events, outputID, 0, 0, 1920, 1080, 1, 0, "HDMI-A-1")
		default:
			return fmt.Errorf("unexpected output global %d", globalName)
		}
	}
	for xdgID, outputID := range xdgOutputTargets {
		switch outputGlobals[outputID] {
		case 10:
			events = appendXDGOutputEvents(events, xdgID, -1280, 0, 1280, 720, "DP-1")
		case 50:
			events = appendXDGOutputEvents(events, xdgID, 0, 0, 1920, 1080, "HDMI-A-1")
		default:
			return fmt.Errorf("xdg-output targets unknown output %d", outputID)
		}
	}
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

func readSyncRequest(conn net.Conn) (uint32, error) {
	sender, opcode, payload, err := readTestMessage(conn)
	if err != nil {
		return 0, err
	}
	if sender != displayObjectID || opcode != displaySync || len(payload) != 4 {
		return 0, fmt.Errorf("unexpected sync request sender=%d opcode=%d payload=%d", sender, opcode, len(payload))
	}
	return binary.NativeEndian.Uint32(payload), nil
}

func readTestMessage(conn net.Conn) (uint32, uint16, []byte, error) {
	client := wireClient{conn: conn}
	return client.readMessage()
}

func registryGlobalPayload(name uint32, iface string, version uint32) []byte {
	payload := uint32Value(name)
	payload = append(payload, stringValue(iface)...)
	return append(payload, uint32Value(version)...)
}

func appendOutputEvents(
	dst []byte,
	outputID uint32,
	x, y, width, height, scale, transform int32,
	name string,
) []byte {
	geometry := int32Values(x, y, 0, 0, 0)
	geometry = append(geometry, stringValue("RobotGo")...)
	geometry = append(geometry, stringValue("Virtual")...)
	geometry = append(geometry, int32Values(transform)...)
	dst = append(dst, testEvent(outputID, eventOutputGeometry, geometry)...)
	mode := append(uint32Value(outputModeCurrent), int32Values(width, height, 60000)...)
	dst = append(dst, testEvent(outputID, eventOutputMode, mode)...)
	dst = append(dst, testEvent(outputID, eventOutputScale, int32Values(scale))...)
	dst = append(dst, testEvent(outputID, eventOutputName, stringValue(name))...)
	return append(dst, testEvent(outputID, eventOutputDone, nil)...)
}

func appendXDGOutputEvents(
	dst []byte,
	xdgID uint32,
	x, y, width, height int32,
	name string,
) []byte {
	dst = append(dst, testEvent(xdgID, eventXDGLogicalPosition, int32Values(x, y))...)
	dst = append(dst, testEvent(xdgID, eventXDGLogicalSize, int32Values(width, height))...)
	dst = append(dst, testEvent(xdgID, eventXDGName, stringValue(name))...)
	return append(dst, testEvent(xdgID, eventXDGDone, nil)...)
}

func int32Values(values ...int32) []byte {
	payload := make([]byte, 0, len(values)*4)
	for _, value := range values {
		payload = append(payload, uint32Value(uint32(value))...)
	}
	return payload
}

func testEvent(sender uint32, opcode uint16, payload []byte) []byte {
	size := 8 + len(payload)
	message := make([]byte, size)
	binary.NativeEndian.PutUint32(message[0:4], sender)
	binary.NativeEndian.PutUint32(message[4:8], uint32(size)<<16|uint32(opcode))
	copy(message[8:], payload)
	return message
}

func writeFragmented(conn net.Conn, payload []byte) error {
	for len(payload) > 0 {
		size := min(3, len(payload))
		written, err := conn.Write(payload[:size])
		if err != nil {
			return err
		}
		payload = payload[written:]
	}
	return nil
}
