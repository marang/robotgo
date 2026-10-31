# Native Entire / OVH migration

[LAB-437](https://linear.app/riotbox/issue/LAB-437), 2026-10-30.
Source migration and the current normalized OVH runtime are verified. Actual
CI/security/dependency runs and an automatic scheduler-watch succeeded after
the central account/home normalization. Three Dagu schedules and the dedicated
retention timer are active. Source integration is tracked in
[the native LAB-437 Trail](https://entire.com/et/marang/-/trails/4).

The evidence files below retain the initial qualification snapshot, not a
permanent live baseline. The closeout also admits exactly the reviewed
non-executable `.codex/hooks.json` and `.entire/.gitignore` Git blobs introduced
by Entire activation; every other capture/session/config path stays denied.
Worker upgrades after account normalization require explicit reconciliation
against completed historical journals and the independently rebuilt normalized
baseline, without modifying those journals. Current upgrade/CI receipts are
recorded in LAB-437.

The subsequently requested complete first-party GitHub exit, including a new
module address, is the separate [P015 delivery plan](../plan/github-exit.md).
LAB-437's verified foundation is not a claim that those remaining dependencies
have already been removed. Current Entire CLI public visibility is
authenticated-user read access; public module distribution is a planned new
surface, not a reason to weaken native authentication.

## Central account normalization

The current public worker guide and live accounts now use `izoone_automation`
(`/home/izoone_automation`) and `izoone_workers` (`/home/izoone_workers`), retaining
the same UIDs/GIDs. The central transaction completed at 21:13 UTC and preserved
credentials, holds, history, service identities and image layers. Its actor is
not identified in this task. RobotGo did not overwrite or undo that transaction.
The central operator also rewrote live fixed DAG paths, retention paths and the
sealed launcher/driver. An independent offline Go 1.26.10 rebuild of the locally
normalized source matches both live code hashes exactly; the immutable CI image
is unchanged. Current helpers use the new boundary and reject legacy profiles.

An automatic watch failed before private run initialization at 21:07; its exact
exception was not captured. A probe queued before normalization subsequently
failed because its immutable old DAG snapshot referenced the removed old home.
Those failures are retained. Fresh named runs use the current definitions.
The qualification table below records the successful **normalized** baseline;
earlier results remain separately archived. Current verified results are in the
[runtime record](../../automation/ovh/evidence/runtime-verification.json).

## Source identity and publication

The public GitHub fork `marang/robotgo` published 28 branches and eight annotated
tags. They were transferred with explicit heads/tags refspecs, preserving both
tag-object and peeled commit hashes. An independent Entire fetch matched every
ref; strict full fsck passed. No local-only branch or internal Entire ref was
transferred and no unrestricted mirror push was used.

Native repository `/et/marang/robotgo` is public, ID
`01M6741GKY2C7Y5SY71CYXVZZJ`, with its sole primary in Frankfurt
(`aws-eu-central-1`, EU jurisdiction). Project `marang` reports the same primary
processing cell and EU region. The original source-transfer HEAD was `main` at
`700a70e2a55ee27c2a7dbe4f3de80a967f5775b9`.
[Exact source evidence](../../automation/ovh/evidence/source-verification.json)
records the refs.

Entire's public visibility permits authenticated Entire readers; the actual
anonymous Git advertisement currently returns HTTP 401. The preserved GitHub
repository remains the anonymous source/distribution path. Native `main` is
protected against force pushes and deletion; GitHub check/review requirements
are not automatically translated into native server-side merge gates.

Default `origin` now uses native Entire; `github` retains the former origin
and `upstream` retains `go-vgo/robotgo`. Published local branches track the
new origin. Existing local-only branches/tags were preserved; in particular the
pre-existing local `v1.0.2` tag is not the authoritative published tag. Fetch
with `--no-tags` and inspect published tag hashes explicitly.

The user granted a RobotGo-specific exception for the already published 2017
`test/test.bmp` screenshot fixture (blob
`897b99346d859eaef18369a045e7673f45a513bb`). It was already deleted from
current source but remains in all published histories. It was retained to
preserve hashes; no history was rewritten. The exception permits no new capture,
session, transcript or checkpoint upload. The initial migration enabled no
provider mirror, cloud Agent or Trail. The 2026-10-31 central policy now permits
Native Entire Trails for proposals/reviews, separately from cloud execution,
telemetry and uploads. Preserve permitted local capture and keep uploads disabled;
local capture settings remain user-managed. Checkpoint uploads are disabled;
migration and worker Entire invocations opt out of telemetry.

## Operational boundary

Before changes read the [central guide](https://automation.izoone.com/agents)
and [worker execution boundary](https://automation.izoone.com/agents/worker-accounts.md).
Verify current authenticated operational state before shared-host changes. Historical provisioning
examples are not current installation instructions.

Execution is on the existing OVH London, UK worker `izoone-ovh-01`, an explicitly
accepted non-EU location. The notebook fallback remains stopped. DreamHost
retains email, domains and DNS. No application deployment or release was made.

Trusted SSH provisioning uses the existing persistent `native-activation.lock`
and a fresh reviewed snapshot. A concurrent Riotbox migration invalidated the
initial thirteen-entry snapshot: RobotGo correctly refused without writes.
The reviewed replacement baseline contained seventeen entries; RobotGo then
added four held definitions, retaining all existing definitions, holds, helpers,
auth, configuration and history. Counts are observations, not future baselines.

The additive checker never bypasses the canonical hardener's stopped-service
and empty-history guards. Historical exact-manifest verifiers intentionally
reject later additions; use the matching transaction baseline/checker and a
fresh complete inventory, not an old manifest count. Remote definition writes,
inline execution, terminal access and Git sync remain disabled.

## Fixed tasks and verification gates

The sealed static Go launcher verifies worker identity and the driver's digest
before starting isolated Python. Only `ci`, `security` and `dependencies` are
accepted, plus `--if-changed` for CI. The host authenticates to native Entire
with the existing separately authorized EU service login; no credentials or
new access grants were copied. Containers receive only a disposable current-main
snapshot, never the host object store, auth home, SSH keys or Podman socket.

Runtime limits remain one worker job, two CPUs, 8 GiB and 256 PIDs, rootless
Podman, no capabilities and no-new-privileges. The image pins Go 1.26.10 and
lint/security tools; an immutable local image ID is sealed in the driver.
Image provisioning used a separate capped transient unit and OCI namespaces,
not weaker shared-PID chroot execution. Raw reports/logs stay private on OVH;
Dagu receives only a bounded status summary.

Existing public package registries and the Go advisory service still receive
dependency metadata requests. Their residency was not independently certified;
European source/worker placement is not an end-to-end registry residency claim.
No new US runner, AI processing service or artifact host was introduced.

| Scope | Status / retained dependency |
| --- | --- |
| Linux CGO/Pure-Go tests/build/vet/lint, API variants, race, sanitizers, OCR, tagged unit contracts and synthetic Xvfb | Actual normalized-account OVH qualification passed for native `main` at `700a70e2`; 10m22s. |
| Security and Go update inventory | Actual normalized-account OVH runs passed in 8.6s/11.1s. Pinned govulncheck 1.9.0 retains its failing text-mode vulnerability verdict; inventory is report-only, never an automatic update/push. |
| Sway, real portal/compositor consent and output interaction | Compile-only where included; existing GitHub-provenance wrappers are not fabricated for Dagu. |
| Native macOS ARM64/Intel and Windows | Existing platform runners remain required; cross-compilation is not runtime evidence. |
| GNOME/KDE VM and Hyprland evidence | Existing KVM/VKMS/device/larger-resource requirements are outside the current admitted worker. |
| Dependabot | Existing GitHub Go-module/Actions update-PR automation remains separate; reports do not replace PR generation. |
| Review, module distribution, releases and published evidence assets | New proposals/reviews use native Trails. Existing GitHub PRs/checks, module identity and release gates/assets remain until verified replacements. Native-origin pushes do not trigger GitHub workflows or automatically synchronize providers. |

Private RobotGo state has its own copied fourteen-day pruner, restricted to
`robotgo-ovh`, with an independent maximum of 100 candidates per invocation.
Its hourly timer avoids consuming the existing projects' daily 100-run budget.
The RobotGo installation left the shared pruner/service/timer byte- and
inode-identical to its reviewed baseline. Subsequent concurrent migration work
added `sway-session-ci` to the shared pruner/service; those changes were observed
and preserved, not restored to an older snapshot. RobotGo remains exclusively
in its dedicated pruner. Actual manual
execution of the hardened own unit removed only a deliberately aged empty
synthetic fixture; the persistent runner lock and young state were preserved.
The own timer was enabled after the successful Dagu proofs and preserved by
central normalization. It runs hourly with up to five minutes' jitter and
retains fourteen days of private RobotGo runs. Current timer observations belong
in the runtime evidence, rather than becoming a future scheduling baseline.

## Qualification and local review

Actual Dagu proofs on `izoone-ovh-01`, all for native `main` at `700a70e2`:

| DAG | Run ID | Result |
| --- | --- | --- |
| CI | `robotgo-ci-postrename-20261030` | Succeeded, 10m22s |
| Security | `robotgo-security-postrename-20261030` | Succeeded, 8.6s |
| Dependencies | `robotgo-dependencies-postrename-20261030` | Succeeded, 11.1s |
| Automatic watch | `034p6S0lVS3vXYmm9ZNvdh` | Succeeded, `unchanged`, 0.52s; scheduler trigger; no source checkout/container |

The existing single worker is shared; other projects' jobs were not interrupted.
No running or queued state counts as a successful proof or a reason to release
scheduled holds. Bounded worker summaries were independently read from private
Dagu logs via trusted SSH and checked against the actual run API, current sealed
profile/image and private successful-CI marker. Raw reports/logs stay on OVH.

The original reviewed release transaction used the existing persistent shared lock and a
fresh 26-entry manifest snapshot, not the dated seventeen-entry admission
baseline. Its exact postcheck matched the snapshot minus only RobotGo's four
empty hold files. All other definitions, protected files and current holds were
preserved. Normalization preserved those activated schedules; no second release
or replay of the old journal was performed. Existing mandatory legacy holds
remain. A fresh normalized-boundary snapshot passes, and the authenticated API
now
reports the four RobotGo DAGs unsuspended, with no definition errors:

- CI: manual only; no cron.
- Watch: `CRON_TZ=Europe/Vienna 7-59/15 * * * *`.
- Security: `CRON_TZ=Europe/Vienna 17 4 * * 1`.
- Dependencies: `CRON_TZ=Europe/Vienna 27 4 * * 1`.

[Current runtime results and boundary evidence](../../automation/ovh/evidence/runtime-verification.json)
and [the normalized qualification attestation](../../automation/ovh/evidence/post-normalization-proof.json)
bind the four completed runs to the commit, immutable image, exactly reproduced
sealed profile and success marker. The successful automatic watch was queued by
the scheduler at 22:22 Vienna and completed after CI at 21:41:01 UTC.
[Historical runtime results](../../automation/ovh/evidence/runtime-verification-pre-normalization.json)
and [the original release attestation](../../automation/ovh/evidence/activation-proof-pre-normalization.json)
retain earlier outcomes and receipt hashes unchanged. Definition/inline/edit-retry requests still return 403;
unknown named parameters return 400 and terminal access returns 404. Live
cancellation and credential-expiry/refresh were not forced; offline cancellation
regressions pass. No application deployment or release was used for verification.
Pre-normalization installer/upgrade journals remain historical: current helpers
reject legacy profile identities. A future profile upgrade requires an explicitly
reviewed normalized baseline, not deletion or replay of those journals.

The first actual attempts failed and their history is retained. CI exposed an
inherited restrictive umask that changed executable/permission fixture modes,
and an overlong temporary prefix for Unix sockets. Security/dependency scans
reported no vulnerabilities but correctly failed on read-only Go-cache cleanup.
The v2 pipeline fixes the execution environment and scoped cleanup, not the
application tests; no failing tests were removed. Its security/dependency runs
succeeded. Its CI run passed through Wayland and Xvfb suites, then exposed
Debian bookworm's `xkbcli` 1.5 missing the oracle's required `keycode` field.
The v3 image builds SHA256-pinned upstream 1.6.0 oracle tools in an isolated
prefix, statically linked to their own xkbcommon implementation. RobotGo still
uses distro libraries; the application and test harness are unchanged. The
fixed pipeline rejects an unexpected oracle version before running tests.
All 121 local migration regressions pass, including real Git checkout/socket/
cache-cleanup fixtures, successor-upgrade/held-schedule transactions and explicit
rejection of legacy profile identities. Headless default Go tests also pass with
Go 1.26.10 after normalization.

Canonical `main` and the LAB-429/431/432 worktrees remain unchanged; GitHub
PRs #184, #185 and #187 are not merged by this task. Migration code, fixed YAMLs,
retention artifacts, agent guidance and evidence are the LAB-437 integration
change. See [the worker artifacts](../../automation/ovh/). The user subsequently
authorized its commit, native feature-branch push, review and merge on 2026-10-31.
No tag, release, original-repository deletion or service cancellation is
authorized by that closeout.

## Local review and native Trail handoff

The 2026-10-31 local review covered tracked and untracked migration changes
against `700a70e2a55ee27c2a7dbe4f3de80a967f5775b9`. Three independent reviewers
plus coordinator lenses retained no confirmed findings. All 121 offline migration
regressions, headless default Go tests, launcher package tests, schema/seal and
diff checks passed. Live cancellation and credential-refresh behavior were not
newly exercised. Image-build output has no separate byte cap; whether the
recurring-job log budget should also cover provisioning is a non-blocking
hardening question, not a claim of an observed failure.

[LAB-437's native Trail](https://entire.com/et/marang/-/trails/4) was initially
created as repository-scoped intent only, with a reciprocal Linear link. Its
current branch, reviews and merge result are recorded there, not inferred from
the initial intent. Immediately before creation, the complete project-agent
list and project plugins were empty;
repository `runners_enabled=false`, `auto_run_on_push=false` and
`trails_enabled=true`. This is a dated precheck, not a permanent automation
guarantee. No cloud execution, telemetry or capture/session upload was enabled.

LAB-437's operational foundation and its source integration are distinct from
the full GitHub exit, which remains planned in P015/LAB-438–441. No legacy
release/platform gate or original repository has been retired. Remove the
ticket worktree only after its native merge and local main synchronization
are verified and no needed uncommitted work remains.
