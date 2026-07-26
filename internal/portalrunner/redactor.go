package portalrunner

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

var redactedValue = []byte("[REDACTED]")

// RedactingWriter removes configured non-empty secrets from a stream,
// including values split across adjacent Write calls.
type RedactingWriter struct {
	mu          sync.Mutex
	destination io.Writer
	secrets     [][]byte
	pending     []byte
	flushed     bool
}

// NewRedactingWriter creates a streaming redactor. Secret bytes are copied so
// callers may clear their source buffers immediately.
func NewRedactingWriter(destination io.Writer, secrets ...[]byte) (*RedactingWriter, error) {
	if destination == nil {
		return nil, errors.New("redaction destination is nil")
	}
	writer := &RedactingWriter{destination: destination}
	for _, secret := range secrets {
		if len(secret) == 0 {
			return nil, errors.New("redaction secret is empty")
		}
		copyOfSecret := bytes.Clone(secret)
		for _, existing := range writer.secrets {
			if bytes.HasPrefix(existing, copyOfSecret) ||
				bytes.HasPrefix(copyOfSecret, existing) {
				return nil, errors.New("redaction secrets must not overlap by prefix")
			}
		}
		writer.secrets = append(writer.secrets, copyOfSecret)
	}
	if len(writer.secrets) == 0 {
		return nil, errors.New("redaction secret set is empty")
	}
	return writer, nil
}

// Write buffers only the suffix needed to recognize a secret split across
// calls. It reports the caller's full input length on successful emission.
func (writer *RedactingWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.flushed {
		return 0, errors.New("redacting writer is already flushed")
	}
	emission := make([]byte, 0, len(data))
	for _, value := range data {
		writer.pending = append(writer.pending, value)
		for len(writer.pending) > 0 {
			fullMatch := false
			prefixMatch := false
			for _, secret := range writer.secrets {
				if bytes.Equal(writer.pending, secret) {
					fullMatch = true
					break
				}
				if bytes.HasPrefix(secret, writer.pending) {
					prefixMatch = true
				}
			}
			if fullMatch {
				emission = append(emission, redactedValue...)
				clear(writer.pending)
				writer.pending = writer.pending[:0]
				break
			}
			if prefixMatch {
				break
			}
			emission = append(emission, writer.pending[0])
			writer.pending = writer.pending[1:]
		}
	}
	if _, err := writer.destination.Write(emission); err != nil {
		return 0, err
	}
	clear(emission)
	return len(data), nil
}

// Flush emits the final buffered suffix exactly once.
func (writer *RedactingWriter) Flush() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.flushed {
		return nil
	}
	writer.flushed = true
	err := writer.emit(writer.pending)
	writer.pending = nil
	for index := range writer.secrets {
		clear(writer.secrets[index])
	}
	writer.secrets = nil
	return err
}

func (writer *RedactingWriter) emit(data []byte) error {
	redacted := bytes.Clone(data)
	for _, secret := range writer.secrets {
		redacted = bytes.ReplaceAll(redacted, secret, redactedValue)
	}
	_, err := writer.destination.Write(redacted)
	clear(redacted)
	return err
}
