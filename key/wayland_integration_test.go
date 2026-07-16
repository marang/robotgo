//go:build cgo && linux && waylandint
// +build cgo,linux,waylandint

package key

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaylandVirtualKeyboardASCIIAndUnmappedUnicode(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-keyboard-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	// The default client keymap can type "A" with a modifier-state update and
	// key press/release. Arbitrary Unicode remains explicitly unsupported when
	// the symbol is absent from that keymap.
	startMockKeyboardServer(sock, 0, 3000, done)
	t.Cleanup(func() { waitForMockKeyboardServerCleanup(t, done) })
	t.Cleanup(closeWaylandKeyboardForTest)
	waitForMockKeyboardServerReady(t, done, time.Second)

	if rc := sendUTFForTest("A"); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("input_utf('A') failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland keyboard synchronization after ASCII input failed with rc=%d", rc)
	}
	if asciiKeys := mockKeyboardKeyEvents(); asciiKeys != 2 {
		stopMockKeyboardServer()
		t.Fatalf("expected exactly 2 key events for uppercase ASCII path, got %d", asciiKeys)
	}
	asciiMods := mockKeyboardModEvents()
	if asciiMods < 2 {
		stopMockKeyboardServer()
		t.Fatalf("expected modifier press and restore events for uppercase path, got %d", asciiMods)
	}
	if lastMods := mockKeyboardLastMods(); lastMods != 0 {
		stopMockKeyboardServer()
		t.Fatalf("uppercase ASCII path left modifiers active: mask=%#x", lastMods)
	}

	// The Go transaction already selected Wayland before entering the native
	// operation. A concurrent environment change must not make the C operation
	// reselect another backend between readiness and event dispatch.
	if err := os.Setenv("WAYLAND_DISPLAY", ""); err != nil {
		stopMockKeyboardServer()
		t.Fatalf("clear WAYLAND_DISPLAY during snapshot regression: %v", err)
	}
	if err := os.Setenv("DISPLAY", ":environment-flipped-after-wayland-snapshot"); err != nil {
		stopMockKeyboardServer()
		t.Fatalf("set DISPLAY during snapshot regression: %v", err)
	}
	beforeSnapshotKeys := mockKeyboardKeyEvents()
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, true, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland KeyDown after environment flip failed with rc=%d", rc)
	}
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, false, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland KeyUp after environment flip failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after environment flip failed with rc=%d", rc)
	}
	if got := mockKeyboardKeyEvents(); got != beforeSnapshotKeys+2 {
		stopMockKeyboardServer()
		t.Fatalf("environment-flip key events=%d, want %d", got, beforeSnapshotKeys+2)
	}
	if err := os.Setenv("WAYLAND_DISPLAY", sock); err != nil {
		stopMockKeyboardServer()
		t.Fatalf("restore WAYLAND_DISPLAY after snapshot regression: %v", err)
	}
	if err := os.Setenv("DISPLAY", ""); err != nil {
		stopMockKeyboardServer()
		t.Fatalf("restore DISPLAY after snapshot regression: %v", err)
	}

	beforeUnmapped := mockKeyboardKeyEvents()
	const keyUnmapped = 2
	if rc := sendUTFForTest("U3042"); rc != keyUnmapped {
		stopMockKeyboardServer()
		t.Fatalf("input_utf('U3042') rc=%d, want explicit unmapped status %d", rc, keyUnmapped)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland keyboard synchronization failed with rc=%d", rc)
	}
	if afterUnmapped := mockKeyboardKeyEvents(); afterUnmapped != beforeUnmapped {
		stopMockKeyboardServer()
		t.Fatalf("unmapped Unicode emitted key events, before=%d after=%d", beforeUnmapped, afterUnmapped)
	}

	stopMockKeyboardServer()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		stopMockKeyboardServer()
		t.Fatalf("mock wayland keyboard server timeout (%s)", filepath.Join(dir, sock))
	}
}

