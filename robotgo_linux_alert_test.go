//go:build linux

package robotgo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func linuxAlertTestDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	t.Setenv("PATH", directory)
	return directory
}

func writeLinuxAlertTestCommand(t *testing.T, directory, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestAlertEFallsBackAfterBackendFailure(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	marker := filepath.Join(directory, "backend-order")
	t.Setenv("ROBOTGO_ALERT_TEST_MARKER", marker)
	writeLinuxAlertTestCommand(t, directory, linuxAlertZenity,
		"#!/bin/sh\nprintf zenity >> \"$ROBOTGO_ALERT_TEST_MARKER\"\nexit 2\n")
	writeLinuxAlertTestCommand(t, directory, linuxAlertKDialog,
		"#!/bin/sh\nprintf kdialog >> \"$ROBOTGO_ALERT_TEST_MARKER\"\nexit 0\n")

	accepted, err := AlertE("title", "message", "OK", "Cancel")
	if err != nil || !accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want accepted", accepted, err)
	}
	order, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(order) != "zenitykdialog" {
		t.Fatalf("backend order = %q, want zenity then kdialog", order)
	}
}

func TestAlertEUserCancelStopsFallback(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	marker := filepath.Join(directory, "backend-order")
	t.Setenv("ROBOTGO_ALERT_TEST_MARKER", marker)
	writeLinuxAlertTestCommand(t, directory, linuxAlertZenity,
		"#!/bin/sh\nprintf zenity >> \"$ROBOTGO_ALERT_TEST_MARKER\"\nexit 1\n")
	writeLinuxAlertTestCommand(t, directory, linuxAlertKDialog,
		"#!/bin/sh\nprintf kdialog >> \"$ROBOTGO_ALERT_TEST_MARKER\"\nexit 0\n")

	accepted, err := AlertE("title", "message", "OK", "Cancel")
	if err != nil || accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want user cancellation", accepted, err)
	}
	order, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(order) != "zenity" {
		t.Fatalf("backend order after cancellation = %q, want zenity only", order)
	}
}

