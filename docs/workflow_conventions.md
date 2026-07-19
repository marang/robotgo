# RobotGo Workflow Conventions

Version: 1.2
Status: Active  
Audience: contributors, reviewers, and coding agents

## 1. Purpose and ownership

This document is the canonical operational workflow for RobotGo planning,
branches, pull requests, CI, reviews, merges, and branch cleanup.

Documentation ownership is split as follows:

- `AGENTS.md` defines non-negotiable engineering and safety rules.
- `docs/workflow_conventions.md` defines Linear coordination and the operational
  Git and GitHub loop.
- `TEST.md` defines validation commands and runtime prerequisites.
- `docs/plan/product-roadmap.md` and `docs/wayland-tasks.md` define product
  direction and delivery status.

Linear is the durable planning and coordination record. Repository
documentation is the durable technical and behavioral record. Tests, Git
history, pull requests, and CI are the implementation and validation record.
Do not leave important state only in chat or local memory.

## 2. Linear planning and project structure

RobotGo work is coordinated in the shared `Lab` Linear team under key `LAB`.
Because this team also owns non-RobotGo work, every RobotGo issue must belong to
the relevant RobotGo project; team membership alone is not sufficient routing.
Current team and project identifiers are recorded in the relevant repository
plan rather than in runtime configuration.

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
- GitHub pull requests and CI own implementation diff, review discussion, and
  merge evidence.

Keep the records linked and synchronized:

- Link the repository plan from its Linear project and the Linear project from
  the repository plan.
- Link an implementation issue in its branch or pull request and link the pull
  request back to the issue.
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

Normal implementation work follows this loop:

`Linear issue -> In Progress -> current main -> isolated branch/worktree -> implementation and tests -> local review -> commit and push -> draft PR -> In Review -> CI and reviewer feedback -> merge -> Done/project update -> sync main -> branch cleanup`

1. Create or select exactly one Linear issue for an executable implementation
   or documentation slice and assign it to the correct RobotGo project.
2. Move the issue to `In Progress` before creating or reusing its branch.
3. Start from a clean, current local `main`.
4. Create a narrowly named feature, fix, docs, test, or chore branch containing
   the `LAB-*` issue key.
5. Implement one coherent slice and update its tests and lasting documentation.
6. Run the default and area-specific validation required by `AGENTS.md` and
   `TEST.md`.
7. Review the complete branch diff, including generated files, build tags,
   platform boundaries, resource ownership, and sensitive-data cleanup.
8. Commit with an English Conventional Commit message and push the branch.
9. Open a draft PR against `main` with behavior, risk, fallback, validation
   evidence, and the Linear issue link when applicable.
10. Move the issue to `In Review` and add a short update covering the change,
    verification, and bounded follow-up work.
11. Inspect every CI job. Fix failures owned by the branch and push the fixes.
12. Once the branch is locally complete and CI is healthy, mark the PR ready for
    review. A draft PR is not evidence that configured reviewers have reviewed
    it.
13. Inspect all review surfaces, address actionable findings, and repeat CI and
    review inspection after every review-driven push.
14. Merge only when the gate in section 6 is satisfied.
15. Move the issue to `Done` and update the project with the delivered outcome,
    merge evidence, and remaining follow-ups.
16. Sync local `main`, verify the merge, and remove the completed feature branch
    when it is no longer needed.

The only issue-first exception is initial team/project creation and draft plan
shaping before an executable slice exists. This exception must not carry product
implementation. Once a slice is ready to change repository behavior, use an
issue before its implementation branch.

Do not push normal implementation work directly to `main`. Do not mix the next
unrelated product slice into a PR that is already at the review boundary.

### 3.1 Parallel agents and worktrees

When another agent or release branch is active, each agent must use its own Git
worktree and feature branch. Do not edit through a shared dirty worktree.

Before committing or opening a PR:

1. verify the branch started from the intended current `main`
2. inspect other active worktrees and likely overlapping files
3. refresh against current `main` when it advanced
4. re-review conflicts instead of overwriting another agent's changes

Keep parallel PRs separate until their scopes and conflict order are understood.
Never place tokens or other credentials in worktree configuration committed to
the repository.

### 3.2 Branch naming

Preferred patterns are:

- `feature/lab-123-short-slice`
- `fix/lab-123-short-slice`
- `docs/lab-123-short-slice`

