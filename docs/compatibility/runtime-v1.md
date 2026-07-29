# Runtime Compatibility Matrix v1

Matrix version: **1**
Published: **2026-07-29**

This matrix separates implemented behavior from blocking runtime evidence.
`supported` means every operation in that deliberately bounded row maps to the
named exact check in the release-evidence manifest. `implemented / evidence
pending` means the backend and its denial/preflight contracts exist, but the
missing real runtime proof excludes that row from RC and stable support
claims. A pending row is not a passing row.

The machine-readable source is
[`runtime-v1.json`](runtime-v1.json). The generated table below is checked by
the default suite, and every evidence name must exist in the exact 29-check
[`v1.0.0-rc.1` Release Evidence run 30442843617](https://github.com/marang/robotgo/actions/runs/30442843617)
for tagged commit `281d8cee29d696e334fe9d4a6f6a7069ab291083`.

## Platform and build matrix

<!-- BEGIN GENERATED RUNTIME SUPPORT MATRIX -->
| Contract ID | Platform/session | Build mode | Scope | Status | Evidence and limits |
|---|---|---|---|---|---|
| `linux-x11-native` | Linux/X11 | Native CGO | X11 capture and bounds, keyboard/pointer input, and X11/EWMH window and process helpers | supported | Blocking checks: `api-compat`, `race`, `sanitizer`, `test (ubuntu-latest)`, `x11-backend-evidence`, `x11-default-suite`. Input requires XTEST; EWMH mutations require a window manager that advertises the operation. |
| `linux-x11-purego` | Linux/X11 | Pure Go | X11 capture and bounds, XTEST input, and X11/EWMH window inspection and control | supported | Blocking checks: `api-compat`, `nocgo (ubuntu-latest)`, `x11-backend-evidence`. Horizontal scroll and mutations without a consistent EWMH manager return explicit unsupported errors. |
| `linux-wayland-wlroots-native` | Linux/Wayland/wlroots | Native CGO with `wayland` | Advertised virtual input, screencopy, logical output geometry, and the evidenced Sway active-window subset | supported | Blocking checks: `api-compat`, `native-capture`, `native-input`, `native-output`, `native-output-multi`, `native-window`, `portal-availability`, `wayland-integration`. Protocol and compositor capabilities are detected at runtime; Wayland core and unadvertised operations fail explicitly. |
| `linux-wayland-hyprland-native` | Hyprland/Wayland | Native CGO with `wayland` | Active-window title, PID, exact bounds, maximize query/set/restore, and legacy wrapper parity | supported | Blocking checks: `Hyprland window evidence / hyprland-window`, `api-compat`. Portable handle/client and PID-target operations remain explicitly unsupported; broader foreign-window control is not claimed. |
| `linux-wayland-purego` | Linux/Wayland | Pure Go | Consent-aware one-shot Screenshot capture and RemoteDesktop input, plus bounded native logical output enumeration | supported | Blocking checks: `Display bounds evidence / GitHub-hosted gnome multi-output Wayland bounds`, `Display bounds evidence / GitHub-hosted kde multi-output Wayland bounds`, `Portal input evidence / GitHub-hosted gnome multi-output portal input`, `Portal input evidence / GitHub-hosted kde multi-output portal input`, `api-compat`, `nocgo (ubuntu-latest)`, `portal-availability`, `wayland-integration`. Capture and input require explicit portal consent and an available desktop portal; persistent ScreenCast frames require a Linux CGO build with the `pipewire` tag, and implicit Xwayland is refused. |
| `gnome-wayland-portal` | GNOME/Wayland | Shared Go portal input, CGO with `pipewire` for ScreenCast, and native CGO/Pure-Go bounds | Multi-output RemoteDesktop input, CGO-only persistent ScreenCast capture, and logical display bounds | supported | Blocking checks: `Display bounds evidence / GitHub-hosted gnome multi-output Wayland bounds`, `Portal capture evidence / GitHub-hosted gnome multi-output persistent capture`, `Portal input evidence / GitHub-hosted gnome multi-output portal input`, `api-compat`. Consent remains interactive by desktop policy; compositor-wide foreign-window control is not claimed. |
| `kde-wayland-portal` | KDE Plasma/Wayland | Shared Go portal input, CGO with `pipewire` for ScreenCast, and native CGO/Pure-Go bounds | Multi-output RemoteDesktop input, CGO-only persistent ScreenCast capture, and logical display bounds | supported | Blocking checks: `Display bounds evidence / GitHub-hosted kde multi-output Wayland bounds`, `Portal capture evidence / GitHub-hosted kde multi-output persistent capture`, `Portal input evidence / GitHub-hosted kde multi-output portal input`, `api-compat`. Consent remains interactive by desktop policy; compositor-wide foreign-window control is not claimed. |
| `macos-native-consent-free` | macOS | Native CGO | Default build, public API, display metadata, and non-prompting permission/error contracts | supported | Blocking checks: `api-compat`, `test (macOS-latest)`. This supported scope does not include operations that require Screen Recording or Accessibility grants. |
| `macos-native-permission-granted` | macOS | Native CGO | Screen capture plus permission-granted keyboard/pointer and Accessibility window operations | implemented / evidence pending | Current implementation/preflight checks: `api-compat`, `test (macOS-latest)`. Missing for promotion: repeatable permission-granted execution against an isolated macOS desktop and fixture after a LAB-69 reactivation condition becomes available. Follow-up: [tracking issue](https://linear.app/riotbox/issue/LAB-69/add-permission-granted-self-owned-macos-runtime-evidence). Implemented APIs remain available with explicit permission diagnostics, but this scope is not an RC or stable support claim. |
| `macos-purego-consent-free` | macOS | Pure Go | CoreGraphics bounds and Retina scale, runtime symbol resolution, and non-prompting Screen Recording/Accessibility diagnostics | supported | Blocking checks: `api-compat`, `nocgo (macOS-latest)`. The supported scope is observation/preflight only and does not post input, capture private pixels, or mutate windows. |
| `macos-purego-permission-granted` | macOS | Pure Go | CoreGraphics pixel capture, Quartz input mutation, and Accessibility window inspection/control | implemented / evidence pending | Current implementation/preflight checks: `api-compat`, `nocgo (macOS-latest)`. Missing for promotion: repeatable permission-granted execution against an isolated macOS desktop and fixture after a LAB-69 reactivation condition becomes available. Follow-up: [tracking issue](https://linear.app/riotbox/issue/LAB-69/add-permission-granted-self-owned-macos-runtime-evidence). Hermetic implementation and denial contracts are blocking, but permission-granted behavior is not an RC or stable support claim. |
| `windows-native` | Windows | Native CGO | Default native build, public API, pointer behavior, and platform desktop/window helpers covered by the default suite | supported | Blocking checks: `api-compat`, `test (windows-latest)`. Desktop policy and session isolation can still deny automation and are reported as runtime errors. |
| `windows-purego` | Windows | Pure Go | Capture, SendInput keyboard/pointer, clipboard-assisted paste, and self-owned Win32 window inspection/control | supported | Blocking checks: `api-compat`, `nocgo (windows-latest)`. The blocking runner uses an input desktop and self-owned window; unrelated user windows and private content are never used. |
<!-- END GENERATED RUNTIME SUPPORT MATRIX -->

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
