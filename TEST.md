# Testing Guide

This repository has both default tests and special test suites behind build tags.

## Default Test Suite

Run this first on any platform:

```bash
go test ./...
```

This is the baseline suite used for regular development and should stay green.

The non-CGO contract is also part of CI:

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go test -tags "ocr" ./...

# Optional in-process OCR backend (requires Tesseract and Leptonica development files)
go test -tags "ocr" ./...
```

The default and non-CGO suites also lock the versioned runtime-diagnostic
schema, stable feature ordering, deadline-bounded portal probes, sanitized
output, negotiated protocol versions, permission states, and remediation.
Inspect a live host without opening a consent dialog with:

```bash
go run ./examples/runtime_diagnostics
```

Both build variants are blocking linter targets as well:

```bash
CGO_ENABLED=1 golangci-lint run --timeout=5m ./...
CGO_ENABLED=0 golangci-lint run --timeout=5m ./...
```

Linux native ownership boundaries are a blocking AddressSanitizer and
LeakSanitizer target. Reproduce the default gate on a CGO-capable Linux
`amd64` host with GCC:

```bash
ASAN_OPTIONS=detect_leaks=1:halt_on_error=1:strict_string_checks=1 \
  go test -asan -count=1 ./...

ASAN_OPTIONS=detect_leaks=1:halt_on_error=1:strict_string_checks=1 \
  go test -asan -tags "wayland test" ./screen \
  -run '^TestScreencopy(WlShm|TimeoutIsBounded|DmabufFailureDoesNotCloseStdin)$' \
  -count=1 -timeout=60s -v
```

The tagged tests use hermetic Wayland protocol servers and must all pass; CI
checks their manifest so a missing or renamed ownership test cannot silently
reduce coverage. The gate covers allocation/free, timeout cleanup, and file
descriptor ownership without requiring a graphical session or render node.

The non-CGO suite runs on Linux, macOS, and Windows in CI. It also verifies
runtime build/feature introspection, pixel-color parity, and hermetic Pure-Go
capture dispatch for CoreGraphics, X11, Windows, and the Wayland screenshot
portal. macOS tests use fake CoreGraphics bindings for deterministic permission,
pixel, bounds, Retina display-mode scale, and resource-lifecycle coverage. The
macOS non-CGO leg also resolves the real CoreGraphics display-mode symbols and
queries the active display scale as a blocking test. It additionally resolves
the real Quartz keyboard/pointer and Accessibility symbols and performs a
non-prompting permission preflight without posting input. None of these runtime
checks requires a Screen Recording or Accessibility grant.

Default Linux screen tests use hermetic portal fixtures rather than persisting
the developer's real desktop. Portal regression tests require temporary
screenshot files to be absent after successful decoding and after decode
failures. The production portal reader unlinks the sensitive file immediately
after opening it.

The real scale probe can be reproduced on a macOS GUI runner without granting
Screen Recording or Accessibility access:

```bash
CGO_ENABLED=0 go test \
  -run '^TestPureGoDarwinDisplayScaleRuntime$' -count=1 -v .
```

The safe Pure-Go input preflight can be reproduced on a macOS GUI runner
without moving the pointer or opening a consent dialog:

```bash
CGO_ENABLED=0 go test \
  -run '^TestPureGoDarwinInputRuntime$' -count=1 -v .
```

The Pure-Go window preflight resolves the real CoreGraphics, CoreFoundation,
and Accessibility symbols, reports permission without prompting, and verifies
that framework cleanup can be repeated and reopened:

```bash
CGO_ENABLED=0 go test \
  -run '^TestPureGoDarwinWindow(CapabilityUsesNonPromptingPreflight|CleanupIsReusable)$' \
  -count=1 -v .
```

GitHub-hosted macOS runs this preflight without controlling another
application. Permission-granted activation, minimize/restore, and close remain
opt-in runtime evidence until a self-owned macOS test-window harness is
available; tests must not mutate an unrelated developer window.

Pure-Go `CloseWindowKill` tests use fake window/process backends. They cover
PID, handle and active-window resolution, graceful exit, the bounded force-kill
fallback, stable Linux `pidfd` binding, deadline races, and fail-closed probe
errors without terminating a real process:

```bash
CGO_ENABLED=0 go test \
  -run '^(TestCloseWindowKill|TestCloseWindowProcessIdentity|TestCloseWindowProcessKillLinux|TestWaitForWindowProcessExit)' \
  -count=1 -v .