func TestAlertEXMessageTreatsLabelsAndMessageAsLiteralData(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		labels      []string
		wantButtons string
		wantDefault string
	}{
		{
			name:        "punctuation and Unicode",
			message:     "-buttons injected:99\nUnicode: 日本語 🐾\n",
			labels:      []string{`Save, retry: C:\文書`, `No: thanks, \戻る`},
			wantButtons: `Save\, retry\: C\:\\文書:0,No\: thanks\, \\戻る:2`,
			wantDefault: `Save, retry: C:\文書`,
		},
		{
			name:        "option-like message and trailing label backslash",
			message:     "-file /not-a-message-file",
			labels:      []string{`Path\`},
			wantButtons: `Path\\:0`,
			wantDefault: `Path\`,
		},
		{
			name:        "multiline without trailing newline",
			message:     "first line\nsecond line",
			labels:      []string{"OK", "Cancel"},
			wantButtons: "OK:0,Cancel:2",
			wantDefault: "OK",
		},
		{
			name:        "empty message and default label",
			wantButtons: "Ok:0",
			wantDefault: "Ok",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := linuxAlertTestDirectory(t)
			argvPath := filepath.Join(directory, "xmessage-argv")
			stdinPath := filepath.Join(directory, "xmessage-stdin")
			t.Setenv("ROBOTGO_ALERT_TEST_ARGV", argvPath)
			t.Setenv("ROBOTGO_ALERT_TEST_STDIN", stdinPath)
			writeLinuxAlertTestCommand(t, directory, linuxAlertXMessage, `#!/bin/sh
printf '%s\000' "$@" > "$ROBOTGO_ALERT_TEST_ARGV"
/bin/cat > "$ROBOTGO_ALERT_TEST_STDIN"
`)
			accepted, err := AlertE("title", test.message, test.labels...)
			if err != nil || !accepted {
				t.Fatalf("AlertE = accepted %t, error %v; want accepted", accepted, err)
			}
			argv, err := os.ReadFile(argvPath)
			if err != nil {
				t.Fatal(err)
			}
			wantArgs := []string{
				"-center", "-title", "title",
				"-buttons", test.wantButtons,
				"-default", test.wantDefault,
				"-geometry", "400x200", "-file", "-",
			}
			if want := strings.Join(wantArgs, "\x00") + "\x00"; string(argv) != want {
				t.Fatalf("xmessage argv = %q, want %q", argv, want)
			}
			stdin, err := os.ReadFile(stdinPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(stdin) != test.message {
				t.Fatalf("xmessage stdin = %q, want %q", stdin, test.message)
			}
		})
	}
}

func TestAlertEXMessageUsesDistinctCancelStatus(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	marker := filepath.Join(directory, "xmessage-args")
	t.Setenv("ROBOTGO_ALERT_TEST_MARKER", marker)
	writeLinuxAlertTestCommand(t, directory, linuxAlertXMessage,
		"#!/bin/sh\nprintf '%s' \"$*\" > \"$ROBOTGO_ALERT_TEST_MARKER\"\nexit 2\n")
	writeLinuxAlertTestCommand(t, directory, linuxAlertNotifySend,
		"#!/bin/sh\nprintf notified >> \"$ROBOTGO_ALERT_TEST_MARKER\"\n")

	accepted, err := AlertE("title", "message", "OK", "Cancel")
	if err != nil || accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want xmessage cancellation", accepted, err)
	}
	args, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "OK:0,Cancel:2") {
		t.Fatalf("xmessage arguments = %q, want distinct cancel exit status", args)
	}
	if strings.Contains(string(args), "notified") {
		t.Fatal("xmessage cancellation invoked notify-send fallback")
	}
}

func TestAlertEXMessageRejectsNULMessageBeforeInvocation(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	marker := filepath.Join(directory, "invoked")
	t.Setenv("ROBOTGO_ALERT_TEST_MARKER", marker)
	for _, name := range []string{linuxAlertXMessage, linuxAlertNotifySend} {
		writeLinuxAlertTestCommand(t, directory, name, `#!/bin/sh
printf invoked > "$ROBOTGO_ALERT_TEST_MARKER"
/bin/cat > /dev/null
`)
	}

	accepted, err := AlertE("title", "before\x00after")
	if accepted || !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("AlertE = accepted %t, error %v; want invalid NUL-containing message", accepted, err)
	}
	for _, name := range []string{linuxAlertXMessage, linuxAlertNotifySend} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("AlertE error = %v, want failed backend %s", err, name)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid message invoked a backend or stat failed: %v", err)
	}
}

func TestAlertEXMessageFailureContinuesFallbackOrder(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	marker := filepath.Join(directory, "backend-order")
	t.Setenv("ROBOTGO_ALERT_TEST_MARKER", marker)
	writeLinuxAlertTestCommand(t, directory, linuxAlertZenity,
		"#!/bin/sh\nprintf zenity >> \"$ROBOTGO_ALERT_TEST_MARKER\"\nexit 2\n")
	writeLinuxAlertTestCommand(t, directory, linuxAlertKDialog,
		"#!/bin/sh\nprintf kdialog >> \"$ROBOTGO_ALERT_TEST_MARKER\"\nexit 2\n")
	writeLinuxAlertTestCommand(t, directory, linuxAlertXMessage,
		"#!/bin/sh\nprintf xmessage >> \"$ROBOTGO_ALERT_TEST_MARKER\"\n/bin/cat > /dev/null\nexit 1\n")
	writeLinuxAlertTestCommand(t, directory, linuxAlertNotifySend,
		"#!/bin/sh\nprintf notify-send >> \"$ROBOTGO_ALERT_TEST_MARKER\"\n")

	accepted, err := AlertE("title", "message")
	if err != nil || !accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want informational fallback success", accepted, err)
	}
	order, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(order) != "zenitykdialogxmessagenotify-send" {
		t.Fatalf("backend order = %q, want zenity, kdialog, xmessage, notify-send", order)
	}
}

func TestAlertEXMessageBoundsInheritedStandardInput(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	pidPath := filepath.Join(directory, "xmessage-child")
	t.Setenv("ROBOTGO_ALERT_TEST_CHILD", pidPath)
	writeLinuxAlertTestCommand(t, directory, linuxAlertXMessage, `#!/bin/sh
exec 3<&0
/bin/sleep 5 <&3 &
printf '%s' "$!" > "$ROBOTGO_ALERT_TEST_CHILD"
`)
	// Read the PID during cleanup too, so failed assertions cannot leak a child.
	t.Cleanup(func() {
		data, err := os.ReadFile(pidPath)
		if err != nil {
			return
		}
		if pid, err := strconv.Atoi(string(data)); err == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	started := time.Now()
	accepted, err := AlertE("title", strings.Repeat("x", 8<<20))
	if !errors.Is(err, exec.ErrWaitDelay) || accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want inherited-stdin cleanup error", accepted, err)
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("AlertE waited %s for inherited stdin", elapsed)
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid child PID %q: %v", data, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		// Orphans can remain zombies under container PID 1, but no longer hold I/O.
		state := strings.LastIndexByte(string(data), ')') + 2
		if state >= 2 && state < len(data) && (data[state] == 'Z' || data[state] == 'X') {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("xmessage descendant %d survived inherited-stdin cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAlertEXMessageWaitsForChoiceBeforeCleanup(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	writeLinuxAlertTestCommand(t, directory, linuxAlertXMessage, `#!/bin/sh
/bin/cat > /dev/null
/bin/sleep 1
`)
	// The helper remains alive longer than the post-exit I/O cleanup delay.
	accepted, err := AlertE("title", "message")
	if err != nil || !accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want a completed user choice", accepted, err)
	}
}

func TestAlertEXMessageErrorIsNotCancellation(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	writeLinuxAlertTestCommand(t, directory, linuxAlertXMessage,
		"#!/bin/sh\nexit 1\n")

	accepted, err := AlertE("title", "message", "OK", "Cancel")
	if err == nil || accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want xmessage backend error", accepted, err)
	}
	if !strings.Contains(err.Error(), linuxAlertXMessage) {
		t.Fatalf("AlertE error = %v, want backend name", err)
	}
}

func TestAlertENotifySendSupportsDefaultInformationalAlert(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	marker := filepath.Join(directory, "notified")
	t.Setenv("ROBOTGO_ALERT_TEST_MARKER", marker)
	writeLinuxAlertTestCommand(t, directory, linuxAlertNotifySend,
		"#!/bin/sh\nprintf notified > \"$ROBOTGO_ALERT_TEST_MARKER\"\n")

	accepted, err := AlertE("title", "message")
	if err != nil || !accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want informational success", accepted, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("notify-send marker: %v", err)
	}
}

func TestAlertENotifySendDoesNotFakeInteractiveChoice(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	marker := filepath.Join(directory, "notified")
	t.Setenv("ROBOTGO_ALERT_TEST_MARKER", marker)
	writeLinuxAlertTestCommand(t, directory, linuxAlertNotifySend,
		"#!/bin/sh\nprintf notified > \"$ROBOTGO_ALERT_TEST_MARKER\"\n")

	accepted, err := AlertE("title", "message", "OK", "Cancel")
	if !errors.Is(err, ErrNotSupported) || accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want unsupported interactive notification", accepted, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-interactive notification was invoked or stat failed: %v", err)
	}
}

func TestAlertENotifySendFailureIsObservable(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	writeLinuxAlertTestCommand(t, directory, linuxAlertNotifySend,
		"#!/bin/sh\nexit 3\n")

	accepted, err := AlertE("title", "message")
	if err == nil || accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want notify-send backend error", accepted, err)
	}
	if !strings.Contains(err.Error(), linuxAlertNotifySend) {
		t.Fatalf("AlertE error = %v, want backend name", err)
	}
}

func TestAlertEMissingBackendIsExplicit(t *testing.T) {
	linuxAlertTestDirectory(t)
	accepted, err := AlertE("title", "message")
	if !errors.Is(err, ErrNotSupported) || accepted {
		t.Fatalf("AlertE = accepted %t, error %v; want ErrNotSupported", accepted, err)
	}
	if Alert("title", "message") {
		t.Fatal("legacy Alert reported success without a backend")
	}
}
