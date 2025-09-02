# Docs

Documents are not necessarily updated synchronously, slower than godoc, please see examples and godoc.
## Wayland Portal

RobotGo can capture the screen on Wayland compositors that do not expose the
`wlr-screencopy` protocol by using the freedesktop portal ScreenCast API and
PipeWire. Ensure `xdg-desktop-portal` and a corresponding backend are installed
and that PipeWire is running.

```go
ctx := context.Background()
bmp, err := portal.Capture(ctx, 0, 0, 100, 100)
if err != nil {
    // handle error
}
defer robotgo.FreeBitmap(robotgo.CBitmap(bmp))
```

