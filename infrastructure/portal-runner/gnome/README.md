# Protected GNOME Portal Runner

This directory defines RobotGo's disposable GNOME Wayland runner for real
RemoteDesktop and ScreenCast evidence. It is infrastructure for an
operator-approved test run, not a general-purpose self-hosted runner.

The host builds an immutable, digest-addressed Ubuntu image from
`manifest.json`. Every protected run boots a private copy-on-write overlay,
starts a real GNOME Wayland session, registers one ephemeral GitHub Actions
runner for one exact workflow attempt, and destroys the guest state afterward.
The runner never accepts fork code, never reuses a desktop session, and never
persists captured frames, input data, portal restore tokens, credentials, or
job logs in its base image.

## Host requirements

Use a trusted Linux host with:

- hardware virtualization through `/dev/kvm`
- QEMU with GTK, `virtio-vga`, xHCI, USB keyboard, and USB tablet support
- `qemu-img`, `cloud-localds`, OpenSSH client tools, and `gh`
- an authenticated `gh` session with repository runner-administration access
- enough private local storage for the pinned base image and a 40 GiB virtual
  disk

On Arch Linux, the visible runtime requires `qemu-desktop`; `qemu-base` alone
does not provide the GTK and virtual-GPU devices. Do not expose the QEMU
console, SSH forwarding, or CONNECT proxy outside the local host.

The default private state root is
`~/.local/share/robotgo-portal-runner`. It must be outside the repository,
owned by the current user, and inaccessible to group or other users.

## Build and local proof

Run these commands from a clean checkout of the exact branch to be tested:

```bash
go run ./internal/cmd/portalrunner validate
go run ./internal/cmd/portalrunner build
go run ./internal/cmd/portalrunner probe
```

`build` verifies every downloaded artifact against the pinned SHA-256 digest.
The image identity also covers the manifest, provisioning implementation, guest
scripts, their executable modes, and content. A changed input therefore cannot
silently reuse an older image.

`probe` boots a disposable non-runner overlay and proves the pinned kernel and
toolchain, GDM/GNOME Wayland session, portal interfaces, PipeWire,
WirePlumber, operator-attestation hooks, allowlisted HTTPS proxy path, and
direct-egress denial. It must complete before a workflow run is approved.

Build and probe logs, serial output, SSH keys, cloud-init inputs, seed disks,
and overlays remain inside a private per-run directory. The command removes
that directory on success, failure, timeout, cancellation, and interrupt. Only
the immutable base image, its build metadata, and the non-sensitive state lock
persist.

## Run one protected workflow cell

1. Dispatch either `RemoteDesktop E2E` or `ScreenCast E2E` for `gnome` at the
   exact branch/ref.
2. Inspect the workflow name, repository, event, head SHA, run ID, and attempt.
   Approve the matching protected GitHub Environment only for that exact
   trusted commit.
3. Start the matching local runner:

   ```bash
   go run ./internal/cmd/portalrunner run \
     -commit <40-character-lowercase-sha> \
     -run-id <github-run-id> \
     -run-attempt <positive-attempt> \
     -cell remote-desktop
   ```

   Use `-cell screencast` for the `ScreenCast E2E` workflow.
4. In the private QEMU GTK window, verify that the fixed disposable GNOME
   desktop is visible and interactive. Only then type the exact line `READY`
   in the host terminal.
5. Keep the private console visible. Inspect the real portal request and grant
   only the device or screen access expected by the selected workflow cell.
   Do not enable console recording, clipboard sharing, or file transfer.
6. Wait until the command reports `protected runner job complete`. A local
   success is reported only after GitHub confirms that the exact workflow
   attempt completed successfully and the ephemeral runner is offline or
   removed.

The orchestrator rejects a different repository, workflow, SHA, run ID,
attempt, event, cell, active duplicate runner, stale image, missing QEMU
capability, or unexpected operator input. Registration credentials are fetched
into memory, sent to the guest only through SSH standard input, cleared from
host buffers, and never placed in host process arguments or persistent host
files. The pinned GitHub runner configurator consumes the token inside the
disposable guest, whose overlay is destroyed after the attempt.

## Failure and cleanup

Pressing `Ctrl-C`, closing the VM, a failed job, a timeout, or a rejected portal
request is a failed evidence run. The supervisor terminates the complete QEMU
process group, closes the local proxy, removes the ephemeral GitHub runner,
and deletes the owned run directory. If cleanup itself fails, the command
returns that failure instead of reporting success.

Before retrying, verify:

```bash
find ~/.local/share/robotgo-portal-runner \
  -maxdepth 2 -type f -printf '%M %p\n'
gh api repos/marang/robotgo/actions/runners
```

There must be no `run-*` directory, private key, overlay, seed image, runtime
log, or stale `robotgo-gnome-*` runner. Do not upload or paste raw guest logs:
they are infrastructure diagnostics and may contain sensitive runtime context.
