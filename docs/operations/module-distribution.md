# Owned module distribution

The accepted identity is `go.izoone.com/robotgo`, with the existing root layout
and v1 API. The next proposed version is `v1.0.4`; the source version is not a
publication receipt. Historical GitHub module versions, tags, release assets
and dated evidence remain immutable. Historical v2 beta tags do not select a
new `/v2` module or justify moving existing refs. Consumers must change all
owned imports together, including subpackages; mixing old and new paths gives
distinct Go types.

The API compatibility files receive the owned module-prefix substitution and
one explicit `Version` release-metadata value update.
All 14 variants, declarations, JSON fields, signatures and third-party package
identities retain their contracts. The current imgo `v0.44.0` dependency and
LAB-423 image compatibility wrappers/tests stay unchanged.

## Build artifact contract

`internal/cmd/modulezip` prepares three standard library artifacts from an
unprefixed committed Git ZIP. It neither publishes nor qualifies a release.
Use Go 1.26.10 and a clean exact-source checkout. An authorized caller must bind
the reviewed source commit/tree, immutable tag and commit timestamp before
packaging; the command verifies the declared module and source `Version`.
It uses `golang.org/x/mod/zip` for creation and validation, retains original
`go.mod` bytes, refuses an existing output directory and removes partial output.

```sh
commit=$(git rev-parse HEAD)
commit_time=$(git show -s --format=%cI "$commit")
git archive --format=zip "$commit" > "$TMPDIR/robotgo-source.zip"
CGO_ENABLED=0 go run ./internal/cmd/modulezip \
  -version v1.0.4 -time "$commit_time" -out "$TMPDIR/robotgo-module" \
  < "$TMPDIR/robotgo-source.zip"
```

| Artifact | Immutable content |
| --- | --- |
| `go-module.info` | `Version` and exact source commit `Time` in UTC RFC3339 |
| `go-module.mod` | Original committed root `go.mod` |
| `go-module.zip` | Standard ZIP rooted at `go.izoone.com/robotgo@v1.0.4/` |

The protected host publisher must independently verify source/tree/time,
version, standard ZIP rules, exact archive members, original manifest bytes
and checksums against the committed source. The helper alone does not prove
source provenance, retained platform qualification or safe public visibility.

## Native integration boundary

Current `.dagu/` jobs run Linux CI, security and dependency checks; the watcher
ignores tags. No native RobotGo release controller or downloads publisher
exists in this source. The retained release workflow still requires its full
exact-source check matrix and six-platform snapshots, and its evidence schema
identifies GitHub Actions. Do not relabel native runs with that provider or
weaken the old preflight's origin guard to make publication appear qualified.

The next integration decision must reuse the protected Dagu/static publisher
pattern for a library: one RobotGo release graph, one lock and one immutable
release root. No binary-app packaging, new daemon, mirror, or cross-repository
runtime dependency is needed. Keep source credentials on the trusted host and
publication authority outside test containers. The reviewed graph must bind
all required approvals, exact source/platform evidence and artifact digests
before the locked publisher exposes the module trio. Native macOS/Windows and
desktop/compositor/hardware capacity remain LAB-440/LAB-441 gates; local Linux
tests and cross-builds do not qualify them.

The approved public protocol is HTTPS vanity discovery with `vcs=mod` and
static Go proxy `@v/list`, version `.info`, `.mod` and `.zip` responses. The
public route serves only approved immutable artifacts and a derived version
catalog; Git objects, credentials, worker state and captures stay private.
Missing versions must fail, writes must be refused, and conflicting retries
must fail without replacing any published byte. A byte-identical retry may
repair only derived catalogs.

## Consumer acceptance

After authorized publication, use an empty credential-free client and default
`GOPROXY=https://proxy.golang.org,direct` and `GOSUMDB=sum.golang.org`, then
repeat with direct vanity discovery. Keep normal checksum verification enabled.
Download `go.izoone.com/robotgo@v1.0.4`, inspect module sums/build information,
and compile a fresh library consumer importing the root, `agent`, `base` and
`input/portal` with `CGO_ENABLED=0`; reference exported types without executing
desktop/input APIs. Also validate the native consumer on qualified native
workers. Prove no first-party GitHub fallback or hidden credentials, reject
missing/corrupt/conflicting inputs and verify immutable retries. A local ZIP
check is only packaging evidence, not public proxy/checksum acceptance.

Synthetic protocol fixtures must use `example.invalid` with any checksum
exception scoped to that fixture. Never submit synthetic production-path bytes
to the public checksum service. Third-party dependencies and the public Go
proxy/checksum services retain their own network and residency boundaries.
