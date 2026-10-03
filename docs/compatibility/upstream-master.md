# Upstream Compatibility Audit

RobotGo tracks useful changes from
[`go-vgo/robotgo`](https://github.com/go-vgo/robotgo) without treating upstream
`master` as an automatically trusted source. Each port must preserve this
fork's explicit error, lifecycle, platform-boundary, and test contracts.

Last audited upstream revision:
[`12f16b7c5d82b317f81da8b45a6984de2a740ec4`](https://github.com/go-vgo/robotgo/commit/12f16b7c5d82b317f81da8b45a6984de2a740ec4)
(upstream 2026-09-21; audited 2026-10-03 in
[LAB-236](https://linear.app/riotbox/issue/LAB-236)). The previous audit covered
`766c6abccc400cc9a2aa96481c2493355b93fe29` (2026-07-08).
Coordination: [P012 — Upstream Compatibility Refresh](https://linear.app/riotbox/project/robotgo-p012-upstream-compatibility-refresh-4078787a22a8).

| Upstream area | Fork status | Decision |
|---|---|---|
| Public helper names (`CmdV`, `Paste`, `Type`, `TypeDelay`, `ClickV1`, `MultiClick`, `Capture1`, `SaveCaptureGo`) | Compatible | Added as portable aliases or checked helpers without changing established signatures |
| Process termination validation | Superseded | The fork rejects unsafe PIDs and binds forced window termination to a verified stable process handle or `pidfd` |
| Keyboard release ordering and macOS click/movement fixes | Superseded | Equivalent or stronger ownership, rollback, ordering, and bounded-movement contracts are already tested |
| Pure-Go X11 main display ID | Intentionally different | The fork keeps display ID 0 as primary because its public capture IDs address Xinerama outputs; upstream indexes X protocol screens instead |
| Experimental Pure-Go Wayland screencopy | Not ported | September's frame timeout and format/flag fixes do not provide the fork's bounded setup, logical scale/transform/crop semantics, checked ownership and fallback contract; retain the established native/portal paths |
| Pure-Go Wayland output enumeration | Superseded | The fork implements a dependency-free, read-only `wl_output`/`xdg-output` client with bounded dial/read/write, validated wire frames and versions, logical multi-output geometry, deterministic primary-first indices, explicit errors, and hermetic plus Weston evidence |

The accepted output-enumeration slice does not change capture selection.
Additional Pure-Go Wayland protocol work remains eligible only as a hardened
backend: bounded cancellation, checked buffer arithmetic, deterministic
cleanup, logical multi-output geometry, transforms, explicit unsupported
errors, and hermetic plus real-compositor evidence are required before capture
code can replace or precede the current portal path.

## September 2026 refresh

The [seven-commit comparison](https://github.com/go-vgo/robotgo/compare/766c6abccc400cc9a2aa96481c2493355b93fe29...12f16b7c5d82b317f81da8b45a6984de2a740ec4)
contains five implementation commits and two PR merges across 29 files. Ports
adapt behavior to this fork's existing backends; they do not merge upstream's
experimental packages, module identity, dependency/version metadata or agent
instructions. Existing release tags and assets remain unchanged.

| Upstream commit | Result |
|---|---|
| `3cdf7bb6` — libei input/session and Wayland pointer fixes | Correct native and high-level portal horizontal scrolling to match the existing left-positive public contract; retain explicit capture consent, real-observation errors and validated logical output mapping |
| `d513e760` — cross-platform input/capture fixes | Honor native screencopy `YInvert`; complete Pure-Go Windows physical-key metadata, extended-key classification and `NumClear`; retain existing macOS ownership, key maps, scroll units and scale semantics |
| `4b7e5e3a` — PR #784 merge | Review boundary for the preceding two commits; no separate implementation |
| `90a28696` — input review repairs | Deduplicate Windows layout-implied modifier families against caller-selected sides; add non-BMP rejection coverage; retain explicit Wayland modifier ownership and no synthetic cursor location |
| `c5b0ffce` — libei smooth-move success handling | Not applicable: the fork returns backend errors and does not turn an unsupported smooth move into a successful absolute jump |
| `d5317eb6` — bitmap pointer arithmetic | Superseded by owned, validated buffers and copy-based BGRA conversion; the old scalar pointer loop is already absent |
| `12f16b7c` — PR #785 merge | Final pinned audit boundary; no separate implementation |

### Behavior and evidence decisions

| Area | Disposition | Fork contract and regression evidence |
|---|---|---|
| Windows physical keys | Adapted | `MapVirtualKeyW` supplies hardware scan metadata at `SendInput` without enabling scan-code injection or rewriting Unicode units. Extended flags survive main-key taps, held-key release, Close and rollback; `NumClear` resolves to `VK_CLEAR`. `upstream_compatibility_windows_test.go` covers the records, cleanup, no-translation case and non-BMP rejection. Mixed foreground/thread layout behavior is not newly claimed. |
| Windows modifiers | Adapted | Explicit sided modifiers satisfy layout-implied generic families; both deliberately requested sides remain distinct. Tests cover Shift/AltGr, aliases, argument order and foreign-held input. |
| Horizontal scroll | Adapted | High-level `Scroll`/`ScrollE` use positive horizontal steps for left, matching `ScrollDir`, X11 and the existing Windows/macOS backends. Native Wayland negates both horizontal axis fields; high-level portal mapping negates horizontal steps. The low-level portal protocol API is unchanged. Mock native-wire and CGO/non-CGO portal tests verify both directions and unchanged vertical signs. |
| Screencopy inversion | Adapted | The compositor's `YInvert` flag reverses the final selected source row after logical crop mapping for both SHM and DMA-BUF. `TestScreencopyYInvertPreservesLogicalCrop` and `TestWaylandPixelRowsHonorYInvert` cover asymmetric padded rows, scale/crops and supported formats. These are synthetic flag fixtures, not a claim that a real compositor emitted inversion during testing. |
| macOS event source | Retained | The Pure-Go backend owns a checked private source until Close. Upstream's per-event HID source and silent nil fallback would change ownership semantics; promotion requires isolated application/runtime evidence, still pending under LAB-69. |
| macOS key maps, scroll and displays | Already covered | ANSI punctuation/shift/keypad normalization, explicit unsupported keys, pixel scroll units, dynamic indexed display bounds and released Retina modes already have `internal/darwininput` and display-scale coverage. Do not replace them with silent index fallback or line-scroll units. |
| Windows scroll, DPI and display IDs | Already covered / intentionally different | Wheel signs and overflow checks are tested. Keep HWND-aware DPI and physical-pixel sizing; a positive indexed display must not silently become the whole virtual desktop. |
| Wayland concurrency and keyboard | Already covered | Separate owned connections, serialized operations, compiled XKB keymaps, modifier requests and explicit held-key ownership supersede upstream singleton locking and fixed-US key maps. Existing wire/output/keyboard integration tests remain authoritative. |
| Wayland capture and output geometry | Already covered | Bounded setup/dispatch, ARGB/XRGB/ABGR/XBGR conversion, logical scale/transform/crop mapping and deterministic multi-output indices remain unchanged. Do not import fabricated fallback dimensions or raw-mode unions. |
| Wayland window lifecycle and parsing | Already covered / inapplicable | Bounded compositor helpers target active/focused windows and return unsupported errors. There is no persistent upstream toplevel proxy table to import; native libwayland and the validated read-only wire decoder retain their existing failure contracts. |
| Portal consent, absolute input and geometry | Retained / already covered | ScreenCast sources are explicit. No-stream absolute input and monitor-hole targets return errors without injection; no corner parking, target clamping or capture-derived global bounds. Added hole/no-stream regression coverage. |
| Portal cursor and session state | Retained | Last-injected coordinates are not observations; Closed/transport failure ends the session until explicit restart. Do not silently reopen consent or synthesize successful smooth movement. |
| Portal keyboard and cleanup | Already covered | Unicode/control keysyms, intrinsic Shift and deduplicated modifiers are supported; control/invalid-rune regression coverage is expanded. Serialized, cancellable transactions and deterministic teardown stay unchanged. |
| Bitmap conversion | Superseded | `NewBitmap`, `ToRGBAGoE` and bitmap safety tests validate owned storage, row stride, overflow and BGRA channel conversion; upstream's pointer-helper replacement is unnecessary. |

Separate existing defects discovered by the audit remain explicit follow-ups:
[LAB-237](https://linear.app/riotbox/issue/LAB-237) validates native compositor
buffer dimensions/stride/pool sizes before allocation and copy;
[LAB-238](https://linear.app/riotbox/issue/LAB-238) closes CGO display-mode/DC
scale-query ownership gaps. Upstream does not fix these defects; neither is
claimed repaired by this refresh.
