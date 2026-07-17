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
  on macOS, native APIs on Windows, and X11. Windows and Linux/X11 additionally
  have keyboard/pointer backends; Wayland capture uses the consent-aware
  Screenshot portal, and unavailable GUI operations return `ErrNotSupported`
  rather than plausible zero values.
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
| Windows | `CGO_ENABLED=0` | Pure-Go capture/bounds plus layout-aware `SendInput` keyboard/text, exact pointer movement/location, smooth movement/drag, buttons, horizontal/vertical scroll, live readiness, ownership checks, and deterministic in-process cleanup |
| Linux/X11 | CGO-enabled default build | X11/XTest input, capture, window, and process paths |
| Linux/X11 | `CGO_ENABLED=0` | Pure-Go X11 capture/bounds plus XTEST mouse, keyboard, text/Unicode, pointer-location, smooth-move/drag, vertical scroll, and live readiness probes; horizontal scroll is explicitly unsupported |
| Linux/Wayland | `-tags wayland` for native protocols; add `pipewire` for persistent ScreenCast frames | Native wlroots capture/input where compositor protocols exist, one-shot Screenshot fallback, reusable ScreenCast/PipeWire capture, explicit RemoteDesktop portal sessions, capability-aware window support |
| Other supported platforms without CGO | `CGO_ENABLED=0` | Linux/Wayland uses the Screenshot portal; explicit RemoteDesktop sessions provide limited Pure-Go Wayland input; remaining unavailable operations return `ErrNotSupported` |

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
  set. Pure-Go Linux/X11 capture/input and the explicitly started Linux
  RemoteDesktop portal subset work in a non-CGO build.
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
`KeyTap` and `KeyToggle` model keys rather than portable text entry. A selected
backend may support a single non-ASCII rune directly (the RemoteDesktop portal
and Pure-Go X11 do), while another native keymap can return `ErrNotSupported`.
Use `TypeStrE` or `UnicodeTypeE` when the intent is text input.

On native Linux and RemoteDesktop portal paths, stateful `KeyDown`/`KeyUp`
and `MouseDown`/`MouseUp` pairs are backend- and session-affine. Equivalent
key aliases such as `esc`/`escape` match the same hold. A duplicate Down, an
Up without a successful RobotGo-owned Down, or an Up after its portal session
was replaced returns `ErrInputOwnership` without sending input on another
backend. Callers can distinguish this contract with
`errors.Is(err, robotgo.ErrInputOwnership)`. Closing or retargeting a native
backend releases RobotGo-owned state; closing a portal session delegates that
release to the compositor.

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

RobotGo normally detects an X11 session through `DISPLAY` and requires an
accessible X server. Native CGO builds may instead select an explicit target
with `SetXDisplayName`, even when both display-server environment variables are
empty. `DetectDisplayServer` remains an environment-only observation; runtime
backend information and capabilities report the explicitly selected X11
target. A Wayland environment remains authoritative, so RobotGo does not route
a Wayland-primary operation through X11 merely because Xwayland is present.

Linux/X11 also supports capture, bounds, and input without a C compiler or X11
development headers:

```bash
CGO_ENABLED=0 go build ./...
```

The input backend requires a reachable X server with XTEST 2.2 or newer. It
supports the high-level mouse/keyboard error APIs, text and Unicode, smooth
movement/drag, scroll, pointer location, and live
`KeyboardReady`/`MouseReady` probes. Pure-Go X11 scroll calls are bounded to
1,000 steps per axis. XTEST input is global; process-target (`pid`) arguments
are rejected explicitly.
Single-character keys are tap-only; persistent key state requires a named key.
Persistent pointer-button toggles are limited to core X11 buttons 1–5. Pure-Go
X11 supports vertical `ScrollE`; horizontal scrolling returns
`ErrNotSupported` because core XTEST button 6/7 state is not observable safely.
`GetRuntimeCapabilities` reports the selected `pure-go-x11` backend.
Key taps without an unambiguous active mapping and text use a bounded pool of
originally unmapped X11 keycodes so delayed XKB clients still decode input
correctly. If one connection exhausts that server-dependent pool of distinct
symbols, the operation fails before injecting more input. Call
`CloseMainDisplayE` only after targets have processed all prior keyboard input
to restore the mappings, verify cleanup, and reset the pool. Place that call in
a scope whose lifetime includes any delayed target processing.

These mappings are server-global, so the Pure-Go backend owns its X11
connection in a separate, re-executed guardian process. If the application
process exits unexpectedly or receives `SIGKILL`, control-socket EOF makes the
guardian run a bounded, conditional cleanup: it releases RobotGo-owned
keys/buttons, allows up to two seconds for already delivered text events, and
restores a scratch before-image only while the current mapping still exactly
matches RobotGo's recorded final image and that keycode is neither pressed nor
a modifier. A different final image is treated as another client's state and
is preserved. X11 cannot reveal an ABA change where another client changes a
mapping and later puts back the exact same image, so that case is inherently
indistinguishable from RobotGo's ownership.

