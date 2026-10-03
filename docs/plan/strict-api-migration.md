# Strict interfaces and CGO decomposition plan

RobotGo will keep its frozen public declarations and legacy behavior while
separating native calls from backend policy, then give every operational
error-hiding helper a checked migration path. The goal is a small, trustworthy
interface over deep modules for dispatch, input ownership, capture, display
queries, and window selection, not one new exported alias for every old name.

Coordination belongs to
[RobotGo P013](https://linear.app/riotbox/project/robotgo-p013-strict-api-migration-and-cgo-decomposition-10af66a71612)
(project ID `e0fdaaad-79b4-4bf0-a726-4c8db5a00665`).
This document owns technical decisions; Linear owns sequencing and delivery
status. Proposed names below are future work, not exports available in LAB-255.

## Scope and compatibility

The [public compatibility contract](../compatibility/public-api.md) remains
authoritative. All 14 variants in `api/compat/config.json` must retain their
exact declarations during decomposition, including C-facing signatures,
platform-dependent aliases, constants, fields, and methods. The exact checker
rejects additions as well as removals. Later strict additions therefore require
reviewed generated baseline updates; do not weaken the gate or hand-edit its
manifests.

Legacy helpers retain defaults, target interpretation, timing, fallback order,
side effects, and partial-failure sequencing. New strict composites may stop
before the next operation after failure, but that does not authorize routing
legacy composites through them. In particular, current `MoveClick` and
`MovesClick` can click after movement fails. Characterize and preserve that
sequence. Preserve CGO/Pure-Go differences in drag, `ScrollRelative`, paste
readiness, scaling, and window identity instead of silently harmonizing them.

Linux remains Wayland-first: no new X11 dependency in a Wayland-primary path,
including a session with `WAYLAND_DISPLAY` set and `DISPLAY` absent. Retain
explicit unsupported errors and constrained, observable fallbacks. Preserve
the X11-primary path and macOS/Windows compile and runtime contracts.

Non-goals are changing legacy signatures, making unsupported compositor
operations appear supported, moving backend policy into an exported package,
replacing the native backends wholesale, or changing release support claims.
LAB-254 capture/conversion nil-success repair is separate from this structural
slice.

## Complete frozen helper inventory

The [machine-readable inventory](strict-api-inventory.json) covers every
exported top-level function in the root package at source commit
`8b4a6ced`. It reconstructs each variant from the full Linux CGO manifest and
its validated deltas, then records source filenames and build conditions.
There are 261 distinct names; exact declarations remain in the manifests rather
than being duplicated here. Source filenames in the inventory are provenance,
not required destinations after decomposition. Build conditions include implicit
CGO selection caused by `import "C"`.

| Classification | Distinct names | Required treatment |
|---|---:|---|
| Existing error-returning interface | 124 | Preserve signatures and error identity; an error signature alone does not prove fidelity |
| Legacy with existing checked migration | 50 | Document the canonical existing helper or composition |
| Legacy requiring planned checked migration | 31 | Deliver the canonical strict family before adding migration deprecation |
| Computation or timing | 19 | Preserve arithmetic, timing and unsafe preconditions; no backend success claim |
| Configuration or availability observation | 12 | Document observation semantics rather than treating availability as operation success |
| Raw native status or pointer interface | 22 | Explicit platform, status, precondition and ownership contract |
| Variant-dependent selected/validity helpers | 2 | Pure-Go `GetBHandle`/`IsValid` hide backend failure; CGO retains raw selected/validity semantics |
| Checked conversion missing | 1 | `ToStrings` needs one validation-preserving alternative |

The six Linux CGO variants each expose 249 root functions; Windows CGO exposes
257 and macOS CGO 245. Linux and macOS Pure-Go variants each expose 240; Windows
Pure-Go variants expose 251. Architecture aliases share their configured
baseline. Portal-tag additions affect `screen/portal`, not this root inventory.
No build variant may be blanket-exempted because another variant exposes a
native status contract.

## Native owners and internal seams

Keep `robotgo.go` and `key.go` as thin facades where their signatures allow it.
Move their implementation preambles exactly once into
`native_adapter_cgo.go` and `key_native_cgo.go`. These remain the owning
translation units for the existing root and key implementation headers.
C-facing exports that need C types can remain beside the owner. Existing
platform-specific CGO files retain their own responsibilities; this is not a
claim that the whole repository has only two translation units.

Repeating `mouse/mouse_c.h`, `key/keypress_c.h`, or implementation includes
from `base/xdisplay_c.h` in newly split files can duplicate external definitions
or create separate static display, generation, keyboard and pointer ledgers.
Do not solve a Go file-size problem by multiplying native owners. Extracted
policy files do not import C and do not include implementation headers.

The native adapter hides C constants, allocation, ownership, pointer conversion,
and native invocation behind private ordinary-Go requests and results.
Capability, backend selection, fallback, delays, lock ordering and tracked holds
remain in Go modules. Keep key-code width/sign conversion in the key native
owner; a platform-dependent `C.MMKeyCode` is not automatically a `uint32`.

| Module | Internal seam and responsibility | Evidence |
|---|---|---|
| Capture | Native dimensions/capture/bitmap adapter; Go backend selection and cancellation | Backend failures, invalid results, cancellation and owned bitmap cleanup |
| Display | Native geometry/scale queries; Go target/default and fallback policy | Wayland-only errors, aggregation, nonzero geometry and platform target semantics |
| Pointer | Native move/location/buttons/scroll adapter; Go dispatch and owned holds | Fake native adapter, no foreign release, rollback, delay and affinity |
| Keyboard | Native key mapping/status/events adapter; Go modifiers, transactions and text | Modifier ordering, held ownership, PID rules, exact text and error identity |
| Window | Native references/status adapter; Go selection, identity and compositor policy | Explicit target mode, selected versus active identity, unsupported cases and reference cleanup |

Desktop protocols and native frameworks are true external dependencies.
Production and fixture adapters justify private seams; do not export injection
hooks merely for tests. Existing local image fixtures and pure argument parsers
need no port. Prefer interface-level behavior tests over new tests coupled to
the extracted file layout. Keep ownership and regression evidence until
equivalent interface tests demonstrate it can safely be replaced.

Responsibility files should separate capabilities/runtime types, capture
policy/facade, display policy, pointer policy/ownership/conveniences, window and
bitmap facades, Wayland bounds parser/cache, and keyboard
policy/ownership/text/constants/helpers. Handwritten Go files stay below 1,000
lines; generated protocol artifacts are excluded. The extraction is behavioral
decomposition, not padding or arbitrary chunks to satisfy a line count.

## Checked migration families

The inventory records a canonical target for every legacy helper. These future
families complete the missing paths without alias-specific proliferation.
Error results preserve `errors.Is` identity for backend, unsupported,
permission, cancellation, and cleanup causes; joined failures retain all causes.

| Future canonical interface | Legacy family | Checked behavior |
|---|---|---|
| `MoveSmoothE` | `MoveSmooth`, `MoveMouseSmooth` | Validate options, distinguish readiness/injection failure from false, preserve successful timing |
| `MoveSmoothRelativeE` | `MoveSmoothRelative` | Use native relative support or checked observation plus absolute movement |
| `DragE` | `Drag` | Acquire only RobotGo's hold, move, attempt release after failure, join errors |
| `DragSmoothE` | `DragSmooth`, `DragMouse` | Checked smooth movement with deterministic release after acquired holds |
| `MoveClickE` | `MoveClick` | Stop before clicking after failed movement |
| `MovesClickE` | `MovesClick` | Checked default smooth movement followed by checked click |
| `ScrollSmoothE` | `ScrollSmooth` | Validate repetitions/delay, stop on failure, preserve successful timing |
| `ScrollRelativeE` | `ScrollRelative` | Retain documented variant distinction: CGO adds observed coordinates; Pure-Go forwards deltas |
| `CloseWaylandInputE` | `CloseWaylandInput` | Cancel portal I/O before device locks; attempt all cleanup and join errors; retain retryable hold ownership |
| `SelectWindowE(target int, isHandle bool) error` | `SetHandle`, `SetHandlePid` | Select internal target without foreground activation or inherited target-mode ambiguity |
| `GetBHandleE() (int, error)` | `GetBHandle` | CGO selected identity; Pure-Go selected identity or checked active fallback; no silent backend failure |
| `GetHandByPidE(target int, isHandle bool) (Handle, error)` | `GetHandById`, `GetHandByPid`, `GetHandPid` | Checked construction of the actual platform Handle, including native macOS references |
| `FindWindowE` | `FindWindow` | Report Windows UTF-16 conversion/query failure and explicit unsupported non-Windows stubs |
| `GetMainIdE() (int, error)` | `GetMainId`, `IsMain` | Checked Linux connection/output query; preserve platform meaning, not a universal display ID |
| `GetXDisplayNameE() (string, error)` | `GetXDisplayName` | Distinguish native allocation/query failure from valid empty configuration; unsupported where no corresponding native query exists |
| `SysScaleE` | `SysScale`, global DPI conveniences | Positive finite scale with checked acquisition, query and cleanup |
| `ScaleFE` | `ScaleF`, `Scaled`, applicable `MoveScale`/size conveniences | Preserve the separate Windows HWND DPI interface |
| `ToByteImgE` | `ToByteImg`, `ToStringImg` | Propagate encoder errors with existing format/default representation |
| `ToStringsE` | `ToStrings` | One checked conversion; reject non-string input without silent nil |

The pinned image serializer defaults to JPEG quality 70 and returns standard
base64 in an oversized, trailing-zero-padded buffer, not raw encoded image
bytes. Its synthetic characterization freezes that existing representation;
the future checked interface must preserve it or separately review an
intentional representation change.

Before implementing the native Handle constructor, define and test ownership
of the returned macOS Accessibility reference, including a usable release path.
Do not introduce a checked constructor that leaks or transfer borrowed
references without an explicit lifetime contract. This is a LAB-257 acceptance
requirement, not an assertion that current ownership is solved.

Use existing interfaces wherever they genuinely cover the same operation:

- `GetDisplayRect` uses `GetDisplayBoundsE` plus `Rect` construction.
- Integer window lookup uses `ResolveWindowHandleE`, including
  `GetHWNDByPid` where integer identity is meaningful. It does not replace a
  native macOS CGO reference constructor.
- `GetHandle` uses `GetActiveE` plus platform identity extraction.
  `GetActiveE` does not replace selected-state inspection or selection.
- `ScrollDir` uses `ScrollE`: down `(0,-amount)`, up `(0,amount)`,
  left `(amount,0)`, right `(-amount,0)`.
- `MoveArgs` uses `LocationE` plus addition. Other scale arithmetic uses a
  checked scale query followed by the existing arithmetic.
- `TypeStrDelay`, `TypeDelay`, and `TypeStringDelayed` use `TypeStrE`
  then a post-call wait. Their delay is not a per-rune interval.
- `SetDelay` uses `GetRuntimeConfig`, updates both delay fields, and calls
  `SetRuntimeConfig`. `ToImage` uses `ToRGBAE`; string serialization uses
  `ToByteImgE` plus string conversion.

`CloseWindowKill` already returns error but its CGO implementation hides
selection, PID lookup, and process-existence errors. Characterize its legacy
sequence and plan an explicitly reviewed strict repair in LAB-257; do not treat
its signature as evidence that these failures already propagate.
Similarly, this plan makes no error-fidelity claim for capture conversions
whose nil-success behavior is assigned to LAB-254.

## Scale and identity semantics

`SysScaleE` and `ScaleFE` are distinct. Outside Windows, the latter can
delegate to the former. A strict addition must not normalize unrelated
platform identifiers or quietly replace a backend.

| Variant | System scale | Scale factor |
|---|---|---|
| CGO Windows | Global desktop DC horizontal `LOGPIXELSX / 96`; target argument historically ignored | Explicit HWND, `-2` desktop HWND, omitted/`-1` foreground HWND |
| Pure-Go Windows | Forwards to HWND scale | Same HWND routing |
| CGO macOS | CoreGraphics mode ratio; omitted inherits configured DisplayID, then `-1` means main display | Delegates to system scale |
| Pure-Go macOS | CoreGraphics mode ratio; omitted/`-1` means main display without configured DisplayID inheritance | Delegates to system scale |
| CGO Linux X11 | Screen-zero physical/Xft DPI; target unused | Delegates to system scale |
| Wayland or Pure-Go Linux | No trustworthy measured global DPI interface in the current implementation | Strict call returns `ErrNotSupported`, never fabricated successful 1 |

The pinned Windows dependency falls back from missing `GetDpiForWindow` to a
DC for the selected HWND and vertical `LOGPIXELSY`. Preserve that fallback
and release its DC; it is not the global-X query. Validate query results and
release every acquired Windows DC and copied macOS display mode on success,
invalid values, and failure.

CGO `GetScaleSize` composes `GetScreenSizeE` with `ScaleFE`; Pure-Go macOS
uses `SysScaleE`. Pure-Go Windows bounds already describe physical pixels,
and Pure-Go Linux does not apply measured scale; both use checked size alone.
CGO `MoveScale` uses public `ScaleF`, while native pointer dispatch/location
uses `scaleFLocked` and `sysScaleLocked`. Preserve and test this current
Windows distinction.

`GetMainId` means Linux output index, macOS CG display ID, or Windows active
HWND; the non-Windows Pure-Go implementation is an intentional default index,
not a native query. `GetBHandle` is selected-state inspection with Pure-Go
active fallback. Native `IsValid` can also update selection. None is equivalent
to foreground activation, and `SetActiveE`/`ActivePid` cannot substitute for
`SelectWindowE`.

## Raw native contracts

Raw exceptions must be enumerated, narrow, and documented per variant. Preserve
C signatures and ABI, pointer validity, borrowed versus owned memory,
allocation/free pairing, and platform-dependent native result interpretation.
This covers C color conversion and bitmap view helpers, `GoString`,
`CheckMouse`, bitmap freeing, and native `GetHandByPidC`; a borrowed
`ToBitmap` view is not an owned validated conversion.

Windows `SendInput`, `SendMsg`, `SetActiveWindow`, `SetFocus`, `SetForeg`,
`GetHWND`, `GetMain`, `GetDesktopWindow`, `GetDPI`, `GetMainDPI`, and
`GetSysDPI` retain native count, handle, boolean, or numeric status contracts.
Document zero/partial results and caller responsibility instead of claiming
Go error fidelity. Windows `FindWindow` is not exempt: the wrapper discards an
actual Go conversion error and needs the checked family above.

CGO raw `IsValid` and native-reference construction do not excuse Pure-Go
backend failures. The future guard must classify each variant separately.
Capability and diagnostic interfaces report availability with reasons, rather
than guaranteeing that a later operation will succeed.

## Ordered delivery

| Slice | Technical result | Required exit evidence |
|---|---|---|
| [LAB-255](https://linear.app/riotbox/issue/LAB-255) | Thin root/key facades, single native owners, private C-free policy seams, responsibility files below the size limit, this inventory and plan | Zero exported declaration drift; default/Pure-Go and relevant tagged fixture suites; native ownership and protected platform evidence |
| [LAB-256](https://linear.app/riotbox/issue/LAB-256) | Checked pointer composites and Wayland lifecycle; migrate affected wrapper docs only when targets exist | Normal/failure/rollback/release/timing tests, original error identity and Wayland-only unsupported/fallback tests |
| [LAB-257](https://linear.app/riotbox/issue/LAB-257) | Checked display/window/scale/image/conversion families and variant-aware inventory guard | Target/default differences, selected fallback, native reference ownership, encoder failures, raw-contract docs, additive freeze review and guard tests |

These are linked delivery scopes, not assertions of implementation, review,
CI, or completion. Strict additions, migration GoDoc, automatic operational
classification, and its new-function rejection gate are not delivered merely
by checking in this planning inventory. No LAB-255 deprecation should direct
users to an undefined future export.

The guard will reconstruct exported functions for every frozen variant and
reject any unclassified addition. Every non-error operational helper must name
an existing checked target/composition or an explicit raw status contract.
Validate target existence, variant availability, and migration documentation;
an `_ =` grep cannot detect transitive hiding or valid-looking nil/zero
fallbacks. Test the guard with missing targets, missing classifications,
unavailable variant targets, and new undocumented wrappers. Keep the exact API
freeze independent; the guard does not replace it.

## Validation and privacy

Use fixture adapters and in-memory images for default tests. Do not capture,
save, type into, or inspect a developer's real desktop/clipboard as structural
validation. Intentional integration artifacts require deterministic cleanup
on success, error, timeout, and cancellation, with deletion regression evidence.

Retain and extend argument parity, tracked mouse/key ownership, portal lock
ordering, bitmap safety, capture cancellation, bounds errors, and Pure-Go
contract suites. Strict composites need injected movement, click/toggle,
location, sleep and encoder dependencies at private seams. Test joined cleanup
errors and `errors.Is`, as well as partial progress and retryable ownership.

Scale tests must distinguish global Windows X DPI from selected HWND DPI,
exercise omitted/`-1`/`-2`/explicit/invalid HWND targets, verify legacy Y
fallback and DC cleanup, preserve macOS configured-default differences and
mode release, and reject fabricated Linux/Wayland scale. Window tests must
separate active lookup, selected lookup/fallback, selection and activation,
including permission and unsupported errors.

Implementation validation requires `go test ./...`, `CGO_ENABLED=0 go test
./...`, the exact public compatibility checker, relevant Wayland/portal/OCR
tagged suites, and the existing sanitizer ownership gate. Native macOS and
Windows evidence comes from their protected platform jobs; cross-compiling
Pure-Go is compile evidence, not native runtime evidence. Follow
[TEST.md](../../TEST.md) for runtime prerequisites and safe integration commands.
No phase is complete until its required platform, compatibility, privacy,
ownership and review evidence is green.
