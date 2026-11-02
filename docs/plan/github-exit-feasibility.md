# GitHub exit: module distribution feasibility

Research date: 2026-10-30; Trail policy updated 2026-10-31.
Planning evidence, not deployment approval.
The user chose a complete first-party GitHub exit, including the module address.
This note supports the overall exit plan; it does not authorize execution.

## Verified capabilities and identity boundary

Current source and the preserved published `v1.0.3` commit declare
`module github.com/marang/robotgo`. The published tag object and commit remain
recorded in the [source evidence](../evidence/2026-10-31-entire-ovh/source-verification.json).
Local inspection also found old-path imports throughout the source: changing
only `go.mod` would not complete migration.

Go identifies modules and package imports by their declared paths. A vanity
endpoint can answer `?go-get=1` with a `go-import` tag whose VCS field is `mod`,
directing clients to an HTTPS module proxy without exposing the development
repository. The proxy protocol permits static files: version lists, `.info`,
original `.mod`, and correctly formatted `.zip` files. Successful versioned
`.mod`/`.zip` responses must remain immutable. [Go module reference](https://go.dev/ref/mod#serving-from-proxy),
[proxy protocol](https://go.dev/ref/mod#goproxy-protocol).

This cannot transparently move the existing GitHub identity. Go's direct
resolver maps `github.com/owner/repo` to GitHub before vanity discovery.
An administrator-controlled domain cannot override that mapping for arbitrary
consumers. [Go resolver source](https://go.dev/src/cmd/go/internal/vcs/vcs.go).
Go also checks a downloaded module's declared path against the requested path;
relabeling the existing `v1.0.3` archive as a new module is not a migration.
[Module-loading source](https://go.dev/src/cmd/go/internal/modload/modfile.go).

The installed CLI 0.12.2 was also inspected through `entire repo edit --help`:
public visibility explicitly grants pull access to authenticated Entire users.
Anonymous native Git is therefore not established by the public flag, and a
401 alone is not a configuration fault. `entire repo protection add --help`
documents force/deletion protection and server-side-merge-only mode; no external
required-check option is exposed by that inspected command. These are local CLI
observations, not proof that every internal API capability was enumerated.

Entire's current official README distinguishes native repositories from GitHub
mirrors, documents the packaged Git helper, and routes its review workflow
through Trails. It does not establish account-free native Git reads, native
release assets, or an external required-check gate. Those are open capabilities
to probe, not guarantees. The current central policy permits Native Entire
Trails for proposals and reviews. It does not authorize cloud-agent execution,
telemetry, provider changes or session/transcript/screenshot/checkpoint uploads.
Before Trail writes, check live automations and reuse existing intent; use
`--no-branch` when source publication is unauthorized. Preserve permitted local
capture with uploads disabled. Installed merge help exposes approval/CI gates
and `--dry-run`; actual Dagu check integration still needs qualification.
[Entire CLI README](https://github.com/entireio/cli/blob/main/README.md#repositories--git-hosting).
[Current central policy](https://automation.izoone.com/agents/worker-accounts.md#source-hosting-and-review).

## Proposed least-privilege options

Candidate only: `izoone.com/robotgo`, with a separately reviewed new-path release.
Neither this name nor any proxy hostname, DNS change, public route, version or
release is approved/provisioned. Rewrite self-imports and examples, test consumer
upgrades, and decide versioning explicitly; `/v2` would be required for a v2
module. [Go major-version guidance](https://go.dev/doc/modules/major-version).

Preferred feasibility option: a credential-free, read-only static release
distribution tree, discoverable through `vcs=mod`. A separately authorized
host-side publisher would fetch only approved tagged source from native Entire,
validate module archives, and prepare an immutable allowlisted bundle. Public
serving would receive no Git object store, credential store, worker state or
publication authority. This is an inference from Go's protocol, not an existing
service. Module ZIP creation/checking has first-party helpers; a normal Git
archive is not sufficient evidence. [Module ZIP tooling](https://pkg.go.dev/golang.org/x/mod/zip).
Tagged module distribution alone does not provide anonymous Git cloning,
source browsing, review gates, release binaries or evidence-asset hosting.

Preserve every previously published Git hash, annotated tag and old module
identity. An optional legacy proxy namespace could retain the original old-path
release contents/checksums for consumers who explicitly configure `GOPROXY`;
that is not transparent discovery or a reason to delete the original repository.
Previously cached third-party copies are not a completeness/availability
guarantee. A new-path release requires new source and explicit later publication
authorization, never retagging old releases.

## Unknowns and acceptance probes

First-party independence means RobotGo development, gates and new releases no
longer require GitHub. It does not mean zero GitHub-hosted upstream dependencies:
`go.mod` still names many. Audit all platform/tag/tool/image dependency closure
and observed cold-cache egress before making that stronger claim. Go's default
proxy/checksum services are separate external processing boundaries; resolve
their residency policy before new publication, without silently disabling
verification. [Go download/privacy reference](https://go.dev/ref/mod#private-module-privacy).

Before implementation approval:

1. Confirm canonical path/version policy and prove old/new consumer fixtures
   coexist without duplicate package-identity mistakes.
2. Exercise a local static fixture: list, explicit version, `.info`, `.mod`,
   ZIP validation, missing versions and checksum failure.
   `go mod download -json` reports archive paths and sums for comparison.
   [Go command reference](https://go.dev/cmd/go/#hdr-Download_modules_to_local_cache).
3. After separate hosting approval, test from an empty credential-free client
   with no Entire helper/account; prove no GitHub fallback, public write API,
   session material or unexpected redirect. Retain old-release checksum evidence.
4. Independently qualify anonymous Git access, pre-merge enforcement, platform
   CI, dependency-update proposals and release/evidence publication replacements.

Native Entire stays in Frankfurt/EU; the existing OVH London rootless worker
retains 2 CPUs/8 GiB/256 PIDs and host-only auth. Notebook fallback stays stopped;
DreamHost retains email/domains/DNS. This research adds no service, US processing,
purchase, mirror, container credential, automatic push, commit or live change.