```

After granting Accessibility access, the opt-in real input tests move to the
center of the main display and restore the original location, and exercise one
ownership-checked Shift hold/release without typing text:

```bash
CGO_ENABLED=0 ROBOTGO_REQUIRE_DARWIN_INPUT_INTEGRATION=1 \
  go test -tags darwinintegration \
  -run '^TestPureGoDarwin(Pointer|Keyboard)Integration$' -count=1 -v .
```

These integration tests are not run on GitHub-hosted macOS because those runners
do not grant Accessibility control to repository test binaries. The
non-prompting symbol and permission contract remains blocking there.

Linux CI additionally runs the non-CGO X11 input backend against a real Xvfb
server with XTEST 2.2 or newer and `us,de` keyboard layouts. A separate X11
evidence job runs one shared public-API contract against native CGO and Pure-Go
binaries, verifies both backends' safety-specific contracts, and compiles/runs
every balanced-comparison benchmark once. The same job starts a reachable Xvfb with XTEST disabled
and verifies that native readiness rejects it without injecting input. Missing
runtime prerequisites fail instead of turning these checks into successful
skips. Performance numbers are report-only; correctness is blocking. Repository
branch protection requires the stable three-OS, lint, vet, race, sanitizer,
Wayland, and X11 evidence checks. Opt-in real-compositor jobs remain excluded
until matching self-hosted runners are registered.

Pure-Go Windows input has hermetic tests for Win32 `INPUT` layout,
foreground-layout key mapping, Unicode surrogate pairs, partial-injection
rollback, ownership, buttons, scrolling, movement, clipboard-paste sequencing,
legacy drag release-on-failure, and pixel-at-pointer dispatch. They run in the
Windows non-CGO CI leg. The same leg runs a real input-desktop pointer and
pixel-color probe and restores the original global cursor position:

```powershell
$env:CGO_ENABLED = "0"
$env:ROBOTGO_REQUIRE_WINDOWS_INPUT_INTEGRATION = "1"
go test -tags windowsintegration -run "^TestPureGoWindowsInputRuntime$" -count=1 -v .
```

The environment variable remains an explicit safety gate for local execution.

Pure-Go Windows window control uses a self-owned Win32 top-level window, so the
hosted runner can validate the full window contract and clipboard-assisted
keyboard injection without typing into another application. The test covers
capability reporting, PID/handle resolution, title, outer/client bounds,
Win32 DPI scale without double-scaling capture bounds, minimize/maximize,
foreground activation, Unicode `PasteStr` into an owned edit control, topmost
state, and `WM_CLOSE`:

```powershell
$env:CGO_ENABLED = "0"
$env:ROBOTGO_REQUIRE_WINDOWS_WINDOW_INTEGRATION = "1"
go test -tags windowsintegration -run "^TestPureGoWindowsWindowRuntime$" -count=1 -v .
```

The environment variable is an explicit local safety gate because the test
temporarily changes the text clipboard; it restores the previous readable text
value during cleanup. CI runs this command as a blocking Windows non-CGO check.

Opt-in macOS runtime capture benchmark:

```bash
ROBOTGO_CAPTURE_BENCHMARK=1 \
  go test -run '^$' -bench BenchmarkCaptureImgRuntime -benchmem .
CGO_ENABLED=0 ROBOTGO_CAPTURE_BENCHMARK=1 \
  go test -run '^$' -bench BenchmarkCaptureImgRuntime -benchmem .
