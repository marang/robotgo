# Autonomous GUI Control Plan

Status: LAB-75 complete; LAB-73 semantic observation in progress

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

The contract intentionally advertises no usable production backend until its
native adapter is present and probed. AT-SPI, Windows UI Automation, and macOS
Accessibility adapters are the next implementation layer; none may broaden
the immutable policy or expose raw handles, object paths, arbitrary platform
properties, backend errors, or consent prompts.

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
