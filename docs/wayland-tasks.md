Wayland Support — Remaining Tasks

Current implementation baseline:

- Display server detection (`x11`/`wayland`) is in place.
- Mouse and key input have Wayland backends.
- Screen capture prefers Wayland screencopy and falls back to portal.
- `ROBOTGO_FORCE_PORTAL=1` is supported for forcing portal capture.
- `ROBOTGO_DISABLE_PORTAL=1` disables portal prompts/fallback for deterministic native-only operation.
- `ROBOTGO_WAYLAND_BACKEND` (`auto|dmabuf|wl_shm|screencast|portal`) is supported.
- `ROBOTGO_CAPTURE_DEBUG=1` logs backend selection/fallback details.
- Linux runtime capability introspection is available via `GetLinuxCapabilities`,
  including hook/event capability status.
- Platform-neutral build introspection is available via
  `GetRuntimeBackendInfo`, including CGO/Pure-Go mode and display-server
  detection without portal or compositor probes.
- Platform-neutral feature introspection is available via
  `GetRuntimeCapabilities`; macOS non-CGO builds report CoreGraphics capture,
  display bounds, real Retina scale, Screen Recording permission, and Quartz
  keyboard/pointer Accessibility state without prompting.
- macOS non-CGO builds provide a Pure-Go Quartz input backend with key
  taps/combinations, owned holds, process targeting, exact UTF-16 text,
  plus absolute, relative and smooth movement, drag, click/double-click, owned
  button toggles, horizontal/vertical scroll, pointer location, deterministic
  cleanup, and explicit Accessibility denial. Media/brightness keys without
  stable Quartz keycodes remain explicitly unsupported.
- Linux/X11 non-CGO builds provide a Pure-Go XGB/XTEST keyboard and pointer
  backend with live readiness probes, text/Unicode, smooth movement/drag,
  scrolling, pointer location, explicit state errors, and deterministic
  cleanup/reconnect. It is selected only for X11-primary sessions and never as
  an implicit Xwayland fallback from Wayland.
- Windows non-CGO builds provide foreground-layout-aware keyboard/text and pointer input
  through user32, including exact Unicode, smooth movement/drag, horizontal and
  vertical scroll, ownership checks, partial-injection rollback, live
  readiness, deterministic in-process cleanup, clipboard-assisted paste, and
  pixel-at-pointer queries. Win32 DPI scale is reported without re-scaling
  capture bounds that are already in physical pixel coordinates. The legacy
  `Drag` wrapper composes real Pure-Go input and releases its button after move
  failures instead of silently doing nothing.
- Non-CGO `CaptureImg`/`CaptureScreen`, their Go-bitmap/string variants, and
  pixel-color queries use the hardened screenshot portal on Wayland and the
  Pure-Go screenshot backend on X11/Windows; unsupported targets fail explicitly.
- Capability introspection probes the live screencopy protocol, desktop portal
  D-Bus owner, virtual pointer and virtual keyboard instead of inferring support
  solely from session environment variables.
- Error-returning mouse and typing variants are available (`MoveE`,
  `MoveRelativeE`, `ClickE`, `ScrollE`, `LocationE`, `TypeStrE`,
  `UnicodeTypeE`) while legacy APIs remain source-compatible.
- Native screencopy has bounded event dispatch, deterministic FD/resource
  cleanup and hermetic regression coverage for compositor stalls and DMABUF failures.
- Fallback output bounds use short success/failure TTLs and can be refreshed
  explicitly with `InvalidateScreenBoundsCache`, so hotplug and scale changes
  do not remain cached for the lifetime of the process.
- Native aggregate/per-output bounds preserve logical negative origins,
  fractional `xdg-output` sizes, core-output scale, and all transforms.
  Deterministic output indices are shared with screencopy, Wayland display
  count/main-index queries avoid X11, and the `wayland-info` fallback accepts
  output records without numeric identifiers.
