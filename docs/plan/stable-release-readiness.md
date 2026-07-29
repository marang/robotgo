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

Before RC publication, the authoritative `marang/robotgo` origin contains only
`v1.0.0-beta.1` and `v1.0.0-beta.2`. Development clones can also contain
`v1.0.0`, `v1.0.1`, and `v1.0.2` tags fetched from the separate
`go-vgo/robotgo` upstream remote. Local tag names are therefore not evidence
of origin state. Release preflight must use `git ls-remote --tags origin` and
the GitHub repository API, and must never push an upstream-derived local tag.

The release-candidate preparation branch changes package `Version`, tests,
notes, tag, and evidence together to `v1.0.0-rc.1`.

## Current evidence

- `git ls-remote --tags origin`, the GitHub tag-ref API, and the GitHub release
  API showed neither `v1.0.0-rc.1` nor `v1.0.0` on 2026-07-29. The similarly
  named stable refs in the development clone came from `upstream`, not
  `origin`. `scripts/preflight-origin-release.sh` now reproduces this check,
  binds the selected commit to authoritative `origin/main`, and rejects a
  non-fork remote.
- `go list -m -json github.com/marang/robotgo@latest` resolved
  `v1.0.0-beta.2` with both the default proxy and `GOPROXY=direct` on
  2026-07-27. Explicit versions remain required for reproducible installs and
  custom/caching proxies can lag.
- Exact merged-main
  [`Release Evidence` run 30434061380](https://github.com/marang/robotgo/actions/runs/30434061380)
  passed six native/Pure-Go platform snapshots and the 29-check manifest on
  commit `cd204c663e4ff5c8d33504d8cbf8b4dce1d8cc59`, including all promoted
  GNOME/KDE portal/bounds lanes and Hyprland.
- P005 is complete with all five milestones at 100%.
- `go doc` currently exposes 259 root declarations, 44 `agent` declarations,
  and 12 `input/portal` declarations. That breadth makes an automated API
  freeze a stable-release requirement.

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
     and is not an RC blocker.

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
- New agent transports: `robotgo-mcp` remains a local stdio adapter, not a
  network or multi-tenant security boundary.

## Blocking delivery gates

| Gate | Requirement | Owner | Status | P009 milestone |
|---|---|---|---|---|
| G1 Version line | Authoritative origin preflight, v1.0 decision, and truthful `@latest` guidance | LAB-64 | Complete — LAB-64 | M1 Contract and API Freeze |
| G2 API freeze | Checked-in public API baseline plus blocking compatibility CI | [LAB-65](https://linear.app/riotbox/issue/LAB-65/add-a-stable-public-go-api-compatibility-gate) | Complete — 14 variants and exact 29-check evidence | M1 Contract and API Freeze |
| G3 Platform claims | Every supported row backed by blocking/approved evidence; pending rows explicit | [LAB-66](https://linear.app/riotbox/issue/LAB-66/resolve-stable-platform-support-claims-and-macos-evidence-scope) | Complete — checked runtime-v1 contract; macOS permission scope pending under LAB-69 | M1 Contract and API Freeze |
| G4 Release candidate | Clean origin `v1.0.0-rc.1` tag, exact evidence, notes, migration, checksums | [LAB-67](https://linear.app/riotbox/issue/LAB-67/prepare-and-publish-robotgo-v100-rc1) | In progress — origin preflight passed; preparing tag commit | M2 v1.0.0 Release Candidate |
| G5 Stable qualification | At least seven calendar days, no unresolved critical/high regression, no API drift, final exact evidence | [LAB-68](https://linear.app/riotbox/issue/LAB-68/qualify-and-publish-robotgo-v100-stable) | Blocked by G4 | M3 v1.0.0 Stable Qualification |

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
