//go:build linux

package portalrunner

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQMPPortalApprovalUsesIndependentKeyboardChords(t *testing.T) {
	t.Parallel()
	socket := filepath.Join(t.TempDir(), "qmp.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	commands := make(chan qmpCommand, 3)
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = connection.Close() }()
		if _, err := connection.Write([]byte(
			`{"QMP":{"version":{"qemu":{"major":11}},"capabilities":[]}}` + "\n",
		)); err != nil {
			serverDone <- err
			return
		}
		decoder := json.NewDecoder(bufio.NewReader(connection))
		encoder := json.NewEncoder(connection)
		for range 3 {
			var command qmpCommand
			if err := decoder.Decode(&command); err != nil {
				serverDone <- err
				return
			}
			commands <- command
			if err := encoder.Encode(map[string]any{
				"return": map[string]any{},
				"id":     command.ID,
			}); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := connectQMP(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.close() }()
	if err := client.approvePortal(
		ctx,
		portalLaneGNOME,
		"remote-desktop",
	); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}

	got := make([]qmpCommand, 0, 3)
	close(commands)
	for command := range commands {
		got = append(got, command)
	}
	if got[0].Execute != "qmp_capabilities" {
		t.Fatalf("first QMP command = %q", got[0].Execute)
	}
	assertQMPChord(t, got[1], []string{"alt", "i"})
	assertQMPChord(t, got[2], []string{"alt", "s"})
}

func TestQMPAbsoluteClickUsesRuntimeDisplayGeometry(t *testing.T) {
	t.Parallel()
	socket := filepath.Join(t.TempDir(), "qmp.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	commands := make(chan qmpCommand, 2)
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = connection.Close() }()
		if _, err := connection.Write([]byte(
			`{"QMP":{"version":{"qemu":{"major":11}},"capabilities":[]}}` + "\n",
		)); err != nil {
			serverDone <- err
			return
		}
		decoder := json.NewDecoder(bufio.NewReader(connection))
		encoder := json.NewEncoder(connection)
		for range 2 {
			var command qmpCommand
			if err := decoder.Decode(&command); err != nil {
				serverDone <- err
				return
			}
			commands <- command
			if err := encoder.Encode(map[string]any{
				"return": map[string]any{},
				"id":     command.ID,
			}); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := connectQMP(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.close() }()
	if err := client.clickAbsolute(ctx, 400, 300, 1600, 900); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	close(commands)
	got := make([]qmpCommand, 0, 2)
	for command := range commands {
		got = append(got, command)
	}
	if got[0].Execute != "qmp_capabilities" {
		t.Fatalf("first QMP command = %q", got[0].Execute)
	}
	assertQMPClick(t, got[1], 400, 300, 1600, 900)
}

func TestQMPKDEConfirmationMovesFromCardToShare(t *testing.T) {
	t.Parallel()
	socket := filepath.Join(t.TempDir(), "qmp.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	commands := make(chan qmpCommand, 5)
	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = connection.Close() }()
		if _, err := connection.Write([]byte(
			`{"QMP":{"version":{"qemu":{"major":11}},"capabilities":[]}}` + "\n",
		)); err != nil {
			serverDone <- err
			return
		}
		decoder := json.NewDecoder(bufio.NewReader(connection))
		encoder := json.NewEncoder(connection)
		for range 5 {
			var command qmpCommand
			if err := decoder.Decode(&command); err != nil {
				serverDone <- err
				return
			}
			commands <- command
			if err := encoder.Encode(map[string]any{
				"return": map[string]any{},
				"id":     command.ID,
			}); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := connectQMP(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.close() }()
	if err := client.confirmKDEPortal(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	close(commands)
	got := make([]qmpCommand, 0, 5)
	for command := range commands {
		got = append(got, command)
	}
	if got[0].Execute != "qmp_capabilities" {
		t.Fatalf("first QMP command = %q", got[0].Execute)
	}
	assertQMPChord(t, got[1], []string{qmpKeyTab})
	assertQMPChord(t, got[2], []string{qmpKeyTab})
	assertQMPChord(t, got[3], []string{qmpKeyTab})
	assertQMPChord(t, got[4], []string{qmpKeySpace})
}

func TestQMPAbsoluteCoordinateRejectsOutsideDisplay(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		pixel     int
		dimension int
	}{
		{pixel: -1, dimension: hostedDisplayWidth},
		{pixel: hostedDisplayWidth, dimension: hostedDisplayWidth},
		{pixel: 0, dimension: 1},
	} {
		if _, err := qmpAbsoluteCoordinate(
			test.pixel,
			test.dimension,
		); err == nil {
			t.Fatalf("unsafe absolute coordinate accepted: %+v", test)
		}
	}
}

func TestQMPRejectsMalformedAndFailedResponses(t *testing.T) {
	t.Parallel()
	for _, response := range []string{
		`{"return":{},"id":99}`,
		`{"error":{"class":"GenericError"},"id":2}`,
	} {
		response := response
		t.Run(response, func(t *testing.T) {
			t.Parallel()
			socket := filepath.Join(t.TempDir(), "qmp.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			go serveQMPResponses(t, listener, []string{
				`{"QMP":{"version":{"qemu":{"major":11}}}}`,
				`{"return":{},"id":1}`,
				response,
			})

			ctx, cancel := context.WithTimeout(
				context.Background(),
				2*time.Second,
			)
			defer cancel()
			client, err := connectQMP(ctx, socket)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.close() }()
			err = client.sendChord(ctx, "alt", "s")
			if err == nil {
				t.Fatal("malformed QMP response was accepted")
			}
		})
	}
}

