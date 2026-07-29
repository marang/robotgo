# Stable Release Readiness

Status: Active

Linear project:
[RobotGo | P009 | Stable Release Readiness](https://linear.app/riotbox/project/robotgo-or-p009-or-stable-release-readiness-a70dfd544b5b)

Decision issue:
[LAB-64](https://linear.app/riotbox/issue/LAB-64/define-the-v10-stable-release-line-and-readiness-gates)

## Release-line decision

The independent `github.com/marang/robotgo` module will use:

1. `v1.0.0-rc.1` for the first stable-line release candidate.
2. `v1.0.0` for the first stable release after qualification.

The authoritative `marang/robotgo` origin now contains the published,
annotated `v1.0.0-rc.1` tag in addition to `v1.0.0-beta.1` and
`v1.0.0-beta.2`; `v1.0.0` remains unused. Development clones can also contain
`v1.0.0`, `v1.0.1`, and `v1.0.2` tags fetched from the separate
`go-vgo/robotgo` upstream remote. Local tag names are therefore not evidence
of origin state. Release preflight must use `git ls-remote --tags origin` and
the GitHub repository API, and must never push an upstream-derived local tag.

The release candidate changed package `Version`, tests, notes, tag, and
evidence together to `v1.0.0-rc.1`. Stable qualification started at its
publication time, `2026-07-29T10:13:46Z`; stable publication is no earlier
than `2026-08-05T10:13:46Z`.

## Current evidence

- `scripts/preflight-origin-release.sh` proved that neither `v1.0.0-rc.1` nor
  `v1.0.0` existed in the fork before publication, bound the selected commit to
  authoritative `origin/main`, and rejected a non-fork remote. The resulting
  annotated RC tag peels to
  `281d8cee29d696e334fe9d4a6f6a7069ab291083`; `v1.0.0` remains absent.
- The same preflight now rejects `v1.0.0` before
  `2026-08-05T10:13:46Z` and fails closed when UTC time cannot be obtained or
  parsed from GitHub's authoritative API `Date` header. Deterministic tests
  cover missing/duplicate headers, the second before the boundary, the exact
  boundary, and later execution without applying the stable gate to RC tags.
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
- P005 is complete with all five milestones at 100%.
- `go doc` currently exposes 259 root declarations, 44 `agent` declarations,
  and 12 `input/portal` declarations. That breadth makes an automated API
  freeze a stable-release requirement.

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

GitHub Issues are disabled for this repository, so qualification findings are
triaged in the Linear RobotGo project. LAB-68 stays open through the full
window. A later observation cannot shorten the minimum duration, and any
critical/high regression resets the release decision to no-go until resolved
and requalified.

## Codebase review findings

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
| G5 Stable qualification | At least seven calendar days, no unresolved critical/high regression, no API drift, final exact evidence | [LAB-68](https://linear.app/riotbox/issue/LAB-68/qualify-and-publish-robotgo-v100-stable) | In progress — fail-closed preflight enforces the window opened 2026-07-29T10:13:46Z; earliest stable 2026-08-05T10:13:46Z | M3 v1.0.0 Stable Qualification |

## RC and stable rules

`v1.0.0-rc.1` is no-go when any API, supported-platform, exact-evidence,
cleanup, security, or documentation gate is missing, skipped unexpectedly, or
stale. The RC tag and GitHub prerelease must identify the same commit and
checksummed evidence bundle. Origin preflight must prove that neither the RC
nor stable tag exists there and that the selected tag object targets the exact
fork commit rather than a local upstream-derived ref.

`v1.0.0` is no-go until the RC has completed at least seven calendar days of
qualification with no unresolved critical/high regression. The final stable
commit may contain only qualification fixes and release metadata relative to
the approved RC API baseline. Final evidence is rerun on the stable tag commit,
and `go list -m github.com/marang/robotgo@latest` must resolve `v1.0.0` with
both the default proxy and `GOPROXY=direct`.
