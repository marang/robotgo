# GitHub-Hosted KDE Portal Runner

This directory defines RobotGo's disposable KDE Plasma Wayland guest for real
RemoteDesktop, ScreenCast, and read-only display-bounds tests. It runs through
nested QEMU/KVM on a fresh GitHub-hosted Ubuntu runner and never uses a
developer workstation or registers a self-hosted Actions runner.

The host builds an immutable, digest-addressed Ubuntu image from
`manifest.json`. Every run boots a private copy-on-write overlay, starts a real
Plasma Wayland session with `xdg-desktop-portal-kde`, transfers only
`git archive` output for the exact clean commit, runs one portal test cell, and
destroys all transient guest state afterward.

The guest receives no checkout credential, Actions token, `.git` directory, or
untracked host file. Captured frames, input data, portal restore tokens, SSH
keys, QMP sockets, and raw logs never enter the immutable image or uploaded
artifacts.

Session startup is fail-closed and phase-bounded. SDDM/runtime/Wayland/KWin,
Plasma Shell, the portal frontend, and the KDE backend receive separate
budgets, so a slow prerequisite cannot silently consume the shell's wait time.
The runner starts only after the complete contract remains ready for three
consecutive probes. A naturally managed shell or portal bounce may settle only
within the current bounded phase. If the verified Plasma Shell user unit
reaches terminal `failed`, the helper may reset its failed/start-limit state
under a three-second hard bound, queue exactly one restart under a three-second
hard bound, wait at most 30 seconds for the unit and process, and emit one
allowlisted recovery marker. Winner and observers continuously revalidate the
base session throughout settlement. Observers retain a 60-second guard that
covers the winner's settlement and bounded result-property probes. A second
failure is terminal. Concurrent
waiters distinguish attempt, completion, reset, queue, start, and generic
failures, so every waiter receives the full post-restart settle budget. A
recovery during final stability repeats both portal phases before stability can
pass. Recovery and readiness claims share a 15-second-bounded guest lock. Every
ready claim
revalidates the complete contract inside that lock, preventing a restart from
racing a successful readiness decision. A failed locked revalidation is
classified without resetting the phase budgets. Raw journals, process
arguments, environment values, and other session details remain inside the
disposable guest. A failed shell restart publishes only an allowlisted systemd
result category and, for an exit or signal, a numeric status from 0 through
255. Free-form unit output never crosses the guest boundary. Claims and outcome
markers use the mode-0700
tmpfiles-managed `/run/robotgo-session-state`, not the user runtime directory,
so runtime-directory failures remain publishable and all state disappears with
the guest. Normal phase deadlines total 220 seconds; the latest possible
recovery adds at most 112 seconds for reset, restart queue, shell settlement,
safe result classification, and a complete portal/stability cycle. The host
guard sends `TERM` after 380
seconds and enforces `KILL` five seconds later, capping both paths plus probe
overhead beneath the 400-second systemd startup limit.

## Hosted proof

The workflows invoke the equivalent of:

```bash
state_root="$RUNNER_TEMP/robotgo-kde-portal-runner"
manifest=infrastructure/portal-runner/kde/manifest.json
go run ./internal/cmd/portalrunner validate \
  -manifest "$manifest" \
  -repository-root "$GITHUB_WORKSPACE" -state-root "$state_root"
go run ./internal/cmd/portalrunner build \
  -manifest "$manifest" \
  -repository-root "$GITHUB_WORKSPACE" -state-root "$state_root"
go run ./internal/cmd/portalrunner hosted-run \
  -manifest "$manifest" \
  -repository-root "$GITHUB_WORKSPACE" -state-root "$state_root" \
  -commit "$GITHUB_SHA" -cell remote-desktop
```

Use `-cell screencast` for persistent PipeWire capture. The separate
`display-bounds` cell requires `-topology multi-output`; it runs native-CGO and
Pure-Go public output APIs with `DISPLAY` unset and creates no portal session.

The host transfers the exact clean tree without credentials and enforces an
active nftables output chain with `policy drop` before the transfer. A private
host-side controller asks KWin only for virtual-screen and active-dialog
geometry through a short-lived private D-Bus receiver. The host selects the
manifest-declared physical monitor cards with the VM's virtual wheel and
pointer, asks KWin only for the resulting cursor position to prove QMP reached
the first target, and then uses the portal's standard Return path. No window
names, accessibility data, screen pixels, or dialog content leave the guest.
RobotGo does not approve its own request, patch the portal backend, or
pre-authorize the application.

## Cleanup

Success, failure, denial, timeout, cancellation, and interrupt terminate the VM
and test process groups and remove the complete sentinel-owned `run-*`
directory. The workflow independently rejects leftover run directories.
Immutable build cache content is non-sensitive and remains digest-bound.

Do not run `hosted-run` on a personal desktop to reproduce CI. Dispatch the
GitHub workflow or use a fresh remote VM with nested KVM.
