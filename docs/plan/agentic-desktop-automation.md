# Agentic Desktop Automation Plan

Status: Draft

Linear coordination:

- Team: `Lab` (`LAB`)
- Team ID: `38f32d3c-b65c-409e-86eb-2996abd84d3e`
- Project: [`P001 | RobotGo Agentic Desktop Automation`](https://linear.app/riotbox/project/p001-or-robotgo-agentic-desktop-automation-342ce54e76e6)
- Project ID: `b24081a5-dcab-44f7-803a-948bc563a03f`
- Plan: [`RobotGo Agentic Desktop Automation — Architecture and Delivery Plan`](https://linear.app/riotbox/document/robotgo-agentic-desktop-automation-architecture-and-delivery-plan-f145b8426b88)
- Plan document ID: `41fa02c3-706f-444a-acd0-ab1373165ee9`

## 1. Outcome

Make RobotGo a safe, observable desktop-automation runtime for agents without
weakening its existing cross-platform Go API or its explicit backend contracts.

The agent-facing layer should let a caller:

1. discover exactly which operations are available
2. observe the desktop in a bounded and privacy-aware form
3. propose and authorize typed actions
4. execute actions with deadlines and deterministic cleanup
5. verify the resulting desktop state
6. retain a redacted audit and replay trail

The initial project is an architecture and delivery container. Do not create a
large speculative issue backlog before the first executable slice and its
boundaries are accepted.

## 2. Architectural direction

Keep the existing `robotgo` package as the compatibility and low-level backend
surface. Add a session-oriented agent layer above it:

```text
LLM or workflow
      |
MCP, JSON-RPC, or Go adapter
      |
Agent session
  - operation capability catalog
  - policy and approval
  - observe -> act -> verify
  - audit and replay
      |
RobotGo platform backends
```

The protocol adapter must remain thin. Session, policy, validation, execution,
and verification behavior belong in reusable Go packages rather than an MCP or
CLI entry point.

## 3. Planned capability slices

### 3.1 Session ownership

Introduce an explicit session that owns configuration, active capture/input
leases, deadlines, cleanup, policy, and audit state. Existing package-level
functions remain source-compatible and may delegate to a default session.

The design must prevent two concurrent agent sessions from silently replacing
each other's ScreenCast or RemoteDesktop state. Until backend state is fully
instance-owned, the agent layer must expose and enforce any process-wide
exclusivity honestly.

### 3.2 Operation-level capability catalog

Extend feature-level diagnostics with a versioned operation catalog. Report
availability per operation, including:

- backend and fallback
- input constraints
- consent and permission requirements
- risk class and confirmation requirement
- timeout/cancellation support
- unsupported reason and remediation

Initial operation families are desktop observation, screen capture, pointer,
keyboard, clipboard, window, and process operations.

### 3.3 Typed actions and structured results

The agent surface must not expose legacy variadic or `interface{}` argument
shapes. It uses strict, JSON-serializable action and target types with complete
preflight validation.

Results include an action ID, status, selected backend, duration, observation
lineage, and structured error data. Errors distinguish at least unsupported,
permission/consent, invalid input, stale target, policy denial, timeout,
retryable failure, and unknown outcome.

### 3.4 Observe, act, and verify

Mutating actions can bind to an observation and declare preconditions, such as
the expected active window or unchanged target region. Verification uses
bounded conditions rather than fixed sleeps where possible.

Useful conditions include window title/state, process state, pixel or region
change, OCR text, and later accessibility element state.

### 3.5 Semantic grounding

Add a normalized UI-element model with role, name, value, state, bounds, and
source. Prefer native accessibility data where a platform exposes it and use
structured OCR or visual matching as an explicit fallback.

Wayland limitations and portal consent remain visible. The agent layer must
never imply universal foreign-window or accessibility support.

### 3.6 Policy, approval, and privacy

Classify actions as read-only, sensitive read, reversible mutation, or
destructive. A policy can constrain:

- allowed operations
- target processes and windows
- displays and screen regions
- clipboard access
- process execution and termination
- action count, text length, rate, and total duration

Screenshots, clipboard contents, typed secrets, restore tokens, and equivalent
sensitive data are excluded from logs by default. Portal consent dialogs remain
explicit user-authorized actions.

### 3.7 Adapter and evaluation

After the Go session contract is proven, add a thin MCP adapter with a small
orthogonal tool set:

- capabilities
- observe
- find
- act
- wait
- close session

Add redacted record/replay, a fake driver, and hermetic evaluation tasks before
claiming reliable autonomous behavior.

## 4. First executable slice

The first implementation slice should prove the architecture with:

1. session creation and deterministic close
2. versioned operation catalog
3. policy evaluation and dry-run
4. desktop observation with runtime diagnostics and optional capture
5. typed move, click, and text actions
6. action result and structured error schema
7. unit tests, a runnable example, and one applicable real-runtime integration
   path

An MCP server is not required for this first slice. It should consume the
accepted Go contract rather than define it.

## 5. Exit criteria

The project is ready to transition from architecture to broader implementation
when:

- session ownership and process-global limitations are explicit and tested
- operation availability can be consumed without parsing prose
- every exposed mutation passes policy and preflight before desktop input
- cancellation releases RobotGo-owned input and session resources
- observation lineage can detect a stale or changed target
- sensitive payloads are absent from default diagnostics and traces
- the first slice works through the Go API and has a stable adapter boundary

Platform support remains governed by `AGENTS.md`, `TEST.md`, and the existing
Wayland-first fallback policy.
