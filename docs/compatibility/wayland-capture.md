# Wayland Capture Compatibility

This matrix records evidence for the native screencopy, one-shot Screenshot
portal, and persistent ScreenCast/PipeWire paths. A pending runner is not a
passing result.

| Date | Desktop | Backend | Build | Result | Evidence |
|---|---|---|---|---|---|
| 2026-07-14 | Hermetic Linux | Native screencopy geometry | `cgo,wayland,test` | pass | Negative/positive output origins, clipped and overflowing regions, fractional scaling, all eight output transforms, enclosing-edge crop semantics |
| 2026-07-11 | Hermetic Linux | ScreenCast/PipeWire | `cgo,pipewire` | pass | Session/request cleanup, FD duplication, repeated consumer lifecycle, crop/fractional scaling, eight transforms, pixel buffer validation, race and lint gates |
| 2026-07-21 | Sway 1.9, nested headless Ubuntu 24.04 | Native wl_shm screencopy | `cgo,wayland,swayintegration` | pass | [`Sway E2E` run 29857289675](https://github.com/marang/robotgo/actions/runs/29857289675), `native-capture`, artifact `sway-native-capture`; exact synthetic color and 1280x720 geometry remained in memory; no image artifact |
| 2026-07-21 | Sway 1.9, nested headless Ubuntu 24.04 | Native logical output geometry | `cgo,wayland,swayintegration` | pass | [`Sway E2E` run 29861058126](https://github.com/marang/robotgo/actions/runs/29861058126), `native-output-multi`, artifact `sway-native-output-multi`; exact per-output and aggregate bounds with negative origin, scale 2, and transform 90; no pixels captured |
| 2026-07-26 | GNOME 46, nested Ubuntu 24.04 | ScreenCast/PipeWire | `cgo,pipewire,integration` | pass, single output | [`ScreenCast E2E` run 30199195890](https://github.com/marang/robotgo/actions/runs/30199195890), exact commit `ac3c6683817b4740219becba9b5242eed4fed7b7`; two non-empty captures from one real consent session, including unchanged-frame reuse, close, and transient-artifact rejection; no frame/log artifact retained |
| 2026-07-26 | KDE Plasma 5.27, nested Ubuntu 24.04 | ScreenCast/PipeWire | `cgo,pipewire,integration` | pass, single output | [`ScreenCast E2E` run 30214614314](https://github.com/marang/robotgo/actions/runs/30214614314), exact commit `f01c2f731a7814bc71bd6fc30524cd5d314477d7`; real consent and PipeWire session, two owned non-empty captures, deterministic close, and transient-artifact rejection; no frame/log artifact retained |

The GNOME/KDE matrix is defined in `.github/workflows/screencast-e2e.yml`.
Both desktops run in credential-free nested guests, capture two frames through
one consent session, and retain no pixels or raw logs. Sway/wlroots native
capture and explicit portal availability run separately in
`.github/workflows/sway-e2e.yml`; a wlroots environment is not counted as a
ScreenCast portal pass unless a compatible portal backend is independently
preflighted and promoted in a future workflow.
