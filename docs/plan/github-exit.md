# P015: GitHub-independent RobotGo delivery

Status: accepted direction; implementation and publication decisions pending.
Date: 2026-10-31 (Europe/Vienna).
[Linear project](https://linear.app/riotbox/project/robotgo-p015-github-independent-delivery-02316a64881b).

The user chose a complete **first-party** GitHub exit, including the module
address. This plan is not a deployment, DNS, purchase, credential, commit, push
or release authorization. [LAB-437](https://linear.app/riotbox/issue/LAB-437)
delivers the already verified native source / admitted Linux-CI foundation;
P015 owns the remaining exit, not a retroactive claim that it already happened.

## Outcome and scope

New RobotGo development, source review, required CI, dependency proposals,
release evidence, release publication and module consumption must work without
first-party GitHub APIs, Actions, PRs, Releases or GitHub credentials.

Previously published Git objects/tags and old module versions remain immutable.
Existing consumers of `github.com/marang/robotgo` are not transparently redirected
by an owned domain. They need an explicit migration to a new identity. Keep the
original repository and optional upstream remote; do not delete or rewrite them.
Historical links/attribution and third-party projects hosted on GitHub are
distinct from first-party operational dependence. Claiming zero GitHub network
access additionally requires cold-cache dependency/tool/image egress proof.

Native source stays in Entire project `marang`, Frankfurt/EU. Execution remains
on the accepted OVH London, UK primary. Ordinary Linux CI retains the qualified
2-CPU / 3-GiB / 512-PID rootless boundary and the existing CGO image. Shared queue
capacity is three jobs; each RobotGo graph serializes its own test steps.
Notebook fallback stays stopped; DreamHost retains email,
domains and DNS. Native Entire Trails are the canonical proposal/review path;
reuse intent and link it reciprocally with Linear. Check live project automations
before Trail writes and use `--no-branch` while source publication is unauthorized.
Trail permission does not authorize cloud-agent execution, telemetry, provider
changes or session/transcript/screenshot/checkpoint uploads. Preserve permitted
local capture and keep uploads disabled. No US hosting/processing, paid resources,
provider mirrors or broader credential access are introduced by this plan.

## Verified starting point

The [migration record](../operations/entire-ovh-migration.md) and
[dated migration evidence](../evidence/2026-10-31-entire-ovh/runtime-verification.json)
record source/ref verification, authenticated host-only source access, fixed
Dagu definitions and successful migration-era CI/security/dependency plus
automatic-watch runs. They are not proof of the remaining native/platform lanes.

Current installed Entire CLI 0.12.2 explicitly defines public visibility as
pull access for authenticated Entire users (`entire repo edit --help`, inspected
2026-10-31). An anonymous 401 therefore does not itself indicate a broken
visibility setting. It is not permission to weaken authentication. Public
module downloads need a separate supported read path if Native Entire's
authenticated Git transport cannot serve ordinary consumers.

`entire repo protection add --help` exposes force/deletion protection and
server-side-merge-only protection. Current native `main` has
`serverSideMergeOnly=false`. The inspected CLI does not establish an external
required-CI-check integration; that capability must be proved, not inferred.
Native Trail merge help exposes approval/CI gates and `--dry-run`, but the
actual Dagu-to-required-check mapping and enforcement remain to be qualified.
The repository-owned [LAB-460 pipeline](../../.dagu/README.md) adds exact-SHA
feature-head execution and five-minute branch polling. Source installation,
actual capped runs and the external CI writer remain separate gates.

[Primary-source feasibility research](github-exit-feasibility.md) covers Go
identity/discovery, immutable module bundles, authentication and distribution
limits. Its public endpoint proposals are not installed services.

## Dependency inventory and target

| Current dependency | Evidence / seam | Replacement and non-negotiable gate |
| --- | --- | --- |
| First-party Go module address | `go.mod`, self-imports, examples, API baselines and CI admission | Explicit new identity/release; consumer migration; old tags and checksums unchanged. |
| Public source/module access | Native Git requires an Entire login under current public policy | Prefer testing an anonymous read-only **tagged module** distribution surface; source credentials remain host-only. It is not a Git mirror or public write service. |
| PRs / required checks | GitHub review rules; native Trail review rules; exact-head operator CI verification | Native Entire Trails with authentic exact-candidate/base admission and review/evidence gates. Qualify enforcement and external CI mapping before retiring the old gate. |
| Evidence identity | `internal/releaseevidence`, `internal/compositorevidence`, `internal/supportmatrix` hardcode GitHub provider/IDs/URLs | Provider-aware, source-bound Dagu evidence; preserve historical validation; never fabricate `GITHUB_*` variables. |
| Weston output runtime | `.github/workflows/go.yml`; ordinary Linux CI includes both guarded Weston runtime tests | Preserve both real Weston tests and require actual PASS on each replacement exact head. |
| Six Sway runtime cells | `.github/workflows/sway-e2e.yml`, `scripts/run-sway-e2e.sh` | Headless/pixman is a plausible fit; genuine Dagu provenance and induced-failure cleanup must pass under unchanged caps. |
| GNOME/KDE and Hyprland | Portal manifests / QEMU require 4 vCPU, 8192 MiB, 40 GiB and KVM; Hyprland requests 4 CPUs/6 GiB plus VKMS/DRM | Separately approved resource/device capacity or a newly qualified equivalent profile. Current no-device/2-CPU admission is insufficient unchanged. |
| Native macOS and Windows | `.github/workflows/go.yml` and six-cell release snapshots | Approved existing native capacity in allowed locations; cross-compilation is not runtime proof. Preserve existing support limits, including LAB-69's separate permission evidence. |
| Release ledger and assets | `.github/workflows/release-evidence.yml`, `.github/release-required-checks.txt`, origin preflight | Provider-neutral required-check ledger, immutable artifacts and an independently authorized publisher with manual gates. |
| Dependabot proposals | `.github/dependabot.yml`; OVH currently only inventories/scans | Reviewable update proposals independent of GitHub PR APIs; no automatic source writeback or credentials in containers. |

## Ordered delivery

### 0. Integrate the reviewed foundation

LAB-437's operational change is verified. On 2026-10-31 the user authorized
commit, native feature-branch push, review and merge of its tooling, instructions,
evidence and this plan. Verify the actual integration result in the existing
[LAB-437 Trail](https://entire.com/et/marang/-/trails/4), not from intent status
alone. This authorization does not publish a new module, release or service.
Future implementation slices use their own ticket/checkout and reviewed base.

### 1. Establish trustworthy source, review and provenance

Work on [LAB-438](https://linear.app/riotbox/issue/LAB-438/define-github-free-evidence-provenance-and-exact-sha-review-gates).

Define a closed admission for an immutable, previously published candidate SHA
in the one configured native repository, plus its exact current base. Do not
add arbitrary repositories, refs, shell commands or inline DAGs to the remote
API. Host-side source acquisition remains separate from credential-free tests.
A successful receipt binds source/tree, candidate/base, actual Dagu identity,
attempt, pipeline/image/profile and required test/log digests.

Before promotion, reject stale bases, changed heads, skipped/missing/duplicate
lanes, artifact tampering and old attempts. Preserve compatibility with
historical evidence instead of relabelling it. Use Native Entire Trails for
review, with reciprocal Linear links and an automation precheck before writes.
Intent-only Trails do not prove branch review or approval. Qualify exact-head
approvals, Dagu check integration, base/head changes and native merge rejection;
inspect `trail merge --dry-run` before any separately authorized merge. If a
required gate cannot be enforced, retain an explicit blocker: a manual receipt
is not equivalent to server-enforced rejection. Do not bypass gates with force
or silently replace pre-merge checks with post-main polling.

### 2. Decide the new module identity and prove public consumption locally

Work on [LAB-439](https://linear.app/riotbox/issue/LAB-439/decide-the-new-go-module-identity-and-prove-credential-free-release); this can start alongside gate design.

Candidate only: `izoone.com/robotgo`, with `/v2` if a v2 policy is selected.
The exact path, versioning, DNS/HTTPS route, publisher and checksum/residency
policy remain decisions—not configured defaults. Review all self-imports,
examples, API baselines, evidence package names and worker module guards.
Prove only the intended identity delta across frozen variants; do not bulk
rebaseline away unrelated API changes or fold P013's backend/API work into this
delivery migration.

Prefer an isolated static-module-proxy prototype backed only by approved
release bundles, discoverable through Go's standard vanity mechanism. It
exports no Git object store, credentials, worker state, sessions or captures.
Use standard module ZIP and checksum validation; never relabel an old release
under the new identity. A fresh consumer must resolve/build without an Entire
account/helper, GitHub token, first-party GitHub fallback or hidden client
credentials. Validate missing versions, malformed bundles, checksum mismatch,
immutable-version collisions and anonymous write refusal.

Do not deploy an endpoint or publish a version until its ownership, public
exposure, integrity and metadata residency policy is approved. Do not silently
disable checksum verification or declare public proxy/checksum services EU-safe.

### 3. Complete current-worker Linux parity

Work on [LAB-440](https://linear.app/riotbox/issue/LAB-440/qualify-real-weston-and-headless-sway-evidence-on-the-capped-ovh), after the provenance contract.

Add reviewed image dependencies and fixed entrypoints for real Weston and six
Sway cells. Keep exact expected-test manifests and induced-failure, timeout and
cancellation cleanup. No fake GitHub workflow identity, physical desktop mount,
device grant, privileged container or hidden cap increase. A local/YAML-only
result is not qualification: require actual capped OVH Dagu success before
activating schedules or retiring equivalent old checks.

### 4. Resolve native and device capacity

Work on [LAB-441](https://linear.app/riotbox/issue/LAB-441/resolve-native-macoswindows-and-kvmdrm-capacity-for-the-github-exit) as a bounded decision/inventory task.

Identify only existing, user-approved native macOS/Windows capacity and
KVM/DRM resources in allowed locations. Record real architecture, isolation,
permissions, source transfer, credential boundaries and cleanup. Do not wake
the notebook fallback, buy hardware/cloud runners, issue worker credentials or
probe unrelated devices. Smaller guest/resource profiles require independent
equivalent evidence; they are not an automatic substitution.

If capacity or required authorization is unavailable, it remains an explicit
cutover blocker. Keep original gates while that is unresolved.

### 5. Replace release publication and dependency proposals

Create implementation tickets after the preceding contracts are settled; do
not prematurely decompose unknown publisher/runner decisions into a backlog.

Replace GitHub's exact-commit check queries, artifact transfer, evidence URLs,
authoritative-tag/time checks and release uploads with reviewed equivalents.
Preserve the current required-check/snapshot floor and explicit support gaps.
Authenticate approvals and artifact origins; test missing/duplicate/stale
proof, wrong-source/wrong-provider data, corrupt files, races, retries and
existing-release collisions. Publishing is a separately approved atomic,
immutable operation, not an ordinary test-container privilege.

Keep source/update credentials on the trusted host. Produce bounded reviewable
dependency patches and proofs; do not grant a container access to Git or release
write credentials, and do not automatically commit/push generated proposals.
Once GitHub Actions are retired, their own updater lane is historical rather
than an unnecessary new operational dependency.

### 6. Rehearse and cut over last

Before disabling any original workflow or changing GitHub's operational role:

- Run fresh exact-source review, all retained native/compositor/platform lanes
  and the complete new release-consumer proof on the new infrastructure.
- Exercise missing evidence, failed approvals, stale heads, tampering, partial
  publication and rollback/retry rejection without weakening the manual gate.
- Observe cold-cache first-party workflow and consumer traffic; deny unexpected
  GitHub fallback, and state third-party/tool/registry residency limits precisely.
- Obtain explicit approval for source publication, the public module route,
  release/tag publication and final cutover. Never infer it from this plan.
- Preserve old repository/ref history, old module consumers and immutable old
  evidence. Keep historical attribution distinct from active dependencies.
- Update install/docs/workflow pointers and record the verified rollback
  boundary. Do not delete original repositories or cancel services.

## Completion criteria

P015 is complete only when the new canonical module consumer and the whole
retained review/test/update/release path work without first-party GitHub
services, with real native evidence and approved publication. A new origin,
green Linux suite, cross-compile, plan, ticket or provider-labelled JSON alone
does not establish that result. Until then, the four near-next tickets and
this project's status remain honest about the remaining work.
