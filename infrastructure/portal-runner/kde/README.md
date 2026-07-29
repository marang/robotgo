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
within the current bounded phase; the helper never restarts either service. A
failure returns one allowlisted stage; raw journals, process arguments,
environment values, and other session details remain inside the disposable
guest. The nominal KDE phase deadlines total 220 seconds; a 250-second host
guard caps them plus bounded probe overhead beneath the 270-second systemd
startup limit.

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
