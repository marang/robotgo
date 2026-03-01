//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

package key

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWaylandUnicodeTypingIntegration(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-keyboard-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	// "A" usually emits shift+key for press/release (4 key events) and
	// "あ" should emit at least key press/release (2 key events).
	startMockKeyboardServer(sock, 6, 3000, done)

	time.Sleep(120 * time.Millisecond)

	if rc := sendUTFForTest("A"); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("input_utf('A') failed with rc=%d", rc)
	}
	time.Sleep(30 * time.Millisecond)
	asciiKeys := mockKeyboardKeyEvents()
	asciiMods := mockKeyboardModEvents()
	if asciiKeys < 4 {
		stopMockKeyboardServer()
		t.Fatalf("expected at least 4 key events for ASCII uppercase path, got %d", asciiKeys)
	}
	if asciiMods == 0 {
		stopMockKeyboardServer()
		t.Fatalf("expected modifier events for uppercase path, got 0")
	}

	if rc := sendUTFForTest("U3042"); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("input_utf('U3042') failed with rc=%d", rc)
	}

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		stopMockKeyboardServer()
		t.Fatalf("mock wayland keyboard server timeout (%s)", filepath.Join(dir, sock))
	}

	totalKeys := mockKeyboardKeyEvents()
	if totalKeys < asciiKeys+2 {
		t.Fatalf("expected at least 2 additional key events for UTF path, before=%d after=%d", asciiKeys, totalKeys)
	}
}