func TestWaylandExactTextPreflightRejectsUnmappedWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	sock := "rg-exact"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	startMockKeyboardServer(sock, 0, 3000, done)
	t.Cleanup(func() { waitForMockKeyboardServerCleanup(t, done) })
	t.Cleanup(closeWaylandKeyboardForTest)
	waitForMockKeyboardServerReady(t, done, time.Second)

	const keyUnmapped = 2
	for _, text := range []string{"😀", "A😀"} {
		beforeKeys := mockKeyboardKeyEvents()
		beforeMods := mockKeyboardModEvents()
		if rc := sendExactTextForTest(text); rc != keyUnmapped {
			stopMockKeyboardServer()
			t.Fatalf("exact Wayland text %q rc=%d, want explicit unmapped status %d", text, rc, keyUnmapped)
		}
		if rc := syncWaylandKeyboardForTest(); rc < 0 {
			stopMockKeyboardServer()
			t.Fatalf("Wayland synchronization after rejecting %q failed with rc=%d", text, rc)
		}
		if got := mockKeyboardKeyEvents(); got != beforeKeys {
			stopMockKeyboardServer()
			t.Fatalf("rejected exact Wayland text %q emitted key events: before=%d after=%d", text, beforeKeys, got)
		}
		if got := mockKeyboardModEvents(); got != beforeMods {
			stopMockKeyboardServer()
			t.Fatalf("rejected exact Wayland text %q emitted modifier events: before=%d after=%d", text, beforeMods, got)
		}
		if got := mockKeyboardLastMods(); got != 0 {
			stopMockKeyboardServer()
			t.Fatalf("rejected exact Wayland text %q left modifiers active: mask=%#x", text, got)
		}
	}

	beforeNewlineKeys := mockKeyboardKeyEvents()
	if rc := sendExactTextForTest("\n"); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("exact Wayland newline failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after exact newline failed with rc=%d", rc)
	}
	if got := mockKeyboardKeyEvents(); got != beforeNewlineKeys+2 {
		stopMockKeyboardServer()
		t.Fatalf("exact newline emitted %d key events, want one press/release pair (%d total)", got-beforeNewlineKeys, beforeNewlineKeys+2)
	}
	if got := mockKeyboardLastMods(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("exact newline left modifiers active: mask=%#x", got)
	}

	beforeKeys := mockKeyboardKeyEvents()
	if rc := sendExactTextForTest("A"); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("exact Wayland ASCII text failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after exact ASCII text failed with rc=%d", rc)
	}
	if got := mockKeyboardKeyEvents(); got != beforeKeys+2 {
		stopMockKeyboardServer()
		t.Fatalf("exact ASCII text key events=%d, want %d", got, beforeKeys+2)
	}
	if got := mockKeyboardLastMods(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("exact ASCII text left modifiers active: mask=%#x", got)
	}

	// Exact text must not tap a keycode already owned by a persistent KeyDown.
	// The conflicting rune is deliberately second to prove the complete string
	// is rejected before the first key event.
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, true, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("persistent A KeyDown failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after A KeyDown failed with rc=%d", rc)
	}
	beforeOwnedConflictKeys := mockKeyboardKeyEvents()
	beforeOwnedConflictMods := mockKeyboardModEvents()
	const keyOwnershipConflict = 7
	if rc := sendExactTextForTest("ba"); rc != keyOwnershipConflict {
		stopMockKeyboardServer()
		t.Fatalf("exact text using an owned keycode rc=%d, want ownership conflict %d", rc, keyOwnershipConflict)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after owned-key conflict failed with rc=%d", rc)
	}
	if got := mockKeyboardKeyEvents(); got != beforeOwnedConflictKeys {
		stopMockKeyboardServer()
		t.Fatalf("owned-key text conflict emitted key events: before=%d after=%d", beforeOwnedConflictKeys, got)
	}
	if got := mockKeyboardModEvents(); got != beforeOwnedConflictMods {
		stopMockKeyboardServer()
		t.Fatalf("owned-key text conflict emitted modifier events: before=%d after=%d", beforeOwnedConflictMods, got)
	}
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, false, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("persistent A KeyUp failed with rc=%d", rc)
	}

	// Persistent modifiers are part of the virtual keyboard state. Exact text
	// may reuse a modifier it needs, but must reject the entire string before
	// emitting anything if any rune would be changed by that state.
	if rc := toggleWaylandKeyForTest(waylandTestKeysymShiftL, true, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("persistent Shift KeyDown failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after Shift KeyDown failed with rc=%d", rc)
	}
	shiftMask := mockKeyboardLastMods()
	if shiftMask == 0 {
		stopMockKeyboardServer()
		t.Fatal("persistent Shift KeyDown did not activate a modifier mask")
	}
	beforeConflictKeys := mockKeyboardKeyEvents()
	beforeConflictMods := mockKeyboardModEvents()
	const keyStateConflict = 6
	if rc := sendExactTextForTest("Aa"); rc != keyStateConflict {
		stopMockKeyboardServer()
		t.Fatalf("exact text with incompatible persistent Shift rc=%d, want state conflict %d", rc, keyStateConflict)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after Shift text conflict failed with rc=%d", rc)
	}
	if got := mockKeyboardKeyEvents(); got != beforeConflictKeys {
		stopMockKeyboardServer()
		t.Fatalf("Shift text conflict emitted key events: before=%d after=%d", beforeConflictKeys, got)
	}
	if got := mockKeyboardModEvents(); got != beforeConflictMods {
		stopMockKeyboardServer()
		t.Fatalf("Shift text conflict emitted modifier events: before=%d after=%d", beforeConflictMods, got)
	}
	if got := mockKeyboardLastMods(); got != shiftMask {
		stopMockKeyboardServer()
		t.Fatalf("Shift text conflict changed persistent modifiers: got=%#x want=%#x", got, shiftMask)
	}

	beforeCompatibleKeys := mockKeyboardKeyEvents()
	beforeCompatibleMods := mockKeyboardModEvents()
	if rc := sendExactTextForTest("A"); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("exact uppercase text reusing persistent Shift failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after compatible Shift text failed with rc=%d", rc)
	}
	if got := mockKeyboardKeyEvents(); got != beforeCompatibleKeys+2 {
		stopMockKeyboardServer()
		t.Fatalf("compatible Shift text key events=%d, want %d", got, beforeCompatibleKeys+2)
	}
	if got := mockKeyboardModEvents(); got != beforeCompatibleMods {
		stopMockKeyboardServer()
		t.Fatalf("compatible Shift text changed modifier events: before=%d after=%d", beforeCompatibleMods, got)
	}
	if got := mockKeyboardLastMods(); got != shiftMask {
		stopMockKeyboardServer()
		t.Fatalf("compatible Shift text changed persistent modifiers: got=%#x want=%#x", got, shiftMask)
	}

	// A compound key press shares, rather than releases, an already held
	// modifier. This covers both KeyPress-style down/up and standalone KeyDown.
	beforeCompoundMods := mockKeyboardModEvents()
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, true, waylandTestModShift); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("compound key down sharing persistent Shift failed with rc=%d", rc)
	}
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, false, waylandTestModShift); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("compound key up sharing persistent Shift failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after compound key failed with rc=%d", rc)
	}
	if got := mockKeyboardModEvents(); got != beforeCompoundMods {
		stopMockKeyboardServer()
		t.Fatalf("compound key released shared Shift: modifier events before=%d after=%d", beforeCompoundMods, got)
	}
	if got := mockKeyboardLastMods(); got != shiftMask {
		stopMockKeyboardServer()
		t.Fatalf("compound key changed persistent Shift: got=%#x want=%#x", got, shiftMask)
	}
	if rc := toggleWaylandKeyForTest(waylandTestKeysymShiftL, false, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("persistent Shift KeyUp failed with rc=%d", rc)
	}

	if rc := toggleWaylandKeyForTest(waylandTestKeysymControl, true, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("persistent Control KeyDown failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after Control KeyDown failed with rc=%d", rc)
	}
	controlMask := mockKeyboardLastMods()
	beforeControlKeys := mockKeyboardKeyEvents()
	beforeControlMods := mockKeyboardModEvents()
	if rc := sendExactTextForTest("a"); rc != keyStateConflict {
		stopMockKeyboardServer()
		t.Fatalf("exact text with persistent Control rc=%d, want state conflict %d", rc, keyStateConflict)
	}
	if got := mockKeyboardKeyEvents(); got != beforeControlKeys {
		stopMockKeyboardServer()
		t.Fatalf("Control text conflict emitted key events: before=%d after=%d", beforeControlKeys, got)
	}
	if got := mockKeyboardModEvents(); got != beforeControlMods {
		stopMockKeyboardServer()
		t.Fatalf("Control text conflict emitted modifier events: before=%d after=%d", beforeControlMods, got)
	}
	if got := mockKeyboardLastMods(); got != controlMask {
		stopMockKeyboardServer()
		t.Fatalf("Control text conflict changed persistent modifiers: got=%#x want=%#x", got, controlMask)
	}
	if rc := toggleWaylandKeyForTest(waylandTestKeysymControl, false, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("persistent Control KeyUp failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization after modifier cleanup failed with rc=%d", rc)
	}
	if got := mockKeyboardLastMods(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("modifier ownership test left modifiers active: mask=%#x", got)
	}
}

