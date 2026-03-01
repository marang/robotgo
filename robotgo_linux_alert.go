//go:build linux

package robotgo

import (
    "fmt"
    "os/exec"
)

// Alert shows a simple alert dialog with optional custom button labels.
// On Linux it prefers native Wayland-friendly tools when available.
// Order of backends: zenity, kdialog, xmessage, notify-send (fire-and-forget).
// If cancel button is not given, only the default button is displayed.
//
// Examples:
//
//     robotgo.Alert("hi", "window", "ok", "cancel")
func Alert(title, msg string, args ...string) bool {
    defaultBtn, cancelBtn := alertArgs(args...)

    // Try zenity (GTK; works on Wayland and X11)
    if hasCmd("zenity") {
        if cancelBtn == "" {
            // OK-only
            cmd := exec.Command("zenity", "--info", "--no-markup",
                "--title", title, "--text", msg,
                "--ok-label", defaultBtn,
            )
            if err := cmd.Run(); err != nil {
                return false
            }
            return true
        }

        // Two buttons
        cmd := exec.Command("zenity", "--question", "--no-markup",
            "--title", title, "--text", msg,
            "--ok-label", defaultBtn, "--cancel-label", cancelBtn,
        )
        if err := cmd.Run(); err != nil {
            // zenity returns non-zero when Cancel pressed
            return false
        }
        return true
    }

    // Try kdialog (Qt; works on Wayland and X11)
    if hasCmd("kdialog") {
        if cancelBtn == "" {
            cmd := exec.Command("kdialog", "--title", title, "--msgbox", msg, "--ok-label", defaultBtn)
            if err := cmd.Run(); err != nil {
                return false
            }
            return true
        }
        cmd := exec.Command("kdialog", "--title", title, "--yesno", msg, "--yes-label", defaultBtn, "--no-label", cancelBtn)
        if err := cmd.Run(); err != nil {
            return false
        }
        return true
    }

    // Fallback to xmessage (X11/XWayland)
    if hasCmd("xmessage") {
        buttons := defaultBtn + ":0"
        if cancelBtn != "" {
            buttons = buttons + "," + cancelBtn + ":1"
        }
        // xmessage prints the index of the selected button to stdout
        out, err := exec.Command(
            "xmessage",
            "-center",
            "-title", title,
            "-buttons", buttons,
            "-default", defaultBtn,
            "-geometry", "400x200",
            msg,
        ).CombinedOutput()
        if err != nil {
            return false
        }
        return string(out) != "1"
    }

    // Last-resort: desktop notification without interaction
    if hasCmd("notify-send") {
        _ = exec.Command("notify-send", title, msg).Run()
        return true
    }

    // No available backend
    _ = fmt.Errorf("robotgo.Alert: no dialog backend found (tried zenity, kdialog, xmessage, notify-send)")
    return false
}

func hasCmd(name string) bool {
    _, err := exec.LookPath(name)
    return err == nil
}
