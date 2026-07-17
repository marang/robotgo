# X11 Native CGO vs Pure-Go Evidence

This report tracks the Phase 3 decision evidence for Linux/X11. Correctness is
the gate; benchmark results describe trade-offs and do not select a backend on
their own.

## Shared behavioral contract

Both binaries must pass the same black-box test against one isolated Xvfb:

- runtime/display identity, feature-capability selection, and live
  keyboard/pointer readiness
- 640x480 capture bounds, known black/white pixels, and `BackendX11`
- absolute/relative pointer movement plus independent location observation
- click, persistent button, vertical-scroll event order, and the
  backend-specific horizontal-scroll contract (native delivery; explicit
  Pure-Go unsupported error)
- named-key press/release and canonical `Ctrl down, key down, key up, Ctrl up`
- ASCII text received by an independent XKB client
- byte-identical keyboard and modifier maps after cleanup
- readiness probes without input-state or keymap mutation

The native binary additionally proves that Unicode, modified keys, and complete
text it cannot safely represent return an error before any input event and
without changing the X11 keyboard map. It covers concurrent display
replacement/close with capture and input, including concurrent display-server
environment changes, rejects an invalid explicit display without falling
through to `DISPLAY`, and returns an explicit error for out-of-bounds pixel
capture. A separate negative contract verifies readiness failure against a
reachable server with XTEST disabled. The Pure-Go-only suite continues to cover
its broader Unicode scratch pool, delayed clients, foreign input ownership,
cleanup conflicts, reconnect, and event-drain stress.

## Reproduce

Install a C compiler, X11/XTest development files, `git`, `xvfb`, `xauth`,
`setxkbmap`, `xkbcomp`, `xkbcli`, and coreutils (`stdbuf` and `tee`), then run:

