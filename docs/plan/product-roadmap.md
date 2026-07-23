# RobotGo Product Roadmap

## Product Goal

Build the most reliable and transparent cross-platform desktop automation
library for Go. The fork should exceed upstream RobotGo and RobotGo Pro through
observable backend selection, explicit failure contracts, first-class native
Wayland support, reproducible tests, and an auditable open implementation.

Compatibility remains a hard requirement: existing public APIs stay stable
unless a change is intentional and documented. New error-returning APIs are
preferred when legacy signatures cannot report failures safely.

## Current Baseline

The July 2026 hardening work establishes the foundation for this roadmap:

- Native Wayland capture prefers screencopy (`dmabuf`/`wl_shm`) and uses the
  screenshot portal only as an explicit, observable fallback.
- Capture and input paths have bounded waits and deterministic cleanup.
- Runtime capabilities probe live protocols and services instead of trusting
  session environment variables alone.
- Mouse, keyboard, capture, and window operations expose explicit errors where
  legacy APIs previously could hide unsupported behavior.
- Non-CGO builds provide supported capture backends, explicit RemoteDesktop
  portal input on Wayland, and XGB/XTEST input plus X11/EWMH window operations
  for X11-primary Linux sessions; remaining unavailable GUI operations return
  `ErrNotSupported`.
- CI covers lint, default tests on Linux/macOS/Windows, non-CGO, Wayland, portal,
  Weston integration, race, vet, and native sanitizer/leak variants.

## Execution Status (2026-07-23)

