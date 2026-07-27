# Explicit Window Identity Plan

Status: Initial delivery implemented by LAB-21

Linear coordination:

- Team: `Lab` (`LAB`)
- Project: [`RobotGo | P007 | Explicit Window Identity`](https://linear.app/riotbox/project/robotgo-or-p007-or-explicit-window-identity-78b4d2482f79)
- Project ID: `83b19143-14a5-40f9-bacc-096b1eee8b12`
- Initial issue: [`LAB-21`](https://linear.app/riotbox/issue/LAB-21/add-explicit-active-window-and-pid-identity-apis)

## Outcome

Make active-window identity truthful across native, Pure-Go, X11, and Wayland
backends. New error-returning APIs expose unsupported, permission, and backend
failures; existing wrappers remain source-compatible and return their
historical zero values on failure.

## Contract

- `GetActiveE` returns a native active-window handle only when the selected
  backend provides a stable handle.
- `GetPidE` returns a validated positive process ID for the active window.
- `GetActive` and `GetPid` delegate to their error-returning equivalents.
- CGO Wayland sessions use the compositor-aware window resolver and never call
  X11-only identity helpers.
- Sway and Hyprland may report the active PID from compositor metadata.
- Wayland core and generic wlroots keep active PID lookup unsupported when no
  trustworthy identity source exists.
- Wayland backends do not synthesize a cross-compositor window handle.

## Compatibility boundaries

The work does not add PID/handle targeting to compositor operations and does
not change the meaning of native handles on X11, Windows, or macOS. Missing,
zero, negative, malformed, or transport-failed compositor identity is an error,
not a plausible PID.

## Validation

Hermetic tests cover Sway and Hyprland success, missing or invalid PID,
malformed output, command failure, unavailable helpers, and unsupported
Wayland handles. Existing protected Sway and opt-in Sway/Hyprland runtime tests
also exercise active PID lookup. Cross-build tests preserve native and Pure-Go
behavior.

Protected Hyprland geometry evidence remains separate infrastructure work.
Hosted GNOME/KDE portal runners are delivered, but their portal evidence does
not prove compositor window identity.
