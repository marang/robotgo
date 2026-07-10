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
- Non-CGO builds compile and return `ErrNotSupported` until a tested Pure-Go
  backend is selected.
- Default, race, lint, non-CGO, Wayland, portal, and cross-platform build gates
  cover the supported build variants.

## Delivery Order

### 1. Complete Reliable Wayland Input

Add the freedesktop RemoteDesktop portal path for GNOME and KDE while retaining
native virtual-pointer/virtual-keyboard protocols for compositors that expose
them. Portal session creation, consent, reconnect, cancellation, and cleanup
must be explicit. Capability reporting must distinguish portal availability,
permission denial, compositor protocol support, and unsupported operations.

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

Exit criteria:

- Users can inspect which implementation is active.
- CGO-disabled builds provide useful supported features without silent
  degradation.
- Benchmarks and behavioral parity tests justify every default switch.

### 4. Close API and Compositor Gaps

Finish the remaining compatibility surface without inventing misleading
cross-platform semantics:

- Window state/query operations (`IsTopMost`, `SetTopMost`, `IsMinimized`,
  `IsMaximized`) with compositor-specific capability reporting.
- Bitmap serialization helpers and consistent region/tolerance color search.
- Hook/event capability detection on Wayland with explicit unsupported errors.
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
