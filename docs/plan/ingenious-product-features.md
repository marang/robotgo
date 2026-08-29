# Ingenious RobotGo Product Features

Status: Product strategy proposal; P010 complete and P011/LAB-79 semantic
conditions and Action Proof v1 slice complete

## Product thesis

RobotGo should not compete only as a larger collection of mouse, keyboard,
screen, and window helpers. Its strongest opportunity is to become a safe,
cross-platform desktop runtime for agents and dependable GUI automation.

The differentiating product loop is:

```text
perceive through several bounded sources
    -> identify one target with confidence and provenance
    -> prove immutable policy and live preconditions
    -> preview or execute one typed action
    -> verify the expected result
    -> release sensitive state deterministically
```

The key promise is not merely that RobotGo can click a control. RobotGo should
be able to explain why it selected that control, prevent ambiguous or stale
actions, prove the outcome, and keep sensitive desktop content inside explicit
trust boundaries.

Implementation checkpoint (LAB-74): RobotGo now defines separate schema-v8
`desktop.ocr` and `desktop.detect-elements` sensitive-read operations over an
explicit subregion of a live LAB-72 image observation. OCR uses only the
in-memory `ocr && cgo` backend; default and Pure-Go builds report it as
unsupported instead of invoking the temporary-file CLI path. Both operations
apply immutable pixel/result/text/language/count/concurrency/rate/timeout
limits, return observation-bound backend/model/confidence provenance, mark
results untrusted, and zero their private subregion copy on every path. This is
the visual fallback layer; LAB-73 accessibility remains authoritative for
semantics.

Implementation checkpoint (LAB-77): schema v9 adds the deny-by-default
`desktop.element-act` operation and `robotgo_element_act` MCP tool. One action
is bound to a live semantic observation and exact caller-visible role, name,
states, bounds, and offered-action set. AT-SPI, Windows UI Automation, and
macOS Accessibility re-resolve their private native references inside the
same exact process/window/title immediately before native dispatch. Sensitive,
disabled, changed, or no-longer-supported targets fail stale; coordinate,
keyboard, clipboard, shell, and visual fallbacks are absent. A native-call
error after dispatch remains explicitly unverified, and set values never enter
results, audits, observations, files, or errors.

Implementation checkpoint (LAB-79): catalog schema v10 adds one optional fixed,
target-relative state/focus/value postcondition, a quota-bearing precheck,
quota- and rate-bearing native final gates, bounded post-dispatch polling, and
Action Proof v1 on every `desktop.element-act` return; policy capacity reserves
the source inspection, one precheck, up to three native gate probes, and every
configured poll. The semantic observation schema remains v1. An
already-satisfied request skips dispatch without action accounting. A
dispatched backend error remains unverified even if a later check matches. The
proof and audit schema expose only fixed outcomes and counts; values, target
text, native references, policy payloads, and raw errors remain private.

Implementation checkpoint (LAB-82): catalog schema v12 adds strict, adaptive,
and review resolution plus single-use Capability Lease v1. Executable leases
bind one session/policy window, TargetSpec digest, exact action, optional
postcondition and value digest, expiry, and one native dispatch. Adaptive
healing admits only deterministic semantic name drift above a policy threshold;
all qualifying candidates count and review proposals are non-executable. Action
Proof v2 and Audit schema v5 expose fixed lifecycle and evidence only.

Implementation checkpoint (LAB-83): TargetSpec v2 and catalog schema v13 add
explicit, policy-gated references to reviewed OCR and visual result items from
one live image observation. Accessibility candidates retain precedence; fixed
OCR/visual score bonuses can disambiguate only existing native candidates and
never grant coordinate authority. Exact provider/region/confidence/age policy,
stale and transformed-lineage rejection, evidence-bound lease expiry, cleanup,
and Audit schema v6 keep pixels and OCR text outside resolver, MCP, lease, and
audit state. Semantic-only TargetSpec v1 remains compatible.

## 1. Semantic and visual scene graph

Combine available perception sources into one normalized, observation-bound UI
scene graph:

- native accessibility role, name, value, state, hierarchy, focus, actions,
  and bounds
- explicitly authorized image regions
- bounded local OCR
- color, template, icon, and visual-layout matches
- exact window identity and geometry
- prior observation lineage

Each element should report a stable platform-neutral role, logical bounds,
source provenance, confidence, ambiguity, and the observation that produced
it. Accessibility remains the preferred source. Vision supplements missing or
insufficient semantics instead of silently replacing them.

Example projection:

```json
{
  "role": "button",
  "name": "Save",
  "bounds": {"x": 812, "y": 640, "width": 96, "height": 34},
  "state": ["enabled"],
  "sources": ["accessibility", "ocr", "visual-layout"],
  "confidence": 0.98,
  "ambiguous": false
}
```