- An explicitly started ScreenCast session provides reusable PipeWire frames
  behind the `pipewire` build tag. It preserves stream geometry/serial metadata,
  supports logical region crop with fractional scaling, converts negotiated raw
  RGB/BGR formats to RGBA, applies SPA crop/transform metadata, and closes
  PipeWire before the portal session.
- Window-control APIs now expose explicit Wayland NotSupported errors via
  error-returning variants (`SetActiveE`, `MinWindowE`, `MaxWindowE`,
  `CloseWindowE`, `GetTitleE`, `IsTopMostE`, `IsMinimizedE`,
  `IsMaximizedE`, `SetTopMostE`).
- Wayland window backend resolver is layered:
  - compositor-specific (`sway`, `hyprland`)
  - wlroots family generic backend (`wlroots-generic`)
  - wayland-core explicit unsupported fallback
- `wlroots-generic` currently implements active-window minimize/maximize
  (state=true) when `wlrctl` is available; unsupported operations remain
  explicit.
- `hyprland` uses the compositor's reported fullscreen mode for
  `IsMaximizedE` and supports both maximize and restore through `hyprctl`.
  Fullscreen is not reported as maximized. Sway and generic wlroots keep
  returning `ErrNotSupported` because their available IPC does not expose a
  trustworthy equivalent state.
- Bitmap string helpers are available via `CaptureBitmapStr`,
  `FindBitmapStr`, `BitmapFromStr`, and `ToStrBitmap`.
- Region/tolerance color search is available via `FindColorCS`/`FindcolorCS`.
- Bitmap-string and color-search contracts run in CGO and non-CGO builds.
  Hermetic backend tests cover native screencopy, Screenshot portal, and
  Pure-Go dispatch. `FindColorCS` returns absolute coordinates, defaults to
  `0.01` tolerance, validates the `0..1` range, and preserves backend errors.
- Explicit RemoteDesktop portal sessions are available through `input/portal`:
  live capability probing, consent/session lifecycle, pointer/keyboard notify
  methods, cancellation, denial, timeout, and teardown are covered hermetically.
- `StartRemoteDesktopInput` activates an explicit high-level fallback for
  relative movement, buttons/clicks, scroll, key taps/toggles, text, and Unicode;
  the consent dialog is never opened implicitly.
- `StartRemoteDesktopInputWithOptions` attaches ScreenCast sources to the same
  session for stream-aware absolute pointer and touchscreen input. Stream
    geometry, mapping ID, PipeWire serial, persistence token availability, and
    permission status (including timeout versus cancellation) are observable
    without exposing token contents. Portal mouse delays have CGO/non-CGO parity.
- Runtime integration tests cover backend capability selection for
  `sway`/`hyprland`/`wlroots-generic` with explicit skip behavior when runtime
  preconditions are not present.
- wlroots min/max E2E test exists behind opt-in env flag:
  `ROBOTGO_WLROOTS_MINMAX_E2E=1`.
- Hyprland maximize query/set/restore E2E coverage exists behind
  `ROBOTGO_HYPRLAND_MAXIMIZE_E2E=1` and restores the initial supported state.

Window backend support matrix (current):

| Backend | Resolver trigger | Active title | Active close | Minimize | Maximize/restore | `IsMaximizedE` |
|---|---|---|---|---|---|---|
| `sway` | `SWAYSOCK` or sway desktop/session | Yes (`swaymsg`) | Yes (`swaymsg kill`) | `wlrctl`, set only | `wlrctl`, set only | No (`ErrNotSupported`) |
| `hyprland` | `HYPRLAND_INSTANCE_SIGNATURE` or hyprland desktop/session | Yes (`hyprctl activewindow -j`) | Yes (`hyprctl dispatch killactive`) | `wlrctl`, set only | Yes (`hyprctl`, set and restore) | Yes (`hyprctl activewindow -j`) |
| `wlroots-generic` | wlroots-family compositor (wayfire/river/labwc/dwl/gamescope) | No (`ErrNotSupported`) | No (`ErrNotSupported`) | `wlrctl`, set only | `wlrctl`, set only | No (`ErrNotSupported`) |
| `wayland-core/*` | wayland session without specific/family backend support | No (`ErrNotSupported`) | No (`ErrNotSupported`) | No (`ErrNotSupported`) | No (`ErrNotSupported`) | No (`ErrNotSupported`) |

