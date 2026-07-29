# Protected Real-Compositor Evidence Plan

Status: Complete; hosted wlroots, GNOME/KDE portal and bounds, and Hyprland
evidence delivered; the later API-freeze candidate passes a 29-check
exact-release contract

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

The P005 runner contract requires every matrix job to use a clean, ephemeral
Linux environment that is destroyed afterward. Persistent personal desktop
sessions are not eligible. GNOME and KDE run as nested guests on fresh
GitHub-hosted runners without self-hosted registration credentials.

The protected workflows enforce these boundaries:

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
to physical devices or a host desktop. Its single-output result does not
replace the separately retained Sway multi-output or GNOME/KDE portal evidence.

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
bounded dialog settle interval, the GNOME host injects keyboard input for
RemoteDesktop and manifest-bound pointer input for multi-output ScreenCast
through QMP. For KDE ScreenCast, a digest-bound KWin helper reports only
virtual-screen and active-dialog geometry through a short-lived private D-Bus
receiver. It also reports the cursor position after the host selects the
physical-monitor target, proving QMP reached the intended dialog coordinate
before the portal's standard Return path is used. Pointer movement and click
are separated by a bounded handoff so Wayland focus reaches the dialog first.
Window names, accessibility data, pixels, and dialog content never leave the
guest. KDE's native
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

The ScreenCast cell opens one real consent session. Its single-output mode
obtains two non-empty frames from one PipeWire stream; its multi-output mode
requires two unique physical monitor streams and obtains one owned non-empty
frame from each. Both validate metadata/ownership, release all buffers/file
descriptors, and close PipeWire before the portal session. The
contracts follow the official
[RemoteDesktop](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.RemoteDesktop.html)
and
[ScreenCast](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.ScreenCast.html)
interfaces.

### Consent-free GNOME and KDE output proof

The follow-up bounds cell reuses the disposable GNOME/KDE guests but does not
use either portal interface. It configures the manifest-bound two-output
topology, removes `DISPLAY` from the test process, and runs the same public
geometry contract once through the native-CGO Wayland client and once through
the Pure-Go Wayland client. It requires exact output count, deterministic
primary-first bounds, aggregate desktop bounds, primary size, invalid-index
errors, and legacy/error API parity. No pixels, clipboard data, window content,
or input events are read or produced.
Both GNOME and KDE pass this contract in retained exact-commit
[`Display Bounds E2E` run 30268702514](https://github.com/marang/robotgo/actions/runs/30268702514).

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
4. **Hosted GNOME single- and multi-output proof delivered:** the pinned
   nested-KVM image, exact clean-tree transfer, real session, independent
   consent control, bounded test,
   egress, process-group shutdown, and artifact cleanup contracts are wired
   into both portal workflows. Retained exact-commit evidence:
   [`RemoteDesktop E2E` run 30199452053](https://github.com/marang/robotgo/actions/runs/30199452053)
   and [`ScreenCast E2E` run 30199195890](https://github.com/marang/robotgo/actions/runs/30199195890).
   Multi-output proof is retained in
   [`RemoteDesktop E2E` run 30220551561](https://github.com/marang/robotgo/actions/runs/30220551561)
   and [`ScreenCast E2E` run 30222315257](https://github.com/marang/robotgo/actions/runs/30222315257).
   The GNOME/KDE multi-output jobs are required by exact-release evidence.
5. **Hosted KDE single- and multi-output proof delivered:** retained exact-commit
   [`RemoteDesktop E2E` run 30204553569](https://github.com/marang/robotgo/actions/runs/30204553569)
   and
   [`ScreenCast E2E` run 30214614314](https://github.com/marang/robotgo/actions/runs/30214614314)
   prove the real portal backends, independent consent handling, bounded test
   execution, and transient-artifact cleanup. Multi-output proof is retained in
   [`RemoteDesktop E2E` run 30220551561](https://github.com/marang/robotgo/actions/runs/30220551561)
   and [`ScreenCast E2E` run 30221893077](https://github.com/marang/robotgo/actions/runs/30221893077).
   The GNOME/KDE multi-output jobs are required by exact-release evidence.
6. **Release promotion delivered:** require all six stable Sway checks for the
   exact release commit and in branch protection, plus the four reusable
   GNOME/KDE multi-output RemoteDesktop/ScreenCast jobs for the exact release
   commit.
7. **Runtime and release promotion delivered:** the consent-free public
   multi-output bounds contract passes in both native-CGO and Pure-Go builds on
   GNOME and KDE in retained
   [`exact-commit run 30268702514`](https://github.com/marang/robotgo/actions/runs/30268702514).
   Both lanes are required for the exact release commit; ordinary PR branch
   protection remains separate because each lane provisions a nested desktop.

Create Linear issues only when the next slice has concrete runner ownership and
acceptance evidence. Do not create speculative implementation tickets for
unfunded or unavailable infrastructure.

## Exit criteria

P005 completed after all of the following became blocking evidence:

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

The current completed contract passes in
[`Release Evidence` run 30284816440](https://github.com/marang/robotgo/actions/runs/30284816440)
on exact merged `main` commit
`912722cd480bd542419bd16e7267bbf22201e1ff`. Its schema-v1 manifest contains
exactly 29 successful checks, including both hosted GNOME/KDE bounds lanes and
the stable public API gate.

## Non-goals

- Running untrusted fork code on self-hosted infrastructure
- Replacing real portal consent with an auto-accepting mock
- Treating Weston, compile-only, or hermetic portal tests as GNOME/KDE evidence
- Persisting captured frames, clipboard data, input payloads, or restore tokens
- Claiming unsupported compositor-wide window control through Wayland core
