# Wayland Input Compatibility

This matrix records runtime evidence for RobotGo input backends. A row is only
marked `pass` after the corresponding real-compositor workflow or a documented
local runtime test completed. Hermetic unit coverage alone is not a runtime
pass.

| Date | Desktop/compositor | Input backend | Build | Result | Evidence |
|---|---|---|---|---|---|
| 2026-07-11 | Sway (wlroots) | Native virtual keyboard/pointer | `linux,wayland,integration` | pass | Local `go test -tags "wayland integration" . ./mouse ./window -v`; keyboard/pointer round trips and Sway capability integration passed |
| 2026-07-11 | Sway (wlroots) | RemoteDesktop portal | CGO/default | unavailable, actionable | Local portal exposes ScreenCast v4/source mask 3 but no `org.freedesktop.portal.RemoteDesktop`; diagnostics return an explicit unavailable error |
| pending | GNOME | RemoteDesktop + ScreenCast mapping | Pure-Go portal client | no runner | Requires self-hosted runner label `gnome` |
| pending | KDE Plasma | RemoteDesktop + ScreenCast mapping | Pure-Go portal client | no runner | Requires self-hosted runner label `kde` |
| pending | wlroots portal backend | RemoteDesktop + ScreenCast mapping | Pure-Go portal client | no runner | Requires a backend implementing RemoteDesktop and runner label `wlroots` |

## Evidence workflow

`.github/workflows/remote-desktop-e2e.yml` validates the CGO-independent pure-Go
portal client once per desktop:

The harness calls the lower-level portal session methods directly, ensuring a
native wlroots input backend cannot satisfy the checks instead of RemoteDesktop.

- RemoteDesktop and ScreenCast capability discovery
- explicit consent and granted keyboard/pointer devices
- relative and absolute pointer movement
- stream geometry and node mapping
- modifier-only keyboard injection
- touchscreen down/up when advertised
- deterministic close

The workflow uploads sanitized environment, portal-version, and test logs. Set
the repository variable `ROBOTGO_REMOTE_DESKTOP_E2E=1` only after protected,
ephemeral GNOME/KDE/wlroots runners are registered. Fork pull requests are
excluded from self-hosted execution.