func waitForMockKeyboardServerReady(t *testing.T, done <-chan struct{}, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		if mockKeyboardServerReady() {
			return
		}
		select {
		case <-done:
			t.Fatal("mock Wayland keyboard server stopped before becoming ready")
		case <-deadline.C:
			stopMockKeyboardServer()
			t.Fatalf("mock Wayland keyboard server was not ready within %s", timeout)
		case <-ticker.C:
		}
	}
}

func TestWaylandKeyboardFlushFailureReconnects(t *testing.T) {
	dir := t.TempDir()
	sock := "robotgo-keyboard-reconnect-wl"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	startMockKeyboardServer(sock, 0, 3000, done)
	t.Cleanup(func() { waitForMockKeyboardServerCleanup(t, done) })
	t.Cleanup(closeWaylandKeyboardForTest)
	waitForMockKeyboardServerReady(t, done, time.Second)

	if rc := waylandKeyboardReadyForTest(); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("initial Wayland keyboard readiness failed with rc=%d", rc)
	}
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, true, 0); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland KeyDown before transport failure failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland synchronization before transport failure failed with rc=%d", rc)
	}
	if rc := disconnectWaylandKeyboardTransportForTest(); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("disconnect Wayland keyboard transport failed with rc=%d", rc)
	}

	const keyInjectionFailed = 3
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, false, 0); rc != keyInjectionFailed {
		stopMockKeyboardServer()
		t.Fatalf("KeyUp after transport disconnect rc=%d, want %d", rc, keyInjectionFailed)
	}
	const waylandDisplayError = 1
	if got := waylandKeyboardLastErrorForTest(); got != waylandDisplayError {
		stopMockKeyboardServer()
		t.Fatalf("Wayland keyboard last error after flush failure = %d, want %d", got, waylandDisplayError)
	}
	if rc := waylandKeyboardReadyForTest(); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland keyboard did not reconnect after flush failure: rc=%d", rc)
	}
	if got := waylandKeyboardLastErrorForTest(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland keyboard last error after reconnect = %d, want 0", got)
	}
	const keyOwnershipConflict = 7
	if rc := toggleWaylandKeyForTest(waylandTestKeysymA, false, 0); rc != keyOwnershipConflict {
		stopMockKeyboardServer()
		t.Fatalf("retry KeyUp after reconnect rc=%d, want cleared-ownership status %d", rc, keyOwnershipConflict)
	}
	beforeReconnectKeys := mockKeyboardKeyEvents()
	if rc := sendUTFForTest("a"); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("input after Wayland keyboard reconnect failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("Wayland keyboard synchronization after reconnect failed with rc=%d", rc)
	}
	if got := mockKeyboardKeyEvents(); got != beforeReconnectKeys+2 {
		stopMockKeyboardServer()
		t.Fatalf("input after reconnect emitted %d total key events, want %d", got, beforeReconnectKeys+2)
	}
	if got := mockKeyboardLastMods(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("input after reconnect left modifiers active: mask=%#x", got)
	}
	stopMockKeyboardServer()

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		stopMockKeyboardServer()
		t.Fatalf("mock Wayland keyboard reconnect timeout (%s)", filepath.Join(dir, sock))
	}
}

