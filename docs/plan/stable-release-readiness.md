# Stable Release Readiness

Status: Active

Linear project:
[RobotGo | P009 | Stable Release Readiness](https://linear.app/riotbox/project/robotgo-or-p009-or-stable-release-readiness-a70dfd544b5b)

Decision issue:
[LAB-64](https://linear.app/riotbox/issue/LAB-64/define-the-v10-stable-release-line-and-readiness-gates)

## Release-line decision

The independent `github.com/marang/robotgo` module will use:

1. `v1.0.0-rc.1` for the first stable-line release candidate.
2. `v1.0.0-rc.2` to re-freeze the expanded post-rc.1 public contract.
3. `v1.0.0` for the first stable release after fresh rc.2 qualification.

The authoritative `marang/robotgo` origin now contains the published,
annotated `v1.0.0-rc.2` and `v1.0.0-rc.1` tags in addition to `v1.0.0-beta.1`
and `v1.0.0-beta.2`; `v1.0.0` remains unused. Development clones can also contain
`v1.0.0`, `v1.0.1`, and `v1.0.2` tags fetched from the separate
`go-vgo/robotgo` upstream remote. Local tag names are therefore not evidence
of origin state. Release preflight must use `git ls-remote --tags origin` and
the GitHub repository API, and must never push an upstream-derived local tag.

The first release candidate changed package `Version`, tests, notes, tag, and
evidence together to `v1.0.0-rc.1`. Subsequent feature work intentionally
expanded and changed the checked public contract, including exported agent
schemas and root capability structures. The original qualification window can
therefore no longer authorize stable publication. LAB-228 published
`v1.0.0-rc.2` at `2026-10-03T08:41:23Z`; its fresh seven-day qualification
window ends no earlier than `2026-10-10T08:41:23Z`.

## Current evidence

- Published `v1.0.0-rc.2` peels to
  `60aa3a44522492a341e6d5b55df7a2fa99ef57b1`, tree
  `873854b0d18d4b780a8fbbbfe835bc25fbadebdd`.
  [Exact-tag Release Evidence run 37110542971](https://github.com/marang/robotgo/actions/runs/37110542971)
  passed all 17 jobs, six snapshots, and the 29-check manifest. Both public
  assets were downloaded and independently verified against the exact
  tag/commit/tree/run; archive SHA-256 is
  `e416ca7a980e19f3b25642fef33c6e964453384d366c5f628d4ccb6923a8f2b6`.
  Default proxy and `GOPROXY=direct` both resolve `@latest` to rc.2.
- `scripts/preflight-origin-release.sh` proved that neither `v1.0.0-rc.1` nor
  `v1.0.0` existed in the fork before publication, bound the selected commit to
  authoritative `origin/main`, and rejected a non-fork remote. The resulting
  annotated RC tag peels to
  `281d8cee29d696e334fe9d4a6f6a7069ab291083`; `v1.0.0` remains absent.
- The preflight rejects stable publication before `2026-10-10T08:41:23Z`
  (epoch `1791621683`), exactly seven days after GitHub published rc.2.
  Deterministic tests cover before/at/after the boundary, GNU/BSD parsing,
  missing/duplicate/invalid time, and clock lookup/parsing failures. RC
  preflight remains independent of the stable time gate and rejects existing
  tags/releases.
- `go list -m -json github.com/marang/robotgo@latest` resolved
  `v1.0.0-rc.1` with the default proxy and `GOPROXY=direct` on 2026-07-29.
  `proxy.golang.org` identifies the exact tag commit and `sum.golang.org`
  publishes both module and `go.mod` hashes. Explicit versions remain required
  for reproducible installs and custom/caching proxies can lag.
- Exact published-tag
  [`Release Evidence` run 30442843617](https://github.com/marang/robotgo/actions/runs/30442843617)
  passed six native/Pure-Go platform snapshots and the 29-check manifest on
  commit `281d8cee29d696e334fe9d4a6f6a7069ab291083`, including all promoted
  GNOME/KDE portal/bounds lanes and Hyprland. Its two public assets have the
  independently verified archive SHA-256
  `7761b673a8f6a8de8e36e74232149a24491fe8ef87dabd8023a665f313f31738`.
- [LAB-78](https://linear.app/riotbox/issue/LAB-78/make-published-release-evidence-assets-fail-closed-on-collisions)
  makes the release write boundary fail closed on an existing asset name. The
  workflow serializes same-tag publishers, checks both exact asset names before
  either upload, and no longer permits `gh release upload --clobber`, so a rerun
  cannot mix assets from different runs, delete, or replace published evidence
  when repository-level immutable releases are unavailable. The non-transactional
  two-file upload can still leave an incomplete same-run pair after a transport
  failure; later reruns fail closed rather than repairing it implicitly.
- P005 is complete with all five milestones at 100%.
- Before the rc.2 version-metadata change, the checked Linux CGO manifest had
  511 additions and 13 removals or replacements after `rc.1`. The final rc.2
  candidate adds the public `Version` transition itself, for 512 additions and
  14 removals or replacements. That breadth makes a fresh reviewed candidate
  and automated API freeze a stable-release requirement.

## Stable qualification log

| Observed at (UTC) | Observation | Result |
|---|---|---|
| 2026-07-29T10:13:46Z | GitHub published the immutable annotated `v1.0.0-rc.1` prerelease | Qualification window opened |
| 2026-07-29T10:30:36Z | Exact tag run `30442843617` completed; 17 workflow jobs, six snapshots, and all 29 required checks succeeded | Pass |
| 2026-07-29T10:31Z | Public archive/checksum, six manifests, tag ref, tree, commit, and release run were independently streamed and verified | Pass |
| 2026-07-29T10:32Z | Default proxy, `proxy.golang.org`, `GOPROXY=direct`, and `sum.golang.org` resolved the RC | Pass |
| 2026-07-29T10:34Z | Linear RobotGo audit found no unresolved critical/high defect; LAB-69 remains a medium, explicitly unsupported-scope evidence task | Pass |
| 2026-07-29T12:38Z | LAB-69 was classified externally blocked with four explicit reactivation paths; permission-granted macOS scopes remain evidence-pending and non-blocking for stable | Pass |
| 2026-07-29T15:35Z | Stable preflight rejected an actual early `v1.0.0` attempt; deterministic before/at/after-boundary and clock-failure tests passed | Pass |
| 2026-10-03T07:33Z | Qualification audit found 511 added and 13 removed/replaced manifest lines after `rc.1`, including exported APIs, public fields, and schema values | Stable no-go; LAB-228 opened for `rc.2` |
| 2026-10-03T08:41:23Z | GitHub published annotated `v1.0.0-rc.2` on `60aa3a44522492a341e6d5b55df7a2fa99ef57b1` after manual evidence run `37109789051` passed | Fresh qualification window opened; stable not before `2026-10-10T08:41:23Z` |
| 2026-10-03T08:55Z | Exact-tag run `37110542971`, public archive/checksum, all six snapshots, 29 successful checks, and default/direct module resolution independently verified | Pass — LAB-228 complete |

GitHub Issues are disabled for this repository, so qualification findings are
triaged in the Linear RobotGo project. LAB-68 stays open through the full
window. A later observation cannot shorten the minimum duration, and any
critical/high regression resets the release decision to no-go until resolved
and requalified.

## Codebase review findings

### Resolved in LAB-228

1. **Post-rc.1 public contract invalidated the original stable window**
   - Location: `api/compat/linux-cgo.api`, `docs/CHANGELOG.md`
   - Category: release correctness
   - Severity: release-blocking
   - Before rc.2 metadata, the reviewed manifest had 511 added and 13 removed
     or replaced lines since `rc.1`, including public fields and schema
     constants. The rc.2 `Version` transition makes the final candidate 512/14.
     These changes may be intentional and source-compatible, but they are not
     the immutable contract that completed the original RC evidence run.
   - Resolution: `rc.2` publishes the reviewed contract on
     `60aa3a44522492a341e6d5b55df7a2fa99ef57b1` with fresh evidence and a new
     seven-day window. Stable remains gated by LAB-68.

### Resolved in LAB-64

1. **Origin tag namespace and install guidance**
   - Location: `docs/releases/v1.0.0-beta.2.md:10`,
     `docs/releases/v1.0.0-beta.2.md:22`
   - Category: release architecture
   - Severity: resolved major
   - Local upstream-derived stable tags were incorrectly treated as tags in
     the fork origin, making an available `v1.0.0` look unavailable.
   - Resolution: use authoritative origin refs, retain the standard v1.0
     beta/RC/stable sequence, document both proxy observations, and require an
     origin-tag preflight in LAB-67 and LAB-68.

### Resolved in LAB-65 and LAB-66

1. **Stable public-API compatibility gate**
   - Location: `.github/workflows/go.yml:17`, `robotgo.go:93`
   - Category: architecture and maintainability
   - Severity: resolved major
   - [LAB-65](https://linear.app/riotbox/issue/LAB-65/add-a-stable-public-go-api-compatibility-gate)
     added deterministic package discovery, checked platform/tag baselines, and
     the protected `api-compat` check. Exact merged-main release evidence is
     linked above.

2. **macOS permission-dependent claims mixed pass and pending**
   - Location: `docs/compatibility/runtime-v1.md`,
     `docs/compatibility/runtime-v1.json`
   - Category: compatibility contract
   - Severity: resolved major
   - [LAB-66](https://linear.app/riotbox/issue/LAB-66/resolve-stable-platform-support-claims-and-macos-evidence-scope)
     split both native and Pure-Go macOS into consent-free supported scopes and
     permission-granted `implemented / evidence pending` scopes. A checked
     machine-readable contract rejects mixed states and maps every supported
     row to exact release checks.
   - Permission-granted promotion is isolated in
     [LAB-69](https://linear.app/riotbox/issue/LAB-69/add-permission-granted-self-owned-macos-runtime-evidence)
     and is not an RC or stable-release blocker. LAB-69 is externally blocked
     because RobotGo has no dedicated macOS runtime, renting one is not an
     option, and hosted runners cannot currently provide repeatable
     permission-granted Screen Recording and Accessibility evidence.

### Ready strengths

1. **Release publication has a narrow write boundary**
   - Location: `.github/workflows/release-evidence.yml:454`
   - Category: security and dependency boundary
   - Severity: ready
   - The write-authorized publish job does not check out or execute repository
     code; it verifies and uploads the already packaged checksum-bound bundle.

2. **Platform backends fail explicitly and remain isolated**
   - Location: `docs/plan/product-roadmap.md:36`,
     `docs/plan/product-roadmap.md:381`
   - Category: design consistency and cross-module coupling
   - Severity: ready
   - Native/Pure-Go and compositor boundaries are explicit, real runtimes are
     evidenced, and unavailable Wayland semantics return supported errors
     instead of fabricated state.

3. **Resource, privacy, and performance contracts are release-grade**
   - Location: `docs/plan/product-roadmap.md:41`,
     `docs/compatibility/release-evidence-v1.md:29`
   - Category: security and performance
   - Severity: ready
   - Sanitizers, bounded waits, deterministic cleanup, exact-source snapshots,
     and disposable real-desktop evidence are blocking. The X11 native/Pure-Go
     benchmark decision is documented; no default backend switch is pending.

### Explicitly non-blocking

- Universal foreign-window operations on Wayland core. Stable scope is the
  capability-gated behavior in the compatibility matrix, not invented parity.
- Additional Pure-Go backends beyond the supported matrix.
- Permission-granted native and Pure-Go macOS capture/input/window operations,
  which remain explicitly evidence-pending under LAB-69 rather than supported.
  Resume promotion only when a dedicated project test Mac, a trusted isolated
  maintainer/community fixture, donated or sponsored isolated capacity, or
  official reproducible permission-granted GitHub-hosted macOS support becomes
  available.
- New agent transports: `robotgo-mcp` remains a local stdio adapter, not a
  network or multi-tenant security boundary.

## Blocking delivery gates

| Gate | Requirement | Owner | Status | P009 milestone |
|---|---|---|---|---|
| G1 Version line | Authoritative origin preflight, v1.0 decision, and truthful `@latest` guidance | LAB-64 | Complete — LAB-64 | M1 Contract and API Freeze |
| G2 API freeze | Checked-in public API baseline plus blocking compatibility CI | [LAB-65](https://linear.app/riotbox/issue/LAB-65/add-a-stable-public-go-api-compatibility-gate) | Complete — 14 variants and exact 29-check evidence | M1 Contract and API Freeze |
| G3 Platform claims | Every supported row backed by blocking/approved evidence; pending rows explicit | [LAB-66](https://linear.app/riotbox/issue/LAB-66/resolve-stable-platform-support-claims-and-macos-evidence-scope) | Complete — checked runtime-v1 contract; macOS permission scope pending under LAB-69 | M1 Contract and API Freeze |
| G4 Release candidate | Clean origin `v1.0.0-rc.1` tag, exact evidence, notes, migration, checksums | [LAB-67](https://linear.app/riotbox/issue/LAB-67/prepare-and-publish-robotgo-v100-rc1) | Complete — published tag, 29-check exact evidence, checksummed assets, and module resolution verified | M2 v1.0.0 Release Candidate |
| G4b Re-freeze expanded API | Clean origin `v1.0.0-rc.2`, reviewed API contract, exact evidence, notes, checksums, and module resolution | [LAB-228](https://linear.app/riotbox/issue/LAB-228/publish-v100-rc2-after-post-rc-public-api-expansion) | Complete — fresh exact-tag evidence and module resolution recorded above | M3 v1.0.0 Stable Qualification |
| G5 Stable qualification | At least seven calendar days from rc.2, no unresolved critical/high regression, no further API drift, final exact evidence | [LAB-68](https://linear.app/riotbox/issue/LAB-68/qualify-and-publish-robotgo-v100-stable) | Qualification open — stable preflight rejects publication before `2026-10-10T08:41:23Z`; boundary recorded in LAB-235 | M3 v1.0.0 Stable Qualification |

## RC and stable rules

`v1.0.0-rc.2` is no-go when any API, supported-platform, exact-evidence,
cleanup, security, or documentation gate is missing, skipped unexpectedly, or
stale. The RC tag and GitHub prerelease must identify the same commit and
checksummed evidence bundle. Origin preflight must prove that neither the RC
nor stable tag exists there and that the selected tag object targets the exact
fork commit rather than a local upstream-derived ref.

`v1.0.0` is no-go until rc.2 has completed at least seven calendar days of
qualification with no unresolved critical/high regression. The final stable
commit may contain only qualification fixes and release metadata relative to
the approved rc.2 API baseline. Final evidence is rerun on the stable tag commit,
and `go list -m github.com/marang/robotgo@latest` must resolve `v1.0.0` with
both the default proxy and `GOPROXY=direct`.
