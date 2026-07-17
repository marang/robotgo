//go:build linux

package x11input

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/jezek/xgb/xproto"
)

const (
	guardianProtocolVersion   uint16 = 3
	guardianMaximumFrame             = 1 << 20
	guardianMaximumInputSteps        = 4096
)

const (
	guardianFrameRequest  = "request"
	guardianFrameResponse = "response"
	guardianFrameEvent    = "event"
)

const (
	guardianOperationHello                 = "hello"
	guardianOperationSetup                 = "setup"
	guardianOperationInitXTest             = "init-xtest"
	guardianOperationXTestVersion          = "xtest-version"
	guardianOperationGrabServer            = "grab-server"
	guardianOperationUngrabServer          = "ungrab-server"
	guardianOperationKeyboardMapping       = "keyboard-mapping"
	guardianOperationModifierMapping       = "modifier-mapping"
	guardianOperationChangeKeyboardMapping = "change-keyboard-mapping"
	guardianOperationPressedKeys           = "pressed-keys"
	guardianOperationQueryPointer          = "query-pointer"
	guardianOperationFakeInput             = "fake-input"
	guardianOperationFakeInputSequence     = "fake-input-sequence"
	guardianOperationClose                 = "close"
)

type guardianEnvelope struct {
	Version   uint16          `json:"version"`
	Kind      string          `json:"kind"`
	ID        uint64          `json:"id,omitempty"`
	Operation string          `json:"operation,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// guardianOutboundEnvelope accepts typed payloads so a frame and its payload
// are marshaled once. The receiving envelope retains RawMessage for strict
// operation-specific decoding.
type guardianOutboundEnvelope struct {
	Version   uint16 `json:"version"`
	Kind      string `json:"kind"`
	ID        uint64 `json:"id,omitempty"`
	Operation string `json:"operation,omitempty"`
	Payload   any    `json:"payload,omitempty"`
	Error     string `json:"error,omitempty"`
}

type guardianHelloRequest struct {
	Token              string `json:"token"`
	Display            string `json:"display"`
	RequestTimeoutNano int64  `json:"request_timeout_nano"`
	CleanupTimeoutNano int64  `json:"cleanup_timeout_nano"`
	CrashSettleNano    int64  `json:"crash_settle_nano"`
}

type guardianSetupResponse struct {
	Setup Setup `json:"setup"`
}

type guardianXTestVersionRequest struct {
	Major byte   `json:"major"`
	Minor uint16 `json:"minor"`
}

type guardianXTestVersionResponse struct {
	Version XTestVersion `json:"version"`
}

type guardianKeyboardMappingRequest struct {
	First xproto.Keycode `json:"first"`
	Count byte           `json:"count"`
}

type guardianKeyboardMappingResponse struct {
	Mapping KeyboardMapping `json:"mapping"`
}

type guardianModifierMappingResponse struct {
	Keycodes []xproto.Keycode `json:"keycodes"`
}

type guardianChangeKeyboardMappingRequest struct {
	First      xproto.Keycode  `json:"first"`
	PerKeycode byte            `json:"per_keycode"`
	Keysyms    []xproto.Keysym `json:"keysyms"`
}

type guardianPressedKeysResponse struct {
	Keys []byte `json:"keys"`
}

type guardianQueryPointerRequest struct {
	Root xproto.Window `json:"root"`
}

type guardianQueryPointerResponse struct {
	State PointerState `json:"state"`
}

type guardianFakeInputRequest struct {
	EventType byte          `json:"event_type"`
	Detail    byte          `json:"detail"`
	Root      xproto.Window `json:"root"`
	X         int16         `json:"x"`
	Y         int16         `json:"y"`
}

type guardianFakeInputStep struct {
	guardianFakeInputRequest
	DelayAfterNano int64 `json:"delay_after_nano,omitempty"`
}

type guardianFakeInputSequenceRequest struct {
	Steps []guardianFakeInputStep `json:"steps"`
}

type guardianEvent struct {
	Open  bool   `json:"open"`
	Error string `json:"error,omitempty"`
}

type guardianFramedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

type guardianFramedReader struct {
	reader io.Reader
	buffer []byte
}

func (writer *guardianFramedWriter) write(envelope guardianEnvelope) error {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode guardian frame: %w", err)
	}
	return writer.writeEncoded(encoded)
}

func (writer *guardianFramedWriter) writePayload(envelope guardianEnvelope, payload any) error {
	encoded, err := json.Marshal(guardianOutboundEnvelope{
		Version:   envelope.Version,
		Kind:      envelope.Kind,
		ID:        envelope.ID,
		Operation: envelope.Operation,
		Payload:   payload,
		Error:     envelope.Error,
	})
	if err != nil {
		return fmt.Errorf("encode guardian frame: %w", err)
	}
	return writer.writeEncoded(encoded)
}

func (writer *guardianFramedWriter) writeEncoded(encoded []byte) error {
	if len(encoded) == 0 || len(encoded) > guardianMaximumFrame {
		return fmt.Errorf("guardian frame size %d exceeds limit %d", len(encoded), guardianMaximumFrame)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(encoded)))
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err := writeAll(writer.writer, header[:]); err != nil {
		return fmt.Errorf("write guardian frame header: %w", err)
	}
	if err := writeAll(writer.writer, encoded); err != nil {
		return fmt.Errorf("write guardian frame body: %w", err)
	}
	return nil
}

func readGuardianEnvelope(reader io.Reader) (guardianEnvelope, error) {
	framedReader := guardianFramedReader{reader: reader}
	return framedReader.read()
}

func (reader *guardianFramedReader) read() (guardianEnvelope, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader.reader, header[:]); err != nil {
		return guardianEnvelope{}, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > guardianMaximumFrame {
		return guardianEnvelope{}, fmt.Errorf("invalid guardian frame size %d", size)
	}
	if cap(reader.buffer) < int(size) {
		reader.buffer = make([]byte, int(size))
	}
	encoded := reader.buffer[:int(size)]
	if _, err := io.ReadFull(reader.reader, encoded); err != nil {
		return guardianEnvelope{}, fmt.Errorf("read guardian frame body: %w", err)
	}
	var envelope guardianEnvelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return guardianEnvelope{}, fmt.Errorf("decode guardian frame: %w", err)
	}
	if envelope.Version != guardianProtocolVersion {
		return guardianEnvelope{}, fmt.Errorf("guardian protocol version %d is not supported (want %d)", envelope.Version, guardianProtocolVersion)
	}
	return envelope, nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func guardianPayload(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode guardian payload: %w", err)
	}
	return payload, nil
}

func decodeGuardianPayload(payload json.RawMessage, target any) error {
	if target == nil {
		if len(payload) != 0 && string(payload) != "null" && string(payload) != "{}" {
			return errors.New("guardian response unexpectedly contains a payload")
		}
		return nil
	}
	if len(payload) == 0 {
		return errors.New("guardian response payload is missing")
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode guardian payload: %w", err)
	}
	return nil
}
