# Stable Release Readiness

Status: Active

Linear project:
[RobotGo | P006 | Stable Release Readiness](https://linear.app/riotbox/project/robotgo-or-p006-or-stable-release-readiness-a70dfd544b5b)

Decision issue:
[LAB-64](https://linear.app/riotbox/issue/LAB-64/define-the-v11-stable-release-line-and-readiness-gates)

## Release-line decision

The independent `github.com/marang/robotgo` module will use:

1. `v1.1.0-rc.1` for the first stable-line release candidate.
2. `v1.1.0` for the first stable release after qualification.

The repository already contains immutable upstream tags `v1.0.0`, `v1.0.1`,
and `v1.0.2`. Those commits declare `module github.com/go-vgo/robotgo`; they
are preserved for history and must not be deleted, moved, or reused. A fork
`v1.0.0` tag is therefore neither technically available nor an acceptable
release operation. `v1.1.0` is the next unused minor release and accurately
represents the fork's large additive feature set.

The package `Version` remains `v1.0.0-beta.2` until the release-candidate
preparation issue changes code, tests, notes, tag, and evidence together.

## Current evidence

- `go list -m -json github.com/marang/robotgo@latest` resolved
  `v1.0.0-beta.2` on 2026-07-27. Historical stable tags are not valid versions
  of this module because their `go.mod` files declare the upstream path.
- Exact merged-main
  [`Release Evidence` run 30272753885](https://github.com/marang/robotgo/actions/runs/30272753885)
  passed six native/Pure-Go platform snapshots and the 28-check manifest on
  commit `a641236b1b8f8bd80d4fbffc526a10aa5862b001`.
- P005 is complete with all five milestones at 100%.
- `go doc` currently exposes 259 root declarations, 44 `agent` declarations,
  and 12 `input/portal` declarations. That breadth makes an automated API
  freeze a stable-release requirement.

## Codebase review findings

### Blocking

1. **Version namespace and install guidance**
   - Location: `docs/releases/v1.0.0-beta.2.md:10`,
     `docs/releases/v1.0.0-beta.2.md:22`
   - Category: release architecture
   - Severity: major
   - The active plan names an unavailable final `v1.0.0`, and the beta.2 notes
     incorrectly say `@latest` prefers the historical stable tags.
   - Action: adopt the v1.1 line and correct the observed module-resolution
     behavior in LAB-64.

2. **No stable public-API compatibility gate**
   - Location: `.github/workflows/go.yml:17`, `robotgo.go:93`
   - Category: architecture and maintainability
   - Severity: major
   - Vet, race, sanitizer, platform, and runtime gates are strong, but none
     rejects an incompatible exported removal or signature change. The large
     root compatibility surface makes manual review insufficient after stable.
   - Action: [LAB-65](https://linear.app/riotbox/issue/LAB-65/add-a-stable-public-go-api-compatibility-gate)
     creates a checked-in, platform-aware baseline and blocking CI check.

3. **Pure-Go macOS support claim mixes pass and pending**
   - Location: `docs/compatibility/runtime-v1.md:6`,
     `docs/compatibility/runtime-v1.md:24`
   - Category: compatibility contract
   - Severity: major
   - The matrix defines `supported` as blocking evidence, but the Pure-Go
     macOS row says both supported and permission-granted evidence pending.
   - Action: [LAB-66](https://linear.app/riotbox/issue/LAB-66/resolve-stable-platform-support-claims-and-macos-evidence-scope)
     must add suitable remote evidence or classify the unevidenced mutations as
     implemented/evidence-pending before the RC.

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
- Permission-granted Pure-Go macOS mutations if LAB-66 truthfully marks them
  evidence-pending instead of supported.
- New agent transports: `robotgo-mcp` remains a local stdio adapter, not a
  network or multi-tenant security boundary.

## Blocking delivery gates

| Gate | Requirement | Owner | P006 milestone |
|---|---|---|---|
| G1 Version line | v1.1 decision and truthful `@latest` guidance merged | LAB-64 | M1 Contract and API Freeze |
| G2 API freeze | Checked-in public API baseline plus blocking compatibility CI | [LAB-65](https://linear.app/riotbox/issue/LAB-65/add-a-stable-public-go-api-compatibility-gate) | M1 Contract and API Freeze |
| G3 Platform claims | Every supported row backed by blocking/approved evidence; pending rows explicit | [LAB-66](https://linear.app/riotbox/issue/LAB-66/resolve-stable-platform-support-claims-and-macos-evidence-scope) | M1 Contract and API Freeze |
| G4 Release candidate | Clean `v1.1.0-rc.1` tag, exact evidence, notes, migration, checksums | [LAB-67](https://linear.app/riotbox/issue/LAB-67/prepare-and-publish-robotgo-v110-rc1) | M2 v1.1.0 Release Candidate |
| G5 Stable qualification | At least seven calendar days, no unresolved critical/high regression, no API drift, final exact evidence | [LAB-68](https://linear.app/riotbox/issue/LAB-68/qualify-and-publish-robotgo-v110-stable) | M3 v1.1.0 Stable Qualification |

## RC and stable rules

`v1.1.0-rc.1` is no-go when any API, supported-platform, exact-evidence,
cleanup, security, or documentation gate is missing, skipped unexpectedly, or
stale. The RC tag and GitHub prerelease must identify the same commit and
checksummed evidence bundle.

`v1.1.0` is no-go until the RC has completed at least seven calendar days of
qualification with no unresolved critical/high regression. The final stable
commit may contain only qualification fixes and release metadata relative to
the approved RC API baseline. Final evidence is rerun on the stable tag commit,
and `go list -m github.com/marang/robotgo@latest` must resolve `v1.1.0`.