Guardian startup requires Linux procfs to expose `/proc/self/exe` and the
sandbox/service policy to permit re-executing the current program and using
Linux abstract Unix sockets. The parent accepts only the authenticated socket
peer whose kernel credentials match the helper it started; no control file
descriptor is inherited through the re-exec initialization phase. Re-exec can
still repeat dependency initializers that run before RobotGo's guardian
initializer. Those initializers must not block or terminate the helper; if they
prevent its authenticated handshake, startup fails before an X11 input
connection is exposed. Failure is explicit, the failed helper is reaped, and
there is no silent in-process X11 fallback. Crash cleanup also requires the
guardian and X server to remain alive and responsive. A
simultaneous guardian/container/host kill, X-server loss, or an X11 transport
that remains blocked beyond the cleanup deadline cannot be restored
synchronously. Request dispatch and cleanup are deadline-bounded; on a blocked
transport the guardian initiates connection close and exits, while the parent
kills and reaps a helper that misses its final exit deadline.

Explicit `CloseMainDisplayE` remains the deterministic path because it reports
actionable cleanup/transport errors and lets callers choose when even
arbitrarily delayed target clients have finished processing input. A foreign
mapping replacement is deliberately relinquished without being reported as a
cleanup failure because overwriting it would be unsafe. A later RobotGo
operation reconnects lazily. In a Wayland-primary session the backend remains
disabled, even when `DISPLAY` points to Xwayland.
If cleanup reports that a scratch keycode is pressed or became a modifier,
release or restore that external state and retry `CloseMainDisplayE`.

RobotGo briefly grabs the X server around mapping/state checks and composite
synthetic events so another X client cannot race those transactions. Core X11
cannot attribute simultaneous physical input, or another press while RobotGo
intentionally holds a key/button; state ownership in those cases remains
best-effort. Avoid mixing automation with concurrent human or synthetic input.

The native CGO X11 path never installs temporary server-global key mappings.
It types printable ASCII represented by the active keymap and preflights the
complete string before the first event, so an unmapped later character cannot
leave partial text or a held modifier. `UnicodeTypeE` follows the same
fail-closed rule for non-ASCII code points. Compound input never releases a
main key or modifier that was already held outside RobotGo. Active Shift,
Level3/Level5, and lock state is preserved and reused only when it produces the
requested character exactly; conflicting shortcut state fails before mutation.
Persistent native `KeyUp` must match a successful RobotGo `KeyDown`, and only
RobotGo-owned keycodes are released. Use the Pure-Go X11 build when full Unicode
text and its explicit scratch-mapping lifecycle are required. Native
`KeyboardReady`/`MouseReady` also verify a live XTEST 2.2 connection. Xlib
operations share a locked configured-display lifecycle; separate XGB resolver
connections use the same configured target and close deterministically.

Error-returning window APIs no longer report success for native operations that
have no implementation. In particular, the current X11 minimize/maximize path
returns `ErrNotSupported` instead of silently doing nothing. Native `GetTitleE`
also returns an explicit error when a title is empty or cannot be retrieved.

### Wayland

Build the native Wayland paths explicitly:

```bash
go build -tags wayland ./...
```

