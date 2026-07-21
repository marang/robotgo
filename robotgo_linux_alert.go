//go:build linux

package robotgo

import (
	"errors"
	"fmt"
	"os/exec"
)

const (
	linuxAlertZenity     = "zenity"
	linuxAlertKDialog    = "kdialog"
	linuxAlertXMessage   = "xmessage"
	linuxAlertNotifySend = "notify-send"

	linuxAlertNoCancelExit       = -1
	linuxAlertDialogCancelExit   = 1
	linuxAlertXMessageCancelExit = 2
)

// Alert shows a simple alert dialog and preserves the legacy bool-only API.
// Use AlertE when backend failures must be distinguished from user rejection.
func Alert(title, msg string, args ...string) bool {
	accepted, _ := AlertE(title, msg, args...)
	return accepted
}

// AlertE shows a simple alert dialog and reports backend failures explicitly.
// On Linux it tries zenity, kdialog, xmessage, then notify-send. The final
// notify-send fallback is valid only for an OK-only informational alert because
// it cannot report a button choice without optional, backend-dependent actions.
func AlertE(title, msg string, args ...string) (bool, error) {
	defaultBtn, cancelBtn := alertArgs(args...)
	var backendErrors []error

	if hasCmd(linuxAlertZenity) {
		commandArgs := []string{
			"--info", "--no-markup",
			"--title", title, "--text", msg,
			"--ok-label", defaultBtn,
		}
		if cancelBtn != "" {
			commandArgs = []string{
				"--question", "--no-markup",
				"--title", title, "--text", msg,
				"--ok-label", defaultBtn, "--cancel-label", cancelBtn,
			}
		}
		accepted, err := runLinuxAlertCommand(
			linuxAlertZenity, commandArgs, linuxAlertDialogCancelExit,
		)
		if err == nil {
			return accepted, nil
		}
		backendErrors = append(backendErrors, err)
	}

	if hasCmd(linuxAlertKDialog) {
		commandArgs := []string{
			"--title", title, "--msgbox", msg, "--ok-label", defaultBtn,
		}
		if cancelBtn != "" {
			commandArgs = []string{
				"--title", title, "--yesno", msg,
				"--yes-label", defaultBtn, "--no-label", cancelBtn,
			}
		}
		accepted, err := runLinuxAlertCommand(
			linuxAlertKDialog, commandArgs, linuxAlertDialogCancelExit,
		)
		if err == nil {
			return accepted, nil
		}
		backendErrors = append(backendErrors, err)
	}

	if hasCmd(linuxAlertXMessage) {
		buttons := defaultBtn + ":0"
		cancelExit := linuxAlertNoCancelExit
		if cancelBtn != "" {
			buttons += fmt.Sprintf(",%s:%d", cancelBtn, linuxAlertXMessageCancelExit)
			cancelExit = linuxAlertXMessageCancelExit
		}
		accepted, err := runLinuxAlertCommand(linuxAlertXMessage, []string{
			"-center",
			"-title", title,
			"-buttons", buttons,
			"-default", defaultBtn,
			"-geometry", "400x200",
			msg,
		}, cancelExit)
		if err == nil {
			return accepted, nil
		}
		backendErrors = append(backendErrors, err)
	}

	if hasCmd(linuxAlertNotifySend) {
		if cancelBtn != "" {
			backendErrors = append(backendErrors, fmt.Errorf(
				"%w: %s cannot report an interactive alert choice",
				ErrNotSupported, linuxAlertNotifySend,
			))
		} else {
			accepted, err := runLinuxAlertCommand(
				linuxAlertNotifySend, []string{title, msg}, linuxAlertNoCancelExit,
			)
			if err == nil {
				return accepted, nil
			}
			backendErrors = append(backendErrors, err)
		}
	}

	if len(backendErrors) > 0 {
		return false, errors.Join(backendErrors...)
	}
	return false, fmt.Errorf(
		"%w: no Linux alert backend found (tried %s, %s, %s, %s)",
		ErrNotSupported,
		linuxAlertZenity,
		linuxAlertKDialog,
		linuxAlertXMessage,
		linuxAlertNotifySend,
	)
}

func runLinuxAlertCommand(name string, args []string, cancelExit int) (bool, error) {
	err := exec.Command(name, args...).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if cancelExit >= 0 && errors.As(err, &exitErr) && exitErr.ExitCode() == cancelExit {
		return false, nil
	}
	return false, fmt.Errorf("run Linux alert backend %s: %w", name, err)
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
