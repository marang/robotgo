//go:build freebsd || linux || netbsd || openbsd || solaris || dragonfly

package clipboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const waylandTestVersion = `if [ "$1" = --version ]; then
printf 'wl-clipboard 2.3.0\n'
exit 0
fi
`

const waylandTestSpool = `# Never write a fixture outside the test's private temporary directory.
[ "$TMPDIR" != "$ROBOTGO_CLIPBOARD_TEST_PARENT_TMP" ] || exit 42
case "$TMPDIR" in "$ROBOTGO_CLIPBOARD_TEST_PARENT_TMP"/*) ;; *) exit 43 ;; esac
printf '%s' "$TMPDIR" > "$ROBOTGO_CLIPBOARD_TEST_ROOT/spool-directory"
/bin/mkdir "$TMPDIR/wl-copy-buffer-fixture" || exit 44
/bin/cat > "$TMPDIR/wl-copy-buffer-fixture/input"
`

func installWaylandTestCommands(t *testing.T, copyBody, pasteBody string) string {
	t.Helper()
	directory := t.TempDir()
	parentTemp := filepath.Join(directory, "parent-temp")
	if err := os.Mkdir(parentTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{wlCopy: copyBody, wlPaste: pasteBody} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory)
	t.Setenv("TMPDIR", parentTemp)
	t.Setenv("ROBOTGO_CLIPBOARD_TEST_PARENT_TMP", parentTemp)
	t.Setenv("ROBOTGO_CLIPBOARD_TEST_ROOT", directory)
	previousTool, previousError := selectedTool, selectedToolError
	selectedTool, selectedToolError = unixClipboardWayland, nil
	t.Cleanup(func() { selectedTool, selectedToolError = previousTool, previousError })
	return directory
}

func requireWaylandTestSpoolGone(t *testing.T, directory string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "spool-directory"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(string(data)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned clipboard spool directory still exists or cannot be checked: %v", err)
	}
}

func TestWaylandClipboardPreservesTextAndCleansSpool(t *testing.T) {
	for _, text := range []string{"", "日本語 🙂\nsecond line\n"} {
		t.Run(text, func(t *testing.T) {
			directory := installWaylandTestCommands(t,
				waylandTestVersion+waylandTestSpool+`/bin/cat "$TMPDIR/wl-copy-buffer-fixture/input" > "$ROBOTGO_CLIPBOARD_TEST_ROOT/selection"`,
				`/bin/cat "$ROBOTGO_CLIPBOARD_TEST_ROOT/selection"`)
			parentTemp := os.Getenv("TMPDIR")
			if err := WriteAllContext(context.Background(), text); err != nil {
				t.Fatal(err)
			}
			got, err := ReadAllContext(context.Background())
			if err != nil || got != text {
				t.Fatalf("clipboard fixture round trip did not preserve text; error = %v", err)
			}
			requireWaylandTestSpoolGone(t, directory)
			if os.Getenv("TMPDIR") != parentTemp {
				t.Fatal("clipboard operation changed the parent's TMPDIR")
			}
		})
	}
}

func TestWaylandClipboardRejectsUnsafeVersionsBeforePayload(t *testing.T) {
	for _, version := range []string{"2.2.1", "2.3", "2.3.0-dev", "garbage", "-2.3.0", "2.3.999999999999999999999999"} {
		t.Run(version, func(t *testing.T) {
			directory := installWaylandTestCommands(t,
				`if [ "$1" = --version ]; then printf 'wl-clipboard `+version+`\n'; exit 0; fi
printf invoked > "$ROBOTGO_CLIPBOARD_TEST_ROOT/payload-invoked"
`, "exit 0")
			err := WriteAllContext(context.Background(), "fixed fixture")
			if err == nil || !strings.Contains(err.Error(), "2.3.0") {
				t.Fatalf("unsafe version error = %v, want actionable minimum version", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "payload-invoked")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsafe helper received a payload invocation")
			}
		})
	}
}

