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

The decision-grade sample for commit `d5fd51c72702a719fd60fb06e0ef246018dc8b4e`
is stored with its raw output, behavior logs, metadata, and completion record in
[data/x11-2026-07-16-d5fd51c](data/x11-2026-07-16-d5fd51c/summary.md). Both
implementations passed the complete shared contract and their exact
backend-specific safety manifests.

The table below is historical evidence for that exact commit. The current
Pure-Go backend subsequently moved its X11 connection into a re-executed
guardian and moved its state machine into a race-testable internal package.
Those changes close the targeted application-process-crash gap but add IPC, so
the stored measurements must not be presented as guardian-path performance.

| Criterion | Evidence | Decision impact |
|---|---|---|
| Capture | Pure-Go/native median latency ratio `3.493x`; 115 versus 2 Go allocations per operation | Native has a material throughput and allocation advantage |
| Stateful buttons and keys | Pure-Go/native median latency ratios `1.395x` for button toggles, `1.347x` for key toggles, and `1.627x` for key presses | Native remains preferable for common input sequences |
| Scroll at sampled commit | Pure-Go/native median latency ratios `2.017x` horizontal and `2.083x` vertical; the current shared comparison retains vertical only because Pure-Go now rejects unobservable horizontal button state explicitly | Native remains preferable; do not present the historical horizontal number as current supported behavior |
| Pointer queries and moves | Location is effectively equal at `1.016x`; Pure-Go absolute and relative move ratios are `0.565x` and `0.398x` | Pure-Go has a real pointer-movement advantage, but not enough to outweigh the broader default-path costs |
| Delayed click and ASCII text | Ratios are `1.016x` and `1.012x`; configured user-visible delays dominate | No meaningful default-selection signal |
| Build and Unicode behavior | Pure-Go removes the C/Xlib build dependency and retains managed Unicode scratch mappings; native avoids server-global temporary mappings | Keep Pure-Go useful and supported for CGO-disabled builds |
| Lifecycle risk at sampled commit | Pure-Go scratch mappings were conflict-aware, but abnormal process termination still lacked a scoped or crash-safe cleanup mechanism | Do not use this historical sample to claim guardian crash behavior or performance |

**Decision:** retain native CGO as the Linux/X11 default when CGO is enabled.
Keep the Pure-Go X11 backend as the supported CGO-disabled implementation. This
is not a rejection of Pure-Go: it already wins pointer-movement latency and
build portability, while native currently wins capture, most input operations,
and allocation cost. Native also avoids server-global Unicode mappings
entirely; the Pure-Go guardian now restores those mappings after a targeted
application-process kill while it and a responsive X server survive. Its
cleanup is deadline-bounded and restores only an exact unchanged, unpressed,
non-modifier final image; foreign final images are preserved, while an
exact-image ABA replacement is not observable in X11. Revisit the default only
after a new decision-grade sample measures the guardian path and passes the
same behavioral, race, and crash-recovery contracts.
