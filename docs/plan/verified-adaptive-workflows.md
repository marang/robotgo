# Verified Adaptive Workflows Plan

Status: P011 in progress; LAB-81 TargetSpec v1 and deterministic resolver complete

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

## Next slices

1. single-use capability leases and strict/adaptive/review healing modes
   ([`LAB-82`](https://linear.app/riotbox/issue/LAB-82/add-single-use-capability-leases-and-controlled-semantic-healing));
2. policy-gated OCR/visual evidence in the same resolver
   ([`LAB-83`](https://linear.app/riotbox/issue/LAB-83/integrate-policy-gated-ocr-and-visual-evidence-into-targetspec));
3. privacy-tiered RobotGo Trace
   ([`LAB-84`](https://linear.app/riotbox/issue/LAB-84/add-privacy-tiered-robotgo-trace-for-verified-transactions));
4. semantic recorder/code generation
   ([`LAB-85`](https://linear.app/riotbox/issue/LAB-85/add-semantic-recorder-and-verified-flow-code-generation));
5. side-effect-free capability planner
   ([`LAB-86`](https://linear.app/riotbox/issue/LAB-86/add-side-effect-free-verified-flow-capability-planner)).

Each slice remains independently deny-by-default and must preserve exact target
scope, explicit fallback provenance, sensitive-data cleanup, and truthful
post-dispatch outcomes.
