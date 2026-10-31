# RobotGo Workflow Conventions

Version: 1.5
Status: Active  
Audience: contributors, reviewers, and coding agents

## 1. Purpose and ownership

This document is the canonical operational workflow for RobotGo planning,
branches, worktrees, Entire Trails, CI, reviews, merges and cleanup. GitHub PR
procedures below apply only to retained legacy work.

Documentation ownership is split as follows:

- `AGENTS.md` defines non-negotiable engineering and safety rules.
- `docs/workflow_conventions.md` defines Linear coordination and the operational
  native Entire delivery loop and transitional provider gates.
- `TEST.md` defines validation commands and runtime prerequisites.
- `docs/plan/product-roadmap.md` and `docs/wayland-tasks.md` define product
  direction and delivery status.

Linear is the durable planning and coordination record. Repository
documentation is the durable technical and behavioral record. Tests, Git
history, Trails and CI are the implementation and validation record.
Do not leave important state only in chat or local memory.

## 2. Linear planning and project structure

RobotGo work is coordinated in the shared `Lab` Linear team under key `LAB`.
Because this team also owns non-RobotGo work, every RobotGo issue must belong to
the relevant RobotGo project and carry `Codebase` → `RobotGo`; team membership
alone is not sufficient routing. Current team, label-group, label, and project
identifiers are recorded in the relevant repository plan rather than in runtime
configuration.

Name RobotGo projects `RobotGo | PNNN | Outcome` so projects group by codebase
inside the shared team before their sequence and purpose are considered.
Use the mutually exclusive `Codebase` label group for issue routing so a ticket
cannot silently claim multiple codebases.

Use Linear at three levels:

1. A project owns one bounded product or architecture outcome and its delivery
   plan.
2. Project milestones describe meaningful exit boundaries when the project is
   large enough to need them.
3. Issues describe executable, reviewable slices only after their scope and
   acceptance evidence are understood.

Do not create a large speculative issue backlog merely to mirror a draft plan.
It is valid to start with only a team, one project, and one project plan. Split
issues from that plan when a slice is ready to implement.

A project plan should state:

- intended outcome and why it matters
- architectural direction and compatibility boundaries
- explicit non-goals
- ordered delivery slices
- safety, privacy, and platform risks
- validation strategy and exit criteria

Source-of-truth boundaries:

- Linear owns project status, sequencing, ownership, coordination decisions,
  and actionable follow-ups.
- Repository plans and architecture documents own lasting technical decisions,
  public contracts, backend policy, and test strategy.
- Native Entire Trails and CI own implementation diff, review discussion and
  merge evidence. Retained GitHub PRs/checks/assets remain legacy evidence until
  their replacements are verified.

Keep the records linked and synchronized:

- Link the repository plan from its Linear project and the Linear project from
  the repository plan.
- Maintain reciprocal links between the Linear issue and its project-scoped
  Trail; retain links to applicable legacy PRs and checks.
- Reflect lasting architectural or behavioral decisions in repository
  documentation; do not leave them only in a Linear comment.
- Update the issue and project after merge, including deferred evidence or
  concrete follow-ups.
- Do not store Linear tokens, screenshots, clipboard contents, restore tokens,
  credentials, or other sensitive desktop artifacts in the repository or
  Linear.

### 2.1 Plan anchoring

When a project plan becomes an accepted direction:

- keep its technical detail under `docs/plan/`
- link it from `docs/README.md`
- connect it to `docs/plan/product-roadmap.md`
- update `AGENTS.md`, `TEST.md`, or backend documentation only when the plan
  changes their lasting contracts

Do not duplicate the complete plan across multiple repository files. Linear may
carry a coordination copy, while the repository plan remains the durable
technical source; keep their scope and status links synchronized.

## 3. Default workflow

### Native source and review; transitional gates