func TestWaylandKeyboardSelectsDeterministicCapableSeatAndCleansAll(t *testing.T) {
	dir := t.TempDir()
	sock := "rg-multi"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	// Seats 0 and 2 are keyboard-capable. The lowest registry global name
	// (seat 0) must be selected; every bound seat remains owned until cleanup.
	startMockKeyboardServerWithSeats(sock, 0, 3000, 3, 0b101, done)
	t.Cleanup(func() { waitForMockKeyboardServerCleanup(t, done) })
	t.Cleanup(closeWaylandKeyboardForTest)
	waitForMockKeyboardServerReady(t, done, time.Second)

	if rc := waylandKeyboardReadyForTest(); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("multi-seat Wayland keyboard readiness failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("multi-seat Wayland synchronization failed with rc=%d", rc)
	}
	if got := mockKeyboardSelectedSeat(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("selected mock Wayland seat=%d, want deterministic seat 0", got)
	}

	closeWaylandKeyboardForTest()
	waitForMockKeyboardResourceCleanup(t, 3, 0)
}

func TestWaylandKeyboardRejectsSeatsWithoutKeyboardCapability(t *testing.T) {
	dir := t.TempDir()
	sock := "rg-nocap"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	startMockKeyboardServerWithSeats(sock, 0, 3000, 2, 0, done)
	t.Cleanup(func() { waitForMockKeyboardServerCleanup(t, done) })
	t.Cleanup(closeWaylandKeyboardForTest)
	waitForMockKeyboardServerReady(t, done, time.Second)

	if rc := waylandKeyboardReadyForTest(); rc == 0 {
		stopMockKeyboardServer()
		t.Fatal("Wayland readiness accepted seats without keyboard capability")
	}
	const noSeatError = 2
	if got := waylandKeyboardLastErrorForTest(); got != noSeatError {
		stopMockKeyboardServer()
		t.Fatalf("Wayland last error for non-keyboard seats=%d, want %d", got, noSeatError)
	}
	if got := mockKeyboardSelectedSeat(); got != ^uint32(0) {
		stopMockKeyboardServer()
		t.Fatalf("virtual keyboard was created for non-keyboard seat %d", got)
	}
	waitForMockKeyboardResourceCleanup(t, 2, 0)
}

