# Testing Guide

This repository has both default tests and special test suites behind build tags.

Before pushing a branch, run the repository-local CI predictor:

```bash
bash scripts/run-local-ci-preflight.sh
```

It checks formatting and diff hygiene, module integrity, workflow syntax,
native and Pure-Go default tests/vet/lint, reconstruction of every checked-in
API baseline supported by the current native host plus all cross-compiled
Pure-Go variants, and the generated runtime-support contract. It deliberately
fails when the pinned
`golangci-lint` version is missing or different. Native CGO macOS/Windows
execution and real compositor evidence still require their hosted jobs, but a
missing or malformed native API baseline is rejected locally before a push.
The default suites never post desktop input or change the clipboard. Legacy
live desktop coverage is opt-in and requires both the `desktopintegration`
build tag and `ROBOTGO_REQUIRE_DESKTOP_INTEGRATION=1`; run it only on a
disposable, self-owned desktop. Clipboard and pointer state are restored by
test cleanup.

## Default Test Suite

Run this first on any platform:

```bash
go test ./...
```

This is the baseline suite used for regular development and should stay green.
It is safe to run from an interactive developer session: real desktop
mutation is excluded unless the explicit integration tag and environment gate
are both enabled.

On a disposable, self-owned native Windows desktop, the protected Windows job
also runs pointer and capture behavior with cleanup enabled:

```bash
ROBOTGO_REQUIRE_DESKTOP_INTEGRATION=1 \
  go test -tags desktopintegration \
  -run '^(TestColor|TestSize|TestMoveMouse|TestMoveMouseSmooth|TestDragMouse|TestMoveRelative|TestMoveSmoothRelative|TestImage)$' \
  -count=1 -timeout=60s -v .
```

Do not set this opt-in variable on a developer workstation. Pointer state is
restored and captured images exist only under `t.TempDir`, which Go removes
after the test.

The stable public Go API is an exact, blocking contract across native Linux,
Wayland/portal/PipeWire, Pure-Go Linux, Windows, and macOS. Run the locally
supported matrix with:

```bash
go run ./internal/cmd/apicompat \
  -variant linux-cgo \
  -variant linux-cgo-wayland \
  -variant linux-cgo-portal \
  -variant linux-cgo-pipewire \
  -variant linux-cgo-full \
  -variant linux-nocgo \
  -variant linux-nocgo-arm64 \
  -variant windows-nocgo \
  -variant windows-nocgo-arm64 \
  -variant darwin-nocgo \
  -variant darwin-nocgo-amd64
```

The OCR CI job separately checks `linux-cgo-ocr` after installing Tesseract
and Leptonica development files. The protected native macOS and Windows
default-build jobs separately check `darwin-cgo` and `windows-cgo` on matching
hosted runners. Any package-name, exported API, or newly discovered public
library-package drift fails until the affected generated manifest is updated
and reviewed. See
[Public Go API Compatibility](docs/compatibility/public-api.md) for scope,
exclusions, and the update command.

The versioned platform-support contract is also machine-checked:

```bash
go run ./internal/cmd/supportmatrix
```

It rejects unknown evidence names, a `supported` row without release-blocking
checks, and any row that mixes supported and pending semantics. After an
intentional edit to
[`runtime-v1.json`](docs/compatibility/runtime-v1.json), regenerate its
human-readable table with `go run ./internal/cmd/supportmatrix -write`; both
files must be reviewed together.

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

Process-termination validation is tested only through an injected fake killer:
the suite proves that zero, negative, and platform-overflow PIDs are rejected
without sending a real signal or terminating a process.

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
  scripts/run-wayland-sanitizer-tests.sh