The crash-recovery contract also requires executable Linux procfs
(`/proc/self/exe` and readable `/proc/<pid>/task/<tid>/children`), Linux
abstract Unix sockets with `SO_PEERCRED`, and child-subreaper support. See the
[X11 integration prerequisites](../../TEST.md#x11integration-native-and-pure-go-x11-input)
for the complete runtime contract.

```bash
scripts/benchmark-x11-backends.sh /tmp/robotgo-x11-backend-evidence
```

Versioned decision evidence requires a clean worktree. The script fixes the
commit, creates an isolated detached worktree, verifies the module cache
against `go.sum`, builds both binaries there, and records the source fingerprint
plus tool/runtime, binary-build, and X11 library metadata without hostname or
`GOENV` path. Benchmark runtime knobs are normalized. Inspect compiler commands,
build settings, library paths, and flags in `metadata.txt` before publishing
because custom toolchains can still contain machine-specific values. For a
local, non-decision smoke against unfinished changes, explicitly set
`ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY=1`; such output must not be committed as
decision evidence and is aborted if the working source changes during the run.

The defaults build each test binary once and collect ten observations per
benchmark and implementation in balanced native-first/Pure-Go-first order,
using `500ms` per benchmark. The output contains exact shared and
backend-specific behavior logs, raw benchmark output, tool metadata, and a
table with median, Q1–Q3 spread, observation count, and median ratio.
`run-status.txt` remains incomplete until the whole run succeeds, and stale
result files are removed before dependency checks. The output directory is
script-owned, protected by a sentinel, and locked against concurrent writers;
a non-empty foreign directory is never modified. Only a clean detached snapshot
with at least five balanced cycles and a duration of at least `500ms`, expressed
as integer milliseconds, seconds, minutes, or hours, is marked decision-grade.
The current comparison requires 10 matching benchmark names in both
outputs, the expected sample count, and all three metrics (`ns/op`, `B/op`,
`allocs/op`). CI uses one iteration only to catch broken benchmark paths; its
self-contained summary is explicitly report-only and never fails on
elapsed-time ratios. The balanced comparison uses a normal XTEST-enabled
server; reproduce the separate disabled-XTEST
negative contract with the command in [TEST.md](../../TEST.md#x11integration-native-and-pure-go-x11-input).

## Interpretation rules

- A backend that fails behavior is not a candidate, regardless of speed.
- `B/op` and `allocs/op` cover Go allocations only. Native C/Xlib allocations
  are invisible to Go's benchmark accounting.
- `ClickLeft`, `KeyPressEnter`, and `TypeASCII8` retain user-visible hold delays;
  their results describe end-to-end latency. Toggle-pair benchmarks isolate
  more of the transport and safety-policy cost.
- Pure-Go performs checked XGB requests plus an explicit Unicode scratch pool,
  delayed-client synchronization, and conflict-aware scratch cleanup. Native
  X11 now also preserves foreign-held input and active safe modifier state, so
  timing differences must be interpreted against the remaining Unicode and
  lifecycle trade-offs rather than as a blanket ownership advantage.
- Shared CI runners are unsuitable for fixed performance thresholds. Compare
  balanced samples from the same machine and X server.

## Current decision

The current decision-grade sample measures commit
`817656f2d52140581f7f6c5535d86f050ee6663b`, including the re-executed Pure-Go
guardian. Its raw output, behavior logs, metadata, and completion record are in
[data/x11-2026-07-17-817656f](data/x11-2026-07-17-817656f/summary.md). Both
implementations passed the complete shared contract and their exact
backend-specific safety manifests; the Pure-Go manifest includes a real
application-process `SIGKILL` and verified guardian cleanup.

The measurement used a user-local extraction of the official Arch Linux
`xorg-server-xvfb` package `21.1.24-1` because the host did not have Xvfb
installed and system package installation was unavailable. The package archive
SHA-256 was
`7f2116f869aedf51eb899dcfee4cf1f3bf6f9f42c71e089dcdbc0907d529e985`.
Consequently, the exact temporary binary path is retained in metadata and the
system-package field is `unknown`; this does not affect the clean detached
source snapshot or the balanced same-server comparison.

| Criterion | Evidence | Decision impact |
|---|---|---|
| Capture | Pure-Go/native median latency ratio `3.420x`; 116 versus 2 Go allocations per operation | Native retains a material throughput and allocation advantage; guardian IPC is not involved in capture |
| Stateful buttons and keys | Pure-Go/native median latency ratios `5.078x` for button toggles, `10.183x` for key toggles, and `2.059x` for delayed key presses | The per-request guardian safety boundary remains measurable and native remains preferable for common input sequences |
| Scroll | Pure-Go/native median latency ratio `8.821x` for vertical scroll; Pure-Go horizontal scroll remains explicitly unsupported because X11 does not expose reliable wheel-button state | Native remains preferable for scrolling |
| Pointer queries and moves | Pure-Go/native median latency ratios are `5.608x` for location, `2.400x` for absolute movement, and `1.602x` for relative movement | IPC round trips dominate the remaining gap; native remains faster for all measured pointer operations |
| Delayed click and ASCII text | Ratios are `1.106x` and `1.331x`; configured user-visible delays dominate much of the end-to-end result | Keep the operations supported, but do not use their ratios alone for backend selection |
| Build and Unicode behavior | Pure-Go removes the C/Xlib build dependency and retains managed Unicode scratch mappings; native avoids server-global temporary mappings | Keep Pure-Go useful and supported for CGO-disabled builds |
| Lifecycle behavior | The Pure-Go safety manifest passes real application-process `SIGKILL` recovery with claim-checked, deadline-bounded guardian cleanup | The measured IPC cost buys a concrete crash-isolation guarantee absent from the historical direct-connection sample |

**Decision:** retain native CGO as the Linux/X11 default when CGO is enabled.
Keep the Pure-Go X11 backend as the supported CGO-disabled implementation. This
is not a rejection of Pure-Go: it preserves useful X11 automation without CGO
and now provides measured crash isolation, while native wins every current
latency and reported Go-allocation comparison. Native also avoids
server-global Unicode mappings entirely. The Pure-Go guardian restores those
mappings after a targeted application-process kill while it and a responsive
X server survive. Its cleanup is deadline-bounded and restores only an exact
unchanged, unpressed, non-modifier final image; foreign final images are
preserved, while an exact-image ABA replacement is not observable in X11.

Compared with the immediately preceding guardian sample at
[data/x11-2026-07-17-6c06469](data/x11-2026-07-17-6c06469/summary.md), reusing
the request timer, response channel, and frame-read buffer while avoiding
double payload marshaling reduces Pure-Go input allocations by `24–33%`.
Allocated Go bytes fall by `41–50%` for pointer/mouse operations and about
`19–20%` for keyboard/text operations. Balanced median latencies remain in the
same range, confirming that round trips—not these removed allocations—dominate
the remaining gap. The optimization preserves request correlation, fail-closed
response-ID handling, timeout teardown, and the full crash-recovery manifest.

The pre-guardian sample remains available in
[data/x11-2026-07-16-d5fd51c](data/x11-2026-07-16-d5fd51c/summary.md) as
historical evidence. Neither older sample may be presented as current
performance. Future work may reduce guardian round trips only if the same
behavior, race, and crash-recovery contracts remain blocking; the evidence does
not justify weakening the safety boundary or changing the default.
