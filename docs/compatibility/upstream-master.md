# Upstream Compatibility Audit

RobotGo tracks useful changes from
[`go-vgo/robotgo`](https://github.com/go-vgo/robotgo) without treating upstream
`master` as an automatically trusted source. Each port must preserve this
fork's explicit error, lifecycle, platform-boundary, and test contracts.

Last audited upstream revision:
[`16930bad397dcd8c5c295dd0c9c70177a135d66f`](https://github.com/go-vgo/robotgo/commit/16930bad397dcd8c5c295dd0c9c70177a135d66f)
(upstream 2026-10-27; audited 2026-10-28 in
[LAB-426](https://linear.app/riotbox/issue/LAB-426), under
[LAB-425](https://linear.app/riotbox/issue/LAB-425)). The October audit compares
against `12f16b7c5d82b317f81da8b45a6984de2a740ec4` and fork
[`16cafc60870553b4a2885ca99c69e68981a489e4`](https://github.com/marang/robotgo/commit/16cafc60870553b4a2885ca99c69e68981a489e4)
(released v1.0.3). It records adoption decisions, not delivered runtime ports
or a new release.
Coordination: [P012 — Upstream Compatibility Refresh](https://linear.app/riotbox/project/robotgo-p012-upstream-compatibility-refresh-4078787a22a8).

## September compatibility baseline

The following historical results apply to the September audited revision,
[`12f16b7c5d82b317f81da8b45a6984de2a740ec4`](https://github.com/go-vgo/robotgo/commit/12f16b7c5d82b317f81da8b45a6984de2a740ec4)
(2026-09-21), audited 2026-10-03 in
[LAB-236](https://linear.app/riotbox/issue/LAB-236). October's API changes are
assessed separately below.

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

The audit also identified two fork defects, both subsequently repaired in
separate maintenance slices rather than by importing upstream changes:
[LAB-237](https://linear.app/riotbox/issue/LAB-237) added checked
row/pool/destination bounds, selected-offer isolation and exact SHM
mapping-length cleanup, with hermetic malformed-metadata and ownership
regressions ([implementation](https://github.com/marang/robotgo/commit/26ac3f8052f4e3fdb6d6460212ba645c5c0a3c4d));
[LAB-238](https://linear.app/riotbox/issue/LAB-238) released native display modes,
Xrm databases and Windows scale-query DCs, with a hermetic C lifecycle harness
([implementation](https://github.com/marang/robotgo/commit/8b4a6ced3b74690dc17eec68be47e98c3cc0f559),
[fork source](../../screen/scale_c.h), [tests](../../native_scale_c_test.go)).

## October 2026 refresh

The [pinned comparison](https://github.com/go-vgo/robotgo/compare/12f16b7c5d82b317f81da8b45a6984de2a740ec4...16930bad397dcd8c5c295dd0c9c70177a135d66f)
contains 23 first-parent integrations, 99 newly reachable commits and 149
changed files. The 99 include January–July branch history newly merged into
`master`; they are not 99 newly authored October changes. Decisions compare
the resulting source with the fork's v1.0.3 contracts, not commit subjects
alone. Proposed adoption and test reuse below are not delivered by this audit.

### Adoption and compatibility decisions

| Area and upstream source | Disposition | Fork decision and evidence |
|---|---|---|
| Supplementary Unicode helper ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/key_pub.go), [tests](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/key_pub_test.go)) | Adopt narrowly | `ToUC` now normalizes both `\u` and `\U` to Xlib's `U` spelling. The fork's [CGO helper](../../key_helpers_cgo.go) and [non-CGO helper](../../robotgo_nocgo_parity.go) normalize only `\u`. Add matching supplementary/max-rune and existing escaping regressions in both variants; do not import permissive argument parsing with this repair. |
| CGO supplementary text ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/key/keypress_c.h)) | Adopt with hardening | The fork's [native Unicode path](../../key/keypress_c.h) truncates macOS/Windows supplementary scalars to one UTF-16 unit, reached through [`UnicodeTypeE`/`TypeStrE`](../../key_text_cgo.go). Adapt surrogate-pair encoding, checked Windows target/message delivery and partial-`SendInput` release handling; preserve fork scalar validation and checked errors. This is distinct from the already-correct Pure-Go text backends. |
| macOS CGO event construction ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/base/types.h), [keyboard source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/key/keypress_c.h), [create-only tests](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/robot_mac_c_test.go)) | Eligible later | The fork's [native keyboard](../../key/keypress_c.h) forces modifier events to KeyDown/KeyUp and does not write zero remaining flags; native event sources also retain default local-event suppression. A later slice can adapt event types, explicit flags, suppression and deterministic source cleanup, with create-only fixtures. Keep the Pure-Go backend's owned private source; permission-granted runtime qualification remains [LAB-69](https://linear.app/riotbox/issue/LAB-69). No immediate implementation issue is opened here. |
| macOS CGO window targeting ([resolver](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/window/pub.h), [mutation source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/window/window.h)) | Adopt with hardening | Resolve an owned AX window before minimize/restore/close: the fork's [minimize helper](../../window/goWindow.h) and [PID close path](../../window/window.h) currently apply window attributes to an application element. Reuse the fork's established [Pure-Go targeting semantics](../../internal/darwinwindow/backend.go), checked errors and deterministic release; do not reinterpret fullscreen as maximize. |
| Windows clipboard ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/clipboard/clipboard_windows.go)) | Adopt with hardening | The [fork backend](../../clipboard/clipboard_windows.go) lacks OS-thread pinning, allocation-sized UTF-16 reads, cleared-error unlock handling and pre-empty allocation. Adapt these and system-DLL loading with injected lifecycle/error tests; preserve context-aware acquisition/cancellation and verify malformed sizes/termination instead of copying the upstream read unchecked. |
| Native Wayland clipboard ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/clipboard/clipboard_unix.go)) | Adopt narrowly | Add `wl-copy`/`wl-paste` selection to the [fork Unix backend](../../clipboard/clipboard_unix.go), with Wayland-only, primary-selection and owner-lifetime tests. Keep bounded [command execution](../../internal/command) without killing a successful write's selection owner after helper-parent exit; clean up unsuccessful/cancelled owners. Do not inherit upstream's X11-without-`DISPLAY` fallback or silently substitute clipboard for unsupported primary selection. |
| macOS clipboard locale ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/clipboard/clipboard_darwin.go)) | Eligible later | A later slice can force UTF-8 for `pbcopy`/`pbpaste` in the [fork backend](../../clipboard/clipboard_darwin.go) without replacing its context/cleanup model; test conflicting locale variables and synthetic non-ASCII stdin/stdout. No immediate implementation issue is opened here. |
| Other clipboard tools and tests ([Unix source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/clipboard/clipboard_unix.go), [test lock](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/internal/cliplock/cliplock.go), [live tests](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/clipboard/clipboard_test.go)) | Defer / already covered / reuse selectively | WSL, Termux, tmux and Plan 9 are separate support decisions, not implicit fallbacks. Fork process cleanup is already covered. The upstream cross-process lock coordinates tests, not production clipboard transactions; reuse only synthetic tests. Preserve the [fork's opt-in live test gating and restoration](../../clipboard/clipboard_test.go), never import default tests that overwrite a real clipboard. |
| Linux alert argument safety ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/internal/dialog/dialog.go)) | Adopt narrowly | Escape xmessage's comma/colon/backslash button grammar and pass message text through `-file -`/stdin. The [fork helper](../../robotgo_linux_alert.go) currently concatenates labels and passes option-like messages positionally. Retain `AlertE`, the richer fallback chain and the existing cancel exit code 2; upstream's bool-only `Show` is not a replacement. |
| Image and bitmap fixtures ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/img_test.go)) | Reuse selectively / retain | Synthetic file-release/error fixtures are useful, but the upstream strict-Base64 assertion conflicts with the fork's deliberate trailing-zero representation. Preserve [image conversion ownership](../../bitmap_safety_test.go), [legacy encoding tests](../../legacy_image_encoding_test.go) and [serializer](../../img.go); no silent representation or format-alias change. |
| Windows HID/PostMessage backends ([selection](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/win/backend.go), [driver](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/win/hid.go), [standalone API](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/win/standalone.go)) | Defer | Interception drivers, targeted window messages, virtual pointer state and global backend switching introduce new authorization, ownership and observation contracts. They are not substitutes for the fork's [checked SendInput backend](../../internal/windowsinput); no driver installation or public backend/package expansion is accepted here. |
| Optional macOS media keys and native dialogs ([media](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/darwin/media.go), [macOS dialog](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/darwin/alert.go), [Windows dialog](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/win/alert.go)) | Defer | These are new capabilities, not fixes to delivered Pure-Go support. Retain [explicit non-Linux Pure-Go alert errors](../../robotgo_nocgo_alert.go) and unsupported media-key behavior until scoped API, ownership and runtime evidence are available. |
| Wayland cursor observation ([provider source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/internal/wlcursor/wlcursor.go)) | Defer provider design | Upstream queries Hyprland's `j/cursorpos` socket or a temporary KWin script/D-Bus callback. The fork's [`LocationE`](../../mouse_policy_cgo.go) explicitly reports unsupported observation; no equivalent provider is implemented. Adoption needs compositor/endpoint identity checks, bounded calls, temporary-script and D-Bus cleanup, and real-runtime evidence. Cached last-injected coordinates are not observations; never add corner parking/clamping or silent consent reopening. |
| Wayland output/capture and X11 keyboard ([Wayland source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/wayland/screen.go), [X11 source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/x11/keyboard.go)) | Already covered / retain | Keep the fork's validated [logical output geometry](../../internal/waylandoutput/geometry_linux.go), [protocol events](../../internal/waylandoutput/events_linux.go), native xdg-output and native/portal capture paths. Preserve X11 transactional key ownership and explicit unsupported native Unicode; no upstream global keymap mutation or backend replacement is warranted. |
| Native smooth movement ([source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/mouse/mouse_c.h)) | Reuse existing issue | The fork's [native loop](../../mouse/mouse_c.h) retains reversed max/min selection in `crude_hypot` and discards intermediate/final movement failures; its exact final endpoint move already exists. Fold the bounded math/failure work into [LAB-256](https://linear.app/riotbox/issue/LAB-256), which owns checked pointer composites/timing compatibility, rather than open a duplicate issue. |
| Public helpers and package/configuration refactors ([API source](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/robotgo_pub.go), [shared config](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/pub/pub.go)) | Exclude wholesale | Upstream now changes return signatures, including `Paste` to `(int, error)`, and adds importable platform/shared packages. Retain fork [checked compatibility helpers](../../upstream_compat.go), explicit errors and [frozen API manifest](public-api.md). Existing [LAB-256](https://linear.app/riotbox/issue/LAB-256), [LAB-257](https://linear.app/riotbox/issue/LAB-257) and [LAB-269](https://linear.app/riotbox/issue/LAB-269) keep their existing scopes; this audit does not authorize interface expansion or strict-API migration. |
| Dependencies, CI and repository presentation ([dependencies](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/go.mod), [CI](https://github.com/go-vgo/robotgo/blob/16930bad397dcd8c5c295dd0c9c70177a135d66f/.github/workflows/go.yml)) | Already covered / reuse selectively / exclude | Shared dependency versions already match the [fork module](../../go.mod); fork Go 1.26.10 is newer than upstream 1.26.0. Do not import upstream module identity, experimental `go-wayland`, logo, instructions or repository layout. Reuse compatible synthetic CI/test ideas only; retain fork API, lifecycle, privacy and real-runtime gates. |

### Near term issue grouping

Five Backlog children of [LAB-425](https://linear.app/riotbox/issue/LAB-425)
form the next implementation horizon. None is delivered by this documentation
audit; each must preserve signatures and provide normal/error/lifecycle tests.

| Issue | Bounded outcome |
|---|---|
| [LAB-427](https://linear.app/riotbox/issue/LAB-427) | Windows clipboard thread, buffer, cancellation and mutation/ownership safety |
| [LAB-428](https://linear.app/riotbox/issue/LAB-428) | CGO Windows/macOS UTF-16 text correctness plus supplementary `ToUC` spelling in both build variants |
| [LAB-429](https://linear.app/riotbox/issue/LAB-429) | Native Wayland clipboard selection and successful selection-owner lifetime |
| [LAB-430](https://linear.app/riotbox/issue/LAB-430) | Literal xmessage button labels and stdin message delivery without changing alert fallback/cancellation semantics |
| [LAB-431](https://linear.app/riotbox/issue/LAB-431) | CGO macOS minimize/restore/close targeting an owned AX window rather than the application element |

### First parent integration chronology

Dates below are integration dates, not the creation dates of all merged work.
Each immutable commit link identifies its first-parent diff.

| Date | Integration | Main source change |
|---|---|---|
| Oct 03 | [d70139fe](https://github.com/go-vgo/robotgo/commit/d70139fe09b19ba3292ac5a50a33f26c2f156a1f) | Earlier keyboard, key-map, active/access and paste/count API work |
| Oct 03 | [ff1f9cf4](https://github.com/go-vgo/robotgo/commit/ff1f9cf436ce403cd53fa1d23bad07b7c8fc17e6) | Pure-Go active-app/access helpers and keyboard synchronization |
| Oct 04 | [dd96c347](https://github.com/go-vgo/robotgo/commit/dd96c34757e854eadd1f1f17ca082178fed45e47) | Dependency updates and ignores |
| Oct 04 | [7295444e](https://github.com/go-vgo/robotgo/commit/7295444e0f303bad97e231f751f692baf5aef71d) | Clipboard commands, Windows ownership and Plan 9 support |
| Oct 04 | [bc9ece5e](https://github.com/go-vgo/robotgo/commit/bc9ece5e42218ced925f39f70b825182cd2bc14a) | Logo and translated documentation |
| Oct 04 | [45d72f6e](https://github.com/go-vgo/robotgo/commit/45d72f6e47c4c0381d4513891d67270574e498d5) | Shared sleep helper and dependency update |
| Oct 04 | [a260579c](https://github.com/go-vgo/robotgo/commit/a260579c00fe280f7e1f58aac1731b7066fca46b) | Upstream agent guidance only |
| Oct 04 | [810cc309](https://github.com/go-vgo/robotgo/commit/810cc309043980b6db74279381fc0cab6f0a4996) | Backend key arrays/maps, macOS media keys and display helpers |
| Oct 05 | [1c192834](https://github.com/go-vgo/robotgo/commit/1c192834e268b1f50edccd1a8f60c8296060e7fb) | Tagged CI, CGO keyboard tests and macOS movement fixes |
| Oct 05 | [c639b502](https://github.com/go-vgo/robotgo/commit/c639b502d30b48f4d5f8a68787011eb50b7fe455) | Requirements and translated README cleanup |
| Oct 05 | [e4f55642](https://github.com/go-vgo/robotgo/commit/e4f5564289b0f28dd2652dfb0950c7a20382687c) | X11 keyboard-map/Unicode changes and Xvfb harness |
| Oct 05 | [7798d0d2](https://github.com/go-vgo/robotgo/commit/7798d0d2908ffc01ac3e12c26bcd6e68e6329b0e) | Lint/coverage CI, image fixtures and Windows clipboard repairs |
| Oct 05 | [7f1dd810](https://github.com/go-vgo/robotgo/commit/7f1dd81054966ac918bd7305ef0f8f343c519962) | CGO and Pure-Go input error returns/validation |
| Oct 05 | [c4d5f314](https://github.com/go-vgo/robotgo/commit/c4d5f3145c59e2841d0ed4894d895e23dad05e93) | Expanded Pure-Go macOS/window APIs |
| Oct 05 | [2064384c](https://github.com/go-vgo/robotgo/commit/2064384c92e0658577cc1923459a8da0f1e911ae) | Clipboard test adjustment |
| Oct 05 | [03658116](https://github.com/go-vgo/robotgo/commit/036581169c221a7973ccb82e127ca87163a26000) | Checked window mutations and cross-process clipboard test lock |
| Oct 07 | [2e4ca97b](https://github.com/go-vgo/robotgo/commit/2e4ca97bb38d612429181a760d5396d8b0fefc2c) | Shared portable helpers and platform dialogs |
| Oct 07 | [1a263fdc](https://github.com/go-vgo/robotgo/commit/1a263fdccdd9f212acaf25358a24c418f5ea3587) | Shared key helpers/configuration refactor |
| Oct 07 | [9b15a82e](https://github.com/go-vgo/robotgo/commit/9b15a82e0eed381583e30f7083baf99c22a77853) | Input error examples and translated docs |
| Oct 07 | [41405680](https://github.com/go-vgo/robotgo/commit/41405680f9be0d82052c9833069d0ea749fd1154) | Requirements/examples documentation |
| Oct 07 | [f8921165](https://github.com/go-vgo/robotgo/commit/f89211650726bb4ebefdb239d42258120c08008f) | Windows PostMessage/HID backend selection and tests |
| Oct 22 | [c0b5fa88](https://github.com/go-vgo/robotgo/commit/c0b5fa88bc7cbed26ebbcd5e922262415a4f51bf) | Wayland cursor query paths, dependency and documentation updates |
| Oct 27 | [16930bad](https://github.com/go-vgo/robotgo/commit/16930bad397dcd8c5c295dd0c9c70177a135d66f) | `lang` to `langs` rename and link repairs |
