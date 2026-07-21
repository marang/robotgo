# Protected Real-Compositor Evidence Plan

Status: Accepted direction; runner-contract implementation is next

Linear project:
[RobotGo | P005 | Protected Compositor Evidence](https://linear.app/riotbox/project/robotgo-or-p005-or-protected-compositor-evidence-d66467e3b5ee)

## Outcome

Turn RobotGo's implemented GNOME, KDE Plasma, and wlroots Wayland paths into
reproducible, protected runtime evidence. A passing matrix cell must exercise
the real compositor, portal backend, PipeWire service, and RobotGo integration
harness. Missing infrastructure, skipped tests, mock services, or absent user
consent are never runtime passes.

This project closes evidence gaps shared by roadmap phases 1, 2, 3, and 5. It
does not add new public APIs or claim compositor behavior that the runtime did
not actually demonstrate.

## Trust and runner model

Each matrix job uses a clean, ephemeral Linux runner that accepts one job and
is destroyed afterward. Persistent personal desktop sessions are not eligible.
Runner groups and labels are restricted to this repository, and runner
registration credentials remain outside the repository and workflow logs.

The protected workflows retain these boundaries:

- trusted repository refs only; fork pull requests never run on these runners,
  and same-repository pull requests require explicit maintainer/Environment
  approval of the exact head commit
- read-only GitHub permissions and checkout credentials disabled
- a protected GitHub Environment with a maintainer approval boundary
- no long-lived repository, cloud, or personal credentials inside the desktop
  session; the unavoidable job-scoped `GITHUB_TOKEN` remains read-only
- outbound network access limited to GitHub endpoints and pinned package/image
  sources required to run the job
- runner application and system logs forwarded outside the disposable VM for
  infrastructure diagnosis, without captured desktop content
- complete VM/session destruction after success, failure, timeout, or
  cancellation

The workflows never use `pull_request_target` to check out and execute pull
request code. Approval makes the reviewed commit eligible for an isolated
runner; repository origin alone is not a trust decision.

GitHub recommends ephemeral runners for autoscaling because one job is assigned
before automatic deregistration. Runner logs must be retained externally, and
runner images must stay within GitHub's supported update window. The normative
platform reference is the
[GitHub self-hosted runner documentation](https://docs.github.com/en/actions/reference/runners/self-hosted-runners).

## Desktop image contract

P005 maintains three independently versioned images or VM definitions:

| Lane | Required session | Primary evidence |
|---|---|---|
| GNOME | Mutter Wayland plus `xdg-desktop-portal-gnome` | RemoteDesktop, persistent ScreenCast, output geometry |
| KDE | KWin Wayland plus `xdg-desktop-portal-kde` | RemoteDesktop, persistent ScreenCast, output geometry |
| wlroots | Sway first, with the matching portal backend where available | Native input/window behavior, output geometry, explicit portal availability |

Images pin the operating-system release and package source. The evidence
manifest records exact installed versions, so an image may be rebuilt for
security updates without pretending that it is the previous runtime. Software
rendering is acceptable when the compositor and PipeWire path behave normally;
a mock compositor or portal backend is not.

Each session starts before runner registration and provides:

- a real Wayland socket and user D-Bus session
- the expected compositor and `XDG_CURRENT_DESKTOP`
- `xdg-desktop-portal` plus the desktop-specific backend
- a running PipeWire/WirePlumber user session for ScreenCast cells
- an operator-visible consent surface for portal cells
- a deterministic test application/window and declared output topology; the
  desktop image contains no personal files, accounts, browser sessions, or
  unrelated application state

Portal consent remains real. The workflow does not patch the backend to
auto-approve requests and does not persist restore tokens between jobs. The
protected Environment tells an operator when interactive approval is required.

## Shared fail-closed preflight

The first implementation slice replaces duplicated shell probes in the two E2E
workflows with one repository-owned command. It validates all prerequisites
before RobotGo reads frames or injects input:

1. expected lane and desktop/compositor identity
2. live Wayland socket under `XDG_RUNTIME_DIR`, without printing its address
3. live user session bus and `org.freedesktop.portal.Desktop` owner
4. required RemoteDesktop and/or ScreenCast interfaces and advertised versions
5. PipeWire development/runtime availability for persistent capture
6. lane-specific native tools and capabilities used by window/input evidence
7. declared output count and multi-output requirement for geometry cells
8. writable runner-temporary evidence directory with cleanup registered

The preflight returns a non-zero status for missing or mismatched requirements.
It distinguishes unavailable infrastructure from a RobotGo test failure, but
both block the matrix cell. It never converts either result into a successful
skip.

Hermetic tests cover desktop matching, required capability selection, malformed
probe output, command failure, sanitization, bounded execution, and partial-file
cleanup. Tests use fixed fixtures and `t.TempDir`; they never inspect the
developer's live desktop, portal, or display environment.

## Versioned evidence manifest

Every cell uploads a schema-versioned manifest and test log. Allowed manifest
fields are deliberately narrow:

- schema version, UTC timestamp, exact Git commit and workflow/run identity
- operating-system ID/version, kernel name/release, architecture, Go version
- desktop lane, compositor name/version, portal frontend/backend package
  versions, PipeWire version, and advertised portal interface versions
- renderer class (`hardware` or `software`) without device serials
- sanitized output count and geometry/scale/transform values required by the
  assertion
- exact test command, outcome category, duration, and SHA-256 of the test log

The manifest and logs must not contain screenshots, frame pixels, window
titles, clipboard contents, injected text, restore tokens, PipeWire node IDs,
portal handles, display/socket addresses, hostnames, usernames, home paths,
runner registration tokens, environment dumps, or credentials. Captured frames
stay in memory and are released before session teardown.

Before artifact upload, a repository-owned validator checks the manifest schema,
required commit binding, allowed fields, and a denylist over both manifest and
test log. Validation failure blocks the cell and suppresses the unsafe artifact;
infrastructure-only diagnosis then uses the separately protected runner logs.
Manifest creation is transactional so a cancellation cannot promote a partial
file as evidence. Workflows capture raw runtime output only inside the
disposable runner; they do not stream it through `tee` into GitHub logs before
validation. After validation they publish a bounded sanitized summary plus the
approved evidence files.

## Runtime evidence matrix

### wlroots first proof

Sway is the first protected lane because RobotGo already has native
virtual-input, screencopy, output, and compositor-window integration harnesses.
The proof includes:

- native pointer and keyboard round trips with deterministic release
- Sway title/close and supported state operations against a self-owned window
- single- and multi-output bounds with scale/transform coverage
- native screencopy selection and buffer cleanup
- explicit RemoteDesktop/ScreenCast portal availability or unsupported result

Input is injected only into the isolated fixture session. Tests restore pointer,
key, button, window, and output state on success and through registered failure,
timeout, and cancellation cleanup paths.

An unavailable wlroots RemoteDesktop backend is valid capability evidence, not
a portal pass.

### GNOME and KDE portal proof

Each lane runs the lower-level RemoteDesktop harness directly so a different
native backend cannot satisfy the test. It validates granted device types,
relative and absolute pointer input, keyboard modifier release, optional touch,
stream mapping, and deterministic session close.

The ScreenCast cell opens one real consent session, obtains two non-empty frames
from the same PipeWire stream, validates geometry and ownership, releases all
buffers/file descriptors, and closes PipeWire before the portal session. The
contracts follow the official
[RemoteDesktop](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.RemoteDesktop.html)
and
[ScreenCast](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.ScreenCast.html)
interfaces.

### Promotion to protected evidence

A lane moves from `pending` to `pass` in the compatibility matrices only when:

- its pinned runner definition is reviewable and reproducible
- preflight and every selected real-runtime test pass without skips
- cleanup assertions pass on success and deliberately induced failure
- the sanitized manifest and log checksum are retained
- the evidence applies to the exact commit recorded by the manifest

The release-evidence workflow must require the promoted compositor checks for
the exact release commit. PR branch protection may require them only after the
runner capacity and consent process can reliably service every trusted PR;
until then, absence remains visible and release-blocking rather than reported as
green.

## Ordered delivery slices

1. Implement the shared preflight, schema-v1 manifest, hermetic tests, and both
   workflow integrations.
2. Provision and prove the ephemeral Sway/wlroots image and promote its native
   rows.
3. Provision and prove GNOME RemoteDesktop and ScreenCast.
4. Provision and prove KDE RemoteDesktop and ScreenCast.
5. Add promoted checks to release evidence and, when operationally reliable,
   branch protection; update the versioned compatibility matrices.

Create Linear issues only when the next slice has concrete runner ownership and
acceptance evidence. Do not create speculative implementation tickets for
unfunded or unavailable infrastructure.

## Exit criteria

P005 is complete only when:

- GNOME, KDE, and wlroots runner definitions are reproducible and ephemeral
- every configured job fails closed on missing infrastructure or consent
- real RemoteDesktop and ScreenCast pass on GNOME and KDE
- wlroots native input, capture, output, and supported window behavior pass
- multi-output geometry evidence exists for all three lanes
- manifests are schema-validated, sanitized, checksummed, and commit-bound
- failure, cancellation, and timeout paths leave no private desktop artifacts
- promoted checks block release evidence for the exact commit
- `docs/compatibility/wayland-input.md`, `wayland-capture.md`, and
  `runtime-v1.md` link the retained evidence

## Non-goals

- Running untrusted fork code on self-hosted infrastructure
- Replacing real portal consent with an auto-accepting mock
- Treating Weston, compile-only, or hermetic portal tests as GNOME/KDE evidence
- Persisting captured frames, clipboard data, input payloads, or restore tokens
- Claiming unsupported compositor-wide window control through Wayland core