| Area | Status | Delivered | Exit criteria still open |
|---|---|---|---|
| Current baseline | Complete in main | Native screencopy, screenshot portal fallback, bounded waits, cleanup, live capability probes, error APIs, non-CGO contract, dedicated race/vet/sanitizer jobs, protected stable CI checks | Keep required jobs green |
| 1. Wayland input | Implementation complete; runtime validation partial | Native virtual keyboard/pointer, consent-aware RemoteDesktop fallback, shared ScreenCast stream mapping, absolute pointer/touch, restore tokens, diagnostics, protected portal harness, and isolated hosted Sway native/availability plus multi-output matrix | Register GNOME/KDE portal runners and collect their multi-output evidence |
| 2. Capture | Hermetic implementation complete; runtime validation partial | Reliable one-shot paths plus one consent-aware ScreenCast session, reusable PipeWire frames, logical region crop, raw pixel conversion, metadata/restore tokens, cleanup, integration harness, non-skipping geometry/transform CI, sanitizer-backed native ownership gates, and isolated hosted Sway native/multi-output evidence | Real GNOME/KDE portal and multi-output evidence |
| 3. Pure-Go | X11 complete; Windows input/window CI-evidenced; macOS capture/display/input and window implementation delivered; Wayland logical output enumeration plus Weston and hosted Sway multi-output evidence delivered; broader phase partial | Build and feature-level introspection; non-CGO macOS CoreGraphics capture/display, Quartz input, and Accessibility window inspection/control with explicit gaps; Windows capture, `SendInput` keyboard/pointer, and Win32 window control with blocking runtime probes; X11 capture, XGB/XTEST input, and X11/EWMH window introspection/control; Wayland portal capture/input plus bounded native `wl_output`/`xdg-output` geometry; permission/error contracts; shared behavioral parity; reproducible balanced benchmark tooling; optimized guardian-path decision evidence; explicit decision to retain native CGO as the X11 default; race-testable internal X11 core; re-exec guardian with application-`SIGKILL` recovery; protected three-OS CI | Collect opt-in real macOS input and self-owned-window evidence, protected GNOME/KDE multi-output Wayland evidence, and assess further backends selectively |
| 4. API/compositor gaps | Parity surface delivered; runtime support partial | Window-state and window-geometry error APIs, bitmap string helpers, `FindColorCS`, hook/event capability reporting, Sway/Hyprland/wlroots resolver, Sway active node/client geometry, Hyprland active compositor-reported geometry, provider-aware Hyprland 0.55+ Lua window dispatch | Further trustworthy compositor-backed state/geometry operations and cross-platform/runtime matrix coverage |
| 5. Reliability product | Partial | Capability APIs, versioned sanitized runtime diagnostics/example, compatibility matrix v1, expanded CI variants, blocking ASan/LeakSanitizer ownership gates, six-cell checksummed release-evidence pipeline, fail-closed real-compositor preflight/evidence contract, promoted six-cell hosted Sway release/branch gate, and the published [`v1.0.0-beta.1`](https://github.com/marang/robotgo/releases/tag/v1.0.0-beta.1) evidence bundle | Provision and promote dedicated GNOME/KDE portal jobs |

No delivery phase is complete until all of its exit criteria are blocking and
green. Phase 1 implementation is merged; its real-compositor evidence remains
an infrastructure blocker. The bounded cross-platform reliability-hardening
project P002 is complete, while roadmap Phase 5 remains partial. The active
[Protected Real-Compositor Evidence Plan](real-compositor-evidence.md) now
provides the shared preflight and sanitized evidence contract; protected runner
provisioning and promotion now remain for GNOME/KDE portal gates. The hosted
single-output Sway/wlroots native and portal-availability matrix is passing on
Ubuntu 24.04 with retained exact-commit evidence;
the separate hosted Sway multi-output cell is passing with retained
[exact-commit evidence](https://github.com/marang/robotgo/actions/runs/29861058126),
while GNOME/KDE multi-output evidence remains open across phases 1, 2, 3, and 5.
The completed [Agent Adapter and Evaluation Plan](agent-adapter-evaluation.md)
provides a local, policy-gated MCP boundary on the agent-session proof. The
adjacent [Safe Agent Visual Conditions Plan](agent-visual-conditions.md) now
has accepted bounded `find` and `wait` semantics in Go plus a thin,
privacy-preserving MCP projection; neither effort broadens platform backend
support or replaces phase exit gates.

## Delivery Order

### 1. Complete Reliable Wayland Input

Add the freedesktop RemoteDesktop portal path for GNOME and KDE while retaining
native virtual-pointer/virtual-keyboard protocols for compositors that expose
them. Portal session creation, consent, reconnect, cancellation, and cleanup
must be explicit. Capability reporting must distinguish portal availability,
permission denial, compositor protocol support, and unsupported operations.

Current status: native virtual-keyboard and virtual-pointer paths, live
readiness probes, error-returning APIs, lazy reconnect, and explicit cleanup are
implemented. A pure-Go RemoteDesktop portal client now covers capability probes,
consent-session creation, device selection, cancellation, denial, timeout,
portal-driven closure, deterministic teardown, and direct pointer/keyboard
notifications. After explicit consent, supported high-level mouse/keyboard APIs
fall back to the active portal session, including in non-CGO builds.
The RemoteDesktop session can now call ScreenCast `SelectSources` before start,
parse logical stream geometry, map `MoveE` absolute coordinates, inject touch,
and expose persistence restore-token availability. Runtime capability and
permission diagnostics, including explicit cancellation and timeout states, are
available without opening a consent dialog. Portal-backed mouse timing is
consistent in CGO and non-CGO builds. Isolated hosted Sway now covers the
single-output native input and explicit portal-availability contracts. The
remaining Phase 1 blocker is real GNOME/KDE portal and multi-output evidence;
the hosted Sway multi-output cell now passes, while this repository currently
has no registered self-hosted portal runners.

Exit criteria:

- Mouse and keyboard automation works on current GNOME, KDE, and wlroots test
  targets or returns a stable, actionable error.
- Hermetic tests cover negotiation, denial, timeout, reconnect, and teardown.
- Real-compositor integration tests cover at least GNOME, KDE, and one wlroots
  compositor.

### 2. Make Capture Production-Grade

Add a ScreenCast portal and PipeWire stream backend for repeated capture. Keep
the existing native screencopy path as the low-latency option and the screenshot
portal as the smallest safe fallback for one-shot capture.

Complete multi-output selection, fractional scaling, transforms, region
cropping, pixel-format conversion, and DMA-BUF fallback behavior across all
backends. Backend choice and fallback reasons remain available through runtime
capabilities and the capture debug trace.

Current status: the one-shot native and screenshot-portal paths are hardened,
including timeout, portal request-race, crop, DMA-BUF failure, and FD ownership
regressions. Portal artifacts are unlinked after an identity-verified open and
decoded with cancellation, encoded-size, dimension, and allocation bounds. An
opt-in `pipewire` build now opens one ScreenCast consent session,
owns its PipeWire remote and stream deterministically, returns repeated raw
frames, converts supported RGB/BGR formats to RGBA, maps logical regions at
fractional scale, applies all eight SPA video transforms and crop metadata,
exposes stream/restore metadata, and provides hermetic plus opt-in runtime
tests. Native readiness is bounded, PipeWire initialization is balanced, and
idle sessions do not convert unrequested frames. `CaptureScreen` can reuse the active session after native
screencopy failure or select it explicitly. Output geometry, scale, and
transform handling now share enclosing-edge crop semantics and have a
non-skipping hermetic CI matrix for negative output origins, fractional scale,
clipped regions, overflow boundaries, and all eight transforms. The complete
real-compositor matrix is still required by this phase.

Exit criteria:

- Repeated capture does not create a portal session per frame.
- Pixel and rectangle semantics are consistent across native and portal paths.
- Leak, timeout, multi-output, transform, and fractional-scale tests are
  blocking release gates.

### 3. Port Pure-Go Backends Selectively

Evaluate upstream Pure-Go implementations feature by feature instead of
replacing proven native code wholesale. A Pure-Go backend is enabled only when
it matches the public contract, has competitive correctness and performance,
and passes the same platform matrix as the CGO backend.

Priority order:

1. Platform detection and non-GUI helpers.
2. Windows and macOS operations with clear ownership boundaries.
3. X11 operations where a Pure-Go path reduces build dependencies without
   weakening behavior.
4. Wayland protocol clients only where they preserve the optimized native
   DMA-BUF path and deterministic resource handling.

Current status: the non-CGO API surface compiles and returns explicit
unsupported errors. `GetRuntimeBackendInfo` reports build facts without probes,
while `GetRuntimeCapabilities` reports feature backends, permission state,
fallbacks, and actionable reasons without opening consent dialogs. `Capture`,
`CaptureImg`, `CaptureScreen`, `CaptureGo`, `CaptureBitmapStr`, and the pixel
color APIs provide non-CGO capture through CoreGraphics on macOS, Windows and
X11 screenshot paths, and the hardened screenshot portal on Wayland. macOS
display enumeration, Screen Recording preflight, RGBA conversion, and
CoreGraphics ownership are covered hermetically on both supported
architectures. Non-CGO macOS also reports the real Retina display-mode factor,
returns scaled pixel dimensions with `GetScaleSize`, releases copied display
modes deterministically, and verifies the real symbols/display query in the
blocking macOS CI leg without requesting Screen Recording access.
The same non-CGO build now provides Quartz keyboard taps/combinations,
ownership-checked persistent holds, PID-targeted events, exact UTF-16 text with
surrogate-pair support, clipboard-assisted paste, and pointer movement,
relative and smooth movement, drag, single/double click, ownership-checked button toggles,
horizontal/vertical scrolling, and pointer location. It preflights
Accessibility without prompting, releases owned keys/buttons deterministically,
and has a blocking real-framework preflight that resolves keyboard and pointer
symbols without posting input. Hermetic tests cover modifier ordering, foreign
state, PID ownership, partial-event rollback, Unicode and cleanup. An opt-in
Accessibility-gated test posts and releases a real modifier without typing into
another application. Media/brightness keys and F21-F24 remain explicitly
unsupported because Quartz exposes no safe stable keycode for them.
The Pure-Go macOS window backend uses the same non-prompting Accessibility
contract and stable `CGWindowID` handles for active/PID/handle resolution,
titles, AX frame bounds, activation, minimize/restore, minimized-state queries,
and graceful close. It releases its dynamically loaded framework references
through `CloseMainDisplayE`. Client geometry intentionally equals the AX frame;
maximize and topmost remain explicit unsupported operations because macOS does
not expose trustworthy cross-application equivalents. CGWindowID mapping uses
the same runtime-resolved macOS bridge as the CGO backend and fails explicitly
if that bridge is unavailable. Hermetic tests cover the contract and hosted
macOS resolves the real symbols and permission preflight.
Permission-granted mutations still require a self-owned runtime window before
they can become blocking evidence.

Windows non-CGO builds now provide a foreground-layout-aware `SendInput` keyboard and text
backend, clipboard-assisted Unicode paste, pixel-at-pointer queries, plus exact pointer movement/location, smooth movement and drag,
buttons, horizontal and vertical scrolling, live readiness checks, partial
injection rollback, ownership conflicts, and deterministic in-process cleanup.
The deprecated `Drag` compatibility API is no longer a silent no-op: it
composes the supported backend primitives and always attempts button release
after a movement failure.
Exact Unicode text is encoded as UTF-16, while `KeyTap` resolves characters
through the foreground target's Windows keyboard layout instead of assuming US key
positions. Hermetic tests validate 32/64-bit `INPUT` layout and transaction
semantics. The Windows non-CGO CI leg runs the explicitly gated real
input-desktop pointer/pixel test and restores the original global cursor position.
The same build provides Win32 window capability reporting, active handle/PID,
PID-to-window resolution, titles, outer/client geometry, activation,
minimize/maximize, topmost state, graceful close, and real Win32 DPI scale
queries while retaining physical capture-space bounds. Its blocking runtime test
creates and owns the target window, exercises each operation, and never mutates
an unrelated application. The owned runtime window also contains an edit
control that provides blocking evidence for clipboard-assisted Unicode paste
and restores the previous readable text clipboard value.

Linux/X11 additionally has a Pure-Go XGB/XTEST keyboard and pointer backend for
the error-returning input APIs, text/Unicode, pointer location, smooth
movement/drag, scroll, live readiness probes, and deterministic connection and
owned-input cleanup. Backend selection requires an X11-primary session and
never treats Xwayland as an implicit Wayland fallback. An Xvfb CI test
uses `us,de` layouts to exercise exact Unicode mappings, real input delivery,
an independently delayed XKB target, foreign input-state preservation,
event-drain stress, cleanup, and reconnect.
The runnable example inspects selected capabilities without opening X11 by
default and requires an explicit `-act` flag before it runs live readiness
checks or injects global input.

The same X11-primary non-CGO build now provides active-window and PID/handle
resolution, title lookup, client/frame geometry, activation,
minimize/maximize, topmost state, and close. It strictly validates untrusted
X11 properties, closes per-operation connections deterministically, and
requires a consistent EWMH window-manager identity that advertises each
requested mutation. Missing, inconsistent, or unadvertised support returns the
public `ErrNotSupported` contract instead of optimistic success. A non-skipping
Xvfb integration test owns a fake EWMH manager and target window, exercises the
full public contract, and proves fail-closed behavior after manager loss. The
runnable example is inspection-only unless `-act` and an explicit mutation are
supplied.

Across the Pure-Go macOS, Windows, and X11 window backends, `CloseWindowKill`
resolves the actual owner PID and uses a bounded wait with a final deadline
probe. Windows and Linux acquire a stable process reference before requesting
the graceful close, verify the bound reference against a process identity
captured before acquisition, revalidate the window owner, and retain that same
verified process handle or `pidfd` through the optional force-kill. Linux uses
the exact procfs process-instance identity around `pidfd_open`; Windows uses
the process creation time from the stable handle. Owner/identity changes and
probe failures abort without a destructive fallback. macOS captures process
identity for the graceful wait and returns explicit unsupported if graceful
close is insufficient because it has no equivalent stable process handle.
Hermetic tests never terminate a real process.

The current upstream public-helper surface is also source-compatible without
adopting upstream's breaking `Click` signature change. The versioned upstream
audit records which changes are adopted, superseded, or rejected and why.
Shared Linux capture and bounds helpers now respect the selected display
server: CGO builds stay within the selected Wayland/X11 session path, Pure-Go
Wayland capture uses the hardened portal, and non-prompting Pure-Go Wayland
bounds use a bounded native `wl_output`/`xdg-output` client instead of falling
through to Xwayland. It provides primary-first per-output and aggregate logical
geometry, fractional `xdg-output` sizes, integer core scale/transforms,
protocol-version clamping, explicit errors, deterministic socket cleanup,
hermetic wire tests, and blocking single- and multi-output Weston evidence. The
multi-output job runs RobotGo as a Wayland-only client against two virtual
Weston outputs and verifies exact scale-2, rotated logical geometry without
capturing pixels.

The Linux/X11 evaluation slice of Phase 3 is complete. Native CGO and Pure-Go
X11 binaries pass one black-box public-API contract for capture, pointer,
buttons, scroll, modifier order, and ASCII text without keyboard-map changes. A
reproducible balanced benchmark script records raw observations, medians,
quartiles, ratios, metadata, and runs a report-only CI smoke. The versioned
decision-grade sample retains native CGO as the Linux/X11 default: native wins
the current measured capture and input latency/allocation comparisons, while
Pure-Go provides CGO-disabled build portability. At the measured revision,
native also had
stronger Unicode crash isolation; the later Pure-Go guardian closes the targeted
application-process-kill gap without changing that historical performance
sample. The current guardian-path comparison shows native winning the measured
latency and Go-allocation metrics while Pure-Go provides CGO-disabled build
portability and managed Unicode mappings. The comparison also exposed and fixed native
modifier-release ordering and unsafe server-global Unicode mapping. Native X11
now preflights complete text and modified keys before injection. Its Xlib
display, capture, input, readiness, replacement, and close paths share a locked
configured-display lifecycle; separate XGB connections use the same configured
target and close deterministically. Live readiness requires XTEST 2.2 and has a
dedicated negative CI contract. The Pure-Go backend retains its broader,
explicitly managed Unicode mapping support. Its stateful X11 implementation now
lives in a Linux internal package that is built by the normal CGO-enabled race
job as well as the production non-CGO adapter. A separate Pure-Go guardian owns
the live X11 connection through a randomly named, token-authenticated Linux
abstract Unix socket whose peer PID/UID the parent verifies. No live control FD
crosses the re-exec initialization phase. The guardian detects application death
through control-socket EOF, bounds normal request dispatch independently from
cleanup, and the parent kills and reaps a helper that misses its exit deadline.
Cleanup releases RobotGo-owned input and
restores a verified scratch before-image only when the exact recorded final
image remains, the keycode is unpressed, and it is not a modifier. Foreign final
images are preserved; X11 cannot identify an ABA replacement that returns to
the same exact image. The blocking Xvfb contract sends a real `SIGKILL` to the
application workload and compares core, modifier, XKB, key, pointer, and button
state. The guarantee requires the guardian and responsive X server to survive;
guardian/host loss or a transport blocked beyond the cleanup deadline still
needs later reconciliation. Current decision-grade evidence measures the
guardian path and confirms that its crash isolation adds material IPC latency,
so native CGO remains the default. Reusing bounded request state, reading frames
through a reusable buffer, and avoiding double payload marshaling reduce input
allocations by `24–33%` without weakening request correlation or cleanup.
Balanced transient press/release pairs now share one guardian request while
retaining per-step ownership, verified release, preflight, server-grab, timeout,
and crash-recovery contracts. This reduces another `5–14%` of allocations for
the affected click, scroll, key-press, and text benchmarks. Stable remote checks
now protect `main`; Linux X11 input/window and Windows
input/self-owned-window runtime evidence are blocking. Selectively evaluating
additional platform backends keeps the broader Phase 3 partial.

Exit criteria:

- Users can inspect which implementation is active.
- CGO-disabled builds provide useful supported features without silent
  degradation.
- Benchmarks and behavioral parity tests justify every default switch.

### 4. Close API and Compositor Gaps

Linear delivery project:
[`RobotGo | P006 | Explicit Window Geometry`](https://linear.app/riotbox/project/robotgo-or-p006-or-explicit-window-geometry-4af461c427fb).

The compatibility surface now includes:

- Error-returning window state/query APIs (`IsTopMostE`, `SetTopMostE`,
  `IsMinimizedE`, `IsMaximizedE`) with explicit unsupported behavior.
- Error-returning window geometry APIs (`GetBoundsE`, `GetClientE`) across CGO
  and non-CGO builds. Sway reports active node/client geometry from compositor
  tree metadata, Hyprland reports its active compositor window box, and unavailable
  client/frame distinctions or PID/handle modes remain explicit
  `ErrNotSupported`. Legacy wrappers return zero geometry on failure rather
  than substituting aggregate Wayland desktop bounds.
- Hyprland active-window maximize query, set, and restore backed by its
  compositor-reported state. Fullscreen remains distinct from maximized;
  Sway/generic wlroots queries stay explicitly unsupported rather than
  inferring state. Mutating close/maximize operations select the active
  `hyprlang` or Hyprland 0.55+ Lua dispatcher syntax and fail closed on
  provider-query transport failures or malformed successful detection.
- Bitmap string helpers (`CaptureBitmapStr`, `FindBitmapStr`, `BitmapFromStr`,
  `ToStrBitmap`).
- Region/tolerance color search through `FindColorCS`/`FindcolorCS`.
- Cross-build helper contracts covering versioned bitmap round-trips, strict
  RGB layout validation, default/explicit color tolerance, absolute region
  coordinates, and capture-error propagation. Hermetic tests exercise native
  Wayland screencopy, the Screenshot portal, and Pure-Go backend dispatch;
  generic string/search tests run in both CGO and non-CGO builds.
- Native and Pure-Go Wayland aggregate/per-output bounds preserve negative origins,
  fractional `xdg-output` logical sizes, integer core-output scale, and all
  transforms. Display indices are deterministic and shared with screencopy;
  display count/main-index queries no longer depend on X11. Native builds retain
  the `wayland-info` fallback for compositors where direct geometry is
  unavailable; Pure-Go uses bounded direct protocol enumeration.
- Hook/event capability reporting on Wayland.

Remaining work must improve runtime support without inventing misleading
cross-platform semantics:

- Extend compositor-backed state/query behavior beyond the delivered Hyprland
  maximize slice only where equally trustworthy state is exposed.
- Collect protected Hyprland geometry evidence and add other compositor
  geometry only where a stable active-window contract is available.
- Add protected real-desktop capture-helper evidence beyond the delivered
  hermetic native Wayland, portal, and cross-build backend contracts.
- Collect protected GNOME/KDE/wlroots multi-output bounds evidence beyond the
  delivered hermetic geometry matrix and single-/multi-output Weston
  integration.

Every addition needs unit tests, an applicable runtime integration test, and a
runnable example unless the environment makes one technically impossible.

### 5. Turn Reliability Into a Product Feature

Publish a versioned compatibility matrix for Linux desktop/compositor versions,
macOS, Windows, CPU architectures, CGO modes, and optional dependencies. Add a
diagnostic API/example that reports selected backends, protocol versions,
fallback decisions, permissions, and actionable remediation without exposing
sensitive environment data.

Current status: `GetRuntimeDiagnostics` schema v1 and its runnable JSON example
report selected feature backends, fallbacks, negotiated Wayland/portal/XTEST
versions, non-prompting permission state, and remediation without exposing
display addresses, restore tokens, stream identifiers, or unrelated environment
values. The published `docs/compatibility/runtime-v1.md` matrix distinguishes
blocking support from pending runtime evidence. CI covers three operating
systems, non-CGO, tagged Wayland/portal, Weston integration, and a blocking
Linux native ASan/LeakSanitizer gate. The sanitizer job verifies its hermetic
Wayland ownership-test manifest and covers allocation/free, timeout cleanup,
and descriptor ownership. Release Evidence v1 records exact commit/tree/ref,
toolchain and runtime identity, sanitized diagnostics, the passed command, and
the test-log SHA-256 for native/Pure-Go Linux, macOS, and Windows cells. It also
requires and records the complete protected CircleCI/lint/vet/race/sanitizer/
platform/Wayland/X11 and six-cell hosted Sway check set for the exact commit.
The first public
pre-release, [`v1.0.0-beta.1`](https://github.com/marang/robotgo/releases/tag/v1.0.0-beta.1),
publishes that verified six-cell bundle and checksum for exact commit
`1bab5e173f6b96f61d349473b348f839291b9a89`; manual runs remain read-only
artifacts. Process termination rejects non-positive and platform-overflow PIDs
before invoking the operating system, preventing Unix process-group signaling
through `Kill(0)` or a narrowed negative PID. Dedicated GNOME/KDE portal jobs
remain open; the hosted wlroots jobs are promoted.

Releases require:

- Default, race, lint, vet, tagged Wayland/portal, non-CGO, and cross-platform
  build gates to pass.
- Dedicated GNOME, KDE, and wlroots compositor jobs with failures distinguished
  from unavailable test infrastructure.
- Leak/sanitizer checks for native ownership boundaries.
- A concise compatibility and migration note for user-visible behavior changes.

Language bindings and additional platforms follow only after the core backend
contracts and release matrix are stable.

## Competitive Standard

The fork is considered better than upstream and RobotGo Pro only when it can
demonstrate the difference, not merely expose more functions. The standard is:

1. Correct behavior or an explicit error; never plausible zero values or silent
   success.
2. Native Wayland support across wlroots, GNOME, and KDE, with consent-aware
   portal integration where required.
3. Stable source compatibility plus error-returning APIs for automation that
   needs guarantees.
4. Observable backend and fallback decisions.
5. Reproducible, blocking validation across supported platforms and build tags.
6. Deterministic cleanup of sessions, descriptors, buffers, and protocol
   objects.
7. Open, auditable implementations and documented capability limits.

## Roadmap Governance

- `docs/wayland-tasks.md` is the operational backlog and current support matrix.
- `docs/plan/wayland-prio-plan.md` contains the detailed Wayland architecture
  and milestone history.
- `docs/plan/agentic-desktop-automation.md` defines the accepted agent-session,
  policy, observe-act-verify, and later adapter architecture. Its executable
  slices remain independently reviewed and do not weaken platform reliability
  gates.
- `docs/plan/agent-visual-conditions.md` defines privacy-safe visual search and
  bounded wait semantics above the accepted agent session.
- This document defines product direction and delivery order.
- A roadmap item is complete only when implementation, tests, examples where
  applicable, compatibility documentation, and required CI gates are complete.
- New work should land as small, auditable changes; performance claims require
  benchmarks and backend-default changes require migration notes.

## Non-Goals

- Claiming universal Wayland behavior that compositor security policy forbids.
- Hiding missing runtime services behind X11 fallbacks in Wayland sessions.
- Trading correctness or resource safety for headline API count.
- Copying upstream or commercial architecture without independently validating
  its behavior against this repository's contracts.
