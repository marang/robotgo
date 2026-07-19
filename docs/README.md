# Docs

Use the following documents as the primary entry points:

- [Workflow conventions](workflow_conventions.md) for Linear planning, branches,
  pull requests, CI, reviewer feedback, merges, and cleanup.
- [Test guide](../TEST.md) for validation commands and runtime prerequisites.
- [Product roadmap](plan/product-roadmap.md) for delivery phases.
- [Agentic desktop automation plan](plan/agentic-desktop-automation.md) for the
  session, policy, observe-act-verify, and adapter architecture.
- [Wayland status](wayland-tasks.md) for backend support and open Wayland work.
- [Upstream compatibility audit](compatibility/upstream-master.md) for
  selectively adopted and intentionally rejected upstream changes.

API details are available through examples and Go documentation. Lasting
backend behavior and support contracts are documented in this directory.

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
