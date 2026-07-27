# AGENTS.md

This file defines project-wide engineering rules for humans and coding agents.
If another document conflicts with this file, follow this file for day-to-day
implementation behavior in this repository.

Normative scope:

1. Sections 1-12 are the repository's active, normative policy.
2. The former generic operating-manual appendix is preserved in a
   [non-normative historical snapshot](docs/archive/legacy-agent-operating-manual.md).
3. If that archive conflicts with this file or actual repository layout, this
   file and repository reality take precedence.

## 1) Project Scope and Priorities

RobotGo is a cross-platform desktop automation library with Go + CGO backends
for:

- Mouse
- Keyboard
- Screen capture and pixel operations
- Window/process helpers
- Image/bitmap conversion utilities

Current strategic priority on Linux is robust Wayland support while preserving
cross-platform compatibility (macOS, Windows, Linux/X11).

## 2) Core Principles

1. Keep behavior correct before optimizing internals.
2. Prefer explicit capability/unsupported behavior over silent degradation.
3. Keep public API behavior stable unless a change is intentional and documented.
4. Keep platform branches isolated and easy to reason about.
5. Treat tests as required evidence, not optional cleanup.
6. Avoid magic strings in runtime logic; use named constants/enums for env keys,
   backend identifiers, protocol tokens, and resolver markers.

## 3) Linux Display-Server Policy (Wayland-First)

Wayland is the primary Linux target. X11 support remains important, but it is
not the default design anchor for Wayland sessions.

X11 support is required when Linux is running an X11 session.

1. In Wayland sessions (`WAYLAND_DISPLAY` set), features should run directly on
   native Wayland paths whenever possible.
2. Do not route Wayland-primary logic through X11-only helpers.
3. X11 fallback in Wayland sessions is allowed only as a constrained,
   explicit, well-justified fallback.
4. Any fallback order must be intentional, documented, and testable.
5. A code path that appears Wayland-capable but still depends on X11 internals
   is a bug.
6. Wayland-only environments (`WAYLAND_DISPLAY` set, `DISPLAY` unset) are a
   first-class runtime target and must not regress.
7. In X11 sessions (`DISPLAY` set, no Wayland session), X11 behavior must stay
   fully supported and tested.

## 4) Fallback Strategy Rules

When a feature supports multiple Linux backends, keep the decision flow clear:

1. Prefer native backend for the detected session.
2. If native backend fails, use the smallest safe fallback.
3. Log fallback decisions behind existing debug knobs (for example
   `ROBOTGO_CAPTURE_DEBUG=1`).
4. Return explicit errors when no supported backend can satisfy the operation.
5. Never hide backend failures by returning zero-values that look valid.

For screen capture on Linux, preserve and extend the current model:

1. Wayland screencopy first (`dmabuf`/`wl_shm`).
2. Portal fallback when required (`ROBOTGO_FORCE_PORTAL` and
   `ROBOTGO_WAYLAND_BACKEND` overrides remain honored).
3. X11 path for X11 sessions.

## 5) Build Tags and Platform Boundaries

Respect current tag split and do not collapse platform boundaries:

- `linux && wayland`: native Wayland-enabled Linux path
- `linux && !wayland`: X11-focused Linux path
- `linux && portal`: explicit portal package path (`screen/portal`)
- `linux && wayland && test`: tagged Wayland capture/DRM test paths
- `linux && wayland && integration`: integration suites requiring compositor setup
- `linux && hostedboundsintegration`: consent-free hosted GNOME/KDE output
  contract (combined with `wayland` for the native-CGO variant)
- `cgo && linux && waylandint`: keyboard integration harness
- `linux && !cgo && x11integration`: Pure-Go X11/XTEST input integration suite

Rules:

1. New Linux backend code must land in the correct tag/file split.
2. Non-Linux and non-CGO builds must continue to compile (provide stubs where
   required).
3. Avoid introducing hidden runtime dependencies that only work in one tagged
   build variant unless intentionally scoped and documented.

## 6) C / CGO Integration Rules

This repository includes C protocol and backend code. Changes must be careful
and minimal.

1. Keep C resource ownership explicit (connect/disconnect, alloc/free, destroy).
2. Match existing ownership patterns (`FreeBitmap`, C alloc/free boundaries).
3. Do not leak Wayland/X11 handles, file descriptors, or buffers.
4. Prefer deterministic cleanup over relying on process teardown.
5. Preserve generated protocol file conventions and build guards.
6. If changing protocol-generation flow, keep `wayland_generate.go` and checked
   in generated artifacts consistent.

## 7) API Behavior Contract

1. Maintain existing exported function signatures unless change is intentional.
2. For operations unsupported on Wayland/compositor policy, return explicit
   errors (`ErrNotSupported` and related helpers) rather than pretending success.
