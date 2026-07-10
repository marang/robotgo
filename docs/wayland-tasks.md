Wayland Support — Remaining Tasks

Current implementation baseline:

- Display server detection (`x11`/`wayland`) is in place.
- Mouse and key input have Wayland backends.
- Screen capture prefers Wayland screencopy and falls back to portal.
- `ROBOTGO_FORCE_PORTAL=1` is supported for forcing portal capture.
- `ROBOTGO_WAYLAND_BACKEND` (`auto|dmabuf|wl_shm|portal`) is supported.
- `ROBOTGO_CAPTURE_DEBUG=1` logs backend selection/fallback details.
- Linux runtime capability introspection is available via `GetLinuxCapabilities`,
  including hook/event capability status.
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
  - 1. Full Wayland screencast portal backend (PipeWire session stream), not only screenshot fallback. `[new vs robotgo-pro]`
  - 2. Window state/query API parity with explicit errors: partial.
    - `IsTopMost`, `IsMinimized`, `IsMaximized`, `SetTopMost` now expose
      explicit error-returning variants where Linux/Wayland state is unsupported.
  - 3. Bitmap serialization API parity: implemented.
    - `CaptureBitmapStr`, `FindBitmapStr`, `BitmapFromStr`, `ToStrBitmap`.
  - 4. Region/tolerance color search parity: implemented.
    - `FindColorCS`/`FindcolorCS` with platform-neutral bitmap scanning.
  - 5. Wayland reliability matrix tests across wlroots/GNOME/KDE:
    - multi-output, fractional scale, transforms, portal consent/fallback. `[new vs robotgo-pro]`
  - 6. Hook/event support status for Wayland: partial.
    - `GetLinuxCapabilities` now reports hook/event status as explicitly
      unsupported under Wayland compositor policy.
  - 7. Expand capability introspection: partial.
    - capture, bounds, keyboard, mouse, window, hook, and event capability
      fields are present; finer troubleshooting metadata remains follow-up.

- Screen Capture:
  - Validate on wlroots, GNOME and KDE.
  - Handle fractional scaling and output transforms.
  - Add multi-output selection using `xdg-output`; choose target output.
  - Region-crop correctness tests across backends.
- Portal Path:
  - Expand troubleshooting for xdg-desktop-portal backend selection and consent prompts.
  - Decide whether to add a full screencast/PipeWire stream path in addition to current screenshot fallback.
- Keyboard Input:
  - Add Unicode typing via xkbcommon compose/keysyms.
  - Verify modifier synchronization and layout handling under various layouts.
- Window APIs:
  - Define/return NotSupported semantics for move/resize/activate/topmost/minimize/title.
  - Make `GetBounds`/`GetClient` robust via `xdg-output` (multi-output + fractional scaling).
  - Add resource‑leak checks for Wayland window helpers.
- API Parity Backlog:
  - Extend compositor-backed implementations for existing topmost/min/max
    status APIs where Wayland protocols or helpers make the state observable:
  - Extend bitmap string helper coverage beyond current in-memory string
    helpers where needed (file open/save helpers remain separate).
  - Keep `FindColorCS` compatibility behavior covered across platform capture
    backends.
  - Keep any new APIs consistent with Wayland semantics:
    - Return explicit `NotSupported` where protocol/compositor limitations apply.
- Build/Tooling:
  - Document pkg-config deps and `wayland-scanner` generation steps; ensure protocol headers are vendored.
  - Keep build tags consistent (`linux,wayland` for native capture and `linux,portal` for explicit portal package path).
- CI/Testing:
  - Stabilize CI jobs with headless Weston for screencopy (dmabuf + wl_shm) and portal.
  - Tests for dmabuf vs wl_shm selection; bounds across outputs.
- Examples/Docs:
  - Add backend selection flags in examples (dmabuf, wl_shm, portal).
  - Provide a support matrix and troubleshooting guide.
