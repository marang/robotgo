# Autonomous GUI Control Plan

Status: P010 complete; LAB-75, LAB-73, LAB-72, and LAB-74 merged

Linear coordination:

- Project: [`RobotGo | P010 | Autonomous GUI Control`](https://linear.app/riotbox/project/robotgo-or-p010-or-autonomous-gui-control-6d7e2b70da14)
- Project ID: `fecf551c-4a6b-43db-808b-eb7830e8d6ca`
- Action issue: [`LAB-75`](https://linear.app/riotbox/issue/LAB-75/expand-observation-bound-mcp-actions-for-autonomous-gui-control)
- Semantic observation: [`LAB-73`](https://linear.app/riotbox/issue/LAB-73/add-accessibility-first-semantic-gui-observations-to-the-mcp)
- Explicit image observation: [`LAB-72`](https://linear.app/riotbox/issue/LAB-72/add-policy-gated-mcp-image-observations-for-unknown-guis)
- OCR and visual detection: [`LAB-74`](https://linear.app/riotbox/issue/LAB-74/add-bounded-ocr-and-visual-element-detection-for-mcp-observations)

## Outcome and order

Extend the completed agent-session and local MCP foundations into practical
GUI control without turning visible desktop content into authorization.
Delivery order is:

1. bounded scroll, drag, keyboard chord, and window activation
2. accessibility-first semantic observation
3. explicit policy-gated image content for otherwise unknown GUIs
4. bounded OCR and visual element detection

Structured accessibility data remains the preferred perception source. Image
and OCR content are later explicit sensitive-read boundaries, not implicit
side effects of an action.

## Semantic observation contract

LAB-73 introduces the schema-v6 `desktop.inspect-ui` operation and local MCP
tool as a separate sensitive-read grant. The first contract slice provides:

- exact allow-listed process/window selection with live title revalidation
- immutable allowed role and property sets
- hard node, tree-depth, aggregate string-byte, query, observation, minimum
  query-interval, and session-lifetime bounds
- opaque observation-scoped element IDs with private backend references
- omission of hidden/offscreen nodes and unconditional redaction of text on
  password or backend-marked sensitive elements
- bounded hierarchy, state, action, focus, and logical-bounds projections
- fixed state/action vocabularies plus hard native-reference size bounds
- explicit unsupported/permission/unavailable capability states
- zeroing of retained backend references on observation release and session
  close

The first production adapter uses an already-active Linux AT-SPI2 bus for
exact process-and-title targets. Discovery is bounded, rejects ambiguous
windows, prunes hidden/offscreen subtrees before reading their text, never
reads password text, and infers only fixed platform-neutral states/actions.
The non-prompting capability probe does not activate the accessibility service.
Inspection rechecks the existing bus and registry and likewise never starts
them as a side effect.
The Windows adapter uses UI Automation 2 for exact process- or HWND-and-title
targets. It revalidates the resolved HWND/PID/title after traversal, disables
automatic focus, applies fixed native call timeouts, prunes foreign-process,
offscreen, password, and disallowed-role content before reads, and releases all
COM/SAFEARRAY/BSTR/VARIANT ownership before returning. Hermetic tests plus a
protected self-owned Win32 button/input/password fixture cover its policy,
privacy, and lifecycle contract. The macOS adapter uses Accessibility for exact
process- or CGWindowID-and-title targets, fixed AX messaging timeouts, and
observation-private PID/window/path references. Process identity is queried
before metadata, secure/hidden/offscreen/foreign/disallowed content is pruned,
and retained AX/CoreFoundation objects are deterministically released. Its
hermetic policy/privacy/lifecycle contract and cross-build are blocking;
permission-granted runtime evidence remains pending under LAB-69.

P011/LAB-77 now consumes semantic element IDs through a separately authorized
`desktop.element-act` operation. It re-resolves the private backend reference
and revalidates exact window/process/title, role, name, state, bounds, and
offered action. The inspection capability alone still never grants mutation.
See the [Verified Adaptive Workflows Plan](verified-adaptive-workflows.md).

## Explicit image observation contract

LAB-72 adds the schema-v7 `desktop.view` operation and opt-in
`robotgo_view` MCP tool for an unfamiliar GUI that has no usable accessibility
projection. The boundary deliberately needs independent grants at three
levels:

1. `-allow-image-content` registers the MCP image tool for this process.
2. Policy must allow `desktop.view`, display/region scope, confirmation when
   configured, and finite source-pixel, encoded-byte, output-dimension, view,
   observation, concurrency, rate, per-view timeout, and session-lifetime
   limits.
3. GNOME/KDE ScreenCast additionally needs `allow_portal_view` and the explicit
   `-start-portal-view` startup action; a view request never opens consent.

Region view is the normal grant. Full-display selection has a distinct
`allow_full_display_view` switch and still remains bound by display and source
pixel limits. Redaction masks are applied to the session-owned raw frame before
its digest, downscaling, PNG encoding, action-lineage comparison, and
verification. Output is one bounded, fully decoded and structurally validated,
non-indexed, metadata-free PNG plus sanitized geometry/backend metadata. It is
never duplicated into structured output or written to a temporary file.

The Go `View` transfers encoded-byte ownership exactly once. The MCP adapter
clears those owned bytes immediately after JSON serialization and tracks them
until then so shutdown also clears content whose serialization was skipped.
Serialized JSON-RPC buffers are owned by the configured SDK transport and are
not mutated by RobotGo because a batch-capable transport may retain several
responses until delivery. The separate redacted raw observation remains in
memory only for explicit follow-up lineage and is zeroed by
`robotgo_release_observation` or session close. Client/model copies and provider
retention begin beyond the RobotGo boundary and are stated at startup and in
user documentation.

Visible content is always untrusted data. It cannot add policy, bypass the
adapter startup grant, authorize mutation, or replace typed action confirmation.
Custom MCP session implementations must construct images through
`agent.NewImageView`, which rejects malformed envelopes, dimension mismatch,
ancillary PNG chunks, indexed palettes, bad checksums, and invalid encoded
payloads without taking ownership on failure.

## Bounded OCR and visual detection contract

LAB-74 adds schema-v8 `desktop.ocr` and `desktop.detect-elements` plus the
separately startup-gated `robotgo_ocr` and `robotgo_detect_elements` MCP tools.
They consume only an explicit rectangle contained by a still-live LAB-72 view
observation; exact full-view analysis needs `allow_full_view_analysis` in
addition to the original view grants. The retained frame has already had every
configured redaction mask applied, and only the requested subregion is cloned
for analysis.

Policy independently bounds source pixels, allowed OCR languages, OCR boxes,
aggregate text bytes, visual proposals, calls, concurrency, rate, duration,
and session lifetime. Text is valid UTF-8 with controls removed and is
truncated only on a valid boundary. Every result reports source observation,
region, confidence, backend/model, truncation and sanitization state, and
`untrusted: true`; recognized instructions cannot change policy, confirmation,
or execute mode.

`ocr && cgo` builds use the Tesseract/Leptonica C APIs with an in-memory image
and explicitly freed word buffers. Other builds report OCR as unsupported and never invoke RobotGo's
file-based Tesseract CLI. Visual fallback uses the deterministic local
`contrast-components-v1` proposal model. Scratch pixels and backend text are
cleared on success, error, timeout, cancellation, release, and close. No
screenshot, OCR dump, replay file, raw diagnostics, or embedding is created.
Accessibility remains the preferred semantic source; visual proposals do not
claim roles, names, or action authority.

## Action contract

LAB-75 adds four typed `robotgo_act` operations:

- `pointer.scroll` moves to an explicit live-bounds-checked coordinate and
  emits a bounded number of scroll events
- `pointer.drag` keeps its start, end, path, button, distance, duration, and
  display inside immutable policy
- `keyboard.chord` accepts one allow-listed key plus only the canonical
  modifiers `alt`, `control`, `meta`, and `shift`; it validates the
  allow-listed process title and binds both key-down and key-up to that PID
- `window.activate` targets one allow-listed process or native handle and
  resolves it to one exact native handle, revalidates its expected title on
  that handle, and activates the same handle

Focus and activation are represented by one operation because supported
platform backends do not expose a reliable cross-platform semantic distinction.
Unsupported targets remain explicit.

Scroll and drag are cooperatively cancelable between injected events. A chord
checks cancellation after key-down and still performs its mandatory release
on persistent-hold backends. Linux and Windows chords are unavailable until
those keyboard backends can atomically bind input to an allowed target; a
global-focus or active-window check alone is not an authorization boundary.
The operation catalog reports scroll axes explicitly and Pure-Go X11 currently
accepts only vertical scroll. Native macOS CGO activation is unavailable
because its legacy handle representation cannot preserve one exact reference
across validation and mutation; Pure-Go macOS supports that contract. The
individual move, click, text, key-toggle, and activation backend calls remain
indivisible. Drag, chord, and activation always require `confirmed: true`, in
addition to the explicit MCP `mode: "execute"`.
Drag interpolation uses the delay-free `MoveImmediateE` primitive, so the
legacy global mouse delay cannot silently extend its bounded hold schedule.
Scroll positioning and events use `MoveImmediateE` and `ScrollImmediateE`, so
the same legacy delay cannot defer cooperative cancellation or session expiry.
Chord key-down and key-up use the delay-free `KeyToggleImmediate` primitive,
so the legacy global key delay cannot silently extend the bounded key hold.
Target title checks use `GetTitleTargetE(target, isHandle)` so explicit process
and handle identities never inherit the global legacy `TreatAsHandle` mode.
Ordinary agent move, click, and text mutations likewise use `MoveImmediateE`,
`ClickImmediateE`, and `TypeStrImmediateE`, isolating the complete agent
mutation surface from legacy process-global post-event delays.
Backend errors after any mutation dispatch are conservatively reported as
`unverified`; validation and other failures proven to precede dispatch remain
`failed`, preserving honest retry semantics.

## Policy and ownership

The immutable policy independently bounds operations, displays, buttons, keys,
modifiers, window identities, scroll events and distance, drag distance and
duration, chord length, action count and rate, and total session lifetime.
Empty allow lists deny access. Extended actions require an action interval and
session timeout; they are never enabled by the default diagnostics-only MCP
policy.

Each mutation capability carries a stable `unavailable_code`. Callers can
distinguish a temporarily unavailable backend, an unsupported platform
contract, and permission denial without parsing `reason`; `fallback` remains
separate and observable.

The process-exclusive session owns a pressed-input ledger. Every key or button
pressed by a multi-step action is released on success, backend failure,
caller cancellation, session timeout, transport disconnect, and `Close`.
The ledger records ownership before a down call: if that call returns an
ambiguous error, RobotGo immediately attempts the matching release and retains
ownership for a later `Close` retry if cleanup also fails.
If the backend explicitly reports that no state was acquired or that another
RobotGo caller owns the input, the tentative entry is removed without sending
a release that could disturb that foreign hold.
Native CGO macOS and Windows serialize pointer operations through a shared
process-wide ledger, preventing package-level holds, clicks, and agent drags
from claiming the same button concurrently; a rejected agent down never sends
an up for the pre-existing hold. `CloseMainDisplayE` remains the explicit
process-wide cleanup boundary and retries any native hold or Windows click
release that could not be confirmed. The strict click APIs identify that state
with `ErrInputReleasePending`; the agent mirrors it into its session ledger,
blocks subsequent actions, and retries it during `Close`.
Once injection has started, an interrupted multi-step action is reported as
`unverified`, so callers do not mistake a partial outcome for a safe retry.
Cleanup failure is returned rather than hidden, blocks further actions, and
retains the process-exclusive owner until a later `Close` retry releases the
input successfully.

## Observation and privacy boundary

Actions may use the existing `ObservationPrecondition`. RobotGo recaptures and
compares the retained in-memory region after policy and audit intent checks but
before injection. A changed region returns `stale-target`. Window activation
also performs identity validation during preflight and again immediately
before mutation.

No action writes a screenshot, OCR input, accessibility dump, typed text,
window title, or replay payload to disk or audit output. Tests use hermetic
fakes or disposable self-owned fixtures only.

## Evidence and exit criteria

The action slice is complete when:

- dry-run and execute work through the Go session and MCP schema
- policy, confirmation, quota, stale-target, rate, and lifetime failures occur
  before mutation
- scroll magnitude, drag paths, chord order, and activation identity are
  proven with hermetic fakes
- cancellation, error, disconnect, and close cannot silently retain pressed
  input ownership
- native and Pure-Go builds, race tests, API compatibility, lint, and relevant
  protected platform evidence pass
- the public documentation describes the safe
  observe → dry-run → confirm → execute → verify → release flow

P010 exits only after the later semantic, image, and OCR slices can safely
inspect and operate an unfamiliar self-owned GUI while releasing all sensitive
state.
