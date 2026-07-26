package portalrunner

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactingWriterRemovesSplitSecrets(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	secret := []byte("registration-token-value")
	writer, err := NewRedactingWriter(&output, secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"before registration-",
		"token-",
		"value after",
	} {
		if _, err := writer.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "before [REDACTED] after" {
		t.Fatalf("redacted output = %q", got)
	}
	if strings.Contains(output.String(), string(secret)) {
		t.Fatal("redacted output contains registration token")
	}
}

func TestRedactingWriterRejectsUnsafeUse(t *testing.T) {
	t.Parallel()
	if _, err := NewRedactingWriter(nil, []byte("secret")); err == nil {
		t.Fatal("nil destination accepted")
	}
	if _, err := NewRedactingWriter(&bytes.Buffer{}); err == nil {
		t.Fatal("empty secret set accepted")
	}
	if _, err := NewRedactingWriter(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("empty secret accepted")
	}
}
