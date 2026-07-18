# Upstream Compatibility Audit

RobotGo tracks useful changes from
[`go-vgo/robotgo`](https://github.com/go-vgo/robotgo) without treating upstream
`master` as an automatically trusted source. Each port must preserve this
fork's explicit error, lifecycle, platform-boundary, and test contracts.

Last audited upstream revision:
`766c6abccc400cc9a2aa96481c2493355b93fe29` (2026-07-08).

| Upstream area | Fork status | Decision |
|---|---|---|
| Public helper names (`CmdV`, `Paste`, `Type`, `TypeDelay`, `ClickV1`, `MultiClick`, `Capture1`, `SaveCaptureGo`) | Compatible | Added as portable aliases or checked helpers without changing established signatures |
| Process termination validation | Superseded | The fork rejects unsafe PIDs and binds forced window termination to a verified stable process handle or `pidfd` |
| Keyboard release ordering and macOS click/movement fixes | Superseded | Equivalent or stronger ownership, rollback, ordering, and bounded-movement contracts are already tested |
| Pure-Go X11 main display ID | Intentionally different | The fork keeps display ID 0 as primary because its public capture IDs address Xinerama outputs; upstream indexes X protocol screens instead |
| Experimental Pure-Go Wayland screencopy | Not ported | The upstream path has unbounded waits, incomplete output scale/transform semantics, unsafe size arithmetic, and weaker connection/resource lifecycle behavior |
| Pure-Go Wayland output enumeration | Superseded | The fork implements a dependency-free, read-only `wl_output`/`xdg-output` client with bounded dial/read/write, validated wire frames and versions, logical multi-output geometry, deterministic primary-first indices, explicit errors, and hermetic plus Weston evidence |

The accepted output-enumeration slice does not change capture selection.
Additional Pure-Go Wayland protocol work remains eligible only as a hardened
backend: bounded cancellation, checked buffer arithmetic, deterministic
cleanup, logical multi-output geometry, transforms, explicit unsupported
errors, and hermetic plus real-compositor evidence are required before capture
code can replace or precede the current portal path.