func TestWaylandKeyboardProcessesRuntimeSeatChanges(t *testing.T) {
	dir := t.TempDir()
	sock := "rg-runtime"
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("WAYLAND_DISPLAY", sock)
	t.Setenv("DISPLAY", "")

	done := make(chan struct{})
	startMockKeyboardServerWithSeats(sock, 0, 5000, 3, 0b101, done)
	t.Cleanup(func() { waitForMockKeyboardServerCleanup(t, done) })
	t.Cleanup(closeWaylandKeyboardForTest)
	waitForMockKeyboardServerReady(t, done, time.Second)

	if rc := waylandKeyboardReadyForTest(); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("initial runtime-seat readiness failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("initial runtime-seat synchronization failed with rc=%d", rc)
	}
	if got := mockKeyboardSelectedSeat(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("initial runtime seat=%d, want 0", got)
	}
	assertWaylandReadinessNonblocking(t)

	// Losing keyboard capability on the selected seat must be consumed at the
	// next operation boundary. Reconnect then deterministically selects seat 2.
	generation := setMockKeyboardSeatCapabilities(0, 0)
	waitForMockKeyboardControl(t, generation)
	if rc := waylandKeyboardReadyForTest(); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("readiness after selected-seat capability loss failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("sync after selected-seat capability loss failed with rc=%d", rc)
	}
	if got := mockKeyboardSelectedSeat(); got != 2 {
		stopMockKeyboardServer()
		t.Fatalf("seat after capability loss=%d, want fallback seat 2", got)
	}
	beforeKeys := mockKeyboardKeyEvents()
	if rc := sendExactTextForTest("a"); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("input after capability-change reconnect failed with rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("sync after capability-change reconnect failed with rc=%d", rc)
	}
	if got := mockKeyboardKeyEvents(); got != beforeKeys+2 {
		stopMockKeyboardServer()
		t.Fatalf("input after capability change emitted %d events, want 2", got-beforeKeys)
	}
	if got := mockKeyboardLastMods(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("capability-change reconnect left modifiers active: %#x", got)
	}

	// Removing the remaining capable global must not leave a stale proxy. The
	// same readiness call consumes global_remove and reports the real topology.
	generation = removeMockKeyboardSeatGlobal(2)
	waitForMockKeyboardControl(t, generation)
	if rc := waylandKeyboardReadyForTest(); rc == 0 {
		stopMockKeyboardServer()
		t.Fatal("readiness accepted a removed selected seat")
	}
	const noSeatError = 2
	if got := waylandKeyboardLastErrorForTest(); got != noSeatError {
		stopMockKeyboardServer()
		t.Fatalf("last error after selected global removal=%d, want %d", got, noSeatError)
	}

	// A later capability change is visible to a fresh connection and restores
	// the backend without process-global teardown.
	const waylandSeatCapabilityKeyboard = 2
	generation = setMockKeyboardSeatCapabilities(0, waylandSeatCapabilityKeyboard)
	waitForMockKeyboardControl(t, generation)
	if rc := waylandKeyboardReadyForTest(); rc != 0 {
		stopMockKeyboardServer()
		t.Fatalf("readiness did not recover after a new capable seat: rc=%d", rc)
	}
	if rc := syncWaylandKeyboardForTest(); rc < 0 {
		stopMockKeyboardServer()
		t.Fatalf("sync after runtime-seat recovery failed with rc=%d", rc)
	}
	if got := mockKeyboardSelectedSeat(); got != 0 {
		stopMockKeyboardServer()
		t.Fatalf("recovered runtime seat=%d, want 0", got)
	}
}

