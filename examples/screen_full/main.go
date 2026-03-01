package main

import (
    "fmt"
    "time"

    "github.com/marang/robotgo"
    "os"
)

func main() {
    // Environment + display server info
    fmt.Println("display server:", robotgo.DetectDisplayServer())
    fmt.Println("env WAYLAND_DISPLAY=", os.Getenv("WAYLAND_DISPLAY"))
    fmt.Println("env DISPLAY=", os.Getenv("DISPLAY"))
    fmt.Println("env XDG_SESSION_TYPE=", os.Getenv("XDG_SESSION_TYPE"))
    if os.Getenv("ROBOTGO_FORCE_PORTAL") != "" {
        fmt.Println("env ROBOTGO_FORCE_PORTAL=1 (forcing portal path)")
    }

    // Capture the full current screen using the display bounds.
    // This ensures correct dimensions even on Wayland where defaults
    // may be unavailable without explicit bounds.
    x, y, w, h := robotgo.GetDisplayBounds(0)
    img, err := robotgo.CaptureImg(x, y, w, h)
    if err != nil {
        fmt.Println("CaptureImg error:", err)
        return
    }

    name := fmt.Sprintf("full_%d.png", time.Now().Unix())
    if err := robotgo.Save(img, name); err != nil {
        fmt.Println("Save error:", err)
        return
    }

    // Identify which backend handled the capture
    backend := robotgo.LastBackend()
    method := "unknown"
    switch backend {
    case robotgo.BackendScreencopy:
        method = "Wayland native (wlr-screencopy)"
    case robotgo.BackendPortal:
        method = "xdg-desktop-portal (Screenshot)"
    case robotgo.BackendX11:
        method = "X11 (XGetImage)"
    default:
        method = string(backend)
    }
    fmt.Println("saved:", name, "backend:", backend, "method:", method)
}
