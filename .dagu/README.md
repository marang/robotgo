# RobotGo native delivery

Native Entire Frankfurt is canonical source. These definitions run ordinary
Linux CI plus six isolated headless Sway cells on the existing OVH London worker through the shared three-slot `ci`
queue. They retain the qualified CGO image, non-root worker identity,
2 CPU / 3 GiB / 512 PID limits, read-only root filesystem, disposable workspace
and private temporary Xvfb/Weston sessions. Source keys, controller credentials,
webhook tokens, host display sockets and Podman sockets stay outside containers.

| Source definition | Installed DAG name |
| --- | --- |
| `pipeline.yaml` | `robotgo-ovh-ci` |
| `security.yaml` | `robotgo-ovh-security` |
| `dependencies.yaml` | `robotgo-ovh-dependencies` |
| `watch.yaml` | `robotgo-ovh-watch` |

`scripts/ci.sh` owns the test commands. The image recipe and active legacy-state
retention source/units live under `scripts/ci/`. The former sealed launcher,
admission and provisioning implementation is retired from current source;
its history remains available in native Git. The recipe records the admitted
image's inputs; mutable distribution packages make a rebuild a new image requiring
qualification. Existing immutable image layers are retained.

The five-minute watcher reads native branch refs through the host-only checkout
adapter. Its first run records a baseline. Later runs deduplicate full source
SHAs, exclude Entire metadata and release-artifact branches, and save the cursor
only after every dispatch succeeds or returns the exact duplicate-run response.
Tags do not trigger builds or releases. A webhook checks repository, branch and
full SHA; manually selecting `main` remains supported. Weekly dependency reports
list module updates and run reachable-vulnerability checks without source writes.
Scanner/network errors fail the job; update inventory is not a security verdict.

Entire state under `.entire/` stays local and ignored, including its existing
local ignore file. Repository Codex hooks no longer invoke Entire automatically.
Explicit native Git/Trail actions remain the reviewed delivery path; user-managed
local capture settings and global hooks are preserved.

Native macOS/Windows, real desktop/compositor consent, Hyprland/GNOME/KDE,
KVM/DRM, release evidence and public module distribution retain their existing
gates. Linux tests and cross-compilation do not replace the 23 retained platform
checks. Source now prepares `v1.0.4` under `go.izoone.com/robotgo`; the accepted
imgo version and old releases are unchanged. The
[module artifact contract](../docs/operations/module-distribution.md) does not add
a native release publisher. LAB-438 tracks the unqualified native CI writer;
operators must
verify exact source, definition/image, run and result before merging.

## Headless Sway runtime

The same CI graph adds six serialized siblings after `prepare`: native input,
capture, window, output, multi-output and portal availability. Ordinary steps
retain their existing image and resource/time limits. Sway uses a dependency-only
derivative image from `scripts/ci/Containerfile.sway`; the root DAG budget adds
six ten-minute cell budgets to the existing 3700 seconds.

The existing Sway test commands and assertions are reused. Explicit Dagu DAG,
run, actual attempt, step, source tree and image identities bind their sanitized
evidence. Historical GitHub evidence retains its original format and validation.
The multi-output cell first exercises the existing induced-failure cleanup seam.

Raw logs, session sockets and private preflight state stay on container `/tmp`
tmpfs. Only successfully verified owned report directories are copied to `/w`
and retained by Dagu's standard exit handler as private run artifacts. Container
force-removal remains the cleanup boundary after hard cancellation.

Normal branch polling supplies the source ref in its existing event payload.
Manual `main` works unchanged. A manual exact commit without that payload must
also provide `source_ref`, the actual full branch ref; conflicting metadata is
rejected. This prevents a commit-only selection from inventing a branch identity.

Definitions and local fixtures alone do not qualify installation. Require actual
exact-source OVH runs of all six cases plus failure, timeout and cancellation
cleanup before treating this runtime slice as complete. No Windows/macOS, other
desktop or release qualification follows from these checks.

## Qualification and cutover

Before installation read https://automation.izoone.com/agents and its worker
boundary; verify current host state and use the documented shared install lock.
A source push never installs definitions. All host writes require the operator's
authorized cutover. Preserve unrelated jobs, holds, credentials and frozen
journals. The original dirty cleanup worktree is not an installation source.

1. Validate the four actual installed filenames with the target Dagu version.
   Run `DAGU_BIN=/path/to/dagu python3 -B .dagu/test-watch.py` using Python and
   PyYAML, plus `python3 -B scripts/ci/retention/test_pruner.py`. The watcher
   fixtures use only temporary state and credential-free loopback HTTP.
2. Keep the watcher schedule and CI webhook held. Install reviewed exact bytes
   under the existing DAG names, retaining the current image. Register the
   `robotgo/ci-webhook` secret only with the controller; use the existing trusted
   checkout adapter. Keep retention timer/source unchanged.
3. Run exact-candidate CI, security and dependency jobs under the recorded caps.
   Require every guarded runtime test to PASS, source/tree cleanliness, and
   cleanup after success, induced failure and cancellation. Capture source SHA,
   definition/helper/image hashes, run IDs, results and cleanup evidence.
4. Qualify the five-minute watcher: baseline without dispatch, native branch
   change, duplicate/retry handling and exact accepted SHA. Only then enable its
   schedule and webhook. Holding execution requires both schedule suspension and
   webhook disablement because webhooks bypass schedule holds.
5. Verify all four installed jobs use these definitions and no active job
   invokes the former sealed `robotgo-ci` launcher. Its watcher/dependency calls
   have been replaced, and the obsolete driver/admission/provisioning source is
   retired. Preserve legacy state and the active retention timer, including
   journals, holds and images. Recover the prior implementation from native
   Git history only within an explicitly reviewed rollback.

Until the exact-head capped runs and cleanup checks pass, this remains a
reviewable replacement proposal. The dated installed-definition backup and
native Git history preserve the rollback boundary. The dated migration
JSON under `docs/evidence/2026-10-31-entire-ovh/` is preserved byte-for-byte and is
historical provenance, not current qualification.