```

Run this from a GUI session after granting Screen Recording access to the test
binary or terminal. Running the same benchmark with and without CGO provides a
direct backend comparison. The hermetic macOS conversion benchmark is available
without a real capture using `-bench BenchmarkDarwinCapturePipeline`.

Reproducible Linux/X11 native-versus-Pure-Go evidence:

```bash
scripts/benchmark-x11-backends.sh /tmp/robotgo-x11-backend-evidence
```

The script requires a clean worktree for decision evidence and automatically
builds from an isolated detached worktree at that commit. Dirty development
smoke is explicit, visibly non-decision-grade, and aborted if its source
fingerprint changes. The script owns an isolated Xvfb with a `us,de` keymap,
runs the shared contract plus exact native- and Pure-Go-specific safety
manifests, and balances benchmark order. Its default five two-order cycles at
`500ms` produce ten observations per benchmark and implementation. Raw Go
output, behavior logs, environment and binary-build metadata without the
hostname or `GOENV` path, and a table with median, Q1–Q3 spread, observation
count, and median ratio are written to the requested directory. That directory
is exclusively owned by the script, guarded against concurrent writers, and
must be new, empty, or contain its valid evidence sentinel. The run starts with
`run-status.txt` marked incomplete, invalidates stale artifacts, and publishes
a complete status only after all behavior and benchmark checks pass. Only a
clean detached snapshot with at least five balanced cycles and a duration of at
least `500ms`, expressed as integer milliseconds, seconds, minutes, or hours,
is marked decision-grade. It also requires 10 matching benchmark
names in both outputs, the expected sample count, and `ns/op`, `B/op`, and
`allocs/op` for every result. The generated summary identifies the commit and
measurement setup and labels CI smoke data as report-only. Use full results for
an explicit engineering decision, never as a timing threshold on a shared
runner. Set
`ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY=1` only for local development smoke; those
results are explicitly not versioned decision evidence. Inspect `metadata.txt`
before publishing it: compiler commands, build settings, library paths, and
flags are recorded for reproducibility and can contain machine-specific values
in custom toolchains.
CI uses the smaller smoke configuration:

```bash
ROBOTGO_X11_EVIDENCE_COUNT=1 \
ROBOTGO_X11_EVIDENCE_BALANCED=0 \
ROBOTGO_X11_EVIDENCE_BENCHTIME=1x \
  scripts/benchmark-x11-backends.sh /tmp/robotgo-x11-smoke