This is a Wayland-targeted build, not a dual X11/Wayland window backend. In a
pure X11 session it reports Window and Hook capabilities as unavailable;
error-returning window operations return `ErrNotSupported`. Use the default
Linux build for an X11 session.

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
and Unicode can fall back to it when native input is unavailable. Native
Wayland `TypeStrE` preflights the complete rune sequence and injects supported
text exactly. If a rune is absent from the active keymap, it produces zero
native input and safely uses an active keyboard-granted RemoteDesktop session;
without one it returns `ErrNotSupported`. Runtime seat removal or keyboard
capability changes are processed without blocking and reconnect to the next
deterministic capable seat. The lower-level `input/portal` package additionally
exposes relative pointer motion, smooth and discrete axes, pointer buttons, and
keycode/keysym events.
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
fmt.Println("mouse:", caps.Mouse.Available, caps.Mouse.Backend, caps.Mouse.Reason)
fmt.Println("process:", caps.Process.Available, caps.Process.Backend)
```

On Linux/X11 with `CGO_ENABLED=0`, capability inspection reports the selected
`pure-go-x11` keyboard and mouse backends without opening an X connection. Call
`KeyboardReady` or `MouseReady` for a live XTEST 2.2+ check before acting. A
Wayland-primary session never selects that backend merely because an Xwayland
`DISPLAY` is present.

In a `CGO_ENABLED=0` build, `Capture`, `CaptureImg`, `CaptureScreen`,
`CaptureGo`, `CaptureBitmapStr`, `GetPixelColor`, and `GetPxColor` use the
Pure-Go CoreGraphics, Windows, or X11 screenshot backend where available.
Wayland sessions use this fork's hardened screenshot portal and preserve
`ROBOTGO_DISABLE_PORTAL`; unsupported targets return `ErrNotSupported`
explicitly. On macOS, capture returns `ErrPermissionDenied` with remediation
when Screen Recording access is absent; capability inspection never requests
that permission implicitly.

Use `CaptureImg()` with no arguments for a full-screen capture. Region capture
requires at least `x, y, width, height`; partial argument lists, non-positive
region dimensions other than the explicit `0x0` full-screen request,
coordinate overflow, and a non-zero origin combined with a
`0x0` full-screen request are rejected before a portal request is created.
Explicit regions whose 32-bit RGBA buffer would exceed 512 MiB are also
rejected before a backend allocates capture memory.

On Linux, `GetPixelColor` and `GetPxColor` use the selected capture backend for
a 1x1 region. Out-of-bounds coordinates and capture failures therefore return
an error instead of being indistinguishable from a valid black pixel.

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
- [Pure-Go X11 input probe and opt-in demo](examples/purego_x11_input/main.go)
- [Pure-Go Windows input readiness and opt-in demo](examples/purego_windows_input/main.go)
- [Consent-aware RemoteDesktop portal input](examples/remote_desktop_input/main.go)
- [Persistent ScreenCast/PipeWire capture](examples/screencast_capture/main.go)
- [Window and process helpers](examples/window/main.go)
- [Display scaling](examples/scale/main.go)

Examples perform real desktop actions. Read them before running them, especially
the window/process example, which can close windows or terminate processes.
The Pure-Go X11 example is safe to run as capability inspection by default; it
performs global input only when both `-act` and an explicit action are supplied:

```bash
CGO_ENABLED=0 go run ./examples/purego_x11_input
CGO_ENABLED=0 go run ./examples/purego_x11_input -act -move 100,100
CGO_ENABLED=0 go run ./examples/purego_x11_input -act -key enter
CGO_ENABLED=0 go run ./examples/purego_x11_input -act -text "Hello"
```

Keyboard actions keep scratch mappings alive for two seconds before verified
cleanup. Increase `-settle` when the focused XKB client may process input later.

On Windows, the Pure-Go example performs readiness checks only unless `-move`
or `-text` is supplied:

```powershell
$env:CGO_ENABLED = "0"
go run ./examples/purego_windows_input
go run ./examples/purego_windows_input -move 400,300 -text "Hello"
```

`SendInput` is subject to Windows User Interface Privilege Isolation: a
normal-integrity process cannot inject input into a higher-integrity target.
Persistent `KeyDown`/`MouseDown` state is owned by the backend and released by
`CloseMainDisplayE`; callers should still use balanced operations or `defer`
cleanup because process termination cannot run in-process cleanup.

## Testing

Run the default and explicit non-CGO contracts first:

```bash
go test ./...
CGO_ENABLED=0 go test ./...
go test -race ./input/portal
```

Linux X11 input has non-skipping Xvfb/XTEST CI checks. The deep Pure-Go suite
uses `us,de` layouts; a separate job applies the same public behavioral
contract and benchmark smoke to native CGO and Pure-Go binaries. It also proves
that native readiness rejects a reachable X server with XTEST disabled. Missing
X11 runtime support fails instead of skipping; see
[the testing guide](TEST.md#x11integration-native-and-pure-go-x11-input) for
the exact commands and prerequisites. The crash proof additionally inspects
`/proc/<pid>/task/<tid>/children` under a Linux child subreaper to verify that
the reported guardian is the exact child that exits and is reaped. The
[current decision-grade comparison](docs/performance/data/x11-2026-07-17-fd97f7e/summary.md)
measures the guardian path and retains native CGO as the X11 default while
Pure-Go remains the supported CGO-disabled backend. The earlier direct-path
sample remains linked from the performance report as historical evidence.
The stable remote checks are required by `main` branch protection. Real
GNOME/KDE/wlroots jobs remain opt-in until matching runners are registered.

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
- [X11 native-vs-Pure-Go evidence](docs/performance/x11-native-vs-purego.md)
- [Current Wayland support and backlog](docs/wayland-tasks.md)
- [Product roadmap](docs/plan/product-roadmap.md)
- [Wayland implementation history](docs/wayland-history.md)

The active product slice remains selective Phase 3 Pure-Go expansion. The
Linux/X11 evaluation is complete: shared behavior is blocking CI,
current guardian-path decision evidence is versioned, and native CGO remains
the default while Pure-Go supports CGO-disabled builds. The Pure-Go X11 core is
race-testable and its separate guardian performs bounded, claim-checked cleanup
after an application-process crash. Its request transport now reuses bounded
state and avoids double payload encoding, with versioned evidence showing lower
allocation cost. Balanced transient press/release pairs now share one guardian
request while preserving per-step crash-cleanup ownership and the existing
preflight/server-grab policy. Required remote checks now protect `main`.
Pure-Go Windows input is the next delivered platform slice, with hermetic
transaction tests and an opt-in real input-desktop probe. Further macOS and
Windows backends remain selective work. Real GNOME/KDE/wlroots validation is an
independent Wayland release gate.

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