Keep one branch aligned with one main issue. Project-bootstrap planning branches
without an issue key are permitted only under the exception above.

## 4. Pull request content

A PR description must state:

- the user-visible or behavioral result
- why the change is needed
- important platform and backend consequences
- explicit fallback or unsupported behavior
- resource, privacy, and compatibility risks
- local validation performed
- intentionally omitted runtime evidence or follow-up work
- the associated Linear issue when one exists

Use English for branch names, commits, PR titles, and PR descriptions. Keep the
description current when later commits materially change the result or risk.

## 5. CI and review polling

CI status and review status are separate. Both must be inspected explicitly.

### 5.1 CI

- Verify authenticated GitHub access before relying on automation.
- Inspect check summaries first; fetch full logs only for failed, cancelled, or
  suspicious jobs.
- Treat a skipped job as acceptable only when its trigger or runner conditions
  intentionally do not apply.
- A local green test run does not replace GitHub Actions, and GitHub Actions do
  not replace relevant local or real-runtime evidence.

### 5.2 GitHub reviews

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

Classify each finding as:

- actionable and in scope: fix it on the PR branch
- valid but out of scope: record a concrete follow-up and explain why it is not
  part of the current PR
- informational, duplicate, stale, or already fixed: verify that classification
  against the current head before dismissing it
- ambiguous or conflicting: resolve the trade-off before changing behavior

After a review-driven push:

1. rerun the affected local checks
2. push the fix
3. wait for required CI to evaluate the new head
4. re-read reviews and unresolved threads
5. verify that earlier approvals or comments still apply to the current head

Never report a PR as fully reviewed merely because its CI is green.

## 6. Merge gate

A PR is ready to merge only when all applicable conditions are true:

- the PR is not a draft
- the branch targets `main` and is mergeable
- required CI checks pass on the current head
- skipped checks are understood and appropriate
- there is no active `CHANGES_REQUESTED` review
- there are no unresolved actionable review threads
- configured or explicitly requested reviewers, including Codex, have had the
  opportunity to review the ready PR, and their result applies to the current
  head
- default and relevant tagged tests pass
- public behavior and test instructions are documented where required
- no test or development run leaves sensitive desktop, clipboard, OCR, input,
  credential, diagnostic, or similar artifacts behind

After merge, verify GitHub reports the PR as merged and local `main` contains the
merge before deleting branches.

## 7. Sensitive test and development data

The cleanup contract in `AGENTS.md` applies throughout this workflow:

- prefer hermetic fixtures and in-memory data
- use `t.TempDir()` and `t.Cleanup()` for unavoidable test files
- clean sensitive artifacts on success, error, timeout, and cancellation
- treat files returned by an external desktop backend as RobotGo's cleanup
  responsibility
- add regression assertions that the artifact no longer exists
- inspect unexpected files before deleting them, then remove confirmed
  RobotGo-created sensitive content and derivative caches

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
- issues in `In Review` only while their PRs are genuinely open
- one to five near-next backlog issues when those slices are already clear

Do not fully decompose distant phases. Projects answer which product/outcome the
work belongs to; labels answer the slice type.

Linear is the active execution surface, not the only long-term archive. Because
the workspace uses the free tier, completed issues may be removed after their
useful context is preserved under
`docs/archive/linear_issues/LAB-123.md`. Do not delete an issue until its PR is
merged, it is `Done`, and the archive entry exists. Archive only durable context
such as purpose, shipped outcome, verification, PR, merge commit, and bounded
follow-ups; never archive sensitive desktop data or credentials.

## 9. Practical closeout checklist

Before declaring a PR complete:

- [ ] Working tree is clean and the intended commits are pushed.
- [ ] PR description matches the current head.
- [ ] PR is ready for review, not draft.
- [ ] CI was checked on the current head.
- [ ] Top-level comments, reviews, and thread-aware inline comments were read.
- [ ] Automated-review reactions were checked.
- [ ] Codex feedback or its clean-review reaction was checked against the
      current head when Codex is configured or requested.
- [ ] All actionable findings were fixed or explicitly tracked.
- [ ] Relevant checks were rerun after the last fix.
- [ ] No sensitive test or development artifacts remain.
- [ ] The Linear issue and project reflect the merged result and follow-ups.
- [ ] Useful issue context was archived before any free-tier cleanup.
- [ ] Merge state and local `main` were verified after merge.