```

## Special Test Suites (Build Tags)

Some tests are intentionally gated because they require OS-specific runtime dependencies or mock compositor/server setup.

### `wayland,test`

Purpose:
- Linux Wayland screencopy/mock-server coverage
- Hermetic native crop mapping for multi-output coordinates, fractional scale,
  overflow boundaries, and all eight output transforms
- Hermetic aggregate/per-output bounds for negative origins, fractional
  `xdg-output` geometry, stable display indices, scale, and all transforms
- DRM helper tests

Typical command:

```bash
go test -tags "wayland test" ./screen -run TestScreencopy -v
go test -tags "wayland test" . -run 'Test(DrmFindRenderNode|Wayland)' -v
```

Prerequisites:
- Linux
- CGO enabled
- Wayland dev/runtime deps
- For DRM tests: `/dev/dri` render node access

### `portal`

Purpose:
- Linux portal package tests (`screen/portal`)

Command:

```bash
go test -tags "portal" ./screen/portal -v
```

Prerequisites:
- Linux
- CGO enabled for the optional `CBitmap` adapter
- A live portal is not required; D-Bus behavior is tested hermetically

### `wayland,integration`

Purpose:
- Integration tests in `mouse/wayland_test.go` and `window/wayland_test.go`
- Runtime backend integration tests in root package for Wayland window resolver paths

Command:

```bash
go test -tags "wayland integration" . ./mouse ./window -v
```

Prerequisites:
- Linux
- Wayland runtime available

### RemoteDesktop portal input

Purpose:

- Hermetic RemoteDesktop request/session lifecycle coverage
- Consent response, denial, timeout, portal closure, device grants, and cleanup
- Direct pointer and keyboard notification dispatch
- Shared RemoteDesktop/ScreenCast negotiation, stream metadata, absolute
  pointer coordinates, optional touch, and restore-token handling
- High-level CGO and non-CGO fallback dispatch after explicit consent
- CGO/non-CGO parity for mouse delays and explicit consent-timeout diagnostics

Command:

```bash
go test -race ./input/portal
```

Prerequisites:

- No live portal is required for the hermetic suite.
- The runnable `examples/remote_desktop_input` probe requires Linux plus
  `xdg-desktop-portal` and a backend that implements RemoteDesktop.
- `-connect` and `-demo` may show a consent dialog; `-demo` injects input only
  after approval. Add `-screen` to demonstrate absolute stream coordinates or
  `-touch` to request and demonstrate touchscreen input. Restore-token contents
  are intentionally never printed.

Opt-in real portal lifecycle test:

```bash
ROBOTGO_REMOTE_DESKTOP_E2E=1 go test -tags "integration" ./input/portal -run TestRemoteDesktopPortalRuntime -v
```

The test opens the lower-level portal session directly and exercises relative
and absolute pointer input, a modifier press/release, optional touch, and
deterministic close. It intentionally does not use the high-level fallback APIs,
so an available native Wayland backend cannot mask a broken portal path. Default
hosted CI only compile-checks this harness because it has no real desktop consent
session. `.github/workflows/remote-desktop-e2e.yml` runs the test without skipping
on explicitly provisioned self-hosted GNOME, KDE, and wlroots Wayland runners,
once per desktop. The portal client is pure Go and therefore independent of the
root package's CGO setting; CGO and non-CGO high-level fallback dispatch remains
covered by the hermetic root tests. The workflow can be triggered
manually at any time. Set the repository variable
`ROBOTGO_REMOTE_DESKTOP_E2E=1` after those runners are provisioned to run the
same matrix on pull requests and pushes to `main`, where it can be configured as
a required check for branches in this repository. Fork pull requests are
intentionally excluded because untrusted code must never execute on persistent
self-hosted desktop runners. Configure the `remote-desktop-e2e` GitHub
Environment with required reviewers and use ephemeral, network-isolated runners.
The workflow uses read-only permissions, does not persist checkout credentials,
and verifies that each runner's `XDG_CURRENT_DESKTOP` matches its matrix label
before injecting input.

Runtime outcomes and missing infrastructure are recorded in
`docs/compatibility/wayland-input.md`; an unavailable runner is not counted as a
passing compositor.

### Persistent ScreenCast/PipeWire capture

Purpose:

- Hermetic ScreenCast request/session negotiation, denial, closure, metadata,
  file-descriptor ownership, and deterministic teardown
- Reusable PipeWire consumer compilation and frame/crop behavior
- Fractional logical-to-physical region mapping and repeated-frame lifecycle
- Multi-output positions (including negative origins), clipped regions, and
  non-zero frame origins
- Native C packed-pixel conversion plus SPA crop/transform metadata processing
- Explicit cursor-metadata rejection for the image capture API

Command:

```bash
go test -race ./screen/portal
go test -race -tags "pipewire" ./screen/portal
```

Prerequisites for the tagged suite:

- Linux, CGO, and `libpipewire-0.3-dev`
- No live portal is required for the hermetic tests

Opt-in real portal/PipeWire test:

```bash
ROBOTGO_SCREENCAST_E2E=1 go test -tags "pipewire integration" ./screen/portal -run TestPipeWireCapturePersistentSessionIntegration -v
```

Run it from a graphical Wayland session. It displays the portal consent UI,
captures two frames from the same session, validates non-empty output, and
closes the PipeWire consumer before the portal session.
`.github/workflows/screencast-e2e.yml` runs the same harness on protected
self-hosted GNOME, KDE, and wlroots runners when the repository variable
`ROBOTGO_SCREENCAST_E2E=1` is enabled, or by manual dispatch.

### `waylandint` (Keyboard integration harness)

Purpose:
- Hermetic mock Wayland keyboard-server coverage for virtual-keyboard setup,
  uppercase ASCII plus modifier restore, exact public `TypeStrE` rune behavior,
  all-rune preflight with zero mutation for unsupported text, deterministic
  keyboard-capable multi-seat selection and cleanup, runtime seat failover,
  transport failure, reconnect, modifier reset, and safe RemoteDesktop fallback
  after a zero-mutation native preflight failure
- Files:
  - `key/wayland_integration_test.go`
  - `key/mock_keyboard_server.go`
  - `key/testdata/mock_keyboard_server.c`
  - `wayland_public_integration_test.go`
  - `wayland_mock_server_integration.go`

Command:

```bash
go test -race -tags "waylandint" ./key -v
go test -race -tags "wayland waylandint" . -run '^TestWaylandPublic' -v
```

Prerequisites:
- Linux
- CGO enabled
- Wayland server/client dev libs

Status:
- Blocking in the `wayland-integration` CI job. The suite is hermetic and does
  not require a running graphical compositor.

### `x11integration` (native and Pure-Go X11 input and window)

Purpose:

- One black-box public-API contract compiled with both `CGO_ENABLED=1` and
  `CGO_ENABLED=0`: capture pixels/bounds/backend identity, pointer movement and
  observation, buttons/scroll, canonical modifier order, ASCII text delivery,
  and unchanged keyboard/modifier maps
- Native regression coverage proving unsupported Unicode, unmapped modified
  keys, and a later unmapped text character fail before any key event and never
  change the server-global keyboard map
- Native display-lifecycle stress across concurrent capture, input, window
  queries, scaling, `SetXDisplayName`, and `CloseMainDisplayE`; this includes
  argumentless bounds-plus-capture leases, mutable `DISPLAY`/`WAYLAND_DISPLAY`
  transitions, out-of-bounds pixel errors, a valid explicit target remaining
  selected with empty display environment variables, and proof that an invalid
  explicit display never falls through to `DISPLAY`
- Deeper Pure-Go-only validation of the Linux X11 input backend
- Independent pointer-position checks and real motion, drag, button, and scroll
  delivery through XGB/XTEST
- Named-key toggles plus exact text/Unicode mapping under a multi-layout
  `us,de` keymap, including a separately focused, deliberately delayed
  `xkbcli` target and deterministic mapping restoration
- Preservation of keys and pointer buttons held by another X11 client
- Event-drain stress coverage plus deterministic owned-input release,
  error-reporting `CloseMainDisplayE`, mapping restoration, and lazy reconnect
- A real application-process `SIGKILL` test: a separately re-executed workload
  leaves Unicode scratch state plus a held key/button behind, while the
  surviving Pure-Go guardian must restore the canonical core map, modifier map,
  XKB description, key state, pointer state, and button mask. The emergency
  test cleanup is claim-bounded: it restores only an exact unchanged scratch
  final image that is unpressed and absent from the modifier map
- Side-effect-free capability selection plus explicit readiness probes against
  the live X server
- Pure-Go active-window, PID/handle, title, client/frame geometry, activation,
  minimize/maximize, topmost, and close behavior against a self-owned fake EWMH
  window manager, including explicit unsupported behavior for unadvertised
  operations and after manager loss
- Adversarial replacement of a reserved mapping before injection; the default
  non-CGO unit suite separately covers modifier-map exclusion and bounded-scroll
  validation

Recommended balanced-comparison command:

```bash
scripts/benchmark-x11-backends.sh /tmp/robotgo-x11-backend-evidence
```

Deep Pure-Go command:

```bash
CGO_ENABLED=0 ROBOTGO_REQUIRE_X11_INTEGRATION=1 \
  xvfb-run -a -s "-screen 0 1280x720x24 -nolisten tcp -noreset" \
  sh -eu -c '
    setxkbmap -layout us,de
    env -u WAYLAND_DISPLAY -u XDG_SESSION_TYPE \
      go test -tags "x11integration" \
      -run "^TestPureGoX11" -count=1 -timeout=30s -v .
  '
