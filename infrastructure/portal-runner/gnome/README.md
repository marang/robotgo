# Protected GNOME Portal Runner

This directory defines RobotGo's disposable GNOME Wayland guest for real
RemoteDesktop and ScreenCast tests. The primary execution path is nested
QEMU/KVM on a fresh GitHub-hosted Ubuntu runner; it does not use a developer
workstation or register a self-hosted Actions runner.

The host builds an immutable, digest-addressed Ubuntu image from
`manifest.json`. Every hosted run boots a private copy-on-write overlay, starts
a real GNOME Wayland session, transfers only `git archive` output for the exact
clean commit, runs one test cell, and destroys the guest state afterward. The
guest receives no checkout credential, Actions token, `.git` directory, or
untracked host file. Captured frames, input data, portal restore tokens, SSH
keys, and raw logs never enter the immutable image or uploaded artifacts.

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

Use `-cell screencast` for the persistent PipeWire cell.

`build` verifies every downloaded artifact against the pinned SHA-256 digest.
The image identity also covers the manifest, provisioning implementation, guest
scripts, their executable modes, and content. A changed input therefore cannot
silently reuse an older image.

`hosted-run` refuses a dirty or different checkout, creates a bounded source
archive for the exact lowercase commit, and starts the test as the unprivileged
`robotgo` guest user. Immediately before requesting portal consent, the test
creates a private non-sensitive readiness marker. A separate host-side QMP
client then sends GNOME's real dialog mnemonics through the VM's virtual
keyboard. RobotGo does not patch or auto-approve the portal backend and is not
the consent-input actor.

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