```

The tagged tests use hermetic Wayland protocol servers and must all pass; CI
checks their manifest so a missing or renamed ownership test cannot silently
reduce coverage. The gate covers allocation/free, timeout cleanup, and file
descriptor ownership without requiring a graphical session or render node.
The runner prints both listing and execution diagnostics before returning the
original failing command's status, so transient sanitizer failures retain their
actionable output.

The non-CGO suite runs on Linux, macOS, and Windows in CI. It also verifies
runtime build/feature introspection, pixel-color parity, and hermetic Pure-Go
capture dispatch for CoreGraphics, X11, Windows, and the Wayland screenshot
portal. macOS tests use fake CoreGraphics bindings for deterministic permission,
pixel, bounds, Retina display-mode scale, and resource-lifecycle coverage. The
macOS non-CGO leg also resolves the real CoreGraphics display-mode symbols and
queries the active display scale as a blocking test. It additionally resolves
the real Quartz keyboard/pointer and Accessibility symbols and performs a
non-prompting permission preflight without posting input. None of these runtime
checks requires a Screen Recording or Accessibility grant. They are the
blocking evidence for the consent-free macOS support rows only; they do not
promote permission-granted capture, input mutation, or window control into the
RC-supported scope.

Default Linux screen tests use hermetic portal fixtures rather than persisting
the developer's real desktop. Portal regression tests require temporary
screenshot files to be absent after successful decoding and after decode
failures. The production portal reader unlinks the sensitive file immediately
after an identity-verified open. Hermetic tests also cover cancellation,
symlink and replacement rejection, sparse oversized files, and PNG headers that
would otherwise request excessive decoder allocation; they never capture the
developer's desktop.

The command-backed OCR cancellation test waits for its fake backend to publish
the private image path before canceling. It then requires bounded command
termination and verifies that the temporary OCR image no longer exists; fixed
startup sleeps or OCR-operation retries are not used.

Unix clipboard read/write cancellation tests likewise wait for a fake command's
readiness before canceling, cover already-canceled contexts separately, and use
only fixed fixture text without reading or changing the developer's clipboard.

Shared external-command lifecycle tests launch only private `t.TempDir()`
backends. They verify that descendants holding inherited stdin/stdout cannot
extend a call beyond the named cleanup delay and that the isolated Unix process
group is gone afterward; they do not access real OCR, clipboard, compositor, or
desktop data.

Agent-session unit tests use an in-memory input driver and never contact or
mutate the desktop. They cover process-exclusive lifecycle, concurrent close,
strict request validation, policy/confirmation/display/text/action limits,
live display-bound enforcement, dry-run, quota handling, sanitized results,
backend errors, bounded synthetic capture, defensive pixel ownership and
zeroing, stale-target rejection, changed/unchanged verification, timeout and
attempt bounds, explicit-observation color search, bounded region waits, query
and observation quotas, bounded semantic UI-tree projection, sensitive-text
redaction, native-reference zeroing, no-match/timeout cleanup, payload-free
audit events, and the documented input and capture cancellation boundaries.
The Linux AT-SPI unit suite uses an in-memory query fixture to verify exact
process/title matching, role/property minimization, hidden-subtree pruning,
fixed role/state/action mapping, and hard identity/tree limits. The shared
Windows UI Automation tree fixture additionally proves that password,
offscreen, disallowed-role, and foreign-process elements receive no content
read, all acquired native references are released on errors and limits, and
opaque runtime IDs stay bounded. The macOS AX tree fixture proves the same
privacy boundary for secure, hidden, offscreen, disallowed-role, duplicate,
and foreign-process elements; its opaque references contain only the selected
PID, CGWindowID, and bounded child-index path, never an AX pointer. No agent or accessibility unit test reads or
persists the developer's desktop, clipboard, OCR input, accessibility content,
or other private data:

```bash
go test -race ./agent
go test -race ./internal/accessibility
CGO_ENABLED=0 go test ./agent
```

The protected Windows job creates only one in-process Win32 window with fixed
button, input, and password fixture text. It verifies the real UI Automation
adapter, password redaction, process scope, and cleanup without screenshots,
files, clipboard access, or foreign-window discovery:

```powershell
$env:ROBOTGO_REQUIRE_WINDOWS_ACCESSIBILITY_INTEGRATION = "1"
go test -tags windowsintegration ./internal/accessibility `
  -run "^TestWindowsUIAInspectsOnlySelfOwnedBoundedFixture$" `
  -count=1 -timeout=30s -v
```

The Linux/Windows/macOS semantic-UI example is an explicit real accessibility read.
Use it only for a self-owned process/window after checking its PID, HWND, or
CGWindowID and
exact title. It keeps native references in memory until release/session close,
emits JSON only to standard output, and never writes an accessibility dump or
screenshot:

```bash
go run ./examples/semantic_ui -pid 1234 -title 'Self-owned fixture' -confirm
```

macOS unit and cross-build checks are blocking, including fixed role/action
mapping, bounded reference paths, native framework symbol resolution, and the
non-prompting permission contract. A permission-granted real semantic fixture
is evidence-pending with the other LAB-69 macOS GUI checks; hosted CI must not
turn missing Accessibility consent into a passing runtime claim.

The MCP adapter suite uses the official SDK's paired in-memory transports and a
fake session. It performs real protocol initialization, listing, typed calls,
find/wait projection, opt-in image-content transfer, independent observation
release, cancellation, concurrent close, schema rejection, output-redaction,
and transport-cleanup checks without reading or changing the developer's
desktop. Synthetic PNGs and captures exist only in memory; tests prove
redaction-before-downscale, rejection of ancillary metadata, single ownership
transfer, owned-byte zeroing after SDK serialization, batch-safe transport
behavior, and raw-observation cleanup. The command tests use only private
`t.TempDir()` policy fixtures, which test cleanup removes:

```bash
go test -race ./agent/mcpserver ./cmd/robotgo-mcp
CGO_ENABLED=0 go test ./agent/mcpserver ./cmd/robotgo-mcp
```

No MCP test starts stdio, opens portal consent, reads developer pixels, injects
input, or persists protocol data. The explicit portal-start test uses only an
in-memory lifecycle fake. Condition and image fixtures stay in memory, and
serialized results are checked for target-color, tolerance, digest, duplicate
image, metadata, and backend payload leakage.

The opt-in runtime path performs one real pointer move to explicit coordinates:

```bash
ROBOTGO_AGENT_INPUT_E2E=1 \
ROBOTGO_AGENT_INPUT_X=100 ROBOTGO_AGENT_INPUT_Y=100 \
ROBOTGO_AGENT_INPUT_DISPLAY=0 \
go test -tags integration ./agent -run TestAgentSessionMoveRuntime -v
```

Run it only in a graphical session where global pointer movement is intended.
It creates no screenshot, clipboard, OCR, or other persistent artifact.

The separate opt-in capture path reads one explicit real region into memory,
checks its dimensions, searches its retained observation, performs one bounded
single-attempt region wait, and zeroes every returned copy and session-owned
buffer on every test cleanup path. The wait uses maximum RGB tolerance so the
test does not retain or print a real desktop color. It never writes a screenshot
to disk and, on Wayland, never opens portal consent implicitly:

```bash
ROBOTGO_AGENT_CAPTURE_E2E=1 \
ROBOTGO_AGENT_CAPTURE_X=0 ROBOTGO_AGENT_CAPTURE_Y=0 \
ROBOTGO_AGENT_CAPTURE_WIDTH=320 ROBOTGO_AGENT_CAPTURE_HEIGHT=200 \
ROBOTGO_AGENT_CAPTURE_DISPLAY=0 \
go test -tags integration ./agent -run TestAgentSessionCaptureRuntime -v
```

Start a consent-aware ScreenCast session before that command when portal
capture is required, or explicitly set `ROBOTGO_DISABLE_PORTAL=1` to test only
a native capture path.

The visual-condition example is inspection-only unless `-allow-capture` is
supplied. Both active modes keep pixels in memory and create no screenshot or
template file:

```bash
go run ./examples/agent_conditions
go run ./examples/agent_conditions -allow-capture -mode find \
  -red 0 -green 120 -blue 255 -tolerance 0.05 \
  -x 0 -y 0 -width 320 -height 200 -display 0