3. Keep semantics consistent across equivalent APIs (`GetBounds`/`GetClient`,
   `GetScreenSize`/`GetScreenRect`, etc.).
4. Avoid adding behavior that is ambiguous under multi-output or scale/transform
   scenarios without tests.

## 8) Testing and Validation Requirements

Minimum local validation for meaningful changes:

1. `go test ./...`
2. Relevant tagged suites for changed area (see `TEST.md`)

Repository-default command baseline in this repo currently uses direct `go`
commands (no required root `Makefile` workflow).

Sensitive-data cleanup is mandatory:

1. Default unit tests must not persist the developer's real desktop, clipboard
   contents, input events, OCR inputs, or similar private data.
2. Integration tests and diagnostics that intentionally exercise real private
   data must remove every sensitive artifact on success, failure, timeout, and
   cancellation paths.
3. Prefer in-memory fixtures and `t.TempDir()`/`t.Cleanup()` for unavoidable
   files. Files returned by external backends remain RobotGo's cleanup
   responsibility.
4. Regression tests must verify that sensitive artifacts no longer exist after
   processing, including error paths.

Common targeted suites:

1. `go test -tags "portal" ./screen/portal -v`
2. `go test -tags "wayland test" ./screen -run TestScreencopy -v`
3. `go test -tags "wayland test" . -run TestDrmFindRenderNode -v`
4. `go test -tags "wayland integration" ./mouse ./window -v`
5. `go test -race -tags "waylandint" ./key -v`
6. `go test -race -tags "wayland waylandint" . -run '^TestWaylandPublic' -v`
7. `CGO_ENABLED=0 ROBOTGO_REQUIRE_X11_INTEGRATION=1 xvfb-run -a -s "-screen 0 1280x720x24 -nolisten tcp -noreset" sh -eu -c 'setxkbmap -layout us,de; env -u WAYLAND_DISPLAY -u XDG_SESSION_TYPE go test -tags x11integration -run "^TestPureGoX11" -count=1 -timeout=30s -v .'`

Wayland-related code changes should include at least one of:

1. A test in a Wayland-only setting.
2. A regression test proving fallback behavior.
3. A test that verifies explicit unsupported/error contract.

When implementing new or previously missing Wayland functionality, provide all
of the following unless technically impossible:

1. At least one runnable example (or update an existing example) showing usage.
2. Unit tests that validate normal and failure/fallback behavior.
3. Integration tests that exercise real compositor/runtime interaction where
   applicable.

If one layer (example/unit/integration) is intentionally omitted, document why
in the PR and add a follow-up task.

## 9) Screen/Bounds-Specific Guardrails

For functions that expose dimensions or rectangles:

1. Do not depend on X11 helpers inside Wayland-primary branches.
2. Validate non-zero dimensions before accepting backend results.
3. Handle multi-output aggregation correctly when backend provides per-output
   geometry.
4. Keep behavior stable when `displayId` is absent or negative.
5. Add regression coverage when touching bounds logic, especially for
   Wayland-only sessions.

## 10) Documentation and Change Hygiene

The operational branch, pull-request, CI, reviewer, merge, and cleanup loop is
defined in `docs/workflow_conventions.md`. Follow it for normal repository work;
this file remains authoritative for engineering and safety rules.

Any backend behavior change should update affected docs:

1. `README.md` for user-visible backend behavior/env vars.
2. `TEST.md` for new/changed test commands or prerequisites.
3. `docs/wayland-tasks.md` if roadmap status changes.

Keep diffs focused:

1. Do not mix unrelated refactors with backend fixes.
2. Prefer small, auditable commits and clear PR notes.
3. Call out risks, fallbacks, and tested environments in PR description.

Documentation granularity policy:

1. Document only decisions with lasting impact:
   - architecture choices
   - backend selection policy/contracts
   - non-obvious trade-offs/risks
   - API/behavior/test-strategy changes
2. Do not document low-signal details:
   - tiny refactors without behavior change
   - local implementation minutiae
   - temporary intermediate steps
3. Keep docs concise, scannable, and decision-oriented to avoid noise.

## 11) Review Checklist (Required Before Merge)

1. Correct tag/file placement for platform-specific changes.
2. No unintended X11 dependency inside Wayland-primary runtime path.
3. Resource lifecycle validated (no obvious leaks).
4. New/updated tests cover changed behavior and regression risk.
5. User-facing docs/env vars remain accurate.
6. Default and relevant tagged tests pass.

## 12) Quick Decision Guide for Agents

When implementing Linux functionality:

1. Detect session (`Wayland` vs `X11`) early.
2. Implement native Wayland path first.
3. Add explicit fallback only if necessary.
4. Make fallback observable and testable.
5. Preserve cross-platform compile behavior.
