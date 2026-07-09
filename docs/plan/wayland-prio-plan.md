# Wayland PRIO Plan (0-3)

## Execution Status (as of 2026-03-23)

### Milestone A
- Status: In progress (implementation and verification complete; examples/docs follow-up open)
- Completed:
1. Capability contract introduced for Linux runtime backends (`GetLinuxCapabilities`).
2. Wayland screen size/rect fallback no longer routes through X11 bounds helper.
3. Wayland bounds fallback parses `wayland-info` output with non-zero validation and cache.
4. Regression coverage added for Wayland-only mode (`WAYLAND_DISPLAY` set, `DISPLAY` unset).
5. `wayland test` + `waylandint` build/link instability fixed (duplicate symbols and tag leakage).
6. Wayland keyboard tag path hardened to avoid X11 symbol dependency in Wayland-only integration helper.
7. Mock/test stability improved with explicit environment-based skips for unavailable compositor/runtime.
- Still open in A:
1. Add environment/integration tests for compositor-specific runtime paths (sway/hyprland/wlroots generic).
2. Extend docs with a concise backend support matrix for window operations.

## 0. PRIO-1: Correct Wayland-First Implementation

### Goal
Make Wayland behavior correct and primary on Linux Wayland sessions, without implicit X11 dependencies.

### Success Criteria
- No implicit X11 dependency when `WAYLAND_DISPLAY` is set.
- Deterministic behavior per feature: native Wayland path, explicit fallback, or explicit unsupported error.
- No silent `0x0` outcomes for screen size/bounds/capture/input.
- Wayland-only test mode (`WAYLAND_DISPLAY` set, `DISPLAY` unset) stays green for required suites.
- Tag-specific test suites compile and run without linker-only or build-tag-only breakage.
- Test failures caused purely by missing compositor/runtime facilities are reported as explicit skips, not false negatives.

### Work Items
1. Define feature-level capability contract for `capture`, `bounds`, `keyboard`, `mouse`.
2. Enforce Wayland-no-X11 guards in runtime paths.
3. Harden Wayland capture path (`dmabuf`/`wl_shm` first, portal fallback second).
4. Harden Wayland bounds path with native output geometry and validated non-zero results.
5. Harden keyboard path for Wayland-only builds/tests (no X11 symbol/link leakage).
6. Add/adjust unit + integration + example coverage per changed Wayland feature.
7. Ensure Wayland test-only helpers do not pull duplicate C symbol definitions (no multiple-definition linker failures).
8. Remove X11 symbol requirements from Wayland-only test tags unless explicitly under X11 fallback tests.
9. Stabilize mock compositor lifecycle in tests (no hangs or aborts on shutdown).

---

## 1. Alternatives to System Packages

### Question
Can this work without host system packages?

### Answer
Partially. Build/runtime dependencies can be reduced, but not fully eliminated for native Wayland capabilities.

### Work Items
1. Prefer pure-Go D-Bus portal path for screenshot fallback where possible.
2. Keep `libportal`/`libpipewire` tagged/optional paths isolated from default flow.
3. Provide containerized dev/test image with pinned native deps for reproducibility.
4. Document minimal required runtime services (portal daemon, compositor features).
5. Ship a reproducible CI/dev container image with required packages preinstalled.
6. Keep default `go test ./...` independent of optional system libs and compositor tools.

### Expected Outcome
- Fewer hard host-package blockers in default workflows.
- Tagged/native suites still available when full system deps exist.

---

## 2. Universal Wayland Integration

### Goal
Provide broad compositor compatibility (not just one environment).

### Implementation Design (Go Interfaces)
Use explicit backend interfaces so compositor-specific logic remains isolated and testable.

1. Add a `windowBackend` interface in Go for global window operations:
   - `Activate(target WindowTarget) error`
   - `Minimize(target WindowTarget, state bool) error`
   - `Maximize(target WindowTarget, state bool) error`
   - `Close(target WindowTarget) error`
   - `Title(target WindowTarget) (string, error)`
   - `Capabilities() FeatureCapability`
2. Keep existing Wayland-core behavior as a baseline backend:
   - returns explicit `ErrNotSupported` for non-universal operations.