```

The extracted core is also exercised with the Linux race detector in a normal
CGO-enabled test process; the production backend remains a `CGO_ENABLED=0`
implementation:

```bash
go test -race -count=20 -timeout=2m ./internal/x11input
```

Native missing-XTEST contract:

```bash
CGO_ENABLED=1 \
ROBOTGO_EXPECT_X11_IMPLEMENTATION=native-cgo \
ROBOTGO_EXPECT_X11_NO_XTEST=1 \
  xvfb-run -a \
  -s "-screen 0 640x480x24 -nolisten tcp -noreset -extension XTEST" \
  sh -eu -c '
    unset WAYLAND_DISPLAY XDG_SESSION_TYPE
    go test -tags x11integration \
      -run "^TestX11MissingXTestReadinessContract$" \
      -count=1 -timeout=30s -v .
  '
```

Prerequisites:

- Linux
- CGO-enabled builds need X11/XTest development files; the deep Pure-Go suite
  uses `CGO_ENABLED=0`
- An X11 server with XTEST 2.2 or newer
- `xvfb`, `xauth`, `setxkbmap` and `xkbcomp` (Debian/Ubuntu package
  `x11-xkb-utils`), `xkbcli` (`libxkbcommon-tools`), and `stdbuf` (`coreutils`)
- A mounted, readable Linux procfs with an executable `/proc/self/exe`; the
  runtime sandbox/service must permit re-executing the current test/program,
  Linux abstract Unix sockets, and `SO_PEERCRED` peer verification. Dependency
  initializers that run before RobotGo's guardian initializer must not block or
  terminate the re-executed helper
- The crash proof additionally needs Linux child-subreaper support and readable
  `/proc/<pid>/task/<tid>/children` files so it can verify the exact guardian
  child is adopted, exits successfully, and is reaped. Normal guardian runtime
  cleanup does not inspect that child-listing file

Without `ROBOTGO_REQUIRE_X11_INTEGRATION=1`, the normal suite skips cleanly when
`DISPLAY` or XTEST is unavailable. Linux CI sets that variable, configures both
layouts, and treats an unavailable or misconfigured X11 runtime as a failure.
The explicit missing-XTEST test is the exception: it requires a reachable X
server and expects the XTEST probe to fail. The tagged suite verifies the active
`us,de` layout itself; the default non-CGO unit suite separately proves that a
Wayland-primary session never selects this backend through implicit Xwayland.
The crash contract covers termination of the application process while its
guardian and X server continue running and respond within the bounded cleanup
deadline. Scratch restoration requires the current mapping to equal the exact
recorded final image and the keycode to be unpressed and non-modifier. Foreign
final images are preserved; an ABA change that returns to the same exact image
cannot be detected by the X11 protocol. A simultaneous guardian/container/host
kill, X-server loss, or pathologically blocked X11 transport cannot provide
synchronous restoration. Normal dispatch and cleanup have independent
deadlines; the parent kills and reaps a helper that does not exit afterward.
Explicit `CloseMainDisplayE` reports actionable cleanup/transport failures;
intentionally preserving a foreign replacement is not itself an error.

## Useful Environment Variables

- `WAYLAND_DISPLAY`
  - Selects Wayland socket in tests that launch/use a mock compositor server.
- `XDG_RUNTIME_DIR`
  - Must be set for Wayland socket creation.
- `ROBOTGO_FORCE_PORTAL=1`
  - Forces portal capture path for Linux capture tests.
- `ROBOTGO_DISABLE_PORTAL=1`
  - Disables portal capture and consent prompts; useful for deterministic native-backend tests.
- `ROBOTGO_WAYLAND_BACKEND`
  - Overrides Linux capture backend selection (`auto|dmabuf|wl_shm|screencast|portal`).
- `ROBOTGO_SCREENCAST_E2E=1`
  - Enables the real persistent ScreenCast/PipeWire integration test.
- `ROBOTGO_CAPTURE_DEBUG=1`
  - Enables backend/fallback diagnostic logs for capture flow.
- `ROBOTGO_WLROOTS_MINMAX_E2E=1`
  - Opt-in for wlroots active-window minimize/maximize E2E integration (`MinWindowE(0)`, `MaxWindowE(0)`).
- `ROBOTGO_SWAY_TITLE_E2E=1`
  - Opt-in for sway active-window title E2E integration (`GetTitleE`).
- `ROBOTGO_HYPRLAND_TITLE_E2E=1`
  - Opt-in for hyprland active-window title E2E integration (`GetTitleE`).
- `ROBOTGO_HYPRLAND_MAXIMIZE_E2E=1`
  - Opt-in for Hyprland active-window maximize query/set/restore integration.
    The test refuses to alter an initially fullscreen window and restores an
    initial normal or maximized state during cleanup. It exercises the
    provider-aware dispatcher selected by `hyprctl status -j`, including
    Hyprland 0.55+ Lua configurations.
- `ROBOTGO_REQUIRE_X11_INTEGRATION=1`
  - Makes missing `DISPLAY` or XTEST support fail the X11 integration suites;
    CI always enables it.
- `ROBOTGO_X11_INPUT_BENCHMARK=1`
  - Enables the X11 input benchmarks. Prefer the balanced comparison script, which sets this
    together with the capture benchmark and implementation identity.

## Recommended Local Sequence

```bash
go test ./...
CGO_ENABLED=0 go test ./...
go test -tags "wayland" ./...
go test -tags "portal" ./screen/portal -v
go test -tags "pipewire" ./screen/portal -v
go test -tags "wayland test" ./screen -run 'TestScreencopy(BitmapStringHelper|WlShm|PortalFallback)' -v
go test -tags "wayland integration" . ./mouse ./window -v
```

Run tag-gated suites as needed for the area you changed. Native or Pure-Go X11
input changes must also run the required `x11integration` comparison command
above.

## Release Evidence

The `Release evidence` workflow validates its schema on every pull request and
`main` push. Published releases and manual dispatches additionally run the
default suite for native CGO and Pure-Go builds on Linux, macOS, and Windows,
then verify and package all six evidence cells.

On a clean Linux native checkout, reproduce the generator/verifier path with:

```bash
set -euo pipefail
output=/tmp/robotgo-release-evidence-local
rm -rf "$output"
mkdir -p "$output"
test_command='go test -count=1 ./internal/releaseevidence ./internal/cmd/releaseevidence'
go test -count=1 \
  ./internal/releaseevidence ./internal/cmd/releaseevidence \
  2>&1 | tee "$output/test.log"