```

Real post-action changed/unchanged verification is intentionally not automated
in this integration suite: it would require mutating and repeatedly inspecting
uncontrolled developer desktop content. Hermetic synthetic-driver tests cover
the complete stale/pass/fail/timeout/cancel contract instead.

Linux alert tests replace every external dialog backend through a private test
`PATH`. They verify fallback order, user rejection, missing/failed backends, and
the non-interactive notification boundary without displaying real UI.
The shared native-result tests also keep macOS and Windows backend failures
distinct from a user rejecting the dialog.

The default CGO macOS and Windows pointer checks preserve and verify restoration
of the original pointer location. They use bounded observation polling instead
of fixed post-event sleeps and do not retry an injected event to manufacture a
passing result. Absolute, relative, and smooth moves must all reach their exact
requested endpoint.

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
implemented / evidence pending until
[LAB-69](https://linear.app/riotbox/issue/LAB-69/add-permission-granted-self-owned-macos-runtime-evidence)
is reactivated and provides an isolated macOS test-window harness. Tests must
not mutate an unrelated developer window.

Pure-Go `CloseWindowKill` tests use fake window/process backends. They cover
PID, handle and active-window resolution, graceful exit, the bounded force-kill
fallback, pre-close stable Linux `pidfd` binding, post-bind owner
revalidation, pre-bind process-identity changes, deadline races, and
fail-closed probe errors without terminating a real process:

```bash
CGO_ENABLED=0 go test \
  -run '^(TestCloseWindowKill|TestCloseWindowProcessIdentity|TestOpenCloseWindowProcessLinux|TestCloseWindowProcessLinux|TestWaitForWindowProcessExit)' \
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

These integration tests are not part of the release gate because GitHub-hosted
macOS does not grant Accessibility control to repository test binaries. Run
them only after a LAB-69 reactivation condition provides an isolated
project-owned, trusted maintainer/community, donated/sponsored, or officially
permission-capable GitHub-hosted environment. They must use synthetic fixtures,
must never target a developer desktop or unrelated content, and must clean up
all artifacts and restored state on every exit path. The non-prompting symbol
and permission contract remains blocking on the current hosted runner.

Linux CI additionally runs the non-CGO X11 input backend against a real Xvfb
server with XTEST 2.2 or newer and `us,de` keyboard layouts. A separate X11
evidence job runs one shared public-API contract against native CGO and Pure-Go
binaries, verifies both backends' safety-specific contracts, and compiles/runs
every balanced-comparison benchmark once. The same job starts a reachable Xvfb with XTEST disabled
and verifies that native readiness rejects it without injecting input. Missing
runtime prerequisites fail instead of turning these checks into successful
skips. Performance numbers are report-only; correctness is blocking. Repository
branch protection requires the public API gate, stable three-OS, lint, vet,
race, sanitizer, Wayland, and X11 evidence checks. The exact release gate
additionally requires hosted Sway, Hyprland, and GNOME/KDE multi-output
portal/bounds evidence. Permission-granted macOS remains explicitly
evidence-pending and outside the supported RC scope; optional operator-driven
jobs cannot silently promote it.

