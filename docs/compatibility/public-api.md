# Public Go API Compatibility

Status: Active
Schema: RobotGo public API manifest v1

RobotGo freezes its importable public library API before `v1.0.0-rc.1`.
The blocking `api-compat` CI job compares the current source tree with
a human-reviewable full manifest and validated platform/build-tag deltas in
`api/compat`.

## Stable package discovery

The checker discovers every non-command package in the
`github.com/marang/robotgo` module that has buildable non-test source. The
current stable set is:

- `github.com/marang/robotgo`
- `github.com/marang/robotgo/agent`
- `github.com/marang/robotgo/agent/mcpserver`
- `github.com/marang/robotgo/base`
- `github.com/marang/robotgo/clipboard`
- `github.com/marang/robotgo/cv`
- `github.com/marang/robotgo/input/portal`
- `github.com/marang/robotgo/key`
- `github.com/marang/robotgo/mouse`
- `github.com/marang/robotgo/screen`
- `github.com/marang/robotgo/screen/portal`
- `github.com/marang/robotgo/window`

Go `internal` packages, `package main` commands, test-only directories, and
the configured `cmd` and `examples` trees are excluded. A new importable
library package is included automatically and fails the exact baseline check
until its API is intentionally reviewed.

## Frozen contract

The manifest records exported:

- constants, their types, and exact values;
- variables and types;
- functions and signatures;
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
| `linux-nocgo`, `linux-nocgo-arm64` | Pure-Go Linux/X11 on AMD64 and ARM64; one invariant Linux API |
| `windows-nocgo`, `windows-nocgo-arm64` | Pure-Go Windows on AMD64 and ARM64; one invariant Windows API |
| `darwin-nocgo`, `darwin-nocgo-amd64` | Pure-Go macOS on ARM64 and AMD64; one invariant macOS API |

The main `api-compat` job evaluates all variants except OCR after installing
the native Linux/Wayland/PipeWire headers once. The existing OCR job evaluates
`linux-cgo-ocr` with its Tesseract and Leptonica headers. Cross-platform
non-CGO manifests are loaded with explicit `GOOS`, `GOARCH`, `CGO_ENABLED`,
empty `GOFLAGS`/`GOEXPERIMENT`, `GOENV=off`, and `GOWORK=off`.

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
