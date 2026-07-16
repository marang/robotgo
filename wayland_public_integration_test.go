//go:build cgo && linux && wayland && waylandint

package robotgo

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	inputportal "github.com/marang/robotgo/input/portal"
)

func TestWaylandPublicTypeStrEUsesExactRunes(t *testing.T) {
	dir := t.TempDir()
	sock := "rg-public"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	startPublicWaylandMockServer(sock, 3000, done)
	t.Cleanup(func() {
		stopPublicWaylandMockServer()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Errorf("public Wayland mock server did not stop during cleanup")
		}
	})
	CloseWaylandInput()
	t.Cleanup(CloseWaylandInput)
	waitForPublicWaylandMockReady(t, done)

	for _, text := range []string{"😀", "A😀"} {
		beforeKeys := publicWaylandMockKeyEvents()
		beforeMods := publicWaylandMockModEvents()
		if err := TypeStrE(text, 0, 0, 0); !errors.Is(err, ErrNotSupported) {
			stopPublicWaylandMockServer()
			t.Fatalf("TypeStrE(%q) error = %v, want ErrNotSupported", text, err)
		}
		if rc := syncWaylandKeyboardForTest(); rc < 0 {
			stopPublicWaylandMockServer()
			t.Fatalf("Wayland synchronization after rejecting %q failed with rc=%d", text, rc)
		}
		if got := publicWaylandMockKeyEvents(); got != beforeKeys {
			stopPublicWaylandMockServer()
			t.Fatalf("rejected TypeStrE(%q) emitted key events: before=%d after=%d", text, beforeKeys, got)
		}
		if got := publicWaylandMockModEvents(); got != beforeMods {
			stopPublicWaylandMockServer()
			t.Fatalf("rejected TypeStrE(%q) emitted modifier events: before=%d after=%d", text, beforeMods, got)
		}
	}

	beforeNewline := publicWaylandMockKeyEvents()
	if err := TypeStrE("\n", 0, 0, 0); err != nil {
		stopPublicWaylandMockServer()
		t.Fatalf("TypeStrE(newline): %v", err)
	}
	waitForPublicWaylandMockKeyEvents(t, beforeNewline+2)
	if got := publicWaylandMockKeyEvents(); got != beforeNewline+2 {
		stopPublicWaylandMockServer()
		t.Fatalf("TypeStrE(newline) emitted %d events, want one press/release pair", got-beforeNewline)
	}

	beforeASCII := publicWaylandMockKeyEvents()
	if err := TypeStrE("A", 0, 0, 0); err != nil {
		stopPublicWaylandMockServer()
		t.Fatalf("TypeStrE(uppercase ASCII): %v", err)
	}
	waitForPublicWaylandMockKeyEvents(t, beforeASCII+2)
	if got := publicWaylandMockKeyEvents(); got != beforeASCII+2 {
		stopPublicWaylandMockServer()
		t.Fatalf("TypeStrE(uppercase ASCII) emitted %d events, want one press/release pair", got-beforeASCII)
	}
	if got := publicWaylandMockLastMods(); got != 0 {
		stopPublicWaylandMockServer()
		t.Fatalf("public TypeStrE left Wayland modifiers active: mask=%#x", got)
	}
}

func TestWaylandPublicTextPreflightFallsBackToRemoteDesktop(t *testing.T) {
	dir := t.TempDir()
	sock := "rg-fallback"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	startPublicWaylandMockServer(sock, 3000, done)
	t.Cleanup(func() {
		stopPublicWaylandMockServer()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Errorf("fallback Wayland mock server did not stop during cleanup")
		}
	})
	CloseWaylandInput()
	session := installFakeHighLevelPortalSession(t, inputportal.DeviceKeyboard)
	// Register after the fake-session restoration cleanup so Close runs first.
	t.Cleanup(CloseWaylandInput)
	waitForPublicWaylandMockReady(t, done)

	const text = "A😀"
	if err := TypeStrE(text, 0, 0, 0); err != nil {
		stopPublicWaylandMockServer()
		t.Fatalf("TypeStrE native-preflight portal fallback: %v", err)
	}
	if got := publicWaylandMockKeyEvents(); got != 0 {
		stopPublicWaylandMockServer()
		t.Fatalf("failed native text preflight emitted %d key events before portal fallback", got)
	}
	if got := publicWaylandMockModEvents(); got != 0 {
		stopPublicWaylandMockServer()
		t.Fatalf("failed native text preflight emitted %d modifier events before portal fallback", got)
	}
	wantEvents := portalRuneEvents(t, []rune(text))
	if got, _ := session.snapshot(); !reflect.DeepEqual(got, wantEvents) {
		stopPublicWaylandMockServer()
		t.Fatalf("portal text fallback events = %v, want %v", got, wantEvents)
	}

	if err := UnicodeTypeE('😀'); err != nil {
		stopPublicWaylandMockServer()
		t.Fatalf("UnicodeTypeE native-keysym portal fallback: %v", err)
	}
	wantEvents = append(wantEvents, portalRuneEvents(t, []rune{'😀'})...)
	if got, _ := session.snapshot(); !reflect.DeepEqual(got, wantEvents) {
		stopPublicWaylandMockServer()
		t.Fatalf("portal keysym fallback events = %v, want %v", got, wantEvents)
	}
	if got := publicWaylandMockKeyEvents(); got != 0 {
		stopPublicWaylandMockServer()
		t.Fatalf("failed native keysym preflight emitted %d key events", got)
	}
}

func portalRuneEvents(t *testing.T, values []rune) []string {
	t.Helper()
	events := make([]string, 0, len(values)*2)
	for _, value := range values {
		keysym, err := portalKeysymForRune(value)
		if err != nil {
			t.Fatalf("portal keysym for %U: %v", value, err)
		}
		events = append(events,
			fmt.Sprintf("keysym:%d:true", keysym),
			fmt.Sprintf("keysym:%d:false", keysym),
		)
	}
	return events
}

func waitForPublicWaylandMockReady(t *testing.T, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if publicWaylandMockReady() {
			return
		}
		select {
		case <-done:
			t.Fatal("public Wayland mock stopped before becoming ready")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("public Wayland mock did not become ready")
}

func waitForPublicWaylandMockKeyEvents(t *testing.T, want uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if publicWaylandMockKeyEvents() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("public Wayland mock key events=%d, want at least %d", publicWaylandMockKeyEvents(), want)
}