The additive `X11 default suite` workflow runs the complete CGO default suite
inside a private GitHub-hosted Xvfb and explicitly proves that the display-aware
screen-size, scale, and title tests pass rather than skip. It retains no
synthetic desktop artifact. Its stable `x11-default-suite` check is required by
`main` branch protection and the exact-commit Release Evidence contract.

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
value during cleanup and verifies the restoration without logging clipboard
contents. CI repeats this test three times as a blocking Windows non-CGO check
so focus recovery, missing EDIT-control delivery acknowledgement, and cleanup
are exercised across independent runs. A failed paste is retried at most once,
and only after the owned window reports either focus loss or no `EN_CHANGE`
acknowledgement at all. Any unexpected edit mutation and every persistent
delivery failure remain blocking.

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
- Hermetic native absolute-pointer mapping for negative aggregate origins and
  exclusive desktop edges
- Bounded Wayland input flush retries for transient `EAGAIN`, interrupted
  waits, explicit still-queued delivery errors, and permanent transport
  failures
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

### Hosted GNOME/KDE display bounds

Purpose:

- Exercise the public native-CGO and Pure-Go Wayland output APIs against the
  same real two-output GNOME or KDE compositor session.
- Require exact per-output bounds, aggregate desktop bounds, primary size,
  display count/main index, invalid-index behavior, and legacy/error API parity.
- Prove the test process is Wayland-only (`DISPLAY` unset) and does not open a
  RemoteDesktop, ScreenCast, screenshot, input, or consent session.