func assertWaylandReadinessNonblocking(t *testing.T) {
	t.Helper()
	done := make(chan int, 1)
	go func() { done <- waylandKeyboardReadyForTest() }()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("no-event Wayland readiness failed with rc=%d", rc)
		}
	case <-time.After(250 * time.Millisecond):
		stopMockKeyboardServer()
		t.Fatal("no-event Wayland readiness blocked waiting for compositor events")
	}
}

func waitForMockKeyboardControl(t *testing.T, generation uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mockKeyboardControlAppliedGeneration() >= generation {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	stopMockKeyboardServer()
	t.Fatalf("mock Wayland runtime control generation %d was not applied", generation)
}

func waitForMockKeyboardResourceCleanup(t *testing.T, wantSeats, wantKeyboards uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mockKeyboardSeatResourcesDestroyed() == wantSeats &&
			mockKeyboardKeyboardResourcesDestroyed() == wantKeyboards {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf(
		"mock Wayland resource cleanup: seats=%d/%d keyboards=%d/%d",
		mockKeyboardSeatResourcesDestroyed(), wantSeats,
		mockKeyboardKeyboardResourcesDestroyed(), wantKeyboards,
	)
}

func waitForMockKeyboardServerCleanup(t *testing.T, done <-chan struct{}) {
	t.Helper()
	stopMockKeyboardServer()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Errorf("mock Wayland keyboard server did not stop during cleanup")
	}
}
