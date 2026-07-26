# Protected Real-Compositor Evidence Plan

Status: Hosted wlroots and GNOME/KDE single-output portal execution delivered;
GNOME/KDE multi-output and portal release promotion remain

Linear project:
[RobotGo | P005 | Protected Compositor Evidence](https://linear.app/riotbox/project/robotgo-or-p005-or-protected-compositor-evidence-d66467e3b5ee)

## Outcome

Turn RobotGo's implemented GNOME, KDE Plasma, and wlroots Wayland paths into
reproducible, protected runtime evidence. Every passing matrix cell must
exercise the real compositor and RobotGo integration harness. Portal cells must
also exercise the real desktop portal backend, ScreenCast cells must exercise
PipeWire, and wlroots-native cells must exercise their selected native
protocols. Missing applicable infrastructure, skipped tests, mock services, or
required but absent user consent are never runtime passes.

This project closes evidence gaps shared by roadmap phases 1, 2, 3, and 5. It
does not add new public APIs or claim compositor behavior that the runtime did
not actually demonstrate.

## Trust and runner model

The P005 target design requires every matrix job to use a clean, ephemeral
Linux environment that is destroyed afterward. Persistent personal desktop
sessions are not eligible. GNOME and KDE run as nested guests on fresh
GitHub-hosted runners without self-hosted registration credentials.

Before the protected workflows are enabled, their implementation must enforce
these boundaries:

- hosted GNOME/KDE runs only on trusted `main` pushes or explicit manual
  dispatch; fork and ordinary pull-request events never boot either guest
- read-only GitHub permissions and checkout credentials disabled
- no repository, cloud, personal, checkout, or runner-registration credential
  inside either portal desktop session
- outbound network access limited to GitHub endpoints and pinned package/image
  sources required to run the job
- runner application and system logs forwarded outside the disposable VM for
  infrastructure diagnosis, without captured desktop content
- complete VM/session destruction after success, failure, timeout, or
  cancellation

The implemented workflows must never use `pull_request_target` to check out and
execute pull request code. Approval makes the reviewed commit eligible for an
isolated runner; repository origin alone is not a trust decision.

GitHub recommends ephemeral runners for autoscaling because one job is assigned
before automatic deregistration. Runner logs must be retained externally, and
runner images must stay within GitHub's supported update window. The normative
platform reference is the
[GitHub self-hosted runner documentation](https://docs.github.com/en/actions/reference/runners/self-hosted-runners).

## Desktop image contract

P005 maintains independently reproducible runtime definitions for three lanes:

| Lane | Required session | Primary evidence |
|---|---|---|
| GNOME | Mutter Wayland plus `xdg-desktop-portal-gnome` | RemoteDesktop, persistent ScreenCast, output geometry |
| KDE | KWin Wayland plus `xdg-desktop-portal-kde` | RemoteDesktop, persistent ScreenCast, output geometry |
| wlroots | Nested headless Sway on pinned GitHub-hosted Ubuntu first | Native input/capture/window behavior, output geometry, explicit portal availability |

Images pin the operating-system release and package source. The evidence
manifest records exact installed versions, so an image may be rebuilt for
security updates without pretending that it is the previous runtime. Software
rendering is acceptable when the compositor and PipeWire path behave normally;
a mock compositor or portal backend is not.

The first wlroots definition uses `ubuntu-24.04` plus distribution Sway,
`swaybg`, and `wev` packages on an ephemeral GitHub-hosted runner. It creates a
private nested compositor with `WLR_BACKENDS=headless`, `WLR_RENDERER=pixman`,
`WLR_LIBINPUT_NO_DEVICES=1`, no `DISPLAY`, and exactly one 1280x720
`HEADLESS-*` output. This is real Sway/wlroots protocol evidence without access
to physical devices or a host desktop. It does not satisfy the still-open
multi-output or GNOME/KDE portal evidence requirements.

Each GNOME/KDE session provides:

- a real Wayland socket and user D-Bus session
- the expected compositor and `XDG_CURRENT_DESKTOP`
- `xdg-desktop-portal` plus the desktop-specific backend
- a running PipeWire/WirePlumber user session for ScreenCast cells
- a real desktop consent surface for portal cells
- a deterministic test application/window and declared output topology; the
  desktop image contains no personal files, accounts, browser sessions, or
  unrelated application state

Portal consent remains real. The workflow does not patch the backend to
auto-approve requests and does not persist restore tokens between jobs.

### Consent handoff

GNOME and KDE use independent host-side consent controllers. The test writes a
private, non-sensitive marker immediately before its portal call. After a
bounded dialog settle interval, the GNOME host injects the required keyboard
input through QMP. For KDE ScreenCast, a digest-bound KWin helper reports only
virtual-screen and active-dialog geometry through a short-lived private D-Bus
receiver. A second digest-bound helper reads only accessibility roles, states,
actions, and rectangles; it reports the physical-card and disabled
Share-button centres without reading labels or content. The host verifies both
targets inside the active dialog, then selects the physical monitor and
confirms Share through QMP pointer input. Window names, accessibility labels or
trees, pixels, and dialog content never leave the guest. KDE's native
non-sandboxed RemoteDesktop backend follows the upstream
notification policy and presents no modal approval dialog. RobotGo cannot
access the private QMP socket, does not patch the portal backend, and does not
call its own input API to grant permission. Missing readiness, early test exit,
denial, timeout, a wrong source type, or a surviving marker fails the cell.

This handoff must be proven operational for the lane before its portal cells
become required. GitHub Environment instructions may link to the private
operator queue, but Environment approval is never reported as portal consent.

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
8. independent QMP consent-driver readiness, plus the KDE dialog-geometry
   locator for its ScreenCast cell
9. writable runner-temporary evidence directory with cleanup registered

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

1. **Implemented:** shared preflight, schema-v1 manifest, hermetic tests, and
   both portal-workflow integrations.
2. **Implemented for the single-output proof:** run native input, capture,
   window, output, and portal-availability cells in isolated hosted Sway and
   retain sanitized exact-commit evidence.
3. **Implemented:** run a distinct isolated hosted Sway multi-output cell with
   negative origin, scale, transform, exact logical per-output bounds,
   aggregate bounds, and induced-failure cleanup evidence. Retained evidence:
   [`Sway E2E` run 29861058126](https://github.com/marang/robotgo/actions/runs/29861058126).
4. **Hosted GNOME single-output proof delivered:** the pinned nested-KVM image, exact
   clean-tree transfer, real session, independent consent control, bounded test,
   egress, process-group shutdown, and artifact cleanup contracts are wired
   into both portal workflows. Retained exact-commit evidence:
   [`RemoteDesktop E2E` run 30199452053](https://github.com/marang/robotgo/actions/runs/30199452053)
   and [`ScreenCast E2E` run 30199195890](https://github.com/marang/robotgo/actions/runs/30199195890).
   Multi-output geometry and release-gate promotion remain open.
5. Provision and prove KDE RemoteDesktop and ScreenCast under the same gate.
6. **Implemented for hosted wlroots:** require all six stable Sway checks for
   the exact release commit and in branch protection. Extend promotion to GNOME
   after wiring its passing jobs into exact-release evidence, and to KDE only
   after its protected runner and consent path are proven.

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
