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
  display bounds, and Screen Recording permission state without prompting.
- Non-CGO `CaptureImg`/`CaptureScreen` and their Go-bitmap/string variants use
  the hardened screenshot portal on Wayland and the Pure-Go screenshot backend
  on X11/Windows; unsupported targets fail explicitly.
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
- Bitmap string helpers are available via `CaptureBitmapStr`,
  `FindBitmapStr`, `BitmapFromStr`, and `ToStrBitmap`.
- Region/tolerance color search is available via `FindColorCS`/`FindcolorCS`.
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

Window backend support matrix (current):

| Backend | Resolver trigger | Active title | Active close | PID/handle close | Activate/min/max |
|---|---|---|---|---|---|
| `sway` | `SWAYSOCK` or sway desktop/session | Yes (`swaymsg`) | Yes (`swaymsg kill`) | No (`ErrNotSupported`) | Yes (`wlrctl window minimize/maximize state:active`, state=true only) |
| `hyprland` | `HYPRLAND_INSTANCE_SIGNATURE` or hyprland desktop/session | Yes (`hyprctl activewindow -j`) | Yes (`hyprctl dispatch killactive`) | No (`ErrNotSupported`) | Yes (`wlrctl window minimize/maximize state:active`, state=true only) |
| `wlroots-generic` | wlroots-family compositor (wayfire/river/labwc/dwl/gamescope) | No (`ErrNotSupported`) | No (`ErrNotSupported`) | No (`ErrNotSupported`) | Yes (`wlrctl window minimize/maximize state:active`, state=true only) |
| `wayland-core/*` | wayland session without specific/family backend support | No (`ErrNotSupported`) | No (`ErrNotSupported`) | No (`ErrNotSupported`) | No (`ErrNotSupported`) |

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
  - 3. Extend window state/query operations from explicit error parity to real
    compositor-backed behavior where state is observable.
  - 4. Wayland reliability matrix across wlroots/GNOME/KDE: multi-output,
    fractional scale, transforms, portal consent, and fallback.
    `[new vs robotgo-pro]`
  - 5. Selectively port useful Pure-Go backends with behavioral parity tests,
    backend introspection, and benchmarks before changing defaults.
  - 6. Publish versioned compatibility data and expand diagnostics with
    protocol versions, permissions, and actionable remediation.
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
  - The blocking hermetic matrix now covers positive/negative output origins,
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
  - Add Unicode typing via xkbcommon compose/keysyms.
  - Verify modifier synchronization and layout handling under various layouts.
- Window APIs:
  - Extend compositor-backed move/resize/activate/topmost/minimize/title
    behavior while preserving explicit `ErrNotSupported` elsewhere.
  - Make `GetBounds`/`GetClient` robust via `xdg-output` (multi-output + fractional scaling).
  - Add resource‑leak checks for Wayland window helpers.
- API Parity Follow-up:
  - Extend compositor-backed implementations for existing topmost/min/max
    status APIs where Wayland protocols or helpers make the state observable.
  - Extend bitmap string helper coverage beyond current in-memory string
    helpers where needed; file open/save helpers remain separate.
  - Keep `FindColorCS` compatibility behavior covered across platform capture
    backends.
  - Keep any new APIs consistent with Wayland semantics:
    - Return explicit `NotSupported` where protocol/compositor limitations apply.
- Build/Tooling:
  - Document pkg-config deps and `wayland-scanner` generation steps; ensure protocol headers are vendored.
  - Keep build tags consistent (`linux,wayland` for native capture and `linux,portal` for explicit portal package path).
  - Non-CGO builds compile with explicit unsupported stubs; selectively port
    and harden upstream Pure-Go backends before enabling them as defaults.
- CI/Testing:
  - Keep the current headless Weston, screencopy, portal, and non-CGO jobs
    blocking.
  - Provision the existing dedicated GNOME, KDE, and wlroots runtime workflow.
  - Keep the new blocking race/vet jobs green; add leak/sanitizer and
    bounds-across-outputs gates.
- Examples/Docs:
  - Add backend selection flags in examples (dmabuf, wl_shm, portal).
  - Publish a versioned support matrix and troubleshooting guide.
