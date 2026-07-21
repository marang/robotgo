//go:build linux

package robotgo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestAlertEXMessageUsesDistinctCancelStatus(t *testing.T) {
	directory := linuxAlertTestDirectory(t)
	marker := filepath.Join(directory, "xmessage-args")
	t.Setenv("ROBOTGO_ALERT_TEST_MARKER", marker)
	writeLinuxAlertTestCommand(t, directory, linuxAlertXMessage,
		"#!/bin/sh\nprintf '%s' \"$*\" > \"$ROBOTGO_ALERT_TEST_MARKER\"\nexit 2\n")

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
