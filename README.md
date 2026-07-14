# RobotGo

[![CI: main](https://github.com/marang/robotgo/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/marang/robotgo/actions/workflows/go.yml?query=branch%3Amain)
[![Go Reference](https://pkg.go.dev/badge/github.com/marang/robotgo.svg)](https://pkg.go.dev/github.com/marang/robotgo)

<p align="center">
  <img src="docs/assets/robotgo-hero.png" alt="RobotGo desktop automation" width="100%">
</p>

RobotGo is a cross-platform desktop automation library for Go. It controls the
mouse and keyboard, captures screens and pixels, manages windows and processes,
and converts images and bitmaps.

## About this fork

> **This is `marang/robotgo`, not the original `go-vgo/robotgo` module.** Use
> `github.com/marang/robotgo` in `go get` and imports. The two repositories are
> separate Go modules.

This fork has diverged substantially from the original implementation, chiefly
to make Linux automation Wayland-first without weakening macOS, Windows, or
Linux/X11 behavior. Relevant new upstream features are still reviewed
selectively, then adapted, hardened, and tested against this repository's
backend and error contracts rather than merged blindly.

Current technical differences include:

- Native Wayland `wlr-screencopy` capture with DMA-BUF/`wl_shm` selection and
  explicit freedesktop Screenshot and persistent ScreenCast/PipeWire portal
  paths.
- A consent-aware RemoteDesktop portal session client for explicit GNOME/KDE
  pointer and keyboard injection, with cancellable lifecycle and cleanup.
- Error-returning mouse, keyboard, capture, and window APIs alongside legacy
  compatibility APIs.
- Runtime capability reporting that probes live protocols and services and
  explains backend choice, fallback, and unsupported behavior.
- Sway, Hyprland, generic wlroots, and Wayland-core window backend resolution,
  with partial operations reported honestly instead of universal support being
  implied.
- A defined non-CGO contract: Pure-Go capture is available through CoreGraphics
  on macOS, native APIs on Windows, and X11; Wayland capture uses the
  consent-aware Screenshot portal, and unavailable GUI operations return
  `ErrNotSupported` rather than plausible zero values.
- Hermetic portal/compositor tests, tagged Wayland integration suites, and CI
  coverage for Linux, macOS, Windows, Wayland, portal, lint, and non-CGO modes.
- Open, auditable native and Go backend code, including explicit resource
  ownership, bounded waits, and fallback diagnostics.

Upstream authorship and history remain credited under
[Upstream and attribution](#upstream-and-attribution). Upstream URLs elsewhere
in that section are historical references, not installation or support links
for this fork.

## Features

| Area | Available functionality |
|---|---|
| Mouse | Move, relative move, smooth move, drag, click, button toggle, scroll, and location where the platform exposes it |
| Keyboard | Key taps and combinations, key state changes, text/Unicode input, delays, and clipboard-assisted input |
| Screen and pixels | Full/region capture, display bounds and scale, pixel/color queries, bitmap conversion and string helpers, image save, and region/tolerance color search |
| Windows | Active window, title, close, minimize/maximize, topmost queries/setters, bounds/client geometry, and compositor-specific Wayland variants where supported |
| Processes | Enumerate, inspect, find, activate, and terminate processes |
| Images | Go image/bitmap conversion, template helpers, encoding, saving, and optional OCR |
| Diagnostics | Build/backend detection plus feature-level capability, permission, fallback, and unsupported reporting |

Availability is platform- and backend-dependent. Prefer error-returning APIs
and inspect `GetRuntimeCapabilities` when an operation is required for a
workflow. `GetLinuxCapabilities` provides additional compositor detail.

## Support overview

| Platform/session | Build | Current behavior |
|---|---|---|
| macOS | CGO-enabled default build | Native mouse, keyboard, capture, window, and process paths; macOS permissions still apply |
| macOS | `CGO_ENABLED=0` | Pure-Go CoreGraphics capture and display bounds with explicit Screen Recording permission diagnostics; other unavailable GUI operations return `ErrNotSupported` |
| Windows | CGO-enabled default build | Native mouse, keyboard, capture, window, and process paths |
| Linux/X11 | CGO-enabled default build | X11/XTest input, capture, window, and process paths |
| Linux/Wayland | `-tags wayland` for native protocols; add `pipewire` for persistent ScreenCast frames | Native wlroots capture/input where compositor protocols exist, one-shot Screenshot fallback, reusable ScreenCast/PipeWire capture, explicit RemoteDesktop portal sessions, capability-aware window support |
| Other supported platforms without CGO | `CGO_ENABLED=0` | Pure-Go capture works on Windows and Linux/X11; Linux/Wayland uses the Screenshot portal; explicit RemoteDesktop sessions provide limited Pure-Go Wayland input; remaining unavailable operations return `ErrNotSupported` |

Wayland compositors intentionally restrict global automation. GNOME and KDE can
use consent-aware Screenshot and RemoteDesktop portal paths. The explicit
RemoteDesktop session client is available under `input/portal`. After explicit
consent through `StartRemoteDesktopInput`, supported high-level input APIs use
that session when native virtual input is unavailable; RobotGo never opens the
dialog implicitly. Native pointer and keyboard automation requires the
compositor to expose the corresponding virtual-input protocols. See
[Wayland status](docs/wayland-tasks.md) for the detailed matrix and open work.
Persistent capture runtime evidence is tracked separately in the
[Wayland capture compatibility matrix](docs/compatibility/wayland-capture.md).

## Requirements

- Go 1.25 or newer, matching [`go.mod`](go.mod).
- A CGO-compatible C toolchain for the full native desktop-automation feature
  set. The explicitly started Linux RemoteDesktop portal subset also works in a
  non-CGO build.
- Platform development libraries for the selected backend.

### macOS

Install Go and the Xcode command-line tools:

```bash
xcode-select --install
```

Grant Accessibility and Screen Recording permissions to the application or
terminal that runs RobotGo when macOS requests them.

### Windows

Install Go and a CGO-compatible compiler such as LLVM-MinGW or MinGW-w64. The
compiler must be available on `PATH` when `go build` runs.

### Linux

The default Linux build targets X11 and requires X11/XTest development files.
On Debian/Ubuntu:

```bash
sudo apt update
sudo apt install build-essential pkg-config libx11-dev libxtst-dev
```

For the native Wayland build, install the Wayland, xkbcommon, GBM, and DRM
development files as well:

```bash
sudo apt install libwayland-dev libxkbcommon-dev wayland-protocols libgbm-dev libdrm-dev
```

Persistent ScreenCast frame capture additionally needs PipeWire development
files and the `pipewire` build tag:

```bash
sudo apt install libpipewire-0.3-dev
go build -tags "wayland pipewire" ./...
```

On non-FHS systems where PipeWire headers and libraries are outside the default
compiler paths, derive them from pkg-config before building:

```bash
export CGO_CFLAGS="$(pkg-config --cflags-only-I libpipewire-0.3)"
export CGO_LDFLAGS="$(pkg-config --libs libpipewire-0.3)"
go build -tags "wayland pipewire" ./...
```

Package names differ on other distributions. Optional runtime integrations:

- `xdg-desktop-portal` plus the matching desktop backend provides screenshot
  fallback and consent-aware RemoteDesktop input sessions.
- `xsel` or `xclip` provides clipboard access on Linux.
- `wayland-info` can provide a bounds fallback when native output geometry is
  unavailable.
- `wlrctl`, `swaymsg`, or `hyprctl` enables the compositor-specific window
  operations documented in the [Wayland status](docs/wayland-tasks.md).
- `zenity` or `kdialog` enables native-style alert dialogs on Linux.
- Tesseract is required only for the optional OCR helpers. The default helper
  invokes the `tesseract` command; `-tags ocr` selects the in-process Gosseract
  backend and additionally requires Tesseract and Leptonica development files.

`libpng` is not a direct RobotGo build requirement; PNG/JPEG image handling is
implemented through Go image packages in the current module.

## Installation

Add this fork to a Go module:

```bash
go get github.com/marang/robotgo@latest
```

Import it with the same module path:

```go
import "github.com/marang/robotgo"
```

Do not mix this import with `github.com/go-vgo/robotgo`; Go treats them as
different modules.

## Quick start

Prefer error-returning APIs in automation that must detect unsupported backends
or runtime failures:

```go
package main

import (
	"fmt"
	"log"

	"github.com/marang/robotgo"
)

func main() {
	if err := robotgo.MoveE(100, 200); err != nil {
		log.Printf("move unavailable: %v", err)
	}
	if err := robotgo.ClickE("left"); err != nil {
		log.Printf("click unavailable: %v", err)
	}

	bit, err := robotgo.CaptureScreen(0, 0, 320, 200)
	if err != nil {
		log.Fatal(err)
	}
	defer robotgo.FreeBitmap(bit)

	fmt.Println("capture backend:", robotgo.LastBackend())
}
```

Legacy APIs remain available for source compatibility. Their signatures may be
unable to report all backend failures, so new reliability-sensitive code should
use variants such as `MoveE`, `MoveRelativeE`, `ClickE`, `ScrollE`, `LocationE`,
`TypeStrE`, `UnicodeTypeE`, and the error-returning window APIs.

Low-level helpers whose signatures directly expose `C.*` types remain CGO-only.
Portable callers should use `Bitmap`, `CHex`, `Handle`, the error-returning APIs,
and the high-level capture, input, and window functions instead.

For concurrent programs, change process-wide legacy defaults atomically with
`GetRuntimeConfig` and `SetRuntimeConfig`. Direct assignments to `MouseSleep`,
`KeySleep`, `DisplayID`, `NotPid`, and `Scale` remain compatible for startup
configuration but must not race with active operations.

When converting caller-provided raw pixels, create an owned value with
`NewBitmap`. Conversion variants such as `ToRGBAGoE`, `ToCBitmapE`,
`ImgToCBitmapE`, and `ByteToCBitmapE` validate dimensions, layout, buffer size,
and decode errors; their legacy counterparts remain available for compatibility.

Potentially blocking helpers have context-aware variants: `ReadAllContext` and
`WriteAllContext` also select the regular or primary Unix clipboard explicitly;
`GetTextContext` and `GetTextImgContext` bound command-backed OCR execution and
temporary-file cleanup. With `-tags ocr`, cancellation is observed before and
after the synchronous in-process Tesseract call; that native call cannot itself
be interrupted.

## Linux display backends

### X11

The normal Linux build uses the X11 backend:

```bash
go build ./...
```

An X11 session requires `DISPLAY` and an accessible X server. RobotGo does not
silently route a Wayland-primary operation through X11 merely because Xwayland
is present.

Error-returning window APIs no longer report success for native operations that
have no implementation. In particular, the current X11 minimize/maximize path
returns `ErrNotSupported` instead of silently doing nothing.

### Wayland

Build the native Wayland paths explicitly:

```bash
go build -tags wayland ./...
```

Capture selection is:

1. Native `wlr-screencopy` using DMA-BUF when supported.
2. Native `wl_shm` when DMA-BUF is unavailable or unsuitable.
3. An already authorized persistent ScreenCast/PipeWire session, when one was
   explicitly started.
4. The freedesktop Screenshot portal when native capture fails and portal use
   is allowed.

The portal may prompt the user. Native screencopy and virtual input are most
useful on wlroots compositors; availability is probed at runtime rather than
inferred from environment variables alone.

For repeated GNOME/KDE capture, build with `-tags pipewire`, explicitly open
one consent session, then read as many frames as required without creating a
new portal request per frame:

```go
ctx := context.Background()
err := robotgo.StartScreenCastCapture(ctx, robotgo.ScreenCastCaptureOptions{
	Sources: robotgo.ScreenCastSourceMonitor,
	Cursor:  robotgo.ScreenCastCursorEmbedded,
	Persist: robotgo.ScreenCastPersistApp,
})
if err != nil {
	log.Fatal(err)
}
defer robotgo.CloseScreenCastCapture()

frame, err := robotgo.CaptureScreenCast(ctx) // image.Image
```

`CaptureScreenCast(ctx, x, y, width, height)` crops in logical compositor
coordinates and maps fractional stream scaling to physical pixels.
`ScreenCastCaptureStreams` exposes selected stream geometry and PipeWire
metadata; `ScreenCastCaptureRestoreToken` returns the newest single-use restore
token. Keep restore tokens private and replace the stored token after every
restored session. `CaptureScreen` continues to prefer native screencopy, then
reuses an active ScreenCast session on native failure. Use
`ROBOTGO_WAYLAND_BACKEND=screencast` only when the persistent session should be
mandatory.

The image capture backend supports hidden and embedded cursor modes. Raw cursor
metadata remains available to lower-level `OpenScreenCast` consumers, but
`OpenPipeWireCapture` rejects that mode explicitly because its `image.Image`
result cannot represent separate cursor metadata. Starting a capture waits for
the PipeWire stream to reach a usable state; an idle session recycles frames
without converting them until a capture is requested.

For explicit GNOME/KDE portal input, probe support without prompting and then
call `StartRemoteDesktopInput` with the required device mask. While that session
is active, relative movement, buttons/clicks, scrolling, key taps/toggles, text,
and Unicode can fall back to it when native input is unavailable. The lower-level
`input/portal` package additionally exposes relative pointer motion, smooth and
discrete axes, pointer buttons, and keycode/keysym events.
`StartRemoteDesktopInputWithOptions` can attach monitor/window/virtual
ScreenCast sources to the same consent session. Selected stream position and
logical size then let `MoveE` map global coordinates to absolute portal input;
touch down/motion/up is available when the portal grants touchscreen access.
Stream metadata includes the node ID, optional mapping ID and PipeWire serial,
and a persistence restore token without exposing that token through
diagnostics. Restore tokens are single-use: store them securely, pass the latest
value as `RemoteDesktopInputOptions.RestoreToken`, and replace it with the token
returned by the restored session. For multiple streams, the optional `displayId`
argument to `MoveE` selects the stream by its returned slice index; without it,
RobotGo uses logical stream positions. The session must be closed
deterministically.

RemoteDesktop keyboard and relative-pointer capability remains available when
only the optional ScreenCast probe is degraded. In that case `Probe` returns the
usable partial capability together with the ScreenCast error. Inspect
`Capability.ScreenCastIssue` or `RemoteDesktopInputStatus.ScreenCastReason`
before relying on absolute input, touch, or stream metadata.
Consent diagnostics distinguish `not-requested`, `granted`, `closed`,
`cancelled`, `timed-out`, `denied`, `failed`, and `unavailable`. A timeout means
the caller's consent deadline elapsed; it does not imply that the portal itself
is unavailable.

Successful portal-backed `MoveE`, `MoveRelativeE`, `ClickE`, and `ScrollE`
honor the same `MouseSleep` and scroll-delay behavior in CGO and non-CGO builds.

The example defaults to probe-only mode:

```bash
go run ./examples/remote_desktop_input
go run ./examples/remote_desktop_input -connect
go run ./examples/remote_desktop_input -connect -screen
# Read the example before using the opt-in input demo:
go run ./examples/remote_desktop_input -demo -screen
go run ./examples/remote_desktop_input -demo -touch
```

Useful capture controls:

| Variable | Values/effect |
|---|---|
| `ROBOTGO_WAYLAND_BACKEND` | `auto`, `dmabuf`, `wl_shm`, `screencast`, or `portal` |
| `ROBOTGO_FORCE_PORTAL=1` | Force screenshot portal capture |
| `ROBOTGO_DISABLE_PORTAL=1` | Disable portal prompts and fallback |
| `ROBOTGO_CAPTURE_DEBUG=1` | Log backend selection and fallback decisions |

`LastBackend` reports the backend used by the latest capture. Long-running
Wayland applications can call `CloseWaylandInput` to release persistent virtual
pointer and keyboard objects; later input calls reconnect lazily.

Successful non-CGO capture reports `BackendX11` on supported X11 systems,
`BackendPortal` on Linux/Wayland, and `BackendPureGo` on other supported
Pure-Go platforms.

Global pointer position and global foreign-window control are not universally
available in Wayland core. `LocationE` and unsupported window operations return
`ErrNotSupported`. Sway, Hyprland, and some wlroots environments have partial
window support through compositor-specific tools; inspect capabilities instead
of assuming parity with X11.

### Runtime diagnostics

`GetRuntimeBackendInfo` is platform-neutral and reports whether the current
binary contains native CGO backends or the Pure-Go compatibility build. It does
not open portals or contact a compositor:

```go
info := robotgo.GetRuntimeBackendInfo()
fmt.Println("implementation:", info.BuildImplementation)
fmt.Println("cgo:", info.CGOEnabled)
fmt.Println("platform:", info.GOOS, info.GOARCH)
fmt.Println("display:", info.DisplayServer)
```

`GetRuntimeCapabilities` adds feature-level status. It may perform bounded
runtime probes, but never opens a consent dialog:

```go
caps := robotgo.GetRuntimeCapabilities()
fmt.Println("capture:", caps.Capture.Available, caps.Capture.Backend, caps.Capture.Reason)
fmt.Println("bounds:", caps.Bounds.Available, caps.Bounds.Backend, caps.Bounds.Reason)
fmt.Println("keyboard:", caps.Keyboard.Available, caps.Keyboard.Backend, caps.Keyboard.Reason)
fmt.Println("process:", caps.Process.Available, caps.Process.Backend)
```

In a `CGO_ENABLED=0` build, `Capture`, `CaptureImg`, `CaptureScreen`,
`CaptureGo`, and `CaptureBitmapStr` use the Pure-Go CoreGraphics, Windows, or
X11 screenshot backend where available. Wayland sessions use this fork's
hardened screenshot portal and preserve `ROBOTGO_DISABLE_PORTAL`; unsupported
targets return `ErrNotSupported` explicitly. On macOS, capture returns
`ErrPermissionDenied` with remediation when Screen Recording access is absent;
capability inspection never requests that permission implicitly.

Use `CaptureImg()` with no arguments for a full-screen capture. Region capture
requires at least `x, y, width, height`; partial argument lists, non-positive
region dimensions other than the explicit `0x0` full-screen request,
coordinate overflow, and a non-zero origin combined with a
`0x0` full-screen request are rejected before a portal request is created.
Explicit regions whose 32-bit RGBA buffer would exceed 512 MiB are also
rejected before a backend allocates capture memory.

`GetLinuxCapabilities` reports the detected session, compositor, selected
feature backends, fallbacks, and unsupported reasons:

```go
caps := robotgo.GetLinuxCapabilities()
fmt.Println("display:", caps.DisplayServer)
fmt.Println("compositor:", caps.Compositor)
fmt.Println("capture:", caps.Capture.Backend, caps.Capture.Available, caps.Capture.Fallback)
fmt.Println("keyboard:", caps.Keyboard.Backend, caps.Keyboard.Available, caps.Keyboard.Reason)
fmt.Println("mouse:", caps.Mouse.Backend, caps.Mouse.Available, caps.Mouse.Reason)
fmt.Println("remote desktop:", caps.RemoteDesktop.Backend, caps.RemoteDesktop.Available, caps.RemoteDesktop.Reason)
fmt.Println("window:", caps.Window.Backend, caps.Window.Available, caps.Window.Reason)
```

Run the complete diagnostic example with:

```bash
go run -tags wayland ./examples/linux_capabilities
```

## Examples

The checked-in examples use this fork's module path and track the current API:

- [Mouse](examples/mouse/main.go)
- [Keyboard and clipboard](examples/key/main.go)
- [Screen capture and pixels](examples/screen/main.go)
- [Full-screen capture with backend reporting](examples/screen_full/main.go)
- [Linux capabilities](examples/linux_capabilities/main.go)
- [Cross-platform runtime capabilities](examples/runtime_capabilities/main.go)
- [Consent-aware RemoteDesktop portal input](examples/remote_desktop_input/main.go)
- [Persistent ScreenCast/PipeWire capture](examples/screencast_capture/main.go)
- [Window and process helpers](examples/window/main.go)
- [Display scaling](examples/scale/main.go)

Examples perform real desktop actions. Read them before running them, especially
the window/process example, which can close windows or terminate processes.

## Testing

Run the default and explicit non-CGO contracts first:

```bash
go test ./...
CGO_ENABLED=0 go test ./...
go test -race ./input/portal
```

Wayland and portal code has additional tagged suites:

```bash
go test -tags "wayland" ./...
go test -tags "portal" ./screen/portal -v
go test -tags "wayland test" ./screen -run TestScreencopy -v
go test -tags "wayland integration" . ./mouse ./window -v
```

See [TEST.md](TEST.md) for prerequisites, DRM tests, keyboard integration, and
opt-in compositor E2E checks.

Real Wayland input results are tracked in the
[versioned compatibility matrix](docs/compatibility/wayland-input.md).

## Documentation and roadmap

- [Go API reference](https://pkg.go.dev/github.com/marang/robotgo)
- [Key names and conversion](docs/keys.md)
- [Testing guide](TEST.md)
- [Current Wayland support and backlog](docs/wayland-tasks.md)
- [Product roadmap](docs/plan/product-roadmap.md)
- [Wayland implementation history](docs/wayland-history.md)

The active product priority is validating the explicit RemoteDesktop high-level
fallback on real GNOME/KDE runtimes while retaining native virtual-input paths
on wlroots compositors.

## Upstream and attribution

This fork descends from [go-vgo/robotgo](https://github.com/go-vgo/robotgo) and
preserves its history and license notices. The original RobotGo author is
[vz](https://github.com/vcaesar); upstream contributors remain credited in the
Git history and source headers. These links are intentionally upstream
attribution, not installation or support links for this fork.

Development, issues, CI, and current contributors for this fork live at:

- [marang/robotgo](https://github.com/marang/robotgo)
- [Issues](https://github.com/marang/robotgo/issues)
- [Current contributors](https://github.com/marang/robotgo/graphs/contributors)

## License

RobotGo is distributed under the [Apache License 2.0](LICENSE). Vendored or
generated components retain their applicable notices in the source tree.
