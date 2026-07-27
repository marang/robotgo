# Wayland Input Compatibility

This matrix records runtime evidence for RobotGo input backends. A row is only
marked `pass` after the corresponding real-compositor workflow or a documented
local runtime test completed. Hermetic unit coverage alone is not a runtime
pass.

| Date | Desktop/compositor | Input backend | Build | Result | Evidence |
|---|---|---|---|---|---|
| 2026-07-11 | Sway (wlroots) | Native virtual keyboard/pointer | `linux,wayland,integration` | pass | Local `go test -tags "wayland integration" . ./mouse ./window -v`; keyboard/pointer round trips and Sway capability integration passed |
| 2026-07-11 | Sway (wlroots) | RemoteDesktop portal | CGO/default | unavailable, actionable | Local portal exposes ScreenCast v4/source mask 3 but no `org.freedesktop.portal.RemoteDesktop`; diagnostics return an explicit unavailable error |
| 2026-07-21 | Sway 1.9, nested headless Ubuntu 24.04 | Native virtual keyboard/pointer | `cgo,wayland,swayintegration` | pass | [`Sway E2E` run 29857289675](https://github.com/marang/robotgo/actions/runs/29857289675), `native-input`, artifact `sway-native-input`; self-owned `wev` target, no physical devices or host desktop |
| 2026-07-21 | Sway 1.9, nested headless Ubuntu 24.04 | Native logical output mapping used by absolute input | `cgo,wayland,swayintegration` | pass | [`Sway E2E` run 29861058126](https://github.com/marang/robotgo/actions/runs/29861058126), `native-output-multi`, artifact `sway-native-output-multi`; exact two-output negative-origin, scale-2, transform-90 topology |
| 2026-07-26 | GNOME 46, nested Ubuntu 24.04 | RemoteDesktop + ScreenCast mapping | Pure-Go portal client | pass, single output | [`RemoteDesktop E2E` run 30199452053](https://github.com/marang/robotgo/actions/runs/30199452053), exact commit `ac3c6683817b4740219becba9b5242eed4fed7b7`; real portal consent, relative/absolute pointer, modifier key, optional touch, session close, and transient-artifact rejection |
| 2026-07-26 | KDE Plasma 5.27, nested Ubuntu 24.04 | RemoteDesktop + ScreenCast mapping | Pure-Go portal client | pass, single output | [`RemoteDesktop E2E` run 30204553569](https://github.com/marang/robotgo/actions/runs/30204553569), exact commit `5bae59335b82bb3ca0d22c7e39a029273b3f3ed8`; real portal backend, relative/absolute pointer, modifier key, optional touch, stream mapping, deterministic close, and transient-artifact rejection |
| 2026-07-26 | GNOME 46, nested Ubuntu 24.04 | RemoteDesktop + two ScreenCast monitor mappings | Pure-Go portal client | pass, multi-output | [`RemoteDesktop E2E` run 30220551561](https://github.com/marang/robotgo/actions/runs/30220551561), exact commit `3ab8a785e3662af341c629377dccbdcaed3a69d6`; canonical 1280x720 + 1024x768 topology, two unique physical streams, relative and per-output absolute pointer input, modifier key, deterministic close, and transient-artifact rejection |
| 2026-07-26 | KDE Plasma 5.27, nested Ubuntu 24.04 | RemoteDesktop + two ScreenCast monitor mappings | Pure-Go portal client | pass, multi-output | [`RemoteDesktop E2E` run 30220551561](https://github.com/marang/robotgo/actions/runs/30220551561), exact commit `3ab8a785e3662af341c629377dccbdcaed3a69d6`; canonical two-output topology, two unique physical streams with version-aware optional metadata, relative/per-output absolute pointer input, deterministic close, and transient-artifact rejection |

## Evidence workflow

`.github/workflows/remote-desktop-e2e.yml` validates the CGO-independent pure-Go
portal client in credential-free nested GNOME and KDE guests:

The harness calls the lower-level portal session methods directly, ensuring a
native input backend cannot satisfy the checks instead of RemoteDesktop.

- RemoteDesktop and ScreenCast capability discovery
- explicit consent and granted keyboard/pointer devices
- relative and absolute pointer movement
- exact stream count plus version-aware geometry and node mapping
- modifier-only keyboard injection
- touchscreen down/up when advertised
- deterministic close

The GNOME and KDE jobs retain no desktop artifact or raw log. They run
automatically on trusted `main` pushes and can be manually dispatched; fork and
ordinary pull-request events do not boot either guest. Manual dispatch selects
`gnome`, `kde`, or `all`.
Sway/wlroots native input and explicit portal-unavailability evidence use
separate P005 lanes; they are not counted as RemoteDesktop portal passes.

`.github/workflows/sway-e2e.yml` independently validates the native Sway input
path and explicit portal availability in an isolated GitHub-hosted compositor.
It uploads only schema-v1 sanitized evidence and never grants the job access to
real input devices or a host desktop.
