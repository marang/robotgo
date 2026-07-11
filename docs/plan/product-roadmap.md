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
- Non-CGO builds compile and return `ErrNotSupported` for native GUI operations;
  the explicitly authorized RemoteDesktop portal is the first tested Pure-Go
  input backend.
- CI covers lint, default tests on Linux/macOS/Windows, non-CGO, Wayland, portal,
  Weston integration, race, and vet variants.

## Execution Status (2026-07-11)

| Area | Status | Delivered | Exit criteria still open |
|---|---|---|---|
| Current baseline | Complete in branch | Native screencopy, screenshot portal fallback, bounded waits, cleanup, live capability probes, error APIs, non-CGO contract, dedicated race/vet jobs | Confirm new CI jobs on remote branch |
| 1. Wayland input | Implementation complete; runtime validation blocked | Native virtual keyboard/pointer, consent-aware RemoteDesktop fallback, shared ScreenCast stream mapping, absolute pointer/touch, restore tokens, diagnostics and E2E harness | Register GNOME/KDE/wlroots runners and collect green CGO/non-CGO evidence |
| 2. Capture | Partial | Reliable one-shot screencopy and screenshot portal, region crop, output geometry, scale/transform foundations | ScreenCast/PipeWire stream and full multi-output/fractional-scale/transform gates |
| 3. Pure-Go | Foundation only | Non-CGO builds fail explicitly instead of degrading silently | Useful selected Pure-Go backends, parity tests, benchmarks, backend introspection |
| 4. API/compositor gaps | Parity surface delivered; runtime support partial | Window-state error APIs, bitmap string helpers, `FindColorCS`, hook/event capability reporting, Sway/Hyprland/wlroots resolver | Compositor-backed state operations and cross-platform/runtime matrix coverage |
| 5. Reliability product | Partial | Capability API/example and expanded CI variants | Versioned compatibility matrix, richer diagnostics, dedicated compositor jobs, sanitizer/leak gates |

No delivery phase is complete until all of its exit criteria are blocking and
green. The active implementation slice is Phase 1: RemoteDesktop portal input
for GNOME and KDE while preserving native wlroots input protocols.

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
consistent in CGO and non-CGO builds. The
remaining Phase 1 blocker is real GNOME/KDE/wlroots evidence; this repository
currently has no registered self-hosted runners for that matrix.

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
regressions. Output geometry, scale, and transform handling exist but do not yet
have the complete compositor matrix required by this phase. ScreenCast/PipeWire
streaming is not implemented.

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
unsupported errors. No useful Pure-Go GUI backend is enabled yet; this is a
safety baseline, not completion of the phase.

Exit criteria:

- Users can inspect which implementation is active.
- CGO-disabled builds provide useful supported features without silent
  degradation.
- Benchmarks and behavioral parity tests justify every default switch.

### 4. Close API and Compositor Gaps

The compatibility surface now includes:

- Error-returning window state/query APIs (`IsTopMostE`, `SetTopMostE`,
  `IsMinimizedE`, `IsMaximizedE`) with explicit unsupported behavior.
- Bitmap string helpers (`CaptureBitmapStr`, `FindBitmapStr`, `BitmapFromStr`,
  `ToStrBitmap`).
- Region/tolerance color search through `FindColorCS`/`FindcolorCS`.
- Hook/event capability reporting on Wayland.

Remaining work must improve runtime support without inventing misleading
cross-platform semantics:

- Implement compositor-backed window state/query behavior where the compositor
  exposes trustworthy state and retain explicit unsupported results elsewhere.
- Validate bitmap and color-search behavior across capture backends and
  supported platforms.
- Stable multi-screen and bounds behavior for negative origins, transforms,
  scale, and absent display identifiers.

Every addition needs unit tests, an applicable runtime integration test, and a
runnable example unless the environment makes one technically impossible.

### 5. Turn Reliability Into a Product Feature

Publish a versioned compatibility matrix for Linux desktop/compositor versions,
macOS, Windows, CPU architectures, CGO modes, and optional dependencies. Add a
diagnostic API/example that reports selected backends, protocol versions,
fallback decisions, permissions, and actionable remediation without exposing
sensitive environment data.

Current status: `GetLinuxCapabilities` and its runnable example expose selected
feature backends, availability, fallbacks, reasons, and notes. CI covers three
operating systems, non-CGO, tagged Wayland/portal, and Weston integration.
Protocol versions, permission state, remediation guidance, versioned support
data, dedicated GNOME/KDE/wlroots jobs, and native sanitizer gates remain open.

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
