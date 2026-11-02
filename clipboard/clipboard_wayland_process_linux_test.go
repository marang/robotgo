//go:build linux

package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const waylandTestOwner = `exec 3< "$TMPDIR/wl-copy-buffer-fixture/input"
(
printf ready > "$ROBOTGO_CLIPBOARD_TEST_ROOT/owner-ready"
while [ ! -f "$ROBOTGO_CLIPBOARD_TEST_ROOT/request" ]; do /bin/sleep 0.01; done
/bin/cat <&3 > "$ROBOTGO_CLIPBOARD_TEST_ROOT/owner-result"
exec /bin/sleep 30
) </dev/null >/dev/null &
printf '%s %s' "$$" "$!" > "$ROBOTGO_CLIPBOARD_TEST_ROOT/processes.tmp"
/bin/mv "$ROBOTGO_CLIPBOARD_TEST_ROOT/processes.tmp" "$ROBOTGO_CLIPBOARD_TEST_ROOT/processes"
`

func waylandTestProcessRecord(directory string) (group, child int, err error) {
	data, err := os.ReadFile(filepath.Join(directory, "processes"))
	if err != nil {
		return 0, 0, err
	}
	_, err = fmt.Sscanf(string(data), "%d %d", &group, &child)
	if err == nil && (group <= 0 || child <= 0) {
		err = errors.New("invalid clipboard fixture process record")
	}
	return group, child, err
}

func waylandTestProcessAlive(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return false
	}
	if err != nil {
		return true
	}
	closingParen := strings.LastIndexByte(string(data), ')')
	if closingParen < 0 || closingParen+2 >= len(data) {
		return true
	}
	return data[closingParen+2] != 'Z' && data[closingParen+2] != 'X'
}

func requireWaylandTestProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(clipboardTestSynchronizationTimeout)
	for waylandTestProcessAlive(pid) {
		if time.Now().After(deadline) {
			t.Errorf("owned clipboard fixture process %d survived cleanup", pid)
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func cleanupWaylandTestOwner(t *testing.T, directory string) {
	t.Helper()
	// Register before the invocation and read the record during cleanup, not
	// only after assertions: early failures must not orphan a successful owner.
	t.Cleanup(func() {
		group, child, err := waylandTestProcessRecord(directory)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			t.Error(err)
			return
		}
		_ = syscall.Kill(-group, syscall.SIGKILL)
		_ = syscall.Kill(child, syscall.SIGKILL)
		requireWaylandTestProcessGone(t, child)
	})
}

func TestWaylandClipboardSuccessfulOwnerSurvivesHandoff(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run("legacy="+strconv.FormatBool(legacy), func(t *testing.T) {
			testWaylandClipboardOwnerHandoff(t, legacy)
		})
	}
}

func testWaylandClipboardOwnerHandoff(t *testing.T, legacy bool) {
	t.Helper()
	directory := installWaylandTestCommands(t, waylandTestVersion+waylandTestSpool+waylandTestOwner, "exit 0")
	cleanupWaylandTestOwner(t, directory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const fixture = "selection-owner fixture 日本語\n"
	started := time.Now()
	var err error
	if legacy {
		err = WriteAll(fixture)
	} else {
		err = WriteAllContext(ctx, fixture)
	}
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) >= clipboardTestSynchronizationTimeout {
		t.Fatal("successful owner held the clipboard call open")
	}
	cancel()
	requireClipboardTestPath(t, filepath.Join(directory, "owner-ready"))
	requireWaylandTestSpoolGone(t, directory)
	_, child, err := waylandTestProcessRecord(directory)
	if err != nil || !waylandTestProcessAlive(child) {
		t.Fatalf("successful owner did not survive context cancellation: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "request"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	requireClipboardTestPath(t, filepath.Join(directory, "owner-result"))
	// Wait for the complete response instead of treating file creation as EOF.
	deadline := time.Now().Add(clipboardTestSynchronizationTimeout)
	for {
		data, err := os.ReadFile(filepath.Join(directory, "owner-result"))
		if err == nil && string(data) == fixture {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("owner could not read its open spool after unlinking: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWaylandClipboardFailureTerminatesOwnerAndCleansSpool(t *testing.T) {
	for _, cancelCall := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelCall), func(t *testing.T) {
			tail := "exit 7\n"
			if cancelCall {
				tail = "wait\n"
			}
			directory := installWaylandTestCommands(t, waylandTestVersion+waylandTestSpool+waylandTestOwner+tail, "exit 0")
			cleanupWaylandTestOwner(t, directory)
			if cancelCall {
				cancel, result := startCancelableClipboardTestCall(t, func(ctx context.Context) error {
					return WriteAllContext(ctx, "fixed fixture")
				})
				requireClipboardTestPath(t, filepath.Join(directory, "processes"))
				cancel()
				requireCanceledClipboardTestCall(t, result)
			} else if err := WriteAllContext(context.Background(), "fixed fixture"); err == nil {
				t.Fatal("failed copy returned success")
			}
			_, child, err := waylandTestProcessRecord(directory)
			if err != nil {
				t.Fatal(err)
			}
			requireWaylandTestProcessGone(t, child)
			requireWaylandTestSpoolGone(t, directory)
		})
	}
}

func TestWaylandClipboardCleanupFailureTerminatesOwner(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denied cleanup fixture requires an unprivileged user")
	}
	directory := installWaylandTestCommands(t, waylandTestVersion+waylandTestSpool+waylandTestOwner+`/bin/chmod 500 "$ROBOTGO_CLIPBOARD_TEST_PARENT_TMP"
`, "exit 0")
	cleanupWaylandTestOwner(t, directory)
	parentTemp := os.Getenv("TMPDIR")
	t.Cleanup(func() {
		if err := os.Chmod(parentTemp, 0o700); err != nil {
			t.Error(err)
		}
	})
	if err := WriteAllContext(context.Background(), "fixed fixture"); err == nil || !strings.Contains(err.Error(), "remove private") {
		t.Fatalf("cleanup failure = %v", err)
	}
	_, child, err := waylandTestProcessRecord(directory)
	if err != nil {
		t.Fatal(err)
	}
	requireWaylandTestProcessGone(t, child)
	if err := os.Chmod(parentTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "spool-directory"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(string(data)); err != nil {
		t.Fatal(err)
	}
	requireWaylandTestSpoolGone(t, directory)
}
