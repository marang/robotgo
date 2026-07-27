# GitHub-Hosted Hyprland Window Runner

This directory defines RobotGo's isolated Hyprland window-geometry evidence
image. The `Hyprland E2E` workflow runs it only on a fresh GitHub-hosted Ubuntu
runner.

The host loads Linux `vkms`, verifies that exactly one DRM card is backed by
that virtual driver, and passes only that card into the pinned Arch Linux
container. The runtime has no network, `/dev/input`, render node, host display
socket, writable source tree, checkout credentials, or physical GPU device.
Hyprland owns one virtual 1280x720 output and RobotGo controls only a
self-created `wev` window.

The runtime proves:

- active title and PID lookup through Hyprland;
- exact active-window bounds at `(120,80) 480x320`;
- legacy/error-returning bounds parity;
- explicit unsupported contracts for portable handles, client bounds, and
  PID-targeted geometry;
- AddressSanitizer/LeakSanitizer execution;
- deterministic fixture, compositor, seat-manager, socket, log, and temporary
  directory cleanup on success and failure.

Only sanitized `evidence.json`, `test.log`, and `summary.md` files are retained.
The induced-failure job proves that private compositor logs and runtime state
are removed before the real cell runs.

Do not run the workflow wrapper against a developer's DRM device. Local work
should use the hermetic contract and compile gates documented in
[`TEST.md`](../../TEST.md); real execution belongs on the disposable hosted
`vkms` runner.
