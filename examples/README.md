# RobotGo examples

These examples use this fork's module path:

```bash
go get github.com/marang/robotgo
```

Run an example from the repository root, for example:

```bash
go run ./examples/runtime_capabilities
go run ./examples/screen_full
```

Available examples:

- [`runtime_capabilities`](runtime_capabilities/main.go): cross-platform build,
  backend, feature, permission, and unsupported diagnostics.
- [`linux_capabilities`](linux_capabilities/main.go): detailed Linux display
  server, compositor, portal, and fallback diagnostics.
- [`screen`](screen/main.go): capture, pixel, bitmap, and display operations.
- [`screen_full`](screen_full/main.go): full-screen capture with selected
  backend reporting.
- [`screencast_capture`](screencast_capture/main.go): persistent
  ScreenCast/PipeWire capture on supported Wayland builds.
- [`remote_desktop_input`](remote_desktop_input/main.go): explicit,
  consent-aware RemoteDesktop portal input.
- [`mouse`](mouse/main.go): pointer movement, clicks, and scrolling.
- [`key`](key/main.go): keyboard and clipboard operations.
- [`window`](window/main.go): window and process helpers.
- [`scale`](scale/main.go): display scaling information.

Examples perform real desktop actions. Inspect them before running, especially
the input and window/process examples. Portal examples may show a consent dialog
only when explicitly asked to connect or demonstrate an operation.
