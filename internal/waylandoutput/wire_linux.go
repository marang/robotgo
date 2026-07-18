//go:build linux

package waylandoutput

import (
	"encoding/binary"
	"fmt"
	"io"
)

const maxMessageSize = 1 << 20

func (c *wireClient) writeRequest(sender uint32, opcode uint16, payload []byte) error {
	size := 8 + len(payload)
	if size > maxMessageSize || size%4 != 0 {
		return fmt.Errorf("invalid request size %d", size)
	}
	message := make([]byte, size)
	binary.NativeEndian.PutUint32(message[0:4], sender)
	binary.NativeEndian.PutUint32(message[4:8], uint32(size)<<16|uint32(opcode))
	copy(message[8:], payload)
	for len(message) > 0 {
		written, err := c.conn.Write(message)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrUnexpectedEOF
		}
		message = message[written:]
	}
	return nil
}

func (c *wireClient) readMessage() (uint32, uint16, []byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(c.conn, header[:]); err != nil {
		return 0, 0, nil, err
	}
	sender := binary.NativeEndian.Uint32(header[0:4])
	word := binary.NativeEndian.Uint32(header[4:8])
	size := int(word >> 16)
	if size < len(header) || size > maxMessageSize || size%4 != 0 {
		return 0, 0, nil, fmt.Errorf("%w: invalid message size %d", ErrProtocol, size)
	}
	payload := make([]byte, size-len(header))
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return 0, 0, nil, err
	}
	return sender, uint16(word), payload, nil
}

func uint32Value(value uint32) []byte {
	payload := make([]byte, 4)
	binary.NativeEndian.PutUint32(payload, value)
	return payload
}

func stringValue(value string) []byte {
	length := len(value) + 1
	padded := (length + 3) &^ 3
	payload := make([]byte, 4+padded)
	binary.NativeEndian.PutUint32(payload[:4], uint32(length))
	copy(payload[4:], value)
	return payload
}

type payloadReader struct {
	data []byte
	pos  int
}

func newPayloadReader(data []byte) *payloadReader {
	return &payloadReader{data: data}
}

func (reader *payloadReader) uint32() (uint32, bool) {
	if len(reader.data)-reader.pos < 4 {
		return 0, false
	}
	value := binary.NativeEndian.Uint32(reader.data[reader.pos : reader.pos+4])
	reader.pos += 4
	return value, true
}

func (reader *payloadReader) int32() (int32, bool) {
	value, ok := reader.uint32()
	return int32(value), ok
}

func (reader *payloadReader) string() (string, bool) {
	length, ok := reader.uint32()
	if !ok || length == 0 || length > uint32(len(reader.data)-reader.pos) {
		return "", false
	}
	padded := (int(length) + 3) &^ 3
	if padded < int(length) || padded > len(reader.data)-reader.pos {
		return "", false
	}
	raw := reader.data[reader.pos : reader.pos+int(length)]
	reader.pos += padded
	if raw[len(raw)-1] != 0 {
		return "", false
	}
	return string(raw[:len(raw)-1]), true
}

func (reader *payloadReader) done() bool {
	return reader.pos == len(reader.data)
}

func (reader *payloadReader) err(field string) error {
	return fmt.Errorf("%w: malformed %s", ErrProtocol, field)
}