This scene graph becomes the shared foundation for discovery, action targeting,
verification, debugging, and higher-level workflows.

## 2. Stable multi-signal element references

Absolute coordinates and a single brittle selector should no longer be the
default targeting model. RobotGo can resolve an element from several bounded
signals:

- exact window/process identity
- accessibility role and accessible name
- hierarchy and neighboring elements
- relative window position
- OCR text and visual appearance
- previous observation and element path

Conceptual Go API:

```go
button := robotgo.FindElement(
	robotgo.Role("button"),
	robotgo.Name("Save"),
	robotgo.Near(robotgo.Text("Filename")),
)
```

References remain observation-bound. Before mutation, RobotGo re-resolves the
private native or visual identity and revalidates the window, role, state, and
bounds. A disappeared, recycled, moved beyond policy, or materially changed
target is rejected as stale.

This allows selectors to survive safe changes in DPI, fractional scaling,
theme, window position, and modest layout revisions.

## 3. Transactional observe-act-verify

Treat meaningful GUI actions as guarded transactions:

```text
observe -> validate preconditions -> act -> verify -> commit
                                      \-> stop or compensate
```

Capabilities should include:

- required precondition observations
- exact target and state validation immediately before dispatch
- expected postconditions rather than fixed sleeps
- bounded retry policies for observation, never blind mutation retries
- explicit `failed`, `unverified`, and `succeeded` outcomes
- optional safe compensating actions where the operation is genuinely
  reversible

Examples include verifying that a dialog closed, a checkbox changed state, a
file appeared, or a bounded region changed as expected. A compensation must
never be presented as rollback when the external side effect cannot actually
be reversed.

## 4. Confidence and ambiguity as first-class data

Every semantic or visual resolution should expose confidence and evidence:

```json
{
  "confidence": 0.93,
  "matched_by": ["accessible-name", "role", "relative-position"],
  "rejected_candidates": 2,
  "ambiguous": false
}
```

Policy can define minimum confidence and permitted evidence combinations.
RobotGo should stop, return candidates, or require confirmation when several
targets remain plausible. Low confidence must never silently degrade into an
absolute-coordinate click.

## 5. Visual action preview

Before a sensitive mutation, an optional local-only preview can show:

- selected window and target bounds
- planned pointer path or affected control
- expected action and postcondition
- relevant policy scope and confirmation requirement
- ambiguity or sensitive-area warnings

The preview should use a local overlay and disclose no additional pixels to the
remote client. It is especially useful for first-run workflows, destructive
actions, and operator-supervised agent sessions.

## 6. Secure secret entry

Secrets should not have to pass through a model, prompt, clipboard, audit log,
or ordinary typed-text result.

Conceptual API:

```go
err := robotgo.TypeSecret("github-production-token", target)
```

RobotGo resolves the named value from an OS keyring or a short-lived local
secret provider and injects it only when all constraints hold:

- exact allow-listed process/window
- exact secure/password input element
- current observation and focus proof
- explicit secret-use policy and optional human confirmation
- no clipboard fallback
- no content in logs, errors, observations, or audits
- immediate zeroing of RobotGo-owned buffers

The agent sees the secret identifier and outcome, never the secret value.

## 7. Declarative desktop workflows

Add a strict workflow format above individual actions:

```yaml
steps:
  - wait:
      window: "Save document"
  - fill:
      element: {role: textbox, name: "Filename"}
      value: "report.pdf"
  - click:
      element: {role: button, name: "Save"}
  - verify:
      window_closed: true
```

The workflow compiler selects the best allowed backend per step while
preserving the same policy, confirmation, observation-lineage, timeout, and
cleanup contracts as the lower-level API. The format must not introduce an
escape hatch for arbitrary code or shell execution.

## 8. Bounded self-healing selectors

When an exact locator becomes stale, RobotGo may search through an explicitly
ordered and policy-bounded fallback ladder:

1. exact observation/native reference
2. exact role and name
3. hierarchy and neighboring elements
4. OCR within the same allowed region
5. reviewed visual similarity
6. operator or caller clarification

RobotGo should return the proposed repair and its evidence. It must not silently
rewrite durable workflows or widen the application, window, display, or region
scope.

## 9. Window-local coordinate virtualization

Provide logical window-local coordinates and normalized positions in addition
to global pixels:

```go
err := robotgo.ClickInWindow(window, 0.8, 0.9)
```

RobotGo resolves these immediately against the current exact window generation
and handles:

- DPI and fractional scaling
- multi-monitor origins
- rotated or transformed outputs
- moved and resized windows
- Wayland, X11, Windows, and macOS geometry differences

Semantic element actions remain preferable. Window-local coordinates offer a
safer fallback than persistent global coordinates.