3. Add compositor-specific backend implementations behind the interface:
   - `wlrootsBackend` (Sway/wlroots family)
   - `mutterBackend` (GNOME)
   - `kwinBackend` (KDE)
4. Add deterministic backend resolver:
   - detect compositor/session once at startup/runtime probe
   - choose most specific backend first, then baseline Wayland backend
   - no implicit X11 fallback in Wayland sessions
5. Extend `GetLinuxCapabilities()` to report:
   - detected compositor family
   - selected window backend
   - per-window-operation capability state

### Backend Hierarchy and Resolver Policy (Normative)
For Wayland window operations, backend selection must be deterministic and layered:

1. Compositor-specific backends first:
   - `swayBackend`
   - `hyprlandBackend`
   - future compositor-specific backends
2. Shared compositor-family backend second:
   - `wlrootsGenericBackend`
3. Baseline universal fallback last:
   - `waylandCoreBackend` (explicit `ErrNotSupported` for non-universal operations)

Rules:
1. Do not use implicit X11 fallback inside Wayland sessions.
2. Each backend must implement `Capabilities()` and expose explicit availability.
3. Each backend layer must ship resolver tests and behavior tests (supported vs unsupported paths).
4. Adding a new compositor backend must not change public API semantics.

### Work Items
1. Define compositor matrix: wlroots, GNOME (Mutter), KDE (KWin).
2. Define feature matrix per compositor:
   - `dmabuf`
   - `wl_shm`
   - portal fallback
   - multi-output
   - scale/transform handling
   - consent/failure behavior
3. Introduce deterministic backend selection and explicit degradation behavior.
4. Add CI jobs for matrix subsets with explicit skip policy when environment cannot satisfy a case.
5. Split "functional unsupported" vs "environment unavailable" reporting in test output.
6. Add smoke checks for protocol/version compatibility drift (for generated wayland protocol code).

### Expected Outcome
- Predictable behavior across major Wayland compositors.
- Fewer environment-dependent surprises.

---

## 3. Better Solutions (Architecture Upgrades)

### Goal
Improve maintainability, diagnosability, and long-term Wayland robustness.

### Work Items
1. Introduce backend interfaces by feature:
   - `captureBackend`
   - `boundsBackend`
   - `keyboardBackend`
2. Add `GetCapabilities()` API for runtime backend/feature introspection.
3. Expand portal support to full screencast/PipeWire stream path (not only screenshot fallback).
4. Standardize structured error codes and backend decision tracing.
5. Add a backend decision trace contract (debug-only) that can be asserted in tests.

### Expected Outcome
- Cleaner backend separation.
- Better user-facing diagnostics and safer fallback behavior.

### Ongoing Engineering TODOs
1. Replace remaining legacy magic strings in runtime decision logic with named constants/enums.
2. Enforce "no new magic strings" for all new logic paths.
3. Apply opportunistic cleanup: whenever touching a file, replace nearby magic-string decision logic in the same change when safe.

---

## Delivery Milestones

### Milestone A (1-2 days)
- Capability contract and Wayland-no-X11 guards.
- Core regression fixes and baseline tests.
- Tag-build/link reliability fixes (`wayland test`, `waylandint`).
- Mock-server lifecycle stabilization.

### Milestone B (2-4 days)
- Portal optionalization improvements.
- Stabilized tagged suites and dependency documentation.
- CI/container baseline for optional native deps.

### Milestone C (3-5 days)
- Compositor matrix validation.
- `GetCapabilities()` draft API and docs/examples.
- Deterministic skip/error taxonomy in test and runtime diagnostics.

### Milestone D (3-5 days)
- Introduce `windowBackend` interface and resolver.
- Implement `wlrootsBackend` (Sway-first target) with integration tests.
- Add initial GNOME/KDE backend scaffolding with explicit capability reporting.
- Keep unsupported operations explicit where compositor API is unavailable.

### Milestone D1 (2-4 days)
- Implement `wlrootsGenericBackend` behind `windowBackend`.
- Add resolver branch for wlroots-family detection and selection.
- Add capability tests for wlroots generic backend.