PID/handle-specific control remains unsupported for all listed Wayland
backends.

- Priority Backlog (1-7):
  - 1. Register protected GNOME/KDE/wlroots runners and validate the complete
    RemoteDesktop high-level matrix in hermetic CGO and non-CGO builds. Shared ScreenCast
    mapping, absolute pointer/touch, consent, denial, cancellation, timeout,
    restore metadata, teardown, and high-level dispatch have hermetic coverage.
    `[new vs robotgo-pro]`
  - 2. Validate the reusable ScreenCast/PipeWire backend on protected real
    GNOME/KDE/wlroots runners and promote its leak/timeout tests to release
    gates. The implementation and opt-in integration harness are present.
    `[new vs robotgo-pro]`
  - 3. Continue window state/query operations beyond the delivered Hyprland
    maximize slice where a compositor exposes equally trustworthy state.
  - 4. Wayland reliability matrix across wlroots/GNOME/KDE: multi-output,
    fractional scale, transforms, portal consent, and fallback.
    `[new vs robotgo-pro]`
  - 5. Selectively port useful Pure-Go backends with behavioral parity tests,
    backend introspection, and benchmarks before changing defaults. Linux/X11
    input is implemented; deep Pure-Go coverage and a shared native/Pure-Go
    behavioral contract run in non-skipping Xvfb/XTEST CI. A balanced benchmark
    smoke is report-only. Native X11 now has atomic input preflight, one shared
    configured-display lifecycle/target, live XTEST readiness, and an XTEST-disabled negative
    contract. Current optimized guardian-path evidence retains native CGO as the
    X11 default while keeping Pure-Go supported for CGO-disabled builds. The
    lower-allocation request transport and balanced transient-input sequencing
    preserve the crash contract. Stable checks now protect `main`; evaluating
    further backends remains open. Pure-Go Windows also provides Win32 window
    introspection/control, backed by a blocking self-owned runtime-window test
    for PID/handle resolution, geometry, state, activation, topmost, and close.
    The Pure-Go X11 core is now
    race-testable, and a separate X11 guardian uses an authenticated abstract
    Unix socket with kernel-verified peer credentials, bounded request dispatch,
    and deadline-bounded cleanup after an application-process `SIGKILL`. It
    releases owned input and restores only an exact unchanged, unpressed,
    non-modifier scratch claim. Foreign final images are preserved; exact-image
    ABA replacement is not observable in X11.
  - 6. Keep the published runtime compatibility matrix and diagnostic schema
    versioned. Schema v1 now reports negotiated protocol versions, permission
    state, and actionable remediation without sensitive session data.
  - 7. Promote race/vet and native leak/sanitizer checks to blocking release
    gates, with dedicated GNOME/KDE/wlroots jobs.

- Recently completed parity work:
  - Window state/query APIs expose `IsTopMostE`, `IsMinimizedE`,
    `IsMaximizedE`, and `SetTopMostE` with explicit Wayland errors.
  - Bitmap string helpers are implemented via `CaptureBitmapStr`,
    `FindBitmapStr`, `BitmapFromStr`, and `ToStrBitmap`.
  - Region/tolerance color search is implemented via
    `FindColorCS`/`FindcolorCS`.
  - `GetLinuxCapabilities` reports hook/event status explicitly.