`.github/workflows/display-bounds-e2e.yml` runs the proof in disposable
GitHub-hosted nested-KVM guests. The manifest-bound topology is a 1280x720
primary at `(0,0)` and a 1024x768 secondary at `(1280,0)`. Each desktop job
first runs the CGO native client with `wayland,hostedboundsintegration`, then
rebuilds the same public contract with `CGO_ENABLED=0`. Missing output
protocols, an unexpected topology, X11 exposure, a skipped test, or surviving
runner state fails the job. The workflow is credential-free inside the guest,
uses no portal consent marker, captures no pixels, and removes its VM, source
archive, logs, and runner binary on every completion path.
Retained exact-commit
[`Display Bounds E2E` run 30268702514](https://github.com/marang/robotgo/actions/runs/30268702514)
passes both GNOME and KDE lanes on `8f12eacf640a903fb078b598085b3664892a40a8`.
Both reusable lanes are required by Release Evidence for the exact candidate
commit and recorded in its protected-check manifest.

The nested guests run automatically only on trusted `main` pushes or an
explicit workflow dispatch; pull requests do not boot them. Local compile
coverage for the two build variants is:

```bash
go test -run '^$' -tags "wayland hostedboundsintegration" .
CGO_ENABLED=0 go test -run '^$' -tags "hostedboundsintegration" .
```

### RemoteDesktop portal input

Purpose:

- Hermetic RemoteDesktop request/session lifecycle coverage
- Consent response, denial, timeout, portal closure, device grants, and cleanup
- Direct pointer and keyboard notification dispatch
- Shared RemoteDesktop/ScreenCast negotiation, stream metadata, absolute
  pointer coordinates, optional touch, and restore-token handling
- High-level CGO and non-CGO fallback dispatch after explicit consent
- Native-to-portal absolute fallback preserves caller global coordinates even
  when legacy native scaling is enabled
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
so an available native Wayland backend cannot mask a broken portal path.
Default CI compile-checks this harness without opening a consent session.
`.github/workflows/remote-desktop-e2e.yml` runs GNOME and KDE inside disposable
nested-QEMU guests on GitHub-hosted Ubuntu. wlroots native and
portal-availability evidence is promoted through the separate P005 runner path
and is not a RemoteDesktop pass. The exact-release workflow reuses the
multi-output GNOME and KDE jobs and requires both to pass for its candidate
SHA. The portal client is pure Go and therefore
independent of the root package's CGO setting; CGO and non-CGO high-level
fallback dispatch remains covered by the hermetic root tests. Both desktop
lanes run automatically for pushes to `main` and can be selected manually with
`gnome`, `kde`, or `all`; the manual `topology` input selects `single-output`,
`multi-output`, or both. The two-output lane configures a 1280x720 primary at
`(0,0)` and a 1024x768 secondary at `(1280,0)`, requires two unique physical
monitor streams, and exercises absolute input against each stream. Pull
requests do not boot the nested guests. The
workflow uses read-only permissions, retains no checkout credentials, and
registers no self-hosted runner.

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
closes the PipeWire consumer before the portal session. The second capture
also covers compositors that suppress unchanged frames: the backend waits one
short poll for a fresh frame and then returns an owned copy of the latest frame.
With `ROBOTGO_PORTAL_MULTI_OUTPUT=1` and the canonical expected-output manifest,
the integration test instead requires two unique physical monitor streams and
captures one owned, non-empty PipeWire frame from each stream.
`.github/workflows/screencast-e2e.yml` runs GNOME and KDE in the same
disposable GitHub-hosted nested-QEMU model. Both run on `main` pushes and on
manual `gnome|kde|all` and single-/multi-output dispatches. The exact-release
workflow reuses and requires both multi-output jobs for its candidate SHA.
KDE's pinned KWin
helper reports only
virtual-screen, active-dialog, and pointer geometry through private D-Bus. The
host validates a plausible dialog, scrolls the pinned CardsGridView, selects
both physical monitor cards through digest-bound targets, verifies that QMP
reached the initial target, and accepts through the portal's standard Return
path. GNOME's two monitor buttons are selected through manifest-bound QMP
pointer coordinates derived from its pinned 46.2 dialog contract. The
integration test rejects missing, duplicate, virtual, or unexpected streams;
optional stream metadata is interpreted according to the negotiated ScreenCast
interface version.
wlroots does not count as a ScreenCast pass and is promoted separately under
P005.

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
- A Wayland-enabled CGO build running against Xvfb, proving that the build tag
  does not remove `Capture` or `CaptureImg` from a real X11 runtime
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

Wayland-enabled build on an X11 runtime:

```bash
CGO_ENABLED=1 \
  xvfb-run -a -s "-screen 0 640x480x24 -nolisten tcp -noreset" \
  sh -eu -c '
    unset WAYLAND_DISPLAY XDG_SESSION_TYPE
    go test -tags "wayland x11integration" \
      -run "^TestWaylandBuildPreservesX11Capture$" \
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
- `ROBOTGO_COMPOSITOR_OUTPUT_COUNT`
  - Protected-runner declaration of the fixed fixture output count. The shared
    compositor preflight rejects missing, non-positive, or insufficient values.
- `ROBOTGO_COMPOSITOR_OPERATOR_READY_FILE`
  - Absolute path to the orchestrator-owned portal-consent readiness
    attestation. The root-owned file and its root-owned parent directory must
    not be writable by group or others. Its exact single-line value binds
    `ready` to the approved commit, GitHub run ID/attempt, lane, and cell;
    repository code must not create or refresh it.

    ```text
    ready commit=<approved-sha> run=<run-id> attempt=<attempt> lane=<gnome|kde> cell=<remote-desktop|screencast>
    ```
- `ROBOTGO_CAPTURE_DEBUG=1`
  - Enables backend/fallback diagnostic logs for capture flow.
- `ROBOTGO_WLROOTS_MINMAX_E2E=1`
  - Opt-in for wlroots active-window minimize/maximize E2E integration (`MinWindowE(0)`, `MaxWindowE(0)`).
- `ROBOTGO_SWAY_TITLE_E2E=1`
  - Opt-in for sway active-window title/PID E2E integration (`GetTitleE`,
    `GetPidE`).
    The protected hosted-Sway `native-window` cell additionally proves exact
    `GetBoundsE`/`GetClientE` node/client geometry against a self-owned,
    borderless fixture
    and removes the fixture and private compositor runtime on every exit path.
- `ROBOTGO_HYPRLAND_TITLE_E2E=1`
  - Opt-in for hyprland active-window title/PID E2E integration (`GetTitleE`,
    `GetPidE`).
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
go test . -run '^(TestRunBoundedWindowCommand|TestRunWindowCommandWithin|TestWindowBackendCommandErrors|TestWindowCommand)' -count=1 -v
CGO_ENABLED=0 go test -tags "waylandoutputintegration" . \
  -run '^TestPureGoWaylandOutputEnumerationWeston$' -count=1 -timeout=30s -v

ROBOTGO_REQUIRE_WAYLAND_OUTPUT_INTEGRATION=1 \
  xvfb-run -a -s '-screen 0 1920x1080x24 -nolisten tcp -noreset' \
  env CGO_ENABLED=0 \
  go test -tags "waylandoutputintegration" . \
    -run '^TestPureGoWayland.*EnumerationWeston$' \
    -count=1 -timeout=30s -v
```

The non-CGO Wayland output suite is hermetic and deliberately sets both
`WAYLAND_DISPLAY` and `DISPLAY` to prove the X11 backend is never contacted.
Its mock compositor covers fragmented wire frames, bounded stalls and socket
cleanup, protocol-version clamping, logical multi-output geometry, negative
origins, scale, transforms, deterministic indices, and explicit errors:

```bash
CGO_ENABLED=0 go test ./internal/waylandoutput -count=1 -v
CGO_ENABLED=0 go test -run '^TestPureGoWaylandBounds|^TestPureGoCaptureAliasUsesWaylandPortal' .
```

The first tagged command starts a single-output headless Weston instance. The
second additionally starts Weston under Xvfb with two virtual outputs, scale
factor 2, and a 90-degree transform. It verifies exact logical per-output and
aggregate bounds through the public error-returning APIs. Both instances use a
test-owned `XDG_RUNTIME_DIR`, terminate and wait for Weston, and let `t.TempDir`
remove the socket and all runtime files. The Xvfb wrapper removes its temporary
authorization data. Neither test captures nor writes desktop image data. CI
requires both tests to pass by setting
`ROBOTGO_REQUIRE_WAYLAND_OUTPUT_INTEGRATION=1`; missing Weston, Xvfb, outputs,
or protocol data is a failure rather than a skip.

Run tag-gated suites as needed for the area you changed. Native or Pure-Go X11
input changes must also run the required `x11integration` comparison command
above.

The focused window-helper command runs are hermetic and do not require a
compositor. On Linux with CGO, they create test-only commands and PID records
under `t.TempDir()`, then prove that timeouts and inherited-I/O failures remove
the complete owned process group. Defensive `t.Cleanup` handlers repeat the
termination on every test exit; no desktop data or command artifact survives
the test.

## Real-Compositor Evidence

The `Sway E2E` workflow runs a nested Sway 1.9 compositor on an ephemeral
GitHub-hosted Ubuntu 24.04 runner. Every matrix cell receives a private
`XDG_RUNTIME_DIR`, the headless wlroots backend, the Pixman renderer, no
libinput devices, and no X11 display. Five cells retain the fixed single-output
1280x720 topology for native input, capture, Sway window control, output
geometry, and explicit portal availability. A sixth `native-output-multi` cell
adds a second headless output and proves exact public per-output and aggregate
bounds with a negative origin, scale 2, and a 90-degree transform. Input targets
only a self-owned `wev` surface; capture uses a fixed synthetic `swaybg` color
and keeps all pixels in memory.

The workflow runs exactly one tagged test per cell:

```bash
go test -count=1 -timeout=2m -tags=wayland,swayintegration . \
  -run '^TestSwayNativeInputRuntime$' -v
```

Replace the test name with `TestSwayNativeCaptureRuntime`,
`TestSwayNativeWindowRuntime`, `TestSwayNativeOutputRuntime`, or
`TestSwayNativeOutputMultiRuntime` for the native cells, or with
`TestSwayPortalAvailabilityRuntime` for the availability cell. The
multi-output topology is a normal 1280x720 output at `(0,0)` plus a physical
1200x800 output at `(-600,0)` with scale 2 and transform 90; Sway exposes the
second output as a logical 400x600 rectangle and RobotGo must report aggregate
bounds `(-600,0) 1880x720`. The repository-owned
`scripts/run-sway-e2e.sh` wrapper is CI-specific: it verifies the exact commit,
starts the isolated compositor, runs fail-closed preflight, finalizes only a
canonical sanitized schema-v1 result, and kills both compositor and test
process groups before deleting the private runtime directory. A separate
induced-failure step creates the multi-output topology and proves that cleanup
path before the multi-output cell runs.

Only `evidence.json`, `test.log`, and `summary.md` are uploaded. Supervisor
output, raw test output, sockets, fixture logs, screen pixels, and portal
session data remain in runner-temporary memory/storage and are deleted on
success and failure. The hosted Sway job is safe for fork pull requests because
it has read-only permissions, no credentials, no physical input devices, and no
access to a developer or self-hosted desktop.

The separate `Hyprland E2E` workflow proves compositor-specific active-window
geometry on one GitHub-hosted `vkms` display. It loads and verifies exactly one
virtual DRM card on the disposable host, passes only that card into a pinned
Arch Linux container, and runs Hyprland without a network, `/dev/input`, render
node, host display socket, writable checkout, or credentials. The test owns its
`wev` fixture and verifies title, PID, exact bounds, legacy/error API parity,
and explicit unsupported handle/client/PID-target contracts under
AddressSanitizer and LeakSanitizer.

The runner also executes an induced failure after compositor startup and
requires compositor/seat-manager processes, sockets, raw logs, the private
runtime directory, and the dedicated container-transfer workspace to be gone.
The normal test separately proves cleanup of its self-owned fixture. Only
`evidence.json`, `test.log`, and `summary.md` are uploaded. Local development
should run only the side-effect-free contract and compile gates:

```bash
go test ./internal/compositorevidence ./internal/cmd/compositorevidence ./scripts
go test -run '^$' -tags='wayland,hyprlandintegration' .
```

Do not point `scripts/run-hyprland-container.sh` at a developer GPU. Real
execution belongs to the disposable hosted `vkms` runner documented in
[`infrastructure/hyprland-runner`](infrastructure/hyprland-runner/README.md).

The GNOME and KDE jobs build pinned images on fresh GitHub-hosted Ubuntu
runners, transfer only the exact clean commit through `git archive`, and
execute inside disposable guests with live Wayland/user-bus sessions. A private
readiness marker is created only after `CreateSession` and every `Select*`
request has completed, immediately before the dialog-producing portal `Start`.
The GNOME client stores the exact random path of the pending `Start` request in
a private target file and blocks. The controller validates that target, creates
a private start gate, and then accepts only the matching new request object;
older exported negotiation objects cannot satisfy the probe. The independent
host-side controller operates modal consent through QMP keyboard or
manifest-bound pointer input on GNOME. For KDE ScreenCast, an immutable guest
helper reports only
control geometry and the host performs both actions through QMP's private
pointer; KDE's native non-sandboxed RemoteDesktop backend uses its upstream
notification policy and therefore has no modal approval to drive.
Before sending GNOME's QMP input, the controller waits until the backend
exports the new transient `Start` request object that immediately precedes
dialog creation. The two-way marker/gate handshake proves the earlier non-modal
requests have completed and binds readiness to the exact random `Start` path.
The target and object tree remain inside the disposable guest; only an `ok`
marker or an allowlisted failure stage can reach the host. This removes the
startup race between the test's pre-`Start` marker and an on-demand portal
backend without inspecting any window title or content. For parentless GNOME
dialogs, the host first clicks the neutral center of the pinned dialog
headerbar through QMP's private tablet, then sends the documented button
mnemonics; multi-output ScreenCast already establishes focus through its first
physical-output card click.
RobotGo never approves its own request or patches a portal. No Actions token,
checkout credential, `.git` directory, untracked file, screen frame, restore
token, or raw log enters retained guest state.

Missing KVM capacity, portal/session readiness, consent, exact-commit match,
test pass, or cleanup fails the cell. VM destruction is the final
cancellation/timeout boundary, and an `always()` workflow step terminates only
verified runner-owned QEMU processes, removes sentinel-owned `run-*`
directories, and rejects leftovers.

KDE session readiness uses independent bounded budgets for SDDM/runtime/Wayland
and KWin, Plasma Shell, the portal frontend, and the KDE portal backend. This
prevents a slow early phase from consuming the shell's entire budget and then
being misreported as a shell failure. The complete contract must remain true
for three consecutive probes before source transfer begins. Failures expose
only an allowlisted stage such as `desktop-shell-never-seen` or
`portal-backend-unstable`; process arguments, journals, environment values, and
other guest data do not cross SSH. A naturally managed Plasma Shell or portal
frontend bounce may settle only inside the current bounded phase. If the
verified `plasma-plasmashell.service` reaches terminal `failed`, RobotGo may
reset that unit's failed/start-limit state under a three-second hard bound and
queue exactly one restart under a three-second hard bound. The helper then
waits at most 30 seconds for the user unit and process to become ready, emitting
only `ROBOTGO_SESSION_RECOVERY=desktop-shell`. A second failure is terminal,
and both winner and observers continuously revalidate the base session during
settlement. The final stability probes still reject a crash loop. Concurrent
waiters share attempt, completion, and failure markers. Each begins its full
30-second settle phase only after the winning bounded recovery completes. A
terminal failure
during stability repeats the portal phases before stability can pass. Recovery
and readiness claims are serialized under one 15-second-bounded guest lock:
a ready claim revalidates the complete contract inside the lock and prevents
any later restart, while a recovery claim forces every waiter to observe
completion and revalidate. A failed locked revalidation falls through to the
allowlisted terminal-stage classifier instead of resetting the phase budgets.
Reset, queue, and start failures receive distinct allowlisted stages shared by
all waiters. Claims and outcomes live only in the mode-0700 tmpfiles-managed
`/run/robotgo-session-state`, outside the per-user runtime directory, so even a
runtime-directory failure remains publishable and the state vanishes with the
guest. The normal KDE phase deadlines total 220 seconds; the one-recovery path
adds at most the three-second reset, three-second restart queue, 30-second shell
settle, fresh 30-second portal frontend, 30-second backend, and 10-second
stability cycle. The KDE host guard sends `TERM` after 380 seconds and enforces
`KILL` five seconds later, including bounded probe overhead beneath the
400-second systemd runner bound. GNOME retains its shorter 130-second deadline
with the same five-second kill-after beneath its 150-second systemd bound.

Hosted workflow calls share
`scripts/run_hosted_portal_e2e_ci.sh` as their fixed 30-minute outer guard.
Guest package/image installation has an earlier 20-minute phase deadline. If
image provisioning fails or stalls, the builder prints at most the final
64 KiB of its pre-registration build log before cleanup. Registration tokens,
screen frames, and input evidence cannot enter that log because runner
registration occurs only after the immutable image is complete. The normal
`always()` cleanup still removes the bounded log, VM state, and helper binary.
The GNOME image installs the focused `ubuntu-session`, GDM, Shell, and portal
contract, binds the disposable account to that session through AccountsService,
sets a fixed US XKB input source for the virtual keyboard, and rejects both
generic `gnome-session` and `ubuntu-desktop-minimal`. The KDE image likewise
installs the explicit Plasma Workspace/Wayland contract and rejects
`plasma-desktop`. This avoids pulling unrelated applications and services
through the throttled, reproducibility-pinned snapshot while retaining the
real desktop sessions under test. The large kernel-extra package remains
version- and SHA-256-pinned but is fetched first from an Ubuntu-listed HTTPS
mirror with a shorter budget, then from its immutable Launchpad artifact URL
as a verified fallback, instead of the throttled snapshot. Partial and
installed archives are removed from temporary guest storage. APT downloads
only signed package indexes, omitting translations, desktop metadata, and
command-not-found indexes that cannot affect the pinned headless image package
set.

The reproducible images, hosted supervisor, independent consent drivers,
exact-tree transfer, and mandatory cleanup checks are documented in
[GitHub-Hosted GNOME Portal Runner](infrastructure/portal-runner/gnome/README.md)
and [GitHub-Hosted KDE Portal Runner](infrastructure/portal-runner/kde/README.md).
`probe` remains an infrastructure diagnostic; `hosted-run` is the automated,
credential-free hosted integration path. Portal cells use the independent
consent controller; the display-bounds cell is read-only and consent-free.

wlroots is intentionally not treated as a RemoteDesktop/ScreenCast pass in
these portal workflows. Its hosted native and explicit portal-availability
evidence is promoted separately under the
[protected real-compositor plan](docs/plan/real-compositor-evidence.md).

Run the hermetic contract locally without touching the live desktop:

```bash
go test -race ./internal/compositorevidence \
  ./internal/cmd/compositorevidence
```

Default tests inject fixed command, desktop, portal, timeout, and filesystem
fixtures and use `t.TempDir()`. Do not invoke the real workflow commands on a
personal desktop: portal cells intentionally request real screen/input consent
and belong only on the disposable fixture images with the protected operator
handoff.

## Release Evidence

The `Release evidence` workflow validates its schema on every pull request and
`main` push. Published releases and manual dispatches additionally run the
default suite for native CGO and Pure-Go builds on Linux, macOS, and Windows,
then verify and package all six evidence cells. Its exact-commit protected
manifest also requires every hosted Sway cell: native input, capture, window,
single-output geometry, multi-output geometry, and portal availability. A
missing, pending, skipped, neutral, cancelled, timed-out, or failed Sway check
blocks the release bundle. It additionally invokes and requires the
multi-output GNOME and KDE RemoteDesktop and ScreenCast jobs for the same
candidate SHA. Missing, skipped, stale, or failed portal evidence therefore
also blocks packaging and publication. The consent-free GNOME and KDE
multi-output bounds jobs and isolated Hyprland window-geometry cell are invoked
for that exact candidate and are likewise required before the 29-check manifest
can be packaged.
The last pre-API-freeze 28-check contract passes on exact merged `main` in
[`Release Evidence` run 30272753885](https://github.com/marang/robotgo/actions/runs/30272753885).
The current post-freeze 29-check contract passes on the exact published tag in
[`v1.0.0-rc.1` Release Evidence run 30442843617](https://github.com/marang/robotgo/actions/runs/30442843617)
at commit `281d8cee29d696e334fe9d4a6f6a7069ab291083`.

On a clean Linux native checkout, reproduce the generator/verifier path with:

```bash
set -euo pipefail
output="$(mktemp -d "${TMPDIR:-/tmp}/robotgo-release-evidence-local.XXXXXX")"
cleanup() {
  rm -rf -- "$output"
}
trap cleanup EXIT INT TERM
chmod 700 "$output"
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

Before creating the stable tag, complete the documented seven-day RC
qualification window with no unresolved critical/high regression. A reviewed
stable-preparation PR must update the package version, tests, changelog, and
add the non-empty `docs/releases/v1.0.0.md` release notes. After that PR is
merged, run from the clean `main` worktree:

```bash
set -euo pipefail
git pull --ff-only origin main
test "$(git branch --show-current)" = main
test -z "$(git status --porcelain --untracked-files=all)"
test -s docs/releases/v1.0.0.md
commit="$(git rev-parse HEAD)"
test "$(git ls-remote --heads origin refs/heads/main | awk '{print $1}')" = "$commit"
./scripts/preflight-origin-release.sh v1.0.0 "$commit"
```

For `v1.0.0`, the preflight fails closed before
`2026-08-05T10:13:46Z`, the first instant after seven full days from the
published RC. It obtains the current time from the authoritative GitHub API
`Date` header over the existing authenticated HTTPS path rather than trusting
the operator machine's clock. Its regression suite covers one second before
the boundary, the exact boundary, later execution, missing or duplicate remote
time headers, malformed parsed time output, and remote/time-parser failure.
RC-tag preflight remains independent of the stable qualification gate.

The preflight checks `origin/main`, exact origin tag refs, GitHub tag refs, and
GitHub releases. It rejects a non-`marang/robotgo` remote. A colliding local
tag may have been fetched from `go-vgo/robotgo`; the preflight rejects it
without treating it as origin evidence. Inspect and explicitly delete that
non-authoritative local ref before rerunning the preflight, and never
force-replace it. Only after the stable-preparation PR, review, CI, and an exact
manual release-evidence run on that merged commit pass may a release operator
create the annotated tag, verify its peeled commit, and push that one ref:

```bash
set -euo pipefail
git tag -a v1.0.0 "$commit" -m "RobotGo v1.0.0"
test "$(git rev-parse 'v1.0.0^{}')" = "$commit"
git push origin refs/tags/v1.0.0:refs/tags/v1.0.0
```

Never use `git push --tags`. Create the GitHub stable release with
`--verify-tag`; its `release.published` event reruns exact-tag evidence and
attaches the checksum-bound archive:

```bash
set -euo pipefail
gh release create v1.0.0 \
  --repo marang/robotgo \
  --verify-tag \
  --title "RobotGo v1.0.0" \
  --notes-file docs/releases/v1.0.0.md
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
