//go:build linux

package portalrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"time"
)

const (
	qmpConnectInterval = 100 * time.Millisecond
	qmpMaximumResponse = 1024 * 1024
	qmpChordInterval   = 250 * time.Millisecond

	qmpKeyAlt    = "alt"
	qmpKeyI      = "i"
	qmpKeyReturn = "ret"
	qmpKeyS      = "s"

	qmpEventAbsolute = "abs"
	qmpEventButton   = "btn"
	qmpPointerAxisX  = "x"
	qmpPointerAxisY  = "y"
	qmpPointerLeft   = "left"

	qmpAbsoluteMaximum  = 0x7fff
	hostedDisplayWidth  = 1280
	hostedDisplayHeight = 720
)

type qmpClient struct {
	connection net.Conn
	decoder    *json.Decoder
	encoder    *json.Encoder
	nextID     uint64
}

type qmpMessage struct {
	Greeting json.RawMessage `json:"QMP"`
	Return   json.RawMessage `json:"return"`
	Error    *qmpError       `json:"error"`
	Event    string          `json:"event"`
	ID       uint64          `json:"id"`
}

type qmpError struct {
	Class string `json:"class"`
}

type qmpCommand struct {
	Execute   string `json:"execute"`
	Arguments any    `json:"arguments,omitempty"`
	ID        uint64 `json:"id"`
}

type qmpInputEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type qmpKeyEventData struct {
	Down bool        `json:"down"`
	Key  qmpKeyValue `json:"key"`
}

type qmpKeyValue struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type qmpAbsoluteEventData struct {
	Axis  string `json:"axis"`
	Value int    `json:"value"`
}

type qmpButtonEventData struct {
	Down   bool   `json:"down"`
	Button string `json:"button"`
}

func connectQMP(ctx context.Context, socketPath string) (*qmpClient, error) {
	if !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath {
		return nil, errors.New("QMP socket path must be clean and absolute")
	}
	dialer := net.Dialer{}
	for {
		connection, err := dialer.DialContext(ctx, "unix", socketPath)
		if err == nil {
			client := &qmpClient{
				connection: connection,
				decoder: json.NewDecoder(
					io.LimitReader(connection, qmpMaximumResponse),
				),
				encoder: json.NewEncoder(connection),
			}
			if err := client.negotiate(ctx); err != nil {
				_ = connection.Close()
				return nil, err
			}
			return client, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connect to QMP socket: %w", ctx.Err())
		case <-time.After(qmpConnectInterval):
		}
	}
}

func (client *qmpClient) negotiate(ctx context.Context) error {
	if client == nil ||
		client.connection == nil ||
		client.decoder == nil ||
		client.encoder == nil {
		return errors.New("QMP client is not initialized")
	}
	if err := client.applyDeadline(ctx); err != nil {
		return err
	}
	var greeting qmpMessage
	if err := client.decoder.Decode(&greeting); err != nil {
		return errors.New("read QMP greeting")
	}
	if len(greeting.Greeting) == 0 {
		return errors.New("QMP greeting is invalid")
	}
	return client.execute(ctx, "qmp_capabilities", nil)
}

func (client *qmpClient) sendChord(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return errors.New("QMP key chord is empty")
	}
	events := make([]qmpInputEvent, 0, len(keys)*2)
	for _, key := range keys {
		if key == "" {
			return errors.New("QMP key chord contains an empty key")
		}
		events = append(events, qmpKeyEvent(key, true))
	}
	for index := len(keys) - 1; index >= 0; index-- {
		events = append(events, qmpKeyEvent(keys[index], false))
	}
	return client.execute(ctx, "input-send-event", map[string]any{
		"events": events,
	})
}

func (client *qmpClient) clickAbsolute(
	ctx context.Context,
	x,
	y,
	width,
	height int,
) error {
	absoluteX, err := qmpAbsoluteCoordinate(x, width)
	if err != nil {
		return err
	}
	absoluteY, err := qmpAbsoluteCoordinate(y, height)
	if err != nil {
		return err
	}
	return client.execute(ctx, "input-send-event", map[string]any{
		"events": []qmpInputEvent{
			{
				Type: qmpEventAbsolute,
				Data: qmpAbsoluteEventData{
					Axis: qmpPointerAxisX, Value: absoluteX,
				},
			},
			{
				Type: qmpEventAbsolute,
				Data: qmpAbsoluteEventData{
					Axis: qmpPointerAxisY, Value: absoluteY,
				},
			},
			{
				Type: qmpEventButton,
				Data: qmpButtonEventData{
					Down: true, Button: qmpPointerLeft,
				},
			},
			{
				Type: qmpEventButton,
				Data: qmpButtonEventData{
					Down: false, Button: qmpPointerLeft,
				},
			},
		},
	})
}

