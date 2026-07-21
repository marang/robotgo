# Safe Agent Visual Conditions Plan

Status: Go contract completed by LAB-14; MCP projection active in LAB-15

Linear coordination:

- Project: [`RobotGo | P004 | Safe Visual Conditions`](https://linear.app/riotbox/project/robotgo-or-p004-or-safe-visual-conditions-9eebd34245ff)
- Project ID: `94e14e7f-81c0-4808-b932-a8211ece1b8b`
- Issue: [`LAB-14 — Add bounded visual find and wait conditions`](https://linear.app/riotbox/issue/LAB-14/add-bounded-visual-find-and-wait-conditions)
- Issue: [`LAB-15 — Expose safe visual find and wait through MCP`](https://linear.app/riotbox/issue/LAB-15/expose-safe-visual-find-and-wait-through-mcp)

## Outcome

Provide reusable, privacy-aware visual conditions above `agent.Session` before
adding protocol tools or broader semantic grounding. A caller can search one
explicit observation or wait on one explicit region without implicit
full-screen capture, fixed sleeps, file-backed screenshots, or sensitive
payloads in results and audit events.

## Initial contract

`Session.FindColor` evaluates a typed RGB/tolerance condition against a live
capture already owned by the same session. It never calls a desktop capture
backend. The result contains only match state, global logical coordinates,
display ID, condition ID, and observation lineage.

`Session.WaitColor` performs finite capture attempts for one explicit
`CaptureRegion`. Attempts, interval, timeout, query count, observation count,
pixel count, display access, and optional confirmation all come from immutable
session policy. Cooperative cancellation is checked between captures and on
each scanned row; a synchronous platform capture may still need to return
before its buffer can be zeroed.

Every nonmatching or failed frame is zeroed immediately. Only a matched frame
may remain session-owned. The sanitized result references it by observation ID,
and `Session.ReleaseObservation` lets callers zero and remove it without
receiving capture metadata. Session close remains the final cleanup boundary.

## Policy and capability boundary

The catalog adds `desktop.find-color` and `desktop.wait-color` as sensitive
reads. `desktop.find-color` also requires `desktop.observe`, because it can
only consume an observation created inside the session. It therefore also
requires explicit capture/display limits, and its catalog entry reports the
capture backend needed to create that observation. `desktop.wait-color`
requires the same capture/display limits and positive bounded wait settings.
Both share `MaxQueries`; wait attempts additionally consume the existing
`MaxObservations` budget.

LAB-15 projects the accepted contract through `robotgo_find` and
`robotgo_wait`; both delegate directly to these session methods. The adapter
does not reimplement matching, capture, authorization, confirmation, quotas,
audit, cancellation, or cleanup. `robotgo_release_observation` provides the
explicit zero-and-remove boundary for a matched wait observation without
requiring the client to close the whole session.

## Privacy and safety constraints

- no implicit capture or portal-consent request
- no pixels, target RGB values, tolerance, or capture digest in results/audit
- no screenshot, template, OCR, clipboard, or replay file on disk
- invalid input, policy denial, and audit-intent failure precede desktop I/O
- timeout, cancellation, backend failure, no-match, and audit failure have
  explicit structured errors and deterministic cleanup
- examples are inspection-only until an explicit capture flag is supplied

## Validation

Hermetic fake-driver tests cover match/no-match, absolute coordinates,
tolerance validation, confirmation, query and observation quotas, stale/closed
observations, bounded exhaustion, timeout, payload-free audit, audit failures,
and source/retained-buffer cleanup. The opt-in agent capture integration path
also exercises both operations without writing a desktop artifact.

## Non-goals

- OCR or accessibility-tree queries
- bitmap/template files or caller-supplied screenshot persistence
- implicit full-screen capture or unbounded polling
- window/process conditions
- semantic image matching or duplicated protocol-layer condition logic

## Exit criteria

- the Go contract, policy fields, capability catalog, error model, and audit
  schema are versioned and documented
- all temporary sensitive buffers are zeroed on every return path
- default, race, non-CGO, lint, vet, tagged capture, and integration compile
  gates pass
- GitHub CI and configured review surfaces are green with no unresolved finding
- the MCP projection exposes only sanitized results and deterministic release
