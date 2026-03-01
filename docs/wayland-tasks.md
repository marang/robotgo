Wayland Support — Remaining Tasks

Current implementation baseline:

- Display server detection (`x11`/`wayland`) is in place.
- Mouse and key input have Wayland backends.
- Screen capture prefers Wayland screencopy and falls back to portal.
- `ROBOTGO_FORCE_PORTAL=1` is supported for forcing portal capture.
- `ROBOTGO_WAYLAND_BACKEND` (`auto|dmabuf|wl_shm|portal`) is supported.
- `ROBOTGO_CAPTURE_DEBUG=1` logs backend selection/fallback details.
- Window-control APIs now expose explicit Wayland NotSupported errors via
  error-returning variants (`SetActiveE`, `MinWindowE`, `MaxWindowE`,
  `CloseWindowE`, `GetTitleE`).

- Priority Backlog (1-7):
  - 1. Full Wayland screencast portal backend (PipeWire session stream), not only screenshot fallback. `[new vs robotgo-pro]`
  - 2. Window state/query API parity with explicit errors:
    - `IsTopMost`, `IsMinimized`, `IsMaximized`, `SetTopMost`. `[parity]`
  - 3. Bitmap serialization API parity:
    - `CaptureBitmapStr`, `FindBitmapStr`, `BitmapFromStr`, `ToStrBitmap`. `[parity]`
  - 4. Region/tolerance color search parity:
    - `FindColorCS` with consistent behavior across platforms. `[parity]`
  - 5. Wayland reliability matrix tests across wlroots/GNOME/KDE:
    - multi-output, fractional scale, transforms, portal consent/fallback. `[new vs robotgo-pro]`
  - 6. Hook/event support status for Wayland:
    - capability detection and explicit unsupported contract where needed. `[new vs robotgo-pro]`
  - 7. Capability introspection API:
    - expose runtime support per backend/feature (for example `GetCapabilities`). `[new vs robotgo-pro]`

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
  - Add explicit topmost/min/max status APIs where missing:
    - `IsTopMost`, `SetTopMost`, `IsMinimized`, `IsMaximized`
  - Evaluate/implement bitmap string helper parity where missing:
    - `CaptureBitmapStr`, `FindBitmapStr`, `BitmapFromStr`, `ToStrBitmap`
  - Evaluate/implement region color-search parity where missing:
    - `FindColorCS` surface and compatibility behavior
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
