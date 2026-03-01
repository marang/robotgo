# Docs

Documents are not necessarily updated synchronously, slower than godoc, please see examples and godoc.
## Wayland Portal

RobotGo capture flow on Linux:

1. Wayland: use native `wlr-screencopy` (DMA-BUF or wl_shm).
2. If Wayland capture is unavailable/fails, fallback to portal screenshot.
3. X11: use X11 capture path.

Portal fallback needs `xdg-desktop-portal` and a matching desktop backend.
Use `ROBOTGO_FORCE_PORTAL=1` to force portal capture in tests/environments.

```go
bit, err := robotgo.CaptureScreen(0, 0, 100, 100)
if err != nil {
    // handle error
}
defer robotgo.FreeBitmap(bit)
```

`portal.Capture(...)` exists behind the `linux,portal` build tag and is mainly
for the explicit portal package path.