## 10. Privacy-preserving perception

Visual understanding should minimize data before it crosses a trust boundary:

- capture only allow-listed displays and regions
- require a separate full-display grant
- apply explicit redaction masks before scaling or encoding
- prefer local OCR and scene-graph reduction
- optionally return only relevant tiles
- prune password, hidden, offscreen, and foreign-process accessibility content
- never persist normal observations to files
- zero RobotGo-owned raw, scratch, encoded, and retained buffers at their
  lifecycle boundary

An optional local-only perception mode could provide a reduced scene graph
without sending a full image to a model. Automatic secret detection remains
defense in depth and never replaces region allow-listing.

## 11. Reproducible GUI fixtures and evidence

Ship a first-class evaluation kit with disposable, self-owned fixture apps and
virtual desktops:

- deterministic dialogs, controls, canvases, and accessibility trees
- multiple DPI, scaling, theme, display, and transform configurations
- GNOME, KDE, wlroots, X11, Windows, and macOS evidence adapters
- injected stale targets, backend failures, cancellation, and cleanup failures
- privacy assertions for logs, files, audit events, protocol payloads, and
  retained memory
- measurable task success, false-target, ambiguity, and recovery rates

This is a product feature as much as test infrastructure: users can qualify
their own policies, backends, and workflows before touching a real desktop.

## 12. Explainable automation

RobotGo should be able to answer “why did you do that?” without leaking the
sensitive content itself:

> Selected candidate 4 because the role, accessible name, exact window
> identity, and observation bounds matched. Candidate 7 was rejected because
> its confidence was below policy.

The explanation contract includes sanitized decision provenance, policy rule,
confidence, ambiguity, backend, and observation/action IDs. It excludes raw
pixels, OCR text unless explicitly permitted, secrets, native handles, window
titles outside the approved contract, and backend error details.

## Flagship direction: RobotGo Verified Flows

The product category should be broader than a growing automation API or MCP
tool collection. RobotGo Verified Flows are proof-carrying, adaptive GUI
transactions for desktop agents:

> Resolve one described target, revalidate its live identity and state,
> authorize exactly one bounded action, execute through the safest permitted
> backend, verify a domain-relevant result, and return a privacy-reduced proof.

This builds on RobotGo's difficult existing foundations: native cross-platform
backends including Wayland, explicit capability and unsupported contracts,
private observation-bound references, immutable policy, input ownership,
verification, and deterministic cleanup. MCP, accessibility, image content,
OCR, and visual matching remain important inputs. The defensible product is
the contract that composes them safely.

### Proof-carrying GUI transaction

One verified transaction has six explicit phases:

| Phase | Contract |
| --- | --- |
| Resolve | Select exactly one target from retained, semantic, structural, and optionally visual evidence. |
| Validate | Re-read process, window, title, role, state, bounds, and offered action immediately before dispatch. |
| Authorize | Prove immutable policy or a single-use capability lease permits this exact target, action, fallback, and time window. |
| Execute | Prefer a native semantic action; use pointer or keyboard fallback only when policy explicitly permits it. |
| Verify | Evaluate a domain-relevant postcondition rather than assuming that dispatch or arbitrary pixel change means success. |
| Prove | Return the resolver decision, authorization, backend, outcome, verification, and cleanup state without sensitive payloads. |

“Transaction” must not imply an ACID rollback for irreversible desktop side
effects. Outcomes should remain honest about the mutation boundary:

- `rejected-before-dispatch`
- `failed-before-dispatch`
- `verified`
- `unverified-after-dispatch`
- `cleanup-pending`

Blind mutation retry is never valid after an unverified dispatch. A retry is
safe only after RobotGo can prove the earlier action did not occur or that the
requested postcondition is already satisfied.

### Observation-bound semantic element actions

The first P011 foundation is the typed `desktop.element-act` contract delivered
by LAB-77.
It consumes an observation ID, an observation-scoped element ID, one semantic
action, exact expected identity/state, and an optional postcondition. Before
dispatch RobotGo must:

1. resolve the private retained backend reference again;
2. prove the same process and exact window identity or generation;
3. re-read role, permitted name/state properties, bounds, and offered actions;
4. reject disappeared, recycled, ambiguous, disabled, materially moved, or
   otherwise changed elements as stale;
5. execute `press`, `focus`, `set-value`, `toggle`, `expand`, `collapse`,
   `increment`, or `decrement` only when both backend and policy allow it.

Coordinate fallback is a separate capability. It must never be inferred from
the presence of element bounds or silently substituted for a native semantic
action.

### Deterministic locator and controlled healing

A versioned `TargetSpec` should describe application and exact window
identity, role, accessible name, required states, hierarchy or neighboring
anchors, and optional reviewed visual evidence. Resolution follows an explicit
policy-bounded ladder:

```text
private retained reference
    -> exact semantic match
    -> structural or relative semantic match
    -> OCR or reviewed visual match
    -> explicit operator decision
```

Scores must be deterministic compositions of documented evidence, not opaque
model confidence. Each weaker fallback reports matched and changed evidence,
candidate count, rejected candidates, ambiguity, and the policy rule that
allowed the transition.

Healing modes should remain small and precise:

- `strict`: exact retained or semantic identity only;
- `adaptive`: one unique candidate above a fixed policy threshold may proceed;
- `review`: return a proposed locator patch without mutation.

More than one plausible candidate is always `ambiguous-target`. Durable
workflows are never rewritten silently, and critical operations cannot be
authorized solely by visual healing.

### Semantic conditions and action proof v1

LAB-79 introduces the narrow first condition contract: state present/absent,
focused/not-focused, and value-equals-action-value for `set-value`, all on the
same retained target. Checking that a postcondition is already satisfied before
dispatch makes retryable workflows safer and more idempotent. Element/window
appearance or disappearance, cross-element conditions, selection counts, row
counts, and bounded progress remain later slices rather than implied support.

Every attempted semantic element mutation yields a versioned, machine-readable
action proof containing only privacy-reduced metadata:

```json
{
  "schema_version": "1",
  "transaction_id": "action-7821",
  "status": "verified",
  "resolution": {
    "strategy": "retained-reference",
    "candidate_count": 1,
    "exact": true,
    "healing": false
  },
  "authorization": {
    "policy_allowed": true,
    "confirmation_required": true,
    "confirmed": true
  },
  "execution": {
    "backend": "windows-uia",
    "action": "toggle",
    "status": "dispatched",
    "fallback": false
  },
  "verification": {
    "condition_kind": "state-present",
    "status": "matched",
    "precheck_attempts": 1,
    "final_gate_checked": true,
    "postcondition_attempts": 2,
    "already_satisfied": false
  },
  "cleanup": {
    "transient_resources_released": true
  }
}
```

The default proof excludes screenshots, entered values, accessibility dumps,
native handles, and raw backend errors.

### Single-use capability leases and RobotGo Trace

A capability lease authorizes one action against one current process/window
generation and revalidated target for a short lifetime. It binds the action,
fallback ceiling, postcondition, policy digest, optional observation, expiry,
and consumption state. Any target change or expiry makes the lease unusable.

A later RobotGo Trace viewer should make the proof inspectable through a
timeline, semantic before/after diff, resolver candidates, backend and
fallback reason, policy decision, conditions, cancellation boundary, input
ownership, and cleanup. Its privacy levels are explicit:

1. `metadata-only`
2. `semantic-redacted`
3. `visual-redacted`
4. `full-explicit`

Recorder and code generation come after the transaction contract. They should
record semantic target evidence and observed effects, not merely global input
coordinates.

## Recommended delivery sequence

The current autonomous-GUI work establishes the required safety boundaries.
The recommended product sequence is:

1. policy-gated MCP image observations (`LAB-72`, complete)
2. bounded OCR and visual detection (`LAB-74`, complete)
3. observation-bound semantic element actions with full live revalidation
   (`LAB-77`, complete)
4. add target-relative semantic postconditions and Action Proof v1
   (`LAB-79`, complete)
5. introduce a versioned `TargetSpec` and deterministic explainable resolver
   (`LAB-81`, complete)
6. add single-use capability leases plus strict, adaptive, and review healing
   (`LAB-82`, complete)
7. integrate image/OCR evidence into the same resolver and authorization
   contract (`LAB-83`, complete)
8. add RobotGo Trace with explicit privacy levels
9. add recorder and verified-flow code generation
10. add a side-effect-free cross-platform capability planner
11. add secure secret entry, local previews, and higher-level declarative
    workflows on the same transaction contract

Each slice should be independently deny-by-default, reviewable, tested against
self-owned fixtures, and honest about unsupported platform behavior.

## Product moat

The defensible advantage is the combination, not any single primitive:

- cross-platform native and Pure-Go backends
- semantic-first perception with policy-gated visual fallback
- immutable capability and authorization contracts
- observation-bound, stale-safe targeting
- deterministic resource and sensitive-data cleanup
- uncertainty-aware decisions
- outcome verification and explainability
- reproducible platform evidence

The concise product promise is:

> RobotGo identifies a GUI target through multiple bounded perception sources,
> explains the evidence and uncertainty, acts only inside immutable policy, and
> proves the expected result without leaving sensitive artifacts behind.

In product terms: RobotGo is the policy-enforced verified execution runtime for
cross-platform desktop automation—the safety kernel for computer-use agents.