- Screen Capture:
  - A non-skipping hermetic CI matrix now covers positive/negative output origins,
    fractional scaling, clipped/overflowing regions, and all eight transforms
    across native screencopy and PipeWire mapping.
  - Validate the same scale/transform and region-crop behavior on real wlroots,
    GNOME, and KDE sessions.
  - Complete real-compositor multi-output selection and bounds evidence using
    `xdg-output`.
- Portal Path:
  - Expand troubleshooting for xdg-desktop-portal backend selection and consent prompts.
  - Validate the existing high-level RemoteDesktop input fallback on GNOME/KDE.
  - Validate the persistent ScreenCast/PipeWire stream path and repeated-frame
    behavior across GNOME/KDE/wlroots portal backends.
- Keyboard Input:
  - Add native Wayland Unicode typing via xkbcommon compose/keysyms.
  - Verify native Wayland modifier synchronization and layout handling under
    various layouts.
- Window APIs:
  - Extend compositor-backed move/resize/activate/topmost/minimize/title
    behavior while preserving explicit `ErrNotSupported` elsewhere.
  - Validate the delivered `GetBounds`/`GetClient` `xdg-output` multi-output
    and fractional-scale contract on protected wlroots/GNOME/KDE runners.
  - Add resource‑leak checks for Wayland window helpers.
- API Parity Follow-up:
  - Extend compositor-backed implementations for existing topmost/min/max
    status APIs where Wayland protocols or helpers make the state observable.
  - Extend bitmap string helper coverage beyond current in-memory string
    and hermetic capture-backend contracts where real protected desktop
    evidence is needed; file open/save helpers remain separate.
  - Keep the delivered `FindColorCS` compatibility contract covered on real
    protected platform capture runners.
  - Keep any new APIs consistent with Wayland semantics:
    - Return explicit `NotSupported` where protocol/compositor limitations apply.
- Build/Tooling:
  - Document pkg-config deps and `wayland-scanner` generation steps; ensure protocol headers are vendored.
  - Keep build tags consistent (`linux,wayland` for native capture and `linux,portal` for explicit portal package path).
  - Non-CGO builds keep explicit unsupported stubs for unavailable operations
    while supported capture, portal, and Linux/X11 input paths report their real
    capabilities. Harden additional Pure-Go backends selectively before
    enabling any of them as defaults.
- CI/Testing:
  - Keep the current headless Weston, screencopy, portal, and non-CGO CI
    commands green; eliminate unexpected hermetic skips. Stable jobs are
    required by `main` branch protection.
  - Keep the non-CGO Xvfb/XTEST input test on a configured `us,de` keymap in
    Linux CI; missing `DISPLAY` or XTEST must fail the matrix leg rather than
    skip. Keep the shared native/Pure-Go contract and report-only benchmark
    smoke green, including the native XTEST-disabled negative contract and
    display-lifecycle stress; the resulting stable remote checks are required
    by `main` branch protection.
  - Keep the extracted Pure-Go X11 core in the blocking race job and its
    guardian-backed application-`SIGKILL` recovery in the non-skipping Xvfb
    manifest; guardian/host/X-server loss and X11 transport stalls beyond the
    cleanup deadline remain outside that scoped guarantee.
  - Provision the existing dedicated GNOME, KDE, and wlroots runtime workflow.
  - Keep the race/vet CI jobs green and protect them; add leak/sanitizer and
    bounds-across-outputs gates.
- Examples/Docs:
  - Add backend selection flags in examples (dmabuf, wl_shm, portal).
  - Keep `examples/purego_x11_input` side-effect-free by default; live
    readiness checks and global input must remain behind its explicit `-act`
    flag.
  - Keep `examples/purego_windows_input` readiness-only by default; global
    pointer, keyboard, clipboard, or capture actions require explicit `-move`,
    `-text`, `-paste`, or `-color` flags.
  - Keep the explicitly gated real Windows input-desktop pointer probe blocking
    in the non-CGO Windows CI leg; it must restore the original cursor position.
  - Publish a versioned support matrix and troubleshooting guide.
