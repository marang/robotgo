# Wayland Capture Compatibility

This matrix records evidence for the native screencopy, one-shot Screenshot
portal, and persistent ScreenCast/PipeWire paths. A pending runner is not a
passing result.

| Date | Desktop | Backend | Build | Result | Evidence |
|---|---|---|---|---|---|
| 2026-07-14 | Hermetic Linux | Native screencopy geometry | `cgo,wayland,test` | pass | Negative/positive output origins, clipped and overflowing regions, fractional scaling, all eight output transforms, enclosing-edge crop semantics |
| 2026-07-11 | Hermetic Linux | ScreenCast/PipeWire | `cgo,pipewire` | pass | Session/request cleanup, FD duplication, repeated consumer lifecycle, crop/fractional scaling, eight transforms, pixel buffer validation, race and lint gates |
| pending | GNOME | ScreenCast/PipeWire | `cgo,pipewire,integration` | no runner | Protected runner label `gnome`; workflow artifact `screencast-gnome` |
| pending | KDE Plasma | ScreenCast/PipeWire | `cgo,pipewire,integration` | no runner | Protected runner label `kde`; workflow artifact `screencast-kde` |

The protected GNOME/KDE matrix is defined in
`.github/workflows/screencast-e2e.yml`. It captures two frames through one
consent session and uploads only the schema-v1 evidence manifest, canonical
sanitized test log, and summary. Sway/wlroots native capture and explicit portal
availability are separate P005 evidence lanes; a wlroots environment is not
counted as a ScreenCast portal pass unless a compatible portal backend is
independently preflighted and promoted in a future workflow change.
