# RobotGo Workflow Conventions

Version: 1.0  
Status: Active  
Audience: contributors, reviewers, and coding agents

## 1. Purpose and ownership

This document is the canonical operational workflow for RobotGo branches,
pull requests, CI, reviews, merges, and branch cleanup.

Documentation ownership is split as follows:

- `AGENTS.md` defines non-negotiable engineering and safety rules.
- `docs/workflow_conventions.md` defines the operational Git and GitHub loop.
- `TEST.md` defines validation commands and runtime prerequisites.
- `docs/plan/product-roadmap.md` and `docs/wayland-tasks.md` define product
  direction and delivery status.

Repository state, tests, Git history, and GitHub are the durable record. Do not
leave important workflow state only in chat or local memory.

## 2. Default workflow

Normal implementation work follows this loop:

`sync main -> feature branch -> implementation and tests -> local review -> commit and push -> draft PR -> CI -> ready for review -> reviewer feedback -> merge -> sync main -> branch cleanup`

1. Start from a clean, current local `main`.
2. Create a narrowly named feature, fix, docs, test, or chore branch.
3. Implement one coherent slice and update its tests and lasting documentation.
4. Run the default and area-specific validation required by `AGENTS.md` and
   `TEST.md`.
5. Review the complete branch diff, including generated files, build tags,
   platform boundaries, resource ownership, and sensitive-data cleanup.
6. Commit with an English Conventional Commit message and push the branch.
7. Open a draft PR against `main` with behavior, risk, fallback, and validation
   evidence.
8. Inspect every CI job. Fix failures owned by the branch and push the fixes.
9. Once the branch is locally complete and CI is healthy, mark the PR ready for
   review. A draft PR is not evidence that configured reviewers have reviewed
   it.
10. Inspect all review surfaces, address actionable findings, and repeat CI and
    review inspection after every review-driven push.
11. Merge only when the gate in section 5 is satisfied.
12. Sync local `main`, verify the merge, and remove the completed feature branch
    when it is no longer needed.

Do not push normal implementation work directly to `main`. Do not mix the next
unrelated product slice into a PR that is already at the review boundary.

## 3. Pull request content

A PR description must state:

- the user-visible or behavioral result
- why the change is needed
- important platform and backend consequences
- explicit fallback or unsupported behavior
- resource, privacy, and compatibility risks
- local validation performed
- intentionally omitted runtime evidence or follow-up work

Use English for branch names, commits, PR titles, and PR descriptions. Keep the
description current when later commits materially change the result or risk.

## 4. CI and review polling

CI status and review status are separate. Both must be inspected explicitly.

### 4.1 CI

- Verify authenticated GitHub access before relying on automation.
- Inspect check summaries first; fetch full logs only for failed, cancelled, or
  suspicious jobs.
- Treat a skipped job as acceptable only when its trigger or runner conditions
  intentionally do not apply.
- A local green test run does not replace GitHub Actions, and GitHub Actions do
  not replace relevant local or real-runtime evidence.

### 4.2 GitHub reviews

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

## 5. Merge gate

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

## 6. Sensitive test and development data

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

## 7. Practical closeout checklist

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
- [ ] Merge state and local `main` were verified after merge.