func TestWaylandClipboardProbeFailureDoesNotTransferPayload(t *testing.T) {
	directory := installWaylandTestCommands(t, `if [ "$1" = --version ]; then exit 7; fi
printf invoked > "$ROBOTGO_CLIPBOARD_TEST_ROOT/payload-invoked"
`, "exit 0")
	if err := WriteAllContext(context.Background(), "fixed fixture"); err == nil {
		t.Fatal("failed version probe was accepted")
	}
	if _, err := os.Stat(filepath.Join(directory, "payload-invoked")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed version probe transferred payload")
	}
}

func TestWaylandClipboardUsesTheProbedExecutable(t *testing.T) {
	directory := installWaylandTestCommands(t, `if [ "$1" = --version ]; then
printf ready > "$ROBOTGO_CLIPBOARD_TEST_ROOT/probe-ready"
while [ ! -f "$ROBOTGO_CLIPBOARD_TEST_ROOT/probe-continue" ]; do /bin/sleep 0.01; done
printf 'wl-clipboard 2.3.0\n'
exit 0
fi
`+waylandTestSpool, "exit 0")
	otherDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(otherDirectory, wlCopy), []byte(`#!/bin/sh
printf invoked > "$ROBOTGO_CLIPBOARD_TEST_ROOT/unprobed-helper"
/bin/cat > /dev/null
`), 0o700); err != nil {
		t.Fatal(err)
	}
	cancel, result := startCancelableClipboardTestCall(t, func(ctx context.Context) error {
		return WriteAllContext(ctx, "fixed fixture")
	})
	defer cancel()
	requireClipboardTestPath(t, filepath.Join(directory, "probe-ready"))
	// Environment mutation must not swap the payload recipient after probing.
	t.Setenv("PATH", otherDirectory)
	if err := os.WriteFile(filepath.Join(directory, "probe-continue"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(clipboardTestSynchronizationTimeout):
		t.Fatal("clipboard write did not finish after version probing")
	}
	if _, err := os.Stat(filepath.Join(directory, "unprobed-helper")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("clipboard text was sent to a different, unprobed executable")
	}
	requireWaylandTestSpoolGone(t, directory)
}

func TestWaylandClipboardProbeCancellation(t *testing.T) {
	directory := installWaylandTestCommands(t, `printf ready > "$ROBOTGO_CLIPBOARD_TEST_ROOT/probe-ready"
exec /bin/sleep 30
`, "exit 0")
	cancel, result := startCancelableClipboardTestCall(t, func(ctx context.Context) error {
		return WriteAllContext(ctx, "fixed fixture")
	})
	requireClipboardTestPath(t, filepath.Join(directory, "probe-ready"))
	cancel()
	requireCanceledClipboardTestCall(t, result)
}

func TestWaylandClipboardLegacyTimeoutBoundsProbe(t *testing.T) {
	installWaylandTestCommands(t, "exec /bin/sleep 30\n", "exit 0")
	started := time.Now()
	if err := WriteAll("fixed fixture"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("legacy clipboard timeout = %v", err)
	}
	if elapsed := time.Since(started); elapsed > defaultCommandTimeout+clipboardTestSynchronizationTimeout {
		t.Fatalf("legacy clipboard exceeded its command and cleanup deadline: %s", elapsed)
	}
}

