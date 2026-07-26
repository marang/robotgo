# Protected KDE Portal Runner

This directory defines RobotGo's disposable KDE Plasma Wayland guest for real
RemoteDesktop and ScreenCast tests. It runs through nested QEMU/KVM on a fresh
GitHub-hosted Ubuntu runner and never uses a developer workstation or registers
a self-hosted Actions runner.

The host builds an immutable, digest-addressed Ubuntu image from
`manifest.json`. Every run boots a private copy-on-write overlay, starts a real
Plasma Wayland session with `xdg-desktop-portal-kde`, transfers only
`git archive` output for the exact clean commit, runs one portal test cell, and
destroys all transient guest state afterward.

The guest receives no checkout credential, Actions token, `.git` directory, or
untracked host file. Captured frames, input data, portal restore tokens, SSH
keys, QMP sockets, and raw logs never enter the immutable image or uploaded
artifacts.

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

Use `-cell screencast` for persistent PipeWire capture.

The host transfers the exact clean tree without credentials and enforces an
active nftables output chain with `policy drop` before the transfer. A private
host-side controller asks the immutable guest's accessibility bridge only for
the physical output card and disabled confirmation-control geometry. It then
performs both actions through QMP's private virtual pointer. No accessible
names, screen pixels, or dialog content leave the guest. RobotGo does not
approve its own request, patch the portal backend, or pre-authorize the
application.

## Cleanup

Success, failure, denial, timeout, cancellation, and interrupt terminate the VM
and test process groups and remove the complete sentinel-owned `run-*`
directory. The workflow independently rejects leftover run directories.
Immutable build cache content is non-sensitive and remains digest-bound.

Do not run `hosted-run` on a personal desktop to reproduce CI. Dispatch the
GitHub workflow or use a fresh remote VM with nested KVM.
