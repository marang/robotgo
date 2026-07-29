# GitHub-Hosted GNOME Portal Runner

This directory defines RobotGo's disposable GNOME Wayland guest for real
RemoteDesktop, ScreenCast, and read-only display-bounds tests. The primary
execution path is nested
QEMU/KVM on a fresh GitHub-hosted Ubuntu runner; it does not use a developer
workstation or register a self-hosted Actions runner.

The host builds an immutable, digest-addressed Ubuntu image from
`manifest.json`. Every hosted run boots a private copy-on-write overlay, starts
a real Ubuntu GNOME Wayland session, transfers only `git archive` output for
the exact clean commit, runs one test cell, and destroys the guest state
afterward. The guest receives no checkout credential, Actions token, `.git`
directory, or untracked host file. Captured frames, input data, portal restore
tokens, SSH keys, and raw logs never enter the immutable image or uploaded
artifacts.

## Host requirements

The hosted path requires:

- hardware virtualization through `/dev/kvm`
- headless QEMU with `bochs-display`, xHCI, USB keyboard, USB tablet, and QMP
- `qemu-img`, `cloud-localds`, OpenSSH client tools, and Git
- at least the CPU, memory, and temporary-disk capacity declared by the
  manifest

GitHub-hosted jobs place the private state root under `$RUNNER_TEMP`. It must
remain outside the repository, owned by the current user, and inaccessible to
group or other users. SSH and QMP listen only on private host paths or loopback;
the CONNECT proxy is allowlisted and the guest rejects direct egress.

## Build and hosted proof

The workflows perform the equivalent of:

```bash
state_root="$RUNNER_TEMP/robotgo-portal-runner"
go run ./internal/cmd/portalrunner validate \
  -repository-root "$GITHUB_WORKSPACE" -state-root "$state_root"
go run ./internal/cmd/portalrunner build \
  -repository-root "$GITHUB_WORKSPACE" -state-root "$state_root"
go run ./internal/cmd/portalrunner hosted-run \
  -repository-root "$GITHUB_WORKSPACE" -state-root "$state_root" \
  -commit "$GITHUB_SHA" -cell remote-desktop
```

Use `-cell screencast` for the persistent PipeWire cell. The separate
`display-bounds` cell requires `-topology multi-output`; it runs native-CGO and
Pure-Go public output APIs with `DISPLAY` unset and creates no portal session.

`build` verifies every downloaded artifact against the pinned SHA-256 digest.
The package contract uses the focused `ubuntu-session` package, not
`ubuntu-desktop-minimal`: this retains the Ubuntu Shell mode and portal-dialog
keyboard behavior exercised by the consent controller without installing the
broader desktop application closure. The disposable `robotgo` account selects
that session through its root-owned AccountsService record. Its system DConf
profile fixes the virtual keyboard to the US XKB source used by the QMP consent
driver.
The image identity also covers the manifest, provisioning implementation, guest
scripts, their executable modes, and content. A changed input therefore cannot
silently reuse an older image.

`hosted-run` refuses a dirty or different checkout, creates a bounded source
archive for the exact lowercase commit, and starts the test as the unprivileged
`robotgo` guest user. For portal cells, the test creates a private
non-sensitive readiness marker after every non-modal negotiation request has
completed. On GNOME the client stores the exact random pending `Start` request
path in a private target file, then blocks on a private start gate. The guest
controller validates the target, creates that gate, and waits only for that
dialog-producing request; older exported objects are irrelevant. A separate
host-side QMP
client sends GNOME's real dialog mnemonics through the VM's virtual keyboard for
RemoteDesktop. For the pinned two-output ScreenCast
dialog, it selects both manifest-declared monitor buttons through the VM's
virtual pointer. The target coordinates are derived from the validated output
manifest and pinned dialog contract. Before QMP input, the host waits for the
GNOME portal backend to export the new transient `Start` request object that
immediately precedes dialog creation. The earlier `CreateSession` and
`Select*` requests have completed before the marker exists. The exact target and
object tree stay in guest memory and only a fixed readiness result may cross
SSH. For parentless dialogs, QMP first
clicks the neutral center of the pinned headerbar so the subsequent documented
mnemonics reach that dialog; multi-output ScreenCast gains focus through its
first physical-output card click. No pixels, titles, accessibility data, or
window contents leave the guest. RobotGo does not patch or auto-approve the
portal backend and is not the consent-input actor. The display-bounds cell
never creates the marker or enters this consent path.

Build and runtime logs, serial output, SSH keys, cloud-init inputs, seed disks,
and overlays remain inside a private per-run directory. The command removes
that directory on success, failure, timeout, cancellation, and interrupt. Only
the immutable base image, its build metadata, and the non-sensitive state lock
persist.

## Diagnostic commands

`probe` remains available as an infrastructure-only session diagnostic.
`run` is the older visible, operator-driven self-hosted path and requires GTK,
`gh`, runner-administration access, and exact manual `READY` input. Neither
command is used by the hosted GNOME workflows.

## Failure and cleanup

Cancellation, a VM exit, a failed test, a timeout, or rejected consent is a
failed run. The supervisor terminates the complete QEMU/test process groups,
closes the proxy and QMP connection, and deletes the sentinel-owned run
directory. The integration tests also register cleanup for their consent
marker, and the host verifies its removal before reporting success. Cleanup
failure is joined into the command result.

The workflow's final `always()` check independently rejects any transient
`run-*` directory. Raw logs are private diagnostics and must not be uploaded or
pasted.

Do not run `hosted-run` on a personal desktop merely to reproduce CI. Use a
fresh remote VM with nested KVM or dispatch the GitHub workflow.