Current progress:
1. Resolver architecture implemented with explicit compositor detection + specific-backend lookup + family fallback.
2. Specific-backend hooks in place for `sway` and `hyprland`.
3. wlroots family fallback path implemented for known wlroots compositors (`wayfire`, `river`, `labwc`, `dwl`, `gamescope`).
4. Priority and fallback behavior covered by unit tests.
5. First real `wlrootsGenericBackend` operation implemented:
   - active-window minimize/maximize via `wlrctl` (`state:active`, `state=true`).
6. End-to-end resolver/dispatch tests added for wlroots min/max path
   (`MinWindowE(0)` and `MaxWindowE(0)` in Wayland wlroots-family session).
7. Runtime integration coverage added for backend capability selection:
   - `wlroots-generic` runtime capability check (skips when compositor is not wlroots generic).
   - `sway` runtime capability check.
   - `hyprland` runtime capability check (skips unless hyprland runtime is active).
8. Remaining in D1:
   - execute wlroots min/max E2E path under dedicated wlroots runtime job.

### Milestone D2 (2-4 days)
- Implement `swayBackend` (Sway IPC extension path) behind `windowBackend`.
- Implement `hyprlandBackend` (Hyprland-specific extension path) behind `windowBackend`.
- Ensure resolver priority: `sway/hyprland` -> `wlrootsGeneric` -> `waylandCore`.
- Add integration tests for backend selection and operation behavior per compositor.

Current progress:
1. Backend labels and resolver branches for `sway` and `hyprland` exist.
2. First compositor-specific operation implemented:
   - active window title retrieval in `swayBackend` and `hyprlandBackend`.
3. Second compositor-specific operation implemented:
   - active window close in `swayBackend` and `hyprlandBackend`.
4. Third compositor-specific operation implemented:
   - active window minimize/maximize via `wlrctl` fallback in `swayBackend` and `hyprlandBackend` (state=true only).
5. Unit tests added for compositor command parsing and unsupported PID/handle title paths.
6. Unit tests added for compositor min/max `wlrctl` fallback paths.
7. Remaining in D2:
   - extend additional compositor-specific operations beyond title retrieval
   - add environment/integration tests with real compositor runtime

### Milestone E (optional)
- Full portal screencast/PipeWire integration path.

---

## Validation Checklist

1. `go test ./...`
2. `go test -tags "wayland test" . -run TestDrmFindRenderNode -v`
3. `go test -tags "wayland test" ./screen -run TestScreencopy -v`
4. `go test -tags "waylandint" ./key -run TestWaylandUnicodeTypingIntegration -v`
5. `go test -tags "wayland integration" ./mouse ./window -v`
6. Confirm Wayland-only session behavior: `WAYLAND_DISPLAY` set and `DISPLAY` unset.
7. Confirm no implicit X11 calls in Wayland-primary paths via targeted regression tests.
8. Confirm tagged suites do not fail due to duplicate C symbols or tag-incompatible linkage.

---

## Risks and Mitigations

1. Risk: compositor/runtime differences cause flaky integration tests.
   - Mitigation: strict skip policy for environment gaps, matrix execution across wlroots/GNOME/KDE, and deterministic assertions only.
2. Risk: test-only C wrappers introduce duplicate symbols.
   - Mitigation: isolate wrappers in dedicated C files, avoid including large implementation headers in multiple compilation units.
3. Risk: portal behavior varies by backend/consent flow.
   - Mitigation: assert backend selection first; treat pixel-content checks as backend-specific where necessary.
4. Risk: protocol/API drift in generated Wayland code.
   - Mitigation: add regeneration checks and compatibility smoke tests in CI.

---

## Assumptions and Non-Goals

1. Non-goal: eliminate all system dependencies for native Wayland functionality.
2. Assumption: Wayland runtime availability is environment-dependent and must be handled explicitly.
3. Non-goal: force all integration tests to pass on machines without required compositor/runtime packages.
4. Goal: ensure behavior is deterministic and failure modes are explicit even when environment is incomplete.

---

## Completion Definition (0-3)

0. PRIO-1 is complete when:
1. Wayland-primary code paths are X11-independent by design.
2. Required tagged suites compile and execute without structural/linker failures.
3. Remaining skips are environment-declared and not code-regression induced.
4. Documentation and examples match actual backend behavior and prerequisites.
