# Runtime Compatibility Matrix v1

Matrix version: **1**
Published: **2026-07-18**

This matrix separates implemented behavior from blocking runtime evidence.
`supported` means the applicable build and behavioral contract is blocking in
CI. `implemented / evidence pending` means the backend exists and has hermetic
coverage, but a required real desktop or permission-granted runner is not yet
available. A pending row is not a passing row.

## Platform and build matrix

| Platform/session | Build mode | Status | Blocking evidence and limits |
|---|---|---|---|
| Linux/X11 | Native CGO | supported | Default Linux tests, race/vet/lint, ASan/LeakSanitizer ownership gate, Xvfb/XTEST public contract, XTEST-disabled negative contract |
| Linux/X11 | Pure Go | supported | Non-CGO Linux tests plus non-skipping Xvfb/XTEST input, capture, bounds, cleanup, and crash-guardian contracts |
| Linux/Wayland/wlroots | Native CGO, `wayland` | supported for advertised protocols | Weston integration plus hermetic screencopy, virtual input, bounds, scale, transform, fallback, and manifest-checked ASan/LeakSanitizer ownership tests; compositor-specific protocols remain capability-gated |
| Linux/Wayland | Pure Go | supported for portal APIs | Non-CGO portal capture/input contracts; shared capture helpers refuse implicit Xwayland, while non-prompting display bounds return explicit unsupported errors; user consent and portal backend availability remain runtime requirements |
| GNOME/Wayland | CGO and Pure Go portal paths | implemented / evidence pending | Protected GNOME runner required for RemoteDesktop and persistent ScreenCast evidence |
| KDE Plasma/Wayland | CGO and Pure Go portal paths | implemented / evidence pending | Protected KDE runner required for RemoteDesktop and persistent ScreenCast evidence |
| macOS | Native CGO | supported | Hosted macOS build/test; Screen Recording and Accessibility are runtime permissions |
| macOS | Pure Go | supported, granted-input/window evidence pending | CoreGraphics capture/bounds/scale, Quartz input, and Accessibility window contracts; real symbol and non-prompting permission preflights are blocking; permission-granted input and self-owned-window mutations remain opt-in |
| Windows | Native CGO | supported | Hosted Windows build/test |
| Windows | Pure Go | supported | Hosted non-CGO tests plus real input-desktop pointer/pixel and self-owned window/control evidence |

## Architecture evidence

| Architecture | Status | Scope |
|---|---|---|
| `amd64` | supported | Primary Linux/X11/Wayland, Windows, and available hosted-platform evidence |
| `arm64` | Pure-Go cross-build evidenced; native runtime support not broadly claimed | Go and non-CGO implementations are architecture-neutral; no dedicated protected Linux Wayland ARM runner is claimed |
| Other Go architectures | compile/support not claimed by v1 | Add explicit cross-build and runtime evidence before promotion |

GitHub-hosted runner architecture is not pinned by this repository. A release
must record the concrete runner architecture from its workflow evidence rather
than inferring it from an operating-system label.

## Optional dependencies

| Dependency or service | Enables | Behavior when absent |
|---|---|---|
| Wayland client, XKB, GBM/DRM development libraries | Native Wayland build | Build tag is unavailable; Pure-Go portal paths remain possible |
| `xdg-desktop-portal` plus desktop backend | Screenshot, RemoteDesktop, ScreenCast | Capability is unavailable with remediation; RobotGo does not pretend success |
| PipeWire development/runtime libraries and `pipewire` tag | Persistent ScreenCast frames | One-shot Screenshot/native screencopy remain eligible; persistent capture reports unsupported |
| X11 server and EWMH window manager | Pure-Go X11 window introspection/control | Read-only operations report X11 access/property errors; mutations return explicit unsupported errors without a consistent manager that advertises the operation |
| X11/XTEST | Native or Pure-Go X11 input | Readiness and diagnostics report missing/old XTEST explicitly |
| Tesseract/Leptonica and `ocr` tag | OCR helpers | Core automation remains available without OCR |
| Sway/Hyprland/wlroots command tools | Compositor-specific foreign-window operations; Hyprland mutations detect `hyprlang` versus 0.55+ Lua dispatch | Wayland-core capability reports unsupported operations explicitly |

## Diagnostic contract

`GetRuntimeDiagnostics` returns schema version `1` with:

- stable feature ordering and selected backend/fallback state;
- negotiated Wayland, portal, and XTEST protocol versions when observable;
- non-prompting permission/consent state;
- actionable remediation for unavailable features;
- platform, architecture, build implementation, display-server type, and
  compositor family without display addresses, restore tokens, stream IDs, or
  unrelated environment values.

Run `go run ./examples/runtime_diagnostics` to print the JSON report. Schema
changes that rename/remove fields or alter their meaning require a new matrix
and diagnostic schema version.

Detailed real-compositor evidence remains in
[Wayland input](wayland-input.md) and [Wayland capture](wayland-capture.md).
Exact source, build identity, test-log digests, and the embedded sanitized
runtime report for published releases are defined by
[Release Evidence v1](release-evidence-v1.md).
