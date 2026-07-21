# Agent Adapter and Evaluation Plan

Status: Base completed by LAB-13; visual-condition extension active in LAB-15

Linear coordination:

- Project: [`RobotGo | P003 | Agent Adapter and Evaluation`](https://linear.app/riotbox/project/robotgo-or-p003-or-agent-adapter-and-evaluation-7749d1ceaac3)
- Project ID: `3643b1db-00a4-4f93-8604-3e85d0207a0c`
- Issue: [`LAB-13 — Add safe MCP stdio adapter and hermetic protocol evaluation`](https://linear.app/riotbox/issue/LAB-13/add-safe-mcp-stdio-adapter-and-hermetic-protocol-evaluation)
- Extension project: [`RobotGo | P004 | Safe Visual Conditions`](https://linear.app/riotbox/project/robotgo-or-p004-or-safe-visual-conditions-9eebd34245ff)
- Extension: [`LAB-15 — Expose safe visual find and wait through MCP`](https://linear.app/riotbox/issue/LAB-15/expose-safe-visual-find-and-wait-through-mcp)

## Outcome

Expose the accepted `agent.Session` contract to local MCP clients without
moving policy, validation, desktop access, or sensitive capture ownership into
the protocol layer. The first adapter is intentionally small enough to audit
and evaluate as a complete boundary.

## Architecture and contracts

```text
local MCP client
      |
newline-delimited stdio
      |
cmd/robotgo-mcp
      |
agent/mcpserver (schema, projection, lifecycle only)
      |
agent.Session (policy, quota, confirmation, observe/find/wait/act/verify)
      |
RobotGo platform backends
```

The adapter uses the stable official
`github.com/modelcontextprotocol/go-sdk` module. It provides one session per
process. LAB-13 established four tools; LAB-15 extends the same boundary to
seven focused tools after the underlying Go contract was accepted:

| Tool | Contract |
|---|---|
| `robotgo_capabilities` | Return the immutable policy-filtered operation catalog without desktop I/O. |
| `robotgo_observe` | Ask the session for diagnostics or an explicitly policy-bounded capture, then return diagnostics and geometry only. Pixels and lineage digests stay in-process. |
| `robotgo_find` | Evaluate a typed condition against one explicit live observation without implicit capture. |
| `robotgo_wait` | Perform a finite policy-bounded wait over one explicit region and retain only a matched observation. |
| `robotgo_release_observation` | Idempotently zero and remove one retained observation without desktop I/O. |
| `robotgo_act` | Perform a dry-run by default. Actual input requires both `mode: "execute"` and a policy/confirmation combination that authorizes the request. |
| `robotgo_close` | Idempotently close the process-exclusive session and zero retained capture buffers. Later calls fail with one stable sanitized error. |

The command exposes stdio only. Protocol frames are the only stdout content;
startup and transport errors use stderr. Session close runs after peer exit,
transport error, cancellation, signal, or an explicit close call.

## Policy and privacy boundary

With no flags, the command allows only non-capturing runtime diagnostics with a
finite observation quota. Capture, display access, and all mutation remain
denied. A broader policy must come from an explicit file passed with `-policy`.
Policy JSON is size-bounded, rejects unknown fields and trailing values, and is
never read from protocol stdin.

MCP output never contains observation pixels, capture SHA-256 lineage, typed
text, raw backend errors, clipboard data, OCR data, or file-backed desktop
artifacts. The in-process session keeps any optional capture solely for stale
target checks, visual queries, and verification. Clients can release
observations explicitly; session close remains the final zeroing boundary.

## Evaluation evidence

The blocking hermetic suite connects the official MCP client and server over
in-memory transports. It covers initialization, exact tool listing, schemas,
capabilities, observation and visual-condition projection, explicit release,
dry-run and explicit execution routing, invalid input, target/pixel/raw-error
redaction, request cancellation, idempotent close, concurrent action/close,
post-close rejection, and cleanup on server cancellation. Fakes supply all
data; tests neither inspect nor mutate the real desktop and write no sensitive
artifact.

## Non-goals

- HTTP, SSE, OAuth, remote access, or multi-tenant service hosting
- OCR, accessibility trees, clipboard, window, or process tools
- implicit portal consent or automatic policy expansion
- duplicate validation, policy, quota, or backend logic in the adapter
- treating MCP tool annotations as an authorization boundary

Those capabilities require separate product slices after this boundary is
merged and evaluated.

## Exit criteria

- the focused protocol surface and stdio lifecycle are stable and documented
- default startup cannot capture the desktop or inject input
- execution requires an explicit policy plus explicit per-call execute intent
- sensitive values and unclassified errors cannot cross the output boundary
- hermetic MCP and command tests pass with race detection and without CGO
- default repository tests, vet, lint, and relevant platform/tagged suites pass
- GitHub CI and configured review surfaces are green with no unresolved finding