func TestConnectQMPRequiresDeadlineAndCanonicalSocket(t *testing.T) {
	t.Parallel()
	if _, err := connectQMP(
		context.Background(),
		"relative.sock",
	); err == nil {
		t.Fatal("relative QMP socket was accepted")
	}

	socket := filepath.Join(t.TempDir(), "qmp.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go serveQMPResponses(t, listener, []string{
		`{"QMP":{"version":{"qemu":{"major":11}}}}`,
	})
	if _, err := connectQMP(
		context.Background(),
		socket,
	); err == nil || !strings.Contains(err.Error(), "requires a deadline") {
		t.Fatalf("deadline-free QMP connect error = %v", err)
	}
}

func TestHostedQEMUIsHeadlessPrivateAndControllable(t *testing.T) {
	t.Parallel()
	qmpSocket := "/private/run/qmp.sock"
	arguments := buildHostedQEMUArguments(
		Manifest{
			Lane: "gnome",
			VM: VMConfig{
				CPUs:      4,
				MemoryMiB: 8192,
			},
		},
		"/private/disk.qcow2",
		"/private/seed.img",
		"/private/qemu.pid",
		"/private/serial.log",
		22222,
		qmpSocket,
	)
	joined := strings.Join(arguments, " ")
	for _, required := range []string{
		"-device bochs-display,xres=1280,yres=720",
		"-display none",
		"-device qemu-xhci",
		"-device usb-kbd",
		"-device usb-tablet",
		"-qmp unix:" + qmpSocket + ",server=on,wait=off",
		"hostfwd=tcp:127.0.0.1:22222-:22",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("hosted QEMU arguments omit %q", required)
		}
	}
	for _, forbidden := range []string{
		"-daemonize",
		"-display gtk",
		"-virtfs",
		"hostfwd=tcp:0.0.0.0",
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("hosted QEMU arguments contain %q", forbidden)
		}
	}
	if count := strings.Count(joined, "bochs-display"); count != 1 {
		t.Fatalf("hosted QEMU display devices = %d, want exactly one", count)
	}
}

func assertQMPClick(
	t *testing.T,
	command qmpCommand,
	x,
	y,
	width,
	height int,
) {
	t.Helper()
	if command.Execute != "input-send-event" {
		t.Fatalf("QMP click command = %q", command.Execute)
	}
	arguments, ok := command.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("QMP click arguments type = %T", command.Arguments)
	}
	rawEvents, ok := arguments["events"].([]any)
	if !ok || len(rawEvents) != 4 {
		t.Fatalf("QMP click events = %#v", arguments["events"])
	}
	wantX, err := qmpAbsoluteCoordinate(x, width)
	if err != nil {
		t.Fatal(err)
	}
	wantY, err := qmpAbsoluteCoordinate(y, height)
	if err != nil {
		t.Fatal(err)
	}
	assertQMPPointerEvent(t, rawEvents[0], "abs", "x", wantX, false)
	assertQMPPointerEvent(t, rawEvents[1], "abs", "y", wantY, false)
	assertQMPPointerEvent(t, rawEvents[2], "btn", "left", 0, true)
	assertQMPPointerEvent(t, rawEvents[3], "btn", "left", 0, false)
}

func assertQMPPointerEvent(
	t *testing.T,
	rawEvent any,
	eventType,
	name string,
	value int,
	down bool,
) {
	t.Helper()
	event, ok := rawEvent.(map[string]any)
	if !ok || event["type"] != eventType {
		t.Fatalf("QMP pointer event = %#v", rawEvent)
	}
	data, ok := event["data"].(map[string]any)
	if !ok {
		t.Fatalf("QMP pointer data = %#v", event["data"])
	}
	if eventType == "abs" {
		if data["axis"] != name || int(data["value"].(float64)) != value {
			t.Fatalf("QMP absolute event = %#v", data)
		}
		return
	}
	if data["button"] != name || data["down"] != down {
		t.Fatalf("QMP button event = %#v", data)
	}
}

func assertQMPChord(t *testing.T, command qmpCommand, want []string) {
	t.Helper()
	if command.Execute != "input-send-event" {
		t.Fatalf("QMP chord command = %q", command.Execute)
	}
	arguments, ok := command.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("QMP chord arguments type = %T", command.Arguments)
	}
	rawEvents, ok := arguments["events"].([]any)
	if !ok {
		t.Fatalf("QMP events type = %T", arguments["events"])
	}
	got := make([]string, 0, len(rawEvents))
	for _, rawEvent := range rawEvents {
		event, ok := rawEvent.(map[string]any)
		if !ok {
			t.Fatalf("QMP event type = %T", rawEvent)
		}
		data := event["data"].(map[string]any)
		key := data["key"].(map[string]any)
		direction := "up"
		if data["down"].(bool) {
			direction = "down"
		}
		got = append(got, direction+":"+key["data"].(string))
	}
	wantEvents := make([]string, 0, len(want)*2)
	for _, key := range want {
		wantEvents = append(wantEvents, "down:"+key)
	}
	for index := len(want) - 1; index >= 0; index-- {
		wantEvents = append(wantEvents, "up:"+want[index])
	}
	if !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("QMP chord events = %v, want %v", got, wantEvents)
	}
}

func serveQMPResponses(
	t *testing.T,
	listener net.Listener,
	responses []string,
) {
	t.Helper()
	connection, err := listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	if len(responses) == 0 {
		return
	}
	if _, err := connection.Write([]byte(responses[0] + "\n")); err != nil {
		return
	}
	decoder := json.NewDecoder(connection)
	for _, response := range responses[1:] {
		var command qmpCommand
		if err := decoder.Decode(&command); err != nil {
			return
		}
		if _, err := connection.Write([]byte(response + "\n")); err != nil {
			return
		}
	}
}
