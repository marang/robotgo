# Verified Adaptive Workflows Plan

Status: P011 in progress; LAB-83 policy-gated OCR/visual resolver evidence complete

Linear coordination:

- Project: [`RobotGo | P011 | Verified Adaptive Workflows`](https://linear.app/riotbox/project/robotgo-or-p011-or-verified-adaptive-workflows-0d00997b9381)
- Observation-bound element actions: [`LAB-77`](https://linear.app/riotbox/issue/LAB-77/add-observation-bound-semantic-element-actions)
- Semantic conditions and Action Proof v1: [`LAB-79`](https://linear.app/riotbox/issue/LAB-79/add-observation-bound-semantic-postconditions-and-action-proof-v1)
- TargetSpec v1 and deterministic resolver: [`LAB-81`](https://linear.app/riotbox/issue/LAB-81/add-versioned-semantic-targetspec-and-deterministic-resolver)
- Capability leases and controlled healing: [`LAB-82`](https://linear.app/riotbox/issue/LAB-82/add-single-use-capability-leases-and-controlled-semantic-healing)
- OCR/visual resolver evidence: [`LAB-83`](https://linear.app/riotbox/issue/LAB-83/integrate-policy-gated-ocr-and-visual-evidence-into-targetspec)
- Privacy-tiered trace: [`LAB-84`](https://linear.app/riotbox/issue/LAB-84/add-privacy-tiered-robotgo-trace-for-verified-transactions)
- Semantic recorder/codegen: [`LAB-85`](https://linear.app/riotbox/issue/LAB-85/add-semantic-recorder-and-verified-flow-code-generation)
- Side-effect-free capability planner: [`LAB-86`](https://linear.app/riotbox/issue/LAB-86/add-side-effect-free-verified-flow-capability-planner)

## Product contract

P011 moves RobotGo from bounded perception and typed input toward proof-carrying
desktop transactions:

```text
resolve -> validate -> authorize -> execute -> verify -> prove
```

The runtime must remain honest at the irreversible native dispatch boundary.
It never describes an unknown post-dispatch result as failed-before-dispatch or
silently retries it.

## LAB-77 semantic action slice

Schema v9 adds `desktop.element-act` and the `robotgo_element_act` MCP tool.
The operation consumes one live `desktop.inspect-ui` observation, one opaque
element ID, the exact caller-visible expectation, and one fixed semantic
action. Before native dispatch RobotGo re-resolves the private backend
reference and proves:

- the same process and exact top-level window/title;
- the same role, bounded name, states, logical bounds, and offered actions;
- the element remains non-sensitive, visible, enabled, and inside that window;
- immutable policy allows the operation and action, confirmation is present
  when required, and action count/rate/value-byte/action-time/session-time
  limits remain valid.

An observation whose accessible identity text was truncated or sanitized is
readable but not actionable; RobotGo cannot prove an exact live-name match from
partial identity data.

Linux uses AT-SPI object-path ancestry plus native Action, Component,
EditableText, and Value calls. Windows re-resolves the UI Automation runtime ID
inside the exact HWND/process and dispatches the appropriate control pattern.
macOS re-walks the observation-private PID/CGWindowID/child-index path and uses
native AX actions or settable attributes. No backend uses pointer, keyboard,
clipboard, shell, or visual fallback.

The protected Windows job mutates and re-observes only a self-owned edit
fixture. AT-SPI dispatch and failure boundaries are hermetic D-Bus-query
fixtures. macOS compiles on both supported architectures; permission-granted
AX mutation evidence remains pending with the existing macOS accessibility
runtime-evidence work rather than weakening or prompting around permission.

`set-value` accepts an empty valid UTF-8 value so callers can clear an editable
control. Its content is never retained in observations, results, audit events,
or error messages. Password and other sensitive elements remain ineligible.
An error returned after a native mutation call produces `unverified`; a stale
or unsupported target rejected before that call remains `failed`.

## LAB-79 conditions and proof slice

Catalog schema v10 adds one optional target-relative postcondition to
`desktop.element-act`: state present/absent, focused/not-focused, or
value-equals-action-value for `set-value`. Conditions are deliberately singular
and fixed-vocabulary; target disappearance, cross-element conditions, arbitrary
values, condition arrays, visual evidence, and healing remain later work.
The semantic observation schema remains v1.

RobotGo performs one quota-bearing semantic precheck before action accounting.
If the condition already matches, the request succeeds without consuming action
count or rate. Otherwise the native backend performs the final condition and
exact-identity gates immediately before at most one dispatch. Every native
condition probe is quota- and rate-bearing; policy capacity reserves one source
inspection, one precheck, up to three native final-gate probes, and every
configured post-dispatch attempt. Only a dispatched action is charged.
Post-dispatch checks use separate immutable attempt, interval, and timeout
bounds. State, focus, and value checks require the corresponding semantic
property grant.
The normalized binary-control contract keeps reversible checkbox/switch
`toggle` + `checked` separate from one-way radio `press` + `selected`; missing
native state/action providers and arbitrary action-list drift fail closed.

Every `ActUIElement` return carries privacy-reduced Action Proof v1. It reports
only opaque transaction lineage, fixed resolution and authorization decisions,
dispatch/skipped status, precheck/final-gate/postcheck evidence, cleanup state,
and a stable error code. It never includes target strings, action values,
native references, policy payloads, or raw backend errors. A native error after
dispatch remains `unverified-after-dispatch` even if later observation matches;
there is no fallback or automatic retry. Audit schema v3 mirrors the bounded
condition phase, proof/execution status, and attempt counts without payloads.

## LAB-81 TargetSpec and resolver slice

Catalog schema v11 adds TargetSpec v1 and the read-only `desktop.resolve-ui`
operation. A spec binds the exact source process/window target and private
policy title to one exact role/name, optional required state/action sets, and
an optional immediate-parent-first ancestor chain. The resolver scans only the
sanitized retained observation graph. It performs no native query, consumes no
query/observation/action quota, and grants no mutation authority.

Exact and structural semantic matching are deterministic and use fixed
payload-free evidence tokens. One unique match returns the opaque element ID
and a defensive exact expectation for `ActUIElement`; zero returns
`target-not-found`, and multiple matches return `ambiguous-target` without any
candidate IDs. Candidate/identity truncation returns
`incomplete-observation`, while redaction of hidden, sensitive, or disallowed
content does not by itself invalidate a complete allowed candidate set. There
is no fuzzy matching, healing, locator patching, OCR/visual evidence, or input
fallback in this slice. Audit schema v4 adds resolver start/finish evidence
without target names, titles, values, native references, or tree payloads.

## LAB-82 capability leases and controlled semantic healing

Catalog schema v12 adds fixed `strict`, `adaptive`, and `review` modes plus
Capability Lease v1. A lease is bound privately to one session, policy digest,
TargetSpec digest, retained observation element, exact action, optional
postcondition, optional set-value SHA-256 binding, expiry, and one native
dispatch. Only a SHA-256 hash of its random bearer token is retained. Replay,
expiry, wrong session, wrong binding, observation release, close, cancellation,
and pre-dispatch audit failure fail closed; concurrent callers can cross the
native dispatch boundary at most once.

Adaptive resolution uses semantic and structural evidence only. Role, required
state/action sets, enabled state, bounds, and hierarchy shape remain exact;
only target or ancestor names may drift under the deterministic 0-100 policy
threshold. Every qualifying candidate counts, so more than one is always
`ambiguous-target` and score ties never select by tree order. Review returns a
payload-free, non-executable changed-clause proposal and never stores a locator
rewrite or dispatches. Action Proof v2 and Audit schema v5 expose fixed lease
lifecycle and adaptive evidence without tokens, names, titles, values, native
references, or policy payloads.

## LAB-83 OCR and visual resolver evidence

Catalog schema v13 and TargetSpec v2 add at most one OCR clause and one visual
clause referencing opaque items from explicit prior analysis results. Both
clauses must share one live image observation. Semantic-only TargetSpec v1
remains accepted, and native accessibility candidates always keep precedence:
analysis evidence is considered only when semantic adaptation produces no
candidate and never overrides ambiguity or incomplete native evidence.

Immutable policy independently allow-lists evidence sources, exact
backend/model providers, analysis regions, minimum OCR/visual confidence, and
maximum evidence age. Redacted or downscaled views and sanitized, truncated,
clipped, future-dated, stale, released, or cross-observation analysis lineage
fail closed. OCR contributes a fixed 25 points and visual proposals 15 points
to an otherwise eligible native candidate whose bounds contain the selected
item center; scores cap at 100 and every qualifying candidate still counts.
There is no coordinate dispatch or implicit analysis.

Resolver results and MCP output contain fixed source/provider/age provenance
but no OCR text or pixels. Audit schema v6 records only source tokens and
bounded age. Retained evidence keeps geometry, confidence, fixed provenance,
and OCR language identifiers, and is cleared with its image observation or on
analysis publication failure. Executable leases bind both observations,
expire no later than the oldest evidence, and invalidate when either
observation is released. The checked-in `target_evidence_review` example shows
the non-executable visual path against a self-owned GUI.

## Next slices

1. privacy-tiered RobotGo Trace
   ([`LAB-84`](https://linear.app/riotbox/issue/LAB-84/add-privacy-tiered-robotgo-trace-for-verified-transactions));
2. semantic recorder/code generation
   ([`LAB-85`](https://linear.app/riotbox/issue/LAB-85/add-semantic-recorder-and-verified-flow-code-generation));
3. side-effect-free capability planner
   ([`LAB-86`](https://linear.app/riotbox/issue/LAB-86/add-side-effect-free-verified-flow-capability-planner)).

Each slice remains independently deny-by-default and must preserve exact target
scope, explicit fallback provenance, sensitive-data cleanup, and truthful
post-dispatch outcomes.
