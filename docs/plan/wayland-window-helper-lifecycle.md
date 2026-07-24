# Wayland Window Helper Lifecycle Plan

Status: Initial delivery implemented by LAB-22

Linear coordination:

- Team: `Lab` (`LAB`)
- Project: [`RobotGo | P008 | Wayland Window Helper Lifecycle`](https://linear.app/riotbox/project/robotgo-or-p008-or-wayland-window-helper-lifecycle-e009305b14f5)
- Project ID: `89db5b55-8795-413a-9b43-653be5d6bbc3`
- Initial issue: [`LAB-22`](https://linear.app/riotbox/issue/LAB-22/harden-wayland-window-helper-command-lifecycle)

## Outcome

Make one-shot Sway, Hyprland, and generic wlroots window helpers uniformly
bounded and prove that failure cannot leave a helper or its descendants
running. Public APIs retain their signatures and compositor-specific support
contracts.

## Contract

- Every compositor window command uses one internal two-second bounded runner.
- Unix commands retain the shared process-group ownership and 250-millisecond
  inherited-I/O cleanup bound from `internal/command`.
- Deadline and cleanup causes remain discoverable through backend error
  wrapping with `errors.Is`.
- Invalid internal timeout values fail before starting a command.
- Timeout and inherited-I/O failures never become successful window
  operations.

## Privacy and ownership

Lifecycle tests create private shell helpers and PID records only under
`t.TempDir()`. They never contact a compositor, inspect a desktop, capture
pixels, or persist command output. Test cleanup kills the recorded process
group and child defensively on every exit path; temporary files are removed by
the test framework.

## Validation

Hermetic Linux/CGO tests exercise a blocked helper with a spawned descendant
and a successful parent whose descendant retains stdout. They verify bounded
return, exact timeout/`exec.ErrWaitDelay` causes, and the absence of surviving
processes. Cross-platform unit tests cover the production deadline, normal
output, invalid bounds, and cause preservation through Sway, Hyprland, and
wlroots backend errors.

No runnable example is added because this project changes internal ownership
and error fidelity rather than adding a user-invoked operation. Existing
window examples continue to exercise the unchanged public API.