test -z "$(git status --porcelain --untracked-files=all)"
go run ./internal/cmd/releaseevidence generate \
  -out "$output/evidence.json" \
  -test-log "$output/test.log" \
  -commit "$(git rev-parse HEAD)" \
  -tree "$(git rev-parse 'HEAD^{tree}')" \
  -ref "refs/heads/$(git branch --show-current)" \
  -run-id 1 \
  -run-attempt 1 \
  -matrix linux-native-validation \
  -test-command "$test_command" \
  -expected-cgo true
go run ./internal/cmd/releaseevidence verify \
  -evidence "$output/evidence.json" \
  -expected-matrix linux-native-validation \
  -expected-commit "$(git rev-parse HEAD)" \
  -expected-tree "$(git rev-parse 'HEAD^{tree}')" \
  -expected-ref "refs/heads/$(git branch --show-current)" \
  -expected-test-command "$test_command"
```

The versioned schema, matrix, release-asset behavior, and consumer verification
commands are documented in
[Release Evidence v1](docs/compatibility/release-evidence-v1.md).

## Sequential Crash-Tracking Run (No Parallelism)

When debugging intermittent crashes/aborts, run tests sequentially and persist
the currently running package:

```bash
set -euo pipefail
export GOCACHE=/tmp/robotgo-gocache
export GOMODCACHE=/tmp/robotgo-gomodcache
mkdir -p "$GOCACHE" "$GOMODCACHE" docs/plan
STATE_FILE="docs/plan/last-running-test.txt"
HIST_FILE="docs/plan/test-run-history.log"
: > "$HIST_FILE"

for pkg in $(go list ./...); do
  ts=$(date -Iseconds)
  printf "%s RUNNING %s\n" "$ts" "$pkg" | tee "$STATE_FILE" | tee -a "$HIST_FILE"
  go test -count=1 -p 1 -parallel 1 "$pkg" 2>&1 | tee -a "$HIST_FILE"
  ts=$(date -Iseconds)
  printf "%s PASS %s\n" "$ts" "$pkg" | tee -a "$HIST_FILE"
done

printf "%s COMPLETE all-packages\n" "$(date -Iseconds)" | tee "$STATE_FILE" | tee -a "$HIST_FILE"
```

After a crash, inspect:
- `docs/plan/last-running-test.txt` for the package that was active.
- `docs/plan/test-run-history.log` for the last emitted test output.