Native Entire is this migrated repository's canonical source and review
platform. Verify current remotes/project placement: `origin` is native Entire,
`github` retains the previous provider and `upstream` the original fork upstream.
Read the [central source/review policy](https://automation.izoone.com/agents/worker-accounts.md#source-hosting-and-review),
`entire agent-help` and relevant installed command help before Trail writes.

Before Trail writes, check project automations/plugins, scoped agents (through
the final cursor), repository auto-run/runner settings and required processing
locations.
Stop if the write would trigger unrequested execution. Reuse the appropriate
existing Trail and link it reciprocally with Linear. Default `trail create`
can publish a branch: when publication is not authorized, use only an existing
Trail or `trail create --no-branch` for authorized repository-scoped intent.
Intent without a branch is not completed code review or CI evidence.

Trail use does not authorize cloud-agent dispatch, telemetry, model-provider
changes or session/transcript/screenshot/checkpoint publication. Keep these off
unless separately authorized with required residency checks; preserve permitted
local capture and keep session uploads disabled. Native source residency alone
does not establish cloud-agent/model-provider residency.

Native pushes do not trigger GitHub Actions. Retain required legacy platform,
release, module-distribution and existing-PR gates until reviewed equivalents
are verified; new proposals and reviews use native Trails. Cross-provider
publication requires explicit authorization, not automatic mirroring. The
current main-only OVH polling job and force/delete protection are not a
feature-head pre-merge gate. See the [migration record](operations/entire-ovh-migration.md)
and [remaining exit plan](plan/github-exit.md).

Normal implementation work follows this loop:

`Linear issue + Trail -> In Progress -> current main -> isolated branch/worktree -> implementation and tests -> local review -> authorized publication -> exact-head CI and Trail review -> authorized merge -> Done/project update -> sync main -> authorized cleanup`

This loop describes implementation, not a grant to commit, publish, approve,
merge or delete. Stop at the task's authorization boundary; review/planning-only
work may remain local with an intent-only Trail.

1. Create or select exactly one Linear issue for an executable implementation
   or documentation slice, assign it to the correct RobotGo project, and apply
   `Codebase` → `RobotGo`.
2. Move the issue to `In Progress` before creating or reusing its branch.
3. Start from a clean, current local `main`.
4. Create a narrowly named feature, fix, docs, test, or chore branch containing
   the `LAB-*` issue key.
5. Implement one coherent slice and update its tests and lasting documentation.
6. Run the default and area-specific validation required by `AGENTS.md` and
   `TEST.md`.
7. Review the complete branch diff, including generated files, build tags,
   platform boundaries, resource ownership, and sensitive-data cleanup.
8. When authorized, commit with an English Conventional Commit message and
   publish the branch to native Entire.
9. Reuse the project-scoped Trail, or create/link it after the automation
   precheck. Record behavior, risks, fallback, validation and the Linear link.
10. Inspect every required CI job for the actual candidate head. Fix branch-owned
    failures and publish fixes only within the task's authorization.
11. When the branch is ready, request the required authorized Trail reviews.
    An open Trail or intent-only record is not evidence that reviewers reviewed
    the actual code head.
12. Move the issue to `In Review` and add a short update covering the change,
    verification, and bounded follow-up work.
13. Inspect all review surfaces, address actionable findings, and repeat CI and
    review inspection after every review-driven publication.
14. Merge only when authorized and the gate in section 6 is satisfied.
15. Move the issue to `Done` and update the project with the delivered outcome,
    merge evidence, and remaining follow-ups.
16. Sync local `main`, verify the merge, and remove the completed feature
    worktree and branch when they are no longer needed.

The only issue-first exception is initial team/project creation and draft plan
shaping before an executable slice exists. This exception must not carry product
implementation. Once a slice is ready to change repository behavior, use an
issue before its implementation branch.

Do not push normal implementation work directly to `main`. Do not mix the next
unrelated product slice into a Trail change that is already at the review boundary.

### 3.1 Parallel agents and worktrees

When another agent or release branch is active, each agent must use its own Git
worktree and feature branch. Do not edit through a shared dirty worktree.

Before committing or creating/linking branch work on a Trail:

1. verify the branch started from the intended current `main`
2. inspect other active worktrees and likely overlapping files
3. refresh against current `main` when it advanced
4. re-review conflicts instead of overwriting another agent's changes

Keep parallel Trail changes separate until their scopes and conflict order are
understood.
Never place tokens or other credentials in worktree configuration committed to
the repository.

Treat issue worktrees as temporary delivery state. After a Trail change is
merged or a slice is intentionally abandoned:

1. inspect the worktree for modified and untracked files
2. preserve or explicitly account for any work that is not already merged
3. remove the worktree when no active work depends on it
4. prune stale worktree metadata and delete the completed local branch only
   after its merge or disposition is verified

Keep only the canonical `main` worktree and worktrees for genuinely active
work. Never force-remove a dirty worktree merely to complete cleanup.

### 3.2 Branch naming

Preferred patterns are:

- `feature/lab-123-short-slice`
- `fix/lab-123-short-slice`
- `docs/lab-123-short-slice`

Keep one branch aligned with one main issue. Project-bootstrap planning branches
without an issue key are permitted only under the exception above.

## 4. Trail change content

A Trail change description (or a retained legacy PR description) must state:

- the user-visible or behavioral result
- why the change is needed
- important platform and backend consequences
- explicit fallback or unsupported behavior
- resource, privacy, and compatibility risks
- local validation performed
- intentionally omitted runtime evidence or follow-up work
- the associated Linear issue when one exists

Use English for branch names, commits, Trail titles and descriptions. Keep the
description current when later commits materially change the result or risk.

## 5. CI and review polling

CI status and review status are separate. Both must be inspected explicitly.

### 5.1 CI

- Verify authenticated access to the actual CI provider and inspect its exact
  source head/base, pipeline/image and run/attempt identity. Verify GitHub access
  only for retained legacy checks; native publication does not trigger them.
- Inspect compact check counts and active or failed job names first.
- Fetch full logs only for failed, cancelled, suspicious, or explicitly
  evidence-bearing jobs.
- Suppress routine successful command output when the command, exit status,
  exact commit, and durable CI or evidence URL are sufficient. Keep temporary
  full logs only while diagnosing a failure, then remove them.
- Treat a skipped job as acceptable only when its trigger or runner conditions
  intentionally do not apply.
- Local tests, required hosted checks and real-runtime evidence cover different
  gates. Current main-only OVH success cannot qualify an untested feature head.

### 5.2 Native Entire reviews

For the selected repository/branch change, inspect the Trail discussion,
findings (including resolved/dismissed and stale findings where relevant),
approval decisions and configured gates. Use `trail show`, `trail finding`,
`trail approvals` and their current help; project intent may span several
changes, so select the actual repository and branch. Follow pagination through
the final cursor instead of treating the first page as complete.

Review the actual diff and record its candidate head/base. A Trail, an approval
or green provider status alone does not prove current-head review. Verify that
required reviewers and their decisions apply to that head; after changes,
recheck findings, approvals and exact-head CI. If native gate enforcement or a
required external check is unproved/unavailable, report the blocker and retain
the corresponding gate rather than assuming server enforcement.

Classify findings and perform the follow-up loop in section 5.4. Review requests
and decisions remain within the task's authorization; Trail use does not grant
cloud dispatch or approval authority.

### 5.3 Legacy GitHub reviews only

This subsection applies only to existing GitHub PRs and reviews retained during
the transition. It is not an instruction to open a new GitHub PR or to wait for
a GitHub bot on a native Trail.

For every open PR, inspect all of the following:

1. top-level PR conversation comments
2. submitted reviews and their state
3. inline review threads, including `isResolved` and `isOutdated`
4. requested reviewers and the overall review decision
5. PR reactions used by automated reviewers to signal a clean review
6. Codex review output when Codex is configured or requested as a reviewer

Do not rely on a flat comment list: it can omit thread resolution and inline
context. Use a thread-aware GitHub query (`reviewThreads`) or an equivalent
tool.

No Codex comment on a draft PR means only that no comment exists yet. It does
not mean Codex reviewed the change. Mark the PR ready, confirm that the expected
review was requested or triggered, and check again before merge.

In this repository, Codex reviews are submitted by
`chatgpt-codex-connector[bot]`. A review with suggestions is visible as a
submitted review and inline threads; a clean review may be represented only by
the bot's thumbs-up reaction. Confirm that the review names the current head
commit, or that the clean reaction happened after the latest review trigger
with no later push. A result for an older commit is stale; trigger a fresh
review and wait for its result.

### 5.4 Findings and review-driven changes

For native Trails and retained legacy PRs, classify each finding as:

- actionable and in scope: fix it on the reviewed branch
- valid but out of scope: record a concrete follow-up and explain why it is not
  part of the current change
- informational, duplicate, stale, or already fixed: verify that classification
  against the current head before dismissing it
- ambiguous or conflicting: resolve the trade-off before changing behavior

After a review-driven push:

1. rerun the affected local checks
2. publish the fix only when authorized
3. wait for required CI to evaluate the new head
4. re-read reviews and unresolved threads
5. verify that earlier approvals or comments still apply to the current head

Never report a change as fully reviewed merely because its CI is green.

### 5.5 Compact progress and handoffs

Do not repeatedly copy complete green matrices, successful test output, review
boilerplate, or unchanged background into chat, Linear or Trail comments. Report
the outcome, exact head, compact counts, durable URLs, and any exception that
needs attention. Expand the relevant log or context only when a failure,
ambiguity, or audit requires it.

When a long-running task changes agent, session, or day, use this bounded
handoff:

```text
Ticket/project:
Worktree and branch:
Exact head and current main/base:
Trail (or retained legacy PR) and review state:
Verified evidence (commands/results/URLs):
Blockers:
Next action:
Sensitive/transient artifacts and cleanup state:
```

Record facts once and link to durable sources. Do not paste full logs, complete
diffs, old conversation history, or already completed step-by-step narration
into the handoff.

## 6. Merge gate

A native Trail change is ready to merge only when all applicable conditions are true:

- the task authorizes merging and the actual branch change is ready for review
- the branch targets `main` and is mergeable
- required CI checks pass and required approvals apply to the actual current
  head/base
- skipped checks are understood and appropriate
- there is no active blocking review decision
- there are no unresolved actionable review threads
- configured or explicitly requested reviewers, including Codex, have had the
  opportunity to review the ready change, and their result applies to the current
  head
- default and relevant tagged tests pass on the current head after the final
  change
- public behavior and test instructions are documented where required
- no test or development run leaves sensitive desktop, clipboard, OCR, input,
  credential, diagnostic, or similar artifacts behind

Before an authorized native merge, read `trail merge --help`, select the exact
repository/branch change and run `trail merge --dry-run`. Resolve every blocking
gate and recheck the reviewed head/base before merging. Dry-run reports native
configured gates; it does not replace required external evidence. Use no force
bypass without separate explicit authority. Check branch-deletion settings and
retain the branch unless its deletion is authorized.

After merge, verify native Entire's merged result and local `main` contains it
before authorized branch/worktree cleanup. For a retained legacy PR, additionally
require non-draft state and GitHub's applicable checks/reviews, then verify its
GitHub merge result. Never substitute one provider's status for the other's
unverified gate.

## 7. Sensitive test and development data

The mandatory cleanup contract lives in `AGENTS.md` section 8; runtime commands
and prerequisites live in `TEST.md`. Do not duplicate those rules in issue or
Trail/PR prose. Instead, record the privacy impact and concrete cleanup evidence
for the changed path, and block merge when that evidence is missing.

Inspect unexpected files before deleting them. Remove confirmed RobotGo-created
sensitive content and derivative caches, and report the cleanup result without
copying the sensitive content itself.

When a real desktop integration test is necessary, make that opt-in and document
its privacy impact and cleanup behavior in `TEST.md`.

## 8. Linear updates, backlog, and retention

Use two update levels:

- Issue updates record transitions to `In Review`, material changes in
  recommendation, and the merged outcome.
- Project updates record meaningful slices entering review or merging and
  cross-ticket changes to architecture, roadmap, or working mode.

Once implementation starts, keep an honest small horizon:

- one main issue in `In Progress`
- issues in `In Review` only while their Trail changes or retained legacy PRs
  are genuinely under review
- one to five near-next backlog issues when those slices are already clear

Do not fully decompose distant phases. Projects answer which product/outcome the
work belongs to; labels answer the slice type.

Linear is the active execution surface, not the only long-term archive. Because
the workspace uses the free tier, completed issues may be removed after their
useful context is preserved under
`docs/archive/linear_issues/LAB-123.md`. Do not delete an issue until its delivery
change is merged, it is `Done`, and the archive entry exists. Archive only durable
context such as purpose, shipped outcome, verification, Trail/legacy PR, merge
commit and bounded follow-ups; never archive sensitive desktop data or credentials.

## 9. Practical closeout checklist

Before declaring a delivery change complete:

- [ ] Required publication was authorized and verified; the Trail description
      and review state match the actual head. Intent-only/local work is labelled
      unmerged/unpublished, not completed delivery.
- [ ] The CI and review inspections in section 5 apply to the current head, and
      every actionable finding is fixed or explicitly tracked.
- [ ] Relevant local checks were rerun after the final change; the tests,
      documentation, compatibility, and cleanup requirements in `AGENTS.md`,
      `TEST.md`, and the section 6 merge gate are satisfied.
- [ ] The Linear issue and project reflect the merged result and follow-ups.
- [ ] Useful issue context was archived before any free-tier cleanup.
- [ ] Native merge state (or the retained legacy PR's GitHub result) and local
      `main` were verified; completed worktrees and local branches were removed
      after confirming they contain no needed
      uncommitted or unmerged work.
