# Public Go API Compatibility

Status: Active
Schema: RobotGo public API manifest v1

RobotGo first froze its importable public library API before `v1.0.0-rc.1`.
Intentional post-rc.1 additions and schema changes were re-frozen in
`v1.0.0-rc.2`. Stable `v1.0.0` preserves that contract; only the public
`Version` value changes as release metadata. The blocking `api-compat` CI job
compares the current source tree with a human-reviewable full manifest and validated
platform/build-tag deltas in `api/compat`.

LAB-439 explicitly migrates only the owned module prefix to
`go.izoone.com/robotgo` and updates `Version` for the prepared `v1.0.4` release.
All declarations, JSON tags, third-party identities and 14 variant mappings
retain their contracts; old module versions remain immutable.

## Stable package discovery

The checker discovers every non-command package in the
`go.izoone.com/robotgo` module that has buildable non-test source. The
current stable set is:

- `go.izoone.com/robotgo`
- `go.izoone.com/robotgo/agent`
- `go.izoone.com/robotgo/agent/mcpserver`
- `go.izoone.com/robotgo/base`
- `go.izoone.com/robotgo/clipboard`
- `go.izoone.com/robotgo/cv`
- `go.izoone.com/robotgo/input/portal`
- `go.izoone.com/robotgo/key`
- `go.izoone.com/robotgo/mouse`
- `go.izoone.com/robotgo/screen`
- `go.izoone.com/robotgo/screen/portal`
- `go.izoone.com/robotgo/window`

Go `internal` packages, `package main` commands, test-only directories, and
the configured `cmd` and `examples` trees are excluded. A new importable
library package is included automatically and fails the exact baseline check
until its API is intentionally reviewed.

## Frozen contract

The manifest records exported:

- constants, their types, and exact values;
- variables and types;
- functions and signatures;
- package names as well as import paths;
- defined types, aliases, generic constraints, exported struct fields/tags,
  interface contracts, and effective value/pointer method sets.

Parameter names, documentation text, and private implementation details are
not frozen. A marker records that a public struct contains private fields
without exposing or freezing those fields. Any exported API drift, including
an additive declaration, requires an intentional baseline update. This keeps
stable API changes visible in review instead of guessing whether an addition
was accidental.

## Build variants

`api/compat/config.json` pins the module, exclusions, target architecture,
build tags, and baseline mapping. Blocking coverage includes:

| Variant | Contract |
|---|---|
| `linux-cgo` | Native Linux/X11 |
| `linux-cgo-wayland` | Native Wayland, same public API as native Linux |
| `linux-cgo-portal` | Linux portal-tag additions |
| `linux-cgo-pipewire` | PipeWire implementation, same public API as native Linux |
| `linux-cgo-full` | Wayland + portal + PipeWire, with only the portal-tag additions |
| `linux-cgo-ocr` | In-process OCR, same public API as native Linux |
| `windows-cgo` | Native Windows on the protected AMD64 runner |
| `darwin-cgo` | Native macOS on the protected ARM64 runner |
| `linux-nocgo`, `linux-nocgo-arm64` | Pure-Go Linux/X11 on AMD64 and ARM64; one invariant Linux API |
| `windows-nocgo`, `windows-nocgo-arm64` | Pure-Go Windows on AMD64 and ARM64; one invariant Windows API |
| `darwin-nocgo`, `darwin-nocgo-amd64` | Pure-Go macOS on ARM64 and AMD64; one invariant macOS API |

The main `api-compat` job evaluates the Linux and cross-compiled Pure-Go
variants after installing the native Linux/Wayland/PipeWire headers once. The
existing OCR job evaluates `linux-cgo-ocr` with its Tesseract and Leptonica
headers. The protected default macOS and Windows test jobs evaluate their
native CGO variants on the matching hosted operating system and architecture.
Cross-platform non-CGO manifests are loaded with explicit `GOOS`, `GOARCH`,
`CGO_ENABLED`, empty `GOFLAGS`/`GOEXPERIMENT`, `GOENV=off`, and `GOWORK=off`.

## Check and update

Check all variants supported by the current Linux host:

```bash
go run ./internal/cmd/apicompat \
  -variant linux-cgo \
  -variant linux-cgo-wayland \
  -variant linux-cgo-portal \
  -variant linux-cgo-pipewire \
  -variant linux-cgo-full \
  -variant linux-nocgo \
  -variant linux-nocgo-arm64 \
  -variant windows-nocgo \
  -variant windows-nocgo-arm64 \
  -variant darwin-nocgo \
  -variant darwin-nocgo-amd64
```

After an intentional public API decision, regenerate only the affected
variants:

```bash
go run ./internal/cmd/apicompat -write -variant linux-cgo
```

Never edit a manifest or delta by hand. Review the generated diff together
with the API implementation, compatibility rationale, tests, docs, and release
impact.
Variants that share a baseline must generate identical APIs; a multi-variant
write fails rather than silently choosing one. A shared baseline can be
updated only through its first configured owner unless that owner and the
alias variant are selected together.

Derived variants store only sorted removals and additions relative to the
full `linux-cgo.api` baseline. Applying a stale removal, adding an existing
entry, naming the wrong base, or producing an invalid package/declaration set
fails closed. This avoids thousands of duplicated lines while preserving the
exact reconstructed API for every variant.

The checker loads only the current, pinned module source and dependencies. It
does not fetch a historical Git ref, release, or API baseline from the
network. Temporary replacement files are removed on every write path.