func qmpAbsoluteCoordinate(pixel, dimension int) (int, error) {
	if dimension <= 1 || pixel < 0 || pixel >= dimension {
		return 0, errors.New("QMP absolute pointer coordinate is invalid")
	}
	return pixel * qmpAbsoluteMaximum / (dimension - 1), nil
}

func (client *qmpClient) approvePortal(
	ctx context.Context,
	lane,
	cell string,
) error {
	if cell != "remote-desktop" && cell != "screencast" {
		return errors.New("QMP portal approval cell is invalid")
	}
	switch lane {
	case portalLaneGNOME:
		if cell == "remote-desktop" {
			if err := client.sendChord(ctx, qmpKeyAlt, qmpKeyI); err != nil {
				return err
			}
			if err := waitQMPChord(ctx); err != nil {
				return err
			}
		}
		return client.sendChord(ctx, qmpKeyAlt, qmpKeyS)
	case portalLaneKDE:
		return errors.New(
			"KDE portal approval uses protected KWin dialog geometry",
		)
	default:
		return errors.New("QMP portal approval lane is invalid")
	}
}

func waitQMPChord(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(qmpChordInterval):
		return nil
	}
}

func (client *qmpClient) execute(
	ctx context.Context,
	command string,
	arguments any,
) error {
	if command == "" {
		return errors.New("QMP command is empty")
	}
	if err := client.applyDeadline(ctx); err != nil {
		return err
	}
	client.nextID++
	id := client.nextID
	if err := client.encoder.Encode(qmpCommand{
		Execute:   command,
		Arguments: arguments,
		ID:        id,
	}); err != nil {
		return fmt.Errorf("send QMP command %q", command)
	}
	for {
		var response qmpMessage
		if err := client.decoder.Decode(&response); err != nil {
			return fmt.Errorf("read QMP command %q response", command)
		}
		if response.Event != "" {
			continue
		}
		if response.ID != id {
			return fmt.Errorf("QMP command %q response ID mismatch", command)
		}
		if response.Error != nil {
			return fmt.Errorf(
				"QMP command %q failed with class %q",
				command,
				response.Error.Class,
			)
		}
		if response.Return == nil {
			return fmt.Errorf("QMP command %q response is invalid", command)
		}
		return nil
	}
}

func (client *qmpClient) applyDeadline(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("QMP operation requires a deadline")
	}
	if err := client.connection.SetDeadline(deadline); err != nil {
		return errors.New("set QMP operation deadline")
	}
	return nil
}

func (client *qmpClient) close() error {
	if client == nil || client.connection == nil {
		return nil
	}
	return client.connection.Close()
}

func qmpKeyEvent(key string, down bool) qmpInputEvent {
	return qmpInputEvent{
		Type: "key",
		Data: qmpKeyEventData{
			Down: down,
			Key: qmpKeyValue{
				Type: "qcode",
				Data: key,
			},
		},
	}
}

func buildHostedQEMUArguments(
	manifest Manifest,
	disk,
	seed,
	pidFile,
	serialLog string,
	sshPort int,
	qmpSocket string,
) []string {
	base := buildQEMUArguments(
		manifest,
		disk,
		seed,
		pidFile,
		serialLog,
		sshPort,
		true,
	)
	arguments := make([]string, 0, len(base)+10)
	for index := 0; index < len(base); index++ {
		if base[index] == "-daemonize" {
			continue
		}
		if base[index] == "-device" &&
			index+1 < len(base) &&
			base[index+1] == "bochs-display" {
			index++
			continue
		}
		arguments = append(arguments, base[index])
	}
	return append(
		arguments,
		"-device", "qemu-xhci",
		"-device", "usb-kbd",
		"-device", "usb-tablet",
		"-device", fmt.Sprintf(
			"bochs-display,xres=%d,yres=%d",
			hostedDisplayWidth,
			hostedDisplayHeight,
		),
		"-qmp", "unix:"+qmpSocket+",server=on,wait=off",
	)
}