func TestWaylandClipboardCannotCreatePrivateSpool(t *testing.T) {
	directory := installWaylandTestCommands(t, waylandTestVersion+`printf invoked > "$ROBOTGO_CLIPBOARD_TEST_ROOT/payload-invoked"`, "exit 0")
	t.Setenv("TMPDIR", filepath.Join(directory, "missing-parent"))
	if err := WriteAllContext(context.Background(), "fixed fixture"); err == nil || !strings.Contains(err.Error(), "create private") {
		t.Fatalf("private directory creation error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "payload-invoked")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("helper received text without a private temporary directory")
	}
}

func TestWaylandClipboardFailedStartCleansPrivateSpool(t *testing.T) {
	directory := installWaylandTestCommands(t, `if [ "$1" = --version ]; then
/bin/chmod 600 "$0"
printf 'wl-clipboard 2.3.0\n'
exit 0
fi
exit 99
`, "exit 0")
	if err := WriteAllContext(context.Background(), "fixed fixture"); err == nil {
		t.Fatal("non-executable copy helper returned success")
	}
	entries, err := os.ReadDir(filepath.Join(directory, "parent-temp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed helper start left private spool entries: %d, %v", len(entries), err)
	}
}

func TestWaylandClipboardAlreadyCanceledDoesNotStartProbe(t *testing.T) {
	directory := installWaylandTestCommands(t, `printf invoked > "$ROBOTGO_CLIPBOARD_TEST_ROOT/invoked"`, "exit 0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WriteAllContext(ctx, "fixed fixture"); !errors.Is(err, context.Canceled) {
		t.Fatalf("already canceled write = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "invoked")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("already canceled write started the helper")
	}
}

func TestWaylandClipboardFailedCopyCleansSpool(t *testing.T) {
	directory := installWaylandTestCommands(t, waylandTestVersion+waylandTestSpool+"exit 7\n", "exit 0")
	if err := WriteAllContext(context.Background(), "fixed fixture"); err == nil {
		t.Fatal("failed copy returned success")
	}
	requireWaylandTestSpoolGone(t, directory)
}

func TestWaylandClipboardDeadlineCleansSpool(t *testing.T) {
	directory := installWaylandTestCommands(t, waylandTestVersion+waylandTestSpool+`printf ready > "$ROBOTGO_CLIPBOARD_TEST_ROOT/copy-ready"
exec /bin/sleep 30
`, "exit 0")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- WriteAllContext(ctx, "fixed fixture")
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(clipboardTestSynchronizationTimeout):
			t.Error("timed out clipboard fixture did not stop during cleanup")
		}
	})
	requireClipboardTestPath(t, filepath.Join(directory, "copy-ready"))
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timed out write = %v", err)
		}
	case <-time.After(clipboardTestSynchronizationTimeout):
		t.Fatal("timed out write did not return")
	}
	requireWaylandTestSpoolGone(t, directory)
}

func TestWaylandClipboardCleanupFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denied cleanup fixture requires an unprivileged user")
	}
	directory := installWaylandTestCommands(t, waylandTestVersion+waylandTestSpool+`/bin/chmod 500 "$ROBOTGO_CLIPBOARD_TEST_PARENT_TMP"
exit 7
`, "exit 0")
	parentTemp := os.Getenv("TMPDIR")
	t.Cleanup(func() {
		if err := os.Chmod(parentTemp, 0o700); err != nil {
			t.Error(err)
		}
	})
	err := WriteAllContext(context.Background(), "fixed fixture")
	if err == nil || !strings.Contains(err.Error(), "remove private wl-copy") || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("write did not join backend and cleanup errors: %v", err)
	}
	// Restore permissions and remove only this test-owned directory before
	// checking absence, including when the production cleanup failed as intended.
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

func TestWaylandClipboardPrimaryFailureDoesNotRetry(t *testing.T) {
	directory := installWaylandTestCommands(t, waylandTestVersion+`printf '%s\n' "$@" >> "$ROBOTGO_CLIPBOARD_TEST_ROOT/copy-args"
exit 8
`, `printf '%s\n' "$@" >> "$ROBOTGO_CLIPBOARD_TEST_ROOT/paste-args"
exit 8
`)
	if err := WriteAllContext(context.Background(), "fixed fixture", SelectionPrimary); err == nil {
		t.Fatal("unsupported primary write returned success")
	}
	if _, err := ReadAllContext(context.Background(), SelectionPrimary); err == nil {
		t.Fatal("unsupported primary read returned success")
	}
	for path, expected := range map[string]string{
		"copy-args":  "--type\ntext/plain;charset=utf-8\n--primary\n",
		"paste-args": "--no-newline\n--type\ntext\n--primary\n",
	} {
		data, err := os.ReadFile(filepath.Join(directory, path))
		if err != nil || string(data) != expected {
			t.Fatalf("primary helper arguments = %q, error = %v; unexpected retry or substitution", data, err)
		}
	}
}
