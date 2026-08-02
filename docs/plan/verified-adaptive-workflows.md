# Verified Adaptive Workflows Plan

Status: P011 in progress; LAB-77 semantic element-action slice complete

Linear coordination:

- Project: [`RobotGo | P011 | Verified Adaptive Workflows`](https://linear.app/riotbox/project/robotgo-or-p011-or-verified-adaptive-workflows-0d00997b9381)
- Observation-bound element actions: [`LAB-77`](https://linear.app/riotbox/issue/LAB-77/add-observation-bound-semantic-element-actions)

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

## Next slices

1. semantic preconditions/postconditions and Action Proof v1;
2. versioned `TargetSpec` and deterministic exact/structural resolver;
3. single-use capability leases and strict/adaptive/review healing modes;
4. policy-gated OCR/visual evidence in the same resolver;
5. privacy-tiered RobotGo Trace;
6. semantic recorder/code generation and a side-effect-free capability planner.

Each slice remains independently deny-by-default and must preserve exact target
scope, explicit fallback provenance, sensitive-data cleanup, and truthful
post-dispatch outcomes.
